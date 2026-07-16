-- +goose Up
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01-FOLLOWUP reason=foreign-key-only zero-drift repair; no seed data or semantics changed expiry=2026-12-31
-- 000212 rebuilt obligation_status_events with CREATE TABLE ... LIKE ... INCLUDING
-- CONSTRAINTS. PostgreSQL does not copy foreign keys with INCLUDING CONSTRAINTS.
-- 000212 restored the composite obligation FK; this follow-up restores the
-- direct tenant FK from the original partitioned parent for exact baseline drift
-- closure. The composite FK already enforced practical tenant integrity.

SET lock_timeout = '10s';
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'obligation_status_events'::regclass AND conname = 'obligation_status_events_tenant_id_fkey') THEN
    ALTER TABLE obligation_status_events
      ADD CONSTRAINT obligation_status_events_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) NOT VALID;
  END IF;
END
$$;

SET lock_timeout = '10s';
ALTER TABLE obligation_status_events
  VALIDATE CONSTRAINT obligation_status_events_tenant_id_fkey;

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '10s';
ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_tenant_id_fkey;
