-- +goose Up
-- Tasks owns this event projection. Each new candidate and each once-only decision
-- increments pending + 2*decided, so stale deliveries cannot undo newer state.
CREATE TABLE workflow_animal_purchase_decisions (
    tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
    load_id uuid NOT NULL,
    pending integer NOT NULL CHECK (pending >= 0),
    decided integer NOT NULL CHECK (decided >= 0),
    revision bigint NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, load_id)
);

-- Shared subject-hook receipts retain monotonic business facts until a workflow
-- opens. They are inputs to the existing engine, not a second workflow authority.
CREATE TABLE workflow_subject_hook_receipts (
    tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
    template_key text NOT NULL,
    subject_ref_id uuid NOT NULL,
    hook text NOT NULL,
    completed_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, template_key, subject_ref_id, hook)
);

-- +goose Down
DROP TABLE workflow_subject_hook_receipts;
DROP TABLE workflow_animal_purchase_decisions;
