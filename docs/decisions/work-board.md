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
vaccination (wrapping the existing process-integrity read). Procurement and toxin had no
source and the board hid them silently for two weeks -- fixed 2026-09-25, see "Every module is on
the board" below.

Added 2026-09-12, RESHAPED 2026-09-14 -- **the pen visit, as a task of its own under Tasks.**
The 2026-09-12 shape (the PC Care row deriving its state from the visit, a "Pen visit" unit
in its issue view, two pen-visit sources rowing under Preventive Care / Vaccination titled as
the work continuing) is retired: the maintainer's rule is "under Tasks only". A new module
`tasks` (`workboard/domain.ModuleTasks`, appended LAST so the keyset order of every existing
module holds; visible on `pen_visits.execute` or `leadership_tasks.read`) carries ONE
`penvisits/adapters/boardsource` source. On the visit's due day the row is titled "Pen visit ·
Castro 2", subtitled by the work that raised it ("Deworming · work done <date>",
"Vaccination, deworming · work done <date>"), clocked "Visit due <date>" / "Visit delayed ·
owed <date>", owned by the park's configured visitors ("Dinakar +1" while owed, the person who
went once submitted; the operator lens matches any configured visitor), and carries no href
(visits are phone-only). Its issue view is the one unit, visit -> verify. The PC Care row
reads the task's own state -- "completed" once its clips are approved, even while the kernel
clock waits on the visit -- and its issue view has no visit unit.

## The read

A **global keyset** over `(module, source_type, source_id)`, bounded to one tenant,
park and business date per request. Sources are visited in board order; the cursor
names where the previous page stopped, so a page touches at most the source it stopped
in and the ones after it. Sources are never paged independently and merged
afterwards. "All parks" on the web is one request per park.

## Access

- `work_board.read` opens the board. Alone it is the **operator lens**: the read is
  clamped to rows the caller owns (owner user id = actor) **plus the park's unclaimed pool
  rows** -- an untaken health session, an unassigned milk or transport task, a PC care task
  with no assignee, an approval the person raised -- and `own_rows_only` is true. A row
  owned by someone else never shows. The pool half was added after the 2026-09-11 E2E: the
  operator's board hid every health session until the moment they completed it, telling
  them "nothing to do" while work was owed. The same predicate serves the oversight
  `owner=<uuid>` filter, so a director filtering by a person sees that person's rows plus
  what that person could pick up.
- A caller whose permissions open **no module** sees an empty board, never the whole
  registry (`app.StrictIntersect`; the filter helper's "empty means all" never applies to
  the permission-derived set).
- `work_board.oversee` sees everyone's rows inside park scope. Park heads hold it at
  park scope; directors and the CEO at tenant scope.
- **Which modules** a caller sees is not decided by these two: the board intersects
  the modules their existing permissions open (`workboard/app.VisibleModules`) with the
  registered sources. A Feed Director sees feed; the CEO sees everything.
- Park scope goes through `ResolveAuthorizedParkScopeForCapabilities`; a tenant-wide
  caller must name a park (`park_required`).
- Migration `000294` writes the per-person rows (web + mobile) the 2026-08-24 cutover
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
- Counts approval requests carry no park of their own; birth and pen-move requests resolve
  the park from their payload, and a DEATH request from its subject animal's own
  `goats.park_id` (an animal never changes park, and a death is the terminal exit), read
  the way the counts module already reads it for the park-scoped death decision.
- Withdrawn verification items are not on the board: nobody owes a verdict on a proof the
  producer took back.
- A weighing bucket reads its own status alongside the item's, because CLOSE writes only
  the bucket and the kernel sweep never moves a submitted item to `closed`.
- A feed transport row is owned by the task's operator, else by the operator of its
  current attempt: the materializer names nobody and the submit lands on the attempt.
- Vaccination cards are titled by the vaccine/dose label; the wrapped read's `drive_name`
  is a synthesised "<protocol name> - <dose label>" and is never shown.
- Health rows are titled by disease and day, not by the animal's tag: the case table
  stores only the goat id, and this source may not read the herd register.
- Milk feeding is farm-grain since migration 000097, so its "pen" is the park.
- No "not moving" signal yet: that is the second half of the plan's phase 5.

## The subtask read (the issue view)

`GET /work-board/rows/{row_key}/subtasks` drills ONE row into the module's own units of
work for the issue view: each subtask has a name (an animal tag verbatim, "Whole pen", a
session, a medicine, a video), a short subtitle, an owner, a derived work state and lane,
a needs-attention flag and a chain of STEPS (`todo`, `in_progress`, `in_review`, `done`,
`rework`, `needs_attention`, `locked`). The grain is each module's own, and a row with no
finer grain returns itself as one subtask -- a live row never drills into an empty list:

| module | one subtask per | steps |
|---|---|---|
| weighing | scanned animal (tag verbatim, weight); the whole pen for a lump-sum bucket | scan/weigh -> submit -> verify -> close |
| feed transport | attempt (plus the trip as a to-do before any attempt) | film -> verify |
| verification | proof reference on the item (video / photo) | review |
| counts approvals | the request (kids / animals as subtitle) | raise -> approve -> apply |
| milk feeding | the session | prepare -> feed -> submit -> verify |
| health | treatment step (medicine + dose + route, or the action) | give/do -> verify |
| PC care | scanned animal (tag verbatim) | scan -> proof -> submit -> verify |
| vaccination | animal in the pen for that drive (its obligation) | vaccinate -> verify |
| pen visit (Tasks) | the visit itself | visit -> verify |
| any engine workflow (births, deaths, pen moves, pen returns, sales, animal and feed purchases, general SOP runs) | step of the workflow, in the SOP's words | do -> verify (verify only when the step records proof) |
| toxin | step of the procedure the round was opened on | do; the reading step adds review |

Rules that are the contract:

- **Worst first, keyset-paged.** A keyset cannot follow a sort it does not key on, so the
  RANK (needs attention 0, to do 1, in progress 2, in review 3, done 4) is the leading
  segment of the subtask key, the sort is a plain ascending sort on the key, and each
  source states the SQL twin of `domain.RankFor` once. `total` is the WHOLE count.
  Default page 10, clamped to [10, 50].
- **Same scope as the rows read.** The handler resolves the row on the caller's own board
  through `FindRow` first (park scope, module visibility, the operator lens's own-rows
  clamp) and 404s `row_not_found` otherwise; the service refuses a module outside the
  query's visibility. A caller can drill only into what the board would have shown them.
- **Isolation unchanged.** Each `ListSubtasks` is one bounded read inside the module's
  package on its own child table (observations, attempts, media refs, steps, task animals)
  plus workforce_members for a name. The vaccination drill lives in
  `processintegrity/adapters/boardsource` and mirrors the process-integrity membership
  predicates (batch + rule + shed + partition + execution date) on tables that read already
  names; it needs the pool (`WithPool`).
- **Copy firewall.** No uuid, dose code or protocol family name reaches a name, subtitle or
  step; a vaccination subtitle composes through `ControlTowerDoseLabel`, an animal with no
  active tag falls back to its display id, and the pen is always the pen.
- **Hrefs are real routes only.** `Row.Href` is the module's admin-web page for that row:
  `/weighing/weights?park=&weighing=` (no per-bucket page exists), `/feed/analytics`
  (transport has no web page), `/verify?status=all&vi_row=`, `/approvals?ap_row=`,
  `/counts/milk-preparation?mp_park=`, `/health/analytics` (reads no case parameter),
  `/vaccination/execution/sheds/{shed_id}?scope_mode=park&park=&partition_label=`. PC care
  has no web page and carries no href rather than a link that lands nowhere.

## Feed activity cards roll up by ALL-OR-ANY, not by the leftmost lane (maintainer decision 2026-09-14)

A feed card (packing, direction, transport, wastage) is one park's activity for the work-day
with its PENS listed inside as subtasks -- `Godel 1 - Part 1`, `Castro 2`, never a bare shed
base. The card's own lane is derived from those pens by one rule, applied at both grains
(bags -> pen, pens -> card):

| card lane | when |
|---|---|
| To do | NO pen has started |
| In progress | any pen is in progress or sent back, OR the pens are mixed (some started, some not) |
| In review | EVERY pen is handed in and at least one is still with the verifier |
| Done | EVERY pen is completed |

A card In progress with a pen sent back reads Rejected (same lane, amber), so the board points
at the rework.

This REPLACES the original leftmost-lane roll-up ("To do if any pen is unstarted"), which parked
a packing day that was 62 of 63 done in To do because one pen had not started. Work on the card
has begun the moment any pen has, so the card is In progress; it is In review only when the crew
has nothing left to film; it is Done only when every pen is verified. Rulebook:
`feeddirection/adapters/boardsource.rollupRankExpr`; pinned by
`TestFeedActivityCardRollsUpByAllOrAny`, which walks one card To do -> In progress (one pen in
review, one untouched) -> In review -> Done -> Rejected.

## The flag (phase 5, first half)

`POST /work-board/flags` raises a **Leadership Task** to the park's head from a board
row: title "Check · <row title>", body = the row's subtitle, pen and clock, the
director's note, and the day it was flagged from. The request names only WHICH row
(`row_key`, `park_id`, `business_date`) and the note; **the row's copy is never accepted
from the client**. The service looks the key up on the caller's own board (their park
scope, that day, the modules their permissions open), so a caller cannot flag work
outside their park or module visibility and cannot put words in the board's mouth: a row
their board does not hold is `404 row_not_found`. The body carries no row key, id or
module token (copy firewall: the park head reads it on a phone). It rides the Leadership
Tasks module for its number,
status ladder, notes and push, so the board builds no task table of its own. Gated on
`work_board.oversee` + `leadership_tasks.raise` at the route and on the `flag_park_head`
page control (the two halves of the capability-gated lock). The park head resolves from
a park-scoped `park_head` grant first, then a tenant-scoped one whose per-person park
scope covers the park; a park with no head refuses (`park_head_missing`) rather than
falling back to anyone.

Pinned by `workboard/app` (keyset, module filter, owner push-down, summary, the subtask
source resolution and page bounds), `workboard/adapters/http` (operator, park head,
tenant-wide and director lenses; the subtask read's row gate and codes), and one database
round-trip test per source for rows and one for subtasks (output strings, step states,
owner, worst-first order, keyset and total).

## Feed is four activities, direction one card per session, counted once (maintainer instruction 2026-09-25)

"No need per pen or per shed; direction, wastage, packing, transport, four cards only; for feed
direction, morning and evening separately." What changed, and why each part is there:

- **Feed direction is one card per session of the day's sheet**, titled by the sheet's own
  session label ("Feed direction · Morning", "Feed direction · Evening"; "Session N" only if a
  sheet carries no label). Each session's bag is filmed and verified on its own, so folding the
  sessions into one pen made a pen "done" only when BOTH were approved: at 11:44 on 25/09 the
  card read "0/59 done" while 13 morning bags were already approved. Packing, transport and
  wastage stay one card each. The source id is `<park>:direction:<session_no>`; the old
  `<park>:direction` is refused as a cursor.
- **Feed videos are not Verification cards.** `verification/adapters/boardsource` leaves
  `source_module = 'feed'` out of rows, counts and drills. Each feed video used to appear twice:
  inside the feed card at pen grain AND as its own Verification card at bag grain (141 of 142
  Verification cards at Coimbatore on 25/09), so the packing card said "27 pens in review" beside
  54 packing videos in the same column. A verifier still reviews them on /verify; the board shows
  their state on the feed cards only.
- **Every feed card says where each pen is.** `WorkBoardCounts` gained two optional fields,
  `in_review` and `not_started`, which split `pending`; the rest of pending less
  `needs_attention` is "started". The card's count line ("36/59 done · 17 in review · 6 not
  started"), the drawer's Pending tile and the phone card all render that split, and the drawer
  lists the pens from the same pen roll-up, so card and drawer cannot disagree (the old card said
  "59 started" over a drawer showing 11 pens not filmed). The subtitle is just the pen count, so a
  narrow card never cuts the split off mid-word. A source that does not send the split renders
  exactly as before.
- **Feed cards are a crew pool**, not "no one assigned": they carry `owner_state = pool`.

Pinned by `TestFeedCardAndItsDrawerTellTheSameStory` (every card's pen buckets equal its drawer's,
and the evening drawer never shows the morning's rework), the session split in
`TestFeedActivityCardsOnADatabaseRoundTrip`, and the feed item that must never reach
`TestVerificationBoardRowsOnADatabaseRoundTrip`.

## Every module is on the board, or says why not (maintainer instruction 2026-09-25)

The maintainer found Procurement and Toxin testing missing from the board: "it should not be like
that; in future also, if I have any task, any new module, it should automatically link to the work
board; it should not be one more task." Both had a lane on the module list from day one and NO
source feeding it, so the board hid them without a word. Nothing failed. The same hole covered
every workflow of the shared tasks engine -- births, deaths, pen moves, pen returns, sales, the
animal and feed purchase intakes and general SOP runs never reached the board either.

Three pieces, each load-bearing:

1. **One source for the whole tasks engine** (`tasks/adapters/boardsource`). Every
   `workflow_instances` row rows on the board, titled with the phone card's own words
   (`postgres.BoardCardColumns` / `BoardCardJoins` -- the one card read, exported, never copied),
   drilling into its steps. WHICH lane is one table, `engineModuleLanes`: herd operations under
   Counts, the purchase intakes under Procurement, the sale under a new **Sales** lane (appended
   last: the order is the cursor contract), general SOP runs under Tasks. Because every new
   operational feature runs on the engine (docs/decisions/sop-driven-herd-operations.md), a new
   module is on the board the day it ships with no board code. An engine module the table does
   not name still rows -- under Tasks, the catch-all -- rather than vanishing, and
   `TestEveryEngineModuleHasABoardLane` (which reads the live `workflow_instances_module_check`
   out of the migrations) fails the build until someone names its lane.
   A workflow belongs to day D when it was raised on D, when it was raised earlier and is still
   open (owed work carries forward, as a pen visit does), or when it was completed on D. Every
   engine step is owned by a designation, not a person, so every workflow is a pool row.
2. **A toxin source** (`toxin/adapters/boardsource`). Every live or signed-off aflatoxin round,
   at the park its load's code names, from the day it was opened until the day it is signed off;
   the drill is the procedure the round was opened on (its own `sop_version`). An accepted
   Positive reads severity watch (it flags the load, it does not block feeding).
3. **Two build checks that make the next gap impossible to ship quietly.**
   `workboard/app.moduleLanes` / `notBoardWork`: every module in the access catalog
   (`permissions.ModuleCapabilities`) either names the lane its work rows under or says, in
   words, why it owns no board work; `TestEveryModuleDeclaresItsWorkBoardLane` fails for a
   module with neither. `bootstrap.TestEveryWorkBoardLaneHasASource` fails for any lane -- on the
   board's list or declared by a module -- with no registered source; it is the check that would
   have caught Procurement and Toxin. Both were mutation-tested (drop the toxin source, drop the
   toxin declaration: each goes red).

Recorded as NOT board work, each with its reason in `notBoardWork`: leave approvals, leadership
tasks (the board's flag raises them), the market survey, registers, lenses and settings screens.
Moving one onto the board is moving its entry to `moduleLanes` with a source in the same change.

Visibility: the Sales lane opens on `sales.read`; the Procurement lane now also opens for the
desks that WORK a load (`procurement.animal_purchase.write` / `.decide`,
`feed.purchase.write`), not only `procurement.review`. Migration `000428` adds the two
tenant+park indexes the new reads need.
