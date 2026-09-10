# Work Board: one row shape, one read, two surfaces, five lenses

Maintainer decision 2026-09-10 (the Work Board Build Plan). Status: v1 built on
`feat/work-board`; reads and flags only, no assignment.

## What it is

A Jira-shaped board of every operational module's work for **one park and one
business day**, read by the admin-web page `/work-board` and the phone's **My work**
screen through the **same route**, `GET /work-board/rows` (plus `/work-board/summary`
for the lane counts). Each card is one module work item; the four columns are derived
on the server from the work state. Nobody moves a card by hand.

## The row contract

`backend/internal/workboard/domain.Row`. Every module emits exactly this: module,
source type + id (the same ref-type string `verification_items` carries), park, pen
(shed id + partition label + the display composed once through `platform/oploc`),
business date (one spelling; each adapter maps its own), the pinned clock and a farm
worded clock label, work state, severity, owner + owner state, backend-owned title and
subtitle, and the three counts a card shows.

Rules that are the contract, not detail:

- **Work states are reused verbatim** from `processintegrity/domain.WorkState`: the
  eleven values Action Center already renders. A twelfth is a maintainer decision.
- **Lane is derived**: scheduled/due/overdue/deferred/missed → To do; in progress /
  proof pending / rejected / blocked → In progress; verification pending → In review;
  completed → Done. (`domain.LaneFor`.)
- **Owners are never invented.** A module with no assignee reports `missing`; a claim
  pool (health sessions, toxin steps) reports `pool`. The vaccination operator rule
  against fallback owners applies to the board unchanged.
- **Verification is a row of its own**, keyed by the same source tuple, not a flag on
  the work row. That is what gives the board a cross-module view on day one with no new
  table.
- **Counts come from the server.** Lane headers and tiles are whole-filter aggregates
  (`/work-board/summary`); re-summing a fetched page is the banned shape.

## Isolation, in both directions

A Source (`workboard/ports.Source`) lives **inside its module's package** under
`adapters/boardsource/` and reads only that module's tables plus the org tables every
module may read (`locations`, `workforce_members`, `shed_partitions`). The board never
reads a table. Weighing's source therefore reads `weighing_work_items`,
`weighing_campaign_sheds` and `weighing_observations` and nothing else, and the
weighing free-flow guard still scans it.

Sources in v1: weighing work items, feed transport tasks, verification items, counts
approval requests, milk feeding tasks, health treatment sessions, PC care tasks, and
vaccination (wrapping the existing process-integrity read). Procurement and toxin have
no source yet and the board hides them until they do.

## The read

A **global keyset** over `(module, source_type, source_id)`, bounded to one tenant,
park and business date per request. Sources are visited in board order; the cursor
names where the previous page stopped, so a page touches at most the source it stopped
in and the ones after it. Sources are never paged independently and merged
afterwards. "All parks" on the web is one request per park.

## Access

- `work_board.read` opens the board. Alone it is the **operator lens**: the read is
  clamped to rows the caller owns (owner user id = actor) and `own_rows_only` is true.
- `work_board.oversee` sees everyone's rows inside park scope. Park heads hold it at
  park scope; directors and the CEO at tenant scope.
- **Which modules** a caller sees is not decided by these two: the board intersects
  the modules their existing permissions open (`workboard/app.VisibleModules`) with the
  registered sources. A Feed Director sees feed; the CEO sees everything.
- Park scope goes through `ResolveAuthorizedParkScopeForCapabilities`; a tenant-wide
  caller must name a park (`park_required`).
- Migration `000291` writes the per-person rows (web + mobile) the 2026-08-24 cutover
  could never have written, in the `000245` shape, and the department grants that make
  the phone module reachable for field principals.

## Surfaces

- Web: `/work-board` (page contract `work-board`, nav item after Verify, page catalog
  row under module `work_board`). Reuses the mock's taskboard styles; a local overlay
  drawer; an assignee picker with a user search; module and status chips; cursor paging.
- Phone: module `work_board`, one bottom-bar item **My work** at `/work`. Offered to
  leadership on `work_board.read` and to field principals through their department
  grant, narrowed by their mobile tick.

## Known limits, deliberately

- Feed direction, packing and wastage have no persisted work rows (they are computed
  on read and refuse past dates), so they appear only as completions through
  verification until a per-park/day snapshot exists.
- The vaccination source walks the park-day through the process-integrity cursor and
  sorts by row id (bounded, ≤20 pages); a native row-id keyset on that read would make
  it one query.
- No "not moving" signal and no director-to-park-head flag yet: those are the next
  phase of the plan.

Pinned by `workboard/app` (keyset, module filter, owner push-down, summary),
`workboard/adapters/http` (operator, park head, tenant-wide and director lenses), and
one database round-trip test per source.
