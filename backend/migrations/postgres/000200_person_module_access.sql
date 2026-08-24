-- +goose Up
--
-- PER-PERSON MODULE ACCESS (People/HRMS rewrite, maintainer decision 2026-08-24).
--
-- Access stops being DERIVED and becomes ASSIGNED. Until now a person carried a
-- role + department + park, and four separate mechanisms turned those into
-- modules and permissions:
--
--   ordinary staff (phone)  -> department_module_grants
--   leadership   (phone)    -> a curated module list hardcoded per role
--   verifiers    (phone)    -> the verify duties on their position
--   everyone     (web)      -> role -> permission set -> per-screen gate
--
-- That model had already broken in production, and STG shows all three symptoms:
-- four people wear stacked job titles (one wears FIVE) because no single title
-- describes their job; a department is named after an individual
-- (`avishek_health_access`); and that same person has two roster rows. Each is a
-- workaround for the same missing thing -- no way to say "this person, this
-- module, this much authority".
--
-- These tables are that sentence. Read them with
-- backend/internal/permissions/capability.go, which is the ONLY place that says
-- what a capability MEANS in permissions; nothing here encodes that mapping, so
-- the two can never drift.
--
-- Seed/backfill coupling: `backend/cmd/backfill-person-access` fills all four
-- tables from the retired roles via permissions.AssignmentsForRoles, and is
-- wired into seed closeout. The mapping lives in Go and is proved by
-- capability_parity_test.go (zero permission losses across all 47 roles), so it
-- is deliberately NOT reimplemented as SQL here -- a second implementation of a
-- security-critical mapping is exactly how the two would disagree.

-- The DESIGNATION CATALOG: a fixed, extendable list of job titles. Picking one
-- pre-fills a new person's access; it never constrains it afterwards, which is
-- the difference between this and the department inheritance it replaces.
--
-- Rows are seeded here (stable labels) while the per-designation module defaults
-- are written by backfill-person-access from the same Go mapping the migration
-- deliberately does not duplicate.
CREATE TABLE IF NOT EXISTS public.designation_catalog (
  designation_code text PRIMARY KEY,
  label            text NOT NULL,
  -- The HR grade this designation sits at, matching workforce_members.hr_designation_grade.
  grade            text,
  sort_order       integer NOT NULL DEFAULT 100,
  status           text NOT NULL DEFAULT 'active',
  CONSTRAINT designation_catalog_status_check
    CHECK (status IN ('active', 'retired'))
);

-- What ticking a designation pre-fills, per surface and module.
CREATE TABLE IF NOT EXISTS public.designation_module_defaults (
  designation_code text NOT NULL REFERENCES designation_catalog (designation_code) ON DELETE CASCADE,
  surface          text NOT NULL,
  module_key       text NOT NULL,
  capabilities     text[] NOT NULL DEFAULT '{}',
  PRIMARY KEY (designation_code, surface, module_key),
  CONSTRAINT designation_module_defaults_surface_check
    CHECK (surface IN ('web', 'mobile')),
  CONSTRAINT designation_module_defaults_capabilities_check
    CHECK (capabilities <@ ARRAY['view', 'do', 'oversee', 'configure']::text[])
);

-- The starting catalog: the real job titles the farm uses today, which are also
-- exactly the retired flat roles, so each one's defaults are that role's proven
-- assignment set. `counts_approver` is deliberately ABSENT -- it was never a job,
-- it is approval authority layered onto one by name (maintainer decision
-- 2026-08-05), and it stays that way as ticks on a person rather than a title.
INSERT INTO public.designation_catalog (designation_code, label, grade, sort_order) VALUES
  ('ceo_internal',         'CEO / CXO',              'cxo',               10),
  ('pc_director',          'Preventive Care Director', 'director',        20),
  ('growth_director',      'Growth Director',          'director',        30),
  ('feed_director',        'Feed Director',            'director',        40),
  ('health_director',      'Health Director',          'director',        50),
  ('procurement_director', 'Procurement Director',     'director',        60),
  ('procurement_manager',  'Procurement Manager',      'manager',         70),
  ('park_head',            'Park Head',                'manager',         80),
  ('verifier',             'Verifier',                 NULL,              90),
  ('operator',             'Operator',                 NULL,             100)
ON CONFLICT (designation_code) DO NOTHING;

-- One header row per person: how wide their data scope is, and which designation
-- pre-filled their access. Separate from the module rows so "this person has been
-- set up" stays distinguishable from "this person happens to have no modules".
CREATE TABLE IF NOT EXISTS public.person_access (
  tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members (workforce_member_id) ON DELETE CASCADE,
  -- 'tenant' sees every park; 'parks' is limited to the rows in
  -- person_park_scope. Stored explicitly rather than inferred from an empty park
  -- list, because "every park" and "no park chosen yet" are different answers and
  -- inferring one from the other silently widens access.
  scope_mode          text NOT NULL DEFAULT 'parks',
  -- The designation whose defaults were applied. Advisory only: it records what
  -- the access STARTED as, and never overrides the rows themselves -- the whole
  -- point of the rewrite is that a person's access is theirs, not their title's.
  designation_code    text REFERENCES designation_catalog (designation_code),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  row_version         integer NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id, workforce_member_id),
  CONSTRAINT person_access_scope_mode_check
    CHECK (scope_mode IN ('tenant', 'parks')),
  CONSTRAINT person_access_row_version_check
    CHECK (row_version >= 1)
);

-- Which parks a 'parks'-scoped person covers. A park head who must see both parks
-- is two rows here, which is the thing the retired single-park grant could not say.
CREATE TABLE IF NOT EXISTS public.person_park_scope (
  tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members (workforce_member_id) ON DELETE CASCADE,
  park_id             uuid NOT NULL REFERENCES locations (location_id),
  PRIMARY KEY (tenant_id, workforce_member_id, park_id)
);

-- THE ASSIGNMENT. One row per (person, surface, module).
--
-- capabilities is a SET, not a single level, and that is forced by the real
-- roster rather than chosen for flexibility: Dinakar records herd-operation
-- counts AND approves them (he wears `operator` and `counts_approver` at the
-- same time today), and the Assistant Manager tier does the same by
-- construction. A single-level column could not express "does the work AND signs
-- it off", and making a higher level imply the lower ones would hand the Feed
-- Director feed_direction.complete -- a permission deliberately withheld from him.
CREATE TABLE IF NOT EXISTS public.person_module_access (
  tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
  workforce_member_id uuid NOT NULL REFERENCES workforce_members (workforce_member_id) ON DELETE CASCADE,
  -- 'web' (admin-web) or 'mobile' (the Android app). A person may hold DIFFERENT
  -- capabilities on the same module per surface: a director who approves at a
  -- desk and only glances on the phone is the normal case, not an exception.
  surface             text NOT NULL,
  -- The module key, shared with the mobile module registry and the retired
  -- department_module_grants.module_key. NOT a foreign key: the catalog of
  -- modules lives in Go (permissions.ModuleCapabilities) alongside the meaning of
  -- each capability, and splitting the two across a code list and a table is how
  -- they drift. An unknown key grants NOTHING (see PermissionsForAssignments),
  -- so the failure mode of a stale row is missing access, which is visible.
  module_key          text NOT NULL,
  capabilities        text[] NOT NULL DEFAULT '{}',
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  PRIMARY KEY (tenant_id, workforce_member_id, surface, module_key),
  CONSTRAINT person_module_access_surface_check
    CHECK (surface IN ('web', 'mobile')),
  -- Every element must be a known capability. A typo must never be stored: it
  -- would read to a human as granted while granting nothing.
  CONSTRAINT person_module_access_capabilities_check
    CHECK (capabilities <@ ARRAY['view', 'do', 'oversee', 'configure']::text[]),
  -- No duplicates. A repeated element changes nothing about the resolved
  -- permissions but makes the stored row disagree with what the screen shows.
  CONSTRAINT person_module_access_capabilities_distinct_check
    CHECK (array_length(capabilities, 1) IS NULL
           OR array_length(capabilities, 1) = (
             SELECT count(DISTINCT c) FROM unnest(capabilities) AS c
           ))
);

-- The whole-person read: "what can this person do?" is one indexed lookup, and it
-- is on the hot path -- every authenticated request resolves it.
CREATE INDEX IF NOT EXISTS person_module_access_person_idx
  ON public.person_module_access (tenant_id, workforce_member_id);

-- The reverse read the HRMS screen needs: "who can do X on this module?"
CREATE INDEX IF NOT EXISTS person_module_access_module_idx
  ON public.person_module_access (tenant_id, module_key, surface);

-- +goose Down
DROP TABLE IF EXISTS public.person_module_access;
DROP TABLE IF EXISTS public.person_park_scope;
DROP TABLE IF EXISTS public.person_access;
DROP TABLE IF EXISTS public.designation_module_defaults;
DROP TABLE IF EXISTS public.designation_catalog;
