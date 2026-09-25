package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
)

func sessionPlanCommand(park, key string, sessions ...domain.SessionPlanEntry) domain.SetSessionPlanCommand {
	return domain.SetSessionPlanCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-09-25",
			IdempotencyKey: key, RequestFingerprint: "fp-" + key,
		},
		ParkID:   park,
		Sessions: sessions,
	}
}

func activeSessions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park string) map[int32]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT session_no, session_label || '=' || split_fraction::text
FROM feed_session_templates
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND status = 'active'`, fcTenant, park)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	defer rows.Close()
	out := map[int32]string{}
	for rows.Next() {
		var no int32
		var v string
		if err := rows.Scan(&no, &v); err != nil {
			t.Fatalf("scan session: %v", err)
		}
		out[no] = v
	}
	return out
}

// A park with NO sessions -- exactly how a park added on Configuration > Items & settings starts --
// is given them through this write; a replay is idempotent, an identical plan is `unchanged`, an
// edit corrects in place and retires what it leaves out, and a session still serving a feed cannot
// be retired.
func TestSetSessionPlanGivesANewParkItsSessions(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if got := activeSessions(t, ctx, pool, fcOtherPark); len(got) != 0 {
		t.Fatalf("fixture park must start with no sessions, got %v", got)
	}
	plan := []domain.SessionPlanEntry{{SessionNo: 1, Label: "Morning", SplitFraction: "0.6000"}, {SessionNo: 2, Label: "Evening", SplitFraction: "0.4000"}}
	res, err := repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-1", plan...))
	if err != nil {
		t.Fatalf("set plan: %v", err)
	}
	if res.Outcome != domain.OutcomeInserted {
		t.Fatalf("first plan for a park outcome = %q, want inserted", res.Outcome)
	}
	if got := activeSessions(t, ctx, pool, fcOtherPark); got[1] != "Morning=0.6000" || got[2] != "Evening=0.4000" || len(got) != 2 {
		t.Fatalf("sessions = %v", got)
	}
	replay, err := repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-1", plan...))
	if err != nil || !replay.Replayed {
		t.Fatalf("exact replay must return the original result, got %+v, %v", replay, err)
	}
	same, err := repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-2", plan...))
	if err != nil || same.Outcome != domain.OutcomeUnchanged {
		t.Fatalf("identical plan outcome = %+v, %v; want unchanged", same, err)
	}

	edited, err := repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-3",
		domain.SessionPlanEntry{SessionNo: 1, Label: "All day", SplitFraction: "1.0000"}))
	if err != nil || edited.Outcome != domain.OutcomeCorrected {
		t.Fatalf("edit outcome = %+v, %v; want corrected", edited, err)
	}
	if got := activeSessions(t, ctx, pool, fcOtherPark); len(got) != 1 || got[1] != "All day=1.0000" {
		t.Fatalf("session 2 must be retired and session 1 edited, got %v", got)
	}

	// Bring session 2 back and give it a feed; removing it again must be refused.
	if _, err := repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-4", plan...)); err != nil {
		t.Fatalf("restore plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, valid_from)
VALUES ($1::uuid, $2::uuid, 2, 1, 'Concentrate', DATE '2026-09-25')`, fcTenant, fcOtherPark); err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	_, err = repo.SetSessionPlan(ctx, sessionPlanCommand(fcOtherPark, "plan-5",
		domain.SessionPlanEntry{SessionNo: 1, Label: "All day", SplitFraction: "1.0000"}))
	if !errors.Is(err, ports.ErrSessionHasFeeds) {
		t.Fatalf("retiring a session that serves a feed must be refused, got %v", err)
	}
	if got := activeSessions(t, ctx, pool, fcOtherPark); len(got) != 2 {
		t.Fatalf("a refused plan must write nothing, got %v", got)
	}
	// The other park is untouched throughout.
	if got := activeSessions(t, ctx, pool, fcPark); len(got) != 0 {
		t.Fatalf("another park's sessions must not change, got %v", got)
	}
}
