# RBAC, Visibility, Seeds and Leadership Assistant Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

- Founder/builder visibility invariant: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` are the
  platform-owner leadership cohort. In local, staging, and production seed/
  provisioning paths they must be granted `role='ceo_internal'`,
  tenant scope, and the RBAC grants needed for every built visible module.
  New features or visible route changes are incomplete until leadership
  seed commands, bootstrap/nav tests, and docs include the module. RBAC-based
  route visibility applies to non-founder operators, not to these five builder
  accounts.
  **A STG (or any) seed is INCOMPLETE until this grant is MATERIALIZED, not
  merely pending.** `auth_pending_email_grants` rows only become an active
  `user_scope_grants` row via the `/auth/session-events` runtime claim path,
  and admin-web Google SSO does not reliably trigger that path on the first
  login after a fresh seed — the observed failure is `403 permission_denied`.
  For STG, run `make seed-stg-9-person-login`
  (`backend/cmd/seed-stg-login-grants`), which materializes the ACTIVE grant
  directly: tenant scope for the 5 leadership accounts, park scope for named
  park staff/operators, keyed by `platformauth.StableSubjectID(issuer,
  firebase_uid)` — the same derivation the backend uses at request time. Verify
  with `make verify-stg-9-person-login` and
  `docs/runbooks/stg-9-person-login-verification.md` before declaring the seed
  done. `docs/runbooks/stg-login-seed-contract.md` is the canonical personnel
  rule this command implements.
  **Leadership log in with Google SSO OR Firebase email/password** (maintainer
  decision 2026-07-24; the prior SSO-only rule is retired — password convention
  `<FirstName>@2026`, maintainer sets the Firebase password). **All 9 accounts,
  leadership included, also need an active `workforce_members` profile**: the
  mobile `/app/bootstrap` hard-requires a profile row and returns
  `403 operator_profile_missing` without one, so leadership could open admin-web
  but got "Couldn't load your workspace" on the Android app until
  `ensureLeadershipMember` (in `seed-stg-login-grants`) created their
  `auth:<uid>` profile. Materialized grant alone is not enough; the profile is
  part of the completion bar.
- Operator scope invariant: no real operator may receive `scope_type='tenant'`.
  Operators belong to exactly one park (`scope_type='park'`, `scope_id=<park
  location_id>`) plus their explicit shed/task assignments. Tenant scope is
  allowed for platform leadership (`ceo_internal`) and director visibility
  roles (`pc_director`, future director aliases) when they must see both parks.
  If a director also needs to execute scanning work, give that person explicit
  operator-style park/task execution assignment; do not make the operator grant
  tenant-wide. STG login seed changes must pass
  `make stg-operator-scope-guard`; if this guard fails, fix the seed source
  instead of relying on downstream task filtering.
- Current active RBAC roles are documented in
  `docs/runbooks/current-active-rbac-roles.md`. Treat roles outside that list
  (for example `director_preventive_care`, `director_breeding`,
  `manager_feed`, `head_health`, `am_growth`) as dormant catalog scaffolding,
  not live STG/mobile personas. Do not grant or document them as current access
  without also shipping backend permission behavior, Android role handling,
  seed docs, and tests in the same change.
- CPT operator-drive rehearsal seed invariant: the committed packet at
  `fixtures/vaccination-cpt-operator-drive-2026-07-23/` is CPT/Channapatna only
  and uses business date `2026-07-23`. Do not synthesize CBE/Coimbatore rows.
  Seed Amit Kumar, Darshan Talwar, and Sagar Mahoor as equal vaccination
  operators with `200` unique animals/day/operator; seed Chandrakant as
  director-only monitoring scope. The `Adult` source filenames do not narrow
  the vaccination kernel: kid/adult/booster/clinical/combo-spacing/safe-window
  rules still come from backend vaccination rules.
  **Materialized grant + department binding is part of this invariant, not a
  separate concern.** Amit, Darshan, Sagar (operator role) and Chandrakant
  (`pc_director` role) are only real, working STG logins once their
  `user_scope_grants` row is `status='active'` AND their existing named
  `workforce_members` roster row (seeded by `seed-roster-real` /
  `seed-vaccination-cpt-operator-drive`) is bound to `user_id` with
  `department_id = preventive_care`, so `department_module_grants` gives them
  the vaccination bottom bar. `make seed-stg-9-person-login` is wired as a
  required final step of `seed-vaccination-source-full` and
  `seed-vaccination-cpt-operator-drive` when `GOATOS_ENV=stg` — do not seed CPT
  operator-drive rehearsal data on STG without it, and do not declare the
  rehearsal seeded until `make verify-stg-9-person-login` passes.
- Leadership assistant coverage invariant: every leadership-relevant table,
  read API, OpenAPI contract, admin-web route, mobile workflow, reporting view,
  domain event, or official KPI must resolve to a Cube governed metric, a
  `ceo_ai.*` view, an MCP Toolbox tool, a mapped Mesha read API, or a documented
  exclusion in `docs/ceo-ai/coverage-matrix.md` — in the same change. The guard
  is STRUCTURED, not keyword-based (tightened 2026-07-23): a new `CREATE TABLE`
  migration, a new OpenAPI `/path`, or a new exported read handler must ship a
  real coverage artifact (`ceo_ai.*` view / MCP tool / Cube binding / wired
  `Set*DataReader`) or a coverage-matrix row/exclusion NAMING that surface in the
  same commit; a bare keyword-bearing doc touch no longer satisfies it, and pure
  refactors pass without a coverage file. The external MCP connector is not a
  raw table/API auto-publisher; it exposes the leadership assistant product
  entrypoint. New tables/APIs become visible through Claude/Codex/CEO chat only
  after they are covered by the Cube/read-API/Toolbox/`ceo_ai`/SQL-fallback
  layer or explicitly excluded. The read-path routing is Cube-first (official
  KPI → Cube; then read APIs → MCP Toolbox `ceo_ai.*` tools → read-only SQL
  fallback). The planner → catalog →
  wiring → reader chain must be LIVE and CLOSED end-to-end (ROUTE-CLOSURE rule):
  every tool name must resolve in the runtime registry (Cube binding, executor spec,
  toolbox tool, or fallback alias), every RouteAPI target must have a wired reader or
  fallback alias, and every coverage row must reference a golden eval question. HOW-TO:
  `.agents/skills/goatos-leadership-assistant/SKILL.md` (includes ROUTE-CLOSURE rules).
  External MCP setup/docs: `docs/ceo-ai/external-mcp-integration.md`.
  Scaffold: `node tools/ceo-ai/scaffold-coverage.mjs <module>`. Enforced by
  `make leadership-assistant-coverage-guard` + `make assistant-route-closure-guard`
  (local CI + PostToolUse nudge for Claude and Codex).
