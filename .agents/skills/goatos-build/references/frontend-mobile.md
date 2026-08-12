# Frontend And Mobile Reference

Load this when working on admin-web, operator-mobile, UI reuse, RBAC
visibility, generated clients, offline sync, media capture, or app adapters.

Canonical docs:

- `docs/architecture/operational-read-model-contract.md`
- `context/frontend/current-admin-web-scope.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/execution/calendar-vaccination-slice-parallel-handoff.md`
- `docs/decisions/calendar-ownership.md`
- `docs/frontend/admin-web-engineering-quality.md`
- `context/architecture/operational-kernel.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/target-repo-structure.md`
- `docs/mobile/rfid-keyboard-reader.md`
- `docs/decisions/mobile-data-fetch-anti-patterns.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`

Operational read-model authority: admin-web and Android are renderers of
backend-owned operational contracts. For Calendar, Control Tower, Action Center,
Protocol Adherence, Workflows, admin-web detail pages, Android execution/proof
screens, and future vertical surfaces, first identify the canonical write owner,
row/summary grain, stable scope identity, bucket disjointness/overlap, and
whole-result summary contract. Do not repair mismatched numbers by adding
frontend/mobile precedence rules while the backend contract is ambiguous or
stale. See `docs/architecture/operational-read-model-contract.md`.

Android row-action scope: repeated mobile cards with row-level actions must use
row/animal-scoped in-flight state. Do not wire a per-row Save/Update/Retry
through screen-wide `actionInFlight`/`busy` unless the operation truly locks the
whole screen. For individual weighing free-flow, animal A's pending save must
not disable or ignore animal B. Required guard: `make
android-row-action-scope-guard` (also in `make mobile-guard` and local CI).

Android Compose list identity: repeated rows must use the full operational
grain as the Compose key. `campaignShedId` alone is not unique once one
shed/campaign can appear as separate category, period, partition, or leadership
evidence rows. Vaccination proof-needed rows are one row per obligation, so
`goatId` alone is not unique when the same goat has multiple due vaccines or
proof obligations; key by `obligationId` first and use goat/vaccine/tag only as
a fallback composite. Use `WeighingAssignmentUiRow.uiKey` or another
field-complete UI identity, and add a failing fixture/test before changing
LazyColumn/LazyRow keys. If a ViewModel groups backend rows into rendered cards,
the grouping key must be the exact rendered row key (or contain exactly the same
discriminators). Never group by version/metadata fields such as
`sopVersionId`/`taskRowVersion` and then render a `ShedRow.id`/`uiKey` that omits
them: BT+SP / split-row schedules can then create two rendered rows with the
same LazyColumn key. This applies equally to vaccination sheds, weighing
assignments, feed tasks, and any future grouped mobile list. Regression guards:
`WeighingRouteIdentityTest`, `ScanProofIdentityTest`,
`ShedsExecutionIdentityTest`, and `make android-compose-lists-guard`.

## Current Admin-Web Build

The current admin-web slice is:

```text
Admin / Data Ops config + SOP policy
Preventive Care (PC) Vaccination operations
Calendar vaccination due-work command lens
Vaccination execution context scoped by park/shed
Control Tower process-gap summary
Animal Passport contextual drilldown
```

The current admin-web dashboard is a process-integrity product, not decorative
KPIs. Keep these command lenses, all vaccination-only:

```text
Control Tower      = process intact/not intact summary
Action Center      = exact work/gaps to act on now
Calendar           = vaccination due work by time, owner pill, park, shed, date
Protocol Adherence = expected vs actual, gap, severity, owner, next action, evidence
Workflow drilldown = config -> obligation -> SOP -> proof -> verification -> completion
```

Frontend must present the operational kernel honestly. It is a command surface
over backend truth, not a scheduler or source of truth. Screens should show what
process was expected, whether it was followed, where broken, who owns next
action, what is due by when, what evidence exists, and whether reminder,
deadline, or escalation state is active.

Any vaccination mutation that changes due-work, proof, verification, reminders,
snoozes, or drive scheduling must revalidate the shared command-lens set through
`revalidateVaccinationCommandLenses` only. Do not hand-maintain one-off
`revalidatePath` lists for Vaccination, Calendar, Action Center, Protocol
Adherence, Workflows, or Control Tower; `check:vaccination-command-lenses` is
the local guard for that anti-pattern.

Use `context/frontend/vaccination-process-integrity-frontend-handoff.md` for the
mock-shaped frontend split before reshaping `/`, `/action-center`,
`/protocol-adherence`, `/workflows`, `/vaccination`, or
`/sops`.

Build these routes/surfaces only unless the user explicitly reopens scope.
Current implemented admin-web product routes:

```text
/login
/
/vaccination
/vaccination/execution/sheds/{shed_id}
/action-center
/protocol-adherence
/workflows
/workflows/{row_id}
/procurement/source-entry
/procurement/source-entry/loads/{load_id}
/counts/herd
/operations/audit
/config
/sops
/animals/{animal_id}
/calendar
```

`/calendar` is reopened only for the Preventive Care (PC) Vaccination due-work slice. It must use
the generic `CalendarEvent` summary contract, vaccination-specific detail drawer,
generated backend clients, active owner pills `all`, `pc`, `inventory`, and
`admin_data_ops`, and the mock Calendar/drawer UI treatment. Keep primary nav,
mock-fidelity scan coverage, and `smoke:visual:live` coverage in sync with the
route. Do not show live all-domain Calendar content.

Scope lock: do not turn shared engines into visible product breadth. For the
current `/sops` route, the backend SOP engine can remain generic, but admin-web
must present the vaccination SOP slice only. Show vaccination SOPs such as
`vaccination.drive` / `vaccination.*`; hide or zero/disable other domains in
the visible surface; do not show shifting, procurement, HR, counts, breeding,
feed, inventory, or generic health SOPs as active review cards.

Parks is a vertical, but it does NOT own the Vaccination product route/module.
Vaccination execution
context (park, shed, animal stage, defer/blocker state, owner chain, drive status,
proof status, verification status) renders ONLY inside /vaccination,
not as a separate Parks route or sidebar entry. Do not rebuild old Locations or a
generic Parks vertical.

Do not confuse "not generic Action Center" with "no Action Center." Action
Center is the top-level `/action-center` command lens. `/vaccination` is the Preventive Care (PC)
Vaccination operations module only; it must not contain Action Center,
Protocol Adherence, Workflows, Config, or SOP Library as tabs, nested pages, or
large shortcut cards. The shared status model must power Preventive Care (PC), Parks, and Control
Tower: due, overdue, blocked, proof-pending, verification-pending, rejected,
deferred, blocked, and completed. These are read-model/UI statuses; do not
ask backend to mutate canonical `obligation_instances.status` just to match a
Calendar or dashboard label.

## UI Rules

- `mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of truth.
- Port the mock's layout, table shapes, empty states, icon system, spacing,
  density, and interaction model.
- User-facing copy firewall: Android and admin-web screens are for CEO,
  director, and operator workflows, not for exposing implementation mechanics.
  Do not render internal/debug/test/roadmap words in visible UI, including
  screenshots, empty states, loading/error states, snackbars/toasts, cards,
  chips, bottom sheets, drawers, action buttons, camera/proof panels, or alerts.
  Banned visible examples: `V1`, `V2`, `debug`, `mock`, `fixture`, `Paparazzi`,
  `Room`, `outbox`, `idempotency`, `groupKey`, `payload`, `backend`,
  `frontend`, `API`, `route`, `PRD`, `TRD`, `TODO`, `local`, and `localhost`,
  unless the screen is explicitly a developer/admin diagnostics tool. Use
  operator/business language instead: "Proof uploads in background", "Waiting
  for network", "Already scanned", "Needs proof", "Wrong shed", "Try again",
  "Cannot submit yet", "No assigned work", and similar product copy.
- Backend-driven UI contract is mandatory. Frontend must not invent product
  truth. Visible navigation, page titles, section/table labels, column labels,
  filter/sort/page-size semantics, chips/tabs, row-click params, drawer/action
  labels, disabled reasons, empty/error copy, and summary/detail field sets must
  come from backend app/OpenAPI contracts. Frontend owns only layout, CSS,
  responsive density, icon-token mapping, focus/hover behavior, and local
  open/closed or selected-row state. For admin-web, load
  `context/frontend/admin-web-backend-ui-contract.md` before changing shell,
  route bodies, tables, filters, chips, or drawers.
- Vaccination execution status and row action are backend-owned across admin-web
  and Android. Render `workState`, `sopStatus`, `proofStatus`,
  `verificationStatus`, counts, `carrySummary`, and `primaryActionKey`; do not
  derive client-only "in review/done/open scan" truth from local route hacks.
  A submitted or verification-pending shed row with `primaryActionKey=none`
  stays on the vaccination surface until a backend-owned review/detail action
  exists.
- Same-page overlays are transient client UI, not a Server Component
  navigation. Use the shared `LocalOverlayLink` plus a narrow local controller,
  preserve a history/hash deep link, and prove one-click open plus
  Back/Escape/outside/X close with zero route/RSC requests. If detail is not in
  the rendered list, open the drawer immediately from the row summary and fetch
  only that detail inside the drawer through an authenticated Server Action or
  Route Handler. Never use a query-only Next `Link`, native anchor/form, or
  router push to open/close an overlay. Never add a legacy allowance to the
  zero-baseline `make admin-web-local-overlay-guard`.
- Backend-driven does not permit backend-code live-data constants. Tenant,
  location/park, person, goat, shed, vendor/operator IDs, CBE/CPT-style codes,
  capacities, role scope, permissions, and governed dropdown vocabularies must
  come from Postgres/source-backed config and be compiled into the backend
  contract. Stable UI text that rarely changes (nav/page titles, table/filter
  labels, chips/tabs, empty/error copy, disabled reasons) also belongs in the
  backend contract; when runtime governance is needed, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  UI config entries must not relabel live/module-owned option groups such as
  parks, sheds, breeds, SOP labels, feed items, or role/grant scopes, and must
  not override semantic option metadata such as source-system publishability.
  Frontend must not render local defaults and then replace them with async
  config. Static backend code may hold only product contract shape, compile
  mapping, and intentional default skeletons for missing optional UI config rows.
- Do not reuse or recolor old admin-web UI, old `admin-primitives`, old chart
  components, old layout components, or old dashboard routes.
- Run `npm --prefix apps/admin-web run check:mock-fidelity` before frontend
  handoff.
- Run lint/typecheck/build, and run `smoke:visual:live` when local backend and
  admin-web can be started.
- If the user asks to fix a frontend/UI issue, the rendered local page is part
  of the fix, not an optional follow-up. Reproduce the user’s screenshot route,
  viewport, scope, filters, drawer/modal state, and click path before handoff;
  do not rely on source inspection, typecheck, or a server-rendered HTML grep as
  proof that the UI is fixed.
- For any route, table, chip row, drawer, modal, popover, or navigation change,
  open the rendered local page and inspect the screenshots before handoff. When
  a user supplies a screenshot, reproduce that exact route/viewport. The review
  must explicitly cover right-edge/status-column clipping, horizontal overflow,
  chip truncation/wrapping, active navigation state, row-click destination,
  outside-click/back close, close-button behavior, and whether drilldown pages
  show only the scoped real records for the clicked row. Do not claim a UI fix
  from code inspection alone.
- Before handoff for Android/admin-web UI work, grep changed production strings
  and screenshot fixtures for leaked internal words from the copy firewall. A
  screenshot that contains implementation terms is a failed UI review even when
  the build and tests pass.
- Android proof-video capture, compression, burned overlay, upload queue,
  original-upload fallback, and Firebase step telemetry must follow
  `docs/mobile/proof-video-processing-pipeline.md`. Reuse the shared pipeline for
  weighing, vaccination, feed, shifting, and future camera-proof modules; do not
  create per-screen compression/upload implementations. Camera proof creation is
  operator-only and must block until precise location is available. If Android
  can still show the runtime prompt, ask again from the blocked permission gate;
  if the operator selected "Don't ask again", open the app's Android Settings
  page for manual enablement. Operator UI may show product states like
  "Compressing proof..." and "Uploading proof...", but never codec, Room, outbox,
  Media3, GCS, idempotency, signed URL, or bitrate.

For Next.js, React, TypeScript, Node, TanStack Query, Playwright, accessibility,
visual review, and CI practice, follow
`docs/frontend/admin-web-engineering-quality.md`. In particular: keep App Router
pages server-first with narrow client boundaries; keep privileged adapters and
tokens `server-only`; treat Server Actions/Route Handlers as public request
surfaces that re-check auth, validate input, and preserve stable idempotency;
derive React display state without Effects; include every result-changing
scope/window/filter in query keys; and prove UI changes with semantic browser
tests, axe, desktop/narrow screenshots, and human diff review.

## Data Access Rules

- Admin-web and mobile use generated OpenAPI clients and small app adapters.
- Browser/mobile code must not read BigQuery, Sheets, GCS, Firestore, or
  operational databases directly.
- Backend RBAC remains authority. Frontend visibility is convenience, not
  security.
- Frontend timers, localStorage, mock rows, or optimistic UI state must never be
  the canonical reminder, deadline, escalation, proof, or obligation state.
- Tokens stay server-side for admin-web. Do not put bearer tokens in
  `NEXT_PUBLIC_*`, localStorage, rendered HTML, query params, or static assets.
- Android Remote Config force-update is fail-open unless there is a valid
  `http(s)` update URL. Cold launch must start in a checking state and block
  business UI until the first update decision resolves; never render app content
  from an initial optimistic "allowed" state.

## Local Android Toolchain And Device Gate

- Run `make android-doctor` before treating JDK, SDK, or device availability as
  a blocker. Repository scripts resolve JDK 21 and the Android SDK without
  relying on a parent agent shell's environment.
- Run `make android-dev-run` for device proof. It prefers an authorized physical
  phone and automatically starts/waits for a configured AVD when USB is absent.
- A missing USB phone is never by itself a mobile verification blocker. If no AVD
  exists either, record that exact gap and create one using
  `docs/runbooks/android-dev-device.md` before deferring device validation.
- JDK 21 is the Gradle/AGP runtime; source/bytecode compatibility remains 17.

## Removed From Active Frontend Scope

Do not revive these old admin/dashboard features unless product scope is
explicitly reopened and the screen is rebuilt from the mock:

```text
counts dashboard
mortality dashboard
herd search as a global primary surface
Import Review product UI
Data Quality queues
Legacy Sync UI
old Locations page
old Operators page
old SOP builder page
old Tasks page
old admin-primitives/charts/layout components
old app/api BigQuery or Sheets routes
```

Historical docs and git history may contain those names; treat them as
archaeology, not active build instructions.

## Android Motion & Transitions

Canonical contract: `docs/mobile/transitions-and-motion.md` (M3 pattern → Goat OS
surface map, motion tokens, and the current-code audit). Read it before adding or
changing any Compose navigation transition, `AnimatedContent`, or bottom sheet.

Navigation hierarchy is separately locked by
`docs/decisions/android-navigation-stack.md`:

- backend-composed root destinations are L0 and alone own bottom-bar/drawer
  chrome;
- drills are distinct L1/L2/L3/L4 hosted `NavHost` destinations with Up/Back;
- route membership is exact, never prefix-based, and a drill must never reuse an
  L0 route;
- structural details are full-screen destinations; bottom sheets are temporary
  filters/pickers/actions only;
- `make android-navigation-stack-guard` plus the Android JVM unit suite enforce
  the route/chrome regression.

Vaccination execution-specific route guard: the operator flow is
`Vaccination sheds -> Scan -> Submit -> Vaccination sheds`. Once a shed video
submission is synced and the drive/shed is `submitted`, `needs_review`, or
verification-pending, do not route the card to the generic `/record` surface as
a "read-only" fallback. That screen is not the vaccination review/detail surface
and can show contradictory generic counts. If no dedicated vaccination detail
exists, keep the operator on Vaccination sheds and show the submitted/in-review
status there. The submit ACK state must show explicit copy such as `Submitted`,
and the Vaccines-to-carry card path must stay wired to backend `carrySummary`.

Hard rules (screen motion is spatial, never decorative):

- **Drill = shared axis X.** Calendar → day → sheds → Scan → Submit, Overdue →
  Reschedule, and any deeper navigation slide in from the end; Back is the mirror.
  Always define `popEnterTransition`/`popExitTransition` as the reverse of
  `enter`/`exit` (`AppNavHost` already sets this globally).
- **Top-level tab switches = fade through, NOT slide.** Bottom-nav peers
  (Calendar ↔ Overview ↔ Alerts ↔ You) are unrelated destinations with no
  forward/back order — fade them. Do not let them inherit the drill slide.
  (Known gap today: they currently slide; fix pattern is in the canonical doc.)
- **Temporary/contextual surfaces = `ModalBottomSheet`.** Language, sync, scope
  picker, data-gaps, doses-given, scan sub-sheets. Never hand-roll sheet offsets.
- **Do not fade a drill step** (drill is the app's spatial spine) and **do not
  force shared axis Y/Z** where the relationship does not call for it.
- Prefer M3 **Emphasized** easing over the Compose `tween` default; opt into
  **predictive back** (`android:enableOnBackInvokedCallback="true"`).
