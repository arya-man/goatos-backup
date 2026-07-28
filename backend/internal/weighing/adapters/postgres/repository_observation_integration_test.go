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

const (
	repoTenant          = "00000000-0000-4000-8000-000000000001"
	repoParty           = "00000000-0000-4000-8000-000000001001"
	repoPark            = "00000000-0000-4000-8000-000000003001"
	repoOperator        = "00000000-0000-4000-8000-000000000301"
	repoOtherOp         = "00000000-0000-4000-8000-000000000302"
	repoCampaign        = "00000000-0000-4000-8000-000000009001"
	repoAnimalScope     = "00000000-0000-4000-8000-000000009101"
	repoShedScope       = "00000000-0000-4000-8000-000000009102"
	repoAnimal          = "00000000-0000-4000-8000-000000009201"
	repoAnimalProof     = "00000000-0000-4000-8000-000000009301"
	repoPendingProof    = "00000000-0000-4000-8000-000000009302"
	repoShedProof       = "00000000-0000-4000-8000-000000009303"
	repoPhotoProof      = "00000000-0000-4000-8000-000000009304"
	repoAnimalShedProof = "00000000-0000-4000-8000-000000009305"
	repoExpectedShed    = "f1b1bad0-47ab-4248-95dc-8fa1472d4fec"
	repoActualShed      = "654260da-956e-4015-bc95-edf3421cae3c"
	repoPerShed         = "86e47f9c-fd1d-461d-9b9a-45d3be9bf12d"
)

func TestRecordAnimalObservationEnforcesStatusOperatorProofAndMobileActualLocation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "animal:mobile-actual",
		RecordedBy:       repoOperator,
	})
	if err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	if obs.ActualLocationID != repoActualShed || obs.ActualLocationLabel != "Godel 1 - Part 3" {
		t.Fatalf("actual location = (%s, %s), want supplied shed label", obs.ActualLocationID, obs.ActualLocationLabel)
	}
	var availability string
	if err := pool.QueryRow(ctx, `SELECT availability_status FROM weighing_expected_animals WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND animal_id=$3::uuid`, repoTenant, repoCampaign, repoAnimal).Scan(&availability); err != nil {
		t.Fatalf("read availability: %v", err)
	}
	if availability != domain.AvailabilityMovedOtherShed {
		t.Fatalf("availability_status=%s, want moved_other_shed", availability)
	}

	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.5,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:wrong-op", RecordedBy: repoOtherOp,
	})
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("wrong operator err=%v, want forbidden", err)
	}
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.6,
		ProofArtifactID: repoPendingProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:pending-proof", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("pending proof err=%v, want invalid argument", err)
	}
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.65,
		ProofArtifactID: repoAnimalShedProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:shed-scoped-proof", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("shed-scoped animal proof err=%v, want invalid argument", err)
	}

	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.7,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:draft", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("draft campaign err=%v, want immutable", err)
	}
}

func TestRecordShedObservationEnforcesStatusOperatorProofAndCategory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 410,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:wrong-op", RecordedBy: repoOtherOp,
	})
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("wrong shed operator err=%v, want forbidden", err)
	}
	_, err = repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 411,
		ProofArtifactID: repoPhotoProof, IdempotencyKey: "shed:photo-proof", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("photo proof err=%v, want invalid argument", err)
	}
	_, err = repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, WeightKg: 412,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:individual-scope", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("individual category shed observation err=%v, want not found", err)
	}
	setCampaignStatus(t, ctx, pool, domain.StatusCompleted)
	_, err = repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 413,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:completed", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("completed campaign err=%v, want immutable", err)
	}
}

func TestRecordObservationsRollUpScopeAndCampaignCompletion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.4,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:complete-scope", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	assertScopeStatus(t, ctx, pool, repoShedScope, "pending")
	assertCampaignStatus(t, ctx, pool, domain.StatusPublished)

	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 410,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:complete-campaign", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	assertCampaignStatus(t, ctx, pool, domain.StatusCompleted)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, AnimalID: repoAnimal, WeightKg: 12.5,
		ProofArtifactID: repoAnimalProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:after-complete", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("completed campaign animal err=%v, want immutable", err)
	}
}

func TestCreateCampaignRejectsDuplicateParkWeek(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "create:duplicate-park-week",
		Sheds:             []domain.CreateCampaignShed{{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal}},
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("duplicate park/week create err=%v, want immutable conflict", err)
	}
}

func seedWeighingObservationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990001', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		repoAnimal, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-29', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO UPDATE SET status=EXCLUDED.status, operator_user_id=EXCLUDED.operator_user_id`,
		repoCampaign, repoTenant, repoPark, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, expected_animal_count)
VALUES
  ($1::uuid, $3::uuid, $4::uuid, $5::uuid, 'shed', 'Gandhi 1 - Part 1', 'individual_animal', 1),
  ($2::uuid, $3::uuid, $4::uuid, $6::uuid, 'shed', 'Q1', 'per_shed_partition', 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET weighing_category=EXCLUDED.weighing_category`,
		repoAnimalScope, repoShedScope, repoCampaign, repoTenant, repoExpectedShed, repoPerShed)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'Gandhi 1 - Part 1', $5::uuid)
ON CONFLICT (campaign_id, animal_id) DO UPDATE SET status='pending', availability_status='expected_shed', current_location_id=NULL, current_location_label=NULL`,
		repoCampaign, repoTenant, repoAnimal, repoExpectedShed, repoAnimalScope)
	insertProof(t, ctx, pool, repoAnimalProof, "video", "completed", "goat", repoAnimal, "goat", repoAnimal)
	insertProof(t, ctx, pool, repoPendingProof, "video", "pending", "goat", repoAnimal, "goat", repoAnimal)
	insertProof(t, ctx, pool, repoAnimalShedProof, "video", "completed", "shed", repoActualShed, "goat", repoAnimal)
	insertProof(t, ctx, pool, repoShedProof, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	insertProof(t, ctx, pool, repoPhotoProof, "photo", "completed", "shed", repoPerShed, "shed", repoPerShed)
}

func insertProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proofID, proofType, uploadState, scopeType, scopeID, subjectType, subjectID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'weighing-test/' || $1, 'video/mp4', $3, $4, $5::uuid, $6, $7::uuid, $8, $9::uuid, now())
ON CONFLICT (proof_id) DO UPDATE SET upload_state=EXCLUDED.upload_state, proof_type=EXCLUDED.proof_type, scope_type=EXCLUDED.scope_type, scope_id=EXCLUDED.scope_id, subject_type=EXCLUDED.subject_type, subject_id=EXCLUDED.subject_id`,
		proofID, repoTenant, uploadState, scopeType, scopeID, subjectType, subjectID, proofType, repoOperator)
}

func setCampaignStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaigns SET status=$1 WHERE tenant_id=$2::uuid AND campaign_id=$3::uuid`, status, repoTenant, repoCampaign)
}

func assertCampaignStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT status FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, repoCampaign).Scan(&got); err != nil {
		t.Fatalf("read campaign status: %v", err)
	}
	if got != want {
		t.Fatalf("campaign status=%s, want %s", got, want)
	}
}

func assertScopeStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT status FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).Scan(&got); err != nil {
		t.Fatalf("read scope status: %v", err)
	}
	if got != want {
		t.Fatalf("scope %s status=%s, want %s", campaignShedID, got, want)
	}
}

func execWeighingTestSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec weighing fixture SQL: %v\n%s", err, sql)
	}
}
