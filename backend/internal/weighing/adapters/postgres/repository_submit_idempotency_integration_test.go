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
		AnimalID: repoAnimal, ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoExpectedShed,
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
		AnimalID: repoAnimal, ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoExpectedShed,
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
