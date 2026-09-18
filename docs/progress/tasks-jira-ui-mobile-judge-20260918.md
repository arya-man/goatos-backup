# PR #295 — mobile / WhatsApp-webview judge report

Branch `feat/tasks-jira-ui-20260918`. Measured against the live local stack
(web `http://127.0.0.1:13308`, API `http://127.0.0.1:18088`, database
`goatos_tasks_jira_20260918`, CEO actor `ceo_internal` / Ravi, 424 real tasks).

**What was measured, and when.** Branch HEAD moved twice while this review ran,
because three other agents were committing to the same worktree. Measurements
below were taken between **05:53 and 06:05 IST on 2026-09-18**, against:

- branch HEAD `5dae2f6b2` at the start, `50a7c89b9` by the end
  (`perf(tasks): collapse the list endpoint from ~10 round trips to ~5`);
- a **dirty** worktree throughout — 18 modified tracked files plus 3 untracked
  (`leadership-tasks-board.tsx`, `task-board-card.tsx`, `task-detail-panel.tsx`);
- the shell/notification files as of these mtimes:
  `mesha-shell.tsx` 04:22:58, `notification-bell.tsx` 05:28:33,
  `notification-panel.tsx` 04:59:27, `mention-textarea.tsx` 04:59:27,
  `push-permission-prompt.tsx` 05:45:24, `mesha-theme.css` 05:41:01.

**Measurement caveat that affects every timing number here.** The host was at a
**1-minute load average of 157–256** for the whole session (other agents
building and running the same Next dev server). Warm route fetches that took
28s early took 58s later. No latency number in this report should be read as a
performance signal; only the pass/fail observations are meaningful.

---

## LEAD 1 — the "Rendered more hooks than during the previous render" error

### Verdict

| claim | status |
|---|---|
| The hooks error is a hooks-order violation in the notification bell / shell | **DISPROVED** |
| The hooks error originates on `/tasks`, as a consequence of a server-render bailout | **CONFIRMED** (from the dev server's own browser-error relay) |
| The `/tasks` server-render bailout is caused by this PR's committed code | **NO** — it is caused by an **uncommitted, in-flight edit** by a concurrent agent |
| `Router action dispatched before initialization` is introduced by this PR | **CONFIRMED as PR-introduced**, low user impact (details below) |
| The hooks error reproduces on `/counts/herd` at 390x844 | **DID NOT REPRODUCE** (1 of 1 clean trial; see "what I could not finish") |

### What the evidence actually shows

The error did not come from the shared shell. In `scratchpad/web-feature.log`,
the single occurrence of `Rendered more hooks than during the previous render.`
(line 1878) is immediately preceded by a `/tasks` request that server-errored:

```
⨯ Error: Attempted to call initials() from the server but initials is on the
  client. It's not possible to invoke a client function from the server, it can
  only be rendered as a Component or passed to props of a Client Component.
    at TaskBoardCard (features/leadership-tasks/task-board-card.tsx:58:20)
    at PeopleFilter (features/leadership-tasks/leadership-tasks-board.tsx:229:14)
 GET /tasks 200 in 9.3s
[browser] Uncaught Error: Rendered more hooks than during the previous render.
```

and the same log records React's own explanation of the mechanism:

```
[browser] Uncaught Error: Switched to client rendering because the server
rendering errored: Attempted to call initials() from the server ...
```

That is the whole causal chain. The SSR pass throws, React discards it and
re-renders the subtree **on the client**, and the client pass renders a
different number of hooks than the aborted server pass did — which is exactly
what "rendered more hooks than during the previous render" reports. The hooks
error is a *symptom* of the `initials()` boundary violation, not an independent
bug, and it is scoped to `/tasks`.

### Why it looked like a shell bug, and why that was a misattribution

`/counts/herd` is not involved. The original capture attributed the error to
`/counts/herd` at 390px because the dev server's `[browser]` relay lines and the
`/app/leadership-tasks` fetch lines are **interleaved from every browser hitting
this one shared dev server** — and three other agents were driving `/tasks` and
`/tasks-preview` throughout. I verified that leadership tasks genuinely cannot
render on `/counts/herd`: `LeadershipTasksPage` is imported by exactly two
routes, `app/(admin)/tasks/page.tsx` and `app/tasks-preview/page.tsx`.

### The actual root cause of the `/tasks` failure

`apps/admin-web/features/leadership-tasks/leadership-tasks-table.tsx` is a
`"use client"` module. Two new server-rendered files import a **plain helper
function** out of it:

- `features/leadership-tasks/leadership-tasks-board.tsx:7` → `initials` (used at :243)
- `features/leadership-tasks/task-board-card.tsx:7` → `initials` (used at :58)

Importing a non-component export from a `"use client"` module into a server
component turns it into a client *reference*, not a function, so calling it
during SSR throws. Both importing files are **untracked** and were last written
at 05:34–05:37 by the concurrent Tasks agents, so this is in-flight work, not
something PR #295 has committed. It should be fixed by whoever owns those
files, but it must not ship in this state.

**Proposed fix** — move the helper to a module with no `"use client"` directive
and import it from both sides. `initials` is pure string formatting with no
React in it:

```diff
--- a/apps/admin-web/features/leadership-tasks/leadership-tasks-table.tsx
+++ b/apps/admin-web/features/leadership-tasks/leadership-tasks-table.tsx
-export function initials(name: string): string {
-  /* ... */
-}
+export { initials } from "./task-presentation";
```

with the body moved verbatim into a new server-safe
`features/leadership-tasks/task-presentation.ts` (no `"use client"`), and
`leadership-tasks-board.tsx` / `task-board-card.tsx` changed to
`import { initials } from "./task-presentation";`. Re-exporting from the client
module keeps every existing client-side import working unchanged.

There are two other `initials` implementations already in the tree
(`features/work-board/work-board-model.ts:66`,
`features/vaccination-live-tracker/format.ts:62`), both in plain `.ts` modules —
so the server-safe-helper-module pattern is already the house style here.

### `Router action dispatched before initialization` — a real, PR-introduced defect

This one **is** this PR's. It appears twice in the log, and both times next to
the bell's Server Action:

```
 POST /tasks 200 in 169ms
  └─ ƒ loadNotificationFeedAction() in 94ms features/notifications/notification-actions.ts
[browser] Uncaught Error: Internal Next.js error: Router action dispatched before initialization.
```

`features/notifications/notification-bell.tsx:117-130` fires the
`loadNotificationFeedAction` **Server Action from a mount effect**, on every
admin route, on every page load:

```js
useEffect(() => {
  let cancelled = false;
  void (async () => {
    try {
      const result = await loadNotificationFeedAction();
      if (!cancelled) applyFeedResult(result);
    } catch { /* silence */ }
  })();
  return () => { cancelled = true; };
}, [applyFeedResult, pathname]);
```

A Server Action invocation is dispatched through the App Router's action queue.
A mount effect that fires during hydration can run before that queue is
initialised, and Next throws. This is new behaviour: the entire
`apps/admin-web/features/notifications/` directory does not exist on
`origin/main` (`git ls-tree origin/main` returns nothing for it), and
`PushPermissionPrompt` has no mount site on main either — so on `origin/main`
the shell dispatches no router action on mount at all. Every other Server
Action in `apps/admin-web` (23 `"use server"` modules) is triggered by user
interaction, never from a mount effect.

The throw escapes the component's own `try/catch` (it is raised inside Next's
internals, not in the awaited call path), which is why it is logged as
`Uncaught`. Note the log also shows `loadNotificationFeedAction()` **succeeding**
on other loads, so this is a race, not a deterministic failure.

**Proposed fix** — defer the first read out of the hydration window, so the
router is initialised before the action is dispatched:

```diff
   useEffect(() => {
     let cancelled = false;
-    void (async () => {
-      try {
-        const result = await loadNotificationFeedAction();
-        if (!cancelled) applyFeedResult(result);
-      } catch {
-        // Same silence as above; a failed read is not the shell's problem.
-      }
-    })();
-    return () => {
-      cancelled = true;
-    };
+    // The first read must NOT be dispatched inside the hydration commit: a Server Action
+    // goes through the App Router's action queue, and that queue is not initialised yet
+    // ("Internal Next.js error: Router action dispatched before initialization"). One
+    // macrotask is enough, and the bell has no deadline -- nobody is reading a badge in
+    // the first frame.
+    const timer = setTimeout(() => {
+      void (async () => {
+        try {
+          const result = await loadNotificationFeedAction();
+          if (!cancelled) applyFeedResult(result);
+        } catch {
+          // Same silence as above; a failed read is not the shell's problem.
+        }
+      })();
+    }, 0);
+    return () => {
+      cancelled = true;
+      clearTimeout(timer);
+    };
   }, [applyFeedResult, pathname]);
```

### What a user actually experiences

**The page still renders.** This is the important part, and it is measured, not
inferred. On every route I measured, `main` was present with children, `.top`
was present, there was no horizontal page scroll, and none of the four failure
strings (`Something went wrong`, `This screen failed to render`,
`contract unavailable`, `missing copy key` — all four traced to real boundaries
in `app/global-error.tsx`, `components/observability/error-boundary.tsx`,
`components/admin-shell.tsx`, `lib/admin-ui-contract.ts`) appeared. Nothing
vanished.

- The `initials()` / hooks pair on `/tasks`: React falls back to client
  rendering, so the Tasks page still paints, but it paints **without SSR** —
  slower first paint, and the error is logged loudly. It is a dev-visible
  correctness bug that would become a production SSR failure.
- The router-action error: the bell's **first** feed read for that page load is
  lost, so the unread badge can fail to appear until the next route change
  (which re-runs the effect). The rest of the screen is untouched — the
  component's failure isolation works as designed.

### Was a baseline worktree stood up?

**No, deliberately, and the verdicts above are labelled accordingly.** Standing
up `origin/main` would have meant `npm ci` plus a cold Next dev compile on a
host already at load 200+, where a single warm route fetch was taking 16–58s.
It was also unnecessary for attribution: `features/notifications/` **does not
exist on `origin/main` at all**, so there is no baseline behaviour to compare —
the bell, its Server Actions and its mount effect are 100% new code. That is a
stronger attribution than a timing comparison would have given.

### Static confirmation that there is no hooks-order bug

I read all five suspect components in full and checked every hook call site:

| file | hooks | conditional hooks? | early return before a hook? |
|---|---|---|---|
| `features/notifications/notification-bell.tsx` | `usePathname`, `useMemo`, 5×`useState`, 3×`useRef`, 4×`useEffect`, 3×`useCallback`, `useMemo` | no | no |
| `components/push-permission-prompt.tsx` | 3×`useState`, `useEffect`, 2×`useCallback` | no | the early return at :152 is **after** all six |
| `features/notifications/mention-textarea.tsx` | `useId`, `useRef`, 4×`useState`, `useMemo`, 4×`useCallback` | no | no |
| `features/notifications/notification-panel.tsx` | none (presentational) | n/a | n/a |
| `components/mesha-shell.tsx` | 4×`useMemo`, 8×`useState`, 5×`useRef`, `useRouter`, `usePathname`, `useSearchParams`, 6×`useEffect`, 4×`useCallback` | no | no — single `return` at :703 |

And the repo's own ESLint agrees. `npx eslint` over all five files exits **0**
with no findings — including `react-hooks/rules-of-hooks` and
`react-hooks/set-state-in-effect`.

**On whether the earlier `set-state-in-effect` fix is complete: yes, for that
rule** — no effect body in `notification-bell.tsx` reaches state synchronously;
every write is behind an `await` or inside an event handler, and the lint rule
passes. But it left a related defect standing, below.

---

## DEFECT — side effects inside a `setState` updater (bell open handler)

`features/notifications/notification-bell.tsx:220-231` performs two side
effects **inside** the `setOpen` updater callback:

```js
onClick={() => {
  setOpen((current) => {
    const next = !current;
    if (next) {
      placePanel();      // calls setPanelBox(...)
      void refresh();    // dispatches a Server Action
    }
    return next;
  });
}}
```

A `setState` updater must be a pure function of the previous state. React may
invoke it more than once for a single dispatch — it does so deliberately in
StrictMode, and may re-run it when rebasing an update. Consequences:

1. `void refresh()` fires the `loadNotificationFeedAction` **Server Action twice
   per bell open** in development — a duplicated backend read on every click.
2. `placePanel()` calls `setPanelBox` from inside another hook's updater, which
   is the "update a component while rendering a different component" hazard the
   `react-hooks` rules exist to prevent. It happens to be idempotent here, so it
   does not currently misbehave.

This is the same class of mistake the file's own comments say it is avoiding
("never synchronously in an effect body"); the updater is simply a second place
the same rule applies. ESLint does not catch it.

**Proposed fix** — keep the updater pure and do the effects beside it. `open` is
already available in the closure, and `aria-expanded={open}` proves it is in
scope:

```diff
         onClick={() => {
-          setOpen((current) => {
-            const next = !current;
-            if (next) {
-              // Measured HERE, from an event handler, so the first paint of the panel is already
-              // clamped -- there is no frame in which it renders off-screen.
-              placePanel();
-              void refresh();
-            }
-            return next;
-          });
+          const next = !open;
+          // Measured HERE, from the event handler and NOT from inside the state updater: an
+          // updater must be pure, and React invokes it twice in StrictMode -- which fired the
+          // feed Server Action twice per click. The placement still happens before the panel's
+          // first paint, so there is no frame in which it renders off-screen.
+          if (next) {
+            placePanel();
+            void refresh();
+          }
+          setOpen(next);
         }}
```

Status: **CONFIRMED from code** (the double dispatch is a documented React
StrictMode behaviour; I did not instrument the click count, so the *count* is
read from code rather than measured).

---

## LEAD 2 — cross-route sweep

### Method (and why the original harness timed out)

The previous harness used Playwright `waitUntil:"networkidle"` with a 60s
budget. Against a Next **dev** server that never settles — it compiles on first
visit, and its HMR websocket keeps the network non-idle — that can never
succeed, which is why 21 of 24 routes timed out. Replaced with:

- a plain `fetch` **warm pass per route** before measuring, so the measurement
  is not measuring webpack;
- `waitUntil:"domcontentloaded"` plus an explicit `waitForSelector('.top')`,
  then a fixed 7s settle so hydration and client effects land;
- a fresh browser context per route × viewport, so `pageerror` / `console`
  listeners attribute errors to the right page (the flaw that produced the
  original `/counts/herd` misattribution);
- 180s navigation budget.

Scripts: `scratchpad/judge-run.mjs` (sweep), `scratchpad/judge-lead3b.mjs`
(phone checks). Raw output: `scratchpad/judge-run.log`,
`scratchpad/judge-run.json`. Screenshots: `scratchpad/judge-shots/`.

### Results — measured

All clean. `.top` present, `main` present with children, no horizontal page
scroll, no page errors, no failure text.

| route | viewport | HTTP | landed | `.top` | root el | `scrollW == clientW` | bell rect | page errors | failure text |
|---|---|---|---|---|---|---|---|---|---|
| `/counts/herd` | 390x844 | 200 | `/counts/herd` | yes | `main` (1 child) | 390 == 390 ✓ | 340→380, 40x40 | none | none |
| `/counts/herd` | 1440x900 | 200 | `/counts/herd` | yes | `main` (1 child) | 1440 == 1440 ✓ | 1143→1181, 38x38 | none | none |
| `/approvals` | 390x844 | 200 | `/approvals` | yes | `main` (1 child) | 390 == 390 ✓ | 340→380, 40x40 | none | none |
| `/approvals` | 1440x900 | 200 | `/approvals` | yes | `main` (1 child) | 1440 == 1440 ✓ | 1143→1181, 38x38 | none | none |
| `/verify` | 390x844 | 200 | `/verify` | yes | `main` (1 child) | 390 == 390 ✓ | 340→380, 40x40 | none | none |
| `/verify` | 1440x900 | 200 | `/verify` | yes | `main` (1 child) | 1440 == 1440 ✓ | 1143→1181, 38x38 | none | none |

Console errors observed, both accounted for:

- the known pre-existing **503 on a browser-side resource** — present on
  `/approvals` and `/verify` at both viewports; the dev log identifies it as
  `GET /api/auth/firebase-config 503`. Per the brief, not a finding.
- **one hydration-attribute mismatch on `/counts/herd` at 390x844 only** (absent
  at 1440x900, absent on the other two routes). See the open question below.

### Routes I could NOT measure, and why

| route | why not |
|---|---|
| `/people` | The Next dev server wedged on `○ Compiling /people ...` and stayed there for the rest of the session. Next serialises dev compilation, so this blocked *every* subsequent request, including already-compiled routes. With the host at load 200–256 from the concurrent agents, it never completed. |
| `/work-board`, `/feed/config`, `/health/config`, `/leave`, `/` (→ `/weighing/analytics`) | Queued behind the `/people` compile; never reached. `/` had been observed warming at 103s, and `/weighing/analytics` at **3.7 min**, earlier in the session. |
| `/tasks` (all three scopes) | **Deliberately excluded.** The route is mid-rewrite by three concurrent agents and is *currently broken on disk* — it server-errors on `initials()` (LEAD 1) and falls back to client rendering. A warm pass on it never returned. Judging its layout now would measure a half-finished edit. |

**These six routes are unmeasured, not passed.** The sweep that covered them
should be re-run on an idle host before the PR is signed off — the harness
(`judge-run.mjs`) is parameterised by `JROUTES` and will do it unchanged.

---

## LEAD 3 — phone-specific checks for the WhatsApp in-app webview

### Bare `vh` in new code — PASS (CONFIRMED)

The `vh`-then-`dvh` double-declaration pattern is applied consistently, which is
the correct approach for WhatsApp's retractable chrome: `vh` lands first as the
fallback for engines without `dvh`, `dvh` wins where supported. No new rule caps
a box with `vh` alone, and no `vh` value appears in any of the new TSX (the one
grep hit in `notification-bell.tsx:83` is a comment).

| selector | cap |
|---|---|
| `.nc-list` (notification list) | `max-height:55vh; max-height:55dvh` |
| `.mention-pop` | `max-height:40vh; max-height:40dvh` |
| `.lt-page .lt-fsheet-host .lt-fgroup.open` | `max-height:84vh; max-height:84dvh` |
| `.lt-modal` | `max-height:92vh; max-height:92dvh` |
| `.lt-page .ltb-colbd` | `max-height:60vh; max-height:60dvh` |
| `.ltd-activity` | `max-height:56vh; max-height:56dvh` |

Worth recording that `notification-panel.tsx:97-101` documents the exact trap
that makes this necessary: React emits only one declaration per style key, so
an **inline** `55dvh` gives an engine without `dvh` support no cap at all and an
unbounded list. That is why these live in the stylesheet and not in `style={}`.

### The bell's new footprint on the phone top bar — a real PR-caused change, mitigated

This is the most interesting phone-width consequence of the PR and it is easy to
miss. `app/mesha-theme.css` contains, inside `@media(max-width:560px)`:

```css
.top .iconbtn[disabled]{display:none}
```

That rule is **pre-existing** (it is at line 2328 on `origin/main`). On
`origin/main` the bell was a permanently `disabled` button — so at ≤560px it was
**deleted from the layout entirely**. This PR makes the bell live, which means a
40px control plus a 7px gap now appears in the phone top bar *where nothing was
before*.

The PR author found this independently and bought the space back in a new
`@media(max-width:860px)` block, with the measurements recorded in the
stylesheet: `.top` scrollWidth 846→900 against an 800px client, 860→900 against
an 860px client, with the overflow cut off unreachably because
`html,body{overflow-x:hidden;max-width:100vw}` is in force at ≤860px. The space
comes from two discretionary labels rather than from any control:

```css
.top .pscope{max-width:168px}                                  /* park chip */
.top .pscope .muted{display:none}                              /* "· all sheds" suffix */
.top .me .nm, .top .me .rl{ /* ellipsis */ max-width:76px}     /* CEO name / role */
```

That is the right trade — the park picker and the identity block both stay
reachable, and both labels are restated inside their own menus. **Measured
result at 390x844 on three routes: `.top` does not overflow
(`topClipped:false`), no child is clipped, and the document has no horizontal
scroll.** The mitigation works at 390px.

### Tap targets — PASS at phone widths (CONFIRMED at 320/360/375/390)

`.iconbtn` is 38px in the base rule and is overridden to **36px** at
`@media(max-width:760px)` — under the 40px floor — but a later
`@media(max-width:560px)` block restores `.iconbtn{flex:none;width:40px;height:40px}`.
Measured bell rect at 390x844 was **40x40** on all three routes, and 38x38 at
1440x900. So:

- at 320–560px the top-bar icon buttons are 40px — correct;
- **from 561px to 760px they are 36px** — below the floor. That is a
  **pre-existing** gap (the stylesheet's own comment at line 3591 names it:
  "the app-wide touch floor still lives at `@media(max-width:560px)`, so from
  561 to 760px ... these went back to their compact desktop size"), and it now
  applies to the bell too because the bell is newly visible. A 375x667 phone
  held in landscape sits in that band. Not introduced by this PR, but newly
  reaching one more control.

Inside the panel, the new code sets explicit floors — `minWidth:40, minHeight:40`
on the close button, the per-row mark-read button and the refresh button, and
`minHeight:44` on `.pm-item` rows ("44px floor so the row is a real tap target
on a 390px phone").

### The notification panel at 320px / 360px — the prior `left:-250px` finding is FIXED (CONFIRMED)

**Measured, and it works.** Script `scratchpad/judge-lead3b.mjs`, route `/verify`,
one page load then four viewport widths, zero page errors throughout.

| viewport | bell rect | panel `position` | panel left→right | width | fully on-screen in X | bottom within viewport | doc h-scroll |
|---|---|---|---|---|---|---|---|
| 320x844 | x 10→50 (**row 2**) | `fixed` | 8 → 308 | 300 | **yes** | yes (318 ≤ 844) | none (320 == 320) |
| 360x844 | x 10→50 (**row 2**) | `fixed` | 8 → 308 | 300 | **yes** | yes | none (360 == 360) |
| 375x844 | x 325→365 (row 1) | `fixed` | 8 → 308 | 300 | **yes** | yes | none (375 == 375) |
| 390x844 | x 340→380 (row 1) | `fixed` | 8 → 308 | 300 | **yes** | yes | none (390 == 390) |

Screenshots: `scratchpad/judge-shots/bell-open-{320,360,375,390}.png`.

The 320px row is the exact reproduction of the old bug, now passing. The top bar
wraps to two rows at every phone width (`flex-wrap:wrap` at ≤760px), and at 320
and 360 the **bell itself is the control that wraps** — it lands at x 10→50 on
the second row. The old `.parkmenu` idiom (`right:0`, laid out leftwards from the
button) would therefore put the panel's left edge at `50 - 300 = -250px`, which
is precisely the `-250px` the prior review measured at 360. The new clamp:

```
width     = min(300, 320-16)      = 300
preferred = anchor.right - width  = 50 - 300 = -250
rightmost = 320 - 8 - 300         = 12
left      = max(8, min(-250, 12)) = 8          ← measured: 8
```

The arithmetic and the measurement agree exactly. The panel is reachable, and
because it is `position:fixed` it is not subject to the `overflow-x:hidden` that
made the old overflow permanently unscrollable.

**The `backdrop-filter` containing-block hazard I flagged is real but currently
harmless.** `.top` carries `backdrop-filter: saturate(1.4) blur(10px)`, which
makes it the containing block for its `position:fixed` descendants — so the
panel's coordinates resolve against `.top`, not the viewport, while
`placePanel()` computes them in viewport space. The measured `left:8` matches the
viewport-space calculation, confirming the two frames coincide today (`.top`
starts at `(0,0)` and spans the full width). It is load-bearing coincidence, not
design, and would break if `.top` gained a margin, transform or offset.

### DEFECT (CONFIRMED by measurement) — reopening the panel does not re-place it

This is the measured consequence of the impure-`setOpen`-updater defect above,
and it is the finding I would not have predicted from reading alone.

In the four-width run, **every** width reported `left:8`, including 375 and 390
where the bell is on row 1 at x 325→365 / 340→380 and the correct clamp is
`left:80`. The placement computed at the first open (320px) was never
recomputed. A dedicated follow-up (`scratchpad/judge-stale.mjs`, fresh load
directly at 390, no resize) confirms the mechanism from the other side:

```
FRESH-390-FIRST-OPEN  bell 340→380  panel 0→0 (width 0)   expectedLeft 80
AFTER-RESIZE-TO-320   bell  10→50   panel 8→308 (w 300)   expectedLeft 8  ✓
```

Two things are visible here:

1. On a **fresh load at 390, the first click did not open the panel at all** — the
   panel element measured zero-size, i.e. still `display:none` via
   `.parkmenu:not(.on)`. The second click (after the resize) opened it correctly
   and clamped exactly to the expected value. I could not establish the mechanism
   in the time available, so the first-click failure is an **open question**, but
   it is reproducible enough to block on.
2. `panelBox` is refreshed only by a successful open-click and by the
   `resize`/`scroll` listeners, and **those listeners are attached only while the
   panel is open** (`notification-bell.tsx:167`, `if (!open) return;`). So a width
   change that happens while the panel is closed is never reflected.

**Why this matters for the WhatsApp webview specifically.** At the widths I
measured the staleness is benign, because the stale value (8) is the clamp floor
and stays on-screen. The dangerous direction is wide→narrow: a `panelBox`
computed at a wide viewport carries a large `left` (at 1440 it would be ~1132),
and reopening after the viewport narrows would place the panel far off the right
edge. WhatsApp's chrome retracting, and device rotation, both change the
viewport width while the panel is closed — which is exactly the untested path.

**Proposed fix** — make placement a function of the current layout rather than a
value cached at open time. Fixing the impure updater (above) addresses the
reopen path; this additionally covers resize-while-closed:

```diff
-  // Re-place on resize and on any scroll (the top bar is not sticky on every route, so a scroll can
-  // move the bell). Placement itself happens in the click handler and in these listeners -- never
-  // synchronously in an effect body (react-hooks/set-state-in-effect).
   useEffect(() => {
-    if (!open) return;
     const reposition = () => placePanel();
+    // Attached UNCONDITIONALLY, not only while open: `panelBox` is the panel's only source of
+    // coordinates, and a width change that happens while the panel is CLOSED would otherwise
+    // never be reflected -- reopening would then paint the panel at coordinates measured for a
+    // viewport that no longer exists. That is the WhatsApp case exactly: its chrome retracts,
+    // and a phone rotates, both while nobody has the panel open.
     window.addEventListener("resize", reposition);
     window.addEventListener("scroll", reposition, true);
     return () => {
       window.removeEventListener("resize", reposition);
       window.removeEventListener("scroll", reposition, true);
     };
-  }, [open, placePanel]);
+  }, [placePanel]);
```

`placePanel` returns early when `buttonRef.current` is null and is otherwise a
cheap `getBoundingClientRect`, so running it while closed is safe. A belt-and-braces
alternative is to clamp at paint time in the inline style rather than trust the
stored value.

### Test coverage gap (CONFIRMED)

The Tasks page has a dedicated layout regression test,
`features/leadership-tasks/tasks-phone-viewport.test.mjs`. The notification
centre has **no** layout or viewport test: `features/notifications/` ships
`mention-model.test.mjs`, `notification-model.test.mjs` and
`read-idempotency.test.mjs`, all pure model tests. Nothing anywhere asserts on
`data-notification-bell` or `.nc-list` —
`grep -rn "data-notification-bell\|nc-list" --include='*.test.mjs'` returns
nothing.

Given that the panel's correctness rests on *measured* arithmetic against a
stylesheet that another agent is editing in the same PR, and that the previous
version of this exact code put the panel 250px off-screen, this is the single
highest-value thing to add. The assertion is cheap: at 320px and 360px, open the
bell and require `panel.getBoundingClientRect().left >= 0` and
`right <= documentElement.clientWidth`. That would also catch the
`backdrop-filter` containing-block hazard automatically.

---

## Ranked defect list

Separated by evidence class. "CONFIRMED" means I measured or reproduced it;
"SUSPECTED" means I read it from code and did not observe it at runtime.

### Blocking

1. **CONFIRMED — `/tasks` fails server-side rendering.** `initials()` is imported
   from the `"use client"` module `leadership-tasks-table.tsx` into two server
   components (`leadership-tasks-board.tsx:7`, `task-board-card.tsx:7`), so
   calling it during SSR throws. React then falls back to client rendering,
   which is what produces the `Rendered more hooks than during the previous
   render.` error. **Not PR #295's committed code** — both importing files are
   untracked, in-flight work by the concurrent Tasks agents (written 05:34–05:37).
   Fix proposed in LEAD 1: move the helper to a server-safe `.ts` module and
   re-export it from the client module.
2. **CONFIRMED — reopening the notification panel does not re-place it.**
   Measured `left:8` at all four phone widths including 375/390, where the
   correct clamp is `left:80`. Placement is cached at first open and the
   `resize`/`scroll` listeners are attached only while the panel is open, so a
   width change while closed is never reflected. Benign at the widths measured
   (the stale value is the clamp floor), dangerous wide→narrow — which is the
   WhatsApp retracting-chrome and rotation case. Fix proposed in LEAD 3.
3. **OPEN QUESTION — first click on the bell did not open the panel** on a fresh
   load at 390x844 (panel measured zero-size; the next click worked correctly).
   Reproduced once, mechanism not established. Needs a bounded repro before
   sign-off; if real, the bell is simply broken on first use.

### Should fix before merge

4. **CONFIRMED PR-introduced — `Router action dispatched before initialization`.**
   `notification-bell.tsx:117-130` dispatches the `loadNotificationFeedAction`
   Server Action from a mount effect on every admin route, racing App Router
   initialisation. The throw escapes the component's `try/catch`, so the first
   unread-badge read of a page load can be silently lost. Not present on
   `origin/main`, where `features/notifications/` does not exist and the shell
   dispatches no router action on mount. Fix proposed in LEAD 1.
5. **SUSPECTED — side effects inside the `setOpen` updater**
   (`notification-bell.tsx:220-231`). `placePanel()` and `void refresh()` run
   inside a `setState` updater, which must be pure and which React invokes twice
   in StrictMode — so the feed Server Action fires twice per bell open, and
   `setPanelBox` is called from inside another hook's updater. This is very
   likely the mechanism behind findings 2 and 3; fixing it is the first thing to
   try for both. ESLint does not catch it.
6. **CONFIRMED — no layout/viewport test for the bell or panel.** The sibling
   Tasks page has `tasks-phone-viewport.test.mjs`; `features/notifications/`
   ships only model tests, and nothing anywhere asserts on
   `data-notification-bell` or `.nc-list`. Given that this exact code previously
   shipped the panel 250px off-screen, and that its correctness rests on measured
   arithmetic against a stylesheet another agent is editing in the same PR, this
   is the highest-value addition. The assertion is two lines: at 320 and 360,
   open the bell and require `left >= 0` and `right <= clientWidth`. It would
   also catch findings 2 and the `backdrop-filter` hazard automatically.

### Lower priority

7. **SUSPECTED — `position:fixed` panel inside a `backdrop-filter` ancestor.**
   `.top`'s `backdrop-filter` makes it the containing block, so the panel's
   coordinates do not resolve against the viewport as `placePanel()` assumes.
   Measurement confirms the two frames coincide today because `.top` sits at
   `(0,0)` full width — load-bearing coincidence. Covered by the test in item 6.
8. **SUSPECTED — hydration attribute mismatch on `/counts/herd` at 390x844 only**
   (absent at 1440x900, absent on `/approvals` and `/verify`). Mobile-width
   specific, so worth attributing, but I did not capture React's full diff detail
   and could not tie it to the bell: `EMPTY_NOTIFICATION_FEED` gives the badge a
   count of 0 on both server and client, and `PushPermissionPrompt` only mounts
   once the panel is open. **Open question**, needs the full console argument.
9. **CONFIRMED pre-existing — `.iconbtn` is 36px between 561px and 760px**, below
   the 40px tap floor, because the app-wide floor lives at
   `@media(max-width:560px)`. The stylesheet documents this at line 3591. Not
   introduced here, but the newly-visible bell is now subject to it, and a
   375x667 phone in landscape falls in the band.
10. **Verification gap, not a product defect — six of ten sweep routes
    unmeasured** (`/people`, `/work-board`, `/feed/config`, `/health/config`,
    `/leave`, `/`) plus all three `/tasks` scopes. Re-run `judge-run.mjs` with
    `JROUTES` on an idle host to close it.

### PASS — checked, no finding

- **No bare `vh` in new code.** The `vh`-then-`dvh` double-declaration pattern is
  applied consistently across all six new capped boxes (`.nc-list`,
  `.mention-pop`, `.lt-fgroup.open`, `.lt-modal`, `.ltb-colbd`, `.ltd-activity`) —
  correct for WhatsApp's retractable chrome.
- **Top bar fits at 320/360/375/390.** `.top` does not overflow
  (`scrollWidth == clientWidth` at all four), no child is clipped, and the park
  picker, theme toggle and CEO identity block all stay on-screen. The bar wraps
  to two rows, which is pre-existing `flex-wrap:wrap` behaviour, not clipping.
  The PR correctly bought back the bell's new ~47px phone footprint from two
  discretionary labels (the park chip's "· all sheds" suffix and the identity
  block's name/role lines) rather than from any control.
- **Tap targets.** `.iconbtn` measured 40x40 at 320–390; no sub-40px control in
  the top bar at any phone width; no sub-40px control inside the open panel;
  panel rows carry a 44px floor.
- **No horizontal page scroll** on any route or width measured.
- **No hooks-order violation** anywhere in the shell, bell, panel, mention
  composer or push prompt — every hook site read, plus a clean `eslint` run
  (exit 0, including `react-hooks/rules-of-hooks` and
  `react-hooks/set-state-in-effect`).
- **Every screen measured still renders**: `main` present with children, `.top`
  present, none of the four failure-boundary strings present.

---

## Reproduction artefacts

All in `scratchpad/`:

| file | what it is |
|---|---|
| `judge-run.mjs` / `judge-run.log` / `judge-run.json` | the cross-route sweep (parameterised by `JROUTES`, `JVIEWS`) |
| `judge-lead3b.mjs` / `judge-lead3b.log` | phone checks: top-bar fit, tap targets, panel placement at 320/360/375/390 |
| `judge-stale.mjs` / `judge-stale.log` | the panel-placement staleness follow-up |
| `judge-lead1.mjs` | the per-viewport hooks-error repro harness (written; only partially run) |
| `judge-shots/` | screenshots — per route/viewport, plus `bell-open-{320,360,375,390}.png` |

Nothing in the repository was modified and nothing was committed. Every fix in
this report is a proposal only.
