package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed WASTAGE verification gate — proofs of the maintainer-2026-08-18 rule against the REAL
// feed_wastage_completions schema (CHECK constraints, natural/idempotency indexes, and the outbox
// tenant-parity trigger are all part of the assertion surface).
//
// The rule: every feed day, each EXPERIMENT pen owes ONE wastage video. The operator's completion
// records the video and flips the pen-day to 'pending_verification'; the pen-day is 'completed'
// (and feed.wastage.completed is emitted) ONLY when a verifier approves, via ApplyVerifiedWastage.
// A rejected video bounces to 'rework'. The VERIFIER records the measured leftover weight herself
// (RecordWastageMeasurement) — the operator never types a number in this flow.
//
// Gated by pgtest.SkipIfNoDocker (inside setupFeedDirectionDB) + GOATOS_RUN_POSTGRES_TESTS.

func wastageParams() ports.CompleteWastageParams {
	return ports.CompleteWastageParams{
		TenantID:        fdTenant,
		ParkID:          fdPark,
		ShedID:          fdShedA,
		TargetDate:      businessDay(2026, 8, 18),
		WastageProofRef: "proof-wastage-0001",
		CompletedBy:     fdActor,
		IdempotencyKey:  "feed-wastage-key-0001",
		ActorID:         fdActor,
		ActorType:       "operator",
		TraceID:         "trace-feed-wastage-1",
	}
}

// (a) A completion with the video writes a 'pending_verification' EXPERIMENT row — NOTHING is
// completed yet, and the workflow is stamped by the store, never chosen by the caller.
func TestCompleteWastageWritesPendingVerificationExperimentRow(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	res, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatalf("CompleteWastage: %v", err)
	}
	if !res.NewlyPending {
		t.Fatal("first completion NewlyPending = false, want true")
	}
	if res.Status != domain.WastageStatusPendingVerification {
		t.Fatalf("status = %q, want pending_verification", res.Status)
	}

	var status, workflow string
	if err := pool.QueryRow(ctx, `
SELECT status, workflow FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, res.CompletionID).Scan(&status, &workflow); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.WastageStatusPendingVerification {
		t.Fatalf("canonical status = %q, want pending_verification", status)
	}
	if workflow != domain.WorkflowExperiment {
		t.Fatalf("workflow = %q, want experiment — the store stamps it, the caller cannot choose", workflow)
	}

	// The overlay reads the raw status back at the pen grain.
	statuses, err := repo.ListWastageCompletionStatuses(ctx, fdTenant, fdPark, businessDay(2026, 8, 18))
	if err != nil {
		t.Fatalf("ListWastageCompletionStatuses: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Status != domain.WastageStatusPendingVerification || statuses[0].WastageKg != "" {
		t.Fatalf("overlay = %+v, want one pending row with no recorded measurement", statuses)
	}
}

// (b) Verifier approval flips the row to 'completed', emits feed.wastage.completed exactly once,
// and a re-delivered verdict completes nobody twice.
func TestApplyVerifiedWastageCompletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatalf("CompleteWastage: %v", err)
	}

	applied, err := repo.ApplyVerifiedWastage(ctx, ports.ApplyWastageParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		VerifiedBy:   fdActor,
		TraceID:      "trace-verify-wastage-1",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedWastage: %v", err)
	}
	if !applied {
		t.Fatal("ApplyVerifiedWastage applied = false, want true")
	}

	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.WastageStatusCompleted {
		t.Fatalf("canonical status = %q, want completed", status)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.wastage.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows = %d, want 1", outboxCount)
	}

	replay, err := repo.ApplyVerifiedWastage(ctx, ports.ApplyWastageParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, VerifiedBy: fdActor,
		TraceID: "trace-verify-wastage-1-replay",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedWastage replay: %v", err)
	}
	if replay {
		t.Fatal("re-delivered verdict applied = true, want false")
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.wastage.completed' AND aggregate_id = $2::uuid`,
		fdTenant, pending.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox after replay: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows after replay = %d, want 1 (no second event)", outboxCount)
	}
}

// (c) A verifier rejection bounces to 'rework'; the operator re-submits with a NEW video, which
// returns the SAME row to pending_verification with a bumped row_version.
func TestBounceWastageForReworkThenResubmit(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatalf("CompleteWastage: %v", err)
	}

	bounced, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{
		TenantID:     fdTenant,
		CompletionID: pending.CompletionID,
		Reason:       "the scale reading is not visible in the video",
		TraceID:      "trace-rework-wastage-1",
	})
	if err != nil {
		t.Fatalf("BounceWastageForRework: %v", err)
	}
	if !bounced {
		t.Fatal("BounceWastageForRework bounced = false, want true")
	}

	var status string
	var rowVersion int32
	if err := pool.QueryRow(ctx, `
SELECT status, row_version FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status, &rowVersion); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if status != domain.WastageStatusRework {
		t.Fatalf("canonical status = %q, want rework", status)
	}

	resubmit := wastageParams()
	resubmit.IdempotencyKey = "feed-wastage-key-0002"
	resubmit.WastageProofRef = "proof-wastage-0002"
	res, err := repo.CompleteWastage(ctx, resubmit)
	if err != nil {
		t.Fatalf("re-submit CompleteWastage: %v", err)
	}
	if !res.NewlyPending {
		t.Fatal("re-submit NewlyPending = false, want true")
	}
	if res.CompletionID != pending.CompletionID {
		t.Fatalf("re-submit completion id = %s, want existing %s", res.CompletionID, pending.CompletionID)
	}
	if res.RowVersion <= rowVersion {
		t.Fatalf("re-submit row_version = %d, want > %d (bumped)", res.RowVersion, rowVersion)
	}
}

// (d) A pen-day accepts exactly ONE video: a second, DIFFERENT clip under a new key is a loud
// conflict (silent-data-loss regression, same class as packing's), while a genuine re-send of the
// SAME clip is a quiet no-op.
func TestCompleteWastageRejectsASecondDifferentVideoForTheSamePenDay(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatalf("CompleteWastage(first): %v", err)
	}

	second := wastageParams()
	second.WastageProofRef = "proof-wastage-different"
	second.IdempotencyKey = "feed-wastage-key-different"
	if _, err := repo.CompleteWastage(ctx, second); !errors.Is(err, ports.ErrWastageAlreadyRecorded) {
		t.Fatalf("second differing video err = %v, want ErrWastageAlreadyRecorded — answering success "+
			"discards the operator's recording silently", err)
	}

	var storedProof string
	if err := pool.QueryRow(ctx, `
SELECT wastage_proof_ref FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, first.CompletionID).Scan(&storedProof); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if storedProof != "proof-wastage-0001" {
		t.Fatalf("stored wastage_proof_ref = %q, want the first video untouched", storedProof)
	}

	resend := wastageParams() // same proof ref
	resend.IdempotencyKey = "feed-wastage-key-retry"
	again, err := repo.CompleteWastage(ctx, resend)
	if err != nil {
		t.Fatalf("re-sending the SAME video under a new key must be a no-op, got: %v", err)
	}
	if again.NewlyPending {
		t.Fatal("a re-send must not count as a fresh pending transition")
	}
}

// (e) THE VERIFIER'S MEASUREMENT. A first entry stores the value with recorder + instant; a second
// entry REPLACES it and reports the prior value; an exact replay returns the stored state without
// rewriting; zero is VALID; a negative or typo-scale value and an unknown completion are refused.
func TestRecordWastageMeasurementStoresReplacesAndReplays(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	pending, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatalf("CompleteWastage: %v", err)
	}

	// First entry.
	first, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID:       fdTenant,
		CompletionID:   pending.CompletionID,
		WastageKg:      3.5,
		RecordedBy:     fdActor,
		IdempotencyKey: "wastage-measure-1",
		TraceID:        "trace-measure-1",
	})
	if err != nil {
		t.Fatalf("RecordWastageMeasurement: %v", err)
	}
	if first.WastageKg != 3.5 || first.PreviousWastageKg != nil {
		t.Fatalf("first entry = %+v, want 3.5 with no previous value", first)
	}
	if first.SubjectLabel == "" {
		t.Fatal("first entry composed no subject label — the queue row would keep showing the bare pen")
	}

	var storedKg float64
	var recordedBy string
	if err := pool.QueryRow(ctx, `
SELECT wastage_kg::float8, wastage_recorded_by::text FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&storedKg, &recordedBy); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if storedKg != 3.5 || recordedBy != fdActor {
		t.Fatalf("stored (kg=%v, by=%s), want (3.5, %s)", storedKg, recordedBy, fdActor)
	}

	// Status is untouched — the verdict owns the lifecycle.
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, pending.CompletionID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != domain.WastageStatusPendingVerification {
		t.Fatalf("status after measurement = %q, want pending_verification (recording never completes)", status)
	}

	// Exact replay: same key, same value — returns the stored state, no rewrite.
	replay, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID:       fdTenant,
		CompletionID:   pending.CompletionID,
		WastageKg:      3.5,
		RecordedBy:     fdActor,
		IdempotencyKey: "wastage-measure-1",
		TraceID:        "trace-measure-1-replay",
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.WastageKg != 3.5 {
		t.Fatalf("replay value = %v, want 3.5", replay.WastageKg)
	}

	// Same key, DIFFERENT value is a conflict, never a silent overwrite.
	if _, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, WastageKg: 9,
		RecordedBy: fdActor, IdempotencyKey: "wastage-measure-1", TraceID: "t",
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-value err = %v, want ErrIdempotencyConflict", err)
	}

	// Second entry under a new key REPLACES the value and reports the prior one.
	second, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID:       fdTenant,
		CompletionID:   pending.CompletionID,
		WastageKg:      0, // ZERO IS VALID — an empty trough is a real measurement.
		RecordedBy:     fdActor,
		IdempotencyKey: "wastage-measure-2",
		TraceID:        "trace-measure-2",
	})
	if err != nil {
		t.Fatalf("second entry: %v", err)
	}
	if second.WastageKg != 0 || second.PreviousWastageKg == nil || *second.PreviousWastageKg != 3.5 {
		t.Fatalf("second entry = %+v, want 0 replacing 3.5", second)
	}

	// The overlay now carries the recorded value for the operator's completed card.
	statuses, err := repo.ListWastageCompletionStatuses(ctx, fdTenant, fdPark, businessDay(2026, 8, 18))
	if err != nil {
		t.Fatalf("ListWastageCompletionStatuses: %v", err)
	}
	if len(statuses) != 1 || statuses[0].WastageKg == "" {
		t.Fatalf("overlay = %+v, want the recorded value carried", statuses)
	}

	// Refusals: negative, typo-scale, and unknown completion.
	if _, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, WastageKg: -1,
		RecordedBy: fdActor, IdempotencyKey: "wastage-measure-neg", TraceID: "t",
	}); !errors.Is(err, ports.ErrWastageValueOutOfRange) {
		t.Fatalf("negative value err = %v, want ErrWastageValueOutOfRange", err)
	}
	if _, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID: fdTenant, CompletionID: pending.CompletionID, WastageKg: 10001,
		RecordedBy: fdActor, IdempotencyKey: "wastage-measure-huge", TraceID: "t",
	}); !errors.Is(err, ports.ErrWastageValueOutOfRange) {
		t.Fatalf("typo-scale value err = %v, want ErrWastageValueOutOfRange", err)
	}
	if _, err := repo.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID: fdTenant, CompletionID: "fd000000-0000-4000-8000-00000000dead", WastageKg: 1,
		RecordedBy: fdActor, IdempotencyKey: "wastage-measure-missing", TraceID: "t",
	}); !errors.Is(err, ports.ErrWastageCompletionNotFound) {
		t.Fatalf("unknown completion err = %v, want ErrWastageCompletionNotFound", err)
	}
}

// (f) Pen identity: two pens of one shed are two separate wastage rows, and one pen's video never
// closes its neighbour (the 2026-08-08 partition defect class, pinned for wastage).
func TestCompleteWastageKeepsPensApart(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	penOne := wastageParams()
	penOne.PartitionLabel = "1"
	penOne.IdempotencyKey = "feed-wastage-pen-1"
	penOne.WastageProofRef = "proof-wastage-pen-1"
	one, err := repo.CompleteWastage(ctx, penOne)
	if err != nil {
		t.Fatalf("CompleteWastage(pen 1): %v", err)
	}

	penTwo := wastageParams()
	penTwo.PartitionLabel = "2"
	penTwo.IdempotencyKey = "feed-wastage-pen-2"
	penTwo.WastageProofRef = "proof-wastage-pen-2"
	two, err := repo.CompleteWastage(ctx, penTwo)
	if err != nil {
		t.Fatalf("CompleteWastage(pen 2): %v", err)
	}
	if one.CompletionID == two.CompletionID {
		t.Fatalf("pen 1 and pen 2 share a row (%s) — one video would prove both pens", one.CompletionID)
	}
	if !two.NewlyPending {
		t.Fatal("pen 2 must be a fresh pending transition of its own")
	}
}
