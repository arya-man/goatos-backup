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

// TestRecordAnimalObservationFreeFlowUnknownIdentifierStillAccepted proves the
// P0 clinical gate does NOT regress free-flow: a scanned identifier that
// resolves to no known animal is still accepted, with animal_id left NULL.
func TestRecordAnimalObservationFreeFlowUnknownIdentifierStillAccepted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const unknownTag = "909900000000001"
	const unknownTagProof = "00000000-0000-4000-8000-000000009403"
	// recordUnknownAnimalObservationTx requires the proof to be scoped to the
	// assigned shed's location (repoAnimalScope -> repoExpectedShed), not just
	// any completed video proof.
	insertProof(t, ctx, pool, unknownTagProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		AnimalID:          unknownTag,
		ScannedIdentifier: unknownTag,
		WeightKg:          9.9,
		ProofArtifactID:   unknownTagProof,
		IdempotencyKey:    "clinical-gate:unknown-identifier",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("free-flow unknown identifier must be accepted, got err=%v", err)
	}
	// obs.AnimalID is a display field that falls back to the scanned tag
	// (COALESCE(animal_id::text, scanned_identifier)); the free-flow proof is
	// the CANONICAL animal_id column being NULL.
	var storedAnimalID *string
	var scanned string
	if err := pool.QueryRow(ctx, `SELECT animal_id, scanned_identifier FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, obs.ObservationID).Scan(&storedAnimalID, &scanned); err != nil {
		t.Fatalf("read stored observation row: %v", err)
	}
	if storedAnimalID != nil {
		t.Fatalf("stored animal_id=%q, want NULL (free-flow proof)", *storedAnimalID)
	}
	if scanned != unknownTag {
		t.Fatalf("scanned_identifier=%q, want %q", scanned, unknownTag)
	}
}

// TestRecordAnimalObservationRefusesIcuAnimal is the P0 blocker's headline
// case: a known animal marked 'icu' must be refused, and no observation row
// may be written.
func TestRecordAnimalObservationRefusesIcuAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `UPDATE goats SET health_status='icu' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:icu",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrAnimalUnavailable) {
		t.Fatalf("icu animal err=%v, want ErrAnimalUnavailable", err)
	}
	assertNoObservationForIdempotencyKey(t, ctx, pool, "clinical-gate:icu")
}

// TestRecordAnimalObservationRefusesQuarantineAnimal refuses a quarantined animal.
func TestRecordAnimalObservationRefusesQuarantineAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `UPDATE goats SET health_status='quarantine' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:quarantine",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrAnimalUnavailable) {
		t.Fatalf("quarantine animal err=%v, want ErrAnimalUnavailable", err)
	}
	assertNoObservationForIdempotencyKey(t, ctx, pool, "clinical-gate:quarantine")
}

// TestRecordAnimalObservationRefusesSickAnimal refuses a sick animal. 'sick' is
// one of the mandatory clinical defer states beyond the 3 states the blocker
// literally named (sick/quarantine/icu also includes under_treatment and
// recovering); the gate fails closed on all 5 mandatory states.
func TestRecordAnimalObservationRefusesSickAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `UPDATE goats SET health_status='sick' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:sick",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrAnimalUnavailable) {
		t.Fatalf("sick animal err=%v, want ErrAnimalUnavailable", err)
	}
	assertNoObservationForIdempotencyKey(t, ctx, pool, "clinical-gate:sick")
}

// TestRecordAnimalObservationAcceptsHealthyKnownAnimal is the control case:
// a healthy known animal on roster is accepted normally.
func TestRecordAnimalObservationAcceptsHealthyKnownAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:healthy",
		RecordedBy:       repoOperator,
	})
	if err != nil {
		t.Fatalf("healthy known animal must be accepted, got err=%v", err)
	}
	if obs.AnimalID != repoAnimal {
		t.Fatalf("animal_id=%q, want %q", obs.AnimalID, repoAnimal)
	}
}

// TestRecordAnimalObservationRefusesIcuAnimalWithNoRosterRow is the mandatory
// adversarial test: a known animal is 'icu' AND has NO weighing_expected_animals
// row for this campaign at all (never on the roster/scope for this campaign).
// A roster-based (wrong) gate would read this as "no roster row -> vacuously
// available" and incorrectly accept the write. The gate must read
// goats.health_status directly and refuse regardless of roster membership.
func TestRecordAnimalObservationRefusesIcuAnimalWithNoRosterRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const offRosterAnimal = "00000000-0000-4000-8000-000000009401"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990099', 'female', 'kid', 'alive', 'kid', 'icu', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET health_status=EXCLUDED.health_status, current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		offRosterAnimal, repoTenant, repoParty, repoExpectedShed, repoPark)
	// Deliberately do NOT insert a weighing_expected_animals row for
	// offRosterAnimal in repoCampaign: this animal is off the roster/scope.
	var rosterRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_expected_animals WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND animal_id=$3::uuid`, repoTenant, repoCampaign, offRosterAnimal).Scan(&rosterRows); err != nil {
		t.Fatalf("read roster rows: %v", err)
	}
	if rosterRows != 0 {
		t.Fatalf("precondition failed: off-roster animal already has %d weighing_expected_animals rows", rosterRows)
	}

	proofID := "00000000-0000-4000-8000-000000009402"
	insertProof(t, ctx, pool, proofID, "video", "completed", "goat", offRosterAnimal, "goat", offRosterAnimal)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         offRosterAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  proofID,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:icu-off-roster",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrAnimalUnavailable) {
		t.Fatalf("off-roster icu animal err=%v, want ErrAnimalUnavailable (a roster-based gate would wrongly accept this)", err)
	}
	assertNoObservationForIdempotencyKey(t, ctx, pool, "clinical-gate:icu-off-roster")
}

// TestRecordAnimalObservationRefusesDeadLifecycleAnimal proves the gate also
// covers exit lifecycle states (goats.lifecycle_status), not just clinical
// health_status.
func TestRecordAnimalObservationRefusesDeadLifecycleAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `UPDATE goats SET lifecycle_status='dead' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoAnimalScope,
		AnimalID:         repoAnimal,
		WeightKg:         12.4,
		ProofArtifactID:  repoAnimalProof,
		ActualLocationID: repoActualShed,
		IdempotencyKey:   "clinical-gate:dead",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrAnimalUnavailable) {
		t.Fatalf("dead lifecycle animal err=%v, want ErrAnimalUnavailable", err)
	}
	assertNoObservationForIdempotencyKey(t, ctx, pool, "clinical-gate:dead")
}

func assertNoObservationForIdempotencyKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_observations WHERE tenant_id=$1::uuid AND idempotency_key=$2`, repoTenant, key).Scan(&count); err != nil {
		t.Fatalf("read observation rows for idempotency key %s: %v", key, err)
	}
	if count != 0 {
		t.Fatalf("observation rows for refused write (idempotency key %s) = %d, want 0", key, count)
	}
}
