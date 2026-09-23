# Frontend / Admin-web UI Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

- Current admin-web frontend scope supersedes the old dashboard/admin product
  surface. For admin-web UI work, read
  `context/frontend/current-admin-web-scope.md`: build the connected Admin
  Config + Preventive Care (PC) Vaccination + vaccination execution context slice (rendered inside
  /vaccination, with shed detail under /vaccination/execution/sheds/{shed_id}),
  with Control Tower summarizing only process
  gaps. Old Operations/Legacy/SOP/counts/import routes are removed from active
  admin-web and must not be rebuilt unless scope is explicitly reopened. Parks is
  NOT a separate vaccination product route or sidebar entry.
- **NON-NEGOTIABLE — the ONLY admin-web UI/UX source of truth is the mock**
  `mock/goatos-dashboard-mock.html`. PORT its layout, structure, table shapes,
  empty states, icon system, spacing, and density. It is **not a color theme**.
  **Never reuse/adapt/recolor old admin UI** (`admin-primitives.tsx`, old
  cyan/slate palette, emoji icons, collapse-to-KPI layouts) — the old admin UI
  is gone; rebuild from scratch to the mock. MANDATORY before any frontend
  `git mesha-push`: `npm --prefix apps/admin-web run check:mock-fidelity` must
  pass + visual compare to the mock.
- Frontend product taxonomy is non-negotiable:
  - **Vertical** = business operating domain/department, such as Preventive Care (PC), Parks,
    Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
    Network. A vertical owns operational context.
  - **Module** = a concrete workflow/product inside a vertical, such as
    Preventive Care (PC) -> Vaccination, Preventive Care (PC) -> future Treatment/Deworming, Procurement -> Source
    Entry, or future Parks modules. Parks is a scope/context dimension for
    vaccination execution, not the owner of a vaccination module.
  - **Command lens** = top-level cross-module screen, not a vertical or module:
    Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows.
  Preventive Care (PC) is a vertical and must not use the syringe/injection icon; the syringe/
  injection icon belongs to the Vaccination module. Counts is a separate
  vertical, so Control Tower must not show raw goat census totals as its own
  KPI. Control Tower is for gaps, adherence, exceptions, escalations, and next
  actions.
- Frontend command-room/authority guardrail: Control Tower, Action Center,
  Calendar, Protocol Adherence, and Workflows are top-level screens only. Config
  is a top-level Admin / Data Ops authority screen only. Do not
  duplicate them under procurement/source-entry, Preventive Care (PC), Parks, or any future
  vertical as routes, redirects, tabs, or nav items. A vertical can feed those
  top-level screens through a selected domain/filter/lens such as
  `?domain=procurement` or `?category=vaccination`, but it must not create
  nested routes like
  `/vaccination/adherence`, `/vaccination/config`,
  `/procurement/source-entry/action-center`, `/procurement/source-entry/control-tower`,
  or any `/parks/vaccination` nested command paths. Vaccination execution
  renders INSIDE /vaccination, never as a separate Parks route.
  **Ratified exception (maintainer decision 2026-07-19): `/feed/config`.** Feed
  authors a ration grid (ration group x shed tag x feed item -> grams per head),
  per-shed factors, the session template, and the per-workflow dispatch clock.
  That is a Feed-owned data model served by `/feed-config/*`, not protocol
  `rule_dsl`, and `/config?category=feed_direction` cannot render it. `/config`
  remains the single generic protocol-rule authority screen; `/feed/config` is
  classified `module-surface`, not `authority-screen`. This exception covers
  Config for Feed ONLY. No command lens (Control Tower, Action Center, Calendar,
  Protocol Adherence, Workflows) is exempt for any vertical, and none may be.
  **Ratified exception (maintainer decision 2026-08-06): `/health/config`.**
  The SECOND and only other entry, same shape and same reasoning. Health authors
  a treatment protocol: per disease x age band, an ordered day/session course of
  medicine + dosage + unit + route, plain actions, and critical handoffs. That is
  a Health-owned data model served by `/health-config/*`, not protocol `rule_dsl`,
  and `/config?category=health` cannot render a per-day medicine grid. `/config`
  remains the single generic protocol-rule authority screen; `/health/config` is
  classified `module-surface`, not `authority-screen`. This exception covers
  Config for Health ONLY. Canonical prose:
  `docs/decisions/health-config-authoring.md`.
  **SOP split (maintainer decision 2026-08-18): the top-level SOP Library
  (`/sops`) is RETIRED.** SOPs are per-module module-surfaces, mirroring the
  `/feed/config` shape: `/vaccination/sops` (Vaccination SOP, under Preventive
  Care), `/counts/sops` (Herd Operations SOP: birth / death / shifting), and
  `/feed/sops` (Feed SOP: distribution / packing / transport). All three render
  the same `sop-library` table contract over `/admin/sops`, scoped by SOP code
  prefix; the full-page SOP builder lives at `<module page>?compose=1`. There is
  no `/sops` route, redirect, or nav leaf any more, and `/config` stays the
  single generic authority screen.
  **SOP split EXTENSION (maintainer decision 2026-09-14): `/procurement/sops` (Procurement SOP)
  joins the same module-surface shape.** It authors the ANIMAL PURCHASE INSPECTION the phone runs
  -- the pages, the questions, which take a photo / a video / either, which are compulsory --
  as `form_dsl.inspection` of the published `procurement.animal_purchase` version; the backend
  compiles and validates it, the phone renders it, each recorded animal is stamped with the
  version it was answered on and is validated and reviewed on THAT version. No question of that
  inspection may be a Go constant or a phone string again; the legacy Go catalog is the golden
  oracle for the seeded v1 only (`make procurement-sop-guard`). Canonical prose:
  `docs/decisions/procurement-sop.md`.
  **WEIGHING SOP (maintainer decision 2026-09-15): `/weighing/sops` stops being a library
  document and DRIVES weighing.** The rules a weighing task is planned on and runs under -- the
  ways of weighing the planner may pick and the default cap; whether the evening-before FEED &
  WATER REMOVAL is `required` (every task, the 2026-09-03 rule) / **`optional` (the planner
  decides per task, on by default)** / `off` (never), the removal EVENING (`cutoff_time`;
  blank = the farm-wide `feed_water_removal_config` evening shared with deworming), the removal
  card's instruction, its CAPTURES (up to eight authored slots, each video / photo / either,
  compulsory or optional -- the evidence row stores them slot-keyed, `sop_proofs`, and the
  verifier item names each proof by its slot title and kind through
  `verification_items.media_meta`) and extra QUESTIONS the removal operator answers per pen; and
  the lump-sum video window; and the admin-web Weights / ADG Analytics pages' WINDOW (the
  period they open on, fixed date or rolling days, and the earliest calendar day; page
  settings read from the published version, never pinned) -- are `form_dsl.weighing` of the
  PUBLISHED `weighing.session` version.
  The backend validates it at save (`weighingsop/app.WeighingSOPContract`), the create is
  STAMPED with the version (`weighing_campaigns.sop_version`) and the task runs on that version
  to the end (edit, lump-sum submit, removal card and its answers all read the PIN, never the
  latest publish). The seeded document is the pre-SOP behaviour byte for byte and migration
  000315 adds it IN PLACE to the published version, so deploy changes nothing. WEIGHING STAYS
  ISOLATED: `backend/internal/weighing` never names `sop_versions`; it holds
  `ports.SOPRulesSource` and the only file naming the SOP tables on its behalf is
  `backend/internal/weighingsop/adapters/postgres/rules_source.go` (the `feedwaterremoval`
  shape). NOT authorable and never will be from that document: free-flow capture, the
  no-duplicate-scan rule, evidence-grain verification, the approve-carries-the-weight
  correction, the unconditional close gate, and the per-animal video (shown locked on; `false`
  is refused). WHO MAY PLAN is not in the document either: it is the per-person Weighing "Set
  up" tick on `/people` (`weighing.plan`), and the weighing service judges from the
  person-resolved permission set (`domain.Actor.Holds`), never the role map alone. A task's card
  OPENS at its pinned version's evening, whatever a later publish chose. No weighing rule the
  document names may be a Go literal, a phone constant or a web string again
  (`make weighing-sop-guard`). Canonical prose: `docs/decisions/weighing-sop.md`.
  **SOP split EXTENSION (maintainer decision 2026-08-22): `/milk/sops` (Milk
  SOP: preparation / feeding) and `/weighing/sops` (Weighing SOP: the
  scan-and-submit session) join the same shape** — the same `sop-library`
  contract over `/admin/sops`, scoped by the `milk.` and `weighing.` code
  prefixes, with the library documents seeded by migration
  `000186_sop_library_milk_and_weighing.sql` (`milk.preparation`,
  `milk.feeding`, `weighing.session`; library documents only, never a second
  execution engine — sop_tasks stay vaccination.drive-only per the 000175
  precedent). Any OTHER nested `*/sops` route still needs
  its own recorded maintainer decision — the five routes are named in
  `check-ia-guard.mjs` `MODULE_SURFACE_ROUTE_EXCEPTIONS`.
  **Confirmed LANDED-COST rule (maintainer decision 2026-09-01): a load's Purchase
  value is its LANDED cost — animals PLUS transport PLUS booking, labour, transit
  and transition feed — never the ex-farm animal price.** The formula
  (`procurement/domain.loadPurchaseValue`) was always right; only the animal
  figure was ever imported, because the farm's Procurement DB sheet records cost
  as ONE ROW PER EVENT per load and the importer read the Purchase row and
  concluded no split existed. That understated the eight live loads by ₹2.99L
  (5.8%) and overstated profit by the same. All six cost types count; `animal`
  and `transport` keep their columns and the rest roll into `other`. An UNKNOWN
  kind rolls into `other` rather than being dropped — an unclassified cost is
  still money spent. The list keeps three columns and the itemisation appears on
  CLICK; `procurement_load_cost_lines` is the source and the three columns are a
  roll-up maintained in the same transaction, with a hand edit replacing that
  load's lines so a breakdown can never disagree with the figure beside it.
  The table also carries **Landing price / live kg** (landed cost ÷
  `purchase_weight_kg`), and three charts follow the money chart in the same load
  order: weight per animal in vs out, price per kg landing vs sale, and fattening
  days. **The fattening clock starts on ARRIVAL, not purchase** — the farm warms
  animals up at the source — and is ANIMAL-WEIGHTED across a load's sales.
  On the sale side the maintainer kept the DISPLAYED sold value and took only
  weight from `salesDB_clean`, so `sold_weighed_value` feeds price-per-kg ONLY and
  must never be summed into the sold-value column. `sold_weighed_animals` is the
  denominator that keeps the average honest: load 101 sold 66 animals but only 26
  were weighed, and dividing by 66 reports a shrinking animal that never existed.
  A load that has sold nothing reports ABSENCE, never zero.
  **The AGE clock is separate from the fattening clock and must not be merged**
  (maintainer request 2026-09-01): `fattening_days` starts on ARRIVAL and stops at
  SALE; `days_since_purchase` starts at PURCHASE and runs while the load is open.
  A load past `procurement/domain.LoadAgeAlertDays` (90, strictly greater — "exceeds
  90 days") that STILL HOLDS ANIMALS raises a daily alert to the CXO ALONE; a load
  past 90 days that sold out is history and is deliberately silent, because alerting
  on it forever would train the reader to ignore the alert. Once per day comes from
  the BUSINESS DATE in the idempotency key, never a private scheduler — the
  `FeedLowStockNotifier` pattern, required by the task-kernel lock. The notifier
  consumes the FINISHED load-wise read model so the push and the chart can never
  disagree about which loads are overdue. Canonical prose:
  `docs/decisions/load-landed-cost-and-growth.md`; schema: migration
  `000235_procurement_load_cost_lines.sql`.

  **Ratified exception (maintainer decision 2026-09-01): `/sales/config`.**
  The THIRD Config entry, same shape and same reasoning as the two above, plus a
  second half the others do not have. Sales Config is where every sales fact is
  ENTERED or CHANGED — recording a sale, tagging the animals it is made of,
  buyer and farmer-group leads, market quotes, sold-tag lists, weight checks,
  a deal's payments and status, and a purchased load's landed cost. It authors
  nothing generic and duplicates no lens; it is classified `module-surface`, not
  `authority-screen`, and `/config` remains the single generic protocol-rule
  authority screen.
  **The second half is the lock: `/sales/sold`, `/sales/farm-value` and `/sales/loads` are
  READ-ONLY.** (The Sales board was divided in two and retired on 2026-09-11: the closed-sale
  blocks, buyers, pipeline, evidence and -- last -- the deals ledger sit on `/sales/sold`, the
  live-herd valuation with the Over 35 kg card on `/sales/farm-value`, and `/sales` only redirects
  to Sold; the same lock covers all three.)
  Their backend page contracts declare NO write control at all — not a disabled
  one — so neither page can render a button, a form or an entry drawer for
  anyone, the CEO included. That is what makes entry exist in exactly one place;
  an entry form living on two screens is a form whose two copies drift. The
  authorities did not merge with the pages: `record_sale`, `record_pipeline`,
  `record_sales_deal_payment` and `update_sales_deal_status` ride `SalesWrite`,
  while `record_load_cost` keeps `LoadCostWrite`, so the sales desk sees the
  cost control disabled with its reason on a page whose other controls are live.
  The page itself is reached on `SalesRead` (the `/health/config` shape): a
  reader opens it and sees each control disabled with a backend reason rather
  than finding the leaf missing. Pinned by
  `TestSalesReadPagesCarryNoWriteControl` (mutation-tested: restoring the write
  compilation on `/sales` turns it red), `TestSalesConfigPageContract` and the
  load-cost gate's sales-director row.
  **SOP split EXTENSION (maintainer instruction 2026-09-19): `/sales/sops` (Sales SOP)
  joins the same shape** — the `sop-library` contract over `/admin/sops` scoped to the
  `sales.` prefix, with the `sales.deal` SOP seeded by migration `000366`. Unlike the
  library-only milk/weighing documents, this one is RUN: recording a sale opens one
  tasks-engine workflow from its `sales_deal` track (docs/decisions/sales-sop.md), every
  step carrying the DESIGNATION that does it (`owner` → `workflow_actions.owner_role`),
  and the tag-animals step is engine-completed from the tagging confirm, never a tap.
  The machine guard carries the same allowlist — the three Config entries plus
  the six SOP-split routes — in `apps/admin-web/scripts/check-ia-guard.mjs`;
  widening it needs a new recorded maintainer decision here first.
- Config / Protocol Rules is a generic Admin / Data Ops authority screen
  (`/config`) for CEO/COO/superadmin users. It is not owned by Preventive Care (PC) / Vaccination.
  Preventive Care (PC) / Vaccination may link to `/config?category=vaccination`, but the Config UI
  must stay category/schema-driven: changing category changes the form fields and
  `rule_dsl`; do not show vaccination fields for `feed_direction`.

## Every Load Chart Names Its Pens (maintainer request 2026-09-22)

Any graph on the dashboard that names a LOAD carries that load's PENS in a bracket beside the
name -- `131 (CPT Castro 1, CPT Castro 2)`. A load number says which invoice the animals arrived
on; it does not say where to walk, and every question a load chart raises is answered by going to
look at the animals.

Four charts today: the load weight/gain bars on `/weighing/weights`, both Load-wise charts on
`/weighing/analytics`, all five load columns on `/sales/loads`, and the Purchased loads columns on
`/counts/breakdown`. It SUPERSEDES THE SHAPE of the 2026-09-21 change that put pens on the
Load-wise tab: those rode a SUB-LINE and are now the bracket, so every load chart labels a load
the same way. The head counts stay in that tab's Pens TABLE column, which has the room an axis
does not.

**THE FIRST THREE AND THE FOURTH ANSWER DIFFERENT QUESTIONS AND MUST NOT BE RECONCILED.** The
weighing-backed charts read `weighing_shed_load_tags` and say WHERE THIS LOAD WAS WEIGHED: a pen
holding two loads is attributed to neither, and an animal that walked into an untagged pen is
absent. Counts reads the herd register and says WHERE THIS LOAD'S ANIMALS LIVE NOW, so that animal
IS there. On 2026-09-22 the gap was real and the maintainer left it: load 130 read 73 animals in
`CBE Castro 2` against 77 bought, because three had moved into untagged Yashoda pens and a fourth
was never registered. Do not teach either side the other's source.

ONE COMPOSITION, `apps/admin-web/lib/load-pens.ts` (`withLoadPens`), for the same reason `oploc`
is one. THE PARK IS PART OF THE PEN NAME -- `Castro 1` is a real pen in BOTH parks, and an
unqualified bracket on a chart that mixes them is the OL-1 name-merge defect. THE BRACKET IS NEVER
INVENTED -- no known pen means no bracket at all, so a principal without the weighing read, a load
with no tagged pen, and a sold-out load each render exactly as they did before. The axis spells
out two pens and COUNTS the rest (`+3`); the tooltip spells out every one.

Canonical prose: `docs/decisions/load-charts-name-their-pens.md`. Pinned by
`apps/admin-web/lib/load-pens.test.mjs`,
`apps/admin-web/features/weighing/load-comparison-pens.test.mjs` and
`TestCountsBreakdownLoadsNameThePensTheirAnimalsSitIn` (mutation-tested).

- Golden frontend rule for Codex, Claude, and every developer using this repo:
  admin-web/goatos-android (mobile) are renderers, not product-truth owners. Backend
  OpenAPI/app contracts must own visible navigation, route availability, page
  titles, section/table labels, filter/sort/page-size semantics, chips/tabs,
  row-click params, drawer/action labels, empty/error copy, disabled reasons,
  and summary-vs-detail field sets. Frontend may own layout, CSS, responsive
  density, icon-token rendering, focus/hover state, and local open/closed or
  selected-row state only. If a visible label/control/action is hardcoded in a
  frontend page, either move it into a backend contract plus OpenAPI/generated
  client, or document the temporary exception in `context/frontend/` before
  shipping.
  Backend-owned does not mean backend-code hardcoded live data: tenant/location/
  person/goat/shed/vendor/operator IDs, park codes/names, capacities, role/actor
  scope, permissions, and business-managed dropdown vocabularies must come from
  Postgres/source-backed config and be compiled into the contract by backend.
  Stable UI text that rarely changes (nav/page titles, table/filter labels,
  chips/tabs, empty/error copy, disabled reasons) belongs in the backend
  bootstrap contract; when it needs runtime governance, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  These entries may not relabel live/module-DB-owned options such as parks,
  sheds, breeds, SOP labels, feed items, or role/grant scopes, and may not
  override semantic option metadata such as source-system publishability.
  Frontend must not ship local defaults that later get replaced by async config.
  Backend code may hold only product contract shape, compile mapping, and
  intentional default skeletons for missing optional UI config rows; live/domain
  values stay in canonical module tables.
- User-facing copy firewall for mobile and frontend: CEO, director, and operator
  screens must use farm/product language only. Never show internal implementation,
  debug, test, or roadmap wording in visible UI copy, screenshots, empty states,
  toasts/snackbars, banners, cards, chips, buttons, bottom sheets, drawers, or
  alerts. Banned visible words/patterns include `V1`, `V2`, `debug`, `mock`,
  `fixture`, `Paparazzi`, `Room`, `outbox`, `idempotency`, `groupKey`,
  `payload`, `backend`, `frontend`, `API`, `route`, `PRD`, `TRD`, `TODO`,
  `local`, and `localhost`, unless the screen is explicitly a developer/admin
  diagnostics tool. Technical facts belong in docs, tests, logs, and code
  comments; UI must say the business thing: "Proof uploads in background",
  "Waiting for network", "Already scanned", "Needs proof", "Cannot submit yet",
  "Wrong shed", "Try again", etc. Before handing off any mobile/frontend UI
  change, scan changed strings/screenshot fixtures for internal words and inspect
  rendered screenshots for leaked technical copy.
- Same-page drawers, sidebars, modals, and popovers are client-local UI state.
  Ordinary open/close clicks must not navigate, issue a document/RSC request,
  or trigger page-level loading UI. Use `LocalOverlayLink` and a narrow local
  controller with Back/Escape/outside/X/focus restoration. When detail is not
  present in the list response, open immediately from list summary data and
  fetch only the missing detail inside the drawer through an authenticated
  Server Action/Route Handler. Query-only Next links, native anchor/forms, or
  router pushes used to toggle an overlay are banned. Keep
  `make admin-web-local-overlay-guard` at a zero legacy baseline.
- Admin-web interaction patterns (maintainer lock 2026-09-18, Claude AND Codex):
  a click costs what it changes, and a control is the console's own. Caught on
  the Tasks page in one afternoon and banned for every future feature: (1) a
  card/row click that navigates to open a drawer -- intercept in the capture
  phase, open from the row in memory, fetch only the missing detail inside the
  drawer (`features/leadership-tasks/task-drawer-host.tsx`); (2) a server action
  that RETURNS a row AND `revalidatePath`s -- the client applies the row through
  the feature row store, the revalidate is the page flicker; return-the-row or
  `redirect()`, never both; (3) a Board/List view toggle as a `view=` link --
  presentation is client state + `replaceLocalOverlayUrl`; (4) a coloured
  square or `aria-checked` button standing in for a checkbox -- a tick is a real
  `<input type="checkbox">` in a `<label>`; (5) `<input type="date|time">` -- the
  console has ONE date field, `ThemedDatePicker`; (6) `window.confirm` /
  `alert` / `prompt` -- a confirm is two buttons where the action was, never
  the browser's "127.0.0.1 says" box. Machine gate:
  `make admin-web-interaction-patterns-guard` (whole-tree, shrink-only baseline)
  beside `admin-web-local-overlay-guard`; canonical prose:
  `docs/decisions/admin-web-interaction-patterns.md`. Chrome with the network
  panel open on the click is still the proof.
- When the user asks to fix a frontend/UI issue, rendered browser review is part
  of the requested fix for Codex, Claude, and every developer. Do not treat it
  as optional judgment or defer it to the user. Reproduce the user’s route,
  viewport, scope, filters, drawer/modal state, and click path as closely as
  possible; if the user supplied a screenshot, that screenshot is the minimum
  acceptance case. Do not push a frontend fix until the changed screen has been
  opened locally and visually checked, or until you explicitly report why local
  rendering is blocked.
- Raw vaccination config/protocol tokens are never API presentation copy or
  user-facing UI copy. Codes
  such as `et_tt`, `et_tt_adult_w2`, `ppr_booster`, `blue_tongue_first`,
  `goat_pox`, the protocol family name `Preventive Care Vaccination Matrix`,
  and similar backend/config identifiers may exist in backend config,
  raw storage/contracts/DTOs, non-UI tests, or a dedicated display mapper only.
  Backend display fields (`driveName`, `vaccineLabel`, `vaccine_labels`, card
  titles/subtitles, alerts), admin-web, Android screens, Paparazzi screenshot
  fixtures, cards, rows, chips, alerts, logs visible to operators, and generated
  UI galleries must render human labels such as `ET+TT`, `PPR · Booster`,
  `Blue Tongue`, and `Goat Pox`.
  `make ui-vaccine-labels-guard` is part of the standard guardrail/local-CI
  path and must fail any direct UI leak.
- **Maintainer decision, 2026-08-02:** every user-facing notification (push,
  in-app, banner, or leadership escalation — vaccination, weighing, feed, and
  counts alike) must be MEANINGFUL, never abstract. It must carry park name,
  shed/partition label, vaccine/work-item name in human form, animal/shed
  counts, and a farm-readable due date in IST; a leadership escalation must
  name which sheds are outstanding, not just report a count. Prevents the
  count-only-abstract-notification defect (e.g. "Vaccination(s) due soon · 3"
  telling nobody which park/shed/vaccine/date). Enforced by
  `make notification-specificity-guard`
  (`tools/agent-hooks/check-notification-specificity.mjs`), which composes
  with — and does not duplicate — `ui-vaccine-labels-guard`. See
  `docs/decisions/2026-08-02-meaningful-notification-copy.md`.
- A proof PHOTO is on screen the moment its card opens, on admin-web and on the
  phone alike (maintainer decision 2026-09-14): never a blank tile, never an
  "Open photo" / "Tap to open photo" control, never a click the picture waits
  for. A tap ENLARGES the photo; it never REVEALS it. The tap-armed fetch was a
  LIST guardrail (dozens of paid object reads nobody opened) misapplied to the
  one-item detail, where a blank reads as a missing proof. Visible viewport
  photos may load, but they must cache by stable proof identity, never by the
  rotating signed URL; hidden/off-viewport photos must not prefetch. Videos still
  wait for play/open and must not auto-stream, auto-prepare, or remote-poster
  probe. Canonical prose: `docs/decisions/proof-photo-shown-on-open.md`.
- Vaccination proof grain is SOP/backend-owned. Do not hardcode "per goat",
  "shed level", "camera only", or "gallery allowed" in admin-web or Android.
  Backend SOP/form DSL/proof policy decides the proof mode, subject scope,
  minimum/maximum proof count, capture sources, and verifier instruction; clients
  render that contract. Both modes must remain supported: per-goat video proof
  and shed-level video proof. A change from one mode to the other must never
  delete the unused mode, bypass GCS proof upload, skip verifier instructions,
  or invent proof requirements in mobile/frontend state.
- For frontend code changes, perform rendered visual QA before pushing. Open the
  changed local page, capture and inspect screenshots, and compare with the
  authoritative UI/UX source of truth, the mock `mock/goatos-dashboard-mock.html`
  (port its structure, not just its colors). Old dashboard/admin pages are NOT
  the visual target and must not be reused/recolored. Before push, run the
  mandatory gate `npm --prefix apps/admin-web run check:mock-fidelity`.
  Check pixel-level UI quality: sidebar/nav alignment, tab/title spacing,
  typography, color, card padding, chart sizing, labels, icons, empty space,
  overflow, clipping, and desktop/narrow responsive states. Do not accept
  typecheck/build or a `missing_config` page as frontend visual proof. For
  admin-web, run `npm --prefix apps/admin-web run smoke:visual:live` when the
  local backend/admin-web can be started; it captures desktop/narrow
  screenshots and runs layout/a11y/token-leak checks. Open the resulting images
  under `.codex-goatos-render/admin-web-screenshots/` and include the screenshot
  review result in the handoff before pushing. Build passing means only that the
  code compiles; it does not mean the UI ships.
  Route/table/drawer/popover changes also require reproducing the exact changed
  URL and viewport, then visually checking right-edge columns, horizontal
  overflow, clipped or stripped chips, active nav highlight, row-click
  destination, drawer/popup outside-click close, and drawer/popup close button
  behavior. A screenshot supplied by a reviewer/user is a failing visual test
  case until the same route is re-opened and the rendered screen is inspected.
- When a maintainer asks to "fix frontend" or reports a visible UI defect, treat
  frontend review as part of the fix, not as optional agent judgment. Use the
  same rendered lens for every session (Codex, Claude, Cursor, or sub-agent):
  reproduce the route, inspect the changed UI, catch adjacent clipping/overflow/
  close-behavior regressions introduced or exposed by the change, and document
  the visual proof. If a focused frontend fix is green, commit and push that
  focused fix to `main` promptly; do not pile unrelated visual-smoke fallout into
  one end-of-session batch. Split newly discovered adjacent UI bugs into their
  own focused commits unless they block the original fix's visual proof.
- Admin-web route/table/drawer/popover changes require a visual-closeout checklist
  before push. Reproduce the exact URL/viewport from any user screenshot when one
  exists, then verify: no right-edge/status-column clipping; no horizontal page
  overflow unless the table owns it; chips truncate intentionally without
  character-splitting or escaping their cell; drawers and popovers close by their
  close control and by outside click/back navigation; row clicks keep the correct
  module selected in the sidebar; and opened detail views show only the scoped
  real records for the clicked row. If any of these cannot be visually confirmed,
  the change is not ready to land.
