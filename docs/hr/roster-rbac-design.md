# HRMS / Roster / RBAC Design — Position &amp; Coverage Model

Date: 2026-07-10

Status: **PROPOSAL.** This document is design only. It does not create a
migration, schema change, or app code. Everything under "Proposed schema" is a
sketch for maintainer review, not a committed contract. Sections describing
tables that already exist in committed migrations are marked **(existing)**
and must not be re-built.

Grounded in two real source registers pulled via the `goatos-dev` read-only
Google Sheets path (raw JSON kept outside this repo at
`/Users/ravi/mesha/source-material/vgoats-seed/`, gitignored, never committed —
see the Sources section): the Attendance DB (designation/salary/DOJ/location +
per-day attendance) and the Staff-Timetable (shift/CBE-CPT-person-per-role/
week-offs/backup). Per repo convention
(`goatos/AGENTS.md`: "keep committed project docs role-based rather than
person-based"), **no person name or email appears anywhere below** — every
example uses a role/position label. Where the source sheets show a person
assigned to a role, this document describes only the shape of that assignment,
never the person.

## Confirmed decisions (maintainer, 2026-07-10)

Two shape questions this document originally flagged as pending are now
**confirmed, not open**. Both were modeled generically from the start
specifically so no redesign is needed once decided — the schema/rule engine in
§4 already expresses exactly this:

1. **Backup is genuinely two-tier, and the coverage engine is generic across
   both.** Manager-tier positions (Preventive Care Manager, Breeding Manager,
   Feeding Manager, Health/Kidding Manager, Goats Head) fall back to a single
   shared **Backup Manager** position per center. Operator/assistant-tier
   positions (Feeding AM1/2/3, Cleaning AM1/2, Milk AM1/2, Breeding AM,
   Health/Kidding AM1/2, Packaging AM1/2, Trainer AMs) fall back to their
   **own** fixed Backup AM1/Backup AM2 positions — never the Backup Manager.
   §4.3's `backup_group_code` resolution is the confirmed model: the coverage
   engine resolves **the configured backup for any covered position** via one
   generic lookup, not a hardcoded "exactly one Backup Manager" special case —
   which is exactly why it needs no change now that this is decided.
2. **Coverage fires on both ad-hoc leave and the recurring per-position
   Week-OFF day.** Both are confirmed, real triggers, not just the ad-hoc one.
   §4.5 models both: the recurring `week_off_weekday` (derived, read-time, no
   row written) and the ad-hoc `workforce_absences` path (a written
   reassignment record) both resolve to the same `effective_backup` lookup.

## 0. Maintainer corrections folded into this design (2026-07-10)

Two rounds of correction from the maintainer are folded in below; both are
still in force and are restated here so a future reader does not have to dig
through history to find them.

### 0.1 Coverage goes to a fixed backup, never a cross-functional manager

An earlier draft of this document modeled leave coverage as "temporary role
assignment between managers" (e.g. "the Feed Manager temporarily also holds
Vaccination"). **That model is wrong and is dropped.** Functional managers
never cross-cover another vertical: a Feed Manager does not vaccinate; a
Breeding Manager does only breeding. The corrected model:

- Each staff member holds a **fixed, permanent position** (e.g. Preventive
  Care Manager, Feeding Manager, Breeding Manager). It is never mutated by a
  leave event.
- Each scope has a **fixed backup position per covered-position group** (see
  the two-tier note above and §4.3) — a fixed position held by a fixed staff
  member, not a rotating pick and never another functional manager.
- When **any** covered position goes on leave (or hits its recurring week-off
  — see §4.5), its scope's configured backup for that position's group — and
  only that backup — covers its active/due work for the window. No other
  functional position is ever auto-assigned as cross-cover.
- The absent holder's **ownership is unchanged**. Only their active/due tasks
  are reassigned, and only for the coverage window.
- The backup holder receives a **temporary work-execution context** ("Covering
  Vaccination until Friday"), with execution permission for the covered tasks
  granted only for that window.
- If the configured backup is also unavailable, the system **escalates to the
  Park Head**. It never auto-picks another functional position as a fallback.

This aligns with two standing repo principles, cited verbatim:

- "Ownership, custody, task assignment, and location are four different
  things... workforce assignment says who does today's work"
  (`.agents/skills/goatos-build/references/architecture.md:36`).
- "Absence/backfill creates a reassignment record; it does not overwrite
  original ownership." (`context/architecture/final-architecture.md:146`).

**Verbatim rule this document commits to** (state identically in any future
TRD/handoff that implements this):

> manager on leave → find fixed Backup Manager for that scope → assign that
> manager's due work to Backup Manager for date range → notify Backup Manager +
> Park Head → restore normal routing after leave ends. If Backup Manager also
> unavailable → escalate to Park Head. Do not assign another functional manager
> by default.

### 0.2 Three separate axes, center-scoped, CEO-superuser as independent config

A second correction, after pulling the real sheets more closely:

- **Scope unit is the center**, not an abstract park/shed: **Bangalore (HQ —
  CXOs and Directors), CBE, and CPT.** Roster, position, and coverage are all
  scoped by center.
- Keep **three separate axes** distinct in the model (this reinforces
  "ownership ≠ position" from §0.1, taken one step further):
  - **(a) HR Designation grade** — the coarse pay-grade tier from the
    Attendance DB's Employee Master `Designation` column: `CXO`, `Director`,
    `Manager`, `Assistant Manager`.
  - **(b) Operational Position** — the fine-grained Staff-Timetable role:
    `Preventive Care Manager`, `Breeding Manager`, `Feeding Manager`,
    `Health/Kidding Manager`, `Backup Manager`, `Goats Head`, `Park Head`,
    `Feeding AM1/AM2/AM3`, `Cleaning AM1/AM2`, `Milk AM1/AM2`, `Backup
    AM1/AM2`, `Trainer AM1/AM2/AM3`, and so on.
  - **(c) Department ownership** — the existing `departments`/
    `department_module_grants` chain (§1): `Procurement`, `Preventive Care`,
    `Breeding`, `Health`, `Growth`, `Infrastructure`, `Feed`, `Milk`, `Sales`.
  - These three axes are **independent**: a person's Designation grade does
    not imply a Position, and a Position does not imply which department owns
    which product modules. §4.1 keeps them as separate fields/tables rather
    than folding Position into Department (an earlier draft's mistake).
- **CEO-superuser is an explicit config superset, not derived from grade.** A
  person whose HR Designation is `Director` (or any other grade) may or may
  not also be a CEO-superuser — it is a separately seeded allow-list (§3.1),
  never computed from Designation. The two may overlap for some people; grade
  alone never grants it.

## 1. What already exists — do not re-build this

Goat OS already has more of this than a fresh design would assume. The
proposal in §4 is deliberately small because it reuses the following
**already-committed** tables and decisions:

| Existing capability | Table / doc | What it gives us |
| --- | --- | --- |
| Department vocabulary + module ownership | `departments`, `department_module_grants` (migration `000148`), seeded in `000149` | `workforce_members.department_id → departments → department_module_grants` already drives which product modules/verticals a person's department owns, and compiles into `nav_chrome` (sidebar iff ≥2 owned modules) on both admin-web and mobile bootstraps. See `docs/decisions/user-module-ownership-and-nav-chrome.md`. Seeded departments today: `vaccination` (1 module → no sidebar), `admin_data` (3 modules → sidebar), `leadership` (4 modules, all built modules → full command room). This is axis (c) only — the real org has 9 departments (§0.2); only the built-module subset is seeded, by scope-lock. |
| Coarse RBAC role + scope | `user_scope_grants` (migration `000001`) | `role IN ('admin','park_head','operator','verifier','ceo_internal')` + `scope_type`/`scope_id` + `valid_from`/`valid_to`. The **CEO-superuser tier already has a real role value: `ceo_internal`.** This document does not invent a new superuser concept — it grants existing `ceo_internal` at `scope_type='tenant'` to the founder/CEO cohort, independently of any HR Designation grade (§0.2). |
| Email-based provisioning | `auth_pending_email_grants` (migration `000023`, extended in `000150`) | An approved email carries `role` + `scope_type`/`scope_id` + (as of `000150`) a nullable `department_code`. On claim, the permissions layer upserts a `workforce_member` into that department, making the department→module ownership chain real for leadership/admin actors who never get a roster row otherwise. |
| Staff roster identity | `workforce_members` (migration `000050`) | `display_code`, `display_name`, `status`, `primary_role_hint` (coarse: operator/park_head/verifier/supervisor/admin/other — an app-permission hint, NOT the same as HR Designation grade or Operational Position), `primary_location_id`, `department_id`. This is the person row every table below hangs off. |
| Skills / capabilities, scope + time-bounded | `workforce_capabilities`, `workforce_member_capabilities` (migration `000050`) | Already scope-bounded (`scope_type`/`scope_id`) and time-bounded (`valid_from`/`valid_to`, one active row per member+capability+scope via a partial unique index). This is the exact shape a **temporary execution grant** needs — see §4.6, which reuses this table rather than inventing a new one. |
| Shift roster | `workforce_roster_assignments` (migration `000050`) | Per-member, per-scope, per-shift-date rows with `shift_start_at`/`shift_end_at`, `task_type`, `status`, and an `escalation_owner_user_id` — this is where the Park-Head escalation target for a shift already lives structurally. |
| Absence + replacement pointer | `workforce_absences` (migration `000050`) | Already has `workforce_member_id`, `scope_type`/`scope_id`, `starts_at`/`ends_at`, `reason_code`, `status` (`reported`/`approved`/`rejected`/`canceled`), and — critically — **`replacement_member_id`**, a nullable FK to `workforce_members`. This is the real, already-built "who covers this absence" pointer for **ad-hoc** leave. The gap this document closes is *which value is allowed to go in `replacement_member_id`* (§4.5), plus the **recurring** week-off coverage case this table does not model at all (it is a date-range table, not a weekly-recurrence one) — see the Confirmed Decisions section above.

**Consequence for this design:** the only genuinely new concepts below are (1)
the **fixed Operational Position** (axis b) as its own standing-title concept,
kept separate from Designation grade and Department ownership, and (2) the
**generalized backup-group** resolution that covers both the manager tier and
the assistant tier's parallel AM1/AM2 split. Leave, coverage-pointer, and
temporary execution permission all reuse existing tables.

## 2. Source grounding (roles only, no names)

Pulled 2026-07-10 via `gcloud auth application-default print-access-token` +
the Sheets API against the two spreadsheets named in this task. Structure
observed (values, not names, reproduced below):

**Staff-Timetable spreadsheet** — `Park-Departments` tab lists the org's 9
functional departments (axis c): Procurement, Preventive Care, Breeding,
Health, Growth, Infrastructure, Feed, Milk, Sales. `Hirarchy` tab: `Director →
Heads → Managers` — the coarse HR-grade tiers (axis a), consistent with
`CXO`/`Director`/`Manager`/`Assistant Manager` values observed in the
Attendance DB's `Designation` column below. A per-department roster tab (e.g.
`Goats-Team-v1`) has exactly these columns: **Role (Operational Position, axis
b), Shift (time), CBE (person assigned at the Coimbatore center), CPT (person
assigned at the Channapatna center), Week OFFs (a recurring weekday), Backup**.
Two facts fall directly out of this tab's actual rows:

- The `Backup` column's value is almost always **another Position label**
  ("Backup AM1", "Backup Manager"), not a person — the source data itself
  already encodes "coverage is a fixed position, not a person picked at leave
  time."
- The backup mapping is **two-tier, and the assistant tier splits into two
  parallel groups**: Feeding AM1/AM2/AM3, Cleaning AM1/AM2, and Milk AM1 all
  map to **Backup AM1**; Breeding AM, Health/Kidding AM1/AM2, and Packaging
  AM1 map to **Backup AM2**; every manager-tier row (Health/Kidding Manager
  1/2, Breeding Manager, Preventive Care Manager, Feeding Manager, Goats Head)
  maps to the single **Backup Manager**. `Park Head`'s own Backup cell reads
  `--` (no backup within-center — Park Head is itself the top of the local
  escalation chain per §0.1's rule).

The Director-tier rows at the bottom of the same tab enumerate one Director
role per department (Breeding Director, Health Director, the
vaccination/deworming/shifting Director, Infrastructure Director, Procurement
Director, Sales Director, Feeding Director, Farming Director, HR Director)
plus a cross-cutting "Common Work" row (meeting with video verifiers, stock
verification, SOP tracking/setting, shiftings) every Director shares. The
`Preventive-Care-Director` tab is a pure responsibilities/SOP-cadence sheet
(vaccination, deworming, bio-security, feed/water testing, sanitization, fire
safety, video-verification cross-check, team building; weekly cadence: plan
Monday, arrange logistics Tuesday, execute Wednesday–Saturday) — the
Department Director owns the *process*, the roster tab's per-center rows own
*execution*.

**Attendance DB spreadsheet** — `Employee Master` tab columns: SL No, Name,
Gender, DOB, **Designation** (axis a: CXO/Director/Manager/Assistant Manager
among the values observed), **Location** (center: Bangalore for the HQ/CXO/
Director rows observed, plus CBE/CPT for center-based staff), Salary, **DOJ**,
Contact Num, Emergency Num, Status. Monthly attendance tabs (e.g.
`February-26`) add: Basic Salary, Incentive, Type, Designation Type,
Designation, Location, DOJ, one column per calendar day (numeric attendance
value), Total Days, Adv, PTAX, TDS, Salary to Pay, IFSC Code, Bank Account
Number, Status. `By-Category` tab aggregates salary by org category:
Cofounder, Farm Operations, Hardware, Software, Trading Operations — Goat OS
staff span farm + tech + trading; this document's scope is the **farm
operational roster** (the Park-Departments departments across the three
centers), not payroll or the tech/trading headcount.

These two registers are exactly what the task brief described (designation =
role; shift/backup/week-off timetable) and confirm both maintainer
corrections: coverage is role/position-level, not a leave-time pick among
functional peers, and Designation/Position/Department are three genuinely
separate vocabularies in the source data itself, not three names for the same
thing.

## 3. RBAC model

### 3.1 CEO / founder superuser tier

- Uses the **existing** `user_scope_grants.role='ceo_internal'` at
  `scope_type='tenant'`. This role already exists in the committed CHECK
  constraint; nothing new is required to express "sees and does everything."
- Also placed in the **existing** `leadership` department (migration `000149`
  seeds `leadership` owning all four built modules: `pc.vaccination`,
  `admin.config`, `admin.sop`, `admin.audit`) so the nav-chrome module-count
  rule naturally gives this tier the full sidebar/command-room chrome — no
  special-case nav logic needed.
- **This is an explicit config superset over HR Designation grade, never
  derived from it** (§0.2). A person graded `Director` in the Employee Master
  is not automatically a CEO-superuser; conversely, being a CEO-superuser does
  not require being graded `CXO`. The two lists are seeded/maintained
  independently.
- Provisioning path: an approved row in `auth_pending_email_grants` with
  `role='ceo_internal'`, `scope_type='tenant'`, `department_code='leadership'`.
  On claim, the existing permissions layer upserts the `workforce_member` into
  `leadership`, making the ownership chain real. This is a **data/config
  action against existing schema** (approve N email rows), not a migration.
- **Initial cohort size:** 5 accounts (the founder/CEO/COO tier named in the
  task brief). The exact identities are an operational seed decision for the
  maintainer to action via the existing email-grant flow — this document
  intentionally does not list them, per the role-based-not-person-based repo
  convention.

### 3.2 Single-vertical operators (no sidebar)

- Unchanged from the existing ADR (`docs/decisions/user-module-ownership-and-nav-chrome.md`):
  a department owning exactly one built module (today: `vaccination` →
  `pc.vaccination`) gets bottom-bar-only / no-sidebar chrome; extras fold into
  You/Settings. A department owning ≥2 modules gets the sidebar/drawer. This
  document does not change that rule — it only adds the position/coverage
  layer on top of the same department-driven ownership.

## 4. Position & Coverage model (proposal)

Renamed from "Role Assignment" per the maintainer correction — this is not
about reassigning roles, it is about a fixed position plus a bounded,
reversible work-coverage window, kept as its own axis (b) separate from HR
Designation grade (axis a) and Department ownership (axis c).

### 4.1 Three axes, modeled as three separate fields/tables

| Axis | Example values | Where it lives |
| --- | --- | --- |
| (a) HR Designation grade | CXO, Director, Manager, Assistant Manager | Proposed: a small lookup + `workforce_members.hr_designation_grade` (or an equivalent lookup table) sourced from the Employee Master `Designation` column. Distinct from `primary_role_hint` (existing, coarse app-permission hint) and from Operational Position below. |
| (b) Operational Position | Preventive Care Manager, Breeding Manager, Feeding Manager, Health/Kidding Manager, Backup Manager, Goats Head, Park Head, Feeding AM1/2/3, Cleaning AM1/2, Milk AM1/2, Backup AM1/AM2, Trainer AM1/2/3 | Proposed: `workforce_positions` (§4.2) — center-scoped, fixed, never mutated by leave. |
| (c) Department ownership | Procurement, Preventive Care, Breeding, Health, Growth, Infrastructure, Feed, Milk, Sales | **Existing:** `departments` + `department_module_grants` + `workforce_members.department_id` (migration `000148`/`000149`). Not touched by this proposal. |

`workforce_positions` deliberately carries **no** `department_id` foreign key.
The real timetable shows a Backup Manager covering managers across multiple
departments at once (Preventive Care, Breeding, Feeding, Health/Kidding all
fall back to the same Backup Manager slot) — Position and Department are
orthogonal, so baking a department FK into the position table would silently
reintroduce the axis conflation this correction removes. Where a position
happens to correspond 1:1 with a department (e.g. "Preventive Care Manager"
↔ the `preventive_care` department) that correspondence is informal/by
convention, resolved by an app-service lookup when needed (e.g. to compile
`effective_owner`, §4.5) — not a schema constraint.

### 4.2 Fixed position, center-scoped (proposed: `workforce_positions`)

```text
workforce_positions (PROPOSED)
  position_id            uuid PK
  tenant_id              uuid FK -> tenants
  workforce_member_id    uuid FK -> workforce_members
  scope_type             text  ('tenant'|'center')   -- centers: Bangalore (HQ), CBE, CPT
  scope_id               uuid                        -- the center's location row
  position_code          text  (e.g. 'preventive_care_manager', 'feeding_manager',
                                 'breeding_manager', 'health_kidding_manager_1',
                                 'goats_head', 'park_head', 'feeding_am1',
                                 'cleaning_am2', 'milk_am1', 'backup_manager',
                                 'backup_am1', 'backup_am2', ...)
  position_tier          text  ('assistant'|'manager'|'head'|'director'|'cxo')
  is_backup_slot         boolean  (true only for backup_manager/backup_am1/backup_am2-style rows)
  backup_group_code      text NULL  (see §4.3 -- the covered-position group this
                                      position either belongs to, or (if is_backup_slot)
                                      the group it covers)
  week_off_weekday       text NULL  ('monday'..'sunday' -- the recurring day-off
                                      the real timetable's "Week OFFs" column carries;
                                      see §4.5)
  status                 text  ('active'|'inactive'|'ended')
  valid_from             timestamptz
  valid_to               timestamptz NULL
  created_by             uuid NULL
  created_at / updated_at timestamptz

  -- exactly one ACTIVE holder per (scope, position_code):
  UNIQUE (tenant_id, scope_type, scope_id, position_code)
    WHERE status = 'active'
  -- mirrors department_module_grants_active_unique's partial-unique pattern (000148)
```

This single partial-unique index is what makes "exactly one holder of a given
Position per center at a time" structural, not a convention someone can
violate by accident. It does **not** by itself force "exactly one Backup
Manager" — that emerges from the confirmed `backup_group_code` resolution
below, which is deliberately generalized (Confirmed Decisions, item 1, above).

### 4.3 Two-tier, group-based backup resolution (generalized, not hardcoded)

Every covered position (assistant or manager tier) declares a
`backup_group_code` (e.g. `'am1_backup'`, `'am2_backup'`, `'manager_backup'`).
Every backup-slot position (`is_backup_slot=true`) declares the **same**
`backup_group_code` for the group it covers. Resolution is then one lookup,
independent of tier count or shape:

```text
effective_backup(covered_position) =
  the active workforce_positions row in the same scope with
  is_backup_slot = true AND backup_group_code = covered_position.backup_group_code
```

This is why no tier is hardcoded to "exactly one backup": the **manager**
tier has one confirmed group (`manager_backup` → the single Backup Manager
per center), while the **assistant** tier is confirmed to split into two
parallel groups (`am1_backup` → Backup AM1, covering Feeding AM1/2/3 +
Cleaning AM1/2 + Milk AM1; `am2_backup` → Backup AM2, covering Breeding AM +
Health/Kidding AM1/2 + Packaging AM1), matching the real CBE/CPT timetable
exactly (§2). This two-tier shape is now a **confirmed requirement**, not a
hypothetical — the coverage engine must resolve the configured backup for any
covered position generically, exactly as `backup_group_code` resolution does;
it must never assume every covered position shares one backup slot.

A scope may have **no** active row for a given `backup_group_code` (per §2:
CPT's Backup Manager slot is configured today, CBE's observed value was
empty). `effective_backup` then returns nothing, and §4.6's escalation rule
applies — it never falls back to picking an unrelated position.

### 4.4 HR Designation grade is informational, not an access lever

Axis (a) (`workforce_positions`' sibling, the proposed `hr_designation_grade`)
is read-only context (badges, org-chart display, payroll-adjacent reporting)
in this proposal. It does not drive nav-chrome (that's Department ownership,
§1), does not drive execute permission (that's Position + the temporary grant
in §4.6), and does not drive CEO-superuser status (that's the explicit
allow-list in §3.1). Keeping it inert avoids the exact conflation §0.2
corrected — grade, position, and department stay three independent reads.

### 4.5 Coverage triggers: recurring week-off AND ad-hoc leave

Two distinct triggers feed the same `effective_backup` resolution (§4.3), and
**both are confirmed, not optional** — coverage must fire on either one:

- **Recurring week-off (derived, no row written).** Every position carries a
  `week_off_weekday` (§4.2), sourced directly from the timetable's "Week OFFs"
  column. On any date whose weekday matches, `effective_owner` (§4.7) routes
  to that position's `effective_backup` for that single day — computed at
  read time, not by writing a `workforce_absences` row every week. This
  avoids an absurd 52-rows-a-year-per-position write load for a fact that is
  already fully described by one column on the position.
- **Ad-hoc leave (existing `workforce_absences` table, §1).** The only new
  rule is a **service-layer invariant** on `replacement_member_id`:

  > When the absent `workforce_member_id` holds an active `workforce_positions`
  > row, `replacement_member_id` MUST resolve to `effective_backup` (§4.3) for
  > that position in that scope. The absence-approval service looks this up
  > and fills it automatically — it is not operator-chosen. If
  > `effective_backup` returns nothing, `replacement_member_id` stays NULL and
  > the absence is flagged `escalation_required` (§4.7) instead of silently
  > leaving the work uncovered.

  This is a **service-level check** (a join across `workforce_positions`), not
  a plain `CHECK` constraint — Postgres CHECK constraints cannot express "this
  FK must equal the result of a lookup query." The invariant belongs in the
  same service that approves `workforce_absences` rows.

Ownership itself never changes under either trigger: the covered position's
`workforce_positions` row stays `active` throughout. Nothing in
`department_module_grants` or `workforce_positions` is touched by a week-off
or an absence — only a `workforce_absences` row is written/approved for the
ad-hoc case (the recurring case writes nothing at all), exactly matching
"absence/backfill creates a reassignment record; it does not overwrite
original ownership."

### 4.6 Temporary execution permission (reuses `workforce_member_capabilities`)

No new grants table. When a coverage window is established — whether from an
approved absence with a resolved `replacement_member_id`, or (for the
recurring week-off case) computed just-in-time for that single day — the
service inserts one row into the **existing** `workforce_member_capabilities`
table for the backup holder:

```text
workforce_member_capabilities (existing table, new row, no schema change)
  workforce_member_id = <backup holder's member id>
  capability_id        = the covered position's execute capability
                         (e.g. 'vaccination.execute', already seeded in 000050)
  scope_type / scope_id = the covered position's scope (the center)
  valid_from           = the coverage window start (absence.starts_at, or the
                          single week-off day's start)
  valid_to             = the coverage window end (absence.ends_at, or that
                          same day's end)
  status               = 'active'
```

Because `workforce_member_capabilities` is already time-bounded and
scope-bounded, this grant **expires on its own** when the window ends — no
cleanup job, no manual revoke. The backup holder's own permanent position and
capabilities are untouched; this is an additional, separate, temporary row
alongside them.

### 4.7 Escalation when the configured backup is also unavailable

If the resolved backup (§4.3) also has an overlapping approved
`workforce_absences` row (or the same recurring week-off) for the same
window, the absence-approval service does **not** search for another
position. It sets the original coverage need to `escalation_required` and
routes to the Park Head (`escalation_owner_user_id` on the relevant
`workforce_roster_assignments` rows, or the center's `park_head`-position
holder). This is the literal "If Backup Manager also unavailable → escalate
to Park Head. Do not assign another functional manager by default" rule from
§0.1.

### 4.8 Vaccination roster tie (concrete worked example)

"The person holding the Vaccination assignment on a given day owns that day's
drive" resolves as a read, not a write:

```text
effective_owner(position='preventive_care_manager', center, date) =
  1. find the active workforce_positions row for (center, position_code)
  2. if date.weekday() == that row's week_off_weekday
       -> effective owner = effective_backup(that row) (§4.3),
          shown as "Covering Preventive Care until <next non-off day>"
  3. else if an approved workforce_absences row for that holder covers `date`
     AND has a resolved replacement_member_id
       -> effective owner = replacement_member_id,
          shown as "Covering Preventive Care until <absence.ends_at>"
  4. else -> effective owner = the position holder from step 1
```

This is exactly how the Staff-Timetable sheet already models it in practice
(§2): the roster tab's per-center row names who executes today, the Week OFFs
column is a recurring fact, and the `Backup` column is a role, not a
leave-time guess. Drive/execution UI (Story C/L's real proof→verify chain in
`backend/tests/e2e/`) reads this `effective_owner` to label who is
accountable for that center's drive today; it does not change who the
SOP/config-level owner is.

## 5. UI section: "Position & Coverage" (renamed from "Role Assignment")

Per the maintainer corrections, the admin UI section for this proposal is:

1. Each staff member's **three axes are shown as three separate fields**: HR
   Designation grade (read-only context), Operational Position (title +
   center + week-off day, editable only by admin/CEO tier, never mutated by a
   leave/week-off event), and Department ownership (unchanged, existing UI).
2. **The configured backup per (center, backup group)** is shown explicitly —
   one slot per group (e.g. Backup Manager, Backup AM1, Backup AM2 for a
   center), not a free list, and a group may show "not configured" rather than
   silently defaulting to someone.
3. When a leave is applied to a covered position, or its recurring week-off
   arrives, the system **auto-selects** that group's configured backup as the
   cover (§4.5); the CEO tier can override the auto-selection for that
   specific window only (the override still writes to
   `workforce_absences.replacement_member_id` for the ad-hoc case, never to
   `workforce_positions`).
4. The UI **blocks** picking any position outside the covered position's
   configured `backup_group_code` as cover — the picker is constrained to that
   group's `is_backup_slot=true` holder (or, on CEO override, an explicit
   escalation to Park Head), never a free list of all managers/assistants.
5. The absent holder's **ownership badge is unchanged** during the coverage
   window; only their active/due tasks for the window show as reassigned.
6. The backup holder's own view shows a **temporary coverage banner** —
   "Covering Preventive Care until <date>" — for the duration of the window
   (leave or week-off day), and it disappears automatically when the window
   ends (driven by `workforce_member_capabilities.valid_to`, §4.6).

## 6. Sources

Raw pulls (JSON, gitignored, outside this repo, no PII committed):

```text
/Users/ravi/mesha/source-material/vgoats-seed/attendance-db-tabs.json
/Users/ravi/mesha/source-material/vgoats-seed/attendance-employee-master.json
/Users/ravi/mesha/source-material/vgoats-seed/attendance-by-category.json
/Users/ravi/mesha/source-material/vgoats-seed/attendance-february-26.json
/Users/ravi/mesha/source-material/vgoats-seed/staff-timetable-tabs.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-park-departments.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-hierarchy.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-preventive-care-director.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-goats-team-v1.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-health-director.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-breeding-director.json
/Users/ravi/mesha/source-material/vgoats-seed/timetable-farm-goat-managers-v1.json
```

Pulled via `gcloud auth application-default print-access-token` against the
Sheets API v4 `spreadsheets.values.get`, per
`docs/runbooks/google-cloud-environments.md`'s read-only pattern (no browser/
dashboard scraping used). `timetable-goats-team-v1.json` is the source for the
two-tier backup mapping (§2, §4.3): its Role/Shift/CBE/CPT/Week-OFFs/Backup
columns are the direct evidence for `workforce_positions`' shape.

## 7. Open questions (flagged, not decided here)

The two-tier backup grouping and the ad-hoc-leave + recurring-week-off dual
trigger are **confirmed** (see "Confirmed decisions" near the top) and are no
longer open. The following remain open:

1. **Same-day overlap between a recurring week-off and an approved ad-hoc
   leave for the same holder.** Both confirmed triggers resolve to the exact
   same action (route to `effective_backup`), so an overlap is not a conflict
   in practice — but confirm whether the system should still write/notify
   once or twice for that one day (a minor implementation nicety, not a
   modeling gap: §4.5's two triggers already produce the same outcome either
   way).
2. **`workforce_positions.scope_type` set and whether Bangalore HQ needs a new
   location row.** CBE/CPT already exist as `park`-type `locations` rows;
   Bangalore HQ (housing CXOs/Directors) may not have an equivalent
   operational-park location today. Confirm whether HQ gets a `center`-type
   (or reused `park`-type) `locations` row, or whether HQ-tier staff are
   `scope_type='tenant'`-scoped instead (no center subdivision needed at that
   tier).
3. **Proposed `hr_designation_grade` storage.** §4.1/§4.4 propose a small
   lookup (mirroring `departments`' shape) plus a `workforce_members` column,
   but this could equally be a plain `CHECK`-constrained text column with no
   lookup table, since the grade vocabulary (CXO/Director/Manager/Assistant
   Manager) is small and stable. Confirm which shape before implementation.
4. **CEO override audit.** §5 point 3 lets the CEO tier override the
   auto-selected backup for a specific window. Confirm whether that override
   needs its own audit reason field beyond the standard
   `workforce_absences.created_by`/`approved_by` columns already present.
5. **Escalation delivery channel.** §4.7 routes escalation to the Park Head
   via the existing `escalation_owner_user_id`/position-holder concept, but no
   notification adapter is specified here (Slack/FCM/email) — that belongs to
   the kernel's generic `NotificationGateway` port
   (`context/architecture/operational-kernel.md`), not this document.
