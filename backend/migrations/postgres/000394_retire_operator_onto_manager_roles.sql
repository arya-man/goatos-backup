-- +goose Up
-- Retire `operator` as a live role: move every ground operator onto the department manager
-- role they actually hold (maintainer decision 2026-09-23). This is the DATA half of the
-- change 000393 made possible, and it runs on deploy so the console stops saying "Operator"
-- the moment the release lands rather than waiting for somebody to run a script.
--
-- WHY THIS IS SAFE, and it is the whole argument: a person's access is resolved from their
-- own person_module_access rows -- the HRMS ticks -- and NOT from their role
-- (httpmiddleware/auth.go resolves source="person"; capability_backfill.go calls the role
-- maps "DEAD DATA kept for audit"). This migration does not touch those rows for anybody
-- except the two cleaning managers, whose reduction is the one deliberate exception. So for
-- 27 of 29 people nothing they can do changes; only what they are CALLED changes.
--
-- WHAT IT DELIBERATELY DOES NOT TOUCH: workforce_members.primary_role_hint. Every one of the
-- Android app's four role checks reads that field and nothing else -- feed-direction capture,
-- feed-wastage capture, the RFID profile row, and the drawer's roadmap-row filter
-- (ProfileViewModel.kt:72 sets roleLabel = primaryRoleHint, which GoatOsShell.kt:825 then
-- tests). Leaving it as 'operator' is what keeps every ALREADY-INSTALLED phone working
-- byte-for-byte through this release. The hint, those four gates and the phone's own
-- "Operator" wording move together in a later change, once an APK carrying capability flags
-- is out. That is why 000393 keeps 'operator' in the role-hint CHECK.
--
-- IDEMPOTENT AND ENVIRONMENT-SAFE: every statement is keyed on somebody who currently holds
-- an ACTIVE PARK `operator` grant under one of the named display names. On a fresh database,
-- a local stack, or a re-run, nothing matches and the whole migration is a no-op.

SET lock_timeout = '5s';

-- Durable undo identities: names and roles may change after deployment. Keep only
-- the values changed here; revoked access and deleted module ticks stay revoked.
CREATE TABLE public.operator_retirement_000394_undo (
  kind text NOT NULL,
  tenant_id uuid NOT NULL,
  row_id uuid NOT NULL,
  old_value text,
  new_value text NOT NULL,
  PRIMARY KEY (kind, tenant_id, row_id)
);

CREATE TABLE public.operator_retirement_000394_pending_scope_undo (
  tenant_id uuid NOT NULL,
  pending_grant_id uuid NOT NULL,
  old_role text NOT NULL,
  old_scope_type text NOT NULL,
  old_scope_id uuid NOT NULL,
  new_role text NOT NULL,
  new_scope_type text NOT NULL,
  new_scope_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, pending_grant_id)
);

-- +goose StatementBegin
DO $$
DECLARE
  v_tenant      uuid;
  v_moved       int;
  v_leftover    int;
  v_tenants     int;
  v_vm_member   uuid;
  v_vm_park     uuid;
  v_peer_member uuid;
BEGIN
  -- The mapping, as classified by the maintainer. One row per person.
  CREATE TEMP TABLE _op_mapping(display_name text PRIMARY KEY, new_role text NOT NULL) ON COMMIT DROP;
  INSERT INTO _op_mapping VALUES
    -- Health Managers
    ('Amit Kumar','manager_health'), ('Sagar Mahoor','manager_health'),
    ('Kumar Sharath','manager_health'), ('Natheswar','manager_health'),
    ('Naveen','manager_health'), ('Pramod','manager_health'),
    -- Cleaning Managers (reduced to attendance only, below)
    ('Manoj Kumar','manager_cleaning'), ('Mithlesh Kumar','manager_cleaning'),
    -- Feed Managers
    ('Bipin','manager_feed'), ('Bipin Yadav','manager_feed'), ('Dheeraj Singh','manager_feed'),
    ('Mohd Shami','manager_feed'), ('Rajniti Kumar','manager_feed'), ('Ravi Kumbar','manager_feed'),
    ('Santosh Kumar','manager_feed'), ('Santosh Kumar Sahni','manager_feed'),
    ('Manikanth Yadav','manager_feed'), ('Dilkush Kumar','manager_feed'),
    ('Chandan Kumar','manager_feed'), ('Mithun','manager_feed'),
    ('Indrajit','manager_feed'), ('Irfan Gazi','manager_feed'), ('Jay Mangal','manager_feed'),
    ('Shabeer','manager_feed'), ('Subrata Sardar','manager_feed'), ('Munna Kumar','manager_feed'),
    ('Arun Kumar','manager_feed'), ('Sahid Gazi','manager_feed'),
    ('V Munna Kumar','manager_feed');

  -- ONE ROW PER PERSON, not per grant: somebody ticked for both parks holds one active grant
  -- per park (Mithun does today), and the updates key on (tenant, user) so both of their
  -- grants move from a single row. Keeping the duplicate would double-count their ticks.
  -- THE TENANT IS RESOLVED FIRST, and everything below is scoped to it. `display_name` is a
  -- label, not an identity: joining workforce_members on it alone reaches across every tenant
  -- in the database, and this mapping is ONE FARM'S ROSTER. A second tenant employing its own
  -- "Manoj Kumar" would have had him swept into the move -- and, since Manoj is one of the two
  -- Cleaning Managers, reduced to attendance only.
  SELECT count(DISTINCT tenant_id) INTO v_tenants
  FROM (
    SELECT tenant_id FROM user_scope_grants WHERE role = 'operator' AND status = 'active'
    UNION
    SELECT tenant_id FROM auth_pending_email_grants WHERE role = 'operator' AND status = 'active'
  ) operator_tenants;

  IF v_tenants > 1 THEN
    RAISE EXCEPTION 'active operator grants, live or pending, span % tenants; this mapping is one farm''s roster and must not be applied by name across tenants', v_tenants;
  END IF;

  SELECT tenant_id INTO v_tenant
  FROM (
    SELECT tenant_id FROM user_scope_grants WHERE role = 'operator' AND status = 'active'
    UNION
    SELECT tenant_id FROM auth_pending_email_grants WHERE role = 'operator' AND status = 'active'
  ) operator_tenants
  LIMIT 1;

  IF v_tenant IS NULL THEN
    RAISE NOTICE 'no active operator grants at all; nothing to migrate';
    RETURN;
  END IF;

  CREATE TEMP TABLE _op_targets ON COMMIT DROP AS
  SELECT DISTINCT wm.tenant_id, wm.workforce_member_id, wm.user_id, wm.display_name, m.new_role
  FROM _op_mapping m
  JOIN workforce_members wm ON wm.tenant_id = v_tenant AND wm.display_name = m.display_name
  JOIN user_scope_grants g  ON g.tenant_id = wm.tenant_id AND g.user_id = wm.user_id
                           AND g.role = 'operator' AND g.status = 'active' AND g.scope_type = 'park';

  SELECT count(*) INTO v_moved FROM _op_targets;
  IF v_moved = 0 THEN
    -- Nothing matched. A fresh database, a local stack, or a re-run: all no-ops, and correct.
    --
    -- But it is ALSO what a drifted database looks like -- somebody renamed, or a fixture
    -- operator seeded under a name this mapping never had -- and in that case the release
    -- must not go on to retire the designation. That would leave the half-done state this
    -- migration exists to prevent: "Operator" unpickable for anybody new while live operator
    -- grants still resolve. So the designation retire moved INSIDE this block, after the
    -- leftover assertion, and this path skips it.
    --
    -- It says so loudly rather than failing: a throwaway QA database carrying a fixture
    -- operator is not a broken deploy, and hard-failing here would block every local stack
    -- that has one. On STG the mapping matches and the assertion below is the real gate.
    SELECT (SELECT count(*) FROM user_scope_grants          WHERE role = 'operator' AND status = 'active')
         + (SELECT count(*) FROM auth_pending_email_grants WHERE role = 'operator' AND status = 'active')
      INTO v_leftover;
    IF v_leftover > 0 THEN
      RAISE WARNING 'operator retirement skipped: % active operator grant(s), live or pending, match no name in the mapping; the designation stays pickable', v_leftover;
    END IF;
    RETURN;
  END IF;

  -- A display_name resolving to two different PEOPLE would migrate the wrong person, so this
  -- counts DISPLAY NAMES against rows. It used to compare count(DISTINCT workforce_member_id)
  -- against the row count, which could never fail: _op_targets is already DISTINCT over a
  -- tuple containing the member id, so two people sharing a name give two rows AND two
  -- distinct ids, and the check passed on exactly the case it was written to catch. Names are
  -- the thing that can collide here; ids are the thing that cannot.
  IF (SELECT count(DISTINCT display_name) FROM _op_targets) <> v_moved THEN
    RAISE EXCEPTION 'a display name resolved to more than one person in this tenant; refusing to guess which one to move';
  END IF;

  -- ---------------------------------------------------------------------
  -- V Munna Kumar has never been set up: an active grant, but no person_access row, so he
  -- resolves through the ROLE fallback rather than through ticks. Moving his role alone
  -- would be the one case in this migration that really does change somebody's access, so
  -- he is given a working feed manager's setup FIRST, copied from a peer in his own park.
  -- ---------------------------------------------------------------------
  SELECT t.workforce_member_id INTO v_vm_member FROM _op_targets t WHERE t.display_name = 'V Munna Kumar';

  IF v_vm_member IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM person_access pa WHERE pa.tenant_id = v_tenant AND pa.workforce_member_id = v_vm_member) THEN

    SELECT g.scope_id INTO v_vm_park
    FROM user_scope_grants g
    WHERE g.tenant_id = v_tenant AND g.status = 'active' AND g.scope_type = 'park'
      AND g.user_id = (SELECT user_id FROM _op_targets WHERE workforce_member_id = v_vm_member)
    LIMIT 1;

    SELECT t.workforce_member_id INTO v_peer_member
    FROM _op_targets t WHERE t.display_name = 'Arun Kumar';

    IF v_vm_park IS NOT NULL AND v_peer_member IS NOT NULL THEN
      INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode, designation_code, updated_at, row_version)
      VALUES (v_tenant, v_vm_member, 'parks', 'manager_feed', now(), 1);

      INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id)
      VALUES (v_tenant, v_vm_member, v_vm_park)
      ON CONFLICT DO NOTHING;

      INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
      SELECT v_tenant, v_vm_member, p.surface, p.module_key, p.capabilities, now(), p.pages
      FROM person_module_access p
      WHERE p.tenant_id = v_tenant AND p.workforce_member_id = v_peer_member
      ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

      RAISE NOTICE 'V Munna Kumar set up from the Arun Kumar feed manager template';
    ELSE
      RAISE EXCEPTION 'cannot set up V Munna Kumar: park=% peer=%; refusing to move him without working access', v_vm_park, v_peer_member;
    END IF;
  END IF;

  -- ---------------------------------------------------------------------
  -- 1. The role. Both grants move for a two-park person, because this keys on (tenant, user).
  -- ---------------------------------------------------------------------
  INSERT INTO public.operator_retirement_000394_undo
  SELECT 'grant', g.tenant_id, g.grant_id, g.role, t.new_role
  FROM user_scope_grants g JOIN _op_targets t
    ON g.tenant_id = t.tenant_id AND g.user_id = t.user_id
  WHERE g.role = 'operator' AND g.status = 'active' AND g.scope_type = 'park';

  INSERT INTO public.operator_retirement_000394_undo
  SELECT 'designation', pa.tenant_id, pa.workforce_member_id, pa.designation_code, t.new_role
  FROM person_access pa JOIN _op_targets t
    ON pa.tenant_id = t.tenant_id AND pa.workforce_member_id = t.workforce_member_id;

  UPDATE user_scope_grants g
  SET role = t.new_role
  FROM _op_targets t
  WHERE g.tenant_id = t.tenant_id AND g.user_id = t.user_id
    AND g.role = 'operator' AND g.status = 'active' AND g.scope_type = 'park';

  -- ---------------------------------------------------------------------
  -- 2. The designation, so People / HRMS shows the job instead of "Operator".
  -- ---------------------------------------------------------------------
  UPDATE person_access pa
  SET designation_code = t.new_role, updated_at = now(), row_version = pa.row_version + 1
  FROM _op_targets t
  WHERE pa.tenant_id = t.tenant_id AND pa.workforce_member_id = t.workforce_member_id;

  -- ---------------------------------------------------------------------
  -- 3. The two Cleaning Managers: attendance only. This REMOVES modules they hold today and
  --    is the one deliberate access reduction here (maintainer decision 2026-09-23).
  --    Clock In / Out is a baseline module no tick can remove, but the phone only opens for
  --    somebody holding at least one mobile row, so the clock row at `view` is what keeps
  --    the app reachable. Deliberately NOT `oversee`, which carries the cross-person Team
  --    presence board.
  -- ---------------------------------------------------------------------
  DELETE FROM person_module_access pma
  USING _op_targets t
  WHERE pma.tenant_id = t.tenant_id AND pma.workforce_member_id = t.workforce_member_id
    AND t.new_role = 'manager_cleaning' AND pma.module_key <> 'clock';

  INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT t.tenant_id, t.workforce_member_id, 'mobile', 'clock', ARRAY['view']::text[], now(), ARRAY[]::text[]
  FROM _op_targets t WHERE t.new_role = 'manager_cleaning'
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key)
  DO UPDATE SET capabilities = EXCLUDED.capabilities, updated_at = now();

  -- ---------------------------------------------------------------------
  -- 4. Darshan Talwar has left; his member row is already inactive while his grant stayed
  --    live. Revoke it. Nothing is hard-deleted -- his proofs, clock entries and audit rows
  --    are history and stay readable.
  -- ---------------------------------------------------------------------
  UPDATE user_scope_grants g
  SET status = 'revoked'
  FROM workforce_members wm
  WHERE wm.tenant_id = g.tenant_id AND wm.user_id = g.user_id
    AND wm.tenant_id = v_tenant
    AND wm.display_name = 'Darshan Talwar' AND wm.status = 'inactive'
    AND g.status = 'active';

  -- ---------------------------------------------------------------------
  -- 4b. The two TENANT-scoped operator grants, which are not ground operators at all.
  --
  --     Chandrakant and Dinakar are directors. Each carried a tenant-wide `operator` grant
  --     layered on their director roles for one reason: bootstrap_copy.go offered the Herd
  --     Operations (birth / death / shifting) CAPTURE module to holders of that grant, so it
  --     was how the two of them got that module on their phone. That was a recorded decision
  --     (2026-08-07, AGENTS.md "Extended 2026-08-07").
  --
  --     The maintainer RETIRED that decision on 2026-09-23, told that the consequence is
  --     these two lose Herd Operations capture on their phones, and chose it: they do not do
  --     ground capture. The offer branch keyed on the grant goes with it, so this is the data
  --     half of that removal rather than an orphaned revoke.
  --
  --     Their director authority is untouched: pc_director, growth_director,
  --     breeding_director, park_head, counts_approver and toxin_tester all stay exactly as
  --     they are. counts_approver is what lets them APPROVE birth/death/shifting, and that is
  --     a different thing from recording one -- the separation AGENTS.md draws deliberately.
  -- ---------------------------------------------------------------------
  UPDATE user_scope_grants
  SET status = 'revoked'
  WHERE role = 'operator' AND status = 'active' AND scope_type = 'tenant';

  -- ---------------------------------------------------------------------
  -- 4c. THE PENDING GRANTS, which are the other half of every revoke above.
  --
  --     auth_pending_email_grants is a grant waiting for its person to log in:
  --     ClaimPendingEmailGrant materialises it into user_scope_grants the first time that
  --     email authenticates. So revoking the live rows alone retires `operator` only until
  --     the next login -- and the two rows that actually exist on the clone today are
  --     Chandrakant's and Dinakar's, i.e. precisely the two the step above just revoked.
  --     The assertion below would pass, the console would read clean, and the role would
  --     walk back in the next morning.
  --
  --     A pending ground-manager grant is MIGRATED, not revoked: it is a ground manager whose
  --     setup is still waiting on a first login, and they should land on the right role rather
  --     than on nothing. Historically these pending rows were tenant-scoped because the table
  --     allowed no other shape, so this step also narrows them onto the person's active park
  --     grant. Everything still on `operator` after that -- the two tenant director rows, and
  --     a leaver like Darshan -- is revoked, which is the same treatment their live rows got.
  -- ---------------------------------------------------------------------
  --     THE PARK IS AGGREGATED, NOT min()'d. PostgreSQL has no min(uuid) -- uuid carries the
  --     comparison operators an ORDER BY needs but no ordering AGGREGATE -- so `min(g.scope_id)`
  --     does not fail on a database that happens to have no pending rows: it fails at PARSE
  --     time, unconditionally, on every environment including a fresh one. Ordering inside
  --     array_agg is the uuid-safe form.
  CREATE TEMP TABLE _op_pending_targets ON COMMIT DROP AS
  SELECT p.tenant_id, p.pending_grant_id, p.role AS old_role, p.scope_type AS old_scope_type,
         p.scope_id AS old_scope_id, t.new_role,
         array_agg(DISTINCT g.scope_id) AS park_ids
  FROM auth_pending_email_grants p
  JOIN workforce_members wm ON p.tenant_id = wm.tenant_id AND lower(wm.email) = p.normalized_email
  JOIN _op_targets t ON t.tenant_id = wm.tenant_id AND t.workforce_member_id = wm.workforce_member_id
  JOIN user_scope_grants g ON g.tenant_id = t.tenant_id AND g.user_id = t.user_id
                           AND g.role = t.new_role AND g.status = 'active' AND g.scope_type = 'park'
  WHERE p.role = 'operator' AND p.status = 'active'
  GROUP BY p.tenant_id, p.pending_grant_id, p.role, p.scope_type, p.scope_id, t.new_role;

  --     AND THE AMBIGUITY THAT CAN ACTUALLY HAPPEN IS THE PARK, not the person. A pending row
  --     joins to one member by email and a member carries one new_role, so the GROUP BY above
  --     already yields exactly one row per pending_grant_id -- the previous
  --     `HAVING count(*) > 1` over that same grouping could never fire, the same vacuous shape
  --     the display-name check had. What genuinely has more than one value is the PARK: a
  --     two-park person (Mithun holds CBE and CPT today) has two active park grants, and
  --     silently taking the lower uuid would narrow their invite to one park and drop the
  --     other. A pending invite carries ONE scope, so this refuses rather than guesses.
  IF EXISTS (SELECT 1 FROM _op_pending_targets WHERE array_length(park_ids, 1) > 1) THEN
    RAISE EXCEPTION 'a pending operator invite resolves to more than one park for the same person; refusing to guess which park to write into the invite';
  END IF;

  INSERT INTO public.operator_retirement_000394_pending_scope_undo
  SELECT tenant_id, pending_grant_id, old_role, old_scope_type, old_scope_id,
         new_role, 'park', park_ids[1]
  FROM _op_pending_targets;

  UPDATE auth_pending_email_grants p
  SET role = t.new_role,
      scope_type = 'park',
      scope_id = t.park_ids[1],
      updated_at = now()
  FROM _op_pending_targets t
  WHERE p.tenant_id = t.tenant_id
    AND p.pending_grant_id = t.pending_grant_id
    AND p.role = 'operator' AND p.status = 'active';

  UPDATE auth_pending_email_grants
  SET status = 'revoked', updated_at = now()
  WHERE tenant_id = v_tenant AND role = 'operator' AND status = 'active';

  -- ---------------------------------------------------------------------
  -- 5. Nobody in this farm may be left on the role. If anyone is, this release would ship a
  --    console that still says "Operator", so fail the deploy rather than land it half done.
  -- ---------------------------------------------------------------------
  SELECT (SELECT count(*) FROM user_scope_grants          WHERE tenant_id = v_tenant AND role = 'operator' AND status = 'active')
       + (SELECT count(*) FROM auth_pending_email_grants WHERE tenant_id = v_tenant AND role = 'operator' AND status = 'active')
    INTO v_leftover;
  IF v_leftover > 0 THEN
    RAISE EXCEPTION 'operator retirement incomplete: % active operator grant(s) remain, live or pending', v_leftover;
  END IF;

  -- ---------------------------------------------------------------------
  -- 6. Retire the designation so "Operator" cannot be picked for anybody new. The row is KEPT
  --    rather than deleted: person_access.designation_code is an FK to it, and historical
  --    rows must stay readable.
  --
  --    This sits INSIDE the block, after the assertion above, on purpose: the designation is
  --    retired only once the retirement actually completed. The early RETURN further up skips
  --    it, so a database whose operator grants this mapping does not cover keeps a pickable
  --    "Operator" rather than ending up unable to name a role its own people still hold.
  -- ---------------------------------------------------------------------
  UPDATE public.designation_catalog SET status = 'retired' WHERE designation_code = 'operator';

  RAISE NOTICE 'operator retired: % people moved onto department manager roles', v_moved;
END $$;
-- +goose StatementEnd

-- +goose Down
SET lock_timeout = '5s';

UPDATE public.designation_catalog SET status = 'active' WHERE designation_code = 'operator';

-- The Down restores the ROLE and the DESIGNATION only. It deliberately does NOT rebuild the
-- two cleaning managers' deleted module rows or re-grant Darshan: those are decisions, not
-- mechanics, and silently resurrecting removed access on a rollback is worse than leaving it
-- to be re-ticked on People / HRMS. V Munna Kumar's new setup is likewise left in place --
-- he is correctly configured now either way.
--
-- Restore exact rows and prior values, only while they still carry our value.
-- A new grant, renamed person, or same-name person in another tenant is not a target.
UPDATE public.user_scope_grants g SET role = u.old_value
FROM public.operator_retirement_000394_undo u
WHERE u.kind = 'grant' AND g.tenant_id = u.tenant_id AND g.grant_id = u.row_id
  AND g.status = 'active' AND g.role = u.new_value;

UPDATE public.auth_pending_email_grants p
SET role = u.old_role,
    scope_type = u.old_scope_type,
    scope_id = u.old_scope_id,
    updated_at = now()
FROM public.operator_retirement_000394_pending_scope_undo u
WHERE p.tenant_id = u.tenant_id
  AND p.pending_grant_id = u.pending_grant_id
  AND p.status = 'active'
  AND p.role = u.new_role
  AND p.scope_type = u.new_scope_type
  AND p.scope_id = u.new_scope_id;

UPDATE public.person_access pa
SET designation_code = u.old_value, updated_at = now(), row_version = pa.row_version + 1
FROM public.operator_retirement_000394_undo u
WHERE u.kind = 'designation' AND pa.tenant_id = u.tenant_id AND pa.workforce_member_id = u.row_id
  AND pa.designation_code = u.new_value;

DROP TABLE public.operator_retirement_000394_undo;
DROP TABLE public.operator_retirement_000394_pending_scope_undo;
