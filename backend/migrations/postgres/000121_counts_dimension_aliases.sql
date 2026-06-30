-- +goose Up
-- Feed Direction G2: reviewed alias normalization for Counts/Shifting input
-- dimensions. This is protocol evidence, not legacy string-transform logic.

CREATE TABLE count_dimension_aliases (
  alias_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  dimension text NOT NULL,
  source_system text NOT NULL DEFAULT '*',
  source_value text NOT NULL,
  source_value_norm text NOT NULL,
  canonical_value text NOT NULL,
  canonical_label text NULL,
  review_status text NOT NULL DEFAULT 'draft',
  source_ref text NOT NULL,
  source_hash text NOT NULL,
  approved_by uuid NULL,
  approved_at timestamptz NULL,
  effective_from date NOT NULL DEFAULT DATE '1970-01-01',
  effective_to date NULL,
  notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT count_dimension_aliases_dimension_check CHECK (dimension IN ('breed', 'stage_tag', 'age_class', 'sex', 'shed_tag')),
  CONSTRAINT count_dimension_aliases_review_status_check CHECK (review_status IN ('draft', 'approved', 'rejected', 'retired')),
  CONSTRAINT count_dimension_aliases_source_check CHECK (btrim(source_system) <> '' AND btrim(source_value) <> '' AND btrim(source_value_norm) <> ''),
  CONSTRAINT count_dimension_aliases_canonical_check CHECK (btrim(canonical_value) <> ''),
  CONSTRAINT count_dimension_aliases_evidence_check CHECK (btrim(source_ref) <> '' AND btrim(source_hash) <> ''),
  CONSTRAINT count_dimension_aliases_effective_range_check CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE UNIQUE INDEX count_dimension_aliases_current_approved_unique
  ON count_dimension_aliases (tenant_id, dimension, source_system, source_value_norm)
  WHERE review_status = 'approved' AND effective_to IS NULL;

CREATE INDEX count_dimension_aliases_lookup_idx
  ON count_dimension_aliases (
    tenant_id,
    dimension,
    source_system,
    source_value_norm,
    review_status,
    effective_from,
    effective_to
  );

CREATE INDEX count_dimension_aliases_review_queue_idx
  ON count_dimension_aliases (tenant_id, review_status, updated_at DESC, alias_id DESC);

-- +goose Down
DROP TABLE IF EXISTS count_dimension_aliases;
