package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// scsSeedCompletion inserts one vaccination_completions row for an obligation/goat with an
// explicit status, mirroring what RecordCompletionsFromSubmission ('recorded') and a verifier
// verdict ('accepted'/'rejected') leave behind.
func scsSeedCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, batchID, goatID, status string) string {
	t.Helper()
	return scanText(t, ctx, pool,
		`INSERT INTO vaccination_completions
		   (tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key)
		 VALUES ($1, $2, $3, $4, now(), $5, $6)
		 RETURNING completion_id::text`,
		impTenant, obligationID, batchID, goatID, status,
		"scs-completion:"+obligationID+":"+status+":"+time.Now().Format("150405.000000000"))
}

// scsSeedVerificationItem inserts one verification_items row scoped to a shed/goat, mirroring
// internal/sopbridge/vaccination_submission.go's per-goat CreateItem call (ref_type
// "vaccination_goat", shed_id set from the submitting completion's shed). status is
// 'pending' (still open, awaiting a verdict) or a terminal verdict word such as 'rejected'.
func scsSeedVerificationItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, shedID, goatID, status string) {
	t.Helper()
	var verdictReason any
	if status == "rejected" {
		verdictReason = "rescan required"
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO verification_items
		   (tenant_id, vertical, module, category, source_module, source_task_id, source_ref_type, source_ref_id, status, verdict_reason, shed_id, captured_at, idempotency_key)
		 VALUES ($1, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $2, 'vaccination_goat', $3, $4, $5, $6, now(), $7)`,
		impTenant, taskID, goatID, status, verdictReason, shedID,
		"scs-verify:"+taskID+":"+goatID+":"+status); err != nil {
		t.Fatalf("verification item %s/%s: %v", goatID, status, err)
	}
}

// TestShedCompletionSummaryPerGoatRoundStateReopenedShedNotConfusedWithSiblingSubmitted is the
// regression for the live P0: a shed whose obligation was REOPENED by a verifier rejection must
// read as NOT submitted for the CURRENT round (round_submitted=false, submit_state != submitted/
// needs_review), even while a SIBLING shed on the SAME shared park-level drive task genuinely has
// a submission awaiting verification (round_submitted=true, submit_state=submitted).
//
// Before the fix, per_goat_video submit_state was derived from the shared parent sop_tasks.state
// (AGENTS.md bans this: "Shared vaccination drive tasks are aggregate bookkeeping only. A hidden
// park/batch-level sop_tasks.state must not be used as per-shed submitted/proof/verification
// truth."), so the reopened shed inherited the sibling's "needs_review" word and the operator
// could never reach the submit form after rescanning and re-proofing.
func TestShedCompletionSummaryPerGoatRoundStateReopenedShedNotConfusedWithSiblingSubmitted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shedA = "38000000-0000-4000-8000-0000000000a1" // reopened after rejection (Godel 1)
	const shedB = "38000000-0000-4000-8000-0000000000b1" // sibling, genuinely submitted (Godel 2)
	seedShedOperational(t, ctx, pool, shedA, "Godel 1", true, false, false)
	seedShedOperational(t, ctx, pool, shedB, "Godel 2", true, false, false)

	// Shared park-level drive task -- the shape the local sweeper produces today.
	taskID, batchID := scsSeedParkDrive(t, ctx, pool, versionID, impCbe, "per_goat_round_state", nil)
	// The parent task word stays needs_review because shed B's round is genuinely with the
	// verifier -- this is exactly the stale shared state a per-shed read must never inherit.
	if _, err := pool.Exec(ctx, `UPDATE sop_tasks SET state='needs_review' WHERE tenant_id=$1 AND task_id=$2`, impTenant, taskID); err != nil {
		t.Fatalf("mark parent needs_review: %v", err)
	}

	const goatA = "38000000-0000-4000-8000-0000000001a1"
	const goatB = "38000000-0000-4000-8000-0000000001b1"
	seedGoatAtShed(t, ctx, pool, goatA, shedA)
	seedGoatAtShed(t, ctx, pool, goatB, shedB)

	vacc := NewRepository(pool, 5*time.Second)

	// Shed A: seeded as 'completed' first -- MarkObligationCompleted flips an obligation to
	// 'completed' the instant a shed submits (internal/sopbridge/vaccination_submission.go
	// OnTaskSubmitted -> obligation.MarkCompleted), BEFORE any verifier review. Capture RoundID at
	// this pre-reopen "just submitted" snapshot.
	obligationA := scanText(t, ctx, pool,
		`INSERT INTO obligation_instances
		   (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sequence, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $1, DATE '2026-06-23', 'completed', 1, $6)
		 RETURNING obligation_id::text`,
		impTenant, versionID, ruleID, batchID, goatA, "scs-reopen:"+goatA)
	scsSeedCompletion(t, ctx, pool, obligationA, batchID, goatA, "recorded")
	scsSeedVerificationItem(t, ctx, pool, taskID, shedA, goatA, "pending")

	preRejectSummary, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shedA)
	if err != nil {
		t.Fatalf("shed A pre-reject summary: %v", err)
	}
	if preRejectSummary.RoundID == "" {
		t.Fatalf("shed A pre-reject round_id is empty, want a non-empty fingerprint")
	}
	if !preRejectSummary.RoundSubmitted || preRejectSummary.SubmitState != "submitted" {
		t.Fatalf("shed A pre-reject round_submitted=%v submit_state=%q, want true/submitted (obligation completed, verification pending)",
			preRejectSummary.RoundSubmitted, preRejectSummary.SubmitState)
	}

	// Now REJECT: the verification item closes as rejected, the recorded completion moves to
	// rejected (real writer: RejectCompletion archives the row -- mirrored here by flipping
	// status, which is sufficient for this read-path test), and ReopenObligation's own SQL
	// (`SET status = 'due', row_version = row_version + 1`) reopens the obligation. Row-version
	// bump is the real mechanism under test, not a proxy.
	if _, err := pool.Exec(ctx,
		`UPDATE verification_items SET status='rejected', verdict_reason='rescan required'
		 WHERE tenant_id=$1 AND source_task_id=$2 AND source_ref_id=$3 AND status='pending'`,
		impTenant, taskID, goatA); err != nil {
		t.Fatalf("close shed A verification item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE vaccination_completions SET status='rejected' WHERE tenant_id=$1 AND obligation_id=$2`,
		impTenant, obligationA); err != nil {
		t.Fatalf("reject shed A completion: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status='due', completed_at=NULL, row_version=row_version+1, updated_at=now()
		 WHERE tenant_id=$1 AND obligation_id=$2 AND status='completed'`,
		impTenant, obligationA); err != nil {
		t.Fatalf("reopen shed A obligation: %v", err)
	}
	// Operator rescans and re-proofs for the fresh round.
	scsSeedScan(t, ctx, pool, taskID, goatA, "godel1-tag")
	scsSeedGoatProof(t, ctx, pool, taskID, goatA, "godel1-rescan-clip", "completed")

	// Shed B: freshly submitted, genuinely awaiting verification for the CURRENT round.
	scsSeedObligation(t, ctx, pool, versionID, ruleID, batchID, goatB, "completed", 2)
	scsSeedScan(t, ctx, pool, taskID, goatB, "godel2-tag")
	scsSeedGoatProof(t, ctx, pool, taskID, goatB, "godel2-clip", "completed")
	scsSeedVerificationItem(t, ctx, pool, taskID, shedB, goatB, "pending")

	shedASummary, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shedA)
	if err != nil {
		t.Fatalf("shed A summary: %v", err)
	}
	shedBSummary, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shedB)
	if err != nil {
		t.Fatalf("shed B summary: %v", err)
	}

	if shedASummary.RoundSubmitted {
		t.Fatalf("shed A (reopened) round_submitted = true, want false")
	}
	if shedASummary.SubmitState == "submitted" || shedASummary.SubmitState == "needs_review" {
		t.Fatalf("shed A submit_state = %q, must not inherit sibling shed B's submitted word from the shared parent task", shedASummary.SubmitState)
	}
	if !shedASummary.SubmitEnabled {
		t.Fatalf("shed A (rescanned+reproofed) submit_enabled = false, want true so the operator can resubmit; blocking_reason=%v", shedASummary.BlockingReason)
	}
	// The core RoundID proof: reopening this shed's obligation (row_version bump on the SAME
	// obligation row) must change the fingerprint. A client comparing round_id before/after can
	// detect "this round was reopened" even where SubmitState/RoundSubmitted alone cannot.
	if shedASummary.RoundID == preRejectSummary.RoundID {
		t.Fatalf("shed A round_id did not change across reopen: before=%q after=%q, want distinct fingerprints", preRejectSummary.RoundID, shedASummary.RoundID)
	}

	if !shedBSummary.RoundSubmitted {
		t.Fatalf("shed B (genuinely submitted) round_submitted = false, want true")
	}
	if shedBSummary.SubmitState != "submitted" {
		t.Fatalf("shed B submit_state = %q, want submitted", shedBSummary.SubmitState)
	}
	if shedBSummary.RoundID == "" {
		t.Fatalf("shed B round_id is empty, want a non-empty fingerprint")
	}
	if shedBSummary.RoundID == shedASummary.RoundID {
		t.Fatalf("shed A and shed B round_id collide: %q, want distinct sheds to fingerprint distinctly", shedASummary.RoundID)
	}
}
