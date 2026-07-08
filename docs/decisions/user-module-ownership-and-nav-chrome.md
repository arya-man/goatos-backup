# ADR: Department-sourced module/vertical ownership drives visible modules + nav chrome (both surfaces)

Status: accepted (maintainer/CEO decision, 2026-07-09). Applies to **both**
admin-web (dashboard) and goatos-android (mobile). Pairs with the golden frontend
rule (AGENTS.md), `docs/mobile/` nav-chrome edits, and the mobile app-id ADR.

## Context

Manju/CEO (2026-07-09, sw/hw thread):

- The **sidebar/drawer appears only when a principal owns ≥2 features/modules**.
  A single-feature principal (today: the vaccination-only operator and leadership)
  gets **bottom bar only / no sidebar**; the drawer's extras (language, RFID
  reader, notifications, sign out) fold into You/Settings. Backend-driven — the
  client never counts modules or checks role.
- **Certain options must be hidden "based on the login"** — visible nav/settings
  entries are per-principal, decided by the backend.
- **Ownership follows DEPARTMENT**, and department is modeled in the **HR DB**:
  *"I will department to HR DB … all staff including myself."* Departments are
  separate (e.g. the feed team is exclusive).
- **Dark mode is the default theme** (both surfaces); color scheme consistent with
  the website.
- **Standing rule:** such UX/arch decisions apply to both surfaces, from one
  shared backend contract.

Verified gap (2026-07-09): no department concept and no module-ownership model
exist. `user_scope_grants` = role(enum)+org scope only; mobile nav derives from
workforce **capabilities** (skills, a proxy); admin-web nav is **static**
(`adminui/app/service.go:72 navigation()`, filtered by `compileNavigation` ←
`compileRequestContext:230`) so every principal sees the full tree. Grep: zero
`department` in backend migrations/workforce code. Nothing to seed into.

## Decision

**Model department in Goat OS HR (Goat OS is its own HR DB) and derive module
ownership from it.** Ownership chain:

```
workforce_members.department_id ─▶ departments ─▶ department_module_grants
                                                    (which verticals/modules owned)
actor's owned modules = active grants for the actor's department
```

1. **`departments`** — HR department vocabulary (tenant-scoped `code` + `label` +
   status). This is the "HR DB" the maintainer named, kept inside Goat OS.
2. **`workforce_members.department_id`** — the person's department (nullable FK;
   HR record already exists at `workforce_members`).
3. **`department_module_grants`** — department → owned vertical/module (status +
   valid-window, mirrors `user_scope_grants`). Seedable by migration now.
   *(Ownership is per-department, not per-user: departments are the unit of
   ownership. A user inherits their department's modules.)*

4. **Both bootstraps compile visible modules + `nav_chrome` from the department's
   owned modules**, not from role or capabilities:
   - admin-web `compileNavigation` (via `compileRequestContext`): filter the
     static `navigation()` tree to the actor's department's owned verticals/
     modules; hide non-owned groups and the login-hidden options.
   - mobile `workforce` bootstrap: `VisibleNavigation` + `nav_chrome` from owned
     modules (capabilities keep gating feature-flags like `proof_capture`).
5. **`nav_chrome`** — backend-computed contract field on both bootstraps:
   **drawer/sidebar iff `count(active owned visible modules) ≥ 2`, else
   bottom-bar-only (mobile) / no-sidebar (admin-web).** Client renders it; never
   counts/role-checks. Mobile single-feature extras render inside You/Settings.
6. **Seed-first, no UI.** A seed migration inserts departments (vaccination,
   admin_data, leadership…), sets `workforce_members.department_id`, and grants
   department→modules (vaccination dept → `pc.vaccination` only; admin/CEO dept →
   all built modules). Admin/HR UI to edit comes later. Scope-lock: only **built**
   modules are seeded/surfaced (today `pc.vaccination` + `admin.*`).
7. **Dark theme is the default** on both surfaces. Mobile already states it
   (`docs/mobile/design-system.md:13`); admin-web documented in
   `context/frontend/current-admin-web-scope.md`.

## Relationship to existing models (do not conflate)

- `user_scope_grants` = **WHERE** (org scope: park/shed) + coarse role. Unchanged.
- workforce capabilities = **skills**. Unchanged; keep driving feature-flags.
- `departments` / `workforce_members.department_id` = **which HR unit** the person
  is in.
- `department_module_grants` = **which product modules** that department owns.
- RBAC stays **server-authoritative on every command**: ownership hides/shows nav
  + sets chrome; it never widens access.

## Consequences

- Admin-web nav becomes **per-principal** (was static/all). Mock-fidelity + live
  visual QA gate the change; the admin/CEO department seed **must** own every
  built module so the dashboard is not emptied. Highest-blast-radius step.
- `workforce_members` gains `department_id`; the bootstrap member fetch reads it.
- Both bootstrap contracts gain `nav_chrome` + owned-module set; regenerate
  clients; drift check stays green.

## Implementation plan (sequenced)

1. **Migration** `000148` — `departments`, `department_module_grants`,
   `workforce_members.department_id`. (done)
2. **Seed** migration `000149` — departments + grants + members.department_id
   (vaccination dept → pc.vaccination; admin/leadership dept → built module set).
3. **sqlc** + repo `ListActiveModuleGrantsForActor` (member→department→grants).
4. **Contracts** — `nav_chrome` + owned-module set on `admin-api.yaml` +
   `app-api.yaml`; regen clients.
5. **mobile** workforce bootstrap — module-driven `VisibleNavigation` + `nav_chrome`.
6. **admin-web** `compileNavigation` — filter groups by owned modules + `nav_chrome`
   (⚠️ mock-fidelity + visual QA gated).
7. **Tests** — department→nav filter; ≥2⇒sidebar / 1⇒bottom-bar; login-hidden
   options; seeded bootstrap E2E both surfaces; RBAC unchanged.
8. **Verify** — apply 148/149 to dev DB (:55432), `go test`, regen client,
   admin-web visual QA.

## Review follow-ups (2026-07-09)

- **P1 (fixed in 000148):** department references use composite
  `(tenant_id, department_id)` FKs + a matching unique key, so a grant or member
  can never point at another tenant's department (these drive visible nav).
- **P2 (tracked → step 4):** the new ownership tables have no bootstrap-revision
  trigger yet. Wire it in the commit that first makes the bootstraps consume them
  (mirror `000105` `user_scope_grants` → `permissions`), else changing a
  department/grant leaves cached nav stale.
- **P3 (fixed in 000148):** `code`/`vertical` require `^[a-z][a-z0-9_]*$` and
  `module` the dotted `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`
  (cf. `workforce_capabilities_code_check`) — they become bootstrap keys.

## Blast radius (CRG-verified 2026-07-09)

- admin-web: `navigation()` (static) → `compileNavigation()` ← only
  `compileRequestContext` (`compiler.go:230`). Single compile point.
- mobile: `VisibleNavigation = navigationFor(caps)` (`workforce/app/service.go:370`).
- New tables + one nullable column on `workforce_members` — additive, contained.
