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

-- +goose StatementBegin
DO $$
DECLARE
  v_tenant      uuid;
  v_moved       int;
  v_leftover    int;
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
  CREATE TEMP TABLE _op_targets ON COMMIT DROP AS
  SELECT DISTINCT wm.tenant_id, wm.workforce_member_id, wm.user_id, wm.display_name, m.new_role
  FROM _op_mapping m
  JOIN workforce_members wm ON wm.display_name = m.display_name
  JOIN user_scope_grants g  ON g.tenant_id = wm.tenant_id AND g.user_id = wm.user_id
                           AND g.role = 'operator' AND g.status = 'active' AND g.scope_type = 'park';

  SELECT count(*) INTO v_moved FROM _op_targets;
  IF v_moved = 0 THEN
    RAISE NOTICE 'no active park operator grants match the mapping; nothing to migrate';
    RETURN;
  END IF;

  -- A display_name resolving to two different PEOPLE would migrate the wrong person.
  IF (SELECT count(DISTINCT workforce_member_id) FROM _op_targets) <> v_moved THEN
    RAISE EXCEPTION 'a display_name resolved to more than one workforce member; refusing to guess';
  END IF;

  SELECT tenant_id INTO v_tenant FROM _op_targets LIMIT 1;

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
    AND wm.display_name = 'Darshan Talwar' AND g.status = 'active';

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
  -- 5. Nobody may be left on the role. If anyone is, this release would ship a console that
  --    still says "Operator", so fail the deploy rather than land it half done.
  -- ---------------------------------------------------------------------
  SELECT count(*) INTO v_leftover FROM user_scope_grants WHERE role = 'operator' AND status = 'active';
  IF v_leftover > 0 THEN
    RAISE EXCEPTION 'operator retirement incomplete: % active operator grant(s) remain', v_leftover;
  END IF;

  RAISE NOTICE 'operator retired: % people moved onto department manager roles', v_moved;
END $$;
-- +goose StatementEnd

-- 6. Retire the designation so "Operator" cannot be picked for anybody new. The row is KEPT
--    rather than deleted: person_access.designation_code is an FK to it, and historical rows
--    must stay readable.
UPDATE public.designation_catalog SET status = 'retired' WHERE designation_code = 'operator';

-- +goose Down
SET lock_timeout = '5s';

UPDATE public.designation_catalog SET status = 'active' WHERE designation_code = 'operator';

-- The Down restores the ROLE and the DESIGNATION only. It deliberately does NOT rebuild the
-- two cleaning managers' deleted module rows or re-grant Darshan: those are decisions, not
-- mechanics, and silently resurrecting removed access on a rollback is worse than leaving it
-- to be re-ticked on People / HRMS. V Munna Kumar's new setup is likewise left in place --
-- he is correctly configured now either way.
UPDATE public.user_scope_grants SET role = 'operator'
WHERE status = 'active' AND scope_type = 'park'
  AND role IN ('manager_feed', 'manager_health', 'manager_cleaning');

UPDATE public.person_access SET designation_code = 'operator', updated_at = now(), row_version = row_version + 1
WHERE designation_code IN ('manager_feed', 'manager_health', 'manager_cleaning');
