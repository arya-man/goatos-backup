-- +goose Up
/*
U7 (operational-kernel-5k-50k-scale-envelope ADR): retire the FOUR screen projection
read-models. Their screens now serve canonical indexed SQL at the 5k-50k envelope:
  - process-integrity        (process_integrity_projection_rows/_state)
  - vaccination shed         (vaccination_shed_projection_rows/_state, _shard_state)
  - vaccination execution    (vaccination_execution_projection_rows/_state)
  - vaccination operations   (vaccination_operations_projection_rows/_state)
  - vaccination dirty-scopes  drainer state (vaccination_projection_dirty_scopes)

Deliberately NOT touched here (hard calls):
  - calendar_event_projections is KEPT as a surviving projection (its six-namespace
    canonical read is disproportionately hard; treated like the surviving
    vaccination_eligibility_rollups / counts summaries).
  - The partitioned parents (goat_identity_events, audit_log, obligation_status_events)
    are left partitioned; the partition->plain conversion is deferred (harmless at 50k).

Recoverable via git tag kernel-split-workers-v1.
*/
DROP TABLE IF EXISTS process_integrity_projection_rows CASCADE;
DROP TABLE IF EXISTS process_integrity_projection_state CASCADE;
DROP TABLE IF EXISTS vaccination_shed_projection_rows CASCADE;
DROP TABLE IF EXISTS vaccination_shed_projection_state CASCADE;
DROP TABLE IF EXISTS vaccination_shed_shard_state CASCADE;
DROP TABLE IF EXISTS vaccination_execution_projection_rows CASCADE;
DROP TABLE IF EXISTS vaccination_execution_projection_state CASCADE;
DROP TABLE IF EXISTS vaccination_operations_projection_rows CASCADE;
DROP TABLE IF EXISTS vaccination_operations_projection_state CASCADE;
DROP TABLE IF EXISTS vaccination_projection_dirty_scopes CASCADE;

-- +goose Down
-- Irreversible in place: these four retired read-models are derived from canonical
-- data by their (removed) projectors. Restore the full pre-cutover topology from git
-- tag kernel-split-workers-v1 if a rollback is required. No-op here.
SELECT 1;
