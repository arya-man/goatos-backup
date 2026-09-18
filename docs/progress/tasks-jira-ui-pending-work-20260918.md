# Jira-like tasks — state of the branch and what is still open (2026-09-18, evening)

PR: https://github.com/vgoats/goatos/pull/295 · branch `feat/tasks-jira-ui-20260918` · HEAD `51485db0a`
Merge base `36b98fd53`.

This replaces the morning's version of this file, which described a design that has since
been replaced. The morning docs in this directory (`-session-record-`, `-open-bugs-`,
`-verification-`, the three `-judge-` files) are kept as history; where they disagree with
this file, this file is current.

## What the page is now

The `/tasks` page is the Work Board's product, not a Jira look-alike:

- Toolbar (one row at 1440/2000): search · All / To do / In progress / Done / Overdue chips ·
  **Assignee** and **Raised by** as plain dropdowns of ticked checkboxes (several people at
  once; the URL carries `a,b`; the API matches `= ANY(uuid[])`) · Sort · one Dates calendar.
- Board: four equal columns always, whatever the status filter says (the others read 0);
  the approved column header block (status-coloured rule, name, count pill) is unchanged;
  cards carry the deadline block, assignee + raised-by, key + date. On the page ground, no
  wrapper card, no hint paragraphs, no "N on this page".
- Task detail opens as a **drawer over the board**, CLIENT-LOCALLY: a card or row click is
  intercepted, the drawer opens from the row on screen (82 ms in the maintainer's Chrome, was
  ~900 ms + skeleton flash), and notes + activity load inside it by one server action. Status
  change publishes the row to a shared store -- the card moves and the pills adjust with no
  route re-fetch. Board <-> List swaps client-side (79 ms, zero route requests). Escape /
  scrim / Close / Back never navigate; deep links still render open. Machine-guarded:
  `make admin-web-interaction-patterns-guard` + the drawer host pinned in
  `admin-web-local-overlay-guard` (`docs/decisions/admin-web-interaction-patterns.md`). One status control (Cancel task inside it). Edit for leadership on any task.
  Activity feed with All / History / Comments, newest first, from the
  `leadership_task_events` table (migration 000349, written in the same transaction as every
  mutation, backfilled). Comments post in place; a picked @mention is final and renders as a
  brand chip.
- New task: Work Board's assignee picker for "For", the console's own calendar + hour/minute,
  attachments in one row.
- Status vocabulary is the Work Board's (To do / In progress / Done / Cancelled) on the wire
  and everywhere it renders. Android renders `status_chip` verbatim and ignores `activity`.
- Phone (390): filter sheet on the viewport with 2-column chips, a count badge on the Filters
  button, ticks keep the sheet open; snap-row board; ≥44px targets; 0 horizontal overflow.

## Measured at HEAD

`tools/perf/api-latency-gate.mjs` over the four `leadership_tasks_*` cases (now in
`hot-paths.admin-all.json`), 20 iterations, OCI tunnel (one DB round trip p50 ~40ms):

| endpoint | p50 | p90 | p95 | p99 | bytes |
|---|---|---|---|---|---|
| list (25 rows) | 138 | 207 | 218 | 239 | 45 KB (was 77.5 KB: activity no longer rides the list) |
| list filtered | 112 | 197 | 199 | 204 | 32 KB |
| detail | 113 | 191 | 191 | 198 | 38 KB |
| assignees | 68 | 126 | 139 | 151 | 1 KB |

Policy p90 ≤ 300 / p95,p99 ≤ 500: **PASS**. Writes: comment 1.3s → ~0.5s, status ~0.45–0.8s
on the tunnel (11–13 round trips; co-located DB ≈ 100ms). Browser (maintainer's Chrome, dev server): card
open 82ms, board↔list 79ms, status change = one action POST and nothing else; no RSC fetch on
any of the three (was one full route render each).

## Still open

### Needs the maintainer

1. ~~Leadership write authority is inferred, not granted~~ -- RESOLVED (review of PR 295, evening).
   `leadership_tasks.monitor` is an explicit permission on its own module tick,
   `leadership_tasks_monitor` (the `verification_policy` shape): the CEO/CXO role holds it,
   migration 000350 hands the tick to the people already on that grant, and a director ticked
   View + Do + Oversee + Configure on Tasks keeps their own tasks only. The heuristic
   (Raise AND Act AND NOT PenVisitsExecute) is gone from the http adapter and pinned by
   `TestActorFromMonitorIsAnExplicitTickNeverInferred`.
2. **Column tints and avatar colours** (Judge A, D8): the amber/blue/green column rules and the
   per-person avatar colours are inherited from the Work Board. If "palette stays green" means
   these too, say so; they were left as the Work Board has them.

### Browser push reaches the async processes (review of PR 295, evening)

`WithBrowserRecipients` was wired in the API process only; the pushes that actually originate
asynchronously -- a comment, a mention, a status change, delivered by `outbox-relay`,
`domain-event-consumer` and the kernel stages -- were phone-only. Every production composition
site now builds its consumers on the decorated resolver (`kernelstages.notifyRecipients` for the
stages), pinned by `TestEveryNotifierInAnAsyncProcessReachesBrowsers`, which fails on any
`notificationbridge.New*` built on the bare roster.

### Engineering follow-ups (not blocking)

- Write transactions are still 11–13 sequential round trips under one query timeout; on a slow
  link a write can time out mid-transaction and roll back. Batch the pre-write read
  (row + participants) and the audit/outbox/idempotency writes; ~8 trips is reachable.
- `/app/leadership-tasks/assignees` is refetched server-side on every RSC navigation; it is
  ~70ms in parallel with the list, but a short-lived cache keyed by tenant would remove it.
- `mesha-theme.css` grew ~950 lines for this feature (+7.5 KB gz on every route). A route-scoped
  stylesheet for `.lt-*` / `.ltd-*` would take it off the other 62 routes.
- The click-matrix harness (`smoke:tasks-click-matrix:live`) was retargeted to the final UI
  but has not had a clean full run since; run once with a quiet machine.
- Test residue in the throwaway DB (`goatos_tasks_jira_20260918`): dozens of judge comments and
  status flips on tasks #10 and #263. It is a copy; nothing to clean on staging.
- Docs in this directory older than this file describe the collapse-to-one-column board, the
  `+5` avatar filter and the slim Cancelled rail, all of which are gone.

### Not done, and not planned for this PR

- P3 `status_changed_by_name` from the morning doc is moot: the history feed names the actor.
- P5 (gateway-originated push observed end to end) and P6 (notification-feed CTE rewrite)
  belong to their own PRs.

### Guard debt this branch put on the books (shrink-only)

`tools/admin-web-interaction-patterns/baseline.json`: 23 native date inputs in older forms, 61
returning server actions that also `revalidatePath`, 2 ARIA-faked checkboxes -- all pre-existing,
now frozen and only allowed to go down.

## Before this merges

`make ci-local` on a clean tree at the final SHA. Last full green was `c55556621`; everything
after it has been proven piecewise (unit suites, guards, the latency gate, judge runs) but not
by the certifying run.
