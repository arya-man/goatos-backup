package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Weighing is FREE-FLOW: there is no expected roster, so there is no expected
// animal COUNT and no roster VERDICT. These tests pin the two places where the
// real write path used to invent one anyway:
//
//   - weighing_campaign_sheds.expected_animal_count was written as a literal 1 on
//     every bucket, so a CEO-side "how far along" figure computed against it was
//     wrong by construction (a shed where five animals were weighed reported an
//     expectation of one).
//   - weighing_observations.mismatch_status was stamped 'extra_scan' on every
//     insert, so an operator's correct, in-shed work read back as an exception
//     queue. The column is dropped by 000081; "extra" cannot be defined without
//     an expected set, and 000079 deliberately removed the expected set.
//
// Both are asserted against the REAL repository write path (CreateCampaign /
// UpdateCampaign / RecordAnimalObservation) and read back out of Postgres, not
// against a helper.

const noVerdictProof = "00000000-0000-4000-8000-000000009481"

// TestCreateCampaignWritesNoInventedExpectedAnimalCount pins the create path.
func TestCreateCampaignWritesNoInventedExpectedAnimalCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	c, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-08-10",
		PeriodEndDate:     "2026-08-16",
		StartBusinessDate: "2026-08-10",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "create:no-invented-expected-count",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	assertNoExpectedAnimalCount(t, ctx, pool, c.CampaignID)

	// The campaign the write path hands back must not claim an expectation either.
	if c.Progress.IndividualExpectedCount != 0 {
		t.Fatalf("individual_expected_count=%d, want 0: free-flow weighing has no expected animal total", c.Progress.IndividualExpectedCount)
	}
}

// TestUpdateCampaignWritesNoInventedExpectedAnimalCount pins the edit path,
// which restates the same bucket membership fact and had the same literal.
func TestUpdateCampaignWritesNoInventedExpectedAnimalCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "update:no-invented-expected-count",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition},
		},
	}); err != nil {
		t.Fatalf("update campaign: %v", err)
	}

	assertNoExpectedAnimalCount(t, ctx, pool, repoCampaign)
}

func assertNoExpectedAnimalCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT display_name, expected_animal_count
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
ORDER BY display_name`, repoTenant, campaignID)
	if err != nil {
		t.Fatalf("read campaign sheds: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var expected int
		if err := rows.Scan(&name, &expected); err != nil {
			t.Fatalf("scan campaign shed: %v", err)
		}
		seen++
		if expected != 0 {
			t.Fatalf("shed %q expected_animal_count=%d, want 0: weighing is free-flow and has no expected roster, so no bucket may carry an animal expectation", name, expected)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate campaign sheds: %v", err)
	}
	if seen == 0 {
		t.Fatalf("no campaign sheds written for campaign %s", campaignID)
	}
}

// TestRecordObservationStoresNoRosterVerdict pins the observation write path:
// after 000081 there is no mismatch_status column for it to stamp.
func TestRecordObservationStoresNoRosterVerdict(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	insertProof(t, ctx, pool, noVerdictProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "no-roster-verdict-1",
		WeightKg:          18.5, ProofArtifactID: noVerdictProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "free-flow:no-roster-verdict-1", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	var columnCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM information_schema.columns
WHERE table_schema='public' AND table_name='weighing_observations' AND column_name='mismatch_status'`).Scan(&columnCount); err != nil {
		t.Fatalf("inspect weighing_observations columns: %v", err)
	}
	if columnCount != 0 {
		t.Fatalf("weighing_observations still has mismatch_status: free-flow weighing has no expected set, so no scan can be classified expected/wrong/extra; the column can only ever hold an invented verdict")
	}
}
