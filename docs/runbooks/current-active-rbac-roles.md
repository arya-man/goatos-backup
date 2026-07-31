# Current Active RBAC Roles

This is the operational role list for current STG/mobile/admin work. Do not
grant roles outside this list unless a feature explicitly ships support for
that role in backend permissions, Android navigation, seed docs, and tests.

## Used Roles

| Role key | Current meaning | Current scope rule | Current users |
| --- | --- | --- | --- |
| `ceo_internal` | CEO/CXO/founder visibility and override | `tenant` | founder/builder cohort |
| `operator` | Ground execution and scanning for the modules explicitly granted to that person | `park` only for real field users | Amit, Darshan, Sagar, Pramod, Kumar Sharath |
| `pc_director` | Preventive Care Director; Vaccination visibility/action across parks | `tenant` when both parks are needed | Chandrakant |
| `growth_director` | Growth Director; Weighing visibility/action across parks | `tenant` when both parks are needed | Dinakar |
| `verifier` | Video verification review | `tenant` unless narrowed by future verification assignment rules | Jyothi / verifier users |

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

`growth_director` is the current live role for Growth Director. It can open
Weighing tasks across parks, scan/capture, submit, monitor videos, and reopen a
completed weighing shed bucket. It must not get Vaccination.

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
