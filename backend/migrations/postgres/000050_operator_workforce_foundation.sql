-- +goose Up
CREATE TABLE workforce_members (
  workforce_member_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  user_id uuid NULL,
  display_code text NOT NULL,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'candidate',
  primary_role_hint text NOT NULL DEFAULT 'operator',
  primary_location_id uuid NULL REFERENCES locations(location_id),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_members_display_code_check CHECK (btrim(display_code) <> ''),
  CONSTRAINT workforce_members_display_name_check CHECK (btrim(display_name) <> ''),
  CONSTRAINT workforce_members_status_check CHECK (status IN ('candidate', 'active', 'inactive', 'suspended', 'left')),
  CONSTRAINT workforce_members_role_hint_check CHECK (primary_role_hint IN ('operator', 'park_head', 'verifier', 'supervisor', 'admin', 'other')),
  CONSTRAINT workforce_members_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX workforce_members_code_unique_idx
  ON workforce_members (tenant_id, display_code);
CREATE UNIQUE INDEX workforce_members_active_user_unique_idx
  ON workforce_members (tenant_id, user_id)
  WHERE user_id IS NOT NULL AND status = 'active';
CREATE INDEX workforce_members_status_updated_idx
  ON workforce_members (tenant_id, status, updated_at DESC, workforce_member_id DESC);
CREATE INDEX workforce_members_location_status_idx
  ON workforce_members (tenant_id, primary_location_id, status, updated_at DESC)
  WHERE primary_location_id IS NOT NULL;
CREATE INDEX workforce_members_user_idx
  ON workforce_members (tenant_id, user_id)
  WHERE user_id IS NOT NULL;

CREATE TABLE workforce_external_identities (
  external_identity_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NULL REFERENCES workforce_members(workforce_member_id),
  source_system text NOT NULL,
  source_flow text NOT NULL,
  external_ref_type text NOT NULL,
  external_ref_hash text NOT NULL,
  encrypted_external_ref bytea NULL,
  status text NOT NULL DEFAULT 'candidate',
  confidence numeric NOT NULL DEFAULT 0,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  observation_count bigint NOT NULL DEFAULT 1,
  reviewed_by uuid NULL,
  reviewed_at timestamptz NULL,
  review_reason text NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_external_identities_source_system_check CHECK (source_system IN ('slack', 'app_script', 'sheet', 'bq', 'firebase', 'manual', 'other')),
  CONSTRAINT workforce_external_identities_ref_type_check CHECK (external_ref_type IN ('slack_user_id', 'email', 'phone', 'staff_label', 'sheet_user', 'firebase_uid', 'other')),
  CONSTRAINT workforce_external_identities_status_check CHECK (status IN ('candidate', 'mapped', 'rejected', 'conflict', 'retired')),
  CONSTRAINT workforce_external_identities_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
  CONSTRAINT workforce_external_identities_observation_check CHECK (observation_count > 0),
  CONSTRAINT workforce_external_identities_seen_window_check CHECK (last_seen_at >= first_seen_at),
  CONSTRAINT workforce_external_identities_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX workforce_external_identities_ref_unique_idx
  ON workforce_external_identities (tenant_id, source_system, external_ref_type, external_ref_hash);
CREATE INDEX workforce_external_identities_status_seen_idx
  ON workforce_external_identities (tenant_id, status, last_seen_at DESC, external_identity_id DESC);
CREATE INDEX workforce_external_identities_member_status_idx
  ON workforce_external_identities (tenant_id, workforce_member_id, status)
  WHERE workforce_member_id IS NOT NULL;

CREATE TABLE workforce_capabilities (
  capability_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  capability_code text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workforce_capabilities_code_check CHECK (capability_code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
  CONSTRAINT workforce_capabilities_status_check CHECK (status IN ('active', 'inactive', 'retired'))
);

CREATE UNIQUE INDEX workforce_capabilities_code_unique_idx
  ON workforce_capabilities (tenant_id, capability_code);
CREATE INDEX workforce_capabilities_status_idx
  ON workforce_capabilities (tenant_id, status, capability_code);

CREATE TABLE workforce_member_capabilities (
  member_capability_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  capability_id uuid NOT NULL REFERENCES workforce_capabilities(capability_id),
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  status text NOT NULL DEFAULT 'active',
  valid_from timestamptz NOT NULL DEFAULT now(),
  valid_to timestamptz NULL,
  assigned_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workforce_member_capabilities_scope_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT workforce_member_capabilities_status_check CHECK (status IN ('active', 'inactive', 'revoked')),
  CONSTRAINT workforce_member_capabilities_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE UNIQUE INDEX workforce_member_capabilities_active_unique_idx
  ON workforce_member_capabilities (tenant_id, workforce_member_id, capability_id, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL;
CREATE INDEX workforce_member_capabilities_member_active_idx
  ON workforce_member_capabilities (tenant_id, workforce_member_id, status, valid_from, valid_to);
CREATE INDEX workforce_member_capabilities_scope_active_idx
  ON workforce_member_capabilities (tenant_id, capability_id, scope_type, scope_id, status, valid_from, valid_to);

CREATE TABLE workforce_roster_assignments (
  roster_assignment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  team_id uuid NULL,
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  shift_date date NOT NULL,
  shift_start_at timestamptz NOT NULL,
  shift_end_at timestamptz NOT NULL,
  task_type text NULL,
  status text NOT NULL DEFAULT 'scheduled',
  escalation_owner_user_id uuid NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_roster_assignments_scope_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT workforce_roster_assignments_status_check CHECK (status IN ('scheduled', 'active', 'completed', 'missed', 'canceled')),
  CONSTRAINT workforce_roster_assignments_shift_window_check CHECK (shift_end_at > shift_start_at),
  CONSTRAINT workforce_roster_assignments_row_version_check CHECK (row_version >= 1)
);

CREATE INDEX workforce_roster_assignments_scope_date_idx
  ON workforce_roster_assignments (tenant_id, shift_date, scope_type, scope_id, status);
CREATE INDEX workforce_roster_assignments_member_date_idx
  ON workforce_roster_assignments (tenant_id, workforce_member_id, shift_date, status);

CREATE TABLE workforce_absences (
  absence_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  reason_code text NOT NULL,
  status text NOT NULL DEFAULT 'reported',
  replacement_member_id uuid NULL REFERENCES workforce_members(workforce_member_id),
  created_by uuid NULL,
  approved_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_absences_scope_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT workforce_absences_status_check CHECK (status IN ('reported', 'approved', 'rejected', 'canceled')),
  CONSTRAINT workforce_absences_window_check CHECK (ends_at > starts_at),
  CONSTRAINT workforce_absences_row_version_check CHECK (row_version >= 1)
);

CREATE INDEX workforce_absences_member_window_idx
  ON workforce_absences (tenant_id, workforce_member_id, status, starts_at, ends_at);
CREATE INDEX workforce_absences_scope_window_idx
  ON workforce_absences (tenant_id, scope_type, scope_id, status, starts_at, ends_at);

CREATE TABLE workforce_member_devices (
  device_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  platform text NOT NULL DEFAULT 'android',
  app_install_id text NOT NULL,
  device_public_key_hash text NULL,
  push_token_hash text NULL,
  app_version text NOT NULL,
  os_version text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'active',
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  registered_by uuid NULL,
  registered_at timestamptz NOT NULL DEFAULT now(),
  revoked_by uuid NULL,
  revoked_at timestamptz NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_member_devices_platform_check CHECK (platform = 'android'),
  CONSTRAINT workforce_member_devices_status_check CHECK (status IN ('active', 'revoked', 'lost', 'retired')),
  CONSTRAINT workforce_member_devices_app_install_check CHECK (btrim(app_install_id) <> ''),
  CONSTRAINT workforce_member_devices_app_version_check CHECK (btrim(app_version) <> ''),
  CONSTRAINT workforce_member_devices_row_version_check CHECK (row_version >= 1)
);

CREATE UNIQUE INDEX workforce_member_devices_install_unique_idx
  ON workforce_member_devices (tenant_id, app_install_id);
CREATE INDEX workforce_member_devices_member_status_idx
  ON workforce_member_devices (tenant_id, workforce_member_id, status);
CREATE INDEX workforce_member_devices_last_seen_idx
  ON workforce_member_devices (tenant_id, last_seen_at DESC);

CREATE TABLE workforce_member_app_sessions (
  session_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  device_id uuid NULL REFERENCES workforce_member_devices(device_id),
  auth_subject uuid NOT NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  ended_at timestamptz NULL,
  status text NOT NULL DEFAULT 'active',
  app_version text NOT NULL DEFAULT '',
  ip_hash text NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT workforce_member_app_sessions_status_check CHECK (status IN ('active', 'ended', 'denied')),
  CONSTRAINT workforce_member_app_sessions_seen_check CHECK (last_seen_at >= started_at)
);

CREATE INDEX workforce_member_app_sessions_member_status_idx
  ON workforce_member_app_sessions (tenant_id, workforce_member_id, status, last_seen_at DESC);
CREATE INDEX workforce_member_app_sessions_device_idx
  ON workforce_member_app_sessions (tenant_id, device_id, last_seen_at DESC)
  WHERE device_id IS NOT NULL;

INSERT INTO workforce_capabilities (tenant_id, capability_code, description, status)
VALUES
  ('00000000-0000-4000-8000-000000000001', 'movement.execute', 'Execute movement and Shifting SOP work.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'count.verify', 'Verify count and herd snapshot tasks.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'death.report', 'Report mortality and death SOP evidence.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'vaccination.execute', 'Execute vaccination SOP work.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'health.follow_up', 'Execute health follow-up tasks.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'feed.report', 'Report feed task completion.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'proof.verify', 'Review proof and request rework.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'rfid.scan', 'Use RFID scans during operator work.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'media.video_capture', 'Capture video proof for SOP work.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'scale.capture', 'Capture scale readings during operator work.', 'active')
ON CONFLICT (tenant_id, capability_code) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS workforce_member_app_sessions;
DROP TABLE IF EXISTS workforce_member_devices;
DROP TABLE IF EXISTS workforce_absences;
DROP TABLE IF EXISTS workforce_roster_assignments;
DROP TABLE IF EXISTS workforce_member_capabilities;
DROP TABLE IF EXISTS workforce_capabilities;
DROP TABLE IF EXISTS workforce_external_identities;
DROP TABLE IF EXISTS workforce_members;
