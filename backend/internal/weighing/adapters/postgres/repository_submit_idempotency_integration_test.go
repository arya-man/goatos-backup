package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// SubmitIndividualScope idempotency contract.
//
// The submit is retried by a phone on a bad network, so at-least-once delivery is
// the normal case, not the edge case. Two behaviours must hold and neither had a
// regression test before:
//
//  1. EXACT REPLAY (same key, same payload) returns the original outcome and does
//     no new work — no second shed-submission event, no second bucket completion.
//     A duplicate completion event would tell every downstream consumer the shed
//     was submitted twice.
//  2. SAME KEY, DIFFERENT PAYLOAD is a client bug, not a retry. It must surface a
//     conflict rather than quietly applying the second payload under the first
//     key, which would let a truncated scan list overwrite a complete one.
//
// The fingerprint comparison happens inside the same transaction as the side
// effects, so these assertions are about that transaction's behaviour, not about
// a best-effort check.

func TestSubmitIndividualScopeExactReplayDoesNotDuplicateTheCompletionEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const identifier = "submit-replay-rfid"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:submit-replay", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	const key = "submit:individual-scope-replay"
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, key,
		[]string{identifier}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	firstEvents := countOutbox(t, ctx, pool, "weighing.shed_submission.completed")
	if firstEvents != 1 {
		t.Fatalf("completion events after first submit=%d, want 1", firstEvents)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	// Exact replay: identical key AND identical payload.
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, key,
		[]string{identifier}); err != nil {
		t.Fatalf("exact replay submit: %v", err)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed_submission.completed"); got != 1 {
		t.Fatalf("completion events after replay=%d, want 1 — an exact replay must not re-enqueue the completion event", got)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
}

func TestSubmitIndividualScopeSameKeyDifferentPayloadConflicts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const identifier = "submit-conflict-rfid"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:submit-conflict", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	const key = "submit:individual-scope-conflict"
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, key,
		[]string{identifier}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	eventsAfterFirst := countOutbox(t, ctx, pool, "weighing.shed_submission.completed")

	// Same key, DIFFERENT payload: the scan list no longer matches what the key
	// was minted for. This must conflict rather than silently re-apply.
	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, key,
		[]string{identifier, "a-different-rfid"})
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key-different-payload err=%v, want ErrIdempotencyConflict", err)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed_submission.completed"); got != eventsAfterFirst {
		t.Fatalf("completion events after conflicting submit=%d, want %d — a rejected replay must produce no side effects",
			got, eventsAfterFirst)
	}
}

func TestSubmitIndividualScopeMatchesCaseMismatchedTags(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Capture the tag as "abc123" (lowercase).
	const identifier = "abc123"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:casing-test", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	// Submit with the tag in uppercase "ABC123". Tag matching must be case-insensitive,
	// so the row MUST be submitted. The scope must move to 'completed'.
	const submitKey = "submit:casing-test"
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, submitKey,
		[]string{"ABC123"}); err != nil {
		t.Fatalf("submit with case mismatch: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	// Verify that the row is marked as submitted.
	var submittedAt *time.Time
	if err := pool.QueryRow(ctx, `
SELECT submitted_at FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND lower(btrim(scanned_identifier))='abc123'`,
		repoTenant, repoCampaign, repoAnimalScope).Scan(&submittedAt); err != nil {
		t.Fatalf("query observation submitted_at: %v", err)
	}
	if submittedAt == nil {
		t.Fatal("observation submitted_at is NULL, want a non-NULL timestamp")
	}
}

func TestSubmitIndividualScopeMatchesTrimmedTags(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Capture the tag with surrounding whitespace "  abc123  ".
	const identifierWithSpaces = "  abc123  "
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifierWithSpaces, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:trimming-test", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	// Submit with the tag trimmed "abc123". Tag matching must handle both trimming
	// and case-insensitivity, so the row MUST be submitted. The scope must move to 'completed'.
	const submitKey = "submit:trimming-test"
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, submitKey,
		[]string{"abc123"}); err != nil {
		t.Fatalf("submit with trimmed tag: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	// Verify that the row is marked as submitted.
	var submittedAt *time.Time
	if err := pool.QueryRow(ctx, `
SELECT submitted_at FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND lower(btrim(scanned_identifier))='abc123'`,
		repoTenant, repoCampaign, repoAnimalScope).Scan(&submittedAt); err != nil {
		t.Fatalf("query observation submitted_at: %v", err)
	}
	if submittedAt == nil {
		t.Fatal("observation submitted_at is NULL, want a non-NULL timestamp")
	}
}

func TestSubmitIndividualScopeIncompleteRejectRejectsGenuinelyOmittedAnimalsNotCasingDifferences(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Capture two observations: one in lowercase, one in a different case.
	const identifier1 = "rfid-one"
	const identifier2 = "RFID-TWO" // Different case will be normalized
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifier1, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:incomplete-1", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation 1: %v", err)
	}
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
 ScannedIdentifier: identifier2, WeightKg: 12.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:incomplete-2", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation 2: %v", err)
	}

	// Case 1: Submit with ONLY the first identifier in its original case. Since the second
	// animal was captured in uppercase but we're submitting lowercase only, this should be
	// treated as "genuinely omitted" and NOT accepted as "just a casing difference".
	// The submit should FAIL with ErrScopeIncomplete because one captured animal is missing.
	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:incomplete-test-1", []string{identifier1})
	if !errors.Is(err, ports.ErrScopeIncomplete) {
		t.Fatalf("submit with genuinely omitted animal err=%v, want ErrScopeIncomplete", err)
	}
	// The scope must remain in 'pending' state after rejection.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")

	// Case 2: Submit with BOTH identifiers — one in lowercase matching the captured value,
	// and one in the SAME case as was captured (uppercase). Both should match case-insensitively.
	// The submit should SUCCEED and complete the scope.
	err = repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:incomplete-test-2", []string{"rfid-one", "rfid-two"})
	if err != nil {
		t.Fatalf("submit with all animals (normalized case): %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
}
