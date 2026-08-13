package postgres

import (
	"os"
	"strings"
	"testing"
)

// Reopening a lump-sum bucket is where an operator's redone work can be silently
// lost, and the only end-to-end proof of it (repository_lump_sum_resubmit_integration_test.go)
// is behind pgtest.SkipIfNoDocker, so it does NOT run in the default suite. These
// guards read the SQL text itself and run everywhere.
//
// The two failures they exist to stop, both of which shipped as "fixes" before:
//
//  1. Hard-DELETEing the submission row. That erases an operator's rejected proof
//     attempt, which is immutable history, and leaves the verification item and
//     the idempotency record pointing at nothing.
//  2. Leaving the "weighing.shed_observation_accepted" idempotency record behind.
//     RecordShedObservation short-circuits on that record and returns its stored
//     snapshot without touching a table, so an offline outbox replay of the
//     pre-reopen key answers HTTP 200 with the OLD observation id: bucket left
//     in_progress, zero observations written, no outbox event, no verification
//     item. A 500 turned into a silent success that loses the work.
func reopenScopeBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "func (r *Repository) ReopenScope(")
	if start < 0 {
		t.Fatal("ReopenScope not found in repository.go")
	}
	end := strings.Index(src[start:], "\nfunc withdrawnShedObservationIDs(")
	if end < 0 {
		t.Fatal("could not find the end of ReopenScope")
	}
	return src[start : start+end]
}

func TestReopenScopeWithdrawsSubmissionAndClearsItsIdempotencyRecord(t *testing.T) {
	body := reopenScopeBody(t)

	for _, want := range []string{
		// Supersede, never erase.
		"UPDATE weighing_shed_observations",
		"SET withdrawn_at=now()",
		"AND withdrawn_at IS NULL",
		"RETURNING shed_observation_id::text",
		// ... and kill the replay hole for that same submission, in this transaction.
		"DELETE FROM weighing_idempotency_records",
		"AND event_type='weighing.shed_observation_accepted'",
		"AND resource_type='weighing_shed_observation'",
		// Scoped to the observations this reopen actually superseded -- and batched, because a
		// reopen supersedes every open submission in the scope and the per-row form was one round
		// trip each (scale-guard: n-plus-one).
		"AND resource_id = ANY($2::uuid[])",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ReopenScope is missing %q.\nA reopen MUST withdraw the bucket's lump-sum submission AND delete that submission's weighing.shed_observation_accepted idempotency record in the same transaction, or an offline replay of the pre-reopen key returns a stale 200 and the operator's redone work is lost.", want)
		}
	}

	// The clear must stay SCOPED to the ids this reopen withdrew. A DELETE that dropped the
	// resource_id predicate would wipe the whole tenant's accepted-submission idempotency records
	// and silently re-open every replay hole it was written to close.
	if strings.Contains(body, "DELETE FROM weighing_idempotency_records") &&
		!strings.Contains(body, "resource_id") {
		t.Fatal("ReopenScope clears idempotency records without scoping them to the withdrawn observation ids")
	}

	if strings.Contains(body, "DELETE FROM weighing_shed_observations") {
		t.Fatal("ReopenScope hard-DELETEs weighing_shed_observations. A rejected proof attempt is immutable history: stamp withdrawn_at instead (migration 000067) so the row survives and only the one-open-submission slot is freed.")
	}

	// The withdrawal must be inside the SAME transaction as the reopen (tx.Exec /
	// tx.Query), never a second connection that can commit independently.
	if strings.Contains(body, "r.pool.Exec(") || strings.Contains(body, "r.pool.Query(") {
		t.Fatal("ReopenScope writes outside its transaction; every state change here must go through tx")
	}

	// The idempotency delete must be keyed to the WITHDRAWN observations, not to the whole
	// bucket: a bucket that was reopened, resubmitted and reopened again must not have the LIVE
	// submission's record swept away with the old one.
	//
	// Asserted on the id set rather than on a loop. The original per-row DELETE was one round trip
	// per superseded observation (scale-guard: n-plus-one) and is now a single set-based statement
	// bound to `superseded`; both are correctly scoped, and pinning the loop would have forced the
	// slower shape back.
	if !strings.Contains(body, "ANY($2::uuid[])`, tenantID, superseded)") {
		t.Fatal("ReopenScope must clear the idempotency records for exactly the observation ids it withdrew (bound to `superseded`), never for the whole bucket")
	}
	// Scoped to the DELETE statement itself. Checking the whole function body would trip on the
	// withdrawal UPDATE, which is CORRECTLY keyed by campaign_shed_id -- the bucket is the right
	// scope for deciding WHAT to withdraw, and the wrong scope for deciding whose idempotency
	// record to erase.
	if idx := strings.Index(body, "DELETE FROM weighing_idempotency_records"); idx >= 0 {
		stmt := body[idx:]
		if end := strings.Index(stmt, "`"); end >= 0 {
			stmt = stmt[:end]
		}
		if strings.Contains(stmt, "campaign_shed_id") {
			t.Fatal("ReopenScope clears idempotency records by bucket; a later resubmission's live record would be swept away with the withdrawn one")
		}
	}
}

// The verification item raised for the withdrawn submission carries that
// observation id as Source.RefID (adapters/verificationbridge/enqueue.go). If the
// reopen does not report the ids it superseded, the app layer cannot retire those
// items and a verifier is left able to approve work the bucket no longer counts.
func TestReopenScopeReportsSupersededObservationIDs(t *testing.T) {
	body := reopenScopeBody(t)
	if !strings.Contains(body, "return superseded, tx.Commit(ctx)") {
		t.Fatal("ReopenScope must return the withdrawn shed-observation ids so the verification items raised for them can be retired")
	}
	// Weighing must not reach into verification's table to do it.
	if strings.Contains(body, "verification_items") {
		t.Fatal("ReopenScope writes verification_items directly; retire the items through the verification module's own port (backend/AGENTS.md: do not write another module's tables)")
	}
}
