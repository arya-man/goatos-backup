package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Counts shifting APPROVAL + EXECUTION -- proofs against the REAL identity Postgres adapter.
//
// The sibling tests in approval_integration_test.go drive a fakeIdentityTx that only UPDATEs
// goats.shed_id. That fake is blind by construction to everything a real relocation must also do --
// write the canonical goat_identity_events row, enqueue the goat.location.changed outbox message
// keyed to it, and record location history -- so it happily passed while the production path
// returned 500 on every shifting approval:
//
//	identity: relocate goats: ERROR: outbox event <uuid> does not exist for tenant <uuid>
//	(SQLSTATE 23503)
//
// The cause was that RelocateGoatsToShedInTx minted an event_id, inserted it into outbox_messages,
// and never inserted the matching goat_identity_events row that
// outbox_messages_validate_event_tenant_trg requires. These tests wire counts' repository to
// identity's actual repository -- exactly as internal/bootstrap/api.go does -- so the schema's
// triggers and constraints are part of the assertion surface.
//
// WHAT CHANGED (maintainer decision, 2026-07-19). Approving a shifting used to relocate the
// animals. It no longer does: approval is AUTHORIZATION, and the relocation moved to COMPLETION,
// when an operator confirms the animals physically walked. The relocation coverage below was not
// deleted -- it was RELOCATED onto the completion path, which is now the only path in the shifting
// flow that writes an animal's canonical location. The 500 above would surface there today, so the
// regression stays covered.

func newRealIdentityApprovalRepo(t *testing.T, pool *pgxpool.Pool) *Repository {
	t.Helper()
	seedCustodianParty(t, context.Background(), pool)
	return NewRepository(pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(pool, 10*time.Second))
}

// submitShiftingApproval records a pending shifting event and the approval request that governs it,
// returning both ids.
func submitShiftingApproval(
	t *testing.T, ctx context.Context, repo *Repository, key string, goatIDs []string,
) (shiftingEventID, approvalRequestID string) {
	t.Helper()
	shiftingEventID, _, err := repo.RecordShiftingEvent(ctx, shiftingEventForApproval(key))
	if err != nil {
		t.Fatalf("record shifting event: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   shiftingEventID,
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal shifting payload: %v", err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &shiftingEventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "submit-" + key,
		RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("submit shifting approval: %v", err)
	}
	return shiftingEventID, req.ApprovalRequestID
}

func approveShifting(
	repo *Repository, ctx context.Context, key, approvalRequestID, shiftingEventID string, goatIDs []string,
) (domain.ApprovalRequest, bool, error) {
	return repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  approvalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-" + key,
		RequestFingerprint: "decide-fp-" + key,
		Effect: &domain.ApprovalEffect{Shifting: &domain.ShiftingApprovalEffect{
			ShiftingEventID:   shiftingEventID,
			DestinationParkID: countsPark,
			DestinationShedID: countsShedB,
			GoatIDs:           goatIDs,
		}},
	})
}

// completeShifting drives the production completion path.
func completeShifting(
	repo *Repository, ctx context.Context, key, shiftingEventID string,
) (domain.ShiftingExecutionResult, bool, error) {
	return completeShiftingWithTag(repo, ctx, key, shiftingEventID, "")
}

// completeShiftingWithTag drives completion carrying an explicit destination cohort tag, as an
// operator does when shifting animals into an empty shed.
func completeShiftingWithTag(
	repo *Repository, ctx context.Context, key, shiftingEventID, destinationTag string,
) (domain.ShiftingExecutionResult, bool, error) {
	return repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		DestinationTag:     destinationTag,
		IdempotencyKey:     "complete-" + key,
		RequestFingerprint: "complete-fp-" + key + ":" + destinationTag,
	})
}

// seedApprovalGoatWithStage seeds a goat carrying an explicit management_stage (operational cohort),
// so a shift can prove reclassification into the destination shed's cohort.
func seedApprovalGoatWithStage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, stage string) {
	t.Helper()
	seedApprovalGoat(t, ctx, pool, goatID, shedID)
	if _, err := pool.Exec(ctx, `
UPDATE goats SET management_stage = $3 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, stage); err != nil {
		t.Fatalf("seed goat stage: %v", err)
	}
}

// goatStage reads a goat's current management_stage — the operational cohort feed and counts key on.
func goatStage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var stage string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(management_stage, '') FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID).Scan(&stage); err != nil {
		t.Fatalf("read goat stage: %v", err)
	}
	return stage
}

// seedShedProfile configures the destination shed's AUTHORITATIVE operational profile: the active
// shed_profiles row whose animal_stage_id resolves (through animal_stage_lookup) to stageCode. The
// shifting-completion path (identity.resolveDestinationTag) reads the destination cohort from THIS
// configuration, never from whichever goats happen to be standing in the shed, so every completion
// test seeds it for the DESTINATION shed. The animal_stage_lookup vocabulary row is upserted first
// (the counts harness does not seed the stage vocabulary), then its id is written onto the
// shed_profiles row (PK location_id). A destination shed with no such row FAILS CLOSED with
// ErrDestinationProfileMissing.
func seedShedProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, stageCode string) {
	t.Helper()
	var stageID string
	if err := pool.QueryRow(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, $2, $2, 1, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE SET name = EXCLUDED.name, status = 'active'
RETURNING animal_stage_id::text`, countsTenant, stageCode).Scan(&stageID); err != nil {
		t.Fatalf("seed animal stage %q: %v", stageCode, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, row_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1)
ON CONFLICT (location_id) DO UPDATE SET
  animal_stage_id = EXCLUDED.animal_stage_id,
  row_version     = shed_profiles.row_version + 1,
  updated_at      = now()`, shedID, countsTenant, stageID); err != nil {
		t.Fatalf("seed shed profile for %s (%s): %v", shedID, stageCode, err)
	}
}

func shiftingEventStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shiftingEventID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT event_status FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		shiftingEventID).Scan(&status); err != nil {
		t.Fatalf("read shifting event status: %v", err)
	}
	return status
}

// ---------------------------------------------------------------------------
// Approve authorizes and moves NOTHING
// ---------------------------------------------------------------------------

// TestApproveShiftingThroughRealIdentityRepositoryDoesNotMoveAnimals pins the 2026-07-19 flow
// change against the REAL identity adapter: approval is a permission slip, so after it the animals
// are still in the source shed and none of the relocation's downstream artefacts exist.
//
// This is the assertion that used to say the opposite. It is deliberately kept on the real-adapter
// harness rather than the fake one, because "nothing happened" is only a meaningful claim when the
// thing that would have happened is fully wired.
func TestApproveShiftingThroughRealIdentityRepositoryDoesNotMoveAnimals(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000c001"
	goatB := "00000000-0000-4000-8000-00000000c002"
	goatIDs := []string{goatA, goatB}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedApprovalGoat(t, ctx, pool, goatB, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "real-shift-1", goatIDs)

	decided, replayed, err := approveShifting(repo, ctx, "real-shift-1", approvalRequestID, shiftingEventID, goatIDs)
	if err != nil {
		t.Fatalf("approve shifting through the real identity repository: %v", err)
	}
	if replayed {
		t.Fatalf("first approval reported replayed=true, want a fresh decision")
	}
	if decided.Status != domain.ApprovalStatusApproved {
		t.Fatalf("status=%q, want approved", decided.Status)
	}

	// THE RULE: authorization is not relocation.
	for _, goatID := range goatIDs {
		if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
			t.Fatalf("goat %s shed=%s after approval, want it STILL at the source shed %s -- "+
				"approving a shifting authorizes the movement; the animals move at completion",
				goatID, got, countsShedA)
		}
	}
	// None of the relocation's artefacts may exist yet. An approval that quietly emitted
	// goat.location.changed would re-scope these animals' vaccination obligations to a shed they
	// had not reached.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("identity location events after approval=%d, want 0", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("outbox location messages after approval=%d, want 0", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_location_history WHERE tenant_id = $1::uuid`, countsTenant); got != 0 {
		t.Fatalf("location history rows after approval=%d, want 0", got)
	}

	// The paperwork did advance, which is what puts the movement on the execution queue.
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after approval, want %q", got, domain.ShiftingEventStatusAuthorized)
	}
	var appliedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		shiftingEventID).Scan(&appliedAt); err != nil {
		t.Fatalf("read applied_at: %v", err)
	}
	if appliedAt != nil {
		t.Fatalf("applied_at=%v after approval, want NULL -- nothing has been executed yet", appliedAt)
	}
}

// ---------------------------------------------------------------------------
// Complete DOES move -- the relocated coverage
// ---------------------------------------------------------------------------

// TestCompleteShiftingThroughRealIdentityRepositoryMovesAnimalsAndEmitsEvents is the direct
// regression for the shipped 500, carried over from the approval path. It asserts the whole
// production effect: completion succeeds, the animals actually move, the status flips to 'applied'
// with its stamp, and BOTH halves of the event pair land -- the canonical identity event and the
// outbox message that reuses its id.
func TestCompleteShiftingThroughRealIdentityRepositoryMovesAnimalsAndEmitsEvents(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000c001"
	goatB := "00000000-0000-4000-8000-00000000c002"
	goatIDs := []string{goatA, goatB}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedApprovalGoat(t, ctx, pool, goatB, countsShedA)
	// The destination shed has a configured operational profile, so the completion reads the cohort
	// from it (this test is about the location relocation, not reclassification).
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "real-shift-1", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "real-shift-1", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// Premise check: still unmoved after approval, so anything below is caused by the completion.
	for _, goatID := range goatIDs {
		if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
			t.Fatalf("goat %s shed=%s before completion, want source shed %s", goatID, got, countsShedA)
		}
	}

	result, replayed, err := completeShifting(repo, ctx, "real-shift-1", shiftingEventID)
	if err != nil {
		t.Fatalf("complete shifting through the real identity repository: %v\n"+
			"this is the shipped 500's home now: the relocation enqueues a goat outbox message whose "+
			"event_id must have a matching goat_identity_events row, or "+
			"outbox_messages_validate_event_tenant_trg rejects it", err)
	}
	if replayed {
		t.Fatalf("first completion reported replayed=true, want a fresh execution")
	}
	if result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q, want %q", result.EventStatus, domain.ShiftingEventStatusApplied)
	}
	if len(result.MovedGoatIDs) != len(goatIDs) {
		t.Fatalf("moved %d animals, want %d", len(result.MovedGoatIDs), len(goatIDs))
	}

	// The row carries the completion stamp, not just a status flip.
	var (
		eventStatus string
		appliedAt   *time.Time
		appliedBy   *string
	)
	if err := pool.QueryRow(ctx, `
SELECT event_status, applied_at, applied_by::text FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		shiftingEventID).Scan(&eventStatus, &appliedAt, &appliedBy); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if eventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q, want applied", eventStatus)
	}
	if appliedAt == nil || appliedBy == nil || *appliedBy != countsOperator {
		t.Fatalf("applied stamp=(%v,%v), want the completing operator %s -- "+
			"shifting_events_applied_shape_check should have made this unrepresentable",
			appliedAt, appliedBy, countsOperator)
	}

	for _, goatID := range goatIDs {
		if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
			t.Fatalf("goat %s shed=%s after completion, want destination shed %s -- completing a "+
				"shifting must MOVE the animal", goatID, got, countsShedB)
		}

		// The canonical identity timeline row. Its absence is what the outbox trigger detected.
		var identityEventID string
		if err := pool.QueryRow(ctx, `
SELECT identity_event_id::text
FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.location.changed'`,
			countsTenant, goatID).Scan(&identityEventID); err != nil {
			t.Fatalf("goat %s: expected exactly one goat.location.changed identity event: %v", goatID, err)
		}

		// The outbox message must exist AND reuse the identity event's id -- the id threading that
		// finishGoatLifecycleMutation does and that the relocate path had dropped.
		var outboxEventID, scopeID, toShed string
		if err := pool.QueryRow(ctx, `
SELECT event_id::text,
       payload->'payload'->>'scope_id',
       payload->'payload'->>'to_shed_id'
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'goat.location.changed'`,
			countsTenant, goatID).Scan(&outboxEventID, &scopeID, &toShed); err != nil {
			t.Fatalf("goat %s: expected exactly one goat.location.changed outbox message: %v", goatID, err)
		}
		if outboxEventID != identityEventID {
			t.Fatalf("goat %s: outbox event_id=%s but identity event id=%s -- the outbox row must be "+
				"backed by the identity event it derives from", goatID, outboxEventID, identityEventID)
		}
		// The obligation re-scope handler reads these; naming the old shed would strand the animal's
		// open shed-scoped vaccination obligations.
		if scopeID != countsShedB || toShed != countsShedB {
			t.Fatalf("goat %s: outbox payload scope_id=%s to_shed_id=%s, want destination shed %s",
				goatID, scopeID, toShed, countsShedB)
		}

		if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_location_history
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND to_location_id = $3::uuid`,
			countsTenant, goatID, countsShedB); got != 1 {
			t.Fatalf("goat %s: location history rows=%d, want 1", goatID, got)
		}
	}
}

// TestCompleteShiftingIsIdempotentOnReplay proves a retried completion -- the phone-on-a-flaky-link
// case -- returns the original result and relocates nobody a second time.
func TestCompleteShiftingIsIdempotentOnReplay(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000c021"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	// Configured destination so the completion resolves its cohort; this test is about idempotency.
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "real-shift-idem", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "real-shift-idem", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, _, err := completeShifting(repo, ctx, "real-shift-idem", shiftingEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}

	// Exact replay: same key, same fingerprint.
	replayResult, replayed, err := completeShifting(repo, ctx, "real-shift-idem", shiftingEventID)
	if err != nil {
		t.Fatalf("exact replay of the completion: %v", err)
	}
	if !replayed {
		t.Fatalf("replay reported replayed=false, want the original result returned")
	}
	if replayResult.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("replay event_status=%q, want applied", replayResult.EventStatus)
	}

	// A DIFFERENT operator with a DIFFERENT key completing the same movement must also not move the
	// herd onward: completion confirms a physical fact, it is not a command that may run twice.
	if _, replayed, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsApprover,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "complete-someone-else",
		RequestFingerprint: "complete-fp-someone-else",
	}); err != nil {
		t.Fatalf("second operator completing the same movement: %v", err)
	} else if !replayed {
		t.Fatalf("second operator's completion reported replayed=false, want the original result")
	}

	// One move, one event, one outbox message, one history row -- no matter how many times it was
	// confirmed.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`, countsTenant); got != 1 {
		t.Fatalf("identity events after replays=%d, want 1 -- replay must not re-apply", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`, countsTenant); got != 1 {
		t.Fatalf("outbox messages after replays=%d, want 1 -- replay must not re-enqueue", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_location_history WHERE tenant_id = $1::uuid`, countsTenant); got != 1 {
		t.Fatalf("location history rows after replays=%d, want 1", got)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedB {
		t.Fatalf("goat shed=%s after replays, want it at the destination shed %s once", got, countsShedB)
	}
}

// TestCompleteShiftingRollsBackOnUnmovableAnimal proves the relocation and the status flip remain
// ONE transaction against the real adapter: a named animal that cannot move aborts the whole
// completion, leaving no partial move, no orphaned event rows, and the movement still authorized so
// a human can resolve the animal and retry.
//
// This is the fail-closed shortfall behaviour carried over from the approval path.
func TestCompleteShiftingRollsBackOnUnmovableAnimal(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	movable := "00000000-0000-4000-8000-00000000c011"
	exited := "00000000-0000-4000-8000-00000000c012"
	goatIDs := []string{movable, exited}
	seedApprovalGoat(t, ctx, pool, movable, countsShedA)
	seedApprovalGoat(t, ctx, pool, exited, countsShedA)
	// Configured destination so cohort resolution succeeds and the completion fails on the unmovable
	// animal (the behaviour under test), not on a missing destination profile.
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "real-shift-2", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "real-shift-2", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// The animal dies AFTER the movement was authorized -- exactly the real-world case this guard
	// exists for. An exited animal is not movable, so the relocation reports a shortfall.
	if _, err := pool.Exec(ctx, `
UPDATE goats SET lifecycle_status = 'dead', exit_reason = 'died', exited_at = now()
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, countsTenant, exited); err != nil {
		t.Fatalf("exit goat: %v", err)
	}

	_, _, err := completeShifting(repo, ctx, "real-shift-2", shiftingEventID)
	if err == nil {
		t.Fatalf("completion succeeded, want a fail-closed shortfall error")
	}
	if !errors.Is(err, ports.ErrShiftingExecutionIncomplete) {
		t.Fatalf("err=%v, want ErrShiftingExecutionIncomplete", err)
	}

	// Nothing may survive the rollback: not the movable animal's relocation, not its event rows,
	// not the status flip.
	if got := goatShed(t, ctx, pool, movable); got != countsShedA {
		t.Fatalf("movable goat shed=%s after failed completion, want it still at source shed %s",
			got, countsShedA)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("identity events after rolled-back completion=%d, want 0", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("outbox messages after rolled-back completion=%d, want 0", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after failed completion, want it still %q so a human can retry",
			got, domain.ShiftingEventStatusAuthorized)
	}
	// The completion stamp must not have been left behind either.
	var appliedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		shiftingEventID).Scan(&appliedAt); err != nil {
		t.Fatalf("read applied_at: %v", err)
	}
	if appliedAt != nil {
		t.Fatalf("applied_at=%v after rolled-back completion, want NULL", appliedAt)
	}
}

// TestCompleteShiftingFailsClosedWhenSourcePlacementIsStale is the CR-01 regression. The shifting
// event captured its source placement (shed A) at approval time and passes it to the relocation as
// FromShedID/FromParkID -- but that expectation was never ENFORCED, so a completion would overwrite
// a NEWER legitimate relocation. Concretely: an A->B movement is approved, the animal is then
// legitimately relocated A->C, and completing the stale A->B movement would move the animal C->B,
// silently clobbering the newer placement. The source-placement guard must fail closed under the
// relocation row lock, leaving the animal at C and the movement authorized for reconciliation.
func TestCompleteShiftingFailsClosedWhenSourcePlacementIsStale(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000c031"
	goatIDs := []string{goatA}
	// Starts in shed A -- the source the approval will capture.
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	// Configured destination so cohort resolution succeeds and the completion fails on the stale
	// source placement (the behaviour under test), not on a missing destination profile.
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "stale-src-1", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "stale-src-1", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// A NEWER legitimate relocation moves the animal A->C (same park), out of band, after approval.
	// current_location_id/shed_id now point at C, which disagrees with the approved source (A).
	if _, err := pool.Exec(ctx, `
UPDATE goats SET shed_id = $3::uuid, current_location_id = $3::uuid, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, countsTenant, goatA, countsShedC); err != nil {
		t.Fatalf("out-of-band relocate A->C: %v", err)
	}

	// Completing the stale A->B movement must FAIL CLOSED on the source-placement mismatch rather
	// than moving the animal C->B.
	_, _, err := completeShifting(repo, ctx, "stale-src-1", shiftingEventID)
	if err == nil {
		t.Fatalf("completion succeeded, want a fail-closed stale-source error: a stale A->B move must not overwrite the newer C placement")
	}
	if !errors.Is(err, identityports.ErrWriteConflict) {
		t.Fatalf("err=%v, want ErrWriteConflict (stale source placement)", err)
	}

	// The newer C placement is PRESERVED -- the whole point. Overwriting it to B is the data loss
	// this guard prevents.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedC {
		t.Fatalf("goat shed=%s after failed stale completion, want it preserved at the newer shed %s", got, countsShedC)
	}
	// No relocation artefacts and no completion stamp survived the rollback; the movement stays
	// authorized so a human can reconcile it.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("identity events after rolled-back stale completion=%d, want 0", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("outbox messages after rolled-back stale completion=%d, want 0", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after failed stale completion, want it still %q for reconciliation",
			got, domain.ShiftingEventStatusAuthorized)
	}
}

// ---------------------------------------------------------------------------
// Shifting RECLASSIFIES the animal into the destination shed's cohort
// ---------------------------------------------------------------------------

// TestCompleteShiftingAdoptsOccupiedDestinationCohort is the core bug fix. A K1 goat shifted into a
// shed CONFIGURED as K2 must JOIN that shed's cohort: its management_stage becomes K2 in the same
// transaction as the shed move (never stale K1 in a K2 shed), and it emits goat.stage_changed so feed
// and the vaccination generator reclassify it. Before the fix the relocation wrote only shed_id, so
// the goat sat in the K2 shed but was still classified — and fed — as K1. The destination cohort is
// read from the shed's configured profile, not inferred from the resident it joins.
func TestCompleteShiftingAdoptsOccupiedDestinationCohort(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c101"
	resident := "00000000-0000-4000-8000-00000000c102"
	// The mover starts in shed A tagged K1; shed B (the destination) is CONFIGURED as K2 and already
	// holds a K2 resident, so the destination cohort K2 comes from the profile, not the resident.
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
	seedApprovalGoatWithStage(t, ctx, pool, resident, countsShedB, "K2")
	seedShedProfile(t, ctx, pool, countsShedB, "K2")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "reclass-1", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "reclass-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	if _, _, err := completeShifting(repo, ctx, "reclass-1", shiftingEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}

	// Shed AND tag moved together.
	if got := goatShed(t, ctx, pool, mover); got != countsShedB {
		t.Fatalf("mover shed=%s after completion, want destination shed %s", got, countsShedB)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K2" {
		t.Fatalf("mover management_stage=%q after completion, want the destination cohort K2 -- "+
			"a goat shifted into a K2 shed must be classified and fed as K2, not left stale as K1", got)
	}
	// The resident is untouched — the shed stays homogeneous at K2.
	if got := goatStage(t, ctx, pool, resident); got != "K2" {
		t.Fatalf("resident management_stage=%q, want it unchanged at K2", got)
	}

	// The reclassification event pair landed for the mover: canonical identity event + matching outbox.
	var identityEventID string
	if err := pool.QueryRow(ctx, `
SELECT identity_event_id::text FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, mover).Scan(&identityEventID); err != nil {
		t.Fatalf("mover: expected exactly one goat.stage_changed identity event: %v", err)
	}
	var outboxEventID, prevStage, newStage string
	if err := pool.QueryRow(ctx, `
SELECT event_id::text,
       payload->'payload'->>'previous_management_stage',
       payload->'payload'->>'management_stage'
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, mover).Scan(&outboxEventID, &prevStage, &newStage); err != nil {
		t.Fatalf("mover: expected exactly one goat.stage_changed outbox message: %v", err)
	}
	if outboxEventID != identityEventID {
		t.Fatalf("stage outbox event_id=%s but identity event id=%s -- the outbox row must reuse the "+
			"identity event it derives from", outboxEventID, identityEventID)
	}
	if prevStage != "K1" || newStage != "K2" {
		t.Fatalf("stage event payload previous=%q new=%q, want K1->K2", prevStage, newStage)
	}

	// The resident (unmoved, unchanged) gets NO stage event: reclassification is only for animals
	// whose tag actually changes.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, resident); got != 0 {
		t.Fatalf("resident stage events=%d, want 0 -- an unchanged animal is not reclassified", got)
	}

	// GRAIN PROOF: the counts/feed read path keys on goats.management_stage
	// (feed_projected_counts.go and the counts breakdown both COALESCE it), so the mover now groups
	// under K2. Assert it is counted under the new cohort and no longer under K1.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goats
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND management_stage = 'K2'
  AND merged_into_goat_id IS NULL AND exited_at IS NULL`, countsTenant, countsShedB); got != 2 {
		t.Fatalf("live K2 animals in destination shed=%d, want 2 (resident + reclassified mover)", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goats
WHERE tenant_id = $1::uuid AND management_stage = 'K1'
  AND merged_into_goat_id IS NULL AND exited_at IS NULL`, countsTenant); got != 0 {
		t.Fatalf("live K1 animals after the shift=%d, want 0 -- the mover must no longer be fed as K1", got)
	}
}

func TestCompleteShiftingFailsClosedWhenDestinationProfileDriftsAfterAuthorization(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c103"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
	seedShedProfile(t, ctx, pool, countsShedB, "K2")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "profile-drift-1", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "profile-drift-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	seedShedProfile(t, ctx, pool, countsShedB, "K3")
	_, _, err := completeShifting(repo, ctx, "profile-drift-1", shiftingEventID)
	if !errors.Is(err, identityports.ErrDestinationTagConflict) {
		t.Fatalf("complete err=%v, want ErrDestinationTagConflict after destination profile drift", err)
	}
	if got := goatShed(t, ctx, pool, mover); got != countsShedA {
		t.Fatalf("mover shed=%s after rejected completion, want source shed %s", got, countsShedA)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K1" {
		t.Fatalf("mover management_stage=%q after rejected completion, want unchanged K1", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after rejected completion, want authorized for retry/review", got)
	}
}

func TestCompleteShiftingFailsClosedWhenAuthorizedEventLacksDestinationSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c104"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
	seedShedProfile(t, ctx, pool, countsShedB, "K2")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "missing-snapshot-1", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "missing-snapshot-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// Simulate an already-authorized row from before the destination profile snapshot columns shipped.
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events
SET destination_profile_id = NULL,
    destination_profile_row_version = NULL,
    destination_stage = NULL
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, shiftingEventID); err != nil {
		t.Fatalf("clear destination snapshot: %v", err)
	}

	seedShedProfile(t, ctx, pool, countsShedB, "K3")
	_, _, err := completeShifting(repo, ctx, "missing-snapshot-1", shiftingEventID)
	if !errors.Is(err, ports.ErrShiftingDestinationSnapshotMissing) {
		t.Fatalf("complete err=%v, want ErrShiftingDestinationSnapshotMissing for migrated authorized row", err)
	}
	if got := goatShed(t, ctx, pool, mover); got != countsShedA {
		t.Fatalf("mover shed=%s after rejected completion, want source shed %s", got, countsShedA)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K1" {
		t.Fatalf("mover management_stage=%q after rejected completion, want unchanged K1", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after rejected completion, want authorized for re-approval/review", got)
	}
}

// TestCompleteShiftingIntoConfiguredEmptyShedAdoptsProfileCohort proves the empty-destination path:
// with no existing animals to observe, the cohort comes from the shed's CONFIGURED profile. A shed
// configured as Pregnant, with no resident, adopts Pregnant onto the mover — the profile is the
// authority, not the (absent) residents.
func TestCompleteShiftingIntoConfiguredEmptyShedAdoptsProfileCohort(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c111"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K0")
	// Destination shed B is empty but CONFIGURED as Pregnant, so the profile supplies the cohort.
	seedShedProfile(t, ctx, pool, countsShedB, "Pregnant")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "empty-shed-1", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "empty-shed-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// No supplied tag: the configured profile is what names the cohort.
	if _, _, err := completeShifting(repo, ctx, "empty-shed-1", shiftingEventID); err != nil {
		t.Fatalf("complete shifting into configured empty shed: %v", err)
	}

	if got := goatShed(t, ctx, pool, mover); got != countsShedB {
		t.Fatalf("mover shed=%s, want destination shed %s", got, countsShedB)
	}
	if got := goatStage(t, ctx, pool, mover); got != "Pregnant" {
		t.Fatalf("mover management_stage=%q, want the configured profile cohort Pregnant", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, mover); got != 1 {
		t.Fatalf("stage_changed outbox messages=%d, want 1 (K0->Pregnant)", got)
	}
}

// TestCompleteShiftingIntoUnconfiguredShedFailsClosed proves a destination shed with NO active
// configured operational profile FAILS CLOSED rather than inventing a cohort. The cohort is
// authoritative configuration read from shed_profiles, never inferred from residents, so an
// unconfigured shed has no authority to assign a cohort and the move is rejected.
func TestCompleteShiftingIntoUnconfiguredShedFailsClosed(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c121"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K0")
	// Destination shed B is deliberately NOT configured with a shed_profiles row.

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "empty-shed-2", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "empty-shed-2", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	_, _, err := completeShifting(repo, ctx, "empty-shed-2", shiftingEventID)
	if err == nil {
		t.Fatalf("completion into an unconfigured shed succeeded, want a fail-closed error")
	}
	if !errors.Is(err, identityports.ErrDestinationProfileMissing) {
		t.Fatalf("err=%v, want ErrDestinationProfileMissing", err)
	}

	// Nothing moved, the old tag is intact, and the movement stays authorized for retry.
	if got := goatShed(t, ctx, pool, mover); got != countsShedA {
		t.Fatalf("mover shed=%s after rejected completion, want it still at source %s", got, countsShedA)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K0" {
		t.Fatalf("mover management_stage=%q after rejected completion, want it unchanged at K0", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after rejected completion, want it still %q", got, domain.ShiftingEventStatusAuthorized)
	}
}

// TestCompleteShiftingRejectsTagDisagreeingWithConfiguredShed proves a supplied tag that disagrees
// with the destination shed's CONFIGURED cohort is rejected — a shed cannot hold two cohorts, and a
// supplied tag is a request that must agree with the profile, never override it.
func TestCompleteShiftingRejectsTagDisagreeingWithConfiguredShed(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c131"
	resident := "00000000-0000-4000-8000-00000000c132"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
	seedApprovalGoatWithStage(t, ctx, pool, resident, countsShedB, "K2")
	seedShedProfile(t, ctx, pool, countsShedB, "K2") // destination cohort is CONFIGURED as K2

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "disagree-1", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "disagree-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// The operator supplies "Pregnant", which disagrees with the shed's configured K2 cohort.
	_, _, err := completeShiftingWithTag(repo, ctx, "disagree-1", shiftingEventID, "Pregnant")
	if err == nil {
		t.Fatalf("completion with a disagreeing tag succeeded, want a fail-closed conflict")
	}
	if !errors.Is(err, identityports.ErrDestinationTagConflict) {
		t.Fatalf("err=%v, want ErrDestinationTagConflict", err)
	}
	if got := goatShed(t, ctx, pool, mover); got != countsShedA {
		t.Fatalf("mover shed=%s after rejected completion, want it still at source %s", got, countsShedA)
	}
	if got := goatStage(t, ctx, pool, mover); got != "K1" {
		t.Fatalf("mover management_stage=%q after rejected completion, want it unchanged at K1", got)
	}
}

// TestCompleteShiftingReclassificationIsIdempotent proves a retried completion does not double-emit
// goat.stage_changed or thrash the tag.
func TestCompleteShiftingReclassificationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	mover := "00000000-0000-4000-8000-00000000c141"
	resident := "00000000-0000-4000-8000-00000000c142"
	seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
	seedApprovalGoatWithStage(t, ctx, pool, resident, countsShedB, "K2")
	seedShedProfile(t, ctx, pool, countsShedB, "K2")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "reclass-idem", []string{mover})
	if _, _, err := approveShifting(repo, ctx, "reclass-idem", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, _, err := completeShifting(repo, ctx, "reclass-idem", shiftingEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}
	// Exact replay of the same completion.
	if _, replayed, err := completeShifting(repo, ctx, "reclass-idem", shiftingEventID); err != nil {
		t.Fatalf("replay completion: %v", err)
	} else if !replayed {
		t.Fatalf("replay reported replayed=false, want the original result")
	}

	if got := goatStage(t, ctx, pool, mover); got != "K2" {
		t.Fatalf("mover management_stage=%q after replays, want it at K2 once (no thrash)", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, mover); got != 1 {
		t.Fatalf("stage_changed identity events after replays=%d, want 1 -- replay must not re-emit", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'goat.stage_changed'`,
		countsTenant, mover); got != 1 {
		t.Fatalf("stage_changed outbox messages after replays=%d, want 1 -- replay must not re-enqueue", got)
	}
}

// TestCompleteShiftingIntoClinicalCohortFailsClosed is the clinical fail-closed guard (PR #12
// review, 2026-07-20). A shed move must not FABRICATE a clinical fact: an animal does not become
// quarantined, in ICU, sick, under treatment, or recovering by being walked into a clinical shed.
// Adopting such a destination cohort would misclassify a healthy animal and, via the clinical defer
// rule, suppress its vaccination work. The destination cohort is read from the shed's configured
// profile, so a shed CONFIGURED with a clinical cohort must reject with ErrClinicalDestinationTag and
// change nothing — whether or not the shed also holds a clinical resident and whether or not a tag is
// supplied.
func TestCompleteShiftingIntoClinicalCohortFailsClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("configured clinical destination cohort with a clinical resident is rejected", func(t *testing.T) {
		pool := setupCountsDB(t, ctx)
		repo := newRealIdentityApprovalRepo(t, pool)

		mover := "00000000-0000-4000-8000-00000000c131"
		resident := "00000000-0000-4000-8000-00000000c132"
		seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K1")
		// Destination shed B is CONFIGURED as QUARANTINE and holds a quarantine resident.
		seedApprovalGoatWithStage(t, ctx, pool, resident, countsShedB, "quarantine")
		seedShedProfile(t, ctx, pool, countsShedB, "quarantine")

		shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "clinical-1", []string{mover})
		if _, _, err := approveShifting(repo, ctx, "clinical-1", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
			t.Fatalf("approve shifting: %v", err)
		}

		_, _, err := completeShifting(repo, ctx, "clinical-1", shiftingEventID)
		if !errors.Is(err, identityports.ErrClinicalDestinationTag) {
			t.Fatalf("err=%v, want ErrClinicalDestinationTag", err)
		}
		// Fail closed: nothing moved, the mover keeps its tag, and the event stays authorized for a
		// corrected flow (mark the animal clinical via the health path first, then move).
		if got := goatShed(t, ctx, pool, mover); got != countsShedA {
			t.Fatalf("mover shed=%s after rejected completion, want it still at source %s", got, countsShedA)
		}
		if got := goatStage(t, ctx, pool, mover); got != "K1" {
			t.Fatalf("mover management_stage=%q after rejected completion, want unchanged K1", got)
		}
	})

	t.Run("configured clinical profile in an otherwise-empty shed is rejected", func(t *testing.T) {
		pool := setupCountsDB(t, ctx)
		repo := newRealIdentityApprovalRepo(t, pool)

		mover := "00000000-0000-4000-8000-00000000c141"
		seedApprovalGoatWithStage(t, ctx, pool, mover, countsShedA, "K0")
		// Destination shed B is empty but CONFIGURED as ICU, a clinical cohort.
		seedShedProfile(t, ctx, pool, countsShedB, "icu")

		shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "clinical-2", []string{mover})
		if _, _, err := approveShifting(repo, ctx, "clinical-2", approvalRequestID, shiftingEventID, []string{mover}); err != nil {
			t.Fatalf("approve shifting: %v", err)
		}

		_, _, err := completeShifting(repo, ctx, "clinical-2", shiftingEventID)
		if !errors.Is(err, identityports.ErrClinicalDestinationTag) {
			t.Fatalf("err=%v, want ErrClinicalDestinationTag", err)
		}
		if got := goatShed(t, ctx, pool, mover); got != countsShedA {
			t.Fatalf("mover shed=%s after rejected completion, want source %s", got, countsShedA)
		}
		if got := goatStage(t, ctx, pool, mover); got != "K0" {
			t.Fatalf("mover management_stage=%q after rejected completion, want unchanged K0", got)
		}
	})
}
