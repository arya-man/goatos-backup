-- +goose Up
CREATE TABLE sop_definitions (
  sop_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  code text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT sop_definitions_code_check CHECK (code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
  CONSTRAINT sop_definitions_name_check CHECK (btrim(name) <> ''),
  CONSTRAINT sop_definitions_status_check CHECK (status IN ('draft', 'active', 'retired')),
  CONSTRAINT sop_definitions_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX sop_definitions_tenant_code_unique_idx
  ON sop_definitions (tenant_id, code);
CREATE INDEX sop_definitions_tenant_status_idx
  ON sop_definitions (tenant_id, status, updated_at DESC, sop_id DESC);

CREATE TABLE sop_versions (
  sop_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  sop_id uuid NOT NULL REFERENCES sop_definitions(sop_id),
  version int NOT NULL,
  version_label text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  form_dsl jsonb NOT NULL,
  proof_policy jsonb NOT NULL,
  compatibility jsonb NOT NULL DEFAULT '{}'::jsonb,
  validation_report jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by uuid NULL,
  published_by uuid NULL,
  published_at timestamptz NULL,
  retired_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT sop_versions_version_check CHECK (version > 0),
  CONSTRAINT sop_versions_label_check CHECK (btrim(version_label) <> ''),
  CONSTRAINT sop_versions_status_check CHECK (status IN ('draft', 'published', 'retired')),
  CONSTRAINT sop_versions_form_object_check CHECK (jsonb_typeof(form_dsl) = 'object'),
  CONSTRAINT sop_versions_proof_object_check CHECK (jsonb_typeof(proof_policy) = 'object'),
  CONSTRAINT sop_versions_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX sop_versions_tenant_sop_version_unique_idx
  ON sop_versions (tenant_id, sop_id, version);
CREATE INDEX sop_versions_tenant_status_idx
  ON sop_versions (tenant_id, status, updated_at DESC, sop_version_id DESC);
CREATE UNIQUE INDEX sop_versions_one_published_per_sop_idx
  ON sop_versions (tenant_id, sop_id)
  WHERE status = 'published';

CREATE TABLE sop_tasks (
  task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  sop_id uuid NOT NULL REFERENCES sop_definitions(sop_id),
  sop_version_id uuid NOT NULL REFERENCES sop_versions(sop_version_id),
  task_type text NOT NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'assigned',
  assigned_to uuid NULL,
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  priority text NOT NULL DEFAULT 'normal',
  due_at timestamptz NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by uuid NULL,
  verified_by uuid NULL,
  verified_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT sop_tasks_task_type_check CHECK (btrim(task_type) <> ''),
  CONSTRAINT sop_tasks_title_check CHECK (btrim(title) <> ''),
  CONSTRAINT sop_tasks_state_check CHECK (state IN ('queued', 'assigned', 'in_progress', 'submitted', 'accepted', 'needs_review', 'rework_requested', 'rejected', 'canceled')),
  CONSTRAINT sop_tasks_scope_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT sop_tasks_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
  CONSTRAINT sop_tasks_context_object_check CHECK (jsonb_typeof(context) = 'object'),
  CONSTRAINT sop_tasks_row_version_check CHECK (row_version >= 1)
);

CREATE INDEX sop_tasks_queue_idx
  ON sop_tasks (tenant_id, state, due_at, task_id);
CREATE INDEX sop_tasks_assignee_queue_idx
  ON sop_tasks (tenant_id, assigned_to, state, due_at, task_id)
  WHERE assigned_to IS NOT NULL;
CREATE INDEX sop_tasks_scope_queue_idx
  ON sop_tasks (tenant_id, scope_type, scope_id, state, due_at, task_id);
CREATE INDEX sop_tasks_sop_version_idx
  ON sop_tasks (tenant_id, sop_version_id, state);

CREATE TABLE sop_submissions (
  submission_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  task_id uuid NOT NULL REFERENCES sop_tasks(task_id),
  sop_version_id uuid NOT NULL REFERENCES sop_versions(sop_version_id),
  submitted_by uuid NOT NULL,
  idempotency_key text NOT NULL,
  answers jsonb NOT NULL,
  proof_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  state text NOT NULL DEFAULT 'submitted',
  validation_report jsonb NOT NULL DEFAULT '{}'::jsonb,
  submitted_at timestamptz NOT NULL DEFAULT now(),
  accepted_at timestamptz NULL,
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT sop_submissions_idempotency_check CHECK (btrim(idempotency_key) <> ''),
  CONSTRAINT sop_submissions_answers_object_check CHECK (jsonb_typeof(answers) = 'object'),
  CONSTRAINT sop_submissions_proof_array_check CHECK (jsonb_typeof(proof_refs) = 'array'),
  CONSTRAINT sop_submissions_state_check CHECK (state IN ('submitted', 'accepted', 'needs_review', 'rejected', 'voided')),
  CONSTRAINT sop_submissions_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX sop_submissions_tenant_idempotency_unique_idx
  ON sop_submissions (tenant_id, idempotency_key);
CREATE INDEX sop_submissions_task_history_idx
  ON sop_submissions (tenant_id, task_id, submitted_at DESC);
CREATE INDEX sop_submissions_review_idx
  ON sop_submissions (tenant_id, state, submitted_at DESC, submission_id DESC);

CREATE TABLE sop_submission_items (
  item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  submission_id uuid NOT NULL REFERENCES sop_submissions(submission_id),
  task_id uuid NOT NULL REFERENCES sop_tasks(task_id),
  goat_id uuid NULL REFERENCES goats(goat_id),
  item_key text NOT NULL,
  state text NOT NULL,
  result jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sop_submission_items_state_check CHECK (state IN ('accepted', 'needs_review', 'rejected', 'skipped')),
  CONSTRAINT sop_submission_items_key_check CHECK (btrim(item_key) <> ''),
  CONSTRAINT sop_submission_items_result_object_check CHECK (jsonb_typeof(result) = 'object')
);

CREATE INDEX sop_submission_items_submission_idx
  ON sop_submission_items (tenant_id, submission_id, item_id);
CREATE INDEX sop_submission_items_goat_history_idx
  ON sop_submission_items (tenant_id, goat_id, created_at DESC)
  WHERE goat_id IS NOT NULL;

CREATE TABLE movement_commands (
  command_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  task_id uuid NOT NULL REFERENCES sop_tasks(task_id),
  submission_id uuid NOT NULL REFERENCES sop_submissions(submission_id),
  command_type text NOT NULL,
  state text NOT NULL DEFAULT 'accepted',
  payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT movement_commands_type_check CHECK (command_type IN ('shifting.apply')),
  CONSTRAINT movement_commands_state_check CHECK (state IN ('pending', 'accepted', 'failed')),
  CONSTRAINT movement_commands_payload_object_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE UNIQUE INDEX movement_commands_submission_unique_idx
  ON movement_commands (tenant_id, submission_id, command_type);
CREATE INDEX movement_commands_task_idx
  ON movement_commands (tenant_id, task_id, created_at DESC);

WITH seeded_sop AS (
  INSERT INTO sop_definitions (
    tenant_id, code, name, description, status, created_by
  ) VALUES (
    '00000000-0000-4000-8000-000000000001',
    'shifting',
    'Shifting',
    'Move goats from one location to another with proof and verification.',
    'active',
    NULL
  )
  ON CONFLICT (tenant_id, code) DO UPDATE
    SET name = EXCLUDED.name,
        description = EXCLUDED.description,
        status = CASE WHEN sop_definitions.status = 'retired' THEN 'active' ELSE sop_definitions.status END,
        updated_at = now()
  RETURNING tenant_id, sop_id
)
INSERT INTO sop_versions (
  tenant_id,
  sop_id,
  version,
  version_label,
  status,
  form_dsl,
  proof_policy,
  compatibility,
  validation_report,
  created_by,
  published_by,
  published_at
)
SELECT
  tenant_id,
  sop_id,
  1,
  'Shifting v1',
  'published',
  '{
    "schema_version": "goatos.sop-form.v1",
    "sop_code": "shifting",
    "title": "Shifting",
    "repeat_for_each_goat": true,
    "fields": [
      {"key": "shift_type", "label": "Shift Type", "type": "select", "required": true, "options": ["request", "direction"]},
      {"key": "category", "label": "Category", "type": "select", "required": true, "options": ["routine", "health", "sale", "procurement", "emergency"]},
      {"key": "priority", "label": "Priority", "type": "select", "required": true, "options": ["low", "normal", "high", "urgent"]},
      {"key": "goat_ids", "label": "Goats", "type": "goat_lookup", "required": true, "repeat": true},
      {"key": "source_location_id", "label": "Source Location", "type": "location_picker", "required": true, "option_source": "locations.active"},
      {"key": "destination_location_id", "label": "Destination Location", "type": "location_picker", "required": true, "option_source": "locations.active"},
      {"key": "destination_count", "label": "Destination Count", "type": "number", "required": true, "min": 1},
      {"key": "reason", "label": "Reason", "type": "text", "required": false},
      {"key": "proof_video", "label": "Destination Proof Video", "type": "video_proof", "required": true, "proof_action": "video.capture"}
    ],
    "rules": [
      {"type": "block_submission_if", "when": {"field": "destination_location_id", "operator": "empty"}, "message": "Destination location is required."},
      {"type": "proof_required_if", "field": "proof_video", "when": {"field": "category", "operator": "not_empty"}, "message": "Video proof is required before verification."}
    ],
    "workflow": {
      "nodes": [
        {"key": "operator_submission", "label": "Operator Submission", "type": "operator_execution"},
        {"key": "proof_verification", "label": "Proof Verification", "type": "proof_verification"},
        {"key": "accepted", "label": "Accepted", "type": "accepted"},
        {"key": "rework_requested", "label": "Rework Requested", "type": "rework"}
      ],
      "edges": [
        {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
        {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
        {"from": "proof_verification", "to": "rework_requested", "condition": "verifier_rejects"}
      ]
    }
  }'::jsonb,
  '{"required": true, "scope": "batch", "types": ["video"], "minimum_count": 1, "verify_before_apply": true}'::jsonb,
  '{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "select", "multiselect", "goat_lookup", "rfid_scan", "location_picker", "photo_proof", "video_proof"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"], "supported_proof_actions": ["photo.capture", "video.capture"]}'::jsonb,
  '{"valid": true, "errors": [], "warnings": [{"field": "form_dsl", "code": "seeded", "message": "Seeded Shifting v1 walking skeleton."}]}'::jsonb,
  NULL,
  NULL,
  now()
FROM seeded_sop
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS movement_commands;
DROP TABLE IF EXISTS sop_submission_items;
DROP TABLE IF EXISTS sop_submissions;
DROP TABLE IF EXISTS sop_tasks;
DROP TABLE IF EXISTS sop_versions;
DROP TABLE IF EXISTS sop_definitions;
