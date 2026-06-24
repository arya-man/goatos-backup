-- +goose Up
-- Backend-owned proof/media records. Clients receive server-issued proof_id values and storage
-- targets; SOP submissions reference these records instead of arbitrary URLs, Firebase paths, Slack
-- permalinks, or raw local filesystem paths.
CREATE TABLE proof_artifacts (
  proof_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  storage_provider text NOT NULL,
  object_key text NOT NULL,
  content_hash text NOT NULL DEFAULT '',
  mime_type text NOT NULL DEFAULT '',
  size_bytes bigint NOT NULL DEFAULT 0,
  duration_ms bigint NULL,
  upload_state text NOT NULL DEFAULT 'pending',
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  subject_type text NOT NULL,
  subject_id uuid NULL,
  proof_type text NOT NULL,
  uploaded_by uuid NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  uploaded_at timestamptz NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT proof_artifacts_provider_check CHECK (storage_provider IN ('local', 'gcs')),
  CONSTRAINT proof_artifacts_object_key_check CHECK (btrim(object_key) <> ''),
  CONSTRAINT proof_artifacts_upload_state_check CHECK (upload_state IN ('pending', 'uploading', 'completed', 'failed')),
  CONSTRAINT proof_artifacts_scope_check CHECK (scope_type IN ('tenant', 'farm', 'park', 'shed', 'cohort', 'batch', 'task', 'goat')),
  CONSTRAINT proof_artifacts_subject_check CHECK (subject_type IN ('batch', 'goat', 'shed', 'task', 'vial_lot', 'administration', 'other')),
  CONSTRAINT proof_artifacts_proof_type_check CHECK (proof_type IN ('photo', 'video', 'attachment')),
  CONSTRAINT proof_artifacts_size_check CHECK (size_bytes >= 0),
  CONSTRAINT proof_artifacts_duration_check CHECK (duration_ms IS NULL OR duration_ms >= 0),
  CONSTRAINT proof_artifacts_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT proof_artifacts_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX proof_artifacts_tenant_object_key_unique_idx
  ON proof_artifacts (tenant_id, object_key);
CREATE INDEX proof_artifacts_tenant_state_idx
  ON proof_artifacts (tenant_id, upload_state, created_at DESC, proof_id DESC);
CREATE INDEX proof_artifacts_scope_idx
  ON proof_artifacts (tenant_id, scope_type, scope_id, created_at DESC);
CREATE INDEX proof_artifacts_subject_idx
  ON proof_artifacts (tenant_id, subject_type, subject_id, created_at DESC)
  WHERE subject_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS proof_artifacts;
