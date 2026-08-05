#!/bin/bash

set -e

TENANT_ID="00000000-0000-4000-8000-000000000001"
PARK_ID="00000000-0000-4000-8000-000000000002"
OPERATOR_ID="00000000-0000-4000-8000-000000000003"
VERIFIER_ID="00000000-0000-4000-8000-000000000004"

# Read auth tokens
OPERATOR_TOKEN=$(cat /tmp/tok-202.txt 2>/dev/null | head -1 || echo "MISSING")
VERIFIER_TOKEN=$(cat /tmp/tok-104.txt 2>/dev/null | head -1 || echo "MISSING")
CEO_TOKEN=$(cat /tmp/tok-101.txt 2>/dev/null | head -1 || echo "MISSING")

API="http://127.0.0.1:8090"

echo "=== Attack Vector A: Double-Dose Window ==="
echo "Testing that a recorded obligation closes and re-reject reopens..."

# Create a simple test to verify obligation status flow
PGPASSWORD=goatos psql -h 127.0.0.1 -p 15546 -U postgres -d goatos << 'SQL'
-- Clear any test data first
DELETE FROM vaccination_completions WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
DELETE FROM obligation_instances WHERE tenant_id = '00000000-0000-4000-8000-000000000001';

-- Create a test goat
INSERT INTO herd_animals (animal_id, tenant_id, legacy_id, rfid_tag, birth_date_hint, arrival_date, species)
VALUES ('00000000-1111-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'TEST001', 'TEST-RFID-001', 'age', now()::date, 'goat')
ON CONFLICT DO NOTHING;

-- Create a test obligation
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
  target_type, target_id, scope_type, scope_id, due_at, 
  window_start, window_end, status, sop_task_id, idempotency_key, row_version
) VALUES (
  '00000000-2222-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000000001',
  '00000000-3333-4000-8000-000000000001',
  'herd_animal', '00000000-1111-4000-8000-000000000001',
  'park', '00000000-0000-4000-8000-000000000002',
  now()::timestamp with time zone,
  now()::timestamp with time zone,
  (now() + interval '7 days')::timestamp with time zone,
  'due',
  NULL,
  'test-ob:1',
  1
)
ON CONFLICT DO NOTHING;

SELECT 'Test Setup Complete' as status;
SELECT obligation_id, status FROM obligation_instances WHERE obligation_id = '00000000-2222-4000-8000-000000000001';
SQL
