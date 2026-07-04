-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS protocol_rule_dimensions (
    protocol_rule_dimension_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    protocol_version_id uuid NOT NULL REFERENCES protocol_versions(protocol_version_id) ON DELETE CASCADE,
    rule_id uuid NOT NULL REFERENCES protocol_rules(rule_id) ON DELETE CASCADE,
    category text NOT NULL,
    ruleset_family text NOT NULL DEFAULT '',
    matrix_row_id text NOT NULL DEFAULT '',
    selector_key text NOT NULL,
    dose_code text NOT NULL DEFAULT '',
    source_dose_code text NOT NULL DEFAULT '',
    vaccine_code text NOT NULL DEFAULT '',
    vaccine_type text NOT NULL DEFAULT '',
    pathogen_class text NOT NULL DEFAULT '',
    compatibility_group text NOT NULL DEFAULT '',
    species text NOT NULL DEFAULT 'all',
    animal_stage text NOT NULL DEFAULT 'all',
    sex text NOT NULL DEFAULT 'all',
    breed text NOT NULL DEFAULT 'all',
    lifecycle text NOT NULL DEFAULT 'alive',
    health text NOT NULL DEFAULT 'any',
    reproductive text NOT NULL DEFAULT 'any',
    min_age_days integer,
    max_age_days integer,
    trigger_type text NOT NULL DEFAULT '',
    sequence integer NOT NULL DEFAULT 0,
    offset_days integer NOT NULL DEFAULT 0,
    due_window_days integer NOT NULL DEFAULT 0,
    min_gap_days integer NOT NULL DEFAULT 0,
    repeat text NOT NULL DEFAULT 'none',
    catch_up text NOT NULL DEFAULT 'immediate',
    max_delay_days integer NOT NULL DEFAULT 0,
    revaccination_interval_days integer NOT NULL DEFAULT 0,
    eligibility_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    vaccine_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    schedule_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, protocol_version_id, rule_id, selector_key),
    CHECK (sex IN ('female', 'male', 'all')),
    CHECK (species <> ''),
    CHECK (animal_stage <> ''),
    CHECK (breed <> '')
);

CREATE INDEX IF NOT EXISTS protocol_rule_dimensions_match_idx
    ON protocol_rule_dimensions (tenant_id, protocol_version_id, category, species, animal_stage, sex, breed);

CREATE INDEX IF NOT EXISTS protocol_rule_dimensions_match_folded_idx
    ON protocol_rule_dimensions (tenant_id, protocol_version_id, category, species, lower(animal_stage), sex, lower(breed));

CREATE INDEX IF NOT EXISTS protocol_rule_dimensions_age_idx
    ON protocol_rule_dimensions (tenant_id, protocol_version_id, category, min_age_days, max_age_days);

CREATE INDEX IF NOT EXISTS protocol_rule_dimensions_rule_idx
    ON protocol_rule_dimensions (tenant_id, rule_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS protocol_rule_dimensions;
-- +goose StatementEnd
