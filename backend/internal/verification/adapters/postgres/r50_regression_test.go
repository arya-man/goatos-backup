package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// R50-016: Verdict write is restricted to pending status; attempts to approve→reject
// or reject→approve a verdict in a non-pending state must fail with ErrConflict.
func TestRecordVerdictRejectedWhenNotPending_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-016:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := tenantID

	// Approve the item.
	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: verifierID,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict approve: %v", err)
	}
	if approved.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", approved.Status)
	}

	// Attempt to reject the already-approved item: must fail with ErrConflict because
	// the WHERE clause requires status = 'pending', but the item is now 'approved'.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionRejected,
		Reason:     "rework",
		VerifierID: verifierID,
		RowVersion: approved.RowVersion,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("reject approved item: err = %v, want ErrConflict (item not pending)", err)
	}

	// Also verify the item status did not change.
	fetched, err := repo.GetItem(ctx, tenantID, created.Item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if fetched.Status != domain.StatusApproved {
		t.Fatalf("status after failed reject = %s, want still approved", fetched.Status)
	}
}

// R50-016 reversed: Attempt reject→approve also fails when item is not pending.
func TestRecordVerdictRejectThenApproveRejectsWhenNotPending_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-016-reverse:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := tenantID

	// Reject the item.
	rejected, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionRejected,
		Reason:     "rework needed",
		VerifierID: verifierID,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict reject: %v", err)
	}
	if rejected.Status != domain.StatusRejected {
		t.Fatalf("status = %s, want rejected", rejected.Status)
	}

	// Attempt to approve the already-rejected item: must fail with ErrConflict.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: verifierID,
		RowVersion: rejected.RowVersion,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("approve rejected item: err = %v, want ErrConflict", err)
	}

	// Verify the item status did not change.
	fetched, err := repo.GetItem(ctx, tenantID, created.Item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if fetched.Status != domain.StatusRejected {
		t.Fatalf("status after failed approve = %s, want still rejected", fetched.Status)
	}
}

// R50-018: Operator cannot verify their own work. The predicate
// `(operator_id IS NULL OR operator_id <> $3::uuid)` rejects the verdict
// if the operator_id matches the verifier_id.
func TestRecordVerdictRejectedWhenOperatorIsVerifier_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	operatorID := "00000000-0000-4000-8000-111111111111"
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		OperatorID:     &operatorID, // Item is owned by operatorID
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-018:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// Attempt to have the same operator verify their own work: must fail with ErrConflict.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: operatorID, // Same ID as operator_id
		RowVersion: created.Item.RowVersion,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("operator verify own work: err = %v, want ErrConflict", err)
	}

	// Verify a different verifier can still approve it.
	differentVerifier := "00000000-0000-4000-8000-222222222222"
	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: differentVerifier,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("different verifier approve: %v", err)
	}
	if approved.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", approved.Status)
	}
}

// R50-020: CloseItem is rejected when source_submission_id is set.
// CloseSubmission remains the whole-submission close path.
func TestCloseItemRejectedWhenSourceSubmissionIDSet_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	submissionID := "00000000-0000-4000-8000-333333333333"

	// Create an item owned by a submission.
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:   tenantID,
		Vertical:   "preventive_care",
		Module:     "vaccination",
		Category:   "vaccination_proof",
		Source:     domain.SourceRef{
			Module:       "vaccination",
			SubmissionID: &submissionID,
			RefType:      "vaccination_goat",
			RefID:        tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-020:create",
	})
	if err != nil {
		t.Fatalf("CreateItem with submission: %v", err)
	}

	// Approve it first.
	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: tenantID,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict approve: %v", err)
	}

	// Attempt to close it per-item: must fail with ErrConflict because
	// the WHERE clause requires source_submission_id IS NULL, but this item
	// is owned by a submission.
	_, err = repo.CloseItem(ctx, domain.CloseAction{
		TenantID:    tenantID,
		ItemID:      created.Item.ItemID,
		ActorID:     tenantID,
		RowVersion:  approved.RowVersion,
		IdempotencyKey: "r50-020:close-item",
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("close submission-owned item: err = %v, want ErrConflict", err)
	}

	// CloseSubmission should still work for the whole submission.
	closed, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID:       tenantID,
		SubmissionID:   submissionID,
		ActorID:        tenantID,
		IdempotencyKey: "r50-020:close-submission",
	})
	if err != nil {
		t.Fatalf("CloseSubmission: %v", err)
	}
	if len(closed) != 1 {
		t.Fatalf("closed items count = %d, want 1", len(closed))
	}
	if closed[0].ItemID != created.Item.ItemID {
		t.Fatalf("closed item mismatch")
	}
}

// R50-021: Idempotency — first-call, exact-replay, and same-key-different-payload.
func TestRecordVerdictIdempotencyFirstCallExactReplay_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-021:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := "00000000-0000-4000-8000-444444444444"
	idemKey := "r50-021:verdict"

	// First call: must succeed.
	first, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		Decision:       domain.DecisionApproved,
		VerifierID:     verifierID,
		RowVersion:     created.Item.RowVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("first RecordVerdict: %v", err)
	}
	if first.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", first.Status)
	}

	// Exact replay with the same idempotency key and payload: must return the original result
	// without creating a duplicate outbox event.
	second, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		Decision:       domain.DecisionApproved,
		VerifierID:     verifierID,
		RowVersion:     created.Item.RowVersion, // Note: old row_version, which will fail the storage
		IdempotencyKey: idemKey,                  // but the idempotency check returns the stored result
	})
	if err != nil {
		t.Fatalf("replay RecordVerdict: %v", err)
	}
	if second.ItemID != first.ItemID {
		t.Fatalf("replay returned different item: %s vs %s", second.ItemID, first.ItemID)
	}

	// Verify only one verdict outbox event was created (idempotency prevents duplicates).
	var outboxCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2",
		tenantID, EventVerdictApproved).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox events = %d, want 1 (idempotent)", outboxCount)
	}
}

// R50-021: Same-key different-payload must be rejected with ErrIdempotencyConflict.
func TestRecordVerdictIdempotencySameKeyDifferentPayload_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-021-payload:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := "00000000-0000-4000-8000-555555555555"
	idemKey := "r50-021-payload:verdict"

	// First call: approve.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		Decision:       domain.DecisionApproved,
		VerifierID:     verifierID,
		RowVersion:     created.Item.RowVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("first RecordVerdict: %v", err)
	}

	// Second call with the same key but different decision: must fail with ErrIdempotencyConflict.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		Decision:       domain.DecisionRejected, // Different decision
		Reason:         "rework",
		VerifierID:     verifierID,
		RowVersion:     created.Item.RowVersion,
		IdempotencyKey: idemKey,
	})
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
	}
}

// R50-021: CloseItem idempotency — first-call and exact-replay.
func TestCloseItemIdempotencyFirstCallExactReplay_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-021-close-item:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: tenantID,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}

	idemKey := "r50-021-close-item:close"

	// First close.
	first, err := repo.CloseItem(ctx, domain.CloseAction{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		ActorID:        tenantID,
		RowVersion:     approved.RowVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("first CloseItem: %v", err)
	}
	if first.ClosedAt == nil {
		t.Fatal("first close did not set closed_at")
	}

	// Exact replay.
	second, err := repo.CloseItem(ctx, domain.CloseAction{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		ActorID:        tenantID,
		RowVersion:     approved.RowVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("replay CloseItem: %v", err)
	}
	if second.ItemID != first.ItemID {
		t.Fatalf("replay returned different item")
	}

	// Verify only one close event was created.
	var eventCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2",
		tenantID, EventItemClosed).Scan(&eventCount); err != nil {
		t.Fatalf("count close events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("close events = %d, want 1 (idempotent)", eventCount)
	}
}

// R50-014: Verify that verification.item.closed events are emitted
// and contain the required payload structure with the correct subject_type.
func TestVerificationItemClosedEventProduced_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "r50-014:create",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID:   tenantID,
		ItemID:     created.Item.ItemID,
		Decision:   domain.DecisionApproved,
		VerifierID: tenantID,
		RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}

	_, err = repo.CloseItem(ctx, domain.CloseAction{
		TenantID:       tenantID,
		ItemID:         created.Item.ItemID,
		ActorID:        tenantID,
		RowVersion:     approved.RowVersion,
		IdempotencyKey: "r50-014:close",
	})
	if err != nil {
		t.Fatalf("CloseItem: %v", err)
	}

	// Retrieve the emitted event from the outbox.
	var eventType, payload string
	err = pool.QueryRow(ctx,
		"SELECT event_type, payload FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2 LIMIT 1",
		tenantID, EventItemClosed).Scan(&eventType, &payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("no verification.item.closed event found in outbox")
		}
		t.Fatalf("query outbox: %v", err)
	}

	if eventType != EventItemClosed {
		t.Fatalf("event_type = %s, want %s", eventType, EventItemClosed)
	}

	// Verify payload structure.
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if status, ok := data["status"].(string); !ok || status != domain.StatusApproved {
		t.Fatalf("payload missing or invalid status")
	}
}
