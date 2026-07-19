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
	return repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "complete-" + key,
		RequestFingerprint: "complete-fp-" + key,
	})
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
