package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestPartitionLabelExtractedFromDisplayName verifies that partition_label is correctly
// extracted from display_name when a shed has partitions, and that the OperationalLocationDisplay
// renders correctly. This is a smoke test for migration 000121.
func TestPartitionLabelExtractedFromDisplayName(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	tenantID := uuid.NewString()
	parkID := uuid.NewString()

	// Create a test campaign
	campaignID := uuid.NewString()
	cmd := domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-03",
		PlannedCapPerDay:  100,
		OperatorUserID:    uuid.NewString(),
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "test-partition-label-" + campaignID,
		Sheds: []domain.CreateCampaignShed{
			{
				LocationID:       uuid.NewString(),
				LocationType:     "shed",
				DisplayName:      "Yashoda",                    // Non-partitioned
				WeighingCategory: domain.CategoryIndividualAnimal,
			},
			{
				LocationID:       uuid.NewString(),
				LocationType:     "shed",
				DisplayName:      "Castro 2",                   // Numeric partition format
				WeighingCategory: domain.CategoryPerShedPartition,
			},
			{
				LocationID:       uuid.NewString(),
				LocationType:     "shed",
				DisplayName:      "Godel 1 - Part 3",           // Prefixed partition format
				WeighingCategory: domain.CategoryPerShedPartition,
			},
		},
	}

	campaign, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCampaign failed: %v", err)
	}

	// Verify partition_label extraction and OperationalLocationDisplay
	tests := []struct {
		name                            string
		displayName                     string
		expectedPartitionLabel          string
		expectedParentShedName          string
		expectedOperationalLocationDisplay string
	}{
		{
			name:                            "non-partitioned shed",
			displayName:                     "Yashoda",
			expectedPartitionLabel:          "",
			expectedParentShedName:          "Yashoda",
			expectedOperationalLocationDisplay: "Yashoda",
		},
		{
			name:                            "numeric partition format",
			displayName:                     "Castro 2",
			expectedPartitionLabel:          "2",
			expectedParentShedName:          "Castro",
			expectedOperationalLocationDisplay: "Castro 2",
		},
		{
			name:                            "prefixed partition format",
			displayName:                     "Godel 1 - Part 3",
			expectedPartitionLabel:          "Part 3",
			expectedParentShedName:          "Godel 1",
			expectedOperationalLocationDisplay: "Godel 1 - Part 3",
		},
	}

	if len(campaign.Sheds) != len(tests) {
		t.Fatalf("expected %d sheds, got %d", len(tests), len(campaign.Sheds))
	}

	for i, tt := range tests {
		shed := campaign.Sheds[i]
		if shed.DisplayName != tt.displayName {
			t.Errorf("[%s] display_name mismatch: got %q, want %q", tt.name, shed.DisplayName, tt.displayName)
		}
		if shed.PartitionLabel != tt.expectedPartitionLabel {
			t.Errorf("[%s] partition_label mismatch: got %q, want %q", tt.name, shed.PartitionLabel, tt.expectedPartitionLabel)
		}
		if shed.ParentShedName != tt.expectedParentShedName {
			t.Errorf("[%s] parent_shed_name mismatch: got %q, want %q", tt.name, shed.ParentShedName, tt.expectedParentShedName)
		}
		if shed.OperationalLocationDisplay != tt.expectedOperationalLocationDisplay {
			t.Errorf("[%s] operational_location_display mismatch: got %q, want %q", tt.name, shed.OperationalLocationDisplay, tt.expectedOperationalLocationDisplay)
		}
		// Verify that "Yashoda whole" is never rendered
		if shed.OperationalLocationDisplay == "Yashoda whole" {
			t.Errorf("[%s] rendered 'Yashoda whole', which is prohibited", tt.name)
		}
	}
}

// TestPartitionLabelPersistedInDatabase verifies that partition_label is correctly
// stored and retrieved from the database.
func TestPartitionLabelPersistedInDatabase(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	tenantID := uuid.NewString()
	parkID := uuid.NewString()

	cmd := domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-04",
		PlannedCapPerDay:  100,
		OperatorUserID:    uuid.NewString(),
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "test-persist-" + uuid.NewString(),
		Sheds: []domain.CreateCampaignShed{
			{
				LocationID:       uuid.NewString(),
				LocationType:     "shed",
				DisplayName:      "Castro 2",
				WeighingCategory: domain.CategoryPerShedPartition,
			},
		},
	}

	campaign, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCampaign failed: %v", err)
	}

	if len(campaign.Sheds) == 0 {
		t.Fatal("expected at least one shed")
	}
	shed := campaign.Sheds[0]
	campaignShedID := shed.CampaignShedID

	// Retrieve the campaign and verify partition_label is persisted
	retrieved, err := repo.CampaignByID(ctx, tenantID, campaign.CampaignID, ports.CampaignAccess{Unrestricted: true})
	if err != nil {
		t.Fatalf("CampaignByID failed: %v", err)
	}

	if len(retrieved.Sheds) == 0 {
		t.Fatal("expected at least one shed in retrieved campaign")
	}
	retrievedShed := retrieved.Sheds[0]

	if retrievedShed.CampaignShedID != campaignShedID {
		t.Errorf("campaign_shed_id mismatch: got %q, want %q", retrievedShed.CampaignShedID, campaignShedID)
	}
	if retrievedShed.PartitionLabel != "2" {
		t.Errorf("partition_label mismatch: got %q, want '2'", retrievedShed.PartitionLabel)
	}
	if retrievedShed.OperationalLocationDisplay != "Castro 2" {
		t.Errorf("operational_location_display mismatch: got %q, want 'Castro 2'", retrievedShed.OperationalLocationDisplay)
	}
}
