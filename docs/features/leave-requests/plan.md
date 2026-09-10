# Leave requests — operator asks, park head + HR both approve

Branch `feat/leave-requests`. Maintainer decisions given verbally on 2026-09-10; every
line under §1 is one of them, §2 is what this plan derives from them, and §7 lists the
assumptions the maintainer has not yet confirmed.

## 1. Maintainer decisions (2026-09-10)

1. **The operator raises leave from the phone Clock In / Out screen.** Days (one or
   many) plus a reason comment, then submit. A rejected request can be resubmitted.
2. **Two approvers, and BOTH must accept.** The operator's Park Head and a holder of
   the new `hr` role. Either one rejecting ends the request; the operator sees it as
   rejected with the reason.
3. **`hr` is a new role, created now, granted per person.** It carries approvals only
   and no park-level approvals. Leave is its first approval kind; more will follow.
4. **Whom a request routes to is CEO-only HRMS config** ("keep it as a feature flag in
   HRM, only I should set whom it should go to").
5. **Approvers act in the existing Approvals module**, phone and web, beside the
   birth / death / shifting queue.
6. **Leave history lives beside past clockings** on the Clock screen: upcoming,
   pending, approved, rejected, withdrawn.
7. **The operator can withdraw a pending request.**
8. **Roster rewrite and backup coverage on approval is the NEXT step**, not this one.

## 2. Shape

### 2.1 Storage (migration `000288_workforce_leave_requests.sql`)

`workforce_leave_requests` — one row per request, the two approver slots as columns
because the slots are fixed by decision 2, not a variable list:

```
leave_request_id, tenant_id, workforce_member_id, park_id (snapshot of
primary_location_id at raise), starts_on DATE, ends_on DATE (inclusive),
reason TEXT (1..2000), status IN (pending, approved, rejected, withdrawn),
park_head_required BOOL, hr_required BOOL       -- snapshot of the routing config at raise
park_head_decision / _decided_by / _decided_at / _note
hr_decision        / _decided_by / _decided_at / _note
decided_at (final), absence_id (the workforce_absences mirror, on approval),
idempotency_key, request_fingerprint, row_version, raised_at, created_at, updated_at
```

`workforce_leave_approval_config` — one row per tenant: `park_head_required`,
`hr_required` (at least one true), `updated_by`, `updated_at`, `row_version`. An
absent row means both required. This is decision 4's feature flag.

**Reconciliation with the pre-existing roster leave** (`workforce_absences`,
`/admin/roster/leave`, single approver on `roster.manage`, auto backup resolution): the
request table is the APPROVAL WORKFLOW; `workforce_absences` stays the canonical
"this person is absent" fact. On final approval the service inserts one
`workforce_absences` row (`status='approved'`, `reason_code='planned_leave'`, no
replacement) in the same transaction, so every existing read of approved leave
(`IsMemberOnApprovedLeave`, vaccination operator scope) sees it. The old coverage
engine is NOT run (decision 8). The old `/admin/roster/leave` apply/approve routes are
left as they are and are not called by any new surface.

### 2.2 Permissions and roles

- `leave.approve` — decide a leave request. Held by the `park_head` JOB (a park head
  approves their own park's leave, decision 2), by the new `hr` role, and by
  `ceo_internal` (CEO floor).
- `leave.read` — read every request across people. `ceo_internal`, `hr`.
- `leave.approval.configure` — set the routing flags. `ceo_internal` ONLY
  (decision 4; the `VerificationSampling` shape: one capability gates the control AND
  the route).
- Raising and withdrawing ride `AppBootstrap`, exactly like the clock punches
  (decision D2 of the clock plan: everyone clocks, so everyone may ask for leave).
- New role `hr` (catalog migration in the same file): `leave.approve`, `leave.read`,
  `AdminWebBootstrap`, `AppBootstrap`. Nothing park-scoped. Granted per person via
  `perPersonGrants`; nobody named yet — the maintainer names the acting HR.

Slot resolution at decide time is server-side: a caller whose active `park_head`
grant is scoped to the request's `park_id` decides the park-head slot; an `hr`
holder decides the HR slot; the CEO names the slot explicitly. A caller with no
open slot on that request gets 403.

### 2.3 Routes

```
POST /app/leave/requests                          AppBootstrap   raise
POST /app/leave/requests/{id}/withdraw            AppBootstrap   withdraw while pending
GET  /app/leave/requests                          AppBootstrap   own history (bounded)
GET  /app/leave/approvals                         leave.approve  caller's open queue, keyset ~20
POST /app/leave/approvals/{id}/approve            leave.approve
POST /app/leave/approvals/{id}/reject             leave.approve  reason required
GET  /admin/leave/requests                        leave.read     People / HRMS list
GET  /admin/leave/approval-config                 leave.approval.configure
PUT  /admin/leave/approval-config                 leave.approval.configure
```
plus the `/admin-web/leave/approvals*` twins of the three approver routes for the web
Approvals page.

Idempotency: every write takes the `Idempotency-Key` header; the key and a fingerprint
of the request meaning are stored on the row (raise) or on the slot (decide); an
exact replay returns the stored result and a same-key-different-payload replay is
409 `idempotency_conflict`.

Overlap: a new request whose window overlaps an existing pending or approved one for
the same person is refused 409 `leave_overlap`.

### 2.4 Clock status integration

`GET /app/clock/status` gains `leave_requests` (own rows: every pending or upcoming
one, plus the most recent decided ones, bounded at 20, backend-composed labels) and
`on_leave_today` + `leave_today_label`. On an approved leave day the reminder banner
is suppressed and the state label reads "On leave". **A punch on a leave day is NOT
refused** (assumption A1 in §7).

### 2.5 Approvals module

Phone: the existing `approvals` module gets a second bottom-bar item, **Leave**
(`/leave/approvals`), gated on `leave.approve`. The module is offered when the
principal holds `counts.approve_access` OR `leave.approve`, so a park head or an HR
holder sees Approvals with only the Leave tab and the named counts approvers see both.
Capability catalog: new module `leave_approvals` (web + mobile, oversee =
`leave.approve`), rendered inside the phone `approvals` module the way `pc_trimming`
renders inside `pc_care`. A 000245-style repair writes the tick for existing park
heads and hr / ceo holders already on the per-person path.

Web: `/approvals` gets a Leave tab with the same approve / reject verbs; `/people`
gets a **Leave** tab (config toggles for the CEO, request list for `leave.read`).

### 2.6 Notifications

Backend-owned, farm-worded, names + dates + day count (notification-specificity guard):

- raise → every member holding an open required slot: "Leave request: Ramesh, CBE ·
  12–14 Sep 2026 (3 days) · Reason: …" → tap opens `/leave/approvals`.
- final approve / reject → the requester: "Your leave for 12–14 Sep 2026 was
  approved" / "… was rejected by <role> · <reason>" → tap opens `/clock`.
- one slot approved while the other is open → nothing (the requester sees the
  status line on the Clock screen).

### 2.7 Events

`workforce.leave.requested`, `.approved`, `.rejected`, `.withdrawn` on the outbox
from the same transaction as the row change; registered in the envelope enums, the
outbox validator, the domain-event registry and the shared consumer registration.
The notification producer consumes them.

## 3. Build order

1. Migration + domain + repository + service + routes + OpenAPI (backend green).
2. Capability catalog, role, module offer, nav labels in four locales, tick repair.
3. Clock status integration + notification producer + event registrations.
4. Admin-web: /people Leave tab, /approvals Leave tab.
5. Android: request form, Clock screen leave section + withdraw, Approvals Leave tab,
   outbox op types `LEAVE_REQUEST`, `LEAVE_WITHDRAW`, `LEAVE_APPROVE`, `LEAVE_REJECT`.
6. Proof: Go tests (service + Postgres), admin-web Chrome visual + click-through,
   Android on the physical phone against a throwaway stack.

## 7. Assumptions to confirm

- **A1** — clocking in on an approved leave day is allowed and recorded; the day still
  shows the leave. Flip to a refusal if wanted.
- **A2** — the CEO may decide either slot on any request (CEO floor). If the CEO must
  never stand in for the park head or HR, drop `leave.approve` from `ceo_internal`.
- **A3** — an approved leave is mirrored into `workforce_absences` so existing
  approved-leave reads see it; coverage / backup is not resolved (decision 8).
- **A4** — a request cannot start in the past (today or later).
