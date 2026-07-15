-- +goose Up
-- Persisted source-fact lineage ledger (VACC-REV-01/02). One row per DATED source vaccination cell
-- (a cell that is neither blank/NA nor "Pending"). Each fact carries a stable lineage_key = the
-- source-cell identity (animal_key | vaccine_header | dose_code | sequence | source_value), so the
-- seed can prove every dated source fact is accounted for EXACTLY ONCE by source identity/lineage --
-- never by summing accepted-completion counts and completed-obligation counts (the abandoned
-- verifyCommittedDatedFacts double-counted 2x per past fact).
--
-- disposition records how the fact landed. obligation_idem / completion_idem reference the COMMITTED
-- obligation_instances / vaccination_completions rows the in-transaction verify checks against: because
-- obligation/completion inserts use ON CONFLICT DO NOTHING, a silent collision would leave the report
-- reading N/N while a row was discarded. The in-txn verify compares each non-excluded fact to the rows
-- actually persisted in the same transaction, so a drop rolls the whole seed transaction back before
-- commit.
CREATE TABLE vaccination_source_facts (
  source_fact_id  uuid PRIMARY KEY,
  tenant_id       uuid NOT NULL,
  seed_run_id     uuid,
  lineage_key     text NOT NULL,
  animal_key      text NOT NULL,
  vaccine_header  text NOT NULL,
  dose_code       text NOT NULL,
  sequence        integer NOT NULL,
  source_value    text NOT NULL,
  source_date     date,
  disposition     text NOT NULL,
  obligation_idem text,
  completion_idem text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_source_facts_disposition_check CHECK (disposition IN (
    'imported_completion',
    'scheduled_obligation',
    'later_administration_merge',
    'excluded_goat_not_placed',
    'excluded_vaccine_unrecognized',
    'excluded_lifecycle',
    'unresolved')),
  CONSTRAINT vaccination_source_facts_lineage_unique UNIQUE (tenant_id, lineage_key)
);

CREATE INDEX vaccination_source_facts_tenant_disposition_idx
  ON vaccination_source_facts (tenant_id, disposition);

-- +goose Down
DROP TABLE vaccination_source_facts;
