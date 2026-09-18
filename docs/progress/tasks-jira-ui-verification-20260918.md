# Jira-like leadership tasks — verification record (2026-09-18)

PR: https://github.com/vgoats/goatos/pull/295 · branch `feat/tasks-jira-ui-20260918` (from `origin/main` @ `36b98fd53`)

This records what was actually measured, what is still unproven, and the defects
found by review so the next reader does not have to re-derive any of it. Where a
gate fails, the reason is named rather than hidden.

## Why the change exists

The Tasks page rendered every row in one scroll with no pagination, search or
filters. Separately, "task editing not working" was literal: `POST
/app/leadership-tasks/{id}/edit` has existed in the backend all along and the
Android app calls it, but **admin-web had no edit client at all** — on web you
could only change status or add a note, and both failed silently.

## Test environment

Real data, not fixtures: an isolated database `goatos_tasks_jira_20260918` on the
team's OCI instance, migrated to this branch's head, seeded by copying the real
reference tables (locations, departments, workforce_members, grants, module
access) and the 10 real leadership tasks out of the shared `goatos` clone
read-only, then topped up to **424–470 synthetic tasks** spread across 6
assignees, 4 raisers, ~180 days of `raised_at`, all four statuses, and a
deliberate mix of NULL/past/today/future deadlines. Search was made provable:
10 distinctive words appear only in task bodies, never in titles, and
near-duplicate titles exist so substring search has to discriminate.

The shared `goatos` database was never written to (verified before and after:
still at `000327` with 10 tasks).

## API latency — measured, 5 warmup + 20 measured per endpoint

Same database, same SSH tunnel, so tunnel RTT inflates every absolute number;
compare rows against each other, not against a production target.

| endpoint | before (`origin/main`) | after |
|---|---|---|
| `team_progress` limit=50 | p50 233ms / p95 305ms | p50 255ms / p95 313ms |
| `team_progress` limit=20 | — | p50 220ms / p95 291ms |
| `assigned_to_me` limit=50 | p50 135ms / p95 361ms | p50 127ms / p95 217ms |
| `q=decking` limit=20 | n/a (new) | p50 285ms / p95 811ms |
| `sort=deadline_asc` limit=20 | n/a (new) | p50 375ms / p95 528ms |

Note the pre-existing finding this surfaced: `team_progress` — the CEO's default
view — was already the *slowest* scope, because no index led with `tenant_id`
alone. The two faster scopes ride the existing assignee/raiser composites.

The two new capabilities are the slowest paths and were under investigation at
the time of writing; the endpoint issues **three** queries per request (rows plus
two separate count queries that now take the same filter predicates), so the
chips cost real time.

## Correctness verified against real data

Chip counts narrow with the filters, which is the property most likely to rot:

- unfiltered: `all=424 open=189 in_progress=118 done=117`
- `q=decking`: `all=63 open=30 in_progress=25 done=8`
- `q=tarpaulin` (a body-only word): `all=46 open=23 in_progress=23 done=0`
- `q=370` → exactly task #370 (a bare integer also matches `task_no`)

## Index design note

Migration `000345` carries **two** deadline indexes, not one. A btree is NULLS
LAST ascending but NULLS FIRST descending, so a single index cannot serve both
deadline directions; `EXPLAIN` showed the originally-specified expression-led
ordering (`(deadline_at IS NULL) ASC, deadline_at ASC`) degrading to a Seq Scan
plus top-N sort on 40k tenant rows. Search uses a real pg_trgm GIN index over
title and body rather than a non-SARGable `ILIKE` with a guard exception,
because this table grows without bound.

## Cursor compatibility

Cursors are sort-aware (base64url `<sort>\x1f<value>\x1f<task_id>`). A cursor
minted under one sort is **rejected** with 400 rather than silently serving a
wrong page. The legacy plaintext `<ts>|<uuid>` form is still accepted, but only
under the default `raised_at_desc`.

This matters for mobile: the Android app persists its cursor in Room
(`leadership_task_remote_keys.nextCursor`) across restarts and never sends
`sort`. A legacy cursor cannot be mistaken for base64 — it fails to decode at
byte 13 on the `:` of the timestamp — so it always falls through to the legacy
branch and resumes correctly. **The real risk is the rollback direction**: a
persisted *new-format* cursor replayed against a rolled-back backend 400s. It
self-heals on the next list refresh (`LoadType.REFRESH` sends a null cursor and
deletes the key), but an offline or failed refresh leaves it stuck on a retry
footer.

## Mobile webview

The page is opened at phone width inside the WhatsApp in-app browser, which has
retractable chrome — so `vh` is unstable there and every height cap uses `dvh`
with a `vh` fallback.

Fixed after audit: filter chips were ~23px tap targets (the page's primary
filter affordance) and the app-wide touch rule only matched `button`/`.btn`, not
the `<a class="achip">` chips; missing body scroll lock and focus trap on all
three overlays; a non-sticky modal header whose close button scrolled away; and
three inconsistent breakpoints (560/600/760) unified on **760px**, which is what
the rest of `mesha-theme.css` already means by "narrow".

Measured clean: no horizontal page scroll at 320/375/390/760. The wide table
(~850px natural width) is contained by nested `overflow-x:auto` scrollers.

A second audit found four further defects, fixed separately: the @-mention popup
clipped to one of six rows by an ancestor `overflow:hidden`; the notification
panel laying out to `left:-250px` at 360px (unreachable, since `overflow-x:hidden`
prevents scrolling to it); the newly-enabled bell adding a top-bar row and
overflowing the bar at 800–860px; and sub-40px pager/tab targets surviving in
the 561–760px band (a 375×667 phone in landscape is 667px wide).

## Notifications

Assignee and creator are notified on comment, update and status change; mentioned
users are notified too. Rules enforced and tested: the actor is never notified
for their own action, a user who is both a party and mentioned gets exactly one
notification, and a replayed outbox event notifies nobody twice.

Status-change notification already existed and already excluded the actor — it
was left alone. Comment and update events did not exist and were added. **Both
new event types had to be added to `contracts/jsonschema/domain-event-envelope.schema.json`**;
without that the outbox relay silently rejects them as `invalid_event_envelope`
and no notification is ever delivered. This was caught by a guard, not by a test.

A mention grants **direct-open** visibility of the task via a participants table,
so the notification's deep link opens instead of 404ing. List scopes are
deliberately unchanged — a participant does not gain a listing. Mention targets
are explicit user ids, re-validated server-side under the row lock against the
mentionable population; a forged id is refused, not dropped. Note bodies stay
plain text (no inline `@[uuid|Name]` token format) because the Android app ships
independently and older clients would render raw tokens to users.

## Chrome web push — partially proven

Delivery reuses the existing FCM HTTP v1 send path, because an FCM *web*
registration token is the same opaque string shape the existing `setFCMTarget`
already addresses. The queue, retry, dead-address suppression and dedupe ledger
are therefore reused unchanged.

Registrations live in a new table rather than widening `workforce_member_devices`:
that table is operator *device identity* (public key hash, revoke lifecycle tied
to phone bootstrap authority), and widening its `platform = 'android'` CHECK
would silently re-target a dozen Android-shaped reads at rows that are not
phones. Browser recipients are added at the `RecipientResolver` seam instead, so
`pushReachableDeviceSQL` stays the single definition of phone reachability.

**Unproven: actual delivery.** No VAPID key or FCM project is configured locally,
so no token was ever minted and no notification was ever observed arriving in a
Chrome window. What is verified is the state machine, the deep-link mapping, the
recipient decorator, the prune-on-permanent-failure path, and every guard.

Limits inherent to browser push, not defects:
- permission is granted **per browser profile**; enabling on a laptop does nothing for a desktop
- a user who has blocked notifications cannot be un-blocked by us
- nothing fires while Chrome is not running
- it requires a secure context, so plain-http staging has no web push at all
- there is no web equivalent of the phone's `notifications_enabled=false` signal, so a revoked browser grant can read "active" until the next load or send

## Defects found by adversarial review and fixed

- **Shared-profile push hijack.** The registration upsert was keyed on
  `(tenant, browser_install_id)` with no member predicate. Because an FCM web
  token belongs to the browser *profile* and does not change across sign-out, a
  second person signing in on the same Chrome profile refreshed the first
  person's row and received their pushes — including task titles and note
  excerpts. Keying per member would **not** have fixed it (both rows would carry
  the same token). The row now changes hands only on proof of possession of the
  browser; otherwise 409, and the client mints a fresh install id so both people
  get their own registration.
- **Mark-all-read returned 400 for 5+ notifications.** The idempotency key
  concatenated every uuid and blew the backend's 200-char cap, while the UI
  optimistically showed them read — so nothing was ever marked read server-side
  and the rows came back on the next navigation. Now a sha256 of the sorted id
  set: constant 93 chars.
- **A guard that passed green while the page could throw.** The fixture copy test
  only scanned `copy()` literals inside its own feature directory, so keys read
  by the *shared* `worklist-pager.tsx` were invisible to it, and it only asserted
  against the fixture, never the backend map. Widened to follow shared-component
  imports and to assert the backend contract.
- `data-table.tsx` applied the hide-class to `<td>` but not `<th>`, so a hidden
  column left an orphan header cell. Harmless only because that column happened
  to sit last; it would have misaligned every header on a backend reorder.

## Known-failing gates, with reasons

- `make validate-migrations` / `validate-hot-index-migrations` fail with **29
  findings both with and without this branch's migrations, none naming them** —
  verified by moving each new migration aside and re-counting. Pre-existing,
  from 000209/000250/000259–000266.
- `make api-client-check` is `git diff --exit-code` over generated code and fails
  whenever regenerated output is uncommitted.
- `leadership-assistant-coverage-guard` reads `git diff origin/main...HEAD`, i.e.
  committed files only, so uncommitted coverage-matrix rows cannot satisfy it. It
  already failed before this work.

## Still open

- The in-app notification feed has no index serving
  `(tenant_id, context->>'member_id', requested_at DESC, notification_request_id DESC)`;
  the existing per-member indexes are partial and scoped to specific message-key
  prefixes. This query runs on every screen change in the shell, so it needs an
  index before the bell reaches staging.
- Web push delivery needs a VAPID key configured
  (`GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY`) before it can be proven.
- Real staging has the leadership directors oversee-ticked on **mobile only**, so
  the web assignee/mention picker will show fewer people than own tasks until
  those web ticks are set. That is a data decision, not a code one.

## Adversarial review round 2 — mobile webview judge

Full report: `docs/progress/tasks-jira-ui-mobile-judge-20260918.md` (707 lines).

Two corrections to earlier conclusions in this document, both worth recording
because the first one was mine and it was wrong:

1. **The "Rendered more hooks than during the previous render" error is not a
   hooks-order bug and not in the shared shell.** It is a knock-on of `/tasks`
   failing server rendering because `initials()` is imported from a
   `"use client"` module into two server components; React then falls back to
   client rendering and the client re-render produces a different hook count.
   The earlier attribution to `/counts/herd` was an artifact of reading
   `[browser]` relay lines off a dev server shared by four concurrent agents —
   it does not reproduce there, and `LeadershipTasksPage` is imported only by
   `/tasks` and `/tasks-preview`.
2. **`Router action dispatched before initialization` IS introduced by this
   change.** The notification bell dispatches a Server Action from a mount
   effect on every route, racing App Router initialisation.

### The finding reading could not have produced

The notification panel does not re-place on reopen: measured `left:8` at 320,
360, 375 and 390, including the two widths where the bell sits at x 340→380 and
the correct clamp is `left:80`. Placement is computed on first open and cached,
and the `resize`/`scroll` listeners are attached only while the panel is open,
so a width change that happens while it is closed is never reflected. Benign at
these widths (8 is the clamp floor) but wrong wide→narrow — which is exactly
what WhatsApp's retracting chrome and device rotation do. On a fresh 390 load
the first click also failed to open the panel at all (zero-size element), with
the second working; the mechanism was not established, so that is logged as an
open question rather than a confirmed defect.

Probable common cause for both: `placePanel()` and `void refresh()` are invoked
*inside* the `setOpen` state updater, which must be pure and which React calls
twice in StrictMode.

### Confirmed fixed

The earlier `left:-250px` panel defect is genuinely gone. At 320 the bell wraps
to the second row (x 10→50), so the previous idiom would compute exactly
`50 - 300 = -250`; the clamp now lands it at `8 → 308`, matching hand
calculation. Top bar fits at every phone width with nothing clipped, tap
targets are 40px, no bare `vh` remains in new code, and no route produced
horizontal page scroll.

### Limits of this verification — stated so nobody over-reads it

- **Six of ten sweep routes are unmeasured, not passed.** `/people` wedged
  Next's serialised dev compiler and blocked everything queued behind it.
- **The host ran at load 157–256 throughout** (concurrent agents), so no timing
  figure in the judge report is a performance signal. Only pass/fail geometry
  observations are trustworthy.
- Branch HEAD moved twice mid-review, so the report records per-file mtimes for
  everything measured.

### Coverage gap the judge flagged as worth more than any single fix

`features/notifications/` has no layout test, while the sibling Tasks page has
`tasks-phone-viewport.test.mjs`. A two-line assertion — open the bell at 320 and
360, require `left >= 0` and `right <= clientWidth` — would have caught the
staleness defect, the first-click failure, and a latent containing-block hazard
where `.top`'s `backdrop-filter` makes it the containing block for the panel's
`position:fixed` (currently harmless only because `.top` sits at (0,0) full
width).
