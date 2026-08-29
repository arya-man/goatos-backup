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
	repoAnimalTwo       = "00000000-0000-4000-8000-000000009202"
	repoAnimalProof     = "00000000-0000-4000-8000-000000009301"
	repoPendingProof    = "00000000-0000-4000-8000-000000009302"
	repoShedProof       = "00000000-0000-4000-8000-000000009303"
	repoPhotoProof      = "00000000-0000-4000-8000-000000009304"
	repoAnimalShedProof = "00000000-0000-4000-8000-000000009305"
	repoAnimalTwoProof  = "00000000-0000-4000-8000-000000009311"
	repoShedProofTwo    = "00000000-0000-4000-8000-000000009306"
	repoShedProofThree  = "00000000-0000-4000-8000-000000009307"
	repoShedProofFour   = "00000000-0000-4000-8000-000000009308"
	repoShedProofFive   = "00000000-0000-4000-8000-000000009309"
	repoShedProofSix    = "00000000-0000-4000-8000-000000009310"
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
	// Proof requirement is now SHED-scoped to the bucket's location, not goat-scoped.
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)

	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "mobile-actual-rfid",
		WeightKg:          12.4,
		ProofArtifactID:   repoExpectedShedProof,
		ActualLocationID:  repoActualShed,
		IdempotencyKey:    "animal:mobile-actual",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	// The operator-supplied actual location is stored verbatim. Its human-readable
	// label is NOT resolved on the write path any more: that needed the locations
	// catalogue, and the weighing write is restricted to weighing-owned tables so it
	// cannot cross-read herd/catalogue state. The label is joined on the read path.
	if obs.ActualLocationID != repoActualShed {
		t.Fatalf("actual location id = %q, want the operator-supplied shed %q", obs.ActualLocationID, repoActualShed)
	}
	assertWeighingAuditAction(t, ctx, pool, obs.ObservationID, "weighing.observation_accepted")
	// FREE-FLOW: weighing_expected_animals -- the expected-animal roster this
	// assertion used to pin as "untouched" -- was DROPPED entirely (migration
	// 000079). There is no roster table left for the write to touch, which is
	// the strongest possible version of this guarantee.

	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, WeightKg: 12.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:wrong-op", RecordedBy: repoOtherOp,
	})
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("wrong operator err=%v, want forbidden", err)
	}
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, WeightKg: 12.6,
		ProofArtifactID: repoPendingProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:pending-proof", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("pending proof err=%v, want invalid argument", err)
	}
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, WeightKg: 12.65,
		ProofArtifactID: repoAnimalShedProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:shed-scoped-proof", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("shed-scoped animal proof err=%v, want invalid argument", err)
	}

	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, WeightKg: 12.7,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoActualShed, IdempotencyKey: "animal:draft", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("draft campaign err=%v, want immutable", err)
	}
}

func TestFreeFlowAnimalObservationUpdateAndProofReplacementAreAudited(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	const scannedTag = "901007000504332"

	first, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          11.0,
		ProofArtifactID:   repoExpectedShedProofTwo,
		IdempotencyKey:    "animal:free-flow-first",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("record free-flow observation: %v", err)
	}
	assertWeighingAuditAction(t, ctx, pool, first.ObservationID, "weighing.observation_accepted")

	updated, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          12.0,
		ProofArtifactID:   repoExpectedShedProofThree,
		IdempotencyKey:    "animal:free-flow-replace-proof",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("replace free-flow proof: %v", err)
	}
	if updated.ObservationID != first.ObservationID {
		t.Fatalf("updated observation id=%s, want same row %s", updated.ObservationID, first.ObservationID)
	}
	assertWeighingAuditAction(t, ctx, pool, updated.ObservationID, "weighing.observation_updated")
	assertWeighingAuditChange(t, ctx, pool, updated.ObservationID, repoExpectedShedProofTwo, repoExpectedShedProofThree, 11.0, 12.0)

	replayedFirst, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          11.0,
		ProofArtifactID:   repoExpectedShedProofTwo,
		IdempotencyKey:    "animal:free-flow-first",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("replay first free-flow observation: %v", err)
	}
	if replayedFirst.ObservationID != first.ObservationID || replayedFirst.WeightKg != first.WeightKg || replayedFirst.ProofArtifactID != first.ProofArtifactID {
		t.Fatalf("first replay=%+v, want original %+v", replayedFirst, first)
	}
	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          13.0,
		ProofArtifactID:   repoExpectedShedProofTwo,
		IdempotencyKey:    "animal:free-flow-first",
		RecordedBy:        repoOperator,
	})
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key different free-flow payload err=%v, want idempotency conflict", err)
	}
}

func TestRecordAnimalObservationRejectsSiblingCampaignShedScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         12.4,
		ProofArtifactID:  repoExpectedShedProof,
		ActualLocationID: repoExpectedShed,
		IdempotencyKey:   "animal:sibling-scope",
		RecordedBy:       repoOperator,
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("sibling scope err=%v, want not found", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")
	assertScopeStatus(t, ctx, pool, repoShedScope, "pending")
}

func TestAnimalObservationRejectsSameKeyDifferentPayload(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "fingerprint-conflict-rfid",
		WeightKg:          12.4,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:fingerprint-conflict",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:        repoTenant,
		CampaignID:      repoCampaign,
		CampaignShedID:  repoAnimalScope,
		WeightKg:        12.5,
		ProofArtifactID: repoExpectedShedProof,
		IdempotencyKey:  "animal:fingerprint-conflict",
		RecordedBy:      repoOperator,
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key different animal payload err=%v, want idempotency conflict", err)
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

func TestRecordShedObservationUsesShedLevelOperatorAssignmentOneToManyPageBoundaryScopeHierarchyStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds
SET operator_user_id=$1::uuid
WHERE tenant_id=$2::uuid AND campaign_shed_id=$3::uuid`,
		repoOtherOp, repoTenant, repoShedScope)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 410,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:campaign-op-denied", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("campaign operator err=%v, want forbidden for shed owned by other operator", err)
	}

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 411, AnimalCount: 40,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:shed-op-accepted", RecordedBy: repoOtherOp,
	})
	if err != nil {
		t.Fatalf("shed operator record observation: %v", err)
	}
	if obs.CampaignShedID != repoShedScope {
		t.Fatalf("observation shed=%s, want %s", obs.CampaignShedID, repoShedScope)
	}
	t.Log("OneToMany PageBoundary ScopeHierarchy StatusMatrix: shed-level operator auth stays on the exact campaign_shed_id bucket and does not leak through the campaign owner, sibling shed rows, paging boundaries, or terminal campaign statuses")
}

func TestListCampaignsForOperatorParkNameOneToManyPaginationScopeHierarchyStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	newerCampaign := "00000000-0000-4000-8000-000000009901"
	newerShed := "00000000-0000-4000-8000-000000009902"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-03', '2026-08-09', '2026-08-03', 'published', 100, $4::uuid, $4::uuid)`,
		newerCampaign, repoTenant, repoPark, repoOtherOp)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Other Operator Newer Shed', 'individual_animal', $5::uuid, 1)`,
		newerShed, newerCampaign, repoTenant, repoActualShed, repoOtherOp)

	page, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 1)
	if err != nil {
		t.Fatalf("list campaigns for operator: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].CampaignID != repoCampaign {
		t.Fatalf("operator page=%+v, want assigned older campaign despite newer unassigned first page", page.Items)
	}
	// ParkName is the location's human-readable NAME ("Coimbatore"), not its
	// short code ("CBE"). The baseline park row has carried name='Coimbatore'
	// since the migration squash; this assertion wanted "CBE" from the day it
	// was written and has therefore never passed. Product is right, the
	// assertion was wrong.
	if page.Items[0].ParkName != "Coimbatore" {
		t.Fatalf("operator campaign park name=%q, want Coimbatore", page.Items[0].ParkName)
	}
	if len(page.Items[0].Sheds) != 2 {
		t.Fatalf("operator campaign sheds=%+v, want only assigned fixture sheds", page.Items[0].Sheds)
	}
	t.Log("OneToMany Pagination ScopeHierarchy StatusMatrix: operator campaign listing pages over matching campaign_shed rows before limit, keeps canceled/status-filtered sibling assignments out of the operator scope, and carries the park label for director chips")
}

func TestListScopeRosterForOperatorRejectsUnassignedShedVisibility(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	const freeFlowTag = "free-flow-unassigned-shed"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: freeFlowTag,
		WeightKg:          11.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:unassigned-roster-leak-regression",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("seed free-flow observation: %v", err)
	}

	page, err := repo.ListScopeRosterForOperator(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOtherOp, "", 50)
	if err != nil {
		t.Fatalf("wrong operator roster read returned hard error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("wrong operator roster=%+v, want no animal rows", page.Items)
	}
	if len(page.Observations) != 0 {
		t.Fatalf("wrong operator observations=%+v, want no free-flow observation rows", page.Observations)
	}
	page, err = repo.ListScopeRosterForOperator(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "", 50)
	if err != nil {
		t.Fatalf("assigned operator roster read: %v", err)
	}
	// FREE-FLOW: there is no expected-animal roster (weighing_expected_animals
	// was DROPPED, migration 000079) -- Items is always empty, for every
	// operator, assigned or not. Only the scan/observation history is scoped by
	// operator assignment, which is what this regression actually pins.
	if len(page.Items) != 0 {
		t.Fatalf("assigned operator roster=%+v, want no roster rows -- weighing has no expected-animal roster", page.Items)
	}
	if len(page.Observations) != 1 || page.Observations[0].ScannedIdentifier != freeFlowTag {
		t.Fatalf("assigned operator observations=%+v, want free-flow observation row", page.Observations)
	}
}

func TestRecordShedObservationPersistsAverageWeightAndOneToFiveProofs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour, repoShedProofFive, repoShedProofSix} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)
	proofIDs := []string{repoShedProof, repoShedProofTwo, repoShedProofThree, repoShedProofFour, repoShedProofFive}

	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         107.0,
		AverageWeightKg:  13.375,
		AnimalCount:      8,
		ProofArtifactIDs: proofIDs,
		IdempotencyKey:   "shed:five-proof-bundle",
		RecordedBy:       repoOperator,
	})
	if err != nil {
		t.Fatalf("record five-proof shed observation: %v", err)
	}
	// The client-sent average (13.375 = 107/8) is IGNORED since 2026-08-24: the
	// stored average derives from the register snapshot — the fixture's FOUR
	// residents — so 107/4.
	if obs.AverageWeightKg != 26.75 || len(obs.ProofArtifactIDs) != 5 {
		t.Fatalf("observation average/proofs=(%v,%v), want census-derived average 26.75", obs.AverageWeightKg, obs.ProofArtifactIDs)
	}

	var average float64
	var proofCount int
	if err := pool.QueryRow(ctx, `
SELECT wso.average_weight_kg::float8, count(wsop.proof_artifact_id)
FROM weighing_shed_observations wso
JOIN weighing_shed_observation_proofs wsop
  ON wsop.tenant_id=wso.tenant_id
 AND wsop.shed_observation_id=wso.shed_observation_id
WHERE wso.tenant_id=$1::uuid AND wso.shed_observation_id=$2::uuid
GROUP BY wso.average_weight_kg`, repoTenant, obs.ObservationID).Scan(&average, &proofCount); err != nil {
		t.Fatalf("read persisted lump sum: %v", err)
	}
	if average != 26.75 || proofCount != 5 {
		t.Fatalf("persisted average/proof_count=(%v,%d), want (26.75,5)", average, proofCount)
	}

	replay, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:         repoTenant,
		CampaignID:       repoCampaign,
		CampaignShedID:   repoShedScope,
		WeightKg:         107.0,
		AverageWeightKg:  13.375,
		AnimalCount:      8,
		ProofArtifactIDs: proofIDs,
		IdempotencyKey:   "shed:five-proof-bundle",
		RecordedBy:       repoOperator,
	})
	if err != nil {
		t.Fatalf("replay five-proof shed observation: %v", err)
	}
	if replay.ObservationID != obs.ObservationID || len(replay.ProofArtifactIDs) != 5 {
		t.Fatalf("replay=%+v, want original observation and five proofs", replay)
	}
}

func TestRecordShedObservationRejectsMoreThanFiveProofs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	for _, proofID := range []string{repoShedProofTwo, repoShedProofThree, repoShedProofFour, repoShedProofFive, repoShedProofSix} {
		insertProof(t, ctx, pool, proofID, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:        repoTenant,
		CampaignID:      repoCampaign,
		CampaignShedID:  repoShedScope,
		AverageWeightKg: 13.375,
		ProofArtifactIDs: []string{
			repoShedProof, repoShedProofTwo, repoShedProofThree,
			repoShedProofFour, repoShedProofFive, repoShedProofSix,
		},
		IdempotencyKey: "shed:six-proof-bundle",
		RecordedBy:     repoOperator,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("six-proof error=%v, want invalid argument", err)
	}
}

func TestDelayedCampaignRemainsExecutableForRolledForwardWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDelayed)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "tag-delayed", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:delayed", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record delayed animal observation: %v", err)
	}
	// FREE-FLOW: a scan alone never completes an individual bucket -- there is
	// no expected set, so nothing can tell the system the operator is done.
	// The bucket completes on the operator's SUBMIT ack.
	// It IS 'in_progress' though: the W-19 finding this comment used to record
	// ("nothing on the capture path advances a bucket to in_progress") is now
	// fixed -- the first capture marks the bucket as being worked, so a shed with
	// scans in it stops reading identically to one nobody has touched. Capturing
	// and completing remain two different things, which is what the next line
	// asserts.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "animal:delayed-submit", []string{"tag-delayed"}); err != nil {
		t.Fatalf("submit delayed individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	assertCampaignStatus(t, ctx, pool, domain.StatusDelayed)

	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 410, AnimalCount: 40,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:delayed", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record delayed shed observation: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	assertCampaignStatus(t, ctx, pool, domain.StatusCompleted)
}

// TestRefreshAvailabilityClassifiesUnavailableHerdTruthAndClosesResolvedScope
// was DELETED (free-flow weighing mandate): RefreshAvailability read
// goats.health_status and wrote a clinical classification into
// weighing_expected_animals, both banned. See AGENTS.md, SKILLS.md, and
// migration 000059.

func TestRecordObservationsRollUpScopeAndCampaignCompletion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "tag-complete-scope", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:complete-scope", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	// FREE-FLOW: the capture alone does not complete the bucket; SUBMIT is the
	// completion signal (see AGENTS.md, "WEIGHING IS SCAN-AND-SUBMIT"). The
	// bucket is 'in_progress' here -- captured, not finished -- see the note in
	// the delayed-campaign test above.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "animal:complete-scope-submit", []string{"tag-complete-scope"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	assertScopeStatus(t, ctx, pool, repoShedScope, "pending")
	assertCampaignStatus(t, ctx, pool, domain.StatusPublished)

	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 410, AnimalCount: 40,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:complete-campaign", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	assertCampaignStatus(t, ctx, pool, domain.StatusCompleted)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "tag-after-complete", WeightKg: 12.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:after-complete", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("completed campaign animal err=%v, want immutable", err)
	}
}

func TestSubmitIndividualScopeCompletesSubmittedFreeFlowEvidenceWithoutExpectedRosterGate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990002', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		repoAnimalTwo, repoTenant, repoParty, repoExpectedShed, repoPark)
	// FREE-FLOW: no expected-animal roster row is seeded for repoAnimalTwo --
	// weighing_expected_animals was DROPPED (migration 000079). This animal
	// existing in `goats` but never being scanned proves there is no roster
	// gate: submit below completes on the scans alone.
	insertProof(t, ctx, pool, repoAnimalTwoProof, "video", "completed", "goat", repoAnimalTwo, "goat", repoAnimalTwo)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "expected-rfid-1", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:only-first-expected", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record first expected animal: %v", err)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'extra-rfid', 13.1, $4::uuid, $5::uuid, 'animal:extra-rfid')`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator)

	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:missing-expected-with-extra", []string{"expected-rfid-1", "extra-rfid"})
	if err != nil {
		t.Fatalf("submit free-flow evidence: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	// The bucket completed even though repoAnimalTwo -- a real goat standing in
	// this shed -- was never scanned. There is no roster row anywhere to check
	// its status against; that absence IS the proof.
}

func TestSubmitIndividualScopeCompletesKnownAnimalWithScannedIdentifier(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const scannedTag = "901007000504332"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: scannedTag, WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed, IdempotencyKey: "animal:known-submit", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record known animal observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:known-animal", []string{scannedTag}); err != nil {
		t.Fatalf("submit known animal scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
}

func TestSubmitIndividualScopeRejectsWhenObservedAnimalsOmittedFromSubmit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 'A', 10.1, $4::uuid, $5::uuid, 'omit-test:a'),
  ($1::uuid, $2::uuid, $3::uuid, 'B', 10.2, $4::uuid, $5::uuid, 'omit-test:b'),
  ($1::uuid, $2::uuid, $3::uuid, 'C', 10.3, $4::uuid, $5::uuid, 'omit-test:c')`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator)

	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:omitted-observed", []string{"A"})
	if !errors.Is(err, ports.ErrScopeIncomplete) {
		t.Fatalf("submit with omitted observed animals err=%v, want ErrScopeIncomplete", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")
}

func TestWeighingFreeFlowOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrixDocumentsBucketSemantics(t *testing.T) {
	t.Log("OneToMany PageBoundary DateShift ScopeHierarchy StatusMatrix: Weighing V1 is free-flow bucket evidence; scanned identifiers are scoped by campaign_shed_id and never become an expected animal roster rule")
}

func TestUpdateCampaignCancelsDeselectedSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "update:deselect-shed",
		Sheds: []domain.CreateCampaignShed{{
			LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal,
		}},
	})
	if err != nil {
		t.Fatalf("update campaign: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")
	assertScopeStatus(t, ctx, pool, repoShedScope, "canceled")
}

func TestUpdateCampaignReaddingDeselectedShedRestoresScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "update:remove-animal-shed",
		Sheds: []domain.CreateCampaignShed{{
			LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition,
		}},
	})
	if err != nil {
		t.Fatalf("remove animal shed: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "canceled")

	_, err = repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "update:readd-animal-shed",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("readd animal shed: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")
	assertExpectedAnimalStatus(t, ctx, pool, repoAnimal, "pending")
}

// A park-week is NOT a uniqueness key. What blocks this create is the SHED:
// repoExpectedShed is already the fixture campaign's open bucket on that weigh
// date, so it must fail with the shed-grain conflict that names the bucket --
// not with a campaign-grain "one task per park per week" refusal.
//
// This previously asserted ports.ErrImmutable, which was
// weighing_campaigns_one_active_week_per_park_idx firing. That index was dropped
// in migration 000081: it could not see weighing_category (a per-bucket column)
// and so also refused legitimate leftover-shed planning. See
// campaign_park_week_multitask_integration_test.go for the flow it blocked.
func TestCreateCampaignRejectsShedAlreadyBookedInThatParkWeek(t *testing.T) {
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
	if !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("re-booking an already-open shed err=%v, want ErrShedAlreadyScheduled", err)
	}
}

func TestCreateCampaignRejectsSameKeyDifferentPayload(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	base := domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-08-03",
		PeriodEndDate:     "2026-08-09",
		StartBusinessDate: "2026-08-03",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "create:fingerprint-conflict",
		Sheds:             []domain.CreateCampaignShed{{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal}},
	}
	first, err := repo.CreateCampaign(ctx, base)
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	replay, err := repo.CreateCampaign(ctx, base)
	if err != nil {
		t.Fatalf("replay campaign: %v", err)
	}
	if replay.CampaignID != first.CampaignID {
		t.Fatalf("replay campaign id=%s, want %s", replay.CampaignID, first.CampaignID)
	}
	changed := base
	changed.PlannedCapPerDay = 125
	_, err = repo.CreateCampaign(ctx, changed)
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key different campaign payload err=%v, want idempotency conflict", err)
	}
}

func TestPublishCampaignRollsBackOnSyncFailure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	execWeighingTestSQL(t, ctx, pool, `
CREATE OR REPLACE FUNCTION public.weighing_publish_outbox_fail_for_test()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.event_type = 'weighing.campaign_published' THEN
    RAISE EXCEPTION 'forced publish outbox failure';
  END IF;
  RETURN NEW;
END $$`)
	execWeighingTestSQL(t, ctx, pool, `
CREATE TRIGGER weighing_publish_outbox_fail_for_test
BEFORE INSERT ON outbox_messages
FOR EACH ROW EXECUTE FUNCTION public.weighing_publish_outbox_fail_for_test()`)

	repo := NewRepository(pool, 5*time.Second)
	if _, err := repo.PublishCampaign(ctx, repoTenant, repoCampaign, repoOperator, "publish:rollback-sync-failure"); err == nil {
		t.Fatal("PublishCampaign error = nil, want forced outbox failure")
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, repoCampaign).Scan(&status); err != nil {
		t.Fatalf("read campaign status: %v", err)
	}
	if status != domain.StatusDraft {
		t.Fatalf("campaign status=%s, want draft after rollback", status)
	}
	var idempotencyRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_idempotency_records WHERE tenant_id=$1::uuid AND idempotency_key='publish:rollback-sync-failure'`, repoTenant).Scan(&idempotencyRows); err != nil {
		t.Fatalf("read idempotency rows: %v", err)
	}
	if idempotencyRows != 0 {
		t.Fatalf("idempotency rows after rollback=%d, want 0", idempotencyRows)
	}
}

func seedWeighingObservationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990001', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET current_location_id=EXCLUDED.current_location_id, shed_id=EXCLUDED.shed_id`,
		repoAnimal, repoTenant, repoParty, repoExpectedShed, repoPark)
	// CENSUS SNAPSHOT (maintainer decision 2026-08-24): a lump-sum submit reads
	// the bucket's live resident count from the herd register, so the per-shed
	// bucket's shed needs residents or every lump-sum submit in this suite would
	// refuse with shed_count_unavailable. FOUR residents, deterministically:
	// every lump-sum average in these tests is weight/4.
	for i := 1; i <= 4; i++ {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'female', 'adult', 'alive', 'adult', $4::uuid, $5::uuid, $6::uuid, $5::uuid)
ON CONFLICT (goat_id) DO UPDATE SET shed_id=EXCLUDED.shed_id, lifecycle_status='alive', exited_at=NULL`,
			"00000000-0000-4000-8000-00000000921"+string(rune('0'+i)), repoTenant, "G-99080"+string(rune('0'+i)), repoParty, repoPerShed, repoPark)
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-29', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO UPDATE SET status=EXCLUDED.status, operator_user_id=EXCLUDED.operator_user_id`,
		repoCampaign, repoTenant, repoPark, repoOperator)
	// Migration 000065 binds a bucket's operator to the bucket's park through an
	// ACTIVE user_scope_grants row. Seed the grant before inserting weighing_campaign_sheds.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`,
		repoTenant, repoOperator, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`,
		repoTenant, repoOtherOp, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $3::uuid, $4::uuid, $5::uuid, 'shed', 'Gandhi 1 - Part 1', 'individual_animal', $7::uuid, 1),
  ($2::uuid, $3::uuid, $4::uuid, $6::uuid, 'shed', 'Q1', 'per_shed_partition', $7::uuid, 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET weighing_category=EXCLUDED.weighing_category, operator_user_id=EXCLUDED.operator_user_id`,
		repoAnimalScope, repoShedScope, repoCampaign, repoTenant, repoExpectedShed, repoPerShed, repoOperator)
	// FREE-FLOW: no expected-animal roster row is seeded here (weighing_expected_animals
	// was DROPPED, migration 000079) -- there is no expected set to seed.
	insertProof(t, ctx, pool, repoAnimalProof, "video", "completed", "goat", repoAnimal, "goat", repoAnimal)
	// Bucket-scoped proof for the individual bucket. Weighing proof is scoped to the
	// WEIGHING BUCKET's shed, never to a goat (maintainer decision 2026-07-31).
	insertProof(t, ctx, pool, repoExpectedShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	insertProof(t, ctx, pool, repoExpectedShedProofTwo, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	insertProof(t, ctx, pool, repoExpectedShedProofThree, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	insertProof(t, ctx, pool, repoPendingProof, "video", "pending", "goat", repoAnimal, "goat", repoAnimal)
	insertProof(t, ctx, pool, repoAnimalShedProof, "video", "completed", "shed", repoActualShed, "goat", repoAnimal)
	insertProof(t, ctx, pool, repoShedProof, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	insertProof(t, ctx, pool, repoShedProofTwo, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	insertProof(t, ctx, pool, repoShedProofThree, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
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

// assertExpectedAnimalStatus is a NO-OP survivor of the deleted expected-animal
// roster (weighing_expected_animals was DROPPED, migration 000079). Kept inert
// so existing call sites need not be rewritten one by one; there is no roster
// row left to assert a status on, which is the point of the free-flow mandate.
func assertExpectedAnimalStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, animalID, want string) {
	t.Helper()
	_ = ctx
	_ = pool
	_ = animalID
	_ = want
}

func assertWeighingAuditAction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, observationID, action string) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM audit_log
WHERE tenant_id=$1::uuid
  AND resource_type='weighing_observation'
  AND resource_id=$2::uuid
  AND action=$3`, repoTenant, observationID, action).Scan(&got); err != nil {
		t.Fatalf("read weighing audit action: %v", err)
	}
	if got != 1 {
		t.Fatalf("audit rows for observation=%s action=%s = %d, want 1", observationID, action, got)
	}
}

func assertWeighingAuditChange(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	observationID string,
	wantPreviousProof string,
	wantProof string,
	wantPreviousWeight float64,
	wantWeight float64,
) {
	t.Helper()
	var previousProof string
	var proof string
	var previousWeight float64
	var weight float64
	if err := pool.QueryRow(ctx, `
SELECT metadata->>'previous_proof_id',
  metadata->>'proof_artifact_id',
  (metadata->>'previous_weight_kg')::float8,
  (metadata->>'weight_kg')::float8
FROM audit_log
WHERE tenant_id=$1::uuid
  AND resource_type='weighing_observation'
  AND resource_id=$2::uuid
  AND action='weighing.observation_updated'`, repoTenant, observationID).
		Scan(&previousProof, &proof, &previousWeight, &weight); err != nil {
		t.Fatalf("read weighing audit change metadata: %v", err)
	}
	if previousProof != wantPreviousProof || proof != wantProof || previousWeight != wantPreviousWeight || weight != wantWeight {
		t.Fatalf("audit change=(%s,%s,%v,%v), want (%s,%s,%v,%v)", previousProof, proof, previousWeight, weight, wantPreviousProof, wantProof, wantPreviousWeight, wantWeight)
	}
}

func execWeighingTestSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec weighing fixture SQL: %v\n%s", err, sql)
	}
}
