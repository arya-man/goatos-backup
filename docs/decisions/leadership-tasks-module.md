# Leadership Tasks: a director's ask of the CXO desk

Maintainer decision, 2026-09-04 (chat session with the maintainer). Status: ACCEPTED,
implemented on branch `feat/leadership-tasks-module`.

## What the module is

A **Tasks** module on the phone for leadership. A **director** (on STG today Hemant,
Dinakar and Chandrakant) raises a task for **one CXO** (the `ceo_internal` accounts): a
short title, a written brief, and optional attachments -- a **voice note recorded in the
app**, **photos or videos picked from the gallery**, and **any file** from the document
picker. It is not field work: there is no shed, no animal, no proof of work and no
verification. It is a director asking the leadership desk for something.

The CXO it is addressed to sees the task, opens it, and moves it **open → in progress →
done**. The director who raised it can **edit** the brief and attachments or **cancel** it
while it is still open or in progress; a done task is history. Every task carries a
per-tenant running **number** ("#12"). There is **no comment thread in v1**.

The maintainer's words on the surface: "super UI ... clean, neat, perfect" -- it is used by
the most senior people in the company.

## The decisions, each load-bearing

1. **Direction is directors → CXOs only.** The picker lists the CXO accounts; a CXO holds
   no raise permission. Offered as three options to the maintainer, this one was chosen.
2. **Status is owned by the assignee; the raiser only cancels.** `open → in_progress →
   done`, with a reopen of a done task allowed to the assignee (a mis-tap is real), and
   `cancelled` terminal for everyone. The transition rule and the buttons the screen shows
   come from ONE function (`domain.StatusOptionsFor` / `CheckTransition`), so the phone
   can never offer a move the write refuses.
3. **Status only, no thread.** Chosen for v1 to keep the surface minimal.
4. **Android only, with push.** A raised task pushes to the assignee; a task marked done
   pushes back to the raiser. In-progress, reopen and cancel are silent by design.
5. **Unseen badge.** `seen_at` is stamped once, the first time the assignee opens the task.
   The drawer row and the module's bar item show the count of the CXO's unseen, uncancelled
   tasks. This is the first numeric badge in the app, and it is **backend-owned**:
   `BootstrapModule.badge_count`, filled from `workforce/app.ModuleBadgeSource` so a second
   module can carry a number later without touching bootstrap.
6. **The "+" lives in the list header**, not in a bottom-bar tab. Per
   `docs/decisions/nav-entry-point-placement.md` a create drill for the very list on screen
   is an action on that screen, not a feature entry point. Refresh sits beside it.

## Permissions

| Permission | Holders | Gates |
|---|---|---|
| `leadership_tasks.read` | `pc_director`, `growth_director`, `feed_director`, `health_director`, `ceo_internal` | list, detail, status route, seen, and the proof **download** route (ORed in) |
| `leadership_tasks.raise` | the four director roles | raise, edit, the assignee picker, and the proof **upload** handshake (ORed in, the `toxin.execute` lever) |
| `leadership_tasks.act` | `ceo_internal` only | resolved into `can_change_status`; the status route itself is on read and the domain rule decides |

Deliberately **per job**, not per person -- the maintainer's words were "all the
directors". `procurement_director` is NOT granted it: that role holds no `app.bootstrap`
(web-only, pinned by the segregation test), so a phone module on it would be unreachable;
its holder raises through the `feed_director` grant he also wears. A park head, operator,
verifier and the per-person roles hold nothing.

The status route is gated on **read** rather than on act or raise: both parties reach it
(the assignee to move, the raiser to cancel) and the domain rule under the row lock decides
who may do what. Widening it to a single permission would lock one party out.

## Attachments ride the proof store, with a different honesty rule

Bytes go through the existing signed-upload pipeline (`POST /app/proofs/uploads` →
PUT → complete) as `proof_type: attachment` with the real mime type. None of the capture
honesty rules a weighing or toxin proof carries apply -- a gallery pick or a file from a
drive is exactly what the director meant to attach. What IS asserted, at raise/edit time and
inside the write path: the proof exists in this tenant, was registered as an `attachment`,
its upload **finished**, and it was **uploaded by the raiser**. Any gap refuses the whole
write (`422 invalid_attachment`) so a task never points at bytes nobody can open. Mime, size
and duration are read from the stored artifact, never trusted from the request. Removing an
attachment from a task never deletes the artifact; the proof store owns retention.

## The write path on the phone is online, deliberately

Raise needs the server-issued proof ids, which the outbox's proof-upload dispatch never
hands back, so raise/edit/status/seen are direct suspend calls with idempotency keys minted
**once per draft** and reused on retry. Leadership phones are online; the operator-grade
outbox is for field capture. Reads stay offline-first (Room, Paging 3, ~20-row keyset).

## Events

`leadership_task.raised` and `leadership_task.status_changed`, aggregate `leadership_task`,
emitted **inside** the write transaction's outbox (validator branch in migration `000248`),
consumed by `notificationbridge.LeadershipTaskNotifyConsumer` on all three buses
(`kernelstages`, `outbox-relay`, `domain-event-consumer`). Registered in
`context/architecture/domain-event-registry.json`. Each push is addressed to ONE person,
resolved by user id through the roster's device registry -- never to a position.

## Numbering

`task_no` is minted in the raise transaction under
`pg_advisory_xact_lock(hashtext('leadership_tasks:' || tenant_id))` as `max + 1`, unique per
tenant, never reused. The idempotency reservation happens before the lock so a replay never
takes a number.

## Pinned by

- `permissions.TestLeadershipTasksRaiseIsDirectorsAndActIsCEO`,
  `TestLeadershipTaskRoutesAreGatedOnTheDedicatedPermissions`,
  `TestLeadershipTaskPartiesReachAttachmentMedia`, and the amended
  `TestProofUploadStaysClosedToNonExecutors`.
- `workforce/app.TestLeadershipTasksModuleIsOfferedToDirectorsAndCEO`.
- `leadershiptasks/domain.TestStatusOptionsAndTransitionsAgree` (the options ARE the rule).
- `notificationbridge.TestLeadershipTaskRaisedPushGoesToTheAssigneeAndNamesTheAsk` and
  siblings.
- `leadershiptasks/adapters/postgres.TestLeadershipTaskLifecyclePostgresPaths` (numbering
  under contention, idempotent replay, version fence, seen-once, badge count, outbox rows).
