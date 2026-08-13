package postgres

import (
	"context"
	"errors"
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

	// The write path resolves partition labels from the shed_partitions CATALOG, not from the
	// display name -- "Castro 2" (a partition) and "Mandela 1" (an ordinary shed) are the same
	// shape, so only the catalog can tell them apart. Seed the park, the parent sheds, the
	// partition-alias locations, and the catalog rows the resolver reads.
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Test Tenant', 'active')
ON CONFLICT DO NOTHING`, tenantID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenantID, parkID)

	castroParentID, godelParentID := uuid.NewString(), uuid.NewString()
	yashodaID, castroPartID, godelPartID, mandelaID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, l := range []struct{ id, name, status string }{
		{castroParentID, "Castro", "active"},
		{godelParentID, "Godel 1", "active"},
		{yashodaID, "Yashoda", "active"},
		{mandelaID, "Mandela 1", "active"},
		{castroPartID, "Castro 2", "inactive"},
		{godelPartID, "Godel 1 - Part 3", "inactive"},
	} {
		mustExec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4, $5)`, tenantID, l.id, parkID, l.name, l.status)
	}
	// Catalog: Castro really has a partition "2"; Godel 1 really has "Part 3". Mandela 1 has NO
	// catalog partition -- it is an ordinary shed whose name simply ends in a number.
	mustExec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '2', '2', 'active', 'location_alias'),
       ($1::uuid, $3::uuid, 'Part 3', '3', 'active', 'location_alias')`, tenantID, castroParentID, godelParentID)

	// The operator must be park-scoped: weighing rejects a campaign whose operator is not granted
	// on that park (operators are single-park by invariant).
	operatorID := uuid.NewString()
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, tenantID, operatorID, parkID)

	// Create a test campaign
	campaignID := uuid.NewString()
	cmd := domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-03",
		PlannedCapPerDay:  100,
		OperatorUserID:    operatorID,
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "test-partition-label-" + campaignID,
		Sheds: []domain.CreateCampaignShed{
			{
				LocationID:       yashodaID,
				LocationType:     "shed",
				DisplayName:      "Yashoda", // Non-partitioned
				WeighingCategory: domain.CategoryIndividualAnimal,
			},
			{
				LocationID:       castroPartID,
				LocationType:     "shed",
				DisplayName:      "Castro 2", // Numeric partition, PROVEN by the catalog
				WeighingCategory: domain.CategoryPerShedPartition,
			},
			{
				LocationID:       godelPartID,
				LocationType:     "shed",
				DisplayName:      "Godel 1 - Part 3", // Worded partition
				WeighingCategory: domain.CategoryPerShedPartition,
			},
			{
				LocationID:       mandelaID,
				LocationType:     "shed",
				DisplayName:      "Mandela 1", // Ordinary shed whose NAME ends in a number
				WeighingCategory: domain.CategoryIndividualAnimal,
			},
		},
	}

	campaign, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCampaign failed: %v", err)
	}

	// Verify partition_label extraction and OperationalLocationDisplay
	tests := []struct {
		name                               string
		displayName                        string
		expectedPartitionLabel             string
		expectedParentShedName             string
		expectedOperationalLocationDisplay string
	}{
		{
			name:                               "non-partitioned shed",
			displayName:                        "Yashoda",
			expectedPartitionLabel:             "",
			expectedParentShedName:             "Yashoda",
			expectedOperationalLocationDisplay: "Yashoda",
		},
		{
			name:                               "numeric partition format",
			displayName:                        "Castro - 2",
			expectedPartitionLabel:             "2",
			expectedParentShedName:             "Castro",
			expectedOperationalLocationDisplay: "Castro - 2",
		},
		{
			name:                               "prefixed partition format",
			displayName:                        "Godel 1 - Part 3",
			expectedPartitionLabel:             "Part 3",
			expectedParentShedName:             "Godel 1",
			expectedOperationalLocationDisplay: "Godel 1 - Part 3",
		},
		{
			// REGRESSION (both directions): the name looks exactly like the numeric partition case,
			// but no catalog row exists, so no partition may be invented. An earlier regex split
			// stamped "1" here; removing the regex then dropped the REAL "Castro 2" partition.
			// The catalog is what distinguishes them.
			name:                               "ordinary shed whose name ends in a number",
			displayName:                        "Mandela 1",
			expectedPartitionLabel:             "",
			expectedParentShedName:             "Mandela 1",
			expectedOperationalLocationDisplay: "Mandela 1",
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

func TestPartitionAliasResolverRejectsSiblingPartitionLabelLeak(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	tenantID := uuid.NewString()
	parkID := uuid.NewString()
	castroID := uuid.NewString()
	gandhiID := uuid.NewString()
	castroAliasID := uuid.NewString()
	operatorID := uuid.NewString()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Test Tenant', 'active')
ON CONFLICT DO NOTHING`, tenantID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenantID, parkID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $5::uuid, 'shed', 'Castro', 'active'),
       ($1::uuid, $3::uuid, $5::uuid, 'shed', 'Gandhi', 'active'),
       ($1::uuid, $4::uuid, $5::uuid, 'shed', 'Castro 1', 'inactive')`, tenantID, castroID, gandhiID, castroAliasID, parkID)
	mustExec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'location_alias'),
       ($1::uuid, $2::uuid, '2', '2', 'active', 'location_alias'),
       ($1::uuid, $3::uuid, '1', '1', 'active', 'location_alias')`, tenantID, castroID, gandhiID)
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, tenantID, operatorID, parkID)

	valid, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-03",
		PlannedCapPerDay:  100,
		OperatorUserID:    operatorID,
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "partition-alias-valid-" + uuid.NewString(),
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       castroAliasID,
			LocationType:     "shed",
			DisplayName:      "Castro 1",
			PartitionLabel:   "1",
			WeighingCategory: domain.CategoryPerShedPartition,
		}},
	})
	if err != nil {
		t.Fatalf("CreateCampaign valid alias failed: %v", err)
	}
	if got := valid.Sheds[0].LocationID; got != castroID {
		t.Fatalf("valid alias resolved to location_id %q, want Castro %q", got, castroID)
	}
	if got := valid.Sheds[0].PartitionLabel; got != "1" {
		t.Fatalf("valid alias partition_label = %q, want 1", got)
	}

	_, err = repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-08",
		PeriodEndDate:     "2026-08-14",
		StartBusinessDate: "2026-08-10",
		PlannedCapPerDay:  100,
		OperatorUserID:    operatorID,
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "partition-alias-invalid-" + uuid.NewString(),
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       castroAliasID,
			LocationType:     "shed",
			DisplayName:      "Castro 2",
			PartitionLabel:   "2",
			WeighingCategory: domain.CategoryPerShedPartition,
		}},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("CreateCampaign with mismatched alias label error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateCampaignIdempotentReplayDoesNotRehydrateMutableAlias(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	tenantID := uuid.NewString()
	parkID := uuid.NewString()
	castroID := uuid.NewString()
	castroAliasID := uuid.NewString()
	operatorID := uuid.NewString()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Test Tenant', 'active')
ON CONFLICT DO NOTHING`, tenantID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenantID, parkID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'shed', 'Castro', 'active'),
       ($1::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', 'inactive')`, tenantID, castroID, castroAliasID, parkID)
	mustExec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'location_alias')`, tenantID, castroID)
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, tenantID, operatorID, parkID)

	cmd := domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-03",
		PlannedCapPerDay:  100,
		OperatorUserID:    operatorID,
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "partition-alias-replay-" + uuid.NewString(),
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       castroAliasID,
			LocationType:     "shed",
			DisplayName:      "Castro 1",
			PartitionLabel:   "1",
			WeighingCategory: domain.CategoryPerShedPartition,
		}},
	}
	created, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatalf("CreateCampaign failed: %v", err)
	}

	mustExec(`UPDATE locations SET name='Legacy Alias No Longer Matches' WHERE tenant_id=$1::uuid AND location_id=$2::uuid`, tenantID, castroAliasID)
	replayed, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatalf("exact replay after alias mutation failed: %v", err)
	}
	if replayed.CampaignID != created.CampaignID {
		t.Fatalf("replay campaign_id = %q, want original %q", replayed.CampaignID, created.CampaignID)
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

	// Same setup as above: the partition is a CATALOG fact, so the fixture must seed the tenant,
	// park, parent shed, partition-alias location, the catalog row, and a park-scoped operator.
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Test Tenant', 'active')
ON CONFLICT DO NOTHING`, tenantID)
	mustExec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenantID, parkID)
	castroParentID, castroPartID, duplicateParentID, duplicatePartID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	mustExec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'shed', 'Castro', 'active'),
       ($1::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 2', 'inactive'),
       ($1::uuid, $5::uuid, $4::uuid, 'shed', 'Mandela', 'active'),
       ($1::uuid, $6::uuid, $4::uuid, 'shed', 'Mandela 2', 'inactive')`, tenantID, castroParentID, castroPartID, parkID, duplicateParentID, duplicatePartID)
	mustExec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '2', '2', 'active', 'location_alias'),
       ($1::uuid, $3::uuid, '2', '2', 'active', 'location_alias')`, tenantID, castroParentID, duplicateParentID)
	operatorID := uuid.NewString()
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, tenantID, operatorID, parkID)

	cmd := domain.CreateCampaign{
		TenantID:          tenantID,
		ParkID:            parkID,
		PeriodStartDate:   "2026-08-01",
		PeriodEndDate:     "2026-08-07",
		StartBusinessDate: "2026-08-04",
		PlannedCapPerDay:  100,
		OperatorUserID:    operatorID,
		CreatedBy:         uuid.NewString(),
		IdempotencyKey:    "test-persist-" + uuid.NewString(),
		Sheds: []domain.CreateCampaignShed{
			{
				LocationID:       castroPartID,
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
	if shed.LocationID != castroParentID {
		t.Fatalf("canonical location_id = %q, want requested alias to resolve to Castro parent %q, not another shed sharing partition label 2", shed.LocationID, castroParentID)
	}
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
	if retrievedShed.OperationalLocationDisplay != "Castro - 2" {
		t.Errorf("operational_location_display mismatch: got %q, want 'Castro - 2'", retrievedShed.OperationalLocationDisplay)
	}
}
