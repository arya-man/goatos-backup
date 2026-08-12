# Current Active RBAC Roles

This is the operational role list for current STG/mobile/admin work. Do not
grant roles outside this list unless a feature explicitly ships support for
that role in backend permissions, Android navigation, seed docs, and tests.

## Used Roles

| Role key | Current meaning | Current scope rule | Current users |
| --- | --- | --- | --- |
| `ceo_internal` | CEO/CXO/founder visibility and override | `tenant` | founder/builder cohort |
| `operator` | Ground execution and scanning for the modules explicitly granted to that person | `park` only for real field users | Amit, Darshan, Sagar, Pramod, Kumar Sharath |
| `pc_director` | Preventive Care Director; Vaccination visibility/action across parks | `tenant` when both parks are needed | Chandrakant, Dinakar (added per person 2026-08-07) |
| `growth_director` | Growth Director; Weighing visibility/action across parks | `tenant` when both parks are needed | Dinakar |
| `feed_director` | Feed Director; owns the FEED chain (config/ration grid, dispatch sheet, packing worklist, feed proof oversight) across parks | `tenant` when both parks are needed | Feed Director cohort |
| `health_director` | Health Director; owns COUNTS (census / herd register) and health/tagging identity. DISTINCT from `pc_director` | `tenant` when both parks are needed | Health Director cohort |
| `verifier` | Video verification review (mobile + the admin-web verifier workspace) | `tenant` unless narrowed by future verification assignment rules | Jyothi / verifier users |
| `counts_approver` | **Per-person authority, not a job.** Approve/reject on the birth/death/shifting queue, on the phone and admin-web. Held ALONGSIDE a job role, never instead of one | `tenant` (a decision is addressed by request id; a director's remit spans both parks) | Chandrakant, Dinakar |

### `counts_approver` is granted by NAME (maintainer decision 2026-08-05)

This role exists because the maintainer asked for approval rights on named
individuals — "keep rbac per person, not per group" — and the permission model
resolves authority purely from the roles on a caller's active
`user_scope_grants` rows. A caller may hold several rows, so a narrow role held
alongside a job role is the only way to express "this person, not this job".

What that buys, and what it costs if it is ever "simplified":

- `pc_director` and `growth_director` keep their published definitions —
  Preventive Care only, and "Weighing and ONLY Weighing". A future holder of
  either job inherits **no** approval authority; someone must grant it by name.
- The one-module-one-director segregation lock
  (`backend/internal/permissions/director_module_segregation_test.go`) stays
  intact, and Counts ownership stays with `health_director`.
- Revoking the grant row removes the authority and leaves the person's job
  untouched.

Moving `counts.approve_access` / `counts.approve_lifecycle` /
`counts.approve_shifting` onto `pc_director` or `growth_director` is the exact
shortcut this design rejects: it would hand approval power to everyone who ever
holds those jobs. Two tests fail if anyone tries — see
`TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly` and
`TestCountsApproverRoleCarriesOnlyApprovalAuthority` in
`backend/internal/workforce/app/service_test.go`.

The role carries the three approval permissions and **nothing else** — no
`app.bootstrap`, no `admin_web.bootstrap`, no counts read/write. A grant of it
alone is inert: the holder must already have a job role to have any way in. The
named list lives in `backend/cmd/seed-stg-login-grants/approvers.go`
(`perPersonGrants`); adding an email there is the act of granting the authority.

### Per-person grants beyond approval (maintainer decision 2026-08-07)

`perPersonGrants` is now the general "this person, not this job" list, not only
the approver roster. Each entry names an individual and the roles they hold
**alongside** the job role a prior seed already gave them; permissions union
across a caller's active `user_scope_grants` rows, so listing several roles is
additive.

Current STG entries:

| Person | Job role (from the roster seed) | Added per person | Why |
| --- | --- | --- | --- |
| Chandrakant | `pc_director` | `counts_approver`, `operator` | Approve births/deaths/shifting; carry the operator surface (`counts.write` capture, weighing/feed reads) his director role does not include |
| Dinakar | `growth_director` (Weighing owner), `operator` | `counts_approver`, `pc_director`, `growth_director`, `operator` | Same approval authority, plus Preventive Care authority; keeps `growth_director` so Weighing still has an accountable director |

Two properties of this list that a later change will be tempted to break:

- **The `operator` grants here are tenant-scoped**, which the operator-scope
  invariant otherwise forbids. They are allowed because they are layered on
  directors whose remit spans both parks, not on park staff accounts, and each
  one carries a `stg-operator-scope: tenant approved` justification in its own
  block. `tools/agent-hooks/check-stg-operator-scope.mjs` fails the build if a
  tenant-scoped operator grant appears there without one — block-scoped, so an
  annotation copied from a neighbouring entry does not satisfy it.
- **A tenant `operator` grant does not make someone a vaccination operator.**
  The drive operator pool reads `workforce_positions` with
  `position_tier <> 'director'`
  (`backend/internal/obligation/adapters/postgres/visit_shot_lock.go`), never the
  RBAC role, so neither person is pulled into drive auto-assignment and the CPT
  rehearsal invariant ("Chandrakant is director-only monitoring scope") still
  holds.

- **The `operator` grant is also what puts Herd Operations on their phone.**
  Extended later the same day: `leadershipModuleKeys`
  (`backend/internal/workforce/app/bootstrap_copy.go`) offers the `counts`
  capture module — birth, death, shifting — to a leadership principal who holds
  `RoleOperator`. That is these two and nobody else. A bare `pc_director` or
  `growth_director` is offered nothing, so the job still confers no capture.

  Key it on the **grant**, never on the `counts.write` **permission**.
  `park_head` holds `counts.write` on the role, and `TestCountsModuleRoleMatrix`
  pins that a park head gets no capture module; a permission-keyed offer compiles,
  passes its own new test, and silently hands Counts to every park head. That
  pre-existing test is what caught it during implementation.

The 2026-08-07 decision was taken against the stated alternative of moving counts
authority onto the `pc_director` role itself. That was declined for the reasons
in the section above; do not re-propose it as a simplification.

### A director's modules cannot be changed in the database

Worth stating plainly, because it is the first thing anyone tries. For a
principal holding any role in `leadershipGrantRoles`
(`ceo_internal`, `pc_director`, `growth_director`, `feed_director`,
`health_director`, `park_head`), `candidateModuleKeys` returns
`leadershipModuleKeys(grants)` and **never reads `department_module_grants`** —
it replaces that set rather than unioning with it. There is no row in Postgres
that adds a module to a director. Non-leadership staff are the opposite case:
their modules come from their department's grants, which is how Pramod and
Kumar Sharath have Herd Operations without any code entry.

## Dormant Catalog Roles

`org_role_catalog` also contains composite tier/vertical roles such as
`director_preventive_care`, `director_breeding`, `manager_feed`, `head_health`,
and `am_growth`.

Those rows are **catalog scaffolding only right now**:

- no active user is granted them in STG/local seed data
- Android and seed docs should not treat them as live personas
- do not use them as shortcuts for current mobile access
- if one is activated later, the change must update backend permissions,
  Android role handling, seed/runbook docs, and tests in the same commit

`pc_director` is the current live role for Preventive Care Director. It can open
Vaccination cards, scan/capture, submit, and use vaccination close flows. It must
not get Weighing.

`feed_director` is the current live role for Feed Director (live as of 2026-08-01: backend
permissions, routes, notification routing, seed and tests shipped together). It holds
`feed_config.read`/`feed_config.write` (the authored ration grid), `feed_direction.read`
(today's dispatch sheet), `feed_packing.read` (the packing worklist), `feed_direction.oversee`
(the projected-count exception verdicts) and `verification.act`. Scope: `tenant` when the
director must see both parks. It must NOT get Vaccination, Weighing, or Counts. It also does
NOT get `feed_direction.complete`: the handbook puts field execution with the Park Head and
ground team ("All field execution happens through the Park Head"), and directing feeding is
not performing it. It does not get `verification.review` either — Feed_Director.pdf M1 is
double-verifying THE VERIFIER, not becoming one.

`health_director` is the current live role for Health Director, and it is a **distinct role
from `pc_director`**. Preventive Care and Health are separate departments in the org model
(`wiki/Handbooks/Mesha-dept-directors.pdf` DEPARTMENTS grid; `COO.pdf` VERTICALS UNDER COO),
so the two must never be merged or treated as synonyms. It holds `counts.read` (the census /
herd-register surface it owns), `goat.write_identity` (Responsibility 6 Tagging: ear tag/RFID
at birth, purchase and re-tag), `goat.write_health` (observations/diagnosis/treatment) and
`verification.act`. Scope: `tenant` when both parks are needed. It must NOT get Vaccination,
Weighing or Feed. It deliberately does NOT get `counts.write` (capture is ground work) or any
`counts.approve_*` (birth/death admission sits with the CEO tier, shifting approval with the
park head).

**Counts ownership is a maintainer decision, not a documented handbook duty** (2026-08-01).
No handbook assigns census, counting, headcount, roll call or reconciliation to anyone; a
full-text search of `Health_Director.pdf`, `Feed_Director.pdf`, `Mesha-dept-directors.pdf` and
`COO.pdf` returns zero matches, and every "count" token in them is an EOD tally field or a
non-animal stock count. The nearest written anchors on the Health Director's desk are
Responsibility 6 (Tagging, the identity substrate a census sits on) and Responsibility 9
(assessing every death for insurance). Feed ownership, by contrast, IS documented
(`Feed_Director.pdf` ROLE PURPOSE + M1 daily video double verification). Open questions that
were NOT decided and must not be inferred: whether Counts includes shifting/animal movement
(the handbooks give shifting to the BREEDING Director), who owns mortality reconciliation into
the census, and whether the Health Director's tagging duty becomes part of the Counts module.

Notification routing follows the same one-module-one-director rule: Feed proofs notify
`feed_director` and Counts proofs notify `health_director`, each in that module's own wording
and tap route (`pendingModuleProfiles` in
`backend/internal/notificationbridge/verification_notify_consumer.go`). A module that enqueues
a verification item MUST have a profile there AND at least one `verify` duty holder in
`position_module_duties`; both are asserted by tests, and the verify duty rows are seeded by
`backend/cmd/seed-position-duties` onto the tenant `video_verifier` seat that
`backend/cmd/seed-roster-real` creates.

`growth_director` is the current live role for Growth Director. It can open
Weighing tasks across parks, scan/capture, submit, monitor videos, and reopen a
completed weighing shed bucket. It must not get Vaccination.

It holds `weighing.execute` and deliberately **not** `task.execute`. Because it
executes weighing, it also reaches the `/app/proofs` write routes — those accept
`task.execute` OR `weighing.execute`, since capturing the mandatory video is part
of doing the work, not a separate task-execution act. Never "fix" a proof-upload
403 for this role by granting it `task.execute`: that carries vaccination SOP
submission (`POST /app/tasks/{task_id}/submissions`) with it. See
`docs/decisions/proof-capture-authorization.md`; enforced by
`make proof-capture-authorization-guard`.

## Hard Rule

Real operators must never get tenant-scoped `operator` grants. Operators are
park-scoped and module-scoped: a CBE weighing-only operator must not get
Vaccination execution just because their role is `operator`. If a director needs
to scan across both parks, use the director role for that feature:
`pc_director` for Vaccination, `growth_director` for Weighing. Do not make the
`operator` grant tenant-wide.

Android must not decide this from role labels. `/app/bootstrap` owns nav and
feature flags:

- `vaccination_execute=true` means shed cards may enter Vaccination Scan/Submit.
- `weighing_execute=true` means Weighing opens the field execution UI instead
  of the monitor/video display UI.
- `false` means display/review/monitor only.

FCM follows the same action hierarchy. Field completion flows upward to the
owning director/CEO/review side; rework/reopen flows downward to the assigned
operator and keeps the owning director/CEO in the loop. Vaccination uses PC
Director as the owning director. Weighing uses Growth Director as the owning
director.

## Weighing: Feature Flags vs Execute Gates

`weighing_execute` is a per-user bootstrap flag, not a role name. It answers
one question: does this authenticated user's Android session render the
field-execution UI (scan/capture/submit) for a given weighing shed bucket, or
the monitor/review UI?

- `weighing_execute=true` for the exact operator assigned to that
  `weighing_campaign_sheds` bucket (`operator_user_id`), and for
  `growth_director`/`ceo_internal` acting in an execution capacity when the
  product explicitly allows director/CEO scanning.
- `weighing_execute=false` renders monitor/video-review only: leadership and
  any director/CEO viewing a bucket they are not the assigned operator for.
- The flag is bucket-scoped in effect, even though it is delivered as one
  bootstrap boolean per session: backend list/roster reads still apply the
  per-bucket `operator_user_id` predicate (see
  `docs/runbooks/current-active-rbac-roles.md` Hard Rule above and
  `context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` A-1/A-2/C-1).
  A true `weighing_execute` flag never substitutes for that per-bucket SQL
  predicate; it only decides which UI mode Android renders.
- Do not infer `weighing_execute` from role name alone (`operator` vs
  `growth_director`). Always read it from `/app/bootstrap`.

## Park Scope vs Tenant Scope (Weighing)

Weighing repeats the same scope shape as Vaccination:

- Real weighing operators are **park-scoped** (`scope_type='park'`,
  `scope_id=<park_location_id>`) plus their explicit
  `weighing_campaign_sheds.operator_user_id` bucket assignments. An operator
  must never receive `scope_type='tenant'` for weighing.
- `growth_director` is **tenant-scoped** when the director must see both CBE
  and CPT weighing campaigns; `ceo_internal` is tenant-scoped for the same
  reason.
- Park scope gates which campaigns/buckets a user can even query; the
  per-bucket `operator_user_id` predicate inside park scope gates which rows
  a specific operator sees among sheds in their own park (E-1 in the ledger:
  filtering at park level instead of shed/bucket level was a real regression).

## Vaccination Strictness vs Weighing Free-Flow

These two Preventive-Care/Growth modules intentionally use opposite validation
postures, and neither should borrow the other's rule:

- **Vaccination** validates the scanned RFID against herd roster, shed
  ownership, and the animal's vaccination eligibility/schedule before
  accepting a submission. An RFID that does not resolve to a known,
  correctly-scoped goat is rejected or routed to a review/mismatch state.
- **Weighing** is free-flow: `weighing_observations.animal_id` is nullable
  (migration `000007_weighing_free_flow_scanned_identifier.sql`) and there is
  deliberately **no** validation against herd roster, vaccination tables, or
  shed ownership before accepting a scan. The same `scanned_identifier` may
  legitimately appear in different `campaign_shed_id` buckets. See
  `docs/features/weighing/TRD.md` §5/§12 and
  `tools/agent-hooks/check-weighing-free-flow-guard.mjs`.
- A future feature must not "fix" a Weighing gap by importing the
  Vaccination roster-validation pattern, and must not relax Vaccination's
  roster/eligibility checks to match Weighing's free-flow posture.

## One-Bucket-One-Operator Rule

Every `weighing_campaign_sheds` row (a "bucket") has exactly one assigned
operator: `operator_user_id uuid not null`
(migration `000056_weighing_shed_operator_assignments.sql`). There is no
backup/secondary operator column, no operator array, and no
many-operators-per-bucket join table. If a park needs more operator coverage
for a shed, that is expressed as more buckets (more `weighing_campaign_sheds`
rows), never as more operators on one bucket. Enforced by
`tools/agent-hooks/check-weighing-one-operator-per-bucket-guard.mjs`.

## Verifier Proof Flow

Weighing observations require mandatory video proof
(`proof_artifact_id`/`proof_artifact_ids`, subject-scoped per animal or per
shed/partition) before a submission can complete. The `verifier` role reviews
that proof:

1. Operator scans/submits with mandatory proof upload.
2. Submission enqueues a verification item (see
   `backend/internal/weighing/adapters/verificationbridge/enqueue.go`).
3. `verifier` reviews the proof video and approves or bounces for rework.
4. Approval completes the observation/scope; a bounce reopens it for the same
   assigned operator — never a different operator, per the one-bucket-one-
   operator rule above.

This mirrors the same-shaped verifier flow used by Vaccination shed
proof/verification; the two modules share the generic verification module,
not each other's business rules.

Since 2026-08-03 the `verifier` role also holds `admin_web.bootstrap` so the same
review can be done on a laptop. That grant opens the shell ONLY: a principal with
`verification.review` and without `verification.act` receives the verifier lens —
five registry-composed evidence modules and `/verify` — and every other admin-web
page contract is withheld, so a typed URL fails closed. See
`context/architecture/verifier-app-and-flow.md` → "Verifier WEB workspace".

**Bucket close is a separate, later gate than an individual verifier verdict**
(maintainer decision 2026-07-31). Per-item verify/rework happens observation by
observation as proof comes in. A `weighing_campaign_sheds` bucket may only move
to `closed` after EVERY submitted observation in that bucket has been verified
(`weighing_campaign_sheds.status`: `pending` → `in_progress` → `completed`
[operator submitted, awaiting verification] → `closed`, via CEO/Growth Director
action once all proof is verified). A verifier `rework` bounce does not itself
reopen or close the bucket; it only puts that one observation back in front of
the operator. Only CEO/Growth Director can reopen a `closed` (or `completed`)
bucket back to `in_progress`, after which the same assigned operator may add
more scanned RFIDs and submit again. Reopen must never allow the same
`scanned_identifier` (case-insensitive) to be recorded twice within the same
`campaign_shed_id`/day; the same RFID may still appear in a different bucket.

There is no expected-animal denominator for Weighing: no `"N/N"` and no
`"/100"`-style progress computed against a roster or expected count. Expected
animal counts are unknown for weighing; progress is reported only as counts of
scanned/accepted/pending/verified observations.

## FCM Up/Down Hierarchy (Weighing)

Weighing follows the same action-hierarchy FCM pattern documented above for
Vaccination, with Growth Director as the owning director:

- **Upward (completion/proof-ready):** operator submission with proof
  notifies the assigned bucket's context first, then escalates to
  `growth_director` and `ceo_internal` for visibility once a shed/campaign
  reaches a reviewable or completed state.
- **Downward (rework/reopen):** a verifier bounce or a director reopen
  notifies the operator who owns that bucket (`operator_user_id`), never a
  different operator, and keeps `growth_director`/`ceo_internal` in the loop
  as the owning director/CEO chain.
- Recipient resolution follows active role grants (`growth_director`,
  `ceo_internal`) and the live `weighing_campaign_sheds.operator_user_id`
  assignment, never a cached/static roster. See
  `docs/decisions/scale-anti-patterns.md` and the `fcm-recipient-routing-guard`
  make target, which covers Weighing's notification wiring alongside
  Vaccination's.
