-- +goose Up
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair; no seed data or semantics changed expiry=2026-12-31
-- 000212 rebuilt these append-only tables with CREATE TABLE ... LIKE ... INCLUDING
-- CONSTRAINTS. PostgreSQL does not copy foreign keys with INCLUDING CONSTRAINTS,
-- so restore every FK that existed on the pre-departition parents. The
-- NOT VALID/VALIDATE split keeps the catalog change short and lets validation
-- run under the weaker concurrent-validation lock.

SET lock_timeout = '10s';
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair expiry=2026-12-31
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'goat_identity_events'::regclass AND conname = 'goat_identity_events_tenant_id_fkey') THEN
    ALTER TABLE goat_identity_events
      ADD CONSTRAINT goat_identity_events_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'goat_identity_events'::regclass AND conname = 'goat_identity_events_goat_id_fkey') THEN
    ALTER TABLE goat_identity_events
      ADD CONSTRAINT goat_identity_events_goat_id_fkey
        FOREIGN KEY (goat_id) REFERENCES goats(goat_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'goat_identity_events'::regclass AND conname = 'goat_identity_events_decision_id_fkey') THEN
    ALTER TABLE goat_identity_events
      ADD CONSTRAINT goat_identity_events_decision_id_fkey
        FOREIGN KEY (decision_id) REFERENCES identity_decisions(decision_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'goat_identity_events'::regclass AND conname = 'goat_identity_events_goat_tenant_fk') THEN
    ALTER TABLE goat_identity_events
      ADD CONSTRAINT goat_identity_events_goat_tenant_fk
        FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'goat_identity_events'::regclass AND conname = 'goat_identity_events_decision_tenant_fk') THEN
    ALTER TABLE goat_identity_events
      ADD CONSTRAINT goat_identity_events_decision_tenant_fk
        FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) NOT VALID;
  END IF;
END
$$;

SET lock_timeout = '10s';
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair expiry=2026-12-31
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'audit_log'::regclass AND conname = 'audit_log_tenant_id_fkey') THEN
    ALTER TABLE audit_log
      ADD CONSTRAINT audit_log_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'audit_log'::regclass AND conname = 'audit_log_decision_id_fkey') THEN
    ALTER TABLE audit_log
      ADD CONSTRAINT audit_log_decision_id_fkey
        FOREIGN KEY (decision_id) REFERENCES identity_decisions(decision_id) NOT VALID;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'audit_log'::regclass AND conname = 'audit_log_decision_tenant_fk') THEN
    ALTER TABLE audit_log
      ADD CONSTRAINT audit_log_decision_tenant_fk
        FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) NOT VALID;
  END IF;
END
$$;

SET lock_timeout = '10s';
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair expiry=2026-12-31
ALTER TABLE goat_identity_events
  VALIDATE CONSTRAINT goat_identity_events_tenant_id_fkey,
  VALIDATE CONSTRAINT goat_identity_events_goat_id_fkey,
  VALIDATE CONSTRAINT goat_identity_events_decision_id_fkey,
  VALIDATE CONSTRAINT goat_identity_events_goat_tenant_fk,
  VALIDATE CONSTRAINT goat_identity_events_decision_tenant_fk;
SET lock_timeout = '10s';
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair expiry=2026-12-31
ALTER TABLE audit_log
  VALIDATE CONSTRAINT audit_log_tenant_id_fkey,
  VALIDATE CONSTRAINT audit_log_decision_id_fkey,
  VALIDATE CONSTRAINT audit_log_decision_tenant_fk;

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '10s';
ALTER TABLE audit_log
  DROP CONSTRAINT IF EXISTS audit_log_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS audit_log_decision_id_fkey,
  DROP CONSTRAINT IF EXISTS audit_log_tenant_id_fkey;
SET lock_timeout = '10s';
-- seed-migration-guard:ignore owner=ravi issue=GOS-REV-01 reason=foreign-key-only repair expiry=2026-12-31
ALTER TABLE goat_identity_events
  DROP CONSTRAINT IF EXISTS goat_identity_events_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_events_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_events_decision_id_fkey,
  DROP CONSTRAINT IF EXISTS goat_identity_events_goat_id_fkey,
  DROP CONSTRAINT IF EXISTS goat_identity_events_tenant_id_fkey;
