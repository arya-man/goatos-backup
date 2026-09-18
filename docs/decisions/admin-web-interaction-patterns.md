# Admin-web interaction patterns: what a click may cost, and what a control may be

Maintainer decision 2026-09-18, on the Tasks page, after catching all of these in one afternoon:
"put guards and local CI checks -- this kind of pattern should never be allowed in any feature
in future". Machine gate: `make admin-web-interaction-patterns-guard`
(`tools/agent-hooks/check-admin-web-interaction-patterns.mjs`, in `make guardrails` and the
admin-web job of `make ci-local`), plus `make admin-web-local-overlay-guard` for the drawer.

## What went wrong

The `/tasks` page was built as a set of route navigations. Each cost the maintainer a full
server render of the page (~0.9 s in his Chrome) and a flash of the route's `loading.tsx`
skeleton, for an interaction that had every byte it needed already on screen:

| click | what shipped | what it cost |
|---|---|---|
| a card | Next `<Link href="?task=…">` -> route re-render -> drawer | 0.9 s + skeleton flash |
| status in the drawer | form post -> `revalidatePath` -> full route re-fetch | whole page flickers |
| Board <-> List | `<Link href="?view=list">` -> route re-render | 0.9 s to swap two views of the same rows |
| Assignee filter | coloured square standing in for a checkbox | "I should never see such an ugly box in place of a checkbox" |
| Deadline | `<input type="date">` | the OS calendar in a console that has its own |

The repo already had the overlay rule ("same-page drawers are client-local state"); it was
not machine-checked for this feature's shape, and the other four had no rule at all.

## The rules

1. **A same-page overlay opens client-locally.** The card/row keeps its real href (no-JS,
   middle-click), a delegated CAPTURE-phase click listener intercepts it, the drawer opens
   from the row already in memory, the URL gains its param through `pushLocalOverlayUrl`,
   and detail not in the list payload (notes, activity) is fetched INSIDE the drawer by one
   server action with a one-line loading state. Close / Escape / scrim / Back pop history.
   Deep links still render open from the server. Reference:
   `features/leadership-tasks/task-drawer-host.tsx` (measured: 82 ms open, was ~900).
   Guard: `check-admin-web-local-overlays.mjs` (pins that host as required wiring).

2. **An in-place write returns the row; it does not also revalidate.** A server action
   called from a drawer, a status menu or a composer returns `{ ok, task }`; the client
   publishes the row to the feature's row store (`task-row-store.ts`) and the board, the
   table and the drawer re-render from it. `revalidatePath` on top of that re-fetches the
   whole route -- that IS the flicker. A form post that must refresh the page `redirect()`s
   instead. Never both in one function. Guard rule: `revalidate-in-returning-action`.
   Reference: `changeLeadershipTaskStatusInPlaceAction` (only request on the wire is the
   action POST; pills adjust from the store).

3. **A view toggle is presentation state.** Board <-> List, table <-> cards: `TaskViewProvider`
   + `TaskViewToggle` + `TaskViewBody`, `replaceLocalOverlayUrl` for the URL, the heavier view
   lazy-loaded. Data-changing tabs (scope, status filter) still navigate -- they change what
   the server returns. Guard rule: `view-toggle-navigation` (a `view=` href).
   Reference: `features/leadership-tasks/task-view-switch.tsx` (measured: 79 ms, zero route requests).

4. **A tick is a real checkbox.** A multi-select dropdown ("All" + names) is a list of
   `<label><input type="checkbox">…</label>` rows; style the box, never fake the control with a
   coloured `<span>`/`<button aria-checked>`. Guard rule: `fake-checkbox`.
   Reference: `features/leadership-tasks/task-people-dropdown.tsx`.

5. **One date field.** `components/themed-date-picker.tsx` (`ThemedDatePicker`) for a day;
   hour/minute `<select>`s beside it for a time (`task-write-forms.tsx`). Never
   `<input type="date|datetime-local|time|month">`. Guard rule: `native-date-input`.

6. **A list payload carries what the list renders.** The task list dropped the activity feed
   and notes (77.5 KB -> 45 KB); the drawer fetches them. Backend rule, not statically
   guarded: the latency gate (`tools/perf/hot-paths.admin-all.json`, p90 <= 300 ms) and the
   payload-size column are the evidence.

## How the guard works

Whole-tree scan of `apps/admin-web/**/*.{ts,tsx}` (tests, generated client and `.next`
excluded), count-ratcheted per `(file, rule)` in
`tools/admin-web-interaction-patterns/baseline.json` -- shrink-only, like the phone-viewport
guard (`docs/observability/GUARDRAIL_RATCHET.md`). The tree carried 86 findings when the
guard landed (23 native date inputs in older forms, 61 returning actions that also
revalidate, 2 ARIA-faked checkboxes); each is debt to pay down, never a licence. A new
finding in any file fails; a fixed one must come off the books in the same change
(`--update-baseline`). `interaction-guard:ignore: <reason>` on the line or the line above is
the reviewer-facing escape hatch.

The self-test is adversarial per rule (21 cases) and also pins the reference implementations
named above, so the guard's own advice cannot rot: `ThemedDatePicker` exists, the people
dropdown holds a real `type="checkbox"` inside a `<label>`, the view switch calls
`preventDefault` + `replaceLocalOverlayUrl`, the row store exports `publishTaskRow`, and the
in-place status action has no `revalidate*` call.

## Stated blind spots

A checkbox faked with a bare `<button className="on">` and no ARIA; a view param not named
`view`; a revalidate reached through a helper; a component that wraps a native date input.
The runtime proof for a browser-visible change -- Chrome on the running branch, both widths,
with the network panel open on the click -- is still required (AGENTS.md "UI fixes require
real-surface proof"). A green guard says the shape is right, not that the page is fast.

## Measured on the branch (2026-09-18, maintainer's Chrome, dev server)

| interaction | before | after |
|---|---|---|
| card -> drawer visible | ~900 ms + skeleton | 82 ms (feed 170 ms later, inside the drawer) |
| status change in drawer | full route re-fetch (986 ms), page flicker | action POST only; card moves, pills follow |
| Board -> List | ~900 ms route render | 79 ms, zero route requests |
