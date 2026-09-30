package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type capturedAlerts struct{ skips []pccareapp.RepeatSkip }

func (c *capturedAlerts) NotifyRepeatSkipped(_ context.Context, _ string, s pccareapp.RepeatSkip) error {
	c.skips = append(c.skips, s)
	return nil
}

func repeatRules(every int) domain.Rules {
	seed := domain.SeededRules()
	cats := map[string]*domain.CategoryRules{}
	for k, v := range seed.Categories {
		copied := *v
		cats[k] = &copied
	}
	cats[domain.CategoryFumigation].RepeatEveryDays = every
	seed.Categories = cats
	seed.Version = 7
	return seed
}

func repeatService(repo *Repository, every int, now time.Time) *pccareapp.Service {
	return pccareapp.NewService(repo).
		WithRoundStore(repo).
		WithSOPRules(ports.StaticRules{Rules: repeatRules(every)}, repo).
		WithNow(func() time.Time { return now })
}

func istNoon(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, biztime.DefaultLocation())
}

// PC CARE REPEAT (maintainer instruction 2026-09-30) against the real schema: a fumigation with
// "repeat every 30 days" is planned again for the same pen and the same operators 30 days after
// its PLANNED date (two days ahead), exactly once; the chain continues from the repeat; an
// operator who can no longer be assigned drops off; when nobody can, the pen is skipped and the
// planner alerted ONCE; clearing the setting stops it.
func TestFumigationRepeatsForTheSamePenAndOperators(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	source, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 1),
		AssigneeUserIDs:     []string{pcOperator1, pcOperator2},
		IdempotencyKey:      "fum-repeat-source", CreatedBy: pcVerifier, ActorID: pcVerifier, ActorType: "human",
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	sourceTaskID := source.Pens[0].TaskID

	type planned struct {
		date      string
		assignees []string
		repeatOf  string
		version   int
	}
	latest := func() planned {
		t.Helper()
		var p planned
		var repeatOf *string
		if err := pool.QueryRow(ctx, `
SELECT t.planned_business_date::text, t.repeat_of_task_id::text, coalesce(t.sop_version, 0),
       (SELECT array_agg(a.operator_user_id::text) FROM pc_care_task_assignees a WHERE a.task_id = t.task_id)
FROM pc_care_tasks t
WHERE t.tenant_id = $1::uuid AND t.category = 'fumigation' AND t.shed_id = $2::uuid
ORDER BY t.planned_business_date DESC LIMIT 1`, pcTenant, pcShedA).Scan(&p.date, &repeatOf, &p.version, &p.assignees); err != nil {
			t.Fatalf("read latest: %v", err)
		}
		if repeatOf != nil {
			p.repeatOf = *repeatOf
		}
		sort.Strings(p.assignees)
		return p
	}
	count := func() int {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM pc_care_tasks WHERE tenant_id = $1::uuid AND category = 'fumigation'`, pcTenant).Scan(&n)
		return n
	}
	alerts := &capturedAlerts{}

	// Too early: 09-01 + 30 = 10-01, and on 09-28 the two-day horizon reaches only 09-30.
	if res, err := repeatService(repo, 30, istNoon(2026, 9, 28)).RunRepeat(ctx, pcTenant, repo, alerts, 100); err != nil || res.PensCreated != 0 {
		t.Fatalf("09-28: %+v %v, want nothing yet", res, err)
	}
	// Two days ahead: planned for 10-01, same pen, same two operators, pinned to the published card.
	res, err := repeatService(repo, 30, istNoon(2026, 9, 29)).RunRepeat(ctx, pcTenant, repo, alerts, 100)
	if err != nil || res.PensCreated != 1 {
		t.Fatalf("09-29: %+v %v, want one pen repeated", res, err)
	}
	want := []string{pcOperator1, pcOperator2}
	sort.Strings(want)
	if got := latest(); got.date != "2026-10-01" || got.repeatOf != sourceTaskID || got.version != 7 || len(got.assignees) != 2 || got.assignees[0] != want[0] || got.assignees[1] != want[1] {
		t.Fatalf("repeat = %+v, want 2026-10-01 from %s on v7 with both operators", got, sourceTaskID)
	}
	// Idempotent: a second tick the same day and a tick a week later make nothing more.
	for _, now := range []time.Time{istNoon(2026, 9, 29), istNoon(2026, 10, 6)} {
		if res, err := repeatService(repo, 30, now).RunRepeat(ctx, pcTenant, repo, alerts, 100); err != nil || res.PensCreated != 0 {
			t.Fatalf("%s: %+v %v, want nothing (already repeated)", now.Format("01-02"), res, err)
		}
	}
	if count() != 2 {
		t.Fatalf("fumigation tasks = %d, want 2", count())
	}

	// Operator 2 can no longer be assigned: the chain continues from the REPEAT (10-01 + 30)
	// with operator 1 alone. Whether the 10-01 task was done does not matter.
	if _, err := pool.Exec(ctx, `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, pcTenant, pcOperator2); err != nil {
		t.Fatalf("revoke operator 2: %v", err)
	}
	if res, err := repeatService(repo, 30, istNoon(2026, 10, 29)).RunRepeat(ctx, pcTenant, repo, alerts, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("10-29: %+v %v, want the chain to continue", res, err)
	}
	if got := latest(); got.date != "2026-10-31" || len(got.assignees) != 1 || got.assignees[0] != pcOperator1 {
		t.Fatalf("second repeat = %+v, want 2026-10-31 with operator 1 only", got)
	}

	// Nobody left: skipped, the planner alerted ONCE, nobody substituted.
	if _, err := pool.Exec(ctx, `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, pcTenant, pcOperator1); err != nil {
		t.Fatalf("revoke operator 1: %v", err)
	}
	for i := 0; i < 2; i++ {
		res, err := repeatService(repo, 30, istNoon(2026, 11, 28)).RunRepeat(ctx, pcTenant, repo, alerts, 100)
		if err != nil || res.PensCreated != 0 {
			t.Fatalf("11-28 tick %d: %+v %v, want no task", i, res, err)
		}
	}
	if count() != 3 {
		t.Fatalf("fumigation tasks = %d, want 3 (the skipped pen is not planned)", count())
	}
	if len(alerts.skips) != 1 || alerts.skips[0].PlannerUserID != pcVerifier || alerts.skips[0].Reason != pccareapp.RepeatSkipNoOperator ||
		len(alerts.skips[0].PenLabels) != 1 || alerts.skips[0].PenLabels[0] != "Castro" || alerts.skips[0].DueDate.Format("2006-01-02") != "2026-11-30" {
		t.Fatalf("alerts = %+v, want ONE to the planner naming Castro for 2026-11-30", alerts.skips)
	}
}

// Clearing the card's repeat (0) makes nothing, however late it is.
func TestNoRepeatWhenTheCardSaysNone(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	if _, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens: []domain.RoundPen{{ShedID: pcShedA}}, PlannedBusinessDate: pcBusinessDay(2026, 9, 1),
		AssigneeUserIDs: []string{pcOperator1}, IdempotencyKey: "fum-norepeat", CreatedBy: pcVerifier, ActorID: pcVerifier,
	}); err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	res, err := repeatService(repo, 0, istNoon(2026, 12, 1)).RunRepeat(ctx, pcTenant, repo, &capturedAlerts{}, 100)
	if err != nil || res.PensCreated != 0 {
		t.Fatalf("%+v %v, want nothing", res, err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM pc_care_tasks WHERE tenant_id = $1::uuid AND category = 'fumigation'`, pcTenant).Scan(&n)
	if n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
}
