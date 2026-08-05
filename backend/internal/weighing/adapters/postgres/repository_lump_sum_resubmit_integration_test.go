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

// Rework -> resubmit is the core operator loop for a lump-sum bucket: leadership
// reopens the shed because the video was unusable, and the operator submits a new
// weight + new video.
//
// weighing_shed_observations is unique on (tenant_id, campaign_shed_id)
// (weighing_shed_observations_one_open_scope_uidx, partial on withdrawn_at IS NULL
// since migration 000067). ReopenScope used to flip the
// bucket back to 'in_progress' and leave the old submission row in place, so the
// resubmit INSERT died on 23505 and escaped RecordShedObservation as a raw pgx
// error -> HTTP 500. The bucket was permanently unsubmittable.
func TestReopenScopeAllowsLumpSumResubmit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	// Migration 000065 binds a bucket's operator to the bucket's park through an
	// ACTIVE user_scope_grants row, so the shared fixture's operator needs one
	// before any weighing_campaign_sheds write.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:resubmit-first", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("first lump-sum submission: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)

	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoOperator, "reopen:lump-sum", "video unusable"); err != nil {
		t.Fatalf("reopen lump-sum scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusInProgress)

	// The reopened bucket must hold NO OPEN submission any more -- but the rejected
	// attempt itself SURVIVES as history (withdrawn_at stamped, migration 000067).
	var open, withdrawnRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE withdrawn_at IS NULL), count(*) FILTER (WHERE withdrawn_at IS NOT NULL)
FROM weighing_shed_observations
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope).Scan(&open, &withdrawnRows); err != nil {
		t.Fatalf("count shed observations after reopen: %v", err)
	}
	if open != 0 {
		t.Fatalf("OPEN shed observations after reopen=%d, want 0 (submission withdrawn)", open)
	}
	if withdrawnRows != 1 {
		t.Fatalf("withdrawn shed observations after reopen=%d, want 1 (a rejected proof attempt is immutable history, never deleted)", withdrawnRows)
	}

	// The proof bundle of the withdrawn submission survives with it: which video was
	// submitted is exactly what a rework dispute is adjudicated on.
	var bundleRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM weighing_shed_observation_proofs
WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid`, repoTenant, first.ObservationID).Scan(&bundleRows); err != nil {
		t.Fatalf("count proof bundle rows after reopen: %v", err)
	}
	if bundleRows == 0 {
		t.Fatal("proof bundle of the withdrawn submission was destroyed; it is the evidence of what was rejected")
	}

	// THE SILENT-LOSS HOLE: RecordShedObservation short-circuits on the
	// "weighing.shed_observation_accepted" idempotency record and returns its stored
	// snapshot without touching a table. If the reopen leaves that record behind,
	// the operator's offline outbox replays the pre-reopen key and gets HTTP 200 with
	// the OLD observation id -- bucket left in_progress, nothing written, no event,
	// no verification item, redone work gone.
	var staleIdem int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM weighing_idempotency_records
WHERE tenant_id=$1::uuid
  AND event_type='weighing.shed_observation_accepted'
  AND resource_id=$2::uuid`, repoTenant, first.ObservationID).Scan(&staleIdem); err != nil {
		t.Fatalf("count idempotency records after reopen: %v", err)
	}
	if staleIdem != 0 {
		t.Fatalf("idempotency records for the withdrawn submission=%d, want 0", staleIdem)
	}

	// A replay of the pre-reopen key must be an honest refusal, never a stale 200.
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:resubmit-first", RecordedBy: repoOperator,
	}); !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("replay of the withdrawn submission key: err=%v, want ErrImmutable (superseded)", err)
	}

	// THE BUG: this resubmit used to fail with
	// ERROR: duplicate key value violates unique constraint
	// "weighing_shed_observations_one_active_scope_uidx" (SQLSTATE 23505) -> 500.
	second, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 425, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:resubmit-second", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("lump-sum resubmit after reopen: %v", err)
	}
	if second.ObservationID == first.ObservationID {
		t.Fatalf("resubmit reused observation id %s, want a new submission", second.ObservationID)
	}
	if second.WeightKg != 425 {
		t.Fatalf("resubmitted weight=%v, want 425", second.WeightKg)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)

	// Exactly one accepted submission for the bucket - the new one.
	var id string
	if err := pool.QueryRow(ctx, `
SELECT shed_observation_id::text FROM weighing_shed_observations
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid AND withdrawn_at IS NULL`, repoTenant, repoShedScope).Scan(&id); err != nil {
		t.Fatalf("read shed observation after resubmit: %v", err)
	}
	if id != second.ObservationID {
		t.Fatalf("stored shed observation=%s, want the resubmitted %s", id, second.ObservationID)
	}
}

// TestRejectedProofCannotBeReused verifies that an operator cannot re-submit a
// lump-sum observation using a proof that was already attached to a rejected
// (withdrawn or rework) observation. The server MUST refuse with ErrRejectedProofReuse.
//
// BUG SCENARIO: After a verifier rejects a lump-sum shed video, the operator
// re-submits with the SAME rejected video. The app (WeighingViewModel) keeps a
// SYNCED shed proof so the video survives an app restart, making the rejected video
// re-attachable. The server must reject this, forcing a new video.
func TestRejectedProofCannotBeReused(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// First submission with proof A
	first, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:rejected-proof-first", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("first lump-sum submission: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)

	// Verifier rejects it by marking it as rework and then reopening the scope
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations
SET verification_status='rework'
WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid`,
		repoTenant, first.ObservationID)

	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoOperator, "reopen:rejected-proof", "video unusable"); err != nil {
		t.Fatalf("reopen lump-sum scope after rejection: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusInProgress)

	// THE BUG: Operator tries to re-submit using the SAME rejected proof (proof A)
	// Server must REFUSE with ErrRejectedProofReuse, not accept it.
	_, err = repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:rejected-proof-reuse", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrRejectedProofReuse) {
		t.Fatalf("reuse of rejected proof: err=%v, want ErrRejectedProofReuse", err)
	}
}

// TestRejectedProofResubmitWithNewProofSucceeds verifies that an operator CAN
// re-submit a lump-sum observation using a NEW proof after a rejection.
func TestRejectedProofResubmitWithNewProofSucceeds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// First submission with proof A
	first, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:new-proof-first", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("first lump-sum submission: %v", err)
	}

	// Mark as rework and reopen
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_shed_observations
SET verification_status='rework'
WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid`,
		repoTenant, first.ObservationID)

	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoOperator, "reopen:new-proof", "video unusable"); err != nil {
		t.Fatalf("reopen lump-sum scope: %v", err)
	}

	// Re-submit with a NEW proof (proof B, different from proof A)
	second, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 420, AnimalCount: 10, ProofArtifactID: repoShedProofTwo,
		IdempotencyKey: "shed:new-proof-second", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("lump-sum resubmit with new proof: %v", err)
	}

	// Verify new observation was created
	if second.ObservationID == first.ObservationID {
		t.Fatalf("resubmit reused observation id %s, want a new submission", second.ObservationID)
	}
	if second.ProofArtifactID != repoShedProofTwo {
		t.Fatalf("resubmitted with proof %s, want %s", second.ProofArtifactID, repoShedProofTwo)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
}
