package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	tasksboard "github.com/vgoats/goatos/backend/internal/tasks/adapters/boardsource"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	boarddomain "github.com/vgoats/goatos/backend/internal/workboard/domain"
	boardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// KID STAGE SHIFT TASKS, END TO END ON THE PRODUCTION PATH (maintainer decision 2026-09-30,
// docs/decisions/kid-stage-shift-tasks.md). Only what a farm has before the rule acts is seeded:
// the mother, the twins on K0 with their birth recorded, and the stage vocabulary. The litter's
// workflow is opened by the real goat.created consumer, the kids are moved by a REAL growth
// shifting (raised through the route, approved, completed through the apply transaction), and the
// goat.stage_changed events that apply wrote are delivered by the REAL outbox relay to the real
// litter consumer. Nothing about the task is written by the test.

const (
	kidShiftMother = "00000000-0000-4000-8000-00000000fa01"
	kidShiftKidA   = "00000000-0000-4000-8000-00000000fa02"
	kidShiftKidB   = "00000000-0000-4000-8000-00000000fa03"
	kidShiftBirth  = "00000000-0000-4000-8000-00000000fa0e"
)

// seedLitter records twins born at bornAt to kidShiftMother: the goats rows (on stage, with the
// birth date and time the opener anchors on) and the goat_births litter rows.
func seedLitter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stage string, bornAt time.Time) {
	t.Helper()
	seedApprovalGoatWithStage(t, ctx, pool, kidShiftMother, countsShedA, "Mother")
	ist := bornAt.In(time.FixedZone("IST", 5*3600+1800))
	for i, kid := range []string{kidShiftKidA, kidShiftKidB} {
		seedApprovalGoatWithStage(t, ctx, pool, kid, countsShedA, stage)
		if _, err := pool.Exec(ctx, `UPDATE goats SET dob = $3::date, time_of_birth = $4 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
			countsTenant, kid, ist.Format("2006-01-02"), ist.Format("15:04")); err != nil {
			t.Fatalf("seed kid birth moment: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_births (tenant_id, child_goat_id, mother_goat_id, litter_size, birth_event_id, child_ordinal, count_status, count_approved_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 2, $4::uuid, $5, 'approved', now())`,
			countsTenant, kid, kidShiftMother, kidShiftBirth, i+1); err != nil {
			t.Fatalf("seed goat_births: %v", err)
		}
	}
	for _, s := range []string{"K0", "K1", "K2", "Mother"} {
		seedStageVocabulary(t, ctx, pool, s)
	}
}

// kidShiftStack is the tasks service and a bus carrying the two production consumers this flow
// needs: goat.created opens the workflows, goat.stage_changed / goat.exited judge the litter.
func kidShiftStack(pool *pgxpool.Pool) (*tasksapp.Service, eventbus.Bus) {
	svc := tasksapp.NewService(taskspg.NewRepository(pool, 10*time.Second), slog.New(slog.NewTextHandler(io.Discard, nil)))
	bus := eventbus.NewInProcessBus()
	tasksapp.NewGoatCreatedWorkflowHandler(svc).Register(bus)
	tasksapp.NewLitterShiftWorkflowHandler(svc).Register(bus)
	return svc, bus
}

// relayOutbox runs the real outbox relay over every pending message, dispatching to bus.
func relayOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bus eventbus.Bus) {
	t.Helper()
	schema, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	validator, err := outboxapp.NewEnvelopeValidator(schema)
	if err != nil {
		t.Fatalf("envelope validator: %v", err)
	}
	relay := outboxapp.NewService(outboxpg.NewRepository(pool, 30*time.Second), eventbuspublisher.New(bus), validator,
		outboxapp.Config{Limit: 100, MaxAttempts: 5, LeaseTimeout: time.Minute})
	res, err := relay.RunUntilDrained(ctx)
	if err != nil {
		t.Fatalf("outbox relay: %v (result %+v)", err, res)
	}
	t.Logf("outbox relay: %+v", res)
	if res.FailedCount > 0 {
		rows, qerr := pool.Query(ctx, `SELECT event_type, payload::text FROM outbox_messages WHERE status = 'failed'`)
		if qerr == nil {
			for rows.Next() {
				var et, pl string
				_ = rows.Scan(&et, &pl)
				t.Logf("rejected %s: %v\n%s", et, validator.Validate([]byte(pl)), pl)
			}
			rows.Close()
		}
		t.Fatalf("outbox relay rejected %d message(s)", res.FailedCount)
	}
}

// shiftKidsToK1 is raiseAndApplyGrowth with one difference that matters to THIS test: the operator's
// completion carries a trace id, as the real route always does (appTraceID falls back to
// "missing-trace", never blank). The shared helper leaves it blank, which is harmless until the
// apply's goat.stage_changed is RELAYED -- the envelope schema refuses an empty trace_id.
func shiftKidsToK1(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mux *http.ServeMux, repo *Repository, key string, goatIDs []string, wantAdopt string) {
	t.Helper()
	res := raiseTypedShifting(t, mux, key, domain.ShiftTypeGrowth, goatIDs)
	if res.Code != http.StatusOK {
		t.Fatalf("raise status=%d body=%s", res.Code, res.Body.String())
	}
	var raised struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raised); err != nil || raised.ShiftingEventID == "" {
		t.Fatalf("decode raise: %v", err)
	}
	if category, target, adopt := storedShiftingSnapshot(t, ctx, pool, raised.ShiftingEventID); category != domain.ShiftTypeGrowth || target != "K1" || adopt != wantAdopt {
		t.Fatalf("stored %q/%q/%q, want growth/K1/%q", category, target, adopt, wantAdopt)
	}
	if _, _, err := approveShifting(repo, ctx, key, pendingApprovalForShifting(t, ctx, pool, raised.ShiftingEventID), raised.ShiftingEventID, goatIDs); err != nil {
		t.Fatalf("approval: %v", err)
	}
	done, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID: countsTenant, ShiftingEventID: raised.ShiftingEventID, CompletedByUserID: countsOperator,
		CompletedAt: time.Now().In(biztime.DefaultLocation()), ProofRef: "proof-artifact-" + key,
		IdempotencyKey: "complete-" + key, RequestFingerprint: "complete-fp-" + key, TraceID: "trace-" + key,
	})
	if err != nil || done.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("completion: status=%q err=%v", done.EventStatus, err)
	}
}

// boardShowsLitter reads today's Counts lane of the Work Board through the real board source, as
// the park head (oversee lens) or as an operator (own lens), and reports whether the litter's
// kid-shift row is on it.
func boardShowsLitter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, operatorLens bool) bool {
	t.Helper()
	for _, src := range tasksboard.Sources(pool, 10*time.Second) {
		if src.Module() != boarddomain.ModuleCounts {
			continue
		}
		q := boardports.SourceQuery{TenantID: countsTenant, ParkID: countsPark, BusinessDate: biztime.BusinessDate(time.Now()), Limit: 50}
		if operatorLens {
			q.OwnerUserID = countsOperator
		}
		rows, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatalf("board rows: %v", err)
		}
		for _, r := range rows {
			if r.Title != "" && len(r.Title) >= len("Kid shifts") && r.Title[:len("Kid shifts")] == "Kid shifts" {
				return true
			}
		}
		return false
	}
	t.Fatal("no Counts lane board source")
	return false
}

type litterStep struct {
	status    string
	dueAt     *time.Time
	completed *time.Time
	owner     string
	target    string
}

func litterSteps(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, map[string]litterStep) {
	t.Helper()
	var workflowID, state string
	if err := pool.QueryRow(ctx, `
SELECT workflow_id::text, state FROM workflow_instances
WHERE tenant_id = $1::uuid AND template_key = 'birth_litter' AND subject_ref_id = $2::uuid`,
		countsTenant, kidShiftBirth).Scan(&workflowID, &state); err != nil {
		t.Fatalf("the litter has no kid-shift workflow: %v", err)
	}
	rows, err := pool.Query(ctx, `
SELECT action_key, status, due_at, completed_at, COALESCE(owner_role, ''), COALESCE(target_stage, '')
FROM workflow_actions WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, countsTenant, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]litterStep{}
	for rows.Next() {
		var key string
		var s litterStep
		if err := rows.Scan(&key, &s.status, &s.dueAt, &s.completed, &s.owner, &s.target); err != nil {
			t.Fatal(err)
		}
		out[key] = s
	}
	return state, out
}

// A litter born on the farm: the task opens with K1 due exactly 24 hours after birth, the park head's;
// a hand tap is refused; a growth shifting of ONE twin leaves it open; the second twin's shifting
// completes it at that apply's instant, and K2 falls due exactly seven days later.
func TestKidShiftTasksFollowTheLitterThroughRealGrowthShiftings(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)
	svc, bus := kidShiftStack(pool)

	bornAt := time.Now().UTC().Add(-26 * time.Hour).Truncate(time.Minute)
	seedLitter(t, ctx, pool, "K0", bornAt)

	payload, _ := json.Marshal(map[string]string{"goat_id": kidShiftKidA, "origin_type": "birth", "dam_id": kidShiftMother})
	if err := bus.Publish(ctx, eventbus.Event{Type: tasksapp.EventGoatCreated, TenantID: countsTenant, Key: kidShiftKidA, OccurredAt: bornAt, Payload: payload}); err != nil {
		t.Fatalf("goat.created: %v", err)
	}

	state, steps := litterSteps(t, ctx, pool)
	k1, k2 := steps["shift_to_k1"], steps["shift_to_k2"]
	if state != "open" || k1.status != "pending" || k2.status != "pending" {
		t.Fatalf("opened state=%s k1=%s k2=%s, want open/pending/pending", state, k1.status, k2.status)
	}
	if k1.dueAt == nil || k1.dueAt.Sub(bornAt).Round(time.Minute) != 24*time.Hour {
		t.Fatalf("K1 due %v, want 24h after birth %v", k1.dueAt, bornAt)
	}
	if k2.dueAt != nil {
		t.Fatalf("K2 due %v before the litter reached K1, want none", k2.dueAt)
	}
	if k1.owner != "park_head" || k1.target != "K1" || k2.target != "K2" {
		t.Fatalf("owner=%q targets=%q/%q, want park_head and K1/K2", k1.owner, k1.target, k2.target)
	}

	// Owed (past its 24-hour deadline, kids still on K0): on the park head's board, never on an
	// operator's own lens.
	if !boardShowsLitter(t, ctx, pool, false) {
		t.Fatal("an owed litter is not on the park head's Work Board")
	}
	if boardShowsLitter(t, ctx, pool, true) {
		t.Fatal("the litter's task is on an operator's own lens")
	}

	// A by-hand completion is refused: the step is closed only by the herd register.
	detail, err := svc.GetWorkflowBySubject(ctx, countsTenant, tasksdomain.TemplateKeyBirthLitter, kidShiftBirth)
	if err != nil {
		t.Fatalf("read the litter workflow: %v", err)
	}
	for _, a := range detail.Actions {
		if a.ActionKey != "shift_to_k1" {
			continue
		}
		_, err := svc.CompleteAction(ctx, tasksapp.CompleteActionInput{TenantID: countsTenant, WorkflowID: a.WorkflowID, ActionID: a.ActionID, IdempotencyKey: "tap-k1", RequestFingerprint: "tap-k1", CompletedBy: kidShiftMother,
			// A PERSON tapping (the phone and the web always send the caller's roles; nil roles is the
			// engine itself). The CEO floor is used so the refusal is the engine-step rule, not ownership.
			ActorRoles: []string{"ceo_internal"}})
		if !errors.Is(err, tasksdomain.ErrKidShiftPending) {
			t.Fatalf("a hand tap on the K1 shift step returned %v, want kid_shift_pending", err)
		}
	}

	// One twin moves: the other is still on K0, so the task stays owed.
	shiftKidsToK1(t, ctx, pool, mux, repo, "e2e-kidshift-a", []string{kidShiftKidA}, "K1")
	relayOutbox(t, ctx, pool, bus)
	if _, steps = litterSteps(t, ctx, pool); steps["shift_to_k1"].status != "pending" {
		t.Fatalf("K1 step %s with a twin still on K0, want pending", steps["shift_to_k1"].status)
	}

	// The second twin moves: the step completes at that apply and K2 is due seven days on.
	shiftKidsToK1(t, ctx, pool, mux, repo, "e2e-kidshift-b", []string{kidShiftKidB}, "")
	relayOutbox(t, ctx, pool, bus)
	_, steps = litterSteps(t, ctx, pool)
	k1, k2 = steps["shift_to_k1"], steps["shift_to_k2"]
	if k1.status != "completed" || k1.completed == nil {
		t.Fatalf("K1 step %s after both twins reached K1, want completed", k1.status)
	}
	var appliedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT max(occurred_at) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.stage_changed'`, countsTenant, kidShiftKidB).Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if !k1.completed.Equal(appliedAt) {
		t.Fatalf("K1 completed at %v, want the second twin's move %v", k1.completed, appliedAt)
	}
	if k2.dueAt == nil || !k2.dueAt.Equal(appliedAt.Add(7*24*time.Hour)) {
		t.Fatalf("K2 due %v, want 7 days after %v", k2.dueAt, appliedAt)
	}
	// Done today after being owed: still on today's board, so the park head sees it closed.
	if !boardShowsLitter(t, ctx, pool, false) {
		t.Fatal("the litter done late today is not on today's board")
	}
}

// Kids shifted BEFORE the 24-hour deadline: the step completes, the K2 step is not yet due, and the
// park head never sees a task ("if kid already shifted to k2/k1 before deadline in that case don't
// create task").
func TestKidShiftTaskNeverSurfacesWhenTheKidsMovedBeforeTheDeadline(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)
	_, bus := kidShiftStack(pool)

	bornAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	seedLitter(t, ctx, pool, "K0", bornAt)
	payload, _ := json.Marshal(map[string]string{"goat_id": kidShiftKidA, "origin_type": "birth", "dam_id": kidShiftMother})
	if err := bus.Publish(ctx, eventbus.Event{Type: tasksapp.EventGoatCreated, TenantID: countsTenant, Key: kidShiftKidA, OccurredAt: bornAt, Payload: payload}); err != nil {
		t.Fatalf("goat.created: %v", err)
	}
	if boardShowsLitter(t, ctx, pool, false) {
		t.Fatal("the task is on the board before its deadline")
	}

	shiftKidsToK1(t, ctx, pool, mux, repo, "e2e-kidshift-early", []string{kidShiftKidA, kidShiftKidB}, "K1")
	relayOutbox(t, ctx, pool, bus)

	_, steps := litterSteps(t, ctx, pool)
	if steps["shift_to_k1"].status != "completed" {
		t.Fatalf("K1 step %s after an early move, want completed", steps["shift_to_k1"].status)
	}
	if boardShowsLitter(t, ctx, pool, false) {
		t.Fatal("a litter moved before its deadline surfaced a task")
	}
}

// A litter ALREADY on the farm when the rule shipped ("this should already reflect for animals
// which are present also"): twins that reached K1 four days ago get their K1 step completed on the
// day they got there, and their K2 task due three days from now -- not seven.
func TestKidShiftBackfillCountsFromWhenTheLitterReallyReachedK1(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	_, _ = typedE2EStack(t, pool) // the herd fixtures (custodian, park, pens) every counts E2E stands on
	svc, _ := kidShiftStack(pool)

	seedLitter(t, ctx, pool, "K1", time.Now().UTC().Add(-5*24*time.Hour))
	reachedK1 := time.Now().UTC().Add(-4 * 24 * time.Hour).Truncate(time.Second)
	for i, kid := range []string{kidShiftKidA, kidShiftKidB} {
		at := reachedK1.Add(-time.Duration(i) * time.Hour) // the LATER of the two is kid A
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_events (identity_event_id, tenant_id, goat_id, event_type, event_version, occurred_at, recorded_at, payload, idempotency_key)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'goat.stage_changed', 1, $3, $3, jsonb_build_object('goat_id', $2::text, 'management_stage', 'K1', 'previous_management_stage', 'K0'), 'kidshift-history:' || $2::text)`,
			countsTenant, kid, at); err != nil {
			t.Fatalf("seed stage history: %v", err)
		}
	}

	litters, err := taskspg.NewRepository(pool, 10*time.Second).LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, nil, "", 50)
	if err != nil || len(litters) != 1 || litters[0].BirthEventID != kidShiftBirth {
		t.Fatalf("backfill candidates=%v err=%v, want the one litter", litters, err)
	}
	if err := svc.OpenLitterWorkflowForKid(ctx, countsTenant, litters[0].KidGoatID); err != nil {
		t.Fatalf("backfill open: %v", err)
	}
	// Re-running finds nothing: the litter has its workflow.
	if again, _ := taskspg.NewRepository(pool, 10*time.Second).LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, nil, "", 50); len(again) != 0 {
		t.Fatalf("second backfill pass found %d candidates, want 0", len(again))
	}

	_, steps := litterSteps(t, ctx, pool)
	k1, k2 := steps["shift_to_k1"], steps["shift_to_k2"]
	if k1.status != "completed" || k1.completed == nil || !k1.completed.Equal(reachedK1) {
		t.Fatalf("K1 step %s at %v, want completed at %v", k1.status, k1.completed, reachedK1)
	}
	if k2.status != "pending" || k2.dueAt == nil || !k2.dueAt.Equal(reachedK1.Add(7*24*time.Hour)) {
		t.Fatalf("K2 step %s due %v, want pending due %v", k2.status, k2.dueAt, reachedK1.Add(7*24*time.Hour))
	}
	// No kid or mother track was opened for this old birth.
	var others int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_instances WHERE tenant_id = $1::uuid AND template_key IN ('birth_kid', 'birth_mother')`, countsTenant).Scan(&others); err != nil || others != 0 {
		t.Fatalf("backfill opened %d kid/mother workflows (err %v), want 0", others, err)
	}
}

// Farm-born FEMALES to Non-Pregnant 10 weeks from the day of birth, whatever stage they are on
// (maintainer instruction 2026-10-01). A litter already on the farm -- a K3 female and her K3
// brother, born 71 days ago -- is picked up by the backfill for the female alone; the K1/K2 steps
// read as done, the Non-Pregnant step is owed and on the park head's board; a REAL growth shifting
// moves the K3 female straight into a Non-Pregnant pen (the age-entry rule, from the stage's own
// "From (days)"), and the step closes itself while the brother is never asked about.
func TestKidShiftFemaleToNonPregnantAtTenWeeksFromAnyStage(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)
	svc, bus := kidShiftStack(pool)

	bornAt := time.Now().UTC().Add(-71 * 24 * time.Hour).Truncate(time.Minute)
	seedLitter(t, ctx, pool, "K3", bornAt)
	for _, s := range []string{"K3", "Non-Pregnant"} {
		seedStageVocabulary(t, ctx, pool, s)
	}
	// The farm's own settings the rule reads: Non-Pregnant's "From (days)" (migration 000463 sets
	// 70 where a farm has none; the fixture's vocabulary is seeded after migrations), the brother's
	// sex, and a Non-Pregnant pen to move her into.
	if _, err := pool.Exec(ctx, `UPDATE animal_stage_lookup SET min_age_days = 70 WHERE tenant_id = $1::uuid AND stage_code = 'Non-Pregnant'`, countsTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET sex = 'male' WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, countsTenant, kidShiftKidB); err != nil {
		t.Fatal(err)
	}
	seedShedProfile(t, ctx, pool, countsShedB, "Non-Pregnant")

	// The backfill: K0/K1 kids (none here) or a FEMALE before Non-Pregnant (kid A).
	tasks := taskspg.NewRepository(pool, 10*time.Second)
	if none, _ := tasks.LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, nil, "", 50); len(none) != 0 {
		t.Fatalf("a K3 litter is a K0/K1 candidate: %v", none)
	}
	litters, err := tasks.LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, domain.GrowthStagesBefore("Non-Pregnant"), "", 50)
	if err != nil || len(litters) != 1 || litters[0].BirthEventID != kidShiftBirth {
		t.Fatalf("female backfill candidates=%v err=%v, want the one litter", litters, err)
	}
	if err := svc.OpenLitterWorkflowForKid(ctx, countsTenant, litters[0].KidGoatID); err != nil {
		t.Fatalf("backfill open: %v", err)
	}
	_, steps := litterSteps(t, ctx, pool)
	np := steps["shift_to_non_pregnant"]
	if steps["shift_to_k1"].status != "completed" || steps["shift_to_k2"].status != "completed" {
		t.Fatalf("K1/K2 steps %s/%s on a K3 litter, want completed", steps["shift_to_k1"].status, steps["shift_to_k2"].status)
	}
	if np.status != "pending" || np.owner != "park_head" || np.target != "Non-Pregnant" {
		t.Fatalf("Non-Pregnant step %+v, want the park head's pending move", np)
	}
	if np.dueAt == nil || np.dueAt.Sub(bornAt).Round(time.Minute) != 70*24*time.Hour {
		t.Fatalf("Non-Pregnant due %v, want 70 days after birth %v", np.dueAt, bornAt)
	}
	if !boardShowsLitter(t, ctx, pool, false) {
		t.Fatal("an owed Non-Pregnant move is not on the park head's Work Board")
	}

	// The step serves the female alone for its raise.
	detail, err := svc.GetWorkflowBySubject(ctx, countsTenant, tasksdomain.TemplateKeyBirthLitter, kidShiftBirth)
	if err != nil {
		t.Fatal(err)
	}
	groups := tasksdomain.ShiftGroups(detail.LitterKids, "Non-Pregnant", "female")
	if len(groups) != 1 || len(groups[0].Kids) != 1 || groups[0].Kids[0].GoatID != kidShiftKidA {
		t.Fatalf("raise groups %+v, want kid A alone", groups)
	}

	// A real growth shifting moves the K3 female straight to Non-Pregnant.
	res := raiseTypedShifting(t, mux, "e2e-kidshift-np", domain.ShiftTypeGrowth, []string{kidShiftKidA})
	if res.Code != http.StatusOK {
		t.Fatalf("raise status=%d body=%s", res.Code, res.Body.String())
	}
	var raised struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raised); err != nil || raised.ShiftingEventID == "" {
		t.Fatalf("decode raise: %v", err)
	}
	if category, target, _ := storedShiftingSnapshot(t, ctx, pool, raised.ShiftingEventID); category != domain.ShiftTypeGrowth || target != "Non-Pregnant" {
		t.Fatalf("stored %q/%q, want growth/Non-Pregnant", category, target)
	}
	if _, _, err := approveShifting(repo, ctx, "e2e-kidshift-np", pendingApprovalForShifting(t, ctx, pool, raised.ShiftingEventID), raised.ShiftingEventID, []string{kidShiftKidA}); err != nil {
		t.Fatalf("approval: %v", err)
	}
	done, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID: countsTenant, ShiftingEventID: raised.ShiftingEventID, CompletedByUserID: countsOperator,
		CompletedAt: time.Now().In(biztime.DefaultLocation()), ProofRef: "proof-artifact-np",
		IdempotencyKey: "complete-np", RequestFingerprint: "complete-fp-np", TraceID: "trace-np",
	})
	if err != nil || done.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("completion: status=%q err=%v", done.EventStatus, err)
	}
	if got := goatStage(t, ctx, pool, kidShiftKidA); got != "Non-Pregnant" {
		t.Fatalf("kid A stage %q after the apply, want Non-Pregnant", got)
	}
	relayOutbox(t, ctx, pool, bus)

	state, steps := litterSteps(t, ctx, pool)
	if steps["shift_to_non_pregnant"].status != "completed" {
		t.Fatalf("Non-Pregnant step %s with the female on Non-Pregnant and her brother on K3, want completed", steps["shift_to_non_pregnant"].status)
	}
	t.Logf("litter workflow state after the last step: %s", state)
}
