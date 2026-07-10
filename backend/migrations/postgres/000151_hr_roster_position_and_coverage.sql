-- +goose Up
-- HR roster / RBAC position + coverage model. Approved design:
-- docs/hr/roster-rbac-design.md (maintainer-confirmed 2026-07-10). Per the
-- doc's "what already exists -- do not re-build" section (S1), this migration
-- is deliberately small: `workforce_positions` is the ONLY new table. Every
-- other roster concept REUSES an already-committed table:
--   1) Permanent department/module ownership -> departments +
--      department_module_grants + workforce_members.department_id (000148).
--   2) Fixed operational position (this file's new table).
--   3) Leave/absence -> workforce_absences (000050), extended below with the
--      escalation_required status value + a coverage_override_reason column.
--      Its existing replacement_member_id column is the "who covers this
--      absence" pointer the design doc's S4.5 fills automatically.
--   4) Temporary task coverage -> a service-layer invariant on
--      workforce_absences.replacement_member_id (S4.5); NOT a new table.
--   5) Temporary execution permission -> workforce_member_capabilities
--      (000050), a normal time-bounded row for the backup holder; NOT a new
--      grants table.
--   CEO superuser tier -> the existing user_scope_grants.role='ceo_internal'
--      + the existing 'leadership' department (000149); NOT modeled here.
--   Escalation target -> workforce_roster_assignments.escalation_owner_user_id
--      (000050) / the scope's park_head position row; delivery channel is the
--      kernel's existing notification path, not built in this migration.
--
-- Three independent HR axes on a person (S0.2, S4.1 -- never collapse):
--   a) HR Designation grade (CXO/Director/Manager/Assistant Manager) -- a
--      plain nullable CHECK column on workforce_members (S7.3 resolved: no
--      lookup table, vocabulary is small and stable). Informational only
--      (S4.4): must never drive nav, permission, or CEO-superuser status.
--   b) Operational position (this file's workforce_positions) -- fixed,
--      center-scoped, never mutated by leave/week-off.
--   c) Department ownership (existing, untouched).
--
-- Scope unit is the CENTER (S0.2, S7.2 resolved): HQ-tier (CXO/Director) staff
-- are scope_type='tenant'; CBE/CPT staff are scope_type='center' with
-- scope_id = the existing park-type `locations` row for that center (no new
-- center location row). scope_id is intentionally NOT FK'd to `locations`
-- here, matching every other polymorphic scope_id column in this schema
-- (workforce_absences, user_scope_grants, workforce_roster_assignments, ...).
CREATE TABLE workforce_positions (
  position_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members(workforce_member_id),
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  position_code text NOT NULL,
  position_tier text NOT NULL,
  is_backup_slot boolean NOT NULL DEFAULT false,
  backup_group_code text NULL,
  week_off_weekday text NULL,
  status text NOT NULL DEFAULT 'active',
  valid_from timestamptz NOT NULL DEFAULT now(),
  valid_to timestamptz NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT workforce_positions_scope_check CHECK (scope_type IN ('tenant', 'center')),
  CONSTRAINT workforce_positions_position_code_check CHECK (position_code ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT workforce_positions_tier_check CHECK (position_tier IN ('assistant', 'manager', 'head', 'director', 'cxo')),
  CONSTRAINT workforce_positions_backup_group_check CHECK (backup_group_code IS NULL OR backup_group_code ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT workforce_positions_week_off_check CHECK (week_off_weekday IS NULL OR week_off_weekday IN ('monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday')),
  CONSTRAINT workforce_positions_status_check CHECK (status IN ('active', 'inactive', 'ended')),
  CONSTRAINT workforce_positions_window_check CHECK (valid_to IS NULL OR valid_to > valid_from),
  CONSTRAINT workforce_positions_row_version_check CHECK (row_version >= 1)
);

-- Exactly one ACTIVE holder per (scope, position_code) -- mirrors
-- department_module_grants_active_unique's partial-unique pattern (000148).
-- This alone does not force "exactly one Backup Manager"; that emerges from
-- the generalized backup_group_code resolution below (design doc S4.3),
-- which the coverage engine (app.RosterService) resolves generically for
-- BOTH the manager tier (one shared Backup Manager per center) and the
-- assistant tier (its own Backup AM1/AM2 per parallel group) -- never
-- hardcoded to one shape.
CREATE UNIQUE INDEX workforce_positions_active_seat_unique
  ON workforce_positions (tenant_id, scope_type, scope_id, position_code)
  WHERE status = 'active';
CREATE INDEX workforce_positions_member_idx
  ON workforce_positions (tenant_id, workforce_member_id, status);
-- effective_backup(covered_position) lookup: the active is_backup_slot row
-- sharing the covered position's backup_group_code, in the same scope.
CREATE INDEX workforce_positions_backup_group_idx
  ON workforce_positions (tenant_id, scope_type, scope_id, backup_group_code, is_backup_slot, status)
  WHERE backup_group_code IS NOT NULL;

-- Axis (a): HR Designation grade. Informational only -- see file header.
ALTER TABLE workforce_members
  ADD COLUMN hr_designation_grade text NULL;
ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_hr_designation_grade_check
  CHECK (hr_designation_grade IS NULL OR hr_designation_grade IN ('cxo', 'director', 'manager', 'assistant_manager'));

-- workforce_absences (000050) extensions for the coverage invariant (S4.5)
-- and CEO override audit (S7.4 resolved: reuse created_by/approved_by, add
-- only a reason column, no new audit table). Additive only: the existing
-- reported/approved/rejected/canceled vocabulary is unchanged and sufficient
-- per the design doc S1; escalation_required is the one new state, set by the
-- absence-approval service when the covered position's effective_backup
-- cannot be resolved or is itself unavailable (S4.7) -- the replacement_
-- member_id pointer this table already has stays NULL in that case.
ALTER TABLE workforce_absences
  DROP CONSTRAINT IF EXISTS workforce_absences_status_check;
ALTER TABLE workforce_absences
  ADD CONSTRAINT workforce_absences_status_check
  CHECK (status IN ('reported', 'approved', 'escalation_required', 'rejected', 'canceled'));

ALTER TABLE workforce_absences
  DROP CONSTRAINT IF EXISTS workforce_absences_scope_check;
ALTER TABLE workforce_absences
  ADD CONSTRAINT workforce_absences_scope_check
  CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort', 'center'));

ALTER TABLE workforce_absences
  ADD COLUMN coverage_override_reason text NULL;

COMMENT ON COLUMN workforce_absences.replacement_member_id IS
  'HR roster coverage pointer (design doc S4.5). Filled automatically by the absence-approval '
  'service from effective_backup(covered position) -- never operator-chosen. NULL + '
  'status=escalation_required means no backup could be resolved or the backup was itself '
  'unavailable (S4.7); a human (CEO tier) may then set this explicitly via the resolve-coverage '
  'endpoint, constrained to the same backup_group_code, recording why in coverage_override_reason.';

-- workforce_member_capabilities (000050) is reused as-is for the temporary
-- execution grant (S4.6): a normal time-bounded row for the backup holder,
-- scoped to the covered position's center, valid_from/valid_to set to the
-- coverage window (the absence window, or a single week-off day). It already
-- expires on its own -- no cleanup job. Only the scope vocabulary needs
-- extending to admit 'center'.
ALTER TABLE workforce_member_capabilities
  DROP CONSTRAINT IF EXISTS workforce_member_capabilities_scope_check;
ALTER TABLE workforce_member_capabilities
  ADD CONSTRAINT workforce_member_capabilities_scope_check
  CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort', 'center'));

-- +goose Down
ALTER TABLE workforce_member_capabilities
  DROP CONSTRAINT IF EXISTS workforce_member_capabilities_scope_check;
ALTER TABLE workforce_member_capabilities
  ADD CONSTRAINT workforce_member_capabilities_scope_check
  CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort'));

ALTER TABLE workforce_absences DROP COLUMN IF EXISTS coverage_override_reason;

ALTER TABLE workforce_absences
  DROP CONSTRAINT IF EXISTS workforce_absences_scope_check;
ALTER TABLE workforce_absences
  ADD CONSTRAINT workforce_absences_scope_check
  CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort'));

ALTER TABLE workforce_absences
  DROP CONSTRAINT IF EXISTS workforce_absences_status_check;
ALTER TABLE workforce_absences
  ADD CONSTRAINT workforce_absences_status_check
  CHECK (status IN ('reported', 'approved', 'rejected', 'canceled'));

ALTER TABLE workforce_members DROP CONSTRAINT IF EXISTS workforce_members_hr_designation_grade_check;
ALTER TABLE workforce_members DROP COLUMN IF EXISTS hr_designation_grade;

DROP TABLE IF EXISTS workforce_positions;
