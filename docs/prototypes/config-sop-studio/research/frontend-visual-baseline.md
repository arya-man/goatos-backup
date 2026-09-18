# Frontend visual baseline and integration judge

Date: 2026-09-16. Source snapshot: `origin/main` `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`. Browser baseline: authenticated `https://dashboard.mesha.sg`, CEO/CXO company scope, Chrome desktop viewport 1728×819. The served deployment SHA was not independently verified; live UI observations and source findings are distinguished. All interactions were read-only navigation, accordion expansion and opening an existing inline editor/detail modal. No value was entered, saved, published or deleted. Primary checkout untouched.

## Verdict

**Current mock does not meet the requested existing-product UI/UX baseline.** Its colors are related, but navigation, information architecture, page roles and several interaction patterns diverge. Existing operational surfaces cannot be replaced by a generic settings page for every module. The missing generic configuration should become new controls and sources that feed those surfaces, inside the existing shell.

This was a representative visual audit plus complete visible-sidebar inventory, **not** visual certification of every page or responsive breakpoint. Every sidebar leaf below is classified so unvisited routes are explicit. No mobile or light-theme parity claim is made.

## Validated visual evidence

All screenshots are actual viewport captures saved under `research/visual-baseline/` and visually inspected in the tool response. They show loaded intended pages, not login/error/loading states. They are local reference evidence, not publication assets.

| Screenshot | What was visually checked |
|---|---|
| `weighing-analytics.png` | Filter strip, selected period, seven tab segments, five weight cards, three ADG cards and dense pen table; existing shell and exact Sales submenu. |
| `sales-config.png` | Breadcrumb/title/action row and sales register; primary Record sale, secondary Tag animals to sale, clickable rows and deal-status badges. |
| `feed-config.png` | Ration Rules page, scope explanation, compact inline filters, sortable table, effective-date badges, configured-zero distinction, Edit rate. |
| `feed-edit.png` | Existing inline row edit expands in place with labelled number field, explanatory text, Apply and Cancel. Nothing saved. |
| `health-config.png` | Treatment-protocol register, Add disease, search, age-band/draft-state filters, published version and row Edit. |
| `procurement-sop.png` | Existing SOP card library, New SOP, search and a card containing questions/steps/published/proof metadata. |
| `sop-detail.png` | Existing centered, bounded, scrollable detail modal; metadata grid, gates, published questions, sticky footer Close / Change SOP. No edit or publish executed. |
| `vaccination-plan.png` | Existing plan card, published status, effective date, park applicability, schedule table and Open V10 draft affordance. |
| `counts-analytics.png` | Six headline cards, window control, explanation of live versus period data, line chart and same shell. |
| `work-board.png` | Search, assignee/park/date/module controls and To do / In progress / In review / Done lanes with operational cards. |
| `milk-preparation.png` | Others-group leaf, preparation date, quantitative summary cards, verification-state badges and session table. |
| `mock-items.png` | Current mock baseline at `http://127.0.0.1:4320/#Items/Registry`, contrasted against the same-size live shell. |

The specifically referenced later user screenshots were not supplied as file paths to this subtask, so this report does not claim separate inspection of them. The live Weighing and Sales navigation were inspected directly instead.

## Exact live navigation inventory

**V** = loaded destination visually inspected. **A** = rendered live accessibility/DOM content inspected; no screenshot/layout certification. **E** = live error screen visually confirmed. **N** = navigation only (none remaining after the extension below). A/E are not visual passes. Multiple module accordions remain expanded together; each leaf is a real link and the active leaf is highlighted. Query parameters carry scope, commonly `?scope_mode=company`; table filters/pagination are additional URL parameters.

| Group | Exact leaf label | Route | Coverage |
|---|---|---|---|
| Top-level | Approvals | `/approvals`  V |
| Top-level | Verify | `/verify`  A |
| Top-level | Tasks | `/tasks`  V |
| Top-level | Work Board | `/work-board` | V |
| Counts | Herd Analytics | `/counts/analytics` | V |
| Counts | Counts Breakdown | `/counts/breakdown`  A |
| Counts | Herd Operations SOP | `/counts/sops`  A |
| Weighing | ADG Analytics | `/weighing/analytics` | V |
| Weighing | Weighing SOP | `/weighing/sops`  A |
| Sales | Summary | `/sales/sold`  A |
| Sales | Farm value | `/sales/farm-value`  A |
| Sales | Load wise | `/sales/loads`  A |
| Sales | Market analytics | `/sales/market-analytics`  A |
| Sales | Buyer analytics | `/sales/buyer-analytics`  A |
| Sales | Vendors | `/sales/vendors`  A |
| Sales | Sales Config | `/sales/config` | V |
| Feed | Feed Config | `/feed/config` | V |
| Feed | Feed Analytics | `/feed/analytics`  V |
| Feed | Feed SOP | `/feed/sops`  A |
| Preventive Care | Vaccination | `/vaccination`  E |
| Preventive Care | Live Drive Tracker | `/vaccination/live-tracker`  A |
| Preventive Care | Vaccination plan | `/vaccination/plan` | V |
| Procurement | Source Entry | `/procurement/source-entry`  A |
| Procurement | Vendors | `/procurement/vendors`  A |
| Procurement | Feed Purchases | `/procurement/feed-purchases`  A |
| Procurement | Animal purchases | `/procurement/animal-purchases`  A |
| Procurement | Procurement SOP | `/procurement/sops` | V |
| Health | Health Analytics | `/health/analytics`  A |
| Health | Health Config | `/health/config` | V |
| Others | Milk Preparation | `/counts/milk-preparation` | V |
| Others | Milk SOP | `/milk/sops`  A |
| Others | Live Monitor | `/herd-signals`  A |
| Others | Audit Log | `/operations/audit`  A |
| Others | People / HRMS | `/people`  A |
| Others | Leave | `/leave`  A |

35 sidebar leaves after the extension: 12 loaded destination pages visually inspected, 22 rendered live DOM/accessibility observations (one heading-only), and 1 visually confirmed error; additionally one inline edit and one SOP detail modal inspected. Source route inventory also includes non-sidebar/detail/legacy surfaces: calendar and drive detail, action-center/actions, counts/herd, feed/direction and feed/packing, goat detail, operations/dlq, procurement load detail, vaccination execution shed, vaccination plan edit, verification alias, weighing/weights, workflows and workflow row. These are not additional sidebar leaves and were not visually covered here. Do not add them to primary navigation merely because route files exist.

## Existing shell and component contract

Source references below are at the pinned revision.

- `apps/admin-web/components/mesha-shell.tsx:220-256` derives navigation from the backend contract and route paths, and parses shared scope once. `289-325` controls rail and independent accordion state. Collapsed-rail group icons navigate to a first leaf, rather than invisibly toggling children.
- `apps/admin-web/app/mesha-theme.css:1-38` defines dark/light semantic tokens, the brand, panel layers, status colors, on-brand text and radius. `48-58` defines the sticky 58px topbar and 62px rail. `498` defines the 256px scrollable sidebar; `531` the independently scrollable main; `558-566` button styles and disabled treatment.
- Live topbar: left hamburger and circular logo plus Mesha; right All parks scope when appropriate, theme toggle, disabled notifications surface and account menu with CEO / CXO plus tenant/active parks. The prototype's role select and OPERATING SYSTEM label are a different interaction and layout.
- Live navigation: actual outlined SVG icons, group chevrons, dot-indented submenu links, active leaf with green left accent and soft fill; top-level Work Board uses filled green active treatment. The prototype's Unicode symbols and module buttons are visibly different.
- `apps/admin-web/components/data-table.tsx:15-48,74-81` is the shared table contract: backend-defined column labels/visibility/sortability; honest page-local sorting over server-paginated rows; expandable row groups that preserve column alignment and do not refetch on open/close.
- `apps/admin-web/lib/scope.ts:20-55` models company/park/date scope. Existing filters link into URL state. A new mock should preserve that navigation behavior rather than rely only on unrelated `#Module/Tab` vocabulary.
- `apps/admin-web/features/sops/sop-library.tsx:78,182-207,304-328,416` supplies the SOP card library and bounded detail modal pattern. Extensions should preserve the library entry route before opening the existing builder or additional dependency view.

## Concrete mismatches and required changes

### P1 — Navigation replacement hides the actual product

Mock has Items Config, Common workspace, Workflow links, Run insights and eight flat module buttons. It omits Approvals/Verify/Tasks/Work Board and all 31 grouped real leaves. Module selection exposes synthetic Business rules/SOP workflows rather than the current menus. This prevents demonstrating that new configuration feeds existing pages.

**Required:** retain the exact sidebar inventory and group ordering above; add shared configuration as an additive, clearly named destination, while preserving all current leaves, casing and breadcrumbs. Keep multiple accordion groups open, active-route matching and independent scrolling. A not-yet-mocked existing route must have an explicit coverage label rather than silently open an unrelated generic settings page.

### P1 — Existing Config pages have different jobs

Sales Config is a transactional register and market-survey authoring screen, not only numeric eligibility rules. Feed Config edits ration relationships and schedules with effective dates. Health Config authors versioned disease courses by age band. Vaccination plan distinguishes published and draft versions and applies to park scope. Mapping all four to the same generic key/value card would remove existing capabilities.

**Required:** keep their current title, sections, actions, filters and table anatomy. Add item-source selection where the actual field exists, policy editing where the domain owns it, and links back to shared configuration. Do not rename these screens or overwrite their current meaning.

### P1 — Local Run insights does not demonstrate integration with actual analytics

The current local run counters are valid simulator metrics, but they do not establish how shared item/category/config changes affect ADG, Feed, Sales, Health or Herd analytics. Those pages use distinct populations, dates, units and aggregation grains.

**Required:** preserve each existing analytics page. Show representative configured labels/options/threshold sources feeding its existing filters/cards/table, with a clear distinction between authored policy and computed values. Keep the seven Weighing tabs exactly: General, Breed-wise, Birth-wise, Pen-wise, Weight-wise, Time-wise, Comparison. Keep Park, Period, Weighing, Sex, Origin and Download placement/order. Do not turn ADG/realized price/current population into editable parameters.

### P2 — Shell dimensions and controls drift despite similar colors

Mock uses a 64px fixed header, 220px sidebar, differently padded content, role dropdown and a permanent three-step onboarding strip. Live baseline has the 58px sticky bar, 256px sidebar, scoped controls and route-specific page header/filter patterns. The mock uses oversized bold Items registry heading compared with compact live Config headings.

**Required:** use the actual semantic theme tokens and shell dimensions/components where practical. Preserve the existing canvas as an inner page/editor surface; shell fidelity does not require throwing away the functional builder. Place prototype labeling unobtrusively without replacing park/account controls.

### P2 — Form/table/modal patterns need contextual consistency

Mock relies on universal cards/modals and a hierarchy rail. Live Feed edits inline within table rows, Health uses a protocol register, SOP library uses cards with status metadata then a centered detail modal, Sales actions lead to transactions. These differences are purposeful workflow affordances.

**Required:** reuse compact labelled filters, existing button labels and outlined/primary hierarchy; maintain row/status badges, dense table typography, units in labels, effective dates, validation and explicit Cancel. Item hierarchy may be a new local control but should live inside the same card/table shell and not create a second app-wide navigation rail that competes with modules.

### P2 — Completion states should connect to existing operational vocabulary

Work Board uses To do → In progress → In review → Done; completion does not equal approved. The mock's Mark complete → arrival accepted is useful for prerequisite demonstration but does not show verification/proof or operational card projection.

**Required:** state whether a prerequisite means work submitted, work approved or simply acknowledged; demonstrate where generated work appears in the existing Work Board/Tasks surfaces. Keep event/subject/version semantics from backend research. The generic dependency diagram is additive, not a replacement operational board.

## Acceptance plan for further refinement

1. Capture matching desktop and narrow-screen views of the original and additive mock shell at the same viewport; verify all 35 exact labels/routes and accordion states.
2. For each affected module, compare before/after title, breadcrumbs, primary/secondary actions, tabs, filters, table columns, units and empty/error/loading states.
3. Demonstrate one catalog addition reused through at least two existing module authoring surfaces without duplicate item identities; demonstrate that retiring it preserves historical display.
4. Demonstrate a configurable rule in its authoritative Config surface and the existing consumer readback/analytics source, distinguishing computed outputs.
5. Show event-linked downstream work in the current operational vocabulary, with scope, version pin, proof/approval boundary and no duplicate task creation.
6. Repeat real page checks against `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, `Weights could not be loaded`; none appeared in the nine loaded baseline views here, but this observation is not a performance or availability certification.
7. Capture and visually inspect the 22 DOM/accessibility-only destinations, investigate the live Vaccination error, and cover details/editors/responsive states before claiming whole-frontend visual certification. Do not call this audit a complete regression suite.

## Deployment/data qualification

The source revision and live dashboard may differ. Separate staging discovery reports indicate `sop_definitions` has `category_key`, `subcategory` and triggers, while the source snapshot's new calendar configuration table may not yet exist on staging. Therefore frontend choices must use verified served contracts, not assume source migrations are deployed. Item taxonomy and SOP taxonomy must also remain distinguished. No database content was read by this visual subtask.


## Extended read-only destination audit (independent judge)

All 35 sidebar destinations have now been navigated to in authenticated Chrome. This is **destination coverage, not whole-frontend visual or functional certification**. Of the 26 additional destinations, three loaded pages were visually inspected in screenshot tool output (Approvals, Tasks, Feed Analytics), 22 had live rendered DOM/accessibility content inspected, and Vaccination showed an error. Newly viewed screenshots were not persisted; only the earlier file list above is durable image evidence. No new/create/save/approve/reject/access controls were invoked, no form values were entered, and no proof media was explicitly played. Detail routes, alternate tabs, editors, mobile, permissions and timing behavior remain outside this pass.

| Additional destination | Live pattern and limits |
|---|---|
| Approvals | Four summary cards; Pending/Approved/Rejected tabs, type and farm segments; Type/Subject/Raised/Status/Action table. Empty pending state rendered. Visually checked. |
| Verify | Apply/Clear filters, To verify/Accepted/Rejected counters, Actions evidence queue with captured/queue/review timing/status/reason/watch columns. Rows carry `vi_play=1`; deliberately did not open proof playback. DOM checked. |
| Tasks | Operations breadcrumb, New task, For me/Raised by me/Team progress tabs; dense task/deadline/assignee/raised-by/status/evidence table beside Selected task panel. Status vocabulary Open/Doing/Done differs from Work Board review lanes. Visually checked. |
| Counts Breakdown | Farm, Stage, Breed, Pen, Gender filters; Apply filters/Open all; expandable sortable head-count table; pagination; breed, stage/sex and occupancy summaries. DOM checked. |
| Herd Operations SOP / Weighing SOP | Same New SOP/search/card library; counts has published Pen Reconcile/Birth Recording/Death Recording/Shifting; Weighing Session card. Actual versions and proof metadata remain domain-specific. DOM checked. |
| Sales Summary | Sold cards, sold weight distribution, monthly and breed price charts, Buyers table and Deals/Sales ledger. DOM checked. |
| Farm value | Farm segments, value section with disabled-until-change Apply, By category summary. This is valuation, not sale eligibility. DOM checked. |
| Load wise | Load by load reconciliation table. DOM checked. |
| Market analytics | 30/90 days, six months/year ranges; latest prices by city table; separate carcass/offals/live price series and city tabs. Observed prices are data, not default item prices. DOM checked. |
| Buyer analytics | Buyer-level table. DOM checked; no buyer details opened. |
| Sales Vendors / Procurement Vendors | Add vendor; search and record-type/status/state/city/breed filters; disabled-until-change Apply filters; paginated vendor register. Same component shape, different vendor populations/types. No contact values copied to this report. DOM checked. |
| Feed Analytics | Consumption/Stock/Per Animal/Experiment/Execution tabs; date window; five metrics and stacked feed charts. Explicit explanation: quantities are DIRECTED sheet amounts, not measured consumption; execution metric is verifier approval. Cost charts price at latest feed load rates. Visually checked. |
| Feed SOP / Milk SOP | Shared library pattern, distinct published Distribution/Packing/Transport and Preparation/Feeding cards; proof-before-apply metadata. DOM checked. |
| Live Drive Tracker | Drive-day header, live refresh interval, Full Schedule/Command Board links, Park/Vaccine/Operator/Pen/Status filters; six administration-grain metrics; operator/pen/combo/activity/attention/verification sections. Correct zero-drive state; combo animals explicitly distinguished from dose administrations. DOM checked. |
| Vaccination | **Failed to render**, error reference `1009743198`. See finding below. |
| Source Entry | Source Entry Board; New load section; full lifecycle state segments from source warmup to intake/rejection/defer/block/cancel; Supplier warmup — Holding Farm table, search and Filters. Cannot replace with only generic item configuration. DOM checked. |
| Feed Purchases | Record purchase, farm and delivery-state tabs, Purchases register with quantity, rate, vendor, payment and balance data. Actual transaction prices must remain separate from suggested catalogue prices. DOM checked. |
| Animal purchases | Loads table followed by Animals filter and Awaiting decision/Accepted/Rejected/All tabs; per-animal evidence and productivity sections; verdict note plus Accept/Reject. No verdict/proof actions invoked. DOM checked. |
| Health Analytics | Overview/Diseases/Mortality/Treatment/Diagnosis engine tabs, disease/cause metrics and monthly death/case charts. DOM checked. |
| Live Monitor | Only Herd Signals breadcrumb and heading were present in the observation; no data panel was observed. **Heading-only coverage**, not a successful monitor certification. |
| Audit Log | Search, Span of control operator filter, Activity trail table, cursor pagination, advanced raw filters; Export disabled. Initial observation incomplete, follow-up rendered rows. DOM checked. |
| People / HRMS | Staff search, park/department/status filters, Apply, All people directory; per-row Access buttons; some module pills disabled. Access controls never opened or modified. DOM checked. |
| Leave | Who approves leave panel, Save disabled; Waiting for you and All leave requests empty states. No approval configuration changed. DOM checked. |

### Live issue found, separate from mock changes

`https://dashboard.mesha.sg/vaccination?scope_mode=company` displayed **Something went wrong** and **This screen failed to render. The error has been reported. Try again or reload the page. Error reference: 1009743198** in both accessibility output and screenshot. The surrounding authenticated shell rendered. The adjacent live tracker and plan destinations were available. This is an observed served-page failure; its cause and deployment SHA were not diagnosed here and it is not attributable to local mock edits. Do not label the whole existing frontend green. No retry that might obscure the initial state, deployment, or production change was made.

### Refinement implications from extended patterns

1. Shared configuration is additive to existing task, transaction, approval, verification and analytics destinations. Keep contextual primary buttons and exact titles rather than swapping every route for the new editor.
2. Item price defaults, transaction prices, latest-load-derived feed costs and observed market prices are four distinct concepts. Consumer previews should identify which is proposed configurable and which is calculated/read-only.
3. Event dependency demos should say whether gating means acknowledged, submitted or verified; existing Tasks, Work Board and Verify use different state vocabularies.
4. Preserve domain grain: administration versus animal, pen versus individual, directed feed versus consumption, current value versus actual sold amount. Generic catalogue reuse cannot flatten these distinctions.
5. Never copy live personal/contact values into mock seeds merely to reproduce table anatomy. Use clearly illustrative rows and preserve the controls.
