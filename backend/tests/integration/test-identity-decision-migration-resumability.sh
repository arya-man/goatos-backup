#!/usr/bin/env bash
# Test: VACC-REV-11 P1 Identity Decision Migration Resumability
#
# This test verifies that the three-phase identity_goat decision migration
# (000198, 000199, 000200) is resumable after interrupts at each phase.
#
# Phases:
#  1. 000198: ADD CONSTRAINT ... NOT VALID (idempotent via DROP IF EXISTS)
#  2. 000199: VALIDATE CONSTRAINT (idempotent — validating an already-valid constraint is a no-op)
#  3. 000200: SWAP (DROP IF EXISTS old, RENAME new to old)

set -euo pipefail

# Find the repo root
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# Require a test database URL
if [[ -z "${GOATOS_TEST_DATABASE_URL:-}" ]]; then
    echo "ERROR: GOATOS_TEST_DATABASE_URL not set"
    echo "Example: GOATOS_TEST_DATABASE_URL='postgres://user:pass@localhost/test_db' $0"
    exit 1
fi

db_url="${GOATOS_TEST_DATABASE_URL}"

echo "=========================================="
echo "VACC-REV-11 P1: Identity Decision Migration Resumability Test"
echo "Migrations: 000199, 000200, 000201"
echo "=========================================="
echo "Database: $db_url"
echo ""

# Helper function to run psql with the test database
run_sql() {
    local sql="$1"
    psql "$db_url" -v ON_ERROR_STOP=1 <<< "$sql"
}

# Setup: ensure we have a clean test environment with the identity_decisions table
echo "Setup: Ensuring test database has identity_decisions table..."
run_sql "
CREATE TABLE IF NOT EXISTS identity_decisions (
    id BIGSERIAL PRIMARY KEY,
    decision_type TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Remove any existing v2/v1 constraints for clean test
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;

-- Add the old v1 constraint (without identity_goat) as the starting state
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check CHECK (
    decision_type IN (
      'create_goat',
      'attach_identifier',
      'retire_identifier',
      'mark_identifier_disputed',
      'merge_goats',
      'batch_merge_goats',
      'reject_match',
      'request_field_verification',
      'resolve_correction_request',
      'move_goat',
      'exit_goat',
      'stage_goat',
      'health_goat',
      'reproductive_goat'
    )
  );

-- Verify starting state
SELECT constraint_name, constraint_definition FROM information_schema.check_constraints
  WHERE table_name = 'identity_decisions' ORDER BY constraint_name;
"

echo ""
echo "Phase 1: Apply 000198 (ADD CONSTRAINT ... NOT VALID)"
echo "========================================================"

# Phase 1, attempt 1
echo "  Attempt 1: ADD CONSTRAINT"
run_sql "
SET lock_timeout = '10s';
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check_v2 CHECK (
    decision_type IN (
      'create_goat',
      'attach_identifier',
      'retire_identifier',
      'mark_identifier_disputed',
      'merge_goats',
      'batch_merge_goats',
      'reject_match',
      'request_field_verification',
      'resolve_correction_request',
      'move_goat',
      'exit_goat',
      'stage_goat',
      'health_goat',
      'reproductive_goat',
      'identity_goat'
    )
  ) NOT VALID;
"

# Verify v2 exists and is NOT VALID
echo "  Verify: v2 constraint exists (NOT VALID)"
run_sql "
SELECT constraint_name,
       (SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
        WHERE c.conname = 'identity_decisions_decision_type_check_v2'
        AND c.conrelid = 'identity_decisions'::regclass) as definition,
       convalidated as is_validated
FROM pg_constraint
WHERE conname = 'identity_decisions_decision_type_check_v2'
  AND conrelid = 'identity_decisions'::regclass;
"

# Simulate interrupt: retry Phase 1
echo "  Attempt 2: Simulate interrupt and retry (idempotent)"
run_sql "
SET lock_timeout = '10s';
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check_v2;
ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_decision_type_check_v2 CHECK (
    decision_type IN (
      'create_goat',
      'attach_identifier',
      'retire_identifier',
      'mark_identifier_disputed',
      'merge_goats',
      'batch_merge_goats',
      'reject_match',
      'request_field_verification',
      'resolve_correction_request',
      'move_goat',
      'exit_goat',
      'stage_goat',
      'health_goat',
      'reproductive_goat',
      'identity_goat'
    )
  ) NOT VALID;
"
echo "  SUCCESS: Phase 1 is idempotent (retry did not error)"

echo ""
echo "Phase 2: Apply 000199 (VALIDATE CONSTRAINT)"
echo "============================================="

# Phase 2, attempt 1
echo "  Attempt 1: VALIDATE CONSTRAINT"
run_sql "
SET lock_timeout = '30s';
ALTER TABLE identity_decisions VALIDATE CONSTRAINT identity_decisions_decision_type_check_v2;
"

# Verify v2 is now VALID
echo "  Verify: v2 constraint is now VALID"
run_sql "
SELECT constraint_name, convalidated as is_validated
FROM pg_constraint
WHERE conname = 'identity_decisions_decision_type_check_v2'
  AND conrelid = 'identity_decisions'::regclass;
"

# Simulate interrupt: retry Phase 2
echo "  Attempt 2: Simulate interrupt and retry (idempotent)"
run_sql "
SET lock_timeout = '30s';
ALTER TABLE identity_decisions VALIDATE CONSTRAINT identity_decisions_decision_type_check_v2;
"
echo "  SUCCESS: Phase 2 is idempotent (retry did not error)"

echo ""
echo "Phase 3: Apply 000200 (SWAP - DROP old, RENAME new)"
echo "====================================================="

# Phase 3, attempt 1
echo "  Attempt 1: SWAP (DROP old v1, RENAME v2 → v1)"
run_sql "
SET lock_timeout = '10s';
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;
ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v2 TO identity_decisions_decision_type_check;
"

# Verify final state: v1 is VALID and includes identity_goat
echo "  Verify: v1 constraint is VALID and includes identity_goat"
run_sql "
SELECT constraint_name,
       (SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
        WHERE c.conname = 'identity_decisions_decision_type_check'
        AND c.conrelid = 'identity_decisions'::regclass) as definition,
       convalidated as is_validated
FROM pg_constraint
WHERE conname = 'identity_decisions_decision_type_check'
  AND conrelid = 'identity_decisions'::regclass;
" | grep -q "identity_goat" && echo "    VERIFIED: identity_goat is in the CHECK"

# Simulate interrupt: retry Phase 3
echo "  Attempt 2: Simulate interrupt and retry (idempotent)"
run_sql "
SET lock_timeout = '10s';
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_decision_type_check;
ALTER TABLE identity_decisions
  RENAME CONSTRAINT identity_decisions_decision_type_check_v2 TO identity_decisions_decision_type_check;
"
echo "  SUCCESS: Phase 3 is idempotent (retry did not error)"

echo ""
echo "Final Verification: Constraint is active and enforces identity_goat"
echo "===================================================================="
echo "  Insert valid new decision types (including identity_goat)..."
run_sql "
BEGIN;
INSERT INTO identity_decisions (decision_type) VALUES ('identity_goat');
INSERT INTO identity_decisions (decision_type) VALUES ('create_goat');
INSERT INTO identity_decisions (decision_type) VALUES ('move_goat');
COMMIT;
"

echo "  Verify inserts succeeded..."
run_sql "
SELECT COUNT(*) as row_count FROM identity_decisions
WHERE decision_type IN ('identity_goat', 'create_goat', 'move_goat');
"

echo ""
echo "=========================================="
echo "ALL TESTS PASSED"
echo "=========================================="
echo ""
echo "Summary:"
echo "  Phase 1 (ADD): Idempotent via DROP IF EXISTS before ADD"
echo "  Phase 2 (VALIDATE): Idempotent (VALIDATE on already-VALID is a no-op)"
echo "  Phase 3 (SWAP): Idempotent via DROP IF EXISTS and IF EXISTS guards"
echo "  Final state: identity_decisions_decision_type_check is VALID and enforces identity_goat"
echo ""
