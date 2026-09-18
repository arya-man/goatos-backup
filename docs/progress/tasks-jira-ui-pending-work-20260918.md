# Jira-like tasks — pending work and validation state (2026-09-18)

PR: https://github.com/vgoats/goatos/pull/295 · branch `feat/tasks-jira-ui-20260918`
Merge base `36b98fd53` · 32 commits ahead · **zero deletions vs the merge base**

Read alongside:
- `tasks-jira-ui-session-record-20260918.md` — what the change is, measured numbers, my own two errors
- `tasks-jira-ui-open-bugs-20260918.md` — the five UI bugs the maintainer found by clicking
- `tasks-jira-ui-verification-20260918.md` — latency, chip-count honesty, index design, cursor compatibility
- `tasks-jira-ui-mobile-judge-20260918.md`, `-perf-judge-`, `-copy-judge-` — the three adversarial reviews

## Validation state

**Last full gate: `make ci-local` GREEN @ `c55556621`, receipt recorded.**
Eleven commits have landed since. **`ci-local` has NOT been re-run at the current
HEAD** — that is the first thing to do before anyone treats this branch as
certified, and the repo's rule is that only a complete green run on the exact SHA
authorises a push to `main`.

Green at the time of writing (run individually, not as a suite):
`tsc` 0 errors · `go build ./...` clean · admin-web tests **883/883** ·
`scale-guard` zero new · `telemetry-guard` · `date-format-guard` ·
`notification-specificity-guard` · `ui-vaccine-labels-guard` ·
`check-ui-contract-literals` · `check-ia-guard` ·
`admin-web-phone-viewport-guard` zero new · `aggregate-projection-guard` ·
`guardrail-registration-guard` · `admin-web mock-fidelity` ·
`admin-web-server-client-values-guard` · both pen-vocabulary tests ·
`go test ./internal/adminui/... ./internal/leadershiptasks/...`

Pre-existing and **not** caused by this branch: `make validate-migrations` and
`validate-hot-index-migrations` fail with **29 findings both with and without**
each of 000345/346/347/348, verified by moving each file aside and re-counting.
None name them.

## Pending — work that was in flight when this stopped

### P1. The five UI bugs the maintainer found (partially done, UNVERIFIED)
Detail and file/line evidence in `tasks-jira-ui-open-bugs-20260918.md`. An agent
was mid-fix and was stopped; `task-people-filter.tsx` and
`scripts/smoke-tasks-click-matrix-live.mjs` exist on the branch but **neither is
reviewed, exercised, or wired to anything**. Treat them as a starting point, not
a fix.

- **B1/B2 — the person filter.** `+5` is a `<span aria-hidden="true">`: not a
  link, no handler. With 9 assignable people and 6 avatars shown, five are
  unreachable, and the avatars are initials only (`D D M D M A` — two D's, two
  M's). A CXO filtering 424 tasks needs to find a person by NAME. The assignee
  list already arrives from `GET /app/leadership-tasks/assignees` as
  `{user_id, name, title}`. There is no combobox in the repo; the keyboard
  pattern in `features/notifications/mention-picker.tsx` is the thing to reuse
  rather than inventing a second one. Decide whether the toolbar's
  Assignee/Raised-by selects converge with it instead of becoming a third way to
  do the same thing.
- **B3 — "Show only this"** describes the implementation. It applies
  `filter=<status>` so the pager can walk one status, because a column cannot
  show all 183. Reword to the intent; backend-owned key.
- **B4 — board + status filter** renders Open and Doing as "0 on this page" under
  pills reading 183 and 118. Each number is right alone; together it is nonsense.
  A status filter and a status-column board express the same thing — pick one
  (board ignores `filter`, or collapses to the filtered column, or the segment is
  disabled in board view) so no URL can produce an empty board above live counts.
- **B5** — the view param is `t_view`; `?view=list` is silently ignored. Decide
  whether to accept `view` as an alias or reject unknown params loudly.

### P2. The click-through E2E that would have caught them (NOT DONE)
This is the important one, and its absence is why P1 shipped past me. The repo's
rule requires visual regression **and** click-through E2E and says one is not the
other. I delivered screenshots and reported them as both.

What it must do: enumerate every interactive element from the DOM
(`a, button, select, input, [role=button], [role=tab], summary, [draggable]`
inside the page region), activate each, and assert (a) something observable
changed — URL, selected state, an opened panel, a row count — (b) no console or
page error, (c) no failure string. **An element that activates with no observable
change must FAIL the run**; that is precisely the `+5` defect. At 1440 and 390,
covering all three scope tabs, Board/List, every status chip, both person
filters, sort, both date disclosures and their apply/clear, active-filter chips,
Clear, the search box (including that the debounce fires one request), every
column's focus link, a card, the detail panel's Edit/Close/status actions/
composer, the pager, the bell and its panel, and the New task modal. Where an
element is legitimately inert at a viewport (drag on phone), assert it is ABSENT.

`apps/admin-web/scripts/smoke-tasks-click-matrix-live.mjs` is an unreviewed
start. It needs wiring beside the existing `smoke:*` scripts and a decision about
whether it can join a guard target or needs a live stack.

### P3. Backend follow-up: `status_changed_by_name`
The refusal copy names the new status, which is the honest ceiling today.
Naming WHO — "Chandrakant already moved this to Doing" — is not composable:
**no column anywhere records the actor of a status change.** `leadership_tasks`
(000252) has none and no history table carries one. It needs a migration
(`status_changed_by uuid`), a change to the status-change write transaction, a
third `LEFT JOIN` in the shared `taskColumns` projection, a domain field,
payload, OpenAPI and a client regen. The banner never reaches for the missing
key: `task_who` is sent only for `not_assignee`/`not_raiser`, never for a
conflict, so `.named` is unreachable for `version_conflict` by construction.

### P4. Copy keys still rendering from fallback
`board.drag_hint`, `board.drag_moving`, and the pre-existing `board.aria`,
`board.on_this_page`, `board.focus_status`, `board.total_unavailable`,
`board.column_empty` are absent from the backend contract. They render because
every read uses the 3-arg `copy(contract, key, fallback)` form, so nothing
throws — but the contract should own them.

### P5. Web push — proven, with two unjoined halves
A real notification was delivered to a real Chrome with the correct deep link,
and an absent VAPID key now means "use the SDK default" so staging is no longer
gated on a console step. Not yet proven: a **gateway-originated** push *observed
arriving* in one run (gateway→FCM proven, FCM→browser proven, never joined), and
the outbox→consumer→recipient-decorator chain was never triggered by a real
mention.

### P6. The notification-feed CTE
Migration 000348 is the index half (unread count 37.9ms → 13.6ms). The `mine`
CTE still materialises a member's whole deduped history per page. An anti-join
rewrite was measured at **31.3ms → 1.3ms** and verified semantically identical by
md5 of the full result set on first and deep pages — but it belongs to whoever
owns that query, not to this PR.

### P7. Smaller, recorded rather than hidden
- **No-JS commenting is broken independently of this work**: the comment form
  mints its idempotency key in the button's `onClick`, so a genuine no-JS press
  can never satisfy the ≥8-char gate in `actions.ts`. The textarea half works —
  the text reaches the server — but the submit is rejected.
- **Six unmeasured routes** in the mobile sweep (`/people` wedged Next's
  serialised dev compiler). They are unmeasured, **not** passed.
- The bottom sheet's chips and date disclosures were measured at 0px because the
  probe never opened the sheet; their ≥40px sizing is asserted in CSS but not
  measured live with it open.
- The assignee ladder (open→Doing→Done) could not be drag-tested live: the local
  actor is assignee of 0 of 424 tasks, so `cancelled` is the only legal drop.
  Ladder legality rests on the derivation plus fixture rows.
- The Faro event for the drag is wired but unobservable locally (no collector
  configured).
- 6 leftover `goatos_test_*` databases on the OCI server (~30MB each), four
  probably from our test runs, two likely another session's. Not dropped, since
  deleting a database that cannot be definitively attributed is destructive.
- The mention-picker split recovers **0.83 KB** across 63 routes, not the 4.77 KB
  an audit claimed — the audit attributed a shared chunk to the composer that is
  actually the notification bell and copy map. If that 4.77 KB is wanted, the
  bell is the target.

## Two things for the maintainer, not for code

1. **VAPID** is now optional, so staging push works without it. Setting
   `GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY` in `infra/envs/stg` remains worthwhile
   for provenance and independent rotation, and migrates itself: a mismatch
   deletes the old token and mints a new one on the next load.
2. **Nothing to tick on /people.** The earlier claim that the web assignee picker
   would show fewer people than own tasks was wrong — all three read sites
   hardcode `SurfaceMobile`, so the picker reads the mobile tick (9 people
   against 4 actual owners, all present). A regression test pins this so the
   misreading cannot become a migration.

## The judge/lens matrix this PR still needs

I ran four adversarial reviews and each one found real defects — but the
maintainer still found five bugs by clicking, which means the **set of lenses was
incomplete**, not that reviewing does not work. Recorded here so the next person
runs the whole set rather than re-deriving it.

Each lens is a separate judge with its own brief. One agent given "review this"
produces a shallow pass; the value came from a narrow brief plus a demand for
evidence. Every judge must separate **CONFIRMED** (it measured or reproduced the
thing) from **SUSPECTED** (read from code), because three of the four blurred
that at first and one finding turned out not to reproduce at all.

### Lenses that RAN, and what each caught

| # | Lens | Caught |
|---|---|---|
| 1 | **Correctness + security** | The shared-Chrome push hijack (one person's notifications reaching another's browser) and mark-all-read 400'ing for 5+ rows while the UI showed success |
| 2 | **Mobile webview** | The panel at `left:-250px`, 23px chips, the 561px top-bar row — and it CORRECTED two findings it was sent to confirm |
| 3 | **Frontend performance** | The production SSR break (206 errors, zero cards, passing tsc+eslint+875 tests), +15.3 KB on all 63 routes, and it cleared CLS with measurements rather than assuming |
| 4 | **User-facing copy** | The banned "someone changed this task" sitting in the BACKEND contract, four mention refusals collapsing to "something went wrong", a raw VAPID sentence in the top bar |

### Lenses that DID NOT run — and the bugs each would have caught

| # | Lens | Must prove | Would have caught |
|---|---|---|---|
| 5 | **Click-through / interaction** | Every interactive element enumerated from the DOM, activated, and asserted to produce an observable change. **No observable change = FAIL.** At 1440 AND 390 | **B1** — `+5` is a dead `<span aria-hidden="true">`; five people unreachable |
| 6 | **Affordance / information design** | Every control's label read as a first-time CXO would read it; every number on screen reconciled against every other number visible at the same time | **B2** (initials-only, no name search), **B3** ("Show only this" describes the implementation), **B4** (empty columns under live counts of 183/118) |
| 7 | **URL / deep-link contract** | Every query param the page reads, every param it writes, and what happens to an unknown or stale one; every URL shareable and reloadable | **B5** — `t_view` vs `view`, silently ignored |
| 8 | **Empty / boundary states** | Zero rows, one row, exactly one page, a filter matching nothing, a task with no note/attachment/deadline, a person with no tasks | not yet exercised; the fixture has 424 tasks and 0 attachments, so the attachment path has never rendered with data |
| 9 | **Accessibility** | Keyboard-only traversal of the whole page, focus order and visible focus, screen-reader labels, and that nothing interactive is `aria-hidden` | would have caught **B1** independently — the chip is hidden from assistive tech AND unclickable |
| 10 | **Concurrency / staleness** | Two actors on one task; a page held open while the data moves underneath; a phone write against a stale web board | partly done via the drag conflict path; not done for the detail panel, the edit modal, or the notification centre |
| 11 | **Cross-surface parity** | The same business number rendered identically on web, Android and the API — the repo has a standing lock on this | not done; the Android app reads the same list endpoint and was never opened against this branch |
| 12 | **Permissions / role** | The page as each role: park head, director, verifier, operator — not just CEO/CXO | not done; every screenshot in this PR is the CEO lens, and `can_edit`/`status_options` differ per actor |

### The rule the whole matrix exists to enforce

**A screenshot proves a layout; only a click proves a control.** Lens 5 is not
optional and not a nice-to-have — it is the one that separates "the page renders"
from "the page works", and its absence is the single reason this PR reached the
maintainer with dead controls on it. Wire it to a repo script beside the existing
`smoke:*` entries so it can run in CI rather than living in a session.
