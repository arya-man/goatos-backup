#!/bin/bash
set -e

# Comprehensive test for weighing repair migrations 000069-000071
# Seeds defects, applies migrations via goatos_schema_migrations, shows before/after
# Run from repo root: bash backend/tests/weighing_repair_migrations_apply.sh

QA_DB="postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable"

echo "=== WEIGHING REPAIR MIGRATIONS TEST ==="
echo "Database: $QA_DB"
echo ""

# ============================================================================
# B11: Missing work items
# ============================================================================
echo ">>> B11 TEST: Pre-existing campaigns never enter the kernel"
echo ""

# Seed: Create campaign and shed WITHOUT work_item
CAMPAIGN_ID="91000000-0000-0000-0000-000000000001"
SHED_ID="92000000-0000-0000-0000-000000000001"

PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos << 'SQL'
DELETE FROM weighing_work_items WHERE campaign_shed_id = '92000000-0000-0000-0000-000000000001';
DELETE FROM weighing_campaign_sheds WHERE campaign_shed_id = '92000000-0000-0000-0000-000000000001';
DELETE FROM weighing_campaigns WHERE campaign_id = '91000000-0000-0000-0000-000000000001';

INSERT INTO weighing_campaigns (tenant_id, campaign_id, park_id, display_name, start_business_date, planned_cap_per_day, status)
VALUES ('10000000-0000-0000-0000-000000000001', '91000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'Test Campaign B11', '2026-08-01', 10, 'published');

INSERT INTO weighing_campaign_sheds (tenant_id, campaign_id, campaign_shed_id, location_id, operator_user_id, display_name, weighing_category, expected_animal_count, status)
VALUES ('10000000-0000-0000-0000-000000000001', '91000000-0000-0000-0000-000000000001', '92000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'Test Shed B11', 'per_shed_partition', 5, 'pending');
SQL

# BEFORE: Count work items
BEFORE_B11=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT COUNT(*) FROM weighing_work_items WHERE campaign_shed_id = '92000000-0000-0000-0000-000000000001';")
echo "BEFORE B11: $BEFORE_B11 work items (expected 0)"

# Apply migration 000069
echo "Applying 000069_weighing_backfill_missing_work_items.sql..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000069_weighing_backfill_missing_work_items.sql

# AFTER: Count work items
AFTER_B11=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT COUNT(*) FROM weighing_work_items WHERE campaign_shed_id = '92000000-0000-0000-0000-000000000001';")
echo "AFTER B11: $AFTER_B11 work items (expected 1)"

# IDEMPOTENCY: Run again
echo "Testing B11 idempotency (running migration again)..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000069_weighing_backfill_missing_work_items.sql
IDEMPOTENT_B11=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT COUNT(*) FROM weighing_work_items WHERE campaign_shed_id = '92000000-0000-0000-0000-000000000001';")
echo "IDEMPOTENCY B11: $IDEMPOTENT_B11 work items (expected 1, no duplicates)"
echo ""

# ============================================================================
# B02: Silent overwrite via NULL submitted_at
# ============================================================================
echo ">>> B02 TEST: Historical evidence can be silently overwritten"
echo ""

CAMPAIGN_ID_B02="93000000-0000-0000-0000-000000000002"
SHED_ID_B02="94000000-0000-0000-0000-000000000002"
OBS_ID_B02="95000000-0000-0000-0000-000000000002"

# Seed: Create campaign, shed, accept observation, complete, then REOPEN
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos << SQL
DELETE FROM audit_log WHERE resource_id = '$OBS_ID_B02';
DELETE FROM weighing_observations WHERE observation_id = '$OBS_ID_B02';
DELETE FROM weighing_campaign_sheds WHERE campaign_shed_id = '$SHED_ID_B02';
DELETE FROM weighing_campaigns WHERE campaign_id = '$CAMPAIGN_ID_B02';

INSERT INTO weighing_campaigns (tenant_id, campaign_id, park_id, display_name, start_business_date, planned_cap_per_day, status)
VALUES ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B02', '20000000-0000-0000-0000-000000000001', 'Test Campaign B02', '2026-08-01', 10, 'published');

INSERT INTO weighing_campaign_sheds (tenant_id, campaign_id, campaign_shed_id, location_id, operator_user_id, display_name, weighing_category, expected_animal_count, status)
VALUES ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B02', '$SHED_ID_B02', '30000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'Test Shed B02', 'per_shed_partition', 5, 'in_progress');

INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, observation_id, scanned_identifier, weight_kg, proof_artifact_id, accepted_at, submitted_at)
VALUES ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B02', '$SHED_ID_B02', '$OBS_ID_B02', 'goat-123', 25.5, '00000000-0000-0000-0000-000000000001', NOW(), NULL);

INSERT INTO audit_log (tenant_id, actor_id, action, resource_type, resource_id, metadata, created_at)
VALUES ('10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'weighing.observation_accepted', 'weighing_observation', '$OBS_ID_B02', '{}', NOW());

UPDATE weighing_campaign_sheds SET status = 'completed', completed_at = NOW() WHERE campaign_shed_id = '$SHED_ID_B02';

-- REOPEN: status back to in_progress, completed_at back to NULL
UPDATE weighing_campaign_sheds SET status = 'in_progress', completed_at = NULL WHERE campaign_shed_id = '$SHED_ID_B02';
SQL

# BEFORE: Check submitted_at IS NULL
BEFORE_B02=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT submitted_at FROM weighing_observations WHERE observation_id = '$OBS_ID_B02';")
echo "BEFORE B02: submitted_at = '$BEFORE_B02' (expected NULL)"

# Apply migration 000070
echo "Applying 000070_weighing_backfill_submitted_at_from_audit.sql..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000070_weighing_backfill_submitted_at_from_audit.sql

# AFTER: Check submitted_at is stamped
AFTER_B02=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT submitted_at FROM weighing_observations WHERE observation_id = '$OBS_ID_B02';")
echo "AFTER B02: submitted_at = '$AFTER_B02' (expected NON-NULL)"

# IDEMPOTENCY: Run again
echo "Testing B02 idempotency (running migration again)..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000070_weighing_backfill_submitted_at_from_audit.sql
IDEMPOTENT_B02=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "SELECT submitted_at FROM weighing_observations WHERE observation_id = '$OBS_ID_B02';")
echo "IDEMPOTENCY B02: submitted_at = '$IDEMPOTENT_B02' (expected same as AFTER)"
echo ""

# ============================================================================
# B03: Pending verdicts from orphaned verification_items
# ============================================================================
echo ">>> B03 TEST: Historical verification verdicts reset to pending"
echo ""

CAMPAIGN_ID_B03="96000000-0000-0000-0000-000000000003"
SHED_ID_B03="97000000-0000-0000-0000-000000000003"
OBS_ID1_B03="98000000-0000-0000-0000-000000000031"
OBS_ID2_B03="98000000-0000-0000-0000-000000000032"
OBS_ID3_B03="98000000-0000-0000-0000-000000000033"

# Seed: Create observations with verification_items but default pending status
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos << SQL
DELETE FROM verification_items WHERE source_ref_id IN ('$OBS_ID1_B03', '$OBS_ID2_B03', '$OBS_ID3_B03');
DELETE FROM weighing_observations WHERE observation_id IN ('$OBS_ID1_B03', '$OBS_ID2_B03', '$OBS_ID3_B03');
DELETE FROM weighing_campaign_sheds WHERE campaign_shed_id = '$SHED_ID_B03';
DELETE FROM weighing_campaigns WHERE campaign_id = '$CAMPAIGN_ID_B03';

INSERT INTO weighing_campaigns (tenant_id, campaign_id, park_id, display_name, start_business_date, planned_cap_per_day, status)
VALUES ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B03', '20000000-0000-0000-0000-000000000001', 'Test Campaign B03', '2026-08-01', 10, 'published');

INSERT INTO weighing_campaign_sheds (tenant_id, campaign_id, campaign_shed_id, location_id, operator_user_id, display_name, weighing_category, expected_animal_count, status)
VALUES ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B03', '$SHED_ID_B03', '30000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'Test Shed B03', 'per_shed_partition', 5, 'in_progress');

INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, observation_id, scanned_identifier, weight_kg, proof_artifact_id, accepted_at, verification_status)
VALUES
  ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B03', '$SHED_ID_B03', '$OBS_ID1_B03', 'goat-1', 25.5, '00000000-0000-0000-0000-000000000001', NOW(), 'pending'),
  ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B03', '$SHED_ID_B03', '$OBS_ID2_B03', 'goat-2', 26.0, '00000000-0000-0000-0000-000000000001', NOW(), 'pending'),
  ('10000000-0000-0000-0000-000000000001', '$CAMPAIGN_ID_B03', '$SHED_ID_B03', '$OBS_ID3_B03', 'goat-3', 27.0, '00000000-0000-0000-0000-000000000001', NOW(), 'pending');

INSERT INTO verification_items (tenant_id, item_id, vertical, module, category, source_ref_id, status, verified_by, verified_at)
VALUES ('10000000-0000-0000-0000-000000000001', gen_random_uuid(), 'weighing', 'weighing', 'weighing_proof', '$OBS_ID1_B03', 'approved', '50000000-0000-0000-0000-000000000001', NOW());

INSERT INTO verification_items (tenant_id, item_id, vertical, module, category, source_ref_id, status, verdict_reason, verified_by, verified_at)
VALUES ('10000000-0000-0000-0000-000000000001', gen_random_uuid(), 'weighing', 'weighing', 'weighing_proof', '$OBS_ID2_B03', 'rework', 'Image unclear', '50000000-0000-0000-0000-000000000001', NOW());
SQL

# BEFORE: Count pending verdicts
BEFORE_B03=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "
SELECT COUNT(*) FROM verification_items vi
JOIN weighing_observations wo ON wo.observation_id = vi.source_ref_id
WHERE vi.category = 'weighing_proof'
  AND vi.status IN ('approved', 'rework')
  AND wo.verification_status = 'pending';
")
echo "BEFORE B03: $BEFORE_B03 orphaned verdicts (expected 2)"

# Apply migration 000071
echo "Applying 000071_weighing_backfill_verification_status_from_items.sql..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000071_weighing_backfill_verification_status_from_items.sql

# AFTER: Count fixed verdicts
AFTER_B03=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "
SELECT COUNT(*) FROM verification_items vi
JOIN weighing_observations wo ON wo.observation_id = vi.source_ref_id
WHERE vi.category = 'weighing_proof'
  AND vi.status IN ('approved', 'rework')
  AND wo.verification_status = 'pending';
")
echo "AFTER B03: $AFTER_B03 orphaned verdicts (expected 0)"

# Verify mapping: approved -> verified, rework -> rework
VERIFIED=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "
SELECT COUNT(*) FROM verification_items vi
JOIN weighing_observations wo ON wo.observation_id = vi.source_ref_id
WHERE vi.category = 'weighing_proof'
  AND vi.status = 'approved'
  AND wo.verification_status = 'verified';
")
REWORK=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "
SELECT COUNT(*) FROM verification_items vi
JOIN weighing_observations wo ON wo.observation_id = vi.source_ref_id
WHERE vi.category = 'weighing_proof'
  AND vi.status = 'rework'
  AND wo.verification_status = 'rework';
")
echo "Mapping verification: approved->verified=$VERIFIED (expected 1), rework->rework=$REWORK (expected 1)"

# IDEMPOTENCY: Run again
echo "Testing B03 idempotency (running migration again)..."
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -f backend/migrations/postgres/000071_weighing_backfill_verification_status_from_items.sql
IDEMPOTENT_B03=$(PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -t -c "
SELECT COUNT(*) FROM verification_items vi
JOIN weighing_observations wo ON wo.observation_id = vi.source_ref_id
WHERE vi.category = 'weighing_proof'
  AND vi.status IN ('approved', 'rework')
  AND wo.verification_status = 'pending';
")
echo "IDEMPOTENCY B03: $IDEMPOTENT_B03 orphaned verdicts (expected 0, no re-runs)"
echo ""

# ============================================================================
# Record migrations in goatos_schema_migrations
# ============================================================================
echo ">>> Recording migrations in goatos_schema_migrations"
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos << 'SQL'
INSERT INTO goatos_schema_migrations (version, filename, checksum, applied_at)
VALUES
  ('000069_weighing_backfill_missing_work_items', '000069_weighing_backfill_missing_work_items.sql', 'sha256:manually-applied-for-qa-testing', NOW()),
  ('000070_weighing_backfill_submitted_at_from_audit', '000070_weighing_backfill_submitted_at_from_audit.sql', 'sha256:manually-applied-for-qa-testing', NOW()),
  ('000071_weighing_backfill_verification_status_from_items', '000071_weighing_backfill_verification_status_from_items.sql', 'sha256:manually-applied-for-qa-testing', NOW())
ON CONFLICT DO NOTHING;
SQL

# Show applied versions
echo "Current applied migrations (last 5):"
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15544 -U postgres -d goatos -c "SELECT version FROM goatos_schema_migrations ORDER BY version DESC LIMIT 5;"

echo ""
echo "=== ALL TESTS COMPLETE ==="
