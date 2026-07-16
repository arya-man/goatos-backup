package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const shedPositionTenant = "de000000-0000-4000-8000-000000000001"

func TestApplyMappingFillsMissingShedsFromParkPreventiveCareManager(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	for _, stmt := range []string{
		`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'shed-position-test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES
  ('de100000-0000-4000-8000-000000000001', $1::uuid, 'park', 'TEST_CBE', 'Test CBE', 'active'),
  ('de100000-0000-4000-8000-000000000002', $1::uuid, 'park', 'TEST_CPT', 'Test CPT', 'active')
ON CONFLICT (tenant_id, location_code) DO NOTHING`,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status) VALUES
  ('de200000-0000-4000-8000-000000000001', $1::uuid, 'shed', 'TEST_CBE_A', 'CBE A', 'de100000-0000-4000-8000-000000000001', 'active'),
  ('de200000-0000-4000-8000-000000000002', $1::uuid, 'shed', 'TEST_CBE_B', 'CBE B', 'de100000-0000-4000-8000-000000000001', 'active'),
  ('de200000-0000-4000-8000-000000000003', $1::uuid, 'shed', 'TEST_CPT_A', 'CPT A', 'de100000-0000-4000-8000-000000000002', 'active')
ON CONFLICT (tenant_id, location_code) DO NOTHING`,
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint) VALUES
  ('de300000-0000-4000-8000-000000000001', $1::uuid, 'TEST-CBE-PC', 'CBE Preventive Manager', 'active', 'other'),
  ('de300000-0000-4000-8000-000000000002', $1::uuid, 'TEST-CPT-PC', 'CPT Preventive Manager', 'active', 'other'),
  ('de300000-0000-4000-8000-000000000003', $1::uuid, 'TEST-CBE-BACKUP', 'CBE Backup Manager', 'active', 'other'),
  ('de300000-0000-4000-8000-000000000004', $1::uuid, 'TEST-CPT-BACKUP', 'CPT Backup Manager', 'active', 'other')
ON CONFLICT (tenant_id, display_code) DO NOTHING`,
		`INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, is_backup_slot, backup_group_code, status, valid_from) VALUES
  ('de400000-0000-4000-8000-000000000001', $1::uuid, 'de300000-0000-4000-8000-000000000001', 'center', 'de100000-0000-4000-8000-000000000001', 'preventive_care_manager', 'manager', false, 'manager_backup', 'active', now()),
  ('de400000-0000-4000-8000-000000000002', $1::uuid, 'de300000-0000-4000-8000-000000000002', 'center', 'de100000-0000-4000-8000-000000000002', 'preventive_care_manager', 'manager', false, 'manager_backup', 'active', now()),
  ('de400000-0000-4000-8000-000000000003', $1::uuid, 'de300000-0000-4000-8000-000000000003', 'center', 'de100000-0000-4000-8000-000000000001', 'backup_manager', 'manager', true, 'manager_backup', 'active', now()),
  ('de400000-0000-4000-8000-000000000004', $1::uuid, 'de300000-0000-4000-8000-000000000004', 'center', 'de100000-0000-4000-8000-000000000002', 'backup_manager', 'manager', true, 'manager_backup', 'active', now())
ON CONFLICT (position_id) DO NOTHING`,
	} {
		if _, err := pool.Exec(ctx, stmt, shedPositionTenant); err != nil {
			t.Fatalf("seed test tenant: %v", err)
		}
	}

	mappingPath := filepath.Join(t.TempDir(), "partial-shed-manager.csv")
	if err := os.WriteFile(mappingPath, []byte(`shed_code,shed_name,park_code,manager_code,manager_name,assignment_source,source_ref,confidence,needs_review
TEST_CBE_A,CBE A,TEST_CBE,TEST-CBE-PC,CBE Preventive Manager,reviewed,test-reviewed-source,reviewed_source_overlay,false
`), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	st, err := applyMapping(ctx, pool, shedPositionTenant, mappingPath, true)
	if err != nil {
		t.Fatalf("applyMapping: %v", err)
	}
	if st.StrictBlocked {
		t.Fatalf("strict blocked despite park preventive-care fallback: %+v", st)
	}
	if st.ReviewedSeats != 1 || st.ParkFallbackSeats != 2 || st.UnmappedGaps != 0 || st.BackupGaps != 0 || st.SeatsInserted != 3 {
		t.Fatalf("stats = %+v, want reviewed=1 fallback=2 gaps=0 backup_gaps=0 seats=3", st)
	}

	var missingManagers, missingBackups int
	if err := pool.QueryRow(ctx, `
WITH active_sheds AS (
  SELECT s.location_id, p.location_id AS park_id
  FROM locations s
  JOIN locations p ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
  WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed' AND s.status = 'active'
)
SELECT
  count(*) FILTER (WHERE mgr.position_id IS NULL)::int,
  count(*) FILTER (WHERE backup.position_id IS NULL)::int
FROM active_sheds s
LEFT JOIN workforce_positions mgr
  ON mgr.tenant_id = $1::uuid
 AND mgr.scope_type = 'shed'
 AND mgr.scope_id = s.location_id
 AND mgr.position_code = 'shed_manager'
 AND mgr.status = 'active'
LEFT JOIN workforce_positions backup
  ON backup.tenant_id = $1::uuid
 AND backup.scope_type = 'center'
 AND backup.scope_id = s.park_id
 AND backup.position_code = 'backup_manager'
 AND backup.status = 'active'`, shedPositionTenant).Scan(&missingManagers, &missingBackups); err != nil {
		t.Fatalf("query gaps: %v", err)
	}
	if missingManagers != 0 || missingBackups != 0 {
		t.Fatalf("missingManagers=%d missingBackups=%d, want zero", missingManagers, missingBackups)
	}
}
