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
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
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

// A litter born on the farm: the task opens with K1 due exactly 24 hours after birth and no owner;
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
	if k1.owner != "" || k1.target != "K1" || k2.target != "K2" {
		t.Fatalf("owner=%q targets=%q/%q, want no owner and K1/K2", k1.owner, k1.target, k2.target)
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

	litters, err := taskspg.NewRepository(pool, 10*time.Second).LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, "", 50)
	if err != nil || len(litters) != 1 || litters[0].BirthEventID != kidShiftBirth {
		t.Fatalf("backfill candidates=%v err=%v, want the one litter", litters, err)
	}
	if err := svc.OpenLitterWorkflowForKid(ctx, countsTenant, litters[0].KidGoatID); err != nil {
		t.Fatalf("backfill open: %v", err)
	}
	// Re-running finds nothing: the litter has its workflow.
	if again, _ := taskspg.NewRepository(pool, 10*time.Second).LittersOwingShift(ctx, countsTenant, []string{"K0", "K1"}, "", 50); len(again) != 0 {
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
