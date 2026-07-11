-- +goose Up
-- position_module_duties: the normalized "what work does this position do" model (S4.6 duties dimension).
-- Replaces the single hardcoded positionExecuteCapability Go map and unifies THREE previously-disconnected
-- vocabularies: roster position_code, operational module_code, and the calendar executor role (e.g.
-- pc_vaccinator). A position holding (duty_type='execute', module_code='pc.vaccination') IS a vaccination
-- executor; capability_code carries the execution permission a temporary backup grant confers. Effective-
-- dated so duties change over time without losing history. Tenant-scoped. Small config table (few
-- positions) -- no herd-scale concern, but indexed for the two hot reads.
CREATE TABLE IF NOT EXISTS position_module_duties (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    position_code     text NOT NULL,
    module_code       text NOT NULL,
    duty_type         text NOT NULL,
    -- corresponds to workforce_capabilities.code (soft ref, not a hard FK so a missing/unbuilt
    -- capability row never blocks a duty declaration); NULL = duty exists but confers no execution grant
    -- (module not built yet).
    capability_code   text,
    effective_from    timestamptz NOT NULL DEFAULT now(),
    effective_to      timestamptz,
    status            text NOT NULL DEFAULT 'active',
    row_version       integer NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT position_module_duties_duty_type_check
        CHECK (duty_type IN ('execute', 'verify', 'manage', 'support')),
    CONSTRAINT position_module_duties_status_check
        CHECK (status IN ('active', 'inactive'))
);

-- One duty row per (tenant, position, module, duty) effective-window start.
CREATE UNIQUE INDEX IF NOT EXISTS position_module_duties_uq
    ON position_module_duties (tenant_id, position_code, module_code, duty_type, effective_from);

-- Hot read 1: duties for a given position ("what does this person do").
CREATE INDEX IF NOT EXISTS position_module_duties_by_position
    ON position_module_duties (tenant_id, position_code)
    WHERE status = 'active';

-- Hot read 2: holders of a module/duty ("who executes vaccination").
CREATE INDEX IF NOT EXISTS position_module_duties_by_module
    ON position_module_duties (tenant_id, module_code, duty_type)
    WHERE status = 'active';

-- +goose Down
DROP TABLE IF EXISTS position_module_duties;
