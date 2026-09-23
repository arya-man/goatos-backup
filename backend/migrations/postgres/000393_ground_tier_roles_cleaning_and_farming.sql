-- +goose Up
-- The farm has no "operator" (maintainer decision 2026-09-23). It has MANAGERS, by
-- department -- Feed, Health, Farming, Cleaning -- and ASSISTANT MANAGERS under them.
-- That is what the real roster seats always said (backend/cmd/seed-roster-real:
-- feeding_manager, health_kidding_manager_1/_2, breeding_manager, cleaning_am1,
-- cleaning_am2, farming_am, feeding_am1..3) while RBAC had only the one flat `operator`
-- role to grant those people with. There is no `operator` position_code anywhere in the
-- roster: the word only ever existed in the permission layer.
--
-- THIS MIGRATION IS ADDITIVE ONLY, and that is the whole safety argument. `operator` keeps
-- its org_role_catalog row, its designation_catalog row, and its place in the
-- primary_role_hint CHECK. The 11 people carrying it are moved across one at a time in a
-- later step, each verified, and the role is retired only once nobody holds it.
--
-- Nothing here changes any existing person's access. Access is resolved from
-- person_module_access -- the per-person ticks -- and NOT from the role: see
-- permissions/capability_backfill.go, which states that after the 2026-08-24 cutover the
-- role maps are "DEAD DATA kept for audit", and httpmiddleware/auth.go, which resolves
-- `source = "person"` and falls back to the role path only when the tick tables are
-- unprovisioned.

SET lock_timeout = '5s';

-- 1. The two verticals. org_role_catalog.vertical_code is an FK to org_verticals
--    (org_role_catalog_vertical_code_fkey), so the verticals must land first.
--
--    Cleaning and Farming carry NO module of their own: permissions.verticalModule returns
--    nil for them by falling through its switch, so a cleaning manager's app is Clock In /
--    Out, which is baseline for every principal and needs no tick at all
--    (workforce/app/bootstrap_copy.go: "offered to EVERY non-verifier principal regardless
--    of department grants" and "not tick-revocable"). Anything beyond attendance is a
--    per-person tick made on People / HRMS.
INSERT INTO public.org_verticals (vertical_code, label, sort_order)
VALUES ('cleaning', 'Cleaning', 10),
       ('farming',  'Farming',  11)
ON CONFLICT (vertical_code) DO NOTHING;

-- 2. The composite tier x vertical role keys for the two new verticals.
--    permissions.AllTiers x permissions.AllVerticals must each have a catalog row or
--    TestOrgRoleCatalogHasEntryForEveryComposedRole fails, and without the row every grant
--    INSERT is refused: user_scope_grants_role_fk and auth_pending_email_grants_role_fk are
--    both FKs to org_role_catalog(role_key).
--
--    is_legacy = false matches the other composed rows seeded by the clean-slate baseline
--    (only the flat pre-grid roles carry is_legacy = true).
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES
  ('director_cleaning', 'director', 'cleaning', false, 'Director -- Cleaning',          now()),
  ('head_cleaning',     'head',     'cleaning', false, 'Head (Ops-Head) -- Cleaning',   now()),
  ('manager_cleaning',  'manager',  'cleaning', false, 'Manager -- Cleaning',           now()),
  ('am_cleaning',       'am',       'cleaning', false, 'Assistant Manager -- Cleaning', now()),
  ('director_farming',  'director', 'farming',  false, 'Director -- Farming',           now()),
  ('head_farming',      'head',     'farming',  false, 'Head (Ops-Head) -- Farming',    now()),
  ('manager_farming',   'manager',  'farming',  false, 'Manager -- Farming',            now()),
  ('am_farming',        'am',       'farming',  false, 'Assistant Manager -- Farming',  now())
ON CONFLICT (role_key) DO UPDATE
SET tier_code     = EXCLUDED.tier_code,
    vertical_code = EXCLUDED.vertical_code,
    label         = EXCLUDED.label;

-- 3. A JOB gets a designation row (the 000219 / 000247 pattern), so picking "Feed Manager"
--    when adding a person pre-fills the desk. Only the eight roles the Add Person form
--    actually offers get one -- the director/head rows above stay dormant scaffolding, the
--    way the baseline's other composed rows do.
--
--    The grade column is the tier, and both values already exist in the grade vocabulary
--    (configuration/domain.RoleGrades and workforce/app.validDesignationGrades), so no
--    vocabulary change is needed here.
--
--    Per-designation module defaults are written by backend/cmd/backfill-person-access from
--    permissions.AssignmentsForRole(<code>), which resolves each of these through
--    ParseRoleKey -> tierAssignments + verticalModule. That command HARD-FAILS on an active
--    designation with no Go mapping, which is why every code added here must parse as a
--    composite role key.
INSERT INTO public.designation_catalog (designation_code, label, grade, sort_order)
VALUES
  ('manager_feed',     'Feed Manager',               'manager',           110),
  ('manager_health',   'Health Manager',             'manager',           111),
  ('manager_farming',  'Farming Manager',            'manager',           112),
  ('manager_cleaning', 'Cleaning Manager',           'manager',           113),
  ('am_feed',          'Feed Assistant Manager',     'assistant_manager', 120),
  ('am_health',        'Health Assistant Manager',   'assistant_manager', 121),
  ('am_farming',       'Farming Assistant Manager',  'assistant_manager', 122),
  ('am_cleaning',      'Cleaning Assistant Manager', 'assistant_manager', 123)
ON CONFLICT (designation_code) DO NOTHING;

-- 4. A JOB is also somebody's primary_role_hint: the Add Person form stamps the role's hint
--    (workforce/app.grantablePersonRoles) onto workforce_members, and this CHECK lists every
--    hint the column accepts. The Go side is pinned against this file by
--    workforce/app.TestEveryGrantableRoleHintIsAcceptedByTheColumnCheck.
--
--    The hint is the TIER ('manager' / 'assistant_manager'), not the role key, so the list
--    stays at two new values however many verticals are added later.
--
--    'operator' IS DELIBERATELY KEPT. The installed Android app gates feed-direction and
--    feed-wastage capture on primaryRoleHint == "operator" (FeedDirectionViewModel.kt:164,
--    FeedWastageViewModel.kt:146), so moving a feed manager's HINT before that APK ships
--    would silently take feed capture away from them. Migrating a person therefore moves
--    their ROLE and leaves their hint alone; the hint and this list are cleaned up in the
--    same change that replaces those two gates with backend capability flags.
ALTER TABLE public.workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

ALTER TABLE public.workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (
    primary_role_hint = ANY (ARRAY[
      'operator'::text,
      'manager'::text,
      'assistant_manager'::text,
      'park_head'::text,
      'pc_director'::text,
      'growth_director'::text,
      'feed_director'::text,
      'health_director'::text,
      'breeding_director'::text,
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );

-- +goose Down
SET lock_timeout = '5s';

ALTER TABLE public.workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

ALTER TABLE public.workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (
    primary_role_hint = ANY (ARRAY[
      'operator'::text,
      'park_head'::text,
      'pc_director'::text,
      'growth_director'::text,
      'feed_director'::text,
      'health_director'::text,
      'breeding_director'::text,
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );

DELETE FROM public.designation_catalog
WHERE designation_code IN (
  'manager_feed', 'manager_health', 'manager_farming', 'manager_cleaning',
  'am_feed', 'am_health', 'am_farming', 'am_cleaning'
);

-- Grants must be gone before the catalog rows they reference can be: both role FKs point
-- here. A Down that leaves a live grant behind would fail on the DELETE rather than
-- silently orphan it, which is the honest outcome.
DELETE FROM public.org_role_catalog
WHERE role_key IN (
  'director_cleaning', 'head_cleaning', 'manager_cleaning', 'am_cleaning',
  'director_farming', 'head_farming', 'manager_farming', 'am_farming'
);

DELETE FROM public.org_verticals WHERE vertical_code IN ('cleaning', 'farming');
