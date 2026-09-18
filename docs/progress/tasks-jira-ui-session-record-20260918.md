# Jira-like leadership tasks — session record (2026-09-18)

PR: https://github.com/vgoats/goatos/pull/295 · branch `feat/tasks-jira-ui-20260918`
Merge base: `36b98fd53` · `make ci-local` **GREEN @ c55556621** (receipt recorded)

Companion documents in this directory:
- `tasks-jira-ui-verification-20260918.md` — measured latency, chip-count honesty, index design, cursor compatibility, what is still unproven
- `tasks-jira-ui-mobile-judge-20260918.md` — the adversarial mobile-webview review, with its own corrections
- `tasks-jira-ui-perf-judge-20260918.md` — frontend bundle/CLS/debounce measurements

## What the change is

The Tasks page dumped every row into one scroll with no pagination, search or
filters. Separately, "task editing not working" was literal: `POST
/app/leadership-tasks/{id}/edit` had existed all along and the Android app
called it, but **admin-web had no edit client at all** — on web you could only
change status or add a note, and both failed silently.

Shipped: a Jira-shaped board (status columns with true whole-list totals) and
issue detail panel; search / assignee / raiser / date-range / sort / keyset
pagination; the missing edit UI; @mentions with comment/update/status
notifications; an in-app notification centre on the previously-dead top-bar
bell; and Chrome web push.

## Numbers that were measured, not asserted

| | before | after |
|---|---|---|
| `team_progress` limit=50 | p50 274ms / p95 316ms | **p50 125ms / p95 211ms** |
| `q=decking` limit=20 | p50 279ms / p95 491ms | **p50 128ms / p95 239ms** |
| notification feed unread count | Seq Scan 37.9ms | **Index Scan 13.6ms** |
| globally shared client JS (63 routes) | +15.3 KB vs main | **+4.5 KB** |
| CLS on `/tasks` | 0.000 | 0.000 |

The API win was not query tuning. Every query in the list endpoint runs in
under **1.1ms** server-side; the cost was **round trips** — ~10 of them at
~20ms tunnel RTT each, because the chips each paid their own network trip for
under a millisecond of work. Batched to ~5. The SSH tunnel multiplies
round-trip count roughly 100x, so **do not expect this margin on staging**;
against a co-located database the whole difference is single-digit ms.

## Defects review found that tests did not

- **Shared-Chrome push hijack.** The registration upsert was keyed on
  `(tenant, browser_install_id)` with no member predicate. An FCM *web* token
  belongs to the browser PROFILE and does not change across sign-out, so a
  second person signing in on the same profile refreshed the first person's row
  and received their pushes — including task titles and note excerpts. Keying
  per member would NOT have fixed it (both rows would carry the same token). The
  row now changes hands only on proof of possession of the browser.
- **Mark-all-read 400'd for 5+ notifications** while the UI optimistically
  showed them read, so nothing was ever marked read server-side and the rows
  came back on the next navigation. The idempotency key concatenated every uuid
  and blew the backend's 200-char cap.
- **A production SSR break.** `initials()`/`statusTone()` were imported from a
  `"use client"` module into three server components: 206 errors, zero task
  cards, "This screen failed to render" — while the API returned 200 with all
  424 tasks. It passed `tsc`, eslint and 875 tests. Nothing in the repo detected
  that class; there is now a guard for it with 10 adversarial fixtures.
- **A guard that passed green while the page could throw.** The fixture copy
  test only scanned its own feature directory, so keys read by the *shared*
  `worklist-pager.tsx` were invisible, and it asserted against the fixture and
  never the backend map.
- **A test assertion guarding nothing.** `tasks-phone-viewport.test.mjs`
  asserted a 40px floor on `.lt-chips .achip`; the status chips became
  `.lt-seg a` in the toolbar rebuild, so the pattern was satisfied entirely by
  the Clear chip. It passed the whole time — worse than a missing assertion,
  because it reads as coverage, and 23px chips are the exact defect it exists to
  catch.
- **Mobile, measured:** filter chips were ~23px tap targets; `vh` instead of
  `dvh` in a webview whose chrome retracts; the toolbar clipped at 1440 (a
  `flex-wrap:nowrap` inherited when a nested flex box became `display:contents`);
  the notification panel laying out at `left:-250px`, unreachable because
  `overflow-x:hidden` prevents scrolling to it.

## Judgements recorded rather than silently taken

- **No drag-and-drop in the first board pass.** Status changes are
  `row_version` fenced; a drag that silently 409s is worse than no drag. Built
  afterwards with legality derived from each row's own `status_options`.
- **`MentionTextarea` stayed eager**, holding 4.77 KB of the shared bundle. A
  lazy boundary removes the field from the server-rendered HTML and breaks its
  no-JS submit. Split later so only the picker defers.
- **A mention grants direct-open visibility** via a participants table, so the
  push deep link opens instead of 404ing. List scopes deliberately unchanged.
- **Cross-tab badge honesty.** Standing on "For me", the Team-progress badge can
  read 5 while opening that tab lists 2. Each tab's badge always equals its own
  rows; a cross-tab badge reflects the filters the current request carried.
  Inherent to the per-tab filter-drop rule, so the exact numbers are pinned in a
  test rather than the behaviour changed.
- **An absent VAPID key now means "use the SDK default"**, not "feature
  disabled" — browser push was dead in staging, gated on a console step for a
  key that is not required to deliver. A *wrong* key still fails loudly, because
  that is the "accepts but never delivers" trap. Verified against the SDK source
  that revocation and rotation are project-scoped, not key-scoped.

## Corrections made in this session

Both were mine, and both are recorded because the wrong version was acted on.

1. **The "external cleanup process" was largely a git misreading.** Deletions
   were checked with `git diff origin/main..HEAD`, which compares against main's
   TIP. `origin/main` moved from the merge base to `9f2642cfd` during the work,
   so every file another session added to main afterwards read as a file this
   branch had deleted. Against the merge base this branch has always had **zero
   deletions**. Acting on the false reading imported `natural_sql.go` from newer
   main, which referenced a field this branch's older `types.go` did not have —
   so the build break attributed to cleanup was self-inflicted, as was a failing
   test from importing a component test without its component. **On a
   long-running branch, check deletions against
   `git merge-base origin/main HEAD`.**
2. **The "web assignee picker will show fewer people than own tasks" claim was
   wrong.** The data was real — three directors lack `oversee` on the web
   surface, and a fourth has no web row — but all three read sites
   (`repository.go:99`, `:741`, `mentions.go:35`) hardcode `SurfaceMobile`
   regardless of caller. There is one endpoint and both surfaces call it, so the
   picker reads the mobile tick: 9 people against 4 actual task owners, all
   present. **No migration, no seed change, and nothing to tick on /people.** A
   regression test now pins that a mobile-only tick is assignable and a web-only
   tick admits nobody, so the misreading cannot become a migration later.

## Still open

- **Drag-and-drop and the mention-picker split** were in flight at the time of
  writing; neither is reviewed.
- **Copy audit in progress.** A refusal message was drafted as "someone else
  changed this task", which is exactly the abstract wording AGENTS.md bans — the
  system holds the actor and the new status, so it must read like "Chandrakant
  already moved this to Doing", from backend-owned copy with placeholders. The
  same test is being applied to every string this PR adds.
- **Six unmeasured routes** in the mobile sweep (`/people` wedged Next's
  serialised dev compiler); they are unmeasured, not passed.
- **Web push delivery** is proven end to end in a real Chrome, but a
  gateway-originated push has not been *observed arriving* in one run
  (gateway→FCM proven, FCM→browser proven, not joined), and the
  outbox→consumer→recipient-decorator chain was never triggered by a real
  mention.
- **The notification-feed CTE** still materialises a member's whole deduped
  history per page. Migration 000354 is the index half; an anti-join rewrite was
  measured at 31.3ms → 1.3ms and verified semantically identical by md5, but
  belongs to whoever owns that query.

## Gates

Green: `ci-local` (full affected-component suite, receipt at `c55556621`),
`scale-guard` zero new, `telemetry-guard`, `date-format-guard`,
`admin-web-phone-viewport-guard` zero new, `aggregate-projection-guard`,
`guardrail-registration-guard`, `admin-web mock-fidelity`, the new
`admin-web-server-client-values-guard`, 881/881 admin-web tests.

Pre-existing and not caused by this branch: `make validate-migrations` and
`validate-hot-index-migrations` fail with **29 findings both with and without**
each new migration (verified by moving each file aside and re-counting); none
name 000351, 000352, 000353 or 000354.
