package postgres

import (
	"context"
	"testing"
	"time"

	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

func rotationService(repo *Repository, gap int, now time.Time) *pccareapp.Service {
	seed := domain.SeededRules()
	cats := map[string]*domain.CategoryRules{}
	for k, v := range seed.Categories {
		copied := *v
		cats[k] = &copied
	}
	cats[domain.CategoryFumigation].RepeatMode = domain.RepeatModeRotation
	cats[domain.CategoryFumigation].RotationGapDays = gap
	seed.Categories = cats
	seed.Version = rotationSOPVersion
	return pccareapp.NewService(repo).
		WithRoundStore(repo).
		WithSOPRules(ports.StaticRules{Rules: seed}, repo).
		WithNow(func() time.Time { return now })
}

const rotationSOPVersion = 9

// PC CARE ROTATION (maintainer instruction 2026-10-02) against the real schema. CPT holds Castro
// (undivided) and Godel 1 with Part 1, Part 2, Part 3 (EMPTY) and Part 10. The rotation goes
// Castro -> Godel 1 - Part 1 -> Part 2 -> Part 10 (natural order, the empty pen skipped) ->
// Castro again after the gap; ONE pen per day, each planned only once the previous pen is
// SUBMITTED, for the day after; same operators; a closed pen stops it; nobody left = one alert.
func TestFumigationRotatesOnePenADayThroughOccupiedPens(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual'), ($1::uuid, $2::uuid, 'Part 10', '10', 'active', 'manual')
ON CONFLICT DO NOTHING`, pcTenant, covShedGodel); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|", covShedGodel+"|Part 1", covShedGodel+"|Part 2", covShedGodel+"|Part 10")

	first, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}},
		PlannedBusinessDate: pcBusinessDay(2026, 10, 1),
		AssigneeUserIDs:     []string{pcOperator1, pcOperator2},
		SOPVersion:          rotationSOPVersion,
		IdempotencyKey:      "fum-rotation-start", CreatedBy: pcVerifier, ActorID: pcVerifier, ActorType: "human",
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}

	type pen struct {
		taskID, label, date, repeatOf string
		operators                     int
	}
	latest := func() pen {
		t.Helper()
		var p pen
		var repeatOf *string
		if err := pool.QueryRow(ctx, `
SELECT t.task_id::text, s.name || coalesce(' - ' || nullif(t.partition_label, ''), ''), t.planned_business_date::text,
       t.repeat_of_task_id::text, (SELECT count(*) FROM pc_care_task_assignees a WHERE a.task_id = t.task_id)
FROM pc_care_tasks t JOIN locations s ON s.location_id = t.shed_id
WHERE t.tenant_id = $1::uuid AND t.category = 'fumigation'
ORDER BY t.planned_business_date DESC, t.created_at DESC LIMIT 1`, pcTenant).Scan(&p.taskID, &p.label, &p.date, &repeatOf, &p.operators); err != nil {
			t.Fatalf("read latest: %v", err)
		}
		if repeatOf != nil {
			p.repeatOf = *repeatOf
		}
		return p
	}
	submit := func(taskID string, y int, m time.Month, d int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE pc_care_tasks SET status = 'pending_verification', submitted_at = $3
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, taskID, istNoon(y, m, d)); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	tick := func(gap int, now time.Time) pccareapp.RotationResult {
		t.Helper()
		res, err := rotationService(repo, gap, now).RunRotation(ctx, pcTenant, repo, nil, 100)
		if err != nil {
			t.Fatalf("RunRotation %s: %v", now.Format("01-02"), err)
		}
		return res
	}
	expect := func(label, date, from string) pen {
		t.Helper()
		got := latest()
		if got.label != label || got.date != date || got.repeatOf != from || got.operators != 2 {
			t.Fatalf("next pen = %+v, want %s on %s from %s with both operators", got, label, date, from)
		}
		return got
	}

	// Not submitted yet: nothing moves, however late.
	if res := tick(2, istNoon(2026, 10, 3)); res.PensCreated != 0 {
		t.Fatalf("unsubmitted: %+v, want nothing", res)
	}
	submit(first.Pens[0].TaskID, 2026, 10, 1)
	if res := tick(2, istNoon(2026, 10, 1)); res.PensCreated != 1 {
		t.Fatalf("after Castro: %+v, want one pen", res)
	}
	p1 := expect("Godel 1 - Part 1", "2026-10-02", first.Pens[0].TaskID)
	// Once only.
	if res := tick(2, istNoon(2026, 10, 1)); res.PensCreated != 0 {
		t.Fatalf("second tick: %+v, want nothing", res)
	}
	// A LATE submit (10-04) moves the next pen to the day after it, never two pens in one day.
	submit(p1.taskID, 2026, 10, 4)
	tick(2, istNoon(2026, 10, 4))
	p2 := expect("Godel 1 - Part 2", "2026-10-05", p1.taskID)
	submit(p2.taskID, 2026, 10, 5)
	tick(2, istNoon(2026, 10, 5))
	// Part 3 is empty and skipped; Part 10 comes after Part 2.
	p10 := expect("Godel 1 - Part 10", "2026-10-06", p2.taskID)
	submit(p10.taskID, 2026, 10, 6)
	// The wrap waits the 2-day gap: Castro is due 10-09, beyond the 10-06 tick's 10-08 horizon.
	if res := tick(2, istNoon(2026, 10, 6)); res.PensCreated != 0 {
		t.Fatalf("10-06 wrap: %+v, want nothing before the horizon", res)
	}
	tick(2, istNoon(2026, 10, 7))
	again := expect("Castro", "2026-10-09", p10.taskID)

	// Closing the open pen stops the rotation: it is never submitted, so nothing follows it.
	var againRound string
	if err := pool.QueryRow(ctx, `SELECT round_id::text FROM pc_care_tasks WHERE task_id = $1::uuid`, again.taskID).Scan(&againRound); err != nil {
		t.Fatalf("round of the open pen: %v", err)
	}
	if err := repo.CloseRound(ctx, ports.CloseRoundParams{TenantID: pcTenant, RoundID: againRound, Reason: "Rotation paused", ClosedBy: pcVerifier, ActorID: pcVerifier}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if res := tick(2, istNoon(2026, 10, 20)); res.PensCreated != 0 {
		t.Fatalf("closed: %+v, want the rotation stopped", res)
	}
}

func TestRotationDoesNotWakeTasksFromBeforeTheRotatingSOPVersion(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|", covShedGodel+"|Part 1")

	old, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens: []domain.RoundPen{{ShedID: pcShedA}}, PlannedBusinessDate: pcBusinessDay(2026, 9, 30),
		AssigneeUserIDs: []string{pcOperator1}, SOPVersion: rotationSOPVersion - 1,
		IdempotencyKey: "fum-before-rotation", CreatedBy: pcVerifier, ActorID: pcVerifier, ActorType: "human",
	})
	if err != nil {
		t.Fatalf("CreateRound old: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE pc_care_tasks SET status = 'pending_verification', submitted_at = $3 WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, old.Pens[0].TaskID, istNoon(2026, 9, 30)); err != nil {
		t.Fatalf("submit old: %v", err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pc_care_tasks WHERE tenant_id = $1::uuid AND category = 'fumigation'`, pcTenant).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 0 {
		t.Fatalf("old task tick: %+v %v, want nothing until a planner starts the rotation", res, err)
	}
	if count() != 1 {
		t.Fatalf("tasks after old task tick = %d, want 1", count())
	}

	start, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens: []domain.RoundPen{{ShedID: pcShedA}}, PlannedBusinessDate: pcBusinessDay(2026, 10, 1),
		AssigneeUserIDs: []string{pcOperator1}, SOPVersion: rotationSOPVersion,
		IdempotencyKey: "fum-rotation-version-start", CreatedBy: pcVerifier, ActorID: pcVerifier, ActorType: "human",
	})
	if err != nil {
		t.Fatalf("CreateRound start: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE pc_care_tasks SET status = 'pending_verification', submitted_at = $3 WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, start.Pens[0].TaskID, istNoon(2026, 10, 1)); err != nil {
		t.Fatalf("submit start: %v", err)
	}
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("fresh start tick: %+v %v, want the hand-started rotating pen to continue", res, err)
	}
}

// Nobody left to do it: the rotation stops, the planner is alerted ONCE naming the pen it would
// have gone to, and nobody is substituted.
func TestRotationStopsAndAlertsOnceWhenNoOperatorIsLeft(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|", covShedGodel+"|Part 1")
	first, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens: []domain.RoundPen{{ShedID: pcShedA}}, PlannedBusinessDate: pcBusinessDay(2026, 10, 1),
		AssigneeUserIDs: []string{pcOperator1}, SOPVersion: rotationSOPVersion, IdempotencyKey: "fum-rotation-alone", CreatedBy: pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE pc_care_tasks SET status = 'pending_verification', submitted_at = $3 WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, first.Pens[0].TaskID, istNoon(2026, 10, 1)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, pcTenant, pcOperator1); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	alerts := &capturedAlerts{}
	for i := 0; i < 2; i++ {
		res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, alerts, 100)
		if err != nil || res.PensCreated != 0 {
			t.Fatalf("tick %d: %+v %v, want no task", i, res, err)
		}
	}
	if len(alerts.skips) != 1 || !alerts.skips[0].Rotation || alerts.skips[0].PlannerUserID != pcVerifier ||
		len(alerts.skips[0].PenLabels) != 1 || alerts.skips[0].PenLabels[0] != "Godel 1 - Part 1" || alerts.skips[0].DueDate.Format("2006-01-02") != "2026-10-02" {
		t.Fatalf("alerts = %+v, want ONE rotation alert naming Godel 1 - Part 1 for 2026-10-02", alerts.skips)
	}
}

const rotShedOtherPark = "9c000000-0000-4000-8000-000000004401"

func rotationStart(t *testing.T, ctx context.Context, repo *Repository, parkID string, pens []domain.RoundPen, operators []string, key string) ports.RoundRow {
	t.Helper()
	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: parkID, Pens: pens,
		PlannedBusinessDate: pcBusinessDay(2026, 10, 1), AssigneeUserIDs: operators, SOPVersion: rotationSOPVersion,
		IdempotencyKey: key, CreatedBy: pcVerifier, ActorID: pcVerifier, ActorType: "human",
	})
	if err != nil {
		t.Fatalf("CreateRound %s: %v", key, err)
	}
	return round
}

func rotationSubmit(t *testing.T, ctx context.Context, repo *Repository, taskID string) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `UPDATE pc_care_tasks SET status = 'pending_verification', submitted_at = $3 WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, taskID, istNoon(2026, 10, 1)); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

func rotationPlanned(t *testing.T, ctx context.Context, repo *Repository, repeatOf string) (label string, operators int, n int) {
	t.Helper()
	if err := repo.pool.QueryRow(ctx, `
SELECT coalesce(max(s.name || coalesce(' - ' || nullif(t.partition_label, ''), '')), ''),
       coalesce(max((SELECT count(*) FROM pc_care_task_assignees a WHERE a.task_id = t.task_id)), 0)::int, count(*)::int
FROM pc_care_tasks t JOIN locations s ON s.location_id = t.shed_id
WHERE t.tenant_id = $1::uuid AND t.repeat_of_task_id = $2::uuid`, pcTenant, repeatOf).Scan(&label, &operators, &n); err != nil {
		t.Fatalf("read planned: %v", err)
	}
	return label, operators, n
}

// ONE-TO-MANY: a hand-planned round of TWO pens, each holding several animals, worked by two
// operators. The rotation waits for EVERY pen of the round, goes on from the LAST pen in pen order,
// plans exactly one pen, and the residents and assignees never multiply it.
func TestRotationOneToManyRoundOfSeveralPensWaitsForEveryPen(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	for i := 0; i < 3; i++ {
		coverageResidents(t, ctx, repo, pcPark, pcShedA+"|", covShedGodel+"|Part 1", covShedGodel+"|Part 2")
	}
	round := rotationStart(t, ctx, repo, pcPark, []domain.RoundPen{{ShedID: covShedGodel, PartitionLabel: "Part 1"}, {ShedID: pcShedA}},
		[]string{pcOperator1, pcOperator2}, "rot-otm")
	byLabel := map[string]string{}
	for _, p := range round.Pens {
		byLabel[p.PartitionLabel] = p.TaskID
	}
	rotationSubmit(t, ctx, repo, byLabel[""])
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 0 {
		t.Fatalf("half submitted: %+v %v, want nothing", res, err)
	}
	rotationSubmit(t, ctx, repo, byLabel["Part 1"])
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("all submitted: %+v %v, want one pen", res, err)
	}
	// Castro sorts before Godel 1, so the round's last pen is Godel 1 - Part 1 and the next is Part 2.
	label, operators, n := rotationPlanned(t, ctx, repo, byLabel["Part 1"])
	if label != "Godel 1 - Part 2" || operators != 2 || n != 1 {
		t.Fatalf("next = %q ops=%d n=%d, want one Godel 1 - Part 2 with 2 operators", label, operators, n)
	}
}

// PAGE BOUNDARY: two parks rotate at once and the tick limit is ONE. Each tick plans one park's
// next pen; across ticks both are planned exactly once and nothing is planned twice.
func TestRotationPaginationPageBoundaryLimitPerTickResumes(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedRotationOtherPark(t, ctx, repo)
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|")
	coverageResidents(t, ctx, repo, pcOtherPark, rotShedOtherPark+"|")
	cpt := rotationStart(t, ctx, repo, pcPark, []domain.RoundPen{{ShedID: pcShedA}}, []string{pcOperator1}, "rot-page-cpt")
	cbe := rotationStart(t, ctx, repo, pcOtherPark, []domain.RoundPen{{ShedID: rotShedOtherPark}}, []string{pcOtherParkOperator}, "rot-page-cbe")
	rotationSubmit(t, ctx, repo, cpt.Pens[0].TaskID)
	rotationSubmit(t, ctx, repo, cbe.Pens[0].TaskID)
	total := 0
	for i := 0; i < 3; i++ {
		res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 1)
		if err != nil || res.PensCreated > 1 {
			t.Fatalf("tick %d: %+v %v, want at most one pen per tick", i, res, err)
		}
		total += res.PensCreated
	}
	if total != 2 {
		t.Fatalf("pens planned across ticks = %d, want 2 (one per park)", total)
	}
	for _, src := range []string{cpt.Pens[0].TaskID, cbe.Pens[0].TaskID} {
		if _, _, n := rotationPlanned(t, ctx, repo, src); n != 1 {
			t.Fatalf("source %s planned %d times, want 1", src, n)
		}
	}
}

// PARK SCOPE: a park with ONE occupied pen wraps to that same pen -- never to the other park's pen,
// even though the other park's pen sorts after it.
func TestRotationParkScopeNeverCrossesParks(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedRotationOtherPark(t, ctx, repo)
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|")
	coverageResidents(t, ctx, repo, pcOtherPark, rotShedOtherPark+"|")
	cpt := rotationStart(t, ctx, repo, pcPark, []domain.RoundPen{{ShedID: pcShedA}}, []string{pcOperator1}, "rot-scope-cpt")
	rotationSubmit(t, ctx, repo, cpt.Pens[0].TaskID)
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("%+v %v, want one pen", res, err)
	}
	if label, _, n := rotationPlanned(t, ctx, repo, cpt.Pens[0].TaskID); label != "Castro" || n != 1 {
		t.Fatalf("next = %q (n=%d), want Castro again in its own park", label, n)
	}
}

func seedRotationOtherPark(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-Y', 'Yashoda', 'active', $3::uuid, 1) ON CONFLICT (location_id) DO NOTHING`,
		pcTenant, rotShedOtherPark, pcOtherPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}
}

// Stopped by closing a FUTURE pen, restarted by planning an EARLIER pen by hand: the closed pen
// must not keep the rotation stuck, and the stop must still hold until someone restarts it.
func TestRotationRestartsFromAHandPlannedPenAfterAClose(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	coverageResidents(t, ctx, repo, pcPark, pcShedA+"|", covShedGodel+"|Part 1", covShedGodel+"|Part 2")
	first := rotationStart(t, ctx, repo, pcPark, []domain.RoundPen{{ShedID: pcShedA}}, []string{pcOperator1}, "rot-restart-1")
	rotationSubmit(t, ctx, repo, first.Pens[0].TaskID)
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 1)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("first step: %+v %v", res, err)
	}
	var nextRound, nextTask string
	if err := repo.pool.QueryRow(ctx, `SELECT round_id::text, task_id::text FROM pc_care_tasks WHERE repeat_of_task_id = $1::uuid`, first.Pens[0].TaskID).Scan(&nextRound, &nextTask); err != nil {
		t.Fatalf("next pen: %v", err)
	}
	if err := repo.CloseRound(ctx, ports.CloseRoundParams{TenantID: pcTenant, RoundID: nextRound, Reason: "stop", ClosedBy: pcVerifier, ActorID: pcVerifier}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if res, _ := rotationService(repo, 0, istNoon(2026, 10, 3)).RunRotation(ctx, pcTenant, repo, nil, 100); res.PensCreated != 0 {
		t.Fatalf("after close: %+v, want stopped", res)
	}
	// Restart: Godel 1 - Part 2, planned by hand for 10-01 (EARLIER than the closed 10-02 pen).
	restart, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryFumigation, ParkID: pcPark,
		Pens: []domain.RoundPen{{ShedID: covShedGodel, PartitionLabel: "Part 2"}}, PlannedBusinessDate: pcBusinessDay(2026, 10, 1),
		AssigneeUserIDs: []string{pcOperator2}, SOPVersion: rotationSOPVersion, IdempotencyKey: "rot-restart-2", CreatedBy: pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("restart plan: %v", err)
	}
	rotationSubmit(t, ctx, repo, restart.Pens[0].TaskID)
	if res, err := rotationService(repo, 0, istNoon(2026, 10, 3)).RunRotation(ctx, pcTenant, repo, nil, 100); err != nil || res.PensCreated != 1 {
		t.Fatalf("restart: %+v %v, want the rotation to carry on from the hand-planned pen", res, err)
	}
	if label, _, n := rotationPlanned(t, ctx, repo, restart.Pens[0].TaskID); label != "Castro" || n != 1 {
		t.Fatalf("after restart next = %q n=%d, want Castro (wrap after Part 2)", label, n)
	}
}
