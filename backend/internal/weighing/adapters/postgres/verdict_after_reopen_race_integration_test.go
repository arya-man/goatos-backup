package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// seedReopenRaceBucket puts the fixture bucket into the exact state the race
// produces: one lump-sum submission, then a leadership reopen that withdrew it.
// The reopen deliberately stops there -- retiring the verification item is a
// SECOND step the app layer runs after the reopen commits, and the window
// between the two is the whole defect.
func seedReopenRaceBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository) string {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)

	submission, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:reopen-race", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("lump-sum submission: %v", err)
	}
	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoOperator, "reopen:race", "video unusable"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}
	return submission.ObservationID
}

// A verifier who was already on the review screen when leadership reopened the
// bucket presents the CURRENT, unchanged proof id -- the reopen never touches
// proof_artifact_id -- so the evidence-id comparison passed and the approval
// landed on a withdrawn submission: the observation read as 'verified' work that
// the bucket no longer counts, and the operator's redone capture came back to a
// bucket already carrying a verified round.
func TestApplyVerificationVerdictRejectsApprovalOfAReopenedSubmission(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	observationID := seedReopenRaceBucket(t, ctx, pool, repo)

	_, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   observationID,
		RefType:         domain.VerificationRefTypeShed,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      repoOperator,
		EventID:         "verdict:after-reopen",
		EvidenceProofID: repoShedProof,
	})
	if !errors.Is(err, ports.ErrStaleEvidence) {
		t.Fatalf("verdict on a reopened (withdrawn) submission: err=%v, want ErrStaleEvidence", err)
	}

	// And it must not have written the decision anyway.
	var status string
	if err := pool.QueryRow(ctx, `
SELECT verification_status FROM weighing_shed_observations
WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid`, repoTenant, observationID).Scan(&status); err != nil {
		t.Fatalf("read verification_status: %v", err)
	}
	if status == "verified" {
		t.Fatal("the refused verdict still marked the withdrawn submission verified")
	}
}

// The verdict idempotency key is the bus event id. Two verdicts that share it but
// name DIFFERENT evidence are not a redelivery, and answering the second one from
// the first one's snapshot returned "applied" without ever comparing the named
// proof against the observation -- the replay path returns before the
// stale-evidence guard runs.
func TestApplyVerificationVerdictSameEventIDDifferentEvidenceIsAConflictNotAReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	submission, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:fingerprint", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("lump-sum submission: %v", err)
	}

	verdict := domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   submission.ObservationID,
		RefType:         domain.VerificationRefTypeShed,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      repoOperator,
		EventID:         "verdict:shared-event-id",
		EvidenceProofID: repoShedProof,
	}
	if _, err := repo.ApplyVerificationVerdict(ctx, verdict); err != nil {
		t.Fatalf("first verdict: %v", err)
	}
	// Exact redelivery still replays for free.
	if _, err := repo.ApplyVerificationVerdict(ctx, verdict); err != nil {
		t.Fatalf("exact redelivery must replay, got err=%v", err)
	}

	other := verdict
	other.EvidenceProofID = "11111111-1111-4111-8111-111111111111"
	if _, err := repo.ApplyVerificationVerdict(ctx, other); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same event id with different evidence: err=%v, want ErrIdempotencyConflict (never a cached 'applied')", err)
	}
}
