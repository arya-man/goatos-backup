# Dashboard Automation Deep Coverage Audit - 2026-09-21

## Scope

Audit whether dashboard automation covers deep business journeys across the non-vaccination
modules: weighing, feed, sales, procurement, counts/herd analytics, health/ICU/death,
work board/action center, calendar, people, and operations.

## Current Coverage

`tools/dashboard-automation/module-journeys.json` owns all live smoke routes by module.
`tools/dashboard-automation/run-module-journeys.mjs` runs each module by passing route names,
visible text assertions, and read-only safe-click targets into
`apps/admin-web/scripts/smoke-visual-live.mjs`.

The live smoke validates route health, forbidden production failure strings, route identity,
basic visible text, responsive layout, accessibility, pagination controls, core read-only
interactions, and manifest-declared safe clicks on desktop and mobile.

## Confirmed Gap

The manifest `coverage` entries are metadata in the receipt. They do not currently execute
domain assertions for sold/dead/ICU lifecycle, feed reduction after sale, procurement intake
state transitions, work-board card lineage, calendar event ownership, or people permission
edges. Those invariants exist in focused backend/admin-web tests for several modules, but the
dashboard automation has not yet stitched them into a browser-plus-read-only-data sentinel.

## Priority Additions

1. Add read-only sentinel queries per module and fail the module journey when the sentinel
   contradicts the rendered route state.
2. Add a lifecycle cross-module sentinel: live -> ICU/under-treatment -> recovered, live -> sold,
   live -> death review -> approved dead, and rejected death resumes health work.
3. Add sales-to-feed coverage: confirmed sale exits animals, sold analytics includes the deal,
   herd counts drop the animals, and feed-reduction notification/reminder evidence exists.
4. Add procurement-to-health/vaccination/feed coverage: accepted intake appears in procurement,
   source/arrival/health-pending states are visible, and downstream eligibility exclusions are
   reflected without mutating staging or production.
5. Add work-board/action-center lineage coverage: card -> drawer/detail -> subtasks -> audit
   log using stable row identifiers, with no `backend_down`, board-load, or generic error screen.
6. Add calendar ownership coverage beyond PC vaccination: due, completed, missed/history,
   owner filter, and drive detail must reconcile to backend event rows.
7. Add people/ops coverage for permissions and leave/routine state filters, including the
   known dead-screen sweep lens from `docs/decisions/per-person-page-access.md`.

## Non-Goals

This audit did not mutate STG, OCI, production, or local business data. It did not certify that
the above gaps are fixed.
