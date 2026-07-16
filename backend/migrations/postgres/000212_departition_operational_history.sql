-- +goose Up
-- The accepted 5k-50k scale envelope does not need monthly partition
-- maintenance. Rebuild the three append-only parents as ordinary indexed
-- tables while preserving any rows present in a non-production environment.

DROP FUNCTION IF EXISTS goatos_ensure_partition_coverage(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_assert_partition_coverage(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_partition_coverage_report(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_ensure_monthly_partitions(regclass, text, timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_month_partition_name(text, date);
DROP FUNCTION IF EXISTS goatos_partition_parent_specs();

ALTER TABLE identity_decision_events
  DROP CONSTRAINT IF EXISTS identity_decision_events_event_fk;
ALTER TABLE identity_decision_events
  DROP CONSTRAINT IF EXISTS identity_decision_events_event_tenant_fk;

ALTER TABLE goat_identity_events RENAME TO goat_identity_events_partitioned_old;
CREATE TABLE goat_identity_events (
  LIKE goat_identity_events_partitioned_old
    INCLUDING DEFAULTS
    INCLUDING GENERATED
    INCLUDING IDENTITY
    INCLUDING STORAGE
    INCLUDING COMMENTS
    INCLUDING CONSTRAINTS
);
INSERT INTO goat_identity_events SELECT * FROM goat_identity_events_partitioned_old;
DROP TABLE goat_identity_events_partitioned_old CASCADE;

ALTER TABLE goat_identity_events
  ADD CONSTRAINT goat_identity_events_pkey PRIMARY KEY (identity_event_id, recorded_at);
ALTER TABLE goat_identity_events
  ADD CONSTRAINT goat_identity_events_tenant_event_recorded_unique
    UNIQUE (tenant_id, identity_event_id, recorded_at);
CREATE INDEX goat_identity_events_goat_timeline_idx
  ON goat_identity_events(goat_id, occurred_at DESC);
CREATE INDEX goat_identity_events_tenant_type_recorded_idx
  ON goat_identity_events(tenant_id, event_type, recorded_at DESC);
CREATE INDEX goat_identity_events_idempotency_idx
  ON goat_identity_events(idempotency_key);
CREATE INDEX goat_identity_events_tenant_recorded_at_idx
  ON goat_identity_events(tenant_id, recorded_at DESC);
CREATE INDEX goat_identity_events_tenant_recorded_event_idx
  ON goat_identity_events(tenant_id, recorded_at, identity_event_id);

CREATE TRIGGER goat_identity_events_block_merged_goat_child_write_trg
  BEFORE INSERT OR UPDATE ON goat_identity_events
  FOR EACH ROW EXECUTE FUNCTION block_merged_goat_child_write();

ALTER TABLE identity_decision_events
  ADD CONSTRAINT identity_decision_events_event_fk
    FOREIGN KEY (event_id, event_recorded_at)
    REFERENCES goat_identity_events(identity_event_id, recorded_at);
ALTER TABLE identity_decision_events
  ADD CONSTRAINT identity_decision_events_event_tenant_fk
    FOREIGN KEY (tenant_id, event_id, event_recorded_at)
    REFERENCES goat_identity_events(tenant_id, identity_event_id, recorded_at);

ALTER TABLE audit_log RENAME TO audit_log_partitioned_old;
CREATE TABLE audit_log (
  LIKE audit_log_partitioned_old
    INCLUDING DEFAULTS
    INCLUDING GENERATED
    INCLUDING IDENTITY
    INCLUDING STORAGE
    INCLUDING COMMENTS
    INCLUDING CONSTRAINTS
);
INSERT INTO audit_log SELECT * FROM audit_log_partitioned_old;
DROP TABLE audit_log_partitioned_old CASCADE;

ALTER TABLE audit_log
  ADD CONSTRAINT audit_log_pkey PRIMARY KEY (audit_id, recorded_at);
CREATE INDEX audit_log_resource_idx
  ON audit_log(resource_type, resource_id, created_at DESC);
CREATE INDEX audit_log_actor_idx
  ON audit_log(actor_id, created_at DESC);
CREATE INDEX audit_log_tenant_action_idx
  ON audit_log(tenant_id, action, recorded_at DESC);
CREATE INDEX audit_log_tenant_recorded_idx
  ON audit_log(tenant_id, recorded_at DESC, audit_id DESC);
CREATE INDEX audit_log_tenant_resource_recorded_idx
  ON audit_log(tenant_id, resource_type, resource_id, recorded_at DESC);
CREATE INDEX audit_log_tenant_actor_recorded_idx
  ON audit_log(tenant_id, actor_id, recorded_at DESC);
CREATE INDEX audit_log_tenant_scope_recorded_idx
  ON audit_log(tenant_id, scope_type, scope_id, recorded_at DESC);
CREATE INDEX audit_log_tenant_actor_type_recorded_idx
  ON audit_log(tenant_id, actor_type, recorded_at DESC, audit_id DESC);
CREATE INDEX audit_log_tenant_domain_module_category_recorded_idx
  ON audit_log(tenant_id, (metadata->>'domain'), (metadata->>'module'),
    (metadata->>'category'), recorded_at DESC, audit_id DESC);
CREATE INDEX audit_log_tenant_status_recorded_idx
  ON audit_log(tenant_id, (metadata->>'status'), recorded_at DESC, audit_id DESC);
CREATE INDEX audit_log_tenant_result_recorded_idx
  ON audit_log(tenant_id, (metadata->>'result'), recorded_at DESC, audit_id DESC);
CREATE INDEX audit_log_tenant_calendar_event_recorded_idx
  ON audit_log(tenant_id, (metadata->>'calendar_event_id'), recorded_at DESC, audit_id DESC)
  WHERE metadata ? 'calendar_event_id';

ALTER TABLE obligation_status_events RENAME TO obligation_status_events_partitioned_old;
CREATE TABLE obligation_status_events (
  LIKE obligation_status_events_partitioned_old
    INCLUDING DEFAULTS
    INCLUDING GENERATED
    INCLUDING IDENTITY
    INCLUDING STORAGE
    INCLUDING COMMENTS
    INCLUDING CONSTRAINTS
);
INSERT INTO obligation_status_events SELECT * FROM obligation_status_events_partitioned_old;
DROP TABLE obligation_status_events_partitioned_old CASCADE;

ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_pkey
    PRIMARY KEY (obligation_event_id, recorded_at);
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_obligation_tenant_fk
    FOREIGN KEY (tenant_id, obligation_id)
    REFERENCES obligation_instances(tenant_id, obligation_id);
CREATE INDEX obligation_status_events_obligation_idx
  ON obligation_status_events(obligation_id, occurred_at DESC);
CREATE INDEX obligation_status_events_tenant_type_recorded_idx
  ON obligation_status_events(tenant_id, event_type, recorded_at DESC);
CREATE INDEX obligation_status_events_idempotency_idx
  ON obligation_status_events(tenant_id, idempotency_key);

-- +goose Down
-- This is an intentional non-production topology cutover. Restoring the old
-- monthly partition fleet requires restoring the pre-cutover database backup.
DO $$
BEGIN
  RAISE EXCEPTION '000212 is irreversible; restore the pre-cutover backup instead';
END
$$;
