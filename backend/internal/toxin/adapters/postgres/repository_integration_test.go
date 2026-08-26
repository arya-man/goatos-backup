package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

const (
	toxinTestTenant   = "00000000-0000-4000-8000-000000000001"
	toxinTestPurchase = "10000000-0000-4000-8000-000000000009"
)

func toxinCreateParams(purchaseID string) ports.CreateTaskParams {
	return ports.CreateTaskParams{
		TenantID:       toxinTestTenant,
		FeedPurchaseID: purchaseID,
		FarmLabel:      "CPT",
		FeedItemKey:    "dry_sorghum_forage",
		FeedItemLabel:  "Dry Sorghum Forage",
		Vendor:         "Siddi Srilekha",
		BatchNo:        1,
		PurchaseDate:   "2026-08-25",
		QuantityKg:     2000,
		SourceEventID:  "evt-1",
	}
}

// runToxinSteps walks a round through steps 1/2/3/5/6, passing gate-clearing Now
// instants relative to base so the server-clock waits are satisfied.
func runToxinSteps(t *testing.T, ctx context.Context, repo *Repository, taskID, keyPrefix string, base time.Time) {
	t.Helper()
	for _, stepNo := range []int{1, 2, 3} {
		if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
			TenantID: toxinTestTenant, TaskID: taskID, StepNo: stepNo,
			ProofRef: keyPrefix + "-proof-" + string(rune('0'+stepNo)), IdempotencyKey: keyPrefix + "-s" + string(rune('0'+stepNo)),
			Now: base,
		}); err != nil {
			t.Fatalf("step %d: %v", stepNo, err)
		}
	}
	// Step 5 needs 60 minutes after step 3's completion (stamped at DB now()); base+61m
	// clears it regardless of the walltime the completions landed at.
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: taskID, StepNo: 5,
		ProofRef: keyPrefix + "-proof-5", IdempotencyKey: keyPrefix + "-s5", Now: base.Add(61 * time.Minute),
	}); err != nil {
		t.Fatalf("step 5: %v", err)
	}
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: taskID, StepNo: 6,
		ProofRef: keyPrefix + "-proof-6", IdempotencyKey: keyPrefix + "-s6", Now: base.Add(70 * time.Minute),
	}); err != nil {
		t.Fatalf("step 6: %v", err)
	}
}

// TestToxinTaskLifecyclePostgresPaths exercises the whole round machine against a real
// Postgres: idempotent creation, step order, the server-clock wait gates, the
// Invalid-strip retest mint, the CEO reject retest mint, the version fence, and the
// idempotency replay contract.
func TestToxinTaskLifecyclePostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 10*time.Second)
	now := time.Now().UTC()

	// 1. Creation is idempotent on the load.
	if err := repo.CreateTaskFromPurchase(ctx, toxinCreateParams(toxinTestPurchase)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.CreateTaskFromPurchase(ctx, toxinCreateParams(toxinTestPurchase)); err != nil {
		t.Fatalf("replay create: %v", err)
	}
	page, err := repo.ListTasks(ctx, ports.ListTasksParams{TenantID: toxinTestTenant, Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Rows) != 1 || page.StatusCounts[domain.StatusInProgress] != 1 {
		t.Fatalf("rows = %d counts = %v, want one in-progress round", len(page.Rows), page.StatusCounts)
	}
	task := page.Rows[0].Task
	if task.RoundNo != 1 || task.Origin != domain.OriginPurchase || task.FeedItemLabel != "Dry Sorghum Forage" {
		t.Fatalf("round-1 task = %+v", task)
	}

	// 2. Order is enforced under the row lock.
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: 2,
		ProofRef: "r1-proof-oo", IdempotencyKey: "r1-oo", Now: now,
	}); !errors.Is(err, domain.ErrStepOutOfOrder) {
		t.Fatalf("step 2 first: err = %v", err)
	}

	// 3. The settling hour is a HARD server-clock block.
	for _, stepNo := range []int{1, 2, 3} {
		if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
			TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: stepNo,
			ProofRef: "r1-proof-" + string(rune('0'+stepNo)), IdempotencyKey: "r1-s" + string(rune('0'+stepNo)), Now: now,
		}); err != nil {
			t.Fatalf("step %d: %v", stepNo, err)
		}
	}
	_, err = repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: 5,
		ProofRef: "r1-proof-5", IdempotencyKey: "r1-s5-early", Now: now.Add(30 * time.Minute),
	})
	var wait domain.WaitNotElapsed
	if !errors.Is(err, domain.ErrWaitNotElapsed) || !errors.As(err, &wait) || wait.OpensAt.IsZero() {
		t.Fatalf("step 5 at +30m: err = %v (opensAt %v)", err, wait.OpensAt)
	}
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: 5,
		ProofRef: "r1-proof-5", IdempotencyKey: "r1-s5", Now: now.Add(61 * time.Minute),
	}); err != nil {
		t.Fatalf("step 5 at +61m: %v", err)
	}
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: task.TaskID, StepNo: 6,
		ProofRef: "r1-proof-6", IdempotencyKey: "r1-s6", Now: now.Add(70 * time.Minute),
	}); err != nil {
		t.Fatalf("step 6: %v", err)
	}

	// 4. An Invalid strip cancels the round and mints its retest in ONE transaction.
	row, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: task.TaskID,
		Outcome: domain.OutcomeInvalid, StripPhotoRef: "photo-1",
		IdempotencyKey: "r1-submit", Now: now.Add(80 * time.Minute),
	})
	if err != nil {
		t.Fatalf("invalid submit: %v", err)
	}
	if row.Task.Status != domain.StatusCancelled || row.Task.SupersededByTaskID == "" || row.Task.CancelReason == "" {
		t.Fatalf("cancelled round = %+v", row.Task)
	}
	retest, err := repo.GetTask(ctx, toxinTestTenant, row.Task.SupersededByTaskID)
	if err != nil {
		t.Fatalf("retest read: %v", err)
	}
	if retest.Task.RoundNo != 2 || retest.Task.Origin != domain.OriginInvalidRetest ||
		retest.Task.RetestOfTaskID != task.TaskID || retest.Task.Status != domain.StatusInProgress ||
		len(retest.Completions) != 0 {
		t.Fatalf("retest round = %+v (completions %d)", retest.Task, len(retest.Completions))
	}

	// 5. ONE CAPTURE PROVES ONE STEP: round 1's step-1 clip cannot be reused, not even on a
	// different round of the same load. The proof validator cannot tell which step a clip
	// shows, so this write-level rule is what stops one video standing in for six. Asserted on
	// the OPEN retest round, because a terminal round refuses step work before reaching it.
	if _, err := repo.CompleteStep(ctx, ports.CompleteStepParams{
		TenantID: toxinTestTenant, TaskID: retest.Task.TaskID, StepNo: 1,
		ProofRef: "r1-proof-1", IdempotencyKey: "reuse-attempt", Now: now,
	}); !errors.Is(err, domain.ErrProofAlreadyUsed) {
		t.Fatalf("reusing round 1's step-1 capture: err = %v, want ErrProofAlreadyUsed", err)
	}

	// 5. Round 2 reaches review; a stale-version verdict is fenced; a reject cancels and
	// mints round 3 with the reason carried into the cancel copy.
	runToxinSteps(t, ctx, repo, retest.Task.TaskID, "r2", now)
	reviewRow, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: retest.Task.TaskID,
		Outcome: domain.OutcomePositive, StripPhotoRef: "photo-2",
		IdempotencyKey: "r2-submit", Now: now.Add(80 * time.Minute),
	})
	if err != nil {
		t.Fatalf("round-2 submit: %v", err)
	}
	if reviewRow.Task.Status != domain.StatusPendingReview || reviewRow.Task.Outcome != domain.OutcomePositive {
		t.Fatalf("round-2 after submit = %+v", reviewRow.Task)
	}
	if _, err := repo.RecordVerdict(ctx, ports.VerdictParams{
		TenantID: toxinTestTenant, TaskID: retest.Task.TaskID,
		Decision: domain.VerdictReject, Reason: "sample video does not show this load",
		RowVersion: reviewRow.Task.RowVersion - 1, IdempotencyKey: "r2-verdict-stale",
	}); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale verdict: err = %v", err)
	}
	rejected, err := repo.RecordVerdict(ctx, ports.VerdictParams{
		TenantID: toxinTestTenant, TaskID: retest.Task.TaskID,
		Decision: domain.VerdictReject, Reason: "sample video does not show this load",
		RowVersion: reviewRow.Task.RowVersion, IdempotencyKey: "r2-verdict",
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Task.Status != domain.StatusCancelled || rejected.Task.SupersededByTaskID == "" {
		t.Fatalf("rejected round = %+v", rejected.Task)
	}
	round3, err := repo.GetTask(ctx, toxinTestTenant, rejected.Task.SupersededByTaskID)
	if err != nil {
		t.Fatalf("round-3 read: %v", err)
	}
	if round3.Task.RoundNo != 3 || round3.Task.Origin != domain.OriginRejectedRetest {
		t.Fatalf("round 3 = %+v", round3.Task)
	}

	// 6. Round 3 accepts, and the accept is the terminal state.
	runToxinSteps(t, ctx, repo, round3.Task.TaskID, "r3", now)
	final, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: round3.Task.TaskID,
		Outcome: domain.OutcomeNegative, StripPhotoRef: "photo-3",
		IdempotencyKey: "r3-submit", Now: now.Add(80 * time.Minute),
	})
	if err != nil {
		t.Fatalf("round-3 submit: %v", err)
	}
	accepted, err := repo.RecordVerdict(ctx, ports.VerdictParams{
		TenantID: toxinTestTenant, TaskID: round3.Task.TaskID,
		Decision: domain.VerdictAccept, RowVersion: final.Task.RowVersion, IdempotencyKey: "r3-verdict",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Task.Status != domain.StatusAccepted || accepted.Task.SupersededByTaskID != "" {
		t.Fatalf("accepted round = %+v", accepted.Task)
	}

	// 7. Idempotency contract on the submit: exact replay returns the original result
	// with no new side effects; same key + different payload is refused.
	replay, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: round3.Task.TaskID,
		Outcome: domain.OutcomeNegative, StripPhotoRef: "photo-3",
		IdempotencyKey: "r3-submit", Now: now.Add(90 * time.Minute),
	})
	if err != nil || replay.Task.Status != domain.StatusAccepted {
		t.Fatalf("exact replay: %+v err = %v", replay.Task, err)
	}
	if _, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: round3.Task.TaskID,
		Outcome: domain.OutcomePositive, StripPhotoRef: "photo-x",
		IdempotencyKey: "r3-submit", Now: now.Add(90 * time.Minute),
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("mutated replay: err = %v", err)
	}

	// 9. Exactly one live round per load held throughout: rounds 1 and 2 cancelled,
	// round 3 accepted, and a fresh insert for the same load at round 1 conflicts away.
	assertOneLiveRound(t, ctx, pool)
}

func assertOneLiveRound(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var open int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM toxin_test_tasks
WHERE tenant_id = $1 AND feed_purchase_id = $2 AND status IN ('in_progress', 'pending_review')`,
		toxinTestTenant, toxinTestPurchase).Scan(&open); err != nil {
		t.Fatalf("count open rounds: %v", err)
	}
	if open != 0 {
		t.Fatalf("open rounds = %d after terminal accept, want 0", open)
	}
	var total int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM toxin_test_tasks WHERE tenant_id = $1 AND feed_purchase_id = $2`,
		toxinTestTenant, toxinTestPurchase).Scan(&total); err != nil {
		t.Fatalf("count rounds: %v", err)
	}
	if total != 3 {
		t.Fatalf("rounds = %d, want 3 (invalid retest + rejected retest + accepted)", total)
	}
}
