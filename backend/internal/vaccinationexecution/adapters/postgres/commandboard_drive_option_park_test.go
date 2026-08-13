package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationCommandBoardDriveOptionsCarryPark is the regression for a drive selector
// that could not tell two parks apart.
//
// The board is routinely read with no park filter (leadership's all-parks view), and the
// drive option row was grouped on batch alone with no park on it. Two drives of the same
// vaccine in the same window -- one in CBE, one in CPT -- therefore arrived as rows a
// consumer had no field to separate on, so they rendered as one selector entry and their
// animal counts read as a single drive's. Park is now part of the row grain and is carried
// on the row, so the same batch spanning two parks is offered as the two operator days it
// actually is.
func TestVaccinationCommandBoardDriveOptionsCarryPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000d1"
	parkCBE := "70000000-0000-4000-8000-0000010000d1"
	parkCPT := "70000000-0000-4000-8000-0000010000d2"
	shedCBE := "70000000-0000-4000-8000-0000020000d1"
	shedCPT := "70000000-0000-4000-8000-0000020000d2"
	goatCBE := "70000000-0000-4000-8000-0000030000d1"
	goatCPT := "70000000-0000-4000-8000-0000030000d2"
	custodianPartyID := "70000000-0000-4000-8000-00000a0000d1"
	protocolID := "70000000-0000-4000-8000-0000060000d0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000d1"
	ruleID := "70000000-0000-4000-8000-0000070000d1"
	batchID := "70000000-0000-4000-8000-0000040000d1"

	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "park CBE",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CBE', 'park', NULL, 'active')`, parkCBE, tenantID)
	execProjectionSQL(t, ctx, pool, "park CPT",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CPT', 'park', NULL, 'active')`, parkCPT, tenantID)
	execProjectionSQL(t, ctx, pool, "shed CBE",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CBE Shed 1', 'shed', $3, 'active')`, shedCBE, tenantID, parkCBE)
	execProjectionSQL(t, ctx, pool, "shed CPT",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CPT Shed 1', 'shed', $3, 'active')`, shedCPT, tenantID, parkCPT)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian D1', 'active')`,
		custodianPartyID)
	for _, g := range []struct{ id, shed string }{{goatCBE, shedCBE}, {goatCPT, shedCPT}} {
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2024-01-01')`,
			g.id, tenantID, g.shed, custodianPartyID)
	}
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_d1', 'Vaccination D1', 'vaccination', 'active')`, protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`, protocolVersionID, tenantID, protocolID)
	execProjectionSQL(t, ctx, pool, "rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w1', 'birth_age')`, ruleID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	execProjectionSQL(t, ctx, pool, "batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, status, context)
		 VALUES ($1, $2, $3, 'tenant', $2, 'drive:d1', DATE '2026-07-25', 'planned', '{}'::jsonb)`,
		batchID, tenantID, protocolVersionID)

	// ONE batch, same vaccine and window, work in TWO parks.
	for i, o := range []struct{ id, goat, shed, key string }{
		{"70000000-0000-4000-8000-0000080000d1", goatCBE, shedCBE, "drive-park-cbe-d1"},
		{"70000000-0000-4000-8000-0000080000d2", goatCPT, shedCPT, "drive-park-cpt-d1"},
	} {
		execProjectionSQL(t, ctx, pool, "obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, batch_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, $5, 'goat', 'shed', $6, $7, 'scheduled', $8::timestamptz, $9)`,
			o.id, tenantID, batchID, protocolVersionID, o.goat, o.shed, ruleID, asOf, o.key)
		_ = i
	}

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	byPark := map[string]domain.CommandBoardDriveOption{}
	for _, option := range resp.DriveOptions {
		if option.ParkID == "" {
			t.Fatalf("drive option %q carries no park; an all-parks selector cannot tell two parks' drives apart", option.DriveBatchID)
		}
		if _, dup := byPark[option.ParkID]; dup {
			t.Fatalf("two drive options for park %s; the grain is (batch, park)", option.ParkID)
		}
		byPark[option.ParkID] = option
	}
	if len(byPark) != 2 {
		t.Fatalf("drive options = %d, want 2 (one per park); the two parks' work must not collapse into one row", len(byPark))
	}
	for parkID, wantName := range map[string]string{parkCBE: "CBE", parkCPT: "CPT"} {
		option, ok := byPark[parkID]
		if !ok {
			t.Fatalf("no drive option for park %s", parkID)
		}
		if option.ParkName != wantName {
			t.Fatalf("park %s option parkName = %q, want %q", parkID, option.ParkName, wantName)
		}
		if option.TargetCount != 1 {
			t.Fatalf("park %s option targetCount = %d, want 1; counts must be per park, not the batch total", parkID, option.TargetCount)
		}
	}
}
