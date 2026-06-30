-- +goose Up
-- Feed Direction G2 closure evidence: keep every readiness proof write
-- auditable instead of letting the current subgate row overwrite prior CSG10
-- mapping/source-scan/parity/query-plan/import/recompute evidence pointers.

CREATE TABLE counts_shifting_readiness_evidence (
  counts_shifting_readiness_evidence_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  subgate_id text NOT NULL,
  status text NOT NULL,
  evidence_ref text NOT NULL,
  blocker_reason text NOT NULL,
  implementation_ref text NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT counts_shifting_readiness_evidence_subgate_id_check CHECK (subgate_id IN ('CSG1','CSG2','CSG3','CSG4','CSG5','CSG6','CSG7','CSG8','CSG9','CSG10')),
  CONSTRAINT counts_shifting_readiness_evidence_status_check CHECK (status IN ('ready', 'blocked', 'pending')),
  CONSTRAINT counts_shifting_readiness_evidence_ref_check CHECK (btrim(evidence_ref) <> ''),
  CONSTRAINT counts_shifting_readiness_evidence_blocker_check CHECK (btrim(blocker_reason) <> '')
);

CREATE INDEX counts_shifting_readiness_evidence_subgate_idx
  ON counts_shifting_readiness_evidence (tenant_id, subgate_id, recorded_at DESC);

INSERT INTO counts_shifting_readiness_evidence (
  tenant_id, subgate_id, status, evidence_ref, blocker_reason, implementation_ref, recorded_at
)
SELECT tenant_id, subgate_id, status, evidence_ref, blocker_reason, implementation_ref, last_checked_at
FROM counts_shifting_readiness_subgates;

CREATE OR REPLACE FUNCTION record_counts_shifting_readiness_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  INSERT INTO counts_shifting_readiness_evidence (
    tenant_id, subgate_id, status, evidence_ref, blocker_reason, implementation_ref, recorded_at
  ) VALUES (
    NEW.tenant_id, NEW.subgate_id, NEW.status, NEW.evidence_ref,
    NEW.blocker_reason, NEW.implementation_ref, now()
  );
  RETURN NEW;
END;
$$;

CREATE TRIGGER counts_shifting_readiness_evidence_after_write
AFTER INSERT OR UPDATE ON counts_shifting_readiness_subgates
FOR EACH ROW
EXECUTE FUNCTION record_counts_shifting_readiness_evidence();

-- +goose Down
DROP TRIGGER IF EXISTS counts_shifting_readiness_evidence_after_write ON counts_shifting_readiness_subgates;
DROP FUNCTION IF EXISTS record_counts_shifting_readiness_evidence();
DROP TABLE IF EXISTS counts_shifting_readiness_evidence;
