-- Phase 2 of the operator retirement (maintainer decision 2026-09-23).
--
-- Moves the ground operators onto the department manager roles added by
-- 000393_ground_tier_roles_cleaning_and_farming.sql. Run the DRY RUN first; it does the
-- whole thing and rolls back, printing the same verification output the apply prints.
--
--   dry run:  psql "$DSN" -v apply=false -f tools/dev/phase2-ground-tier-people-migration.sql
--   apply:    psql "$DSN" -v apply=true  -f tools/dev/phase2-ground-tier-people-migration.sql
--
-- Connect with PGAPPNAME=claude (never a goatos-* application name) so the audit triggers
-- from migration 000387 record this as a manual change. See
-- docs/runbooks/manual-db-change-audit.md.
--
-- WHAT THIS DOES AND DELIBERATELY DOES NOT DO
--
-- It changes the ROLE and the DESIGNATION. It does NOT touch person_module_access for the
-- 26 role-only people, and that is the entire safety argument: access is resolved from those
-- ticks (httpmiddleware/auth.go, source="person"), so leaving them byte-for-byte identical
-- means nobody's access can move. The verification at the bottom proves it rather than
-- asserting it.
--
-- It also does NOT touch workforce_members.primary_role_hint. The installed Android app
-- gates feed-direction and feed-wastage capture on primaryRoleHint == "operator"
-- (FeedDirectionViewModel.kt:164, FeedWastageViewModel.kt:146), so moving the hint before
-- that APK ships would silently take feed capture away from 20 feed managers. The hint moves
-- in the same change that replaces those gates with capability flags.
--
-- TWO EXCEPTIONS, both explicitly chosen by the maintainer:
--   * The two Cleaning Managers are REDUCED to Clock In / Out only. This REMOVES modules they
--     hold today (Manoj 11, Mithlesh 12, including vaccination, feed, weighing and pc_care).
--   * Darshan Talwar has left. His member row is already inactive; his live grant is revoked
--     here. Nothing is hard-deleted: his proofs, clock entries and audit rows are history and
--     stay readable.
--
-- V Munna Kumar is DELIBERATELY ABSENT. He has no person_access row, so he resolves through
-- the role fallback and migrating him WOULD change his access. He joins a later batch once
-- he is ticked on People / HRMS.

\set ON_ERROR_STOP on
\if :{?apply} \else \set apply false \endif

BEGIN;

SELECT audit.begin_change(
  'Manohar via claude',
  'Operator retirement phase 2: move 28 ground operators to department manager roles; reduce 2 cleaning managers to attendance only; revoke the departed Darshan Talwar. docs/decisions + tools/dev/phase2-ground-tier-people-migration.sql'
);

-- ---------------------------------------------------------------------------
-- The mapping. This is the reviewable half: one row per person, as classified.
-- ---------------------------------------------------------------------------
CREATE TEMP TABLE mapping (display_name text PRIMARY KEY, new_role text NOT NULL) ON COMMIT DROP;

INSERT INTO mapping (display_name, new_role) VALUES
  -- Health Managers (6)
  ('Amit Kumar',          'manager_health'),
  ('Sagar Mahoor',        'manager_health'),
  ('Kumar Sharath',       'manager_health'),
  ('Natheswar',           'manager_health'),
  ('Naveen',              'manager_health'),
  ('Pramod',              'manager_health'),
  -- Cleaning Managers (2) -- also reduced to attendance only, below
  ('Manoj Kumar',         'manager_cleaning'),
  ('Mithlesh Kumar',      'manager_cleaning'),
  -- Feed Managers (20)
  ('Bipin',               'manager_feed'),
  ('Bipin Yadav',         'manager_feed'),
  ('Dheeraj Singh',       'manager_feed'),
  ('Mohd Shami',          'manager_feed'),
  ('Rajniti Kumar',       'manager_feed'),
  ('Ravi Kumbar',         'manager_feed'),
  ('Santosh Kumar',       'manager_feed'),
  ('Santosh Kumar Sahni', 'manager_feed'),
  ('Manikanth Yadav',     'manager_feed'),
  ('Dilkush Kumar',       'manager_feed'),
  ('Chandan Kumar',       'manager_feed'),
  ('Mithun',              'manager_feed'),
  ('Indrajit',            'manager_feed'),
  ('Irfan Gazi',          'manager_feed'),
  ('Jay Mangal',          'manager_feed'),
  ('Shabeer',             'manager_feed'),
  ('Subrata Sardar',      'manager_feed'),
  ('Munna Kumar',         'manager_feed'),
  ('Arun Kumar',          'manager_feed'),
  ('Sahid Gazi',          'manager_feed');

-- Resolve to actual people. Joining through the LIVE operator grant is what keeps this from
-- touching anybody else: a name that does not currently hold an active park operator grant
-- resolves to nothing and is caught by the count check below.
-- ONE ROW PER PERSON, not per grant. A two-park person has two active grants, and the
-- updates below key on (tenant, user) rather than on scope_id, so both of their grants move
-- from a single target row. Keeping the duplicate would also double-count their ticks in the
-- snapshot and make the "unchanged" verification read as CHANGED.
CREATE TEMP TABLE targets ON COMMIT DROP AS
SELECT DISTINCT
       wm.tenant_id,
       wm.workforce_member_id,
       wm.user_id,
       wm.display_name,
       m.new_role
FROM mapping m
JOIN workforce_members wm   ON wm.display_name = m.display_name
JOIN user_scope_grants g    ON g.tenant_id = wm.tenant_id
                           AND g.user_id   = wm.user_id
                           AND g.role      = 'operator'
                           AND g.status    = 'active'
                           AND g.scope_type = 'park';

-- Fail closed on anything unexpected.
--
-- The check is on PEOPLE, not rows: somebody ticked for both parks holds one active grant
-- PER PARK, which is the ordinary multi-park shape (person_access.scope_mode = 'parks'; see
-- the one-park-scope-source rule in AGENTS.md). Mithun is exactly that today -- Channapatna
-- and Coimbatore -- and both of his grants must move, or he would keep an operator grant in
-- one park and hold the new role in the other. What must NOT happen is one display_name
-- resolving to two different PEOPLE, because display_name is not unique by construction.
DO $$
DECLARE
  distinct_names   int;
  distinct_members int;
BEGIN
  SELECT count(DISTINCT display_name), count(DISTINCT workforce_member_id)
    INTO distinct_names, distinct_members
  FROM targets;

  IF distinct_names <> 28 THEN
    RAISE EXCEPTION 'expected 28 mapped people to resolve, got %', distinct_names;
  END IF;
  IF distinct_members <> distinct_names THEN
    RAISE EXCEPTION 'a display_name resolved to more than one person: % members for % names', distinct_members, distinct_names;
  END IF;
END $$;

-- Snapshot every tick BEFORE, so the verification can prove they did not move.
CREATE TEMP TABLE ticks_before ON COMMIT DROP AS
SELECT pma.workforce_member_id, pma.surface, pma.module_key, pma.capabilities
FROM person_module_access pma
JOIN targets t ON t.tenant_id = pma.tenant_id AND t.workforce_member_id = pma.workforce_member_id;

-- ---------------------------------------------------------------------------
-- 1. The role. This is the change.
-- ---------------------------------------------------------------------------
UPDATE user_scope_grants g
SET role = t.new_role
FROM targets t
WHERE g.tenant_id = t.tenant_id
  AND g.user_id   = t.user_id
  AND g.role      = 'operator'
  AND g.status    = 'active'
  AND g.scope_type = 'park';

-- ---------------------------------------------------------------------------
-- 2. The designation, so People / HRMS shows the job rather than "Operator".
--    Advisory only: person_access.designation_code records what the access STARTED as and
--    never overrides the module rows (migration 000219).
-- ---------------------------------------------------------------------------
UPDATE person_access pa
SET designation_code = t.new_role,
    updated_at       = now(),
    row_version      = pa.row_version + 1
FROM targets t
WHERE pa.tenant_id = t.tenant_id
  AND pa.workforce_member_id = t.workforce_member_id;

-- ---------------------------------------------------------------------------
-- 3. The two Cleaning Managers: attendance only.
--    Clock In / Out is a baseline module that no tick can remove
--    (workforce/app/bootstrap_copy.go), but the phone only opens for somebody holding at
--    least one mobile row, so the clock row at `view` is what keeps the app reachable. It
--    grants no permission of its own -- deliberately NOT `oversee`, which would hand them
--    the cross-person Team presence board.
-- ---------------------------------------------------------------------------
DELETE FROM person_module_access pma
USING targets t
WHERE pma.tenant_id = t.tenant_id
  AND pma.workforce_member_id = t.workforce_member_id
  AND t.new_role = 'manager_cleaning'
  AND pma.module_key <> 'clock';

INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
SELECT t.tenant_id, t.workforce_member_id, 'mobile', 'clock', ARRAY['view']::text[], now(), ARRAY[]::text[]
FROM targets t
WHERE t.new_role = 'manager_cleaning'
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO UPDATE
SET capabilities = EXCLUDED.capabilities,
    updated_at   = now();

-- ---------------------------------------------------------------------------
-- 4. Darshan Talwar has left. Revoke the live grant; keep every historical row.
-- ---------------------------------------------------------------------------
UPDATE user_scope_grants g
SET status = 'revoked'
FROM workforce_members wm
WHERE wm.tenant_id = g.tenant_id
  AND wm.user_id   = g.user_id
  AND wm.display_name = 'Darshan Talwar'
  AND g.status = 'active';

-- ---------------------------------------------------------------------------
-- VERIFICATION
-- ---------------------------------------------------------------------------
\echo ''
\echo '=== 1. Role moved, and ticks UNCHANGED for the 26 role-only people ==='
SELECT t.display_name,
       t.new_role,
       CASE WHEN b.n IS NOT DISTINCT FROM a.n THEN 'ticks unchanged ('||COALESCE(a.n,0)||')'
            ELSE 'CHANGED '||COALESCE(b.n,0)||' -> '||COALESCE(a.n,0) END AS ticks
FROM targets t
LEFT JOIN (SELECT workforce_member_id, count(*) n FROM ticks_before GROUP BY 1) b
       ON b.workforce_member_id = t.workforce_member_id
LEFT JOIN (SELECT pma.workforce_member_id, count(*) n
             FROM person_module_access pma
             JOIN targets tt ON tt.tenant_id=pma.tenant_id AND tt.workforce_member_id=pma.workforce_member_id
            GROUP BY 1) a
       ON a.workforce_member_id = t.workforce_member_id
WHERE t.new_role <> 'manager_cleaning'
ORDER BY t.new_role, t.display_name;

\echo ''
\echo '=== 2. The two Cleaning Managers: exactly what was removed ==='
SELECT t.display_name,
       string_agg(b.module_key, ', ' ORDER BY b.module_key) AS modules_removed
FROM targets t
JOIN ticks_before b ON b.workforce_member_id = t.workforce_member_id
WHERE t.new_role = 'manager_cleaning' AND b.module_key <> 'clock'
GROUP BY t.display_name ORDER BY t.display_name;

\echo ''
\echo '=== 3. What the Cleaning Managers hold now ==='
SELECT t.display_name, pma.surface, pma.module_key, pma.capabilities::text
FROM targets t
JOIN person_module_access pma ON pma.tenant_id=t.tenant_id AND pma.workforce_member_id=t.workforce_member_id
WHERE t.new_role = 'manager_cleaning'
ORDER BY t.display_name, pma.surface, pma.module_key;

\echo ''
\echo '=== 4. Nobody is left on the operator role except the held-back V Munna Kumar ==='
SELECT wm.display_name, g.role, g.scope_type, g.status
FROM user_scope_grants g
JOIN workforce_members wm ON wm.tenant_id=g.tenant_id AND wm.user_id=g.user_id
WHERE g.role='operator' AND g.status='active'
ORDER BY wm.display_name;

\echo ''
\echo '=== 5. Darshan Talwar: no live grant remains ==='
SELECT g.role, g.scope_type, g.status
FROM user_scope_grants g
JOIN workforce_members wm ON wm.tenant_id=g.tenant_id AND wm.user_id=g.user_id
WHERE wm.display_name='Darshan Talwar' ORDER BY g.status, g.role;

\echo ''
\if :apply
  \echo '>>> APPLYING'
  COMMIT;
\else
  \echo '>>> DRY RUN -- rolling back, nothing was written'
  ROLLBACK;
\endif
