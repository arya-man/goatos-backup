# Leave requests: the operator asks, the park head and HR both approve

Maintainer decisions, 2026-09-10. Plan and build record:
`docs/features/leave-requests/plan.md`.

## The rule

1. **The operator raises leave from the phone Clock In / Out screen.** One or many days
   plus a reason, then submit. A rejected request can be raised again.
2. **Two approvers, and BOTH must accept.** The operator's Park Head and a holder of the
   `hr` role. Either one rejecting ends the request. The operator sees the outcome and the
   reason beside their past clockings.
3. **`hr` is a role, granted per person.** It carries approvals only and nothing
   park-scoped. Leave is its first approval kind; more will follow.
4. **Whom a request routes to is CEO-only config.** `workforce_leave_approval_config`
   carries two flags, park head and HR. An absent row means both. "Feature flag in HRM,
   only I should set whom it goes to."
5. **Approvers act in the existing Approvals module**, phone and web.
6. **A pending request can be withdrawn** by the person who raised it.
7. **A forgotten clock-out is closed at 23:59:59 IST** of its own day with the hours
   counted to that instant and the row marked auto-closed. This supersedes the
   no-invented-hours half of the Clock plan's D4.
8. **Roster coverage on approval is the next step**, not this one.

## Why the shape

- **The request table is the workflow; `workforce_absences` stays the fact.** The roster
  already had a single-approver leave on `/admin/roster/leave` that also resolves backup
  coverage. Replacing it would have re-opened the roster; ignoring it would have left two
  disagreeing sources of "is this person away". So the request row carries the two-slot
  approval, and the LAST required approval mirrors one `workforce_absences` row
  (`status='approved'`, no replacement) in the same transaction. Every existing
  approved-leave read sees it; coverage is untouched until the roster rewrite.
- **Two fixed slots as columns, not a decisions table.** Decision 2 fixes the approvers at
  exactly two, so a request-to-decision join can never fan out and the queue predicate is
  one row wide.
- **Which slot a caller signs is derived from grants, never from the body.** A park head's
  parks are their park-scoped `park_head` grants; HR is the `hr` role; the CEO floor may
  name either. A park head naming the HR slot is refused. Both halves of the
  role-scoped-UI lock hold: the route table gates on `leave.approve`, and the service
  refuses a slot the caller's grants do not carry.
- **Its own capability module, `leave_approvals`.** Ticking someone to decide leave must
  never hand them birth/death/shifting authority, or the reverse. On the phone it renders
  as the Leave tab inside the Approvals module; on the web it is the `/leave` page. Park
  heads get it mobile-only, because they hold no admin-web bootstrap.
- **Routing is snapshotted at raise.** A request carries `park_head_required` and
  `hr_required` as of the moment it was raised, so flipping the config never changes who a
  request already in a queue is waiting on. A person with no park cannot be routed to a
  park head; that slot drops and HR alone decides. Nobody to route to at all refuses the
  raise (422 `leave_routing_unavailable`) rather than approving silently.
- **Notifications name people, days and counts.** Raise pushes to every approver device
  the write transaction resolved; the final outcome pushes to the requester. One slot
  approving while the other is open pushes nothing.

## Assumptions the maintainer has not yet confirmed

- A punch on an approved leave day is accepted and recorded; the day still shows the leave.
- The CEO may sign either slot (the CEO floor).
- Hours on an auto-closed day count to 23:59:59.

## Pinned by

`backend/internal/workforce/adapters/postgres/leave_repository_integration_test.go`
(raise, replay, overlap, withdraw, both-approve mirrors an absence, either-reject ends it,
slot fencing), `backend/internal/notificationbridge/leave_request_notify_consumer_test.go`,
the adminui page-catalog tests, and the permission parity tests.
