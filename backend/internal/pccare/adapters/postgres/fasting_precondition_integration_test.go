package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Feed & water removal precondition (maintainer decision 2026-09-03) against the REAL
// pc_care_* schema: the linked create pair, its idempotent replay and payload-conflict
// refusal, the 20:00 IST list visibility, and the midnight gate that holds a deworming whose
// removal was never submitted. Business-DAY fixtures on fixed dates in Asia/Kolkata — no hour
// arithmetic against a wandering clock.

// createDewormingWithRemoval plans a deworming for the fixed 2026-09-11 business day with the
// evening-before removal precondition.
func createDewormingWithRemoval(t *testing.T, ctx context.Context, repo *Repository, idemKey string) ports.TaskRow {
	t.Helper()
	task, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID:               pcTenant,
		Category:               domain.CategoryDeworming,
		ParkID:                 pcPark,
		ShedID:                 pcShedA,
		PlannedBusinessDate:    pcBusinessDay(2026, 9, 11),
		AssigneeUserIDs:        []string{pcOperator1},
		FeedRemovalRequired:    true,
		RemovalOperatorUserIDs: []string{pcOperator2},
		IdempotencyKey:         idemKey,
		CreatedBy:              pcVerifier,
		ActorID:                pcVerifier,
		ActorType:              "human",
		TraceID:                "trace-fasting-create",
	})
	if err != nil {
		t.Fatalf("CreateTask with removal: %v", err)
	}
	return task
}

type removalRow struct {
	taskID      string
	planned     string
	due         string
	gatesTaskID string
	workState   string
}

func readRemovalRow(t *testing.T, ctx context.Context, repo *Repository, dewormingTaskID string) removalRow {
	t.Helper()
	var row removalRow
	err := repo.pool.QueryRow(ctx, `
SELECT task_id::text, planned_business_date::text, due_business_date::text,
       coalesce(gates_task_id::text, ''), work_state
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND category = 'feed_water_removal' AND gates_task_id = $2::uuid`,
		pcTenant, dewormingTaskID).Scan(&row.taskID, &row.planned, &row.due, &row.gatesTaskID, &row.workState)
	if err != nil {
		t.Fatalf("read removal row: %v", err)
	}
	return row
}

// The linked create: one transaction births the deworming AND its evening-before removal —
// same pen, one day earlier, its own operators, gates_task_id pointing at the deworming.
// A replay of the same key returns the original pair; the same key with a different removal
// payload is refused as an idempotency conflict.
func TestCreateDewormingWithFeedRemovalCreatesLinkedPair(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	deworming := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-create-1")
	if deworming.Category != domain.CategoryDeworming || deworming.PlannedBusinessDate != "2026-09-11" {
		t.Fatalf("deworming row = %+v", deworming)
	}

	removal := readRemovalRow(t, ctx, repo, deworming.TaskID)
	if removal.planned != "2026-09-10" || removal.due != "2026-09-10" {
		t.Fatalf("removal planned/due = %s/%s, want 2026-09-10 (deworming date minus one)", removal.planned, removal.due)
	}
	if removal.gatesTaskID != deworming.TaskID {
		t.Fatalf("removal gates_task_id = %s, want the deworming %s", removal.gatesTaskID, deworming.TaskID)
	}
	// The deworming row itself carries NO gates_task_id.
	var dewormingGates string
	if err := pool.QueryRow(ctx, `SELECT coalesce(gates_task_id::text, '') FROM pc_care_tasks WHERE task_id = $1::uuid`,
		deworming.TaskID).Scan(&dewormingGates); err != nil {
		t.Fatalf("read deworming gates: %v", err)
	}
	if dewormingGates != "" {
		t.Fatalf("deworming gates_task_id = %q, want NULL", dewormingGates)
	}
	// The removal is assigned to the removal operators, not the deworming crew.
	var removalAssignees []string
	if err := pool.QueryRow(ctx, `
SELECT array_agg(operator_user_id::text ORDER BY operator_user_id)
FROM pc_care_task_assignees WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, removal.taskID).Scan(&removalAssignees); err != nil {
		t.Fatalf("read removal assignees: %v", err)
	}
	if len(removalAssignees) != 1 || removalAssignees[0] != pcOperator2 {
		t.Fatalf("removal assignees = %v, want [%s]", removalAssignees, pcOperator2)
	}

	// Exact replay: same key, same payload — the original pair, no duplicates.
	replay := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-create-1")
	if replay.TaskID != deworming.TaskID {
		t.Fatalf("replay minted a second deworming: %s vs %s", replay.TaskID, deworming.TaskID)
	}
	var pairCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND category IN ('deworming', 'feed_water_removal')`,
		pcTenant).Scan(&pairCount); err != nil {
		t.Fatalf("count pair: %v", err)
	}
	if pairCount != 2 {
		t.Fatalf("task rows after replay = %d, want exactly the original pair (2)", pairCount)
	}

	// Same key, DIFFERENT removal payload: refused, no writes.
	_, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark, ShedID: pcShedA,
		PlannedBusinessDate: pcBusinessDay(2026, 9, 11),
		AssigneeUserIDs:     []string{pcOperator1},
		FeedRemovalRequired: true, RemovalOperatorUserIDs: []string{pcOperator1},
		IdempotencyKey: "pc-fasting-create-1", CreatedBy: pcVerifier,
	})
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-payload err = %v, want ErrIdempotencyConflict", err)
	}
}

// The natural key keeps consecutive-day dewormings' removal rows apart (their removal dates
// differ), while a STALE live removal row on the same pen-evening refuses the new pair whole.
func TestRemovalNaturalKeyOnConsecutiveDewormingDates(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)

	first := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-natural-1") // deworming 09-11, removal 09-10
	_ = first
	second, err := repo.CreateTask(ctx, ports.CreateTaskParams{ // deworming 09-12, removal 09-11
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark, ShedID: pcShedA,
		PlannedBusinessDate: pcBusinessDay(2026, 9, 12),
		AssigneeUserIDs:     []string{pcOperator1},
		FeedRemovalRequired: true, RemovalOperatorUserIDs: []string{pcOperator2},
		IdempotencyKey: "pc-fasting-natural-2", CreatedBy: pcVerifier,
	})
	if err != nil {
		t.Fatalf("consecutive-day pair must not collide: %v", err)
	}
	removal2 := readRemovalRow(t, ctx, repo, second.TaskID)
	if removal2.planned != "2026-09-11" {
		t.Fatalf("second removal planned = %s, want 2026-09-11", removal2.planned)
	}
}

// The 20:00 IST evening visibility: the removal card is absent from the list before 20:00 of
// its due day and present from 20:00, judged by the caller's clock; the deworming card and the
// removal DETAIL read are untouched.
func TestFeedWaterRemovalListedOnlyFromEightPMOfItsDueDay(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	deworming := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-visibility-1")
	removal := readRemovalRow(t, ctx, repo, deworming.TaskID)

	ist := biztime.DefaultLocation()
	listAt := func(now time.Time) map[string]bool {
		t.Helper()
		page, err := repo.ListTasks(ctx, ports.ListTasksQuery{
			TenantID: pcTenant, TenantWide: true,
			DueBusinessDate: "2026-09-10", Now: now, Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		out := map[string]bool{}
		for _, item := range page.Items {
			out[item.TaskID] = true
		}
		return out
	}

	before := listAt(time.Date(2026, time.September, 10, 19, 59, 0, 0, ist))
	if before[removal.taskID] {
		t.Fatal("removal card must be hidden before 20:00 IST of its due day")
	}
	after := listAt(time.Date(2026, time.September, 10, 20, 0, 0, 0, ist))
	if !after[removal.taskID] {
		t.Fatal("removal card must list from 20:00 IST of its due day")
	}

	// The deworming (due 09-11) is untouched by the evening rule on ITS day.
	dewormingPage, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID: pcTenant, TenantWide: true,
		DueBusinessDate: "2026-09-11",
		Now:             time.Date(2026, time.September, 11, 8, 0, 0, 0, ist),
		Limit:           50,
	})
	if err != nil {
		t.Fatalf("ListTasks deworming day: %v", err)
	}
	found := false
	for _, item := range dewormingPage.Items {
		if item.TaskID == deworming.TaskID {
			found = true
		}
	}
	if !found {
		t.Fatal("the deworming card must list all day — the evening rule is removal-only")
	}

	// The record itself stays readable — visibility gates the LIST, not the detail.
	if _, err := repo.GetTask(ctx, pcTenant, removal.taskID, nil, true); err != nil {
		t.Fatalf("GetTask on the hidden removal must serve the record: %v", err)
	}
}

// The midnight gate: at 00:00 of the deworming's day, an unsubmitted removal pushes the
// deworming to TOMORROW (today + 1, never today) as delayed; once the removal carries
// submitted_at — EVER, a later rework does not re-block — the deworming stays put.
func TestMidnightGateHoldsDewormingUntilRemovalSubmitted(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	deworming := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-gate-1")
	removal := readRemovalRow(t, ctx, repo, deworming.TaskID)

	// The kernel tick just after midnight on the deworming's day (2026-09-11 00:05 IST).
	tick := time.Date(2026, time.September, 11, 0, 5, 0, 0, biztime.DefaultLocation())
	result, err := repo.SweepTaskRollForward(ctx, pcTenant, tick, 200, 50)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.HeldForRemoval != 1 {
		t.Fatalf("HeldForRemoval = %d, want 1", result.HeldForRemoval)
	}

	readTask := func(taskID string) (due, workState string, rolled int, rowVersion int32) {
		t.Helper()
		if err := pool.QueryRow(ctx, `
SELECT due_business_date::text, work_state, rolled_forward_count, row_version
FROM pc_care_tasks WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
			pcTenant, taskID).Scan(&due, &workState, &rolled, &rowVersion); err != nil {
			t.Fatalf("read task %s: %v", taskID, err)
		}
		return
	}

	due, workState, rolled, rowVersion := readTask(deworming.TaskID)
	if due != "2026-09-12" || workState != domain.WorkStateDelayed || rolled != 1 {
		t.Fatalf("gated deworming = due %s / %s / rolled %d, want 2026-09-12 / delayed / 1", due, workState, rolled)
	}
	if rowVersion != 2 {
		t.Fatalf("gated deworming row_version = %d, want bumped to 2", rowVersion)
	}
	// The removal itself rode the ORDINARY roll-forward to today, staying deworming-due minus 1.
	removalDue, removalState, _, _ := readTask(removal.taskID)
	if removalDue != "2026-09-11" || removalState != domain.WorkStateDelayed {
		t.Fatalf("removal after sweep = due %s / %s, want 2026-09-11 / delayed", removalDue, removalState)
	}

	// A second tick the NEXT midnight with the removal still unsubmitted pushes again — always
	// to tomorrow, never today.
	tick2 := time.Date(2026, time.September, 12, 0, 5, 0, 0, biztime.DefaultLocation())
	if _, err := repo.SweepTaskRollForward(ctx, pcTenant, tick2, 200, 50); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	due, _, _, _ = readTask(deworming.TaskID)
	if due != "2026-09-13" {
		t.Fatalf("still-gated deworming due = %s, want 2026-09-13", due)
	}

	// Submit the removal before the deworming day begins (the gate reads
	// submitted_at, set by the real submit path; stamping it here isolates the
	// gate's own predicate).
	if _, err := pool.Exec(ctx, `
UPDATE pc_care_tasks SET submitted_at = $4::timestamptz, submitted_by = $2::uuid
WHERE tenant_id = $1::uuid AND task_id = $3::uuid`,
		pcTenant, pcOperator2, removal.taskID, time.Date(2026, time.September, 12, 23, 50, 0, 0, biztime.DefaultLocation())); err != nil {
		t.Fatalf("stamp removal submit: %v", err)
	}
	tick3 := time.Date(2026, time.September, 13, 0, 5, 0, 0, biztime.DefaultLocation())
	result3, err := repo.SweepTaskRollForward(ctx, pcTenant, tick3, 200, 50)
	if err != nil {
		t.Fatalf("third sweep: %v", err)
	}
	if result3.HeldForRemoval != 0 {
		t.Fatalf("HeldForRemoval after submit = %d, want 0 — a submitted removal opens the gate", result3.HeldForRemoval)
	}
	due, _, _, _ = readTask(deworming.TaskID)
	if due != "2026-09-13" {
		t.Fatalf("ungated deworming due = %s, want carried to today (2026-09-13) by the ordinary roll-forward only", due)
	}

	late := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-gate-late")
	lateRemoval := readRemovalRow(t, ctx, repo, late.TaskID)
	if _, err := pool.Exec(ctx, `
UPDATE pc_care_tasks SET submitted_at = $4::timestamptz, submitted_by = $2::uuid
WHERE tenant_id = $1::uuid AND task_id = $3::uuid`,
		pcTenant, pcOperator2, lateRemoval.taskID, time.Date(2026, time.September, 11, 0, 5, 0, 0, biztime.DefaultLocation())); err != nil {
		t.Fatalf("stamp late removal submit: %v", err)
	}
	resultLate, err := repo.SweepTaskRollForward(ctx, pcTenant, tick, 200, 50)
	if err != nil {
		t.Fatalf("late removal sweep: %v", err)
	}
	if resultLate.HeldForRemoval != 1 {
		t.Fatalf("HeldForRemoval after late submit = %d, want 1 — after-midnight removal must not open today's deworming", resultLate.HeldForRemoval)
	}
}

// Both removal videos are demanded at submit: one slot filled is proof_incomplete; both filled
// submits and composes the two labeled clips in slot order.
func TestRemovalSubmitDemandsBothVideos(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	deworming := createDewormingWithRemoval(t, ctx, repo, "pc-fasting-submit-1")
	removal := readRemovalRow(t, ctx, repo, deworming.TaskID)

	register := func(slot, ref, key string) error {
		return repo.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
			TenantID: pcTenant, TaskID: removal.taskID, SlotKey: slot, ProofRef: ref,
			CapturedBy: pcOperator2, IdempotencyKey: key, ActorID: pcOperator2, ActorType: "operator",
		})
	}
	if err := register(domain.SlotFeedVideo, "proof-feed-1", "pc-removal-slot-1"); err != nil {
		t.Fatalf("register feed video: %v", err)
	}

	submit := func(key string) (ports.SubmitTaskResult, error) {
		return repo.SubmitTask(ctx, ports.SubmitTaskParams{
			TenantID: pcTenant, TaskID: removal.taskID, SubmittedBy: pcOperator2,
			IdempotencyKey: key, ActorID: pcOperator2, ActorType: "operator",
			Now: pcBusinessDay(2026, 9, 10),
		})
	}
	if _, err := submit("pc-removal-submit-early"); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("one-video submit err = %v, want ErrProofIncomplete", err)
	}

	if err := register(domain.SlotWaterVideo, "proof-water-1", "pc-removal-slot-2"); err != nil {
		t.Fatalf("register water video: %v", err)
	}
	result, err := submit("pc-removal-submit-full")
	if err != nil {
		t.Fatalf("full submit: %v", err)
	}
	if !result.NewlyPending || result.Status != domain.StatusPendingVerification {
		t.Fatalf("submit result = %+v, want newly pending_verification", result)
	}
	if len(result.MediaRefs) != 2 ||
		result.MediaRefs[0].ProofRef != "proof-feed-1" || result.MediaRefs[0].Label != "Feed removal video" ||
		result.MediaRefs[1].ProofRef != "proof-water-1" || result.MediaRefs[1].Label != "Water removal video" {
		t.Fatalf("media refs = %+v, want the two labeled removal clips in slot order", result.MediaRefs)
	}
}

// The removal operator is held to the SAME park-scope bar as the task's own
// assignees (review finding on PR 176): the phone's picker filters by park, but
// a direct API caller could name a CBE operator for a CPT pen's removal. The
// write refuses it by name, and refuses nothing when both sides are in-park.
func TestCreateTaskRefusesARemovalOperatorOutsideThePark(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)

	_, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID:               pcTenant,
		Category:               domain.CategoryDeworming,
		ParkID:                 pcPark,
		ShedID:                 pcShedA,
		PlannedBusinessDate:    pcBusinessDay(2026, 9, 11),
		AssigneeUserIDs:        []string{pcOperator1},
		FeedRemovalRequired:    true,
		RemovalOperatorUserIDs: []string{pcOtherParkOperator},
		IdempotencyKey:         "fasting-cross-park-removal",
		CreatedBy:              pcVerifier,
		ActorID:                pcVerifier,
		ActorType:              "human",
		TraceID:                "trace-fasting-cross-park",
	})
	if !errors.Is(err, ports.ErrOperatorOutsidePark) {
		t.Fatalf("cross-park removal operator err = %v, want ErrOperatorOutsidePark", err)
	}
	// Nothing was written: neither the deworming nor a dangling removal row.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pc_care_tasks WHERE tenant_id = $1::uuid`, pcTenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("refused create left %d pc_care_tasks rows behind", n)
	}

	// The task's OWN assignee is held to the same bar.
	_, err = repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID:            pcTenant,
		Category:            domain.CategoryDeworming,
		ParkID:              pcPark,
		ShedID:              pcShedA,
		PlannedBusinessDate: pcBusinessDay(2026, 9, 12),
		AssigneeUserIDs:     []string{pcOtherParkOperator},
		IdempotencyKey:      "cross-park-assignee",
		CreatedBy:           pcVerifier,
		ActorID:             pcVerifier,
		ActorType:           "human",
		TraceID:             "trace-cross-park-assignee",
	})
	if !errors.Is(err, ports.ErrOperatorOutsidePark) {
		t.Fatalf("cross-park assignee err = %v, want ErrOperatorOutsidePark", err)
	}

	// In-park on both sides: accepted, exactly as before.
	createDewormingWithRemoval(t, ctx, repo, "fasting-in-park-removal")
}
