package postgres

import (
	"context"
	"fmt"
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

// TestShedCompletionSummaryRoundScopeHierarchyAndOneToMany is the regression for the live P0:
// a shed whose obligation was REOPENED by a verifier rejection must read as NOT submitted for
// the CURRENT round (round_submitted=false, submit_state != submitted/needs_review), even while
// a SIBLING shed on the SAME shared park-level drive task genuinely has a submission awaiting
// verification (round_submitted=true, submit_state=submitted).
//
// projection-review: Grain proof:
//   - OneToMany: shed A and shed B (distinct target_ids in shared batch); RoundID fingerprints
//     diverge on obligation reopen (row_version ++ on same obligation). Membership is stable.
//   - ScopeHierarchy: shedCompletionRoundFacts reads shed-scoped obligations + verification_pending
//     filtered by vi.shed_id; sibling shed B state is invisible to shed A. Scope boundary holds.
//
// Before the fix, per_goat_video submit_state was derived from the shared parent sop_tasks.state
// (AGENTS.md bans this: "Shared vaccination drive tasks are aggregate bookkeeping only. A hidden
// park/batch-level sop_tasks.state must not be used as per-shed submitted/proof/verification
// truth."), so the reopened shed inherited the sibling's "needs_review" word and the operator
// could never reach the submit form after rescanning and re-proofing.
func TestShedCompletionSummaryRoundScopeHierarchyAndOneToMany(t *testing.T) {
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

// TestShedCompletionSummaryRoundPageBoundaryAllObligations exercises pagination: shedCompletionRoundFacts
// reads ALL obligations in the batch with no LIMIT/OFFSET, so shed-completion cardinality is exact
// and RoundID fingerprint is stable across all batch membership (adding a new obligation changes it,
// removing one changes it, but pagination boundaries cannot split the result).
func TestShedCompletionSummaryRoundPageBoundaryAllObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shed = "38000000-0000-4000-8000-0000000000c1"
	seedShedOperational(t, ctx, pool, shed, "TestShed", true, false, false)
	taskID, batchID := scsSeedParkDrive(t, ctx, pool, versionID, impCbe, "per_goat_round_state", nil)

	vacc := NewRepository(pool, 5*time.Second)

	// Create 3 distinct obligations in the same batch (all active: due).
	goats := make([]string, 3)
	obligations := make([]string, 3)
	for i := 0; i < 3; i++ {
		goatID := fmt.Sprintf("38000000-0000-4000-8000-000000000%d01", i)
		goats[i] = goatID
		seedGoatAtShed(t, ctx, pool, goatID, shed)
		obligations[i] = scanText(t, ctx, pool,
			`INSERT INTO obligation_instances
			   (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sequence, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $1, DATE '2026-06-23', 'due', 1, $6)
			 RETURNING obligation_id::text`,
			impTenant, versionID, ruleID, batchID, goatID, "page-boundary:"+goatID)
	}

	sum1, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shed)
	if err != nil {
		t.Fatalf("initial summary: %v", err)
	}
	roundID1 := sum1.RoundID
	if roundID1 == "" {
		t.Fatalf("RoundID empty with 3 open obligations")
	}

	// Mark one obligation completed: RoundID must change (membership changed, row_version changed).
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status='completed', row_version=row_version+1, updated_at=now()
		 WHERE tenant_id=$1 AND obligation_id=$2`,
		impTenant, obligations[0]); err != nil {
		t.Fatalf("complete first obligation: %v", err)
	}
	sum2, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shed)
	if err != nil {
		t.Fatalf("after first completion: %v", err)
	}
	if sum2.RoundID == roundID1 {
		t.Fatalf("RoundID unchanged after completing one obligation: want distinct fingerprints")
	}

	// Reopen it: RoundID must change again (row_version bumped again).
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status='due', row_version=row_version+1, updated_at=now()
		 WHERE tenant_id=$1 AND obligation_id=$2`,
		impTenant, obligations[0]); err != nil {
		t.Fatalf("reopen first obligation: %v", err)
	}
	sum3, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shed)
	if err != nil {
		t.Fatalf("after reopen: %v", err)
	}
	if sum3.RoundID == roundID1 {
		t.Fatalf("RoundID same as initial after reopen: want distinct (row_version changed)")
	}
	if sum3.RoundID == sum2.RoundID {
		t.Fatalf("RoundID same after reopen as after completion: want distinct fingerprints")
	}
	// Pagination-scoped assertion: shedCompletionRoundFacts includes ALL obligations
	// (no LIMIT), so the fingerprint changes are captured at full batch grain, not truncated.
}

// TestShedCompletionSummaryRoundStatusBucketsTerminalAndActive exercises status: all obligation
// statuses (active due/in_progress/completed AND terminal waived/canceled/superseded) are read in
// one query, and shedCompletionRoundState filters them correctly so terminal statuses do NOT
// count as "open" (blocking submit) NOR as "completed" (enabling submit).
func TestShedCompletionSummaryRoundStatusBucketsTerminalAndActive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	versionID, ruleID := scsSeedProtocol(t, ctx, pool)
	const shed = "38000000-0000-4000-8000-0000000000d1"
	seedShedOperational(t, ctx, pool, shed, "TestShed", true, false, false)
	taskID, batchID := scsSeedParkDrive(t, ctx, pool, versionID, impCbe, "per_goat_round_state", nil)

	vacc := NewRepository(pool, 5*time.Second)

	// Create one open (due) and one terminal (canceled) obligation.
	goatOpen := "38000000-0000-4000-8000-0000000001f1"
	goatTerminal := "38000000-0000-4000-8000-0000000001f2"
	seedGoatAtShed(t, ctx, pool, goatOpen, shed)
	seedGoatAtShed(t, ctx, pool, goatTerminal, shed)
	scanText(t, ctx, pool,
		`INSERT INTO obligation_instances
		   (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sequence, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $1, DATE '2026-06-23', 'due', 1, $6)
		 RETURNING obligation_id::text`,
		impTenant, versionID, ruleID, batchID, goatOpen, "status-bucket:open")
	scanText(t, ctx, pool,
		`INSERT INTO obligation_instances
		   (tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sequence, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $1, DATE '2026-06-23', 'canceled', 1, $6)
		 RETURNING obligation_id::text`,
		impTenant, versionID, ruleID, batchID, goatTerminal, "status-bucket:canceled")

	sum, err := vacc.ShedCompletionSummary(ctx, impTenant, taskID, shed)
	if err != nil {
		t.Fatalf("summary with mixed statuses: %v", err)
	}
	// With one open obligation present, shed is NOT submitted (RoundSubmitted=false).
	if sum.RoundSubmitted {
		t.Fatalf("RoundSubmitted=true with open obligation present, want false (open blocks submit)")
	}
	if sum.SubmitState != "draft" {
		t.Fatalf("SubmitState=%q, want draft (open obligation blocks submitted)", sum.SubmitState)
	}
	// Terminal obligation (canceled) must NOT count as "open" or "completed", so it does not
	// change the round-submitted state relative to the one open obligation alone.
}
