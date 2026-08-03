package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Weighing is FREE-FLOW: the write path does NOT cross-check herd state.
//
// Maintainer decision 2026-07-31, SUPERSEDING the "critical-animal-action gate on
// the weighing write path" item (blocker 2/9) that briefly shipped a clinical
// gate here. Weighing records what the scale and the scanner saw. It is an
// observation, not a clinical action: putting an animal on a scale does not
// administer anything, so refusing the weight of a sick or quarantined animal
// does not protect the animal — it just loses the measurement, and losing the
// weight of an animal under treatment destroys exactly the data a vet needs.
//
// The rules these tests pin:
//   - a scanned identifier is never resolved to herd identity in order to decide
//     whether the write is allowed;
//   - goats.health_status / lifecycle_status are never read on the write path;
//   - sick / under-treatment / recovering / quarantine / ICU / exited animals are
//     all weighable;
//   - the raw scanned_identifier is always stored.
//
// Vaccination stays strict and is untouched by any of this.
//
// The machine twin of these tests is `make weighing-free-flow-guard`, whose
// clinical-state-read-in-write-path rule fails the build if a future change
// reintroduces a health/lifecycle predicate in a weighing write function.

const (
	freeFlowClinicalAnimal = "00000000-0000-4000-8000-000000009281"
	freeFlowClinicalProof  = "00000000-0000-4000-8000-000000009381"
	freeFlowOffRoster      = "00000000-0000-4000-8000-000000009282"
	freeFlowOffRosterProof = "00000000-0000-4000-8000-000000009382"
)

// seedWeighableGoat inserts a goat in the individual bucket's expected shed with
// the given clinical state and its own completed video proof.
//
// onRoster is retained ONLY as a no-op parameter so existing call sites need not
// change: there is no expected-animal roster any more (weighing_expected_animals
// was DROPPED, migration 000079) -- a goat cannot be "on" or "off" a roster that
// does not exist, which is the whole point of these tests.
func seedWeighableGoat(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	animalID, proofID, displayID, healthStatus, lifecycleStatus string, onRoster bool,
) {
	t.Helper()
	_ = onRoster
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, health_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'kid', $4, NULLIF($5,''), 'kid', $6::uuid, $7::uuid, $8::uuid, $7::uuid)
ON CONFLICT (goat_id) DO UPDATE SET health_status=EXCLUDED.health_status, lifecycle_status=EXCLUDED.lifecycle_status`,
		animalID, repoTenant, displayID, lifecycleStatus, healthStatus, repoParty, repoExpectedShed, repoPark)
	// Bucket-scoped proof: weighing proof belongs to the WEIGHING BUCKET's shed, never
	// to a goat. Requiring goat-scoped proof was itself herd coupling.
	insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
}

// Every clinical state is weighable. A weight reading is an observation, not a
// clinical intervention, so no health state may block it.
func TestRecordAnimalObservationAcceptsEveryClinicalState(t *testing.T) {
	for _, tc := range []struct {
		name            string
		healthStatus    string
		lifecycleStatus string
	}{
		{name: "icu", healthStatus: "icu", lifecycleStatus: "alive"},
		{name: "quarantine", healthStatus: "quarantine", lifecycleStatus: "alive"},
		{name: "sick", healthStatus: "sick", lifecycleStatus: "alive"},
		{name: "under_treatment", healthStatus: "under_treatment", lifecycleStatus: "alive"},
		{name: "recovering", healthStatus: "recovering", lifecycleStatus: "alive"},
		{name: "healthy", healthStatus: "healthy", lifecycleStatus: "alive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pgtest.SkipIfNoDocker(t)
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()
			seedWeighingObservationFixture(t, ctx, pool)
			repo := NewRepository(pool, 5*time.Second)

			seedWeighableGoat(t, ctx, pool, freeFlowClinicalAnimal, freeFlowClinicalProof, "G-990081",
				tc.healthStatus, tc.lifecycleStatus, true)

			if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
				TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
				ScannedIdentifier: "clinical-" + tc.name,
				WeightKg:          12.0, ProofArtifactID: freeFlowClinicalProof, ActualLocationID: repoExpectedShed,
				IdempotencyKey: "free-flow:clinical-" + tc.name, RecordedBy: repoOperator,
			}); err != nil {
				t.Fatalf("health=%q must still be weighable, got err=%v — weighing records what the scale saw and never gates on clinical state",
					tc.healthStatus, err)
			}
			// Identified by the scanned tag, not by animal_id: the write never sets
			// animal_id, so herd identity is simply absent from the row.
			if got := countRows(t, ctx, pool,
				`SELECT count(*)::int FROM weighing_observations WHERE tenant_id=$1::uuid AND scanned_identifier=$2`,
				repoTenant, "clinical-"+tc.name); got != 1 {
				t.Fatalf("observation rows for a %q animal=%d, want 1", tc.healthStatus, got)
			}
		})
	}
}

// The raw scanned identifier is stored even when the scan resolves to no animal,
// and no roster row is required or created.
func TestRecordAnimalObservationStoresScannedIdentifierWithoutHerdIdentity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	goatsBefore := countRows(t, ctx, pool, `SELECT count(*)::int FROM goats WHERE tenant_id=$1::uuid`, repoTenant)

	const unknownTag = "909900000000001"
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: unknownTag, WeightKg: 9.9, ProofArtifactID: repoExpectedShedProof,
		IdempotencyKey: "free-flow:unknown-identifier", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("unknown scanned identifier must be accepted, got err=%v", err)
	}

	var scanned string
	if err := pool.QueryRow(ctx,
		`SELECT scanned_identifier FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`,
		repoTenant, obs.ObservationID).Scan(&scanned); err != nil {
		t.Fatalf("read stored observation: %v", err)
	}
	if scanned != unknownTag {
		t.Fatalf("stored scanned_identifier=%q, want %q", scanned, unknownTag)
	}
	// The write invented no herd or roster state.
	if got := countRows(t, ctx, pool, `SELECT count(*)::int FROM goats WHERE tenant_id=$1::uuid`, repoTenant); got != goatsBefore {
		t.Fatalf("goats rows changed from %d to %d — weighing must never write herd identity", goatsBefore, got)
	}
	// weighing_expected_animals was DROPPED (migration 000079): there is no
	// roster table left for the write to touch, which is the strongest possible
	// version of this assertion.
}

// A clinically held animal that is not on the campaign roster at all is still
// weighable. This is the exact case the removed gate refused.
func TestRecordAnimalObservationAcceptsOffRosterIcuAnimalAsFreeFlowScan(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	seedWeighableGoat(t, ctx, pool, freeFlowOffRoster, freeFlowOffRosterProof, "G-990082", "icu", "alive", false)

	// Scanned as a raw identifier, exactly as the phone sends it: no animal_id, so
	// no herd lookup happens at all.
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "off-roster-icu-rfid", WeightKg: 10.5,
		ProofArtifactID: repoExpectedShedProof, IdempotencyKey: "free-flow:off-roster-icu",
		RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("off-roster ICU animal scanned free-flow must be accepted, got err=%v", err)
	}
	if obs.ObservationID == "" {
		t.Fatal("no observation recorded for the free-flow scan")
	}
}

// The expected-animal roster is a LABEL, never a gate.
//
// This is the second half of the same ban as the clinical gate, one table over.
// The write CTE used to INNER JOIN weighing_expected_animals with
// `status <> 'unavailable' AND availability_status NOT IN ('icu','quarantine',...)`,
// so a resolved animal that was off-roster — or whose roster snapshot said ICU —
// produced no row and surfaced to the operator as a 404. Roster membership and a
// periodically-refreshed availability snapshot must not decide whether a weight
// can be recorded.
func TestRecordAnimalObservationAcceptsResolvedAnimalRegardlessOfRosterState(t *testing.T) {
	for _, tc := range []struct {
		name               string
		onRoster           bool
		availabilityStatus string
	}{
		// The write no longer consults the roster at all, so roster state is simply
		// irrelevant: each of these records, and each classifies identically.
		{name: "off_roster", onRoster: false},
		{name: "roster_icu", onRoster: true, availabilityStatus: "icu"},
		{name: "roster_quarantine", onRoster: true, availabilityStatus: "quarantine"},
		{name: "roster_exited", onRoster: true, availabilityStatus: "exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pgtest.SkipIfNoDocker(t)
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()
			seedWeighingObservationFixture(t, ctx, pool)
			repo := NewRepository(pool, 5*time.Second)

			seedWeighableGoat(t, ctx, pool, freeFlowOffRoster, freeFlowOffRosterProof, "G-990083",
				"healthy", "alive", tc.onRoster)
			// weighing_expected_animals was DROPPED (migration 000079): there is no
			// roster/availability row left to seed, so every case in this table
			// now exercises the identical code path -- which is the point.

			obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
				TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
				ScannedIdentifier: "roster-state-" + tc.name,
				WeightKg:          14.25, ProofArtifactID: freeFlowOffRosterProof, ActualLocationID: repoExpectedShed,
				IdempotencyKey: "free-flow:roster-" + tc.name, RecordedBy: repoOperator,
			})
			if err != nil {
				t.Fatalf("roster state %q must not block the weight, got err=%v", tc.name, err)
			}

			// The row belongs to the bucket the operator was working, never NULL.
			var storedShed *string
			if err := pool.QueryRow(ctx, `
SELECT campaign_shed_id::text
FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`,
				repoTenant, obs.ObservationID).Scan(&storedShed); err != nil {
				t.Fatalf("read stored observation: %v", err)
			}
			if storedShed == nil || *storedShed != repoAnimalScope {
				t.Fatalf("stored campaign_shed_id=%v, want the operator's bucket %s", storedShed, repoAnimalScope)
			}
			// Free-flow records an observed scan and stores no verdict about it.
			// mismatch_status used to be read here and asserted to be 'extra_scan';
			// the column was DROPPED (000081) because "extra" has no meaning
			// without the expected set 000079 removed. The column's absence is
			// pinned by TestRecordObservationStoresNoRosterVerdict.
		})
	}
}

// A scanned tag that merely LOOKS like a goat UUID is still just a scanned tag.
//
// The write path used to branch on `uuidutil.IsUUIDString(cmd.AnimalID)` and, for
// anything UUID-shaped, join goats and demand goat-scoped proof — so a UUID-looking
// RFID with no herd row was rejected. Under the strict rule the write never resolves
// identity at all: it stores scanned_identifier. There is no animal_id field or
// column left anywhere on this path (command field removed, table column dropped
// by 000078_weighing_observations_drop_animal_id.sql) for a caller to even attempt
// to supply one.
func TestRecordAnimalObservationStoresUuidLookingTagAsPlainScan(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	// A syntactically valid UUID that is deliberately NOT a goat in this tenant.
	const uuidLookingTag = "00000000-0000-4000-8000-0000000099ff"
	if got := countRows(t, ctx, pool,
		`SELECT count(*)::int FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		repoTenant, uuidLookingTag); got != 0 {
		t.Fatalf("fixture precondition failed: %d goats row(s) for the tag, want 0", got)
	}

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: uuidLookingTag,
		WeightKg:          15.75,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "free-flow:uuid-looking-tag",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("UUID-looking scanned tag with no goats row must still record, got err=%v", err)
	}

	var scanned string
	if err := pool.QueryRow(ctx,
		`SELECT scanned_identifier FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`,
		repoTenant, obs.ObservationID).Scan(&scanned); err != nil {
		t.Fatalf("read stored observation: %v", err)
	}
	if scanned != uuidLookingTag {
		t.Fatalf("stored scanned_identifier=%q, want the raw scanned tag %q", scanned, uuidLookingTag)
	}
}
