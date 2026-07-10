# Vaccination Trigger Closure Parallel Handoff

Date: 2026-06-25

> **Superseded for Path B clean-slate herd identity:** this handoff records the
> earlier goat-table implementation state. For current V1 architecture, use
> `docs/preventive-care-vaccination/PRD.md`,
> `docs/preventive-care-vaccination/TRD.md`, and
> `context/product/glossary.md`. Current target terms are `herd_animals`,
> `animal_id`, `animal_identifier_1`, `animal_identifier_2`,
> `animal_identifiers`, `animal_location_history`, and
> `animal_identity_events`. Every accepted goat/sheep/future species has two
> different current Animal IDs; each value is globally single-use for life and
> never reused after death, sale, transfer, tag loss, or tag breakage. Clean
> V1 local/dev/test data is wiped and reseeded through the new importer instead
> of preserving stale goat-only or old-dashboard rows.
> Procurement trust correction: old `HF` / Holding Farm wording in this handoff
> now means our supervised procurement holding park only. Vaccination evidence
> suppresses PC work only when our team administered or validated it in our park
> or procurement holding park under SOP/video/physical validation. Third-party
> supplier/vendor/source claims outside that lifecycle are untrusted notes and
> start PC scheduling after accepted shed entry plus warm-up/health gates.

Purpose: close the remaining build plan before vaccination E2E by documenting the
minimum trigger points, sidebar/IA changes, audit-log needs, backend work, and
frontend work. E2E starts only after these pieces are built and tied together.

Golden rule: build the smallest real vaccination-trigger surface, but make that
surface match the mock's UX discipline thoroughly. Do not build every mock flow;
do not ship a loose or fake version of the few flows we do build.

This doc intentionally reopens only two additional non-vaccination surfaces
because they are required to test the vaccination cascade from a real business
entry point:

- Counts -> Herd Register: create/import a goat, plus the basic setup that makes
  create/import real and safe.
- Admin / Data Ops -> Audit Log: see every operator/admin/system business action
  that produced the vaccination state, plus the filters and entity links needed
  to inspect it. The implementation route can remain `/operations/audit`; the
  visible IA is not an Operations vertical.

It also depends on the already-reopened Procurement -> Source Entry surface only
for the supplier Holding Farm warmup -> accepted-intake trigger branch. That
does not approve broad procurement CRUD, economics, landing-cost, vendor
management, generic source-health SOP catalogs, or new command-room pages under
procurement.

Dependency closure rule:

- A reopened surface includes the supporting backend contracts, reference data,
  UI controls, state transitions, and audit/read paths required for that surface
  to work end to end. It is not just permission to place one button on a page.
- Counts -> Herd Register may therefore build the required real herd-register
  setup needed for vaccination triggers: park/shed/location selectors, breed/
  sex/stage/reference lookups, identifier normalization, duplicate/conflict/
  needs-review states, active/inactive lifecycle display, bulk import preview
  and row errors, limited backend-backed count cards, row-to-Passport links, and
  audit/history links.
- Admin / Data Ops -> Audit Log may build the business audit presentation from
  the mock: operation-family chips, operator/span controls, status/proof/anomaly
  filters, activity trail, cursor pagination, entity-history links, and disabled
  export scaffolding when no export backend exists.
- Procurement -> Source Entry may build supplier/holding-farm lookup,
  purpose/classification, HF evidence review state, pre-dispatch/arrival/
  accepted-intake actions, and the exact dependency setup needed for supplier
  warmup to feed vaccination.
- Shared primitives such as generated clients, lookup endpoints, permissions,
  audit, idempotency, validation, and scope parsing may be built or extended
  when they serve these active surfaces.

Counts is reopened for the vaccination-required slice: Herd Register plus the
supporting setup it needs to create/import/list goats and feed the vaccination
trigger. Do not use this as approval to build unrelated Counts modules, HR,
Parks, Insights, Calendar, Inventory, all-domain Operations, or the old
dashboard product. Do not reuse deleted old dashboard/admin code or old admin
primitives. Rebuild the needed pieces against the current GoatOS schema,
generated clients, and `mock/goatos-dashboard-mock.html`.

## Source Anchors Checked

Business/wiki and source evidence:

- `Preventive Care Director handbook source` page 2: Preventive Care (PC) weekly operations include vaccination
  tracking, stock control, and record management.
- `Preventive Care Director handbook source` page 5: vaccination requires storage/cold-chain
  integrity and health data documentation.
- `Preventive Care Director handbook source` page 6: Health Data Recorder enters vaccination,
  deworming, and health data into software.
- `wiki/graphify-out/converted/RFID source of truth_9d32525c.md`: existing herd
  identity source uses Farm, Old ID, RFID, Age, Gender, Breed, Tag, Shed, and
  Partition.
- `wiki/graphify-out/converted/Birth Reports_10089bdd.md`: birth reporting
  creates mother/kid records, with farm, goat/mother ID, source/destination
  shed, breed, birth time, kid gender, proof, and verification.
- `wiki/graphify-out/converted/Shifting Reports_27be55d1.md`: shifting updates
  shed truth and is required to maintain accurate shed counts.
- `source-material/daily-updates.md`: prior local admin work already framed goat
  identity as real RFID import -> passport records, with remaining actions
  around conflict create-goat and import review approve/fix.
- `context/product/glossary.md`: `HF` means Holding Farm; it is source/holding
  context after procurement and before dispatch, not final ownership truth.
- `context/execution/procurement-source-entry-backend-handoff.md` plus the
  2026-06-25 operator clarification: the goat journey can start at
  purchase/source, governed procurement holding is 4-5 weeks near the buying
  region, source purpose remains context, and goats may be rejected before truck
  loading.
- `context/execution/procurement-vaccination-e2e-plan.md`: Preventive Care (PC) Vaccination
  consumes accepted-intake truth only: goat identity, park/shed, intake date,
  defer signal, and trusted historical vaccination evidence.
- `docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md`: Procurement DB evidence is
  arrival/intake/history evidence for vaccination, including historical
  vaccination-at-procurement evidence and intake health/defer signals.

Mock and legacy evidence:

- Latest mock file checked for this handoff: `mock/goatos-dashboard-mock.html`,
  local mtime `2026-06-25 12:21:51`.
- If that file changes after this timestamp, refresh this doc or the UI ledger
  before coding. Do not build from memory of an older mock.
- `mock/goatos-dashboard-mock.html` sidebar puts `Herd register`,
  `Tagging & identity`, and `Weights & ADG` under `Counts`. For this
  vaccination-trigger slice, only `Herd register` is implemented or shown.
  Do not copy the other Counts leaves into the app as active or disabled nav.
- The mock Herd Register screen has `Import sheet`, `Register goat`, filters,
  herd table rows, KPI cards, and row click to Goat Passport.
- The mock bulk goat drawer is `Bulk register goats` and uses columns:
  `Farm`, `RFID`, `Old tag`, `Park`, `Shed`, `Breed`, `Sex`, `DOB`,
  `Weight(kg)`, `Dam ID`, `Sire/lot`, `Origin`, `Photo URL`.
- The mock audit history side panel says every create, edit, status change,
  verification, filter, and deletion writes an immutable entry; `Open full audit
  log` should lead to the full operations audit.
- The mock Supplier warmup / Holding Farm panel shows purchase/source entry
  before park arrival: load rows, holding farm supplier, warmup week, tagging,
  vaccination-at-HF status, health/selection status, and reject/ship state.
- The latest mock also adds operation-axis audit UI: `Admin · Data-Ops / Audit
  log`, summary cards, anomaly card toggle, operator/span list, activity table,
  top and bottom pagers, entity history rows, and export affordance. The
  implemented `/operations/audit` must account for those elements through real
  backend data, route links, server pagination/filter/sort, or an honest
  disabled/export blocker.
- The latest mock uses reusable table/filter/pagination affordances across
  data tables: `Filters`, active chips, `Clear all`, sort-like table headers,
  row click, page controls, and disabled first/last states. For every table in
  the approved slice, backend and frontend must implement or explicitly block
  those controls together.
- The current operator clarification confirms the same business rule: goats can
  be tagged and held at the procurement holding park for 4-5 weeks near the
  buying region, goats may be rejected before loading onto the truck, and their
  journey starts at place of purchase when purchased.
- The 2026-06-25 operator clarification also says the legacy Counting DB is not
  used for vaccination. GoatOS should maintain canonical goat-to-shed
  association truth instead: which `goat_id` is in which shed/location.
- Legacy dashboards have Counts and BigQuery-backed census/read models, but they
  are reference only. Do not copy direct BigQuery access into Goat OS admin-web.

Goat OS source of truth:

- `docs/preventive-care-vaccination/PRD.md`: "Add a goat -> its obligations generate" and
  birth/procurement goat entry auto-generates vaccination obligations.
- `docs/protocol-engine/state-machines.md`: SM-1 schedule generation triggers on
  `goat.created` or `goat.stage_changed`; SM-2 recomputes on `goat.shifted`;
  SM-3 cancels on `goat.exited`.
- `docs/protocol-engine/obligation-engine.md`: reuse `goats`,
  `goat_identifiers`, `goat_location_history`, `goat_identity_events`,
  `outbox_messages`, `idempotency_keys`, and `audit_log`.
- `context/execution/vaccination-pre-e2e-readiness-audit.md`: E2E is blocked
  until accepted intake / goat creation triggers real generation, sweeper, proof,
  verification, and Postgres-backed read models.
- `backend/internal/procurement/adapters/postgres/repository_integration_test.go`:
  procurement source-entry tests must be updated to the 4-5 week governed
  holding window, pre-dispatch rejection blocking active vaccination work, and
  accepted clean goats creating Preventive Care (PC) handoff/read-model state.

## Product Decision

Goat creation belongs to:

```text
Vertical: Counts
Module: Herd Register
Operational route: /counts/herd
```

It does not belong under Preventive Care (PC) -> Vaccination and it is not Goat Passport search.
Goat Passport remains a contextual detail route from rows or a known direct URL.

Vaccination consumes the output:

```text
Counts -> Herd Register create/import goat
  -> goat.created event
  -> Preventive Care (PC) -> Vaccination obligation generation
```

Procurement accepted intake remains a second valid trigger:

```text
Procurement -> Source Entry accepted intake
  -> accepted-intake or goat.created event
  -> Preventive Care (PC) -> Vaccination obligation generation
```

Both paths must converge on the same generation handler and the same Postgres
truth. They must not generate duplicate obligations.

Supplier Holding Farm warmup belongs to:

```text
Vertical: Procurement
Module: Source Entry
Operational route: /procurement/source-entry
```

It does not belong under Counts -> Herd Register because these goats are not
clean accepted herd yet. It does not belong under Preventive Care (PC) -> Vaccination because Preventive Care (PC)
only consumes accepted-intake truth and trusted source vaccination evidence.

The minimum procurement trigger branch is:

```text
purchase/source or supplier Holding Farm entry
  -> source warmup, tagging, health/selection, and HF vaccination evidence
  -> pre-dispatch accept / reject / defer / block
  -> truck loading + arrival gate
  -> accepted intake for clean goats only
  -> trusted imported completion/history evidence reconciles vaccination due basis
  -> accepted-intake/goat.created event reaches the same vaccination generator
```

Rejected-before-truck, source-only, ownership-pending, identity-conflict,
deferred, missing, extra-unknown, arrival-rejected, dead, sold, or lost goats
stay procurement history/work. They must not enter active herd count, active Preventive Care (PC)
vaccination work, vaccination execution rows, or Goat Passport active-herd
views.

## Golden Rule: Mock-Faithful, Scope-Minimal

For this closure, UI/UX quality is not optional and scope control is not
optional. The frontend must follow the mock's visual grammar for every element it
implements, while implementing only the elements required for vaccination flow
closure.

Use the mock as the source of truth for:

- Left sidebar grouping, indentation, badges, active states, approved visible
  leaves, icons, collapsed/mobile drawer behavior, and nav click behavior.
  Future mock leaves outside this slice are `removed`, not disabled sidebar
  placeholders.
- Top-bar density, company/park scope controls, date/as-of state, notification
  affordances, and profile menu placement.
- Body alignment: page content left edge, max width, section spacing, tab bands,
  table density, cards, status chips, buttons, search/filter rows, and empty
  states.
- Modal/drawer behavior: right-side drawers where the mock uses drawers, modal
  sizing where the mock uses modals, close button position, footer button
  placement, overlay/backdrop, focus handling, Escape/backdrop close, loading
  state, validation state, success state, and error state.
- Color and typography: reuse the Mesha dark/green theme, existing admin-web CSS
  variables, icon sizing, border radius, muted text, alert colors, and chip
  colors. Do not introduce a second palette or a generic SaaS skin.

Scope rule:

- Build only `/counts/herd`, the Admin / Data Ops business Audit Log at
  `/operations/audit`, the minimal supplier
  warmup/accepted-intake path inside `/procurement/source-entry`, and the
  already-approved vaccination/config/SOP surfaces needed to prove the
  vaccination cascade.
- Build the dependency closure inside those surfaces when it is required for
  them to be honest, usable, and backend-backed. For example, Herd Register needs
  real location/identifier/reference-data setup before `Register goat` can be
  more than a fake trigger.
- Do not build the rest of Counts, Calendar, Insights, HR, Inventory, generic
  Parks, generic Operations, or dashboard parity just because the mock shows the
  long-term map.
- A mock element inside an approved surface that is visible but not backed by
  real API/business behavior must be hidden or visibly disabled with an honest
  reason. A mock screen/nav leaf outside the approved slice must be removed/
  hidden, not shown as a disabled sidebar placeholder.
- No frontend-only arrays, fake totals, fake vaccination rows, fake audit rows,
  or local Next route handlers may stand in for backend state.

Every clickable element must be classified before handoff:

```text
real-action      calls a generated client / submits a real form / mutates state
real-navigation  routes to an approved route that renders real state
real-filter      changes backend-backed query params or server-side data
disabled         visibly disabled with title/aria reason and no fake action
removed          not rendered because it belongs to future scope
```

No other state is allowed. A button that looks clickable but does nothing is a
bug. A button that mutates only client memory is a bug unless it is documented as
local form editing before submit.

UI tracking required from the frontend agent:

| Area | Required tracking |
| --- | --- |
| Sidebar | every visible group/leaf, open/closed group toggle, active state, badge, disabled/hidden reason, route |
| Top bar | hamburger, scope mode, park selector, date/as-of selector, theme toggle, notification icon, profile/role menu, outside-click/Escape close |
| Table controls | search, filter drawer, clear all, active filter chips, page size, next/prev cursor, disabled first/last states, sortable headers, row click |
| `/counts/herd` | every card, tab/button/search/filter/sort/pagination/table row/drawer field |
| Register goat drawer | each field, validation source, submit, cancel, close, Escape/backdrop close, loading/success/error |
| Bulk goat drawer | template, upload/paste, preview rows, row errors, sort/filter/page preview, commit footer, disabled `Create 0 records` |
| `/procurement/source-entry` | supplier warmup load rows, filters, sort/pagination, row click, disabled future actions |
| Procurement load detail | HF evidence, source health, pre-dispatch decision, dispatch, arrival, accepted-intake actions, done/blocked state |
| `/operations/audit` | Admin / Data Ops business Audit Log: summary, operation-family chips, status/proof/anomaly filters, operator/span control, clear, sort/pagination, table, entity/history links, export |
| Entity history panels | open/close/full-audit route, data source, empty/error |
| Responsive | desktop, narrow, mobile drawer screenshots inspected manually |

The frontend completion note must include a small UI fidelity ledger with:

```text
route | element | classification | backend data/client | disabled reason | screenshot
```

Mock-fidelity checks must be updated or extended when new sidebar leaves/routes
are added so future agents cannot accidentally activate old routes, fake scope,
or fake actions. `apps/admin-web/scripts/check-ia-guard.mjs` now enforces that
the Counts sidebar group exposes exactly one current leaf, `Herd Register`, for
this slice.

Frontend implementation checkpoints:

```text
before coding    map every required mock element to real-action / real-navigation / real-filter / disabled / removed
during coding    keep sidebar, top bar, body, drawer/modal, and history panel ledger updated
before handoff   run mock-fidelity checks, capture screenshots, inspect them manually, and attach ledger paths
```

Do not wait until the end to discover that a nav item, button, footer action,
filter, close button, or row action is fake. Track it while building.

## Full Interaction Closure Contract

Every element that a user can see and reasonably click, type into, select,
toggle, clear, sort, page through, close, or submit in the built slice must be
closed before E2E. "Closed" means one of the five classifications above plus a
backend/frontend implementation or an explicit disabled/removed reason.

This applies to:

- sidebar groups, leaves, badges, disabled leaves, active route state, desktop
  rail, mobile drawer, and mobile scrim
- top-bar scope mode, park selector, as-of/date selector, theme toggle,
  notifications, profile/role menu, and every menu item
- page header buttons, tabs, subtabs, quick filters, chips, segmented controls,
  toggles, KPI/card clicks, row clicks, entity links, and history links
- table search, server filters, `Clear all`, active filter chips, sortable
  headers, cursor pagination, page size, empty states, error states, and loading
  states
- drawers and modals: every input, select, chip, checkbox, toggle, upload/paste
  field, close icon, cancel button, footer submit, disabled state, validation
  error, success state, and retry state
- action controls named or implied by the mock, including `done`, `verify`,
  `reject`, `request rework`, `accept`, `defer`, `block`, `clear`, `publish`,
  `save draft`, `dry run`, `submit`, `upload proof`, `commit`, and `export`

Backend obligations for those controls:

- List APIs must expose bounded cursor pagination, stable default ordering,
  whitelisted sort keys/directions, server search, server filters, page-size
  caps, next/prev cursor behavior, and clear-filter semantics through omitted or
  empty query params. Invalid cursors, invalid sort keys, unauthorized filters,
  and empty pages after mutation must return deterministic errors or empty
  states, not 500s.
- Action APIs must be idempotent, tenant/RBAC scoped, audited, and safe on
  double-click/retry. Same idempotency key + same payload returns the original
  result; same key + different payload is rejected or returns the original
  result without side effects according to the endpoint contract.
- Toggle/done/status actions must be explicit state transitions, not generic
  PATCH blobs. They must define allowed from/to states, already-done behavior,
  stale version/conflict behavior, permission failure, and audit event.
- Export, notification, broad search, or future-module controls must stay
  disabled until a real backend contract exists.
- Top-bar park/scope data must come from the authenticated actor's allowed
  location/scope set. A scope change must be a query/context change that all
  server reads respect; page-body filters must not override it silently.

Frontend obligations for those controls:

- Use generated clients/server actions only for backend state. No local route
  handlers, fixture arrays, mock totals, or client-only mutation as a substitute
  for real backend behavior.
- Preserve URL/query state for scope, filters, sort, and pagination where a user
  expects refresh/back/share to work. Clearing page filters must reset page
  cursor and active chips while preserving top-bar scope.
- Disable submit/action buttons while requests are in flight. Double-clicks,
  reloads, browser back, Escape/backdrop close, and retry after error must not
  create duplicate backend work.
- Sort toggles must cycle through the documented supported states for that
  table, update the backend query, reset cursor, and show the same result order
  after refresh. If a column is not sortable, it must not look sortable.
- Pagination controls must handle no rows, first page, last page, one-page
  results, changed filters after page N, page-size change, and backend cursor
  expiry.
- Mobile/narrow views must leave footer actions reachable, preserve focus
  traps, avoid clipped text, and support menu/scrim close without hiding active
  controls.
- Role preview may remain local preview state only, but must be labeled and
  must not bypass backend RBAC. Real permissions still come from the backend.
- Superadmin/CEO/COO role preview is an approved top-bar capability. It previews
  how navigation, permissions, and Audit Log span look for other roles; staff
  roles do not get an unrestricted switcher. Audit Log `Viewing as` must use the
  same role-lens source as the top bar, not a separate hard-coded list.

Completion artifact required from both agents:

```text
route | element | user action | classification | backend contract/client |
state model | idempotency/audit | edge cases tested | screenshot/test proof
```

For backend work, this artifact is the backend interaction ledger: one row for
each backend-owned route, action, control contract, state transition, list
query, generated-client change, or disabled/future reason required by the built
slice. Backend rows must point to the contract/client shape, state model,
idempotency and audit behavior, deterministic error behavior, replay behavior,
and test or query-plan proof.

For frontend work, this artifact is the UI fidelity ledger. Frontend rows must
include the route, visible element, user action, classification, backend
client/data source or disabled reason, edge cases tested, and screenshot path.

### Backend Interaction Ledger Rows (2026-06-25 backend pass)

These rows close backend contract surface only. They do not claim visual E2E or
business E2E.

| route | element | user action | classification | backend contract/client | state model | idempotency/audit | edge cases tested | screenshot/test proof |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/procurement/source-entry` | Procurement holding-park board source rows | View source holding work | real-read | `listProcurementSourceEntryLoads`; `ProcurementLoadGoat.purpose`; `ProcurementHoldingStay.purpose`; generated `packages/api-client/src/generated/admin-api.ts` | V1 governed holding is 4-5 weeks near the buying region; source purpose remains context | read-only; no mutation audit | outside-window rows become at-risk/overdue work, not trusted evidence | `go test ./internal/procurement/...`; `make validate-sqlc-plans` |
| `/procurement/source-entry/loads/{load_id}` | Load detail HF evidence panel data | Open load detail / inspect HF dose evidence | real-read | `getProcurementSourceEntryLoad`; required `hf_vaccination_evidence: ProcurementHFVaccinationEvidence[]`; generated admin client | evidence states: `imported`, `trusted`, `rejected`, `conflicting`, `duplicate`; empty loads serialize an empty evidence array | read-only; no mutation audit | load detail returns imported/reviewed HF evidence with source goat and warmup rows | `go test ./internal/procurement/...` |
| `/procurement/source-entry/loads/{load_id}/goats` | Source goat purpose/classification | Add or update a source goat with explicit purpose | real-action | `addProcurementSourceEntryLoadGoat`; `AddProcurementLoadGoatRequest.purpose`; generated admin client | purpose stored on `procurement_load_goats` and `source_holding_stays`; idempotency fingerprint includes `purpose`, `selection_reason`, and `warmup_days` | required `Idempotency-Key`; source-goat audit row through `platform/audit`; replay returns original row; same-key/different-payload conflicts | retry does not duplicate goats or audit; different payload conflicts; source RFID conflict still blocks | `go test ./internal/procurement/...`; focused idempotency integration test |
| `/procurement/source-entry/goats/{goat_id}/source-health` | Source health result | Record passed/failed/deferred source health | real-action | `recordProcurementSourceHealth`; generated admin client | health states update source goat and load status; replay flag suppresses replay-unsafe cancellation hooks | required `Idempotency-Key`; source-health audit row through `platform/audit`; replay returns original check without extra audit | server-defaulted timestamp replay succeeds; explicit changed timestamp conflicts; retry does not duplicate audit | `go test ./internal/procurement/...` |
| `/procurement/source-entry/goats/{goat_id}/hf-vaccination-evidence` | HF vaccination evidence import | Import supplier/HF dose evidence for review | real-action | `recordProcurementHFVaccinationEvidence`; `RecordProcurementHFVaccinationEvidenceRequest`; `ProcurementHFVaccinationEvidenceResponse`; generated admin client | imported evidence is procurement-owned and starts `imported`; requires load/goat/protocol/rule/dose/proof contract | required `Idempotency-Key`; audit action `procurement.hf_vaccination_evidence.imported`; replay returns original evidence; same-key/different-payload conflicts | goat must belong to load; replay/conflict covered; audit row covered | `go test ./internal/procurement/...` |
| `/procurement/source-entry/hf-vaccination-evidence/{evidence_id}/review` | HF evidence review gate | Trust/reject/conflict/mark duplicate evidence | real-action | `reviewProcurementHFVaccinationEvidence`; `ReviewProcurementHFVaccinationEvidenceRequest.expected_row_version`; generated admin client | only `trusted` evidence can satisfy due-basis reconciliation; `trusted` is terminal for this endpoint and correction needs a separate reconciliation workflow | required `Idempotency-Key`; required `expected_row_version`; audit action `procurement.hf_vaccination_evidence.reviewed`; replay returns original review; stale row returns 409; missing row returns 404 | trusted review, replay, idempotency conflict, stale row version, trusted-to-rejected block, missing row, and duplicate audit prevention covered | `go test ./internal/procurement/...`; `go test ./...` |
| goat-created vaccination generation | Trusted procurement holding-park evidence reconciliation | Generate post-arrival obligations from published matrix protocols | real-backend-trigger | `GenerationService` reads trusted completion evidence from Postgres as of the generation time; result exposes `suppressed_by_trusted_history` internally | trusted evidence suppresses only matching `protocol_version_id` + `rule_id` + `dose_code` with `administered_at <= due_at`, `administered_at <= generation_as_of`, `reviewed_at <= generation_as_of`, source context in our park/procurement holding park, and accepted SOP/video/physical validation; imported-only, outside-source, and future evidence do not suppress | event replay still relies on existing generation idempotency key; read-only evidence check has no audit | past trusted procurement holding-park evidence generates zero matching obligations; imported-only and future evidence each generate one obligation | `go test ./internal/vaccination/...`; `go test ./...` |
| `contracts/openapi/admin-api.yaml` / generated client | Contract publication | Frontend wires against generated HF evidence and purpose types | real-contract | OpenAPI paths and schemas for purpose, HF evidence import, HF evidence review with `expected_row_version`, load-detail evidence array; `packages/api-client/src/generated/admin-api.ts` regenerated | client-visible DTOs match backend JSON fields | n/a | `npm --prefix packages/api-client run generate` succeeds; contract validator succeeds; drift checks stop only on expected generated-client diff against `HEAD` | `npm --prefix tools/contract-validation run validate`; `bash tools/agent-hooks/check-contract-drift.sh`; `make api-client-check` |
| `/protocols?category=…` (B3, 2026-06-26) | Config authority protocol-rules table | List every protocol version (draft/published/retired) for a category | real-read | `listProtocolConfigs` (app-api); `ProtocolConfigListResponse`/`ProtocolConfigItem`; generated `packages/api-client/src/generated/app-api.ts`; wired in `lib/api/server.listProtocolConfigs` + `features/config/protocol-rules-page.tsx` | read-only; returns rule-row count, status (draft/published/retired), scope, effective window, linked SOP, source-review state (`rule_dsl.source`), publisher/updated metadata; `protocol.read` permission; bounded `configListLimit` (no cursor — versions/category are small) | read-only; no mutation audit; publish stays gated by `protocol/app/publish.go ValidatePublishable` (the list only surfaces the source-review state, never bypasses the gate) | default category=vaccination; explicit category passthrough; empty list → honest empty state; failed read → error band not silent empty; status reflects source-backed vs not | `go test ./internal/protocol/... ./internal/permissions/...` (`TestListConfigsReturnsItemsAndDefaultsCategory`); `npm --prefix packages/api-client run generate`; live SSR `/config` render (2 real rows, screenshots under `.codex-goatos-render/admin-web-screenshots/config-b3/`) |

The tie-up pass must diff this ledger against the latest
`mock/goatos-dashboard-mock.html` and the rendered admin-web, then either close,
disable, or remove every mismatch inside the approved slice.

## Minimum Business Flow To Build

```text
published source-backed vaccination protocol
  -> published vaccination SOP
  -> admin opens Counts -> Herd Register
  -> admin registers one clean goat or commits a validated bulk import row
  -> backend writes goat, identifiers, current park/shed, location history,
     identity event, audit entry, idempotency row, and goat.created outbox event
  -> goat.created handler generates vaccination obligations from published rules
  -> sweeper creates shed drive / batch / SOP task
  -> operator/admin test path records proof through backend proof/SOP APIs
  -> verifier accepts/rejects/reworks
  -> accepted verification writes vaccination_completions and updates obligation
  -> CT/AC/PA/WF/Vaccination/Shed/Goat Passport read the same Postgres state
  -> Admin / Data Ops Audit Log shows the create, generation, task, proof, verification,
     stock, completion, and any correction/rework events
```

The second required business trigger branch is source-entry:

```text
published source-backed vaccination protocol
  -> published vaccination SOP
  -> admin opens Procurement -> Source Entry
  -> admin creates or opens a supplier Holding Farm load
  -> admin records source goat rows with source identity/tagging, holding farm,
     warmup start/end/days, health/selection state, ownership state, and HF
     vaccination evidence or proof reference
  -> backend keeps source-only goats out of active herd count and active Preventive Care (PC) work
  -> pre-dispatch reject path writes procurement history/audit only and never
     creates active vaccination work
  -> pre-dispatch accepted goats move through truck proof, arrival review, and
     accepted intake
  -> accepted intake emits the same generation path as clean Herd Register create
  -> trusted procurement holding-park vaccination evidence is used as imported completion/history basis
     so post-arrival obligations do not double-dose
  -> park-side quarantine and on-arrival rules remain separate post-arrival work
  -> Admin / Data Ops Audit Log shows source entry, HF evidence import/review, reject or
     accepted intake, handoff, generation, and any skipped/deferred result
```

## Sidebar And IA Rules

Use the mock sidebar as the long-term product map, but build only the leaves
needed for this closure.

Active now:

- Top-level command lenses: `/`, `/action-center`, `/protocol-adherence`,
  `/workflows`.
- Preventive Care (PC) -> Vaccination: `/vaccination`.
- Procurement -> Source Entry: `/procurement/source-entry`.
- Admin / Data Ops -> Config: `/config`.
- Admin / Data Ops -> SOP Library: `/sops`.
- Counts -> Herd Register: `/counts/herd`.
- Admin / Data Ops -> Audit Log: `/operations/audit` (route name is an
  implementation detail; do not create a visible Operations vertical).
- Goat Passport: contextual row/detail links only, `/goats/{goat_id}`.

Do not add:

- `/vaccination/config`, `/vaccination/adherence`, `/vaccination/workflows`, or
  other nested command/authority routes.
- A global Goat Passport search bar.
- Counts dashboard/modules unrelated to the Herd Register dependency closure
  needed for goat create/import/list/passport/audit.
- Standalone Tagging & Identity, Weights & ADG, Count Reconciliation, Calendar,
  Insights, generic Parks, generic HR, or generic Inventory surfaces unless a
  specific piece is required to make Herd Register registration/import work and
  is implemented as part of `/counts/herd`, not as a new broad product surface.

Allowed placeholders:

- Counts sidebar placeholders are not allowed for this slice. The Counts group
  shows only `Herd Register`; do not show `Counts overall`, `Count
  reconciliation`, `Tagging & identity`, or `Weights & ADG` as active or
  disabled leaves.
- If the vaccination trigger path needs identifier capture, temp IDs, duplicate
  tag/RFID review, or tag status, build that inside `/counts/herd`. Do not open
  a standalone Tagging & Identity route for this slice.
- Procurement -> Source Entry may show only the Source Entry board and Load
  Detail needed for supplier warmup / accepted intake. Do not add procurement
  Action Center, Protocol Adherence, Workflows, Control Tower, vendor master,
  landing-cost, or broad procurement CRUD under nested procurement routes.

Audit location and meaning:

- Audit has two meanings and agents must not mix them up.
- Backend/platform audit is internal infrastructure: append-only `audit_log`
  rows used for debugging, replay, idempotency proof, investigations, and
  traceability. It may contain raw action names, UUIDs, trace IDs,
  domain/module/category metadata, and other bounded technical metadata. This
  does not require a raw developer UI in the CEO/admin dashboard.
- CEO/admin Audit Log is the business-facing dashboard surface from the mock. It
  lives under Admin / Data Ops and answers "who did what, where, with what proof,
  and what result." It may read from `audit_log`, but the page must map rows into
  business language and controls. Do not expose raw UUID/domain/module/category
  debug fields as the main UX.
- Operation is an axis inside the Audit Log, not an IA vertical. The mock's
  operation-family chips are filters over business events, not permission to add
  a separate Operations sidebar group.
- Role preview is part of this surface: top-bar Superadmin/CEO/COO preview and
  Audit Log `Viewing as` must stay in sync. Canonical lenses for this slice are
  Superadmin/CEO/COO, Health Director, Procurement Director, HR Director,
  Park Head, Health Manager, Assist/Ground, and Investor.
- Entity history side panels should link to
  `/operations/audit?resource_type=...&resource_id=...`, not a People/HR-only
  audit tab.

## Schema And Local Data Decision

This section is superseded for the current Path B V1 base. Do not preserve or
reuse local goat-only rows as target truth. Build the clean mixed-herd schema and
reseed local/dev/test herd data through the species-aware importer described in
the PRD/TRD.

Use the clean target Goat OS Postgres shape:

- `herd_animals`
- `animal_identifiers`
- `animal_location_history`
- `animal_identity_events`
- `species_catalog`
- `breeds`
- `locations`
- `farm_profiles`, `park_profiles`, `shed_profiles`
- `animal_stage_lookup`
- `audit_log`
- `idempotency_keys`
- `outbox_messages`
- `protocol_*`
- `obligation_*`
- `sop_*`
- `inventory_*`
- `vaccination_completions`

Important schema rules:

- `herd_animals` owns canonical mixed-species identity, lifecycle, current
  location, farm, park, shed, cohort/tag, DOB, origin, entry, exit, and merge
  fields.
- Do not use the legacy Counting DB as a runtime dependency for vaccination,
  audit, source entry, or Herd Register. Treat it as retire/legacy migration
  evidence only.
- The needed "association DB" concept is canonical GoatOS animal-to-shed truth:
  `herd_animals.current_location_id` / `herd_animals.shed_id` plus
  `animal_location_history`, `locations`, and shed/location profile tables. If
  a derived read model is needed, it must be generated from those canonical
  Postgres tables, not from Counting DB.
- Tagging state is derived from current/historical `animal_identifiers`; do not
  add a stored `is_tagged` or `tagging_status` column to `herd_animals`.
- Location truth is `locations` plus profile tables. Do not copy old BigQuery
  shed/census shapes into runtime tables.
- Legacy/BQ/Sheets data may seed canonical rows, but runtime API, sweepers, UI,
  Control Tower, Action Center, Protocol Adherence, Workflows, Vaccination, and
  Audit must read Goat OS Postgres only.
- Local/dev/test herd rows are wiped and reseeded through the clean importer.
  Do not build compatibility code around stale goat-only shapes or old-dashboard
  rows.
- Supplier/HF warmup uses the existing procurement/source-entry schema
  (`procurement_loads`, `procurement_load_goats`, `source_holding_stays`,
  `procurement_pc_handoffs`, proof/audit/outbox tables). Do not create a
  separate holding-farm goat schema and do not mark source-only goats as clean
  active herd.
- Trusted procurement holding-park vaccination evidence becomes vaccination history/imported
  completion evidence only through the review/trust gate. Untrusted or
  conflicting source evidence must not suppress a post-arrival dose.

Local seed/E2E data strategy:

- Prefer testing the trigger with a newly created goat from `/counts/herd`; that
  proves the real operator/admin entry point.
- Existing canonical goats in local DB are useful for list/read/passport smoke,
  but they must not be replayed as `goat.created`. Use the Preventive Care (PC) backfill path for
  existing canonical goats.
- Add an idempotent minimal seed command or documented SQL fixture for the
  vaccination trigger pack if it does not already exist. It must seed only:
  tenant/user grant if missing, one real farm/park/shed chain, animal stage
  lookup/profile rows, source-backed approved vaccination protocol/version/rules
  when available, vaccination SOP version, minimal vaccine inventory, and any
  actor/scope grants required for local testing.
- If source-backed approved rule values are not available, the seed may create
  draft/not-source-backed config for UI validation only, but that path must not
  claim obligation generation readiness.
- The final local E2E seed path must run against the same database the API uses;
  no frontend fixtures, no separate SQLite/JSON state, no in-memory-only goats.

Accepted local trigger variants:

```text
Variant A: existing canonical local DB
  migrations current
  -> approved protocol/SOP/inventory present
  -> operator creates a new clean goat through /counts/herd
  -> goat.created drives vaccination

Variant B: clean local Docker DB
  apply migrations
  -> run idempotent vaccination trigger seed
  -> operator creates a new clean goat through /counts/herd
  -> goat.created drives vaccination

Variant C: existing canonical goats
  migrations current
  -> run Preventive Care (PC) backfill generator, chunked and idempotent
  -> missing obligations/drives are generated without replaying old rows

Variant D: source-entry procurement holding-park trigger
  migrations current
  -> create or seed one procurement holding-park load with 4-5 week holding
  -> reject-before-truck branch proves no Preventive Care (PC) / park/vaccination leakage
  -> accepted-intake branch proves trusted procurement holding-park evidence + same vaccination generator
```

For E2E, Variant A or B is the preferred proof because it validates the actual
goat creation trigger. Variant C is a migration/cutover proof, not a substitute
for testing the create-goat trigger. Variant D is required before claiming the
procurement supplier-warmup use case from the screenshots is covered.

## Parallel Build Contract

Backend Codex and frontend Claude can work in parallel, but the contract boundary
must stay sharp.

Backend owns:

- canonical schema writes and migrations, if needed
- permission/route registration
- OpenAPI operation IDs, request/response schemas, examples, and errors
- generated TypeScript client update
- seed/backfill commands and local runtime wiring
- audit/outbox/relay/sweeper/generation behavior
- API-level tests, migration checks, query-plan/load checks, replay checks

Frontend owns:

- routes, sidebar leaves, top-bar behavior, page layout, drawers/modals, and
  click states for the approved surfaces only
- generated-client usage through server-side fetchers/actions
- UI fidelity ledger and screenshots
- disabled/hidden states when backend contracts are intentionally absent
- no local route handlers or fake fixtures for goats, audit, vaccination,
  obligations, proof, or verification

Parallel rule:

- Both agents must treat `Current Repo Review Status (2026-06-25)` as the
  inventory of existing work. Audit existing create-goat, bulk import, operations
  audit, outbox/eventbus relay, procurement accepted-intake enqueue, and
  obligation-sweeper behavior before changing them. Do not rebuild or replace
  those pieces from scratch unless the doc explicitly calls the current
  implementation insufficient.
- Backend publishes contract changes first or in a small early slice:
  OpenAPI paths, operation IDs, DTOs, errors, and generated clients.
- Frontend may build shell/layout immediately, but write actions and data tables
  must stay disabled/empty with honest backend-missing states until generated
  clients exist.
- If frontend needs a shape not in OpenAPI, stop and request/update the backend
  contract. Do not hand-roll DTOs.
- If backend changes a DTO, regenerate clients and update frontend compile
  before claiming backend done.
- Tie-up is a separate integration pass after both prompts finish. It validates
  generated-client drift, real API calls, UI screenshots, and the E2E start gate.

Parallel blockers that must be raised immediately:

- generated client missing for a required action/table
- backend response cannot express `queued`, `skipped_needs_review`,
  `skipped_ineligible`, row-level bulk errors, or audit filters
- frontend needs a click/action that is outside vaccination trigger scope
- source warmup/HF evidence cannot express trusted, untrusted, duplicate,
  conflicting, or rejected-before-truck states
- local delivery is only logging or manual SQL
- someone proposes WebSocket/MQTT as a prerequisite for admin-web audit/activity
  updates in this slice
- source-backed rules are unavailable, making obligation generation unprovable
- UI cannot match the mock without reusing old removed admin primitives

## Current Repo Review Status (2026-06-25)

Verified from the current working tree, not memory:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
go test ./...
```

After `next build`, the live admin-web dev server was restarted on
`127.0.0.1:3300`; `/`, `/counts/herd`, `/operations/audit`, `/vaccination`, and
`/action-center` returned HTTP 200.

Closed or materially advanced:

- Backend create-goat trigger contracts exist:
  `POST /admin/goats`, `/admin/goats/bulk-preview`, and
  `/admin/goats/bulk-commit`, with generated TypeScript client types.
- Backend create-goat writes canonical goat, identifiers, location history,
  decision/event rows, transactional audit, idempotency, and `goat.created`
  outbox.
- Procurement accepted intake now writes `goat.created` identity/outbox/audit
  rows for accepted goats only.
- `outbox-relay` has a real local `eventbus` publisher mode that registers the
  same vaccination `goat.created` generation handler.
- `obligation-sweeper` is packaged and compiles, and backend tests for identity,
  outbox, obligation, vaccination, operations audit, and procurement pass.
- `/operations/audit` has backend list/summary routes, generated clients, and a
  real admin-web page.

Status before E2E (most data-plane items CLOSED 2026-06-26):

- ~~`/counts/herd` write UI is not wired.~~ DONE: `Register goat` and `Import
  sheet` open real drawers posting `createAdminGoat` / `previewAdminGoatBulkImport`
  / `commitAdminGoatBulkImport` through the generated admin client
  (`features/counts/herd-actions.ts`). Only `New report` stays disabled (no API).
- ~~No single local run has proven seed -> goat create -> relay
  `GOATOS_OUTBOX_PUBLISHER=eventbus` -> generation -> sweeper -> SOP task ->
  proof/SOP submission -> verification accept -> `vaccination_completions` ->
  CT/AC/PA/WF/Vaccination/Passport reads.~~ DONE: captured live in one run with
  concrete IDs and idempotent replay; repeatable via
  `tools/dev/vaccination-chain-proof.sh`. Full IDs + per-surface read-model table
  in `docs/runbooks/vaccination-local-business-chain.md`. (A local-DB gap was
  fixed via the approved goose/psql path: migration 000082 fanout tables were
  unapplied on the docker DB — `sop_task_submission_fanouts` missing 500'd the SOP
  submission until applied.)
- `seed-vaccination-trigger` creates the protocol/inventory/SOP/lot trigger pack
  (published version `b011`, rule `b012`, SOP `b0..0002`, FEFO lot `b002`); the
  captured command sequence now exists (the script above).
- `procurement_pc_handoffs.event_status = emitted` still means outbox enqueued,
  not downstream success — treat downstream assertions as the proof (unchanged).
- ~~Config still needs a tenant/category protocol list/read endpoint.~~ DONE
  2026-06-26: `GET /protocols?category=…` (`listProtocolConfigs`, `protocol.read`)
  wired to `/config`. See pre-E2E audit B3 (CLOSED).
- ~~Supplier warmup/Holding Farm vaccination is not covered.~~ DONE for scope:
  purpose-specific warmup, HF dose import/review, and trusted-evidence
  suppression exist (see `context/frontend/supplier-warmup-vaccination-gaps.md`).
- Remaining: optional production roster expansion beyond the source-derived
  ET/K1/day-21 local/dev baseline, the four-goat negative matrix in one run,
  full click-matrix closure, and Google/prod provisioning.

## Claude Feedback Counter-Review (2026-06-25)

Accepted and fixed in docs:

- The feedback correctly identified a governance mismatch: `/counts/herd` and
  `/operations/audit` were reopened by this trigger-closure doc, but
  `apps/admin-web/AGENTS.md` and `context/frontend/current-admin-web-scope.md`
  still omitted them from Active Routes and still made old Counts/Herd wording
  look forbidden. Those docs now list `/counts/herd`, `/operations/audit`, and
  the scoped procurement source-entry routes as active surfaces, while keeping
  unrelated Counts modules, old `/herd`, old Operations, and broad dashboard
  routes removed.

Countered / downgraded:

- The alleged procurement in-flight idempotency race is not a correctness
  blocker in the current repository shape: `reserveIdempotency` and
  `completeIdempotency` run inside the same transaction for the write paths
  reviewed, so a concurrent same-key insert waits on the first transaction's
  key outcome rather than reading a committed empty `result_id`. This is now
  guarded by `TestProcurementIdempotencyReserveSerializesConcurrentSameKey`,
  which must pass normally and under a focused `-race` run. Keep broader
  lock-timeout/load behavior as a follow-up, not as a P1 data-corruption claim.
- The suggested "make AcceptIntake all-or-none" is already the intended shape:
  accepted goats, Preventive Care (PC) handoffs, audit/outbox writes, load status, and idempotency
  completion run in one transaction. What is still useful is an explicit
  regression test proving replay after crash/rollback/commit returns a complete
  handoff set and never a partial list.

Accepted as prompt backlog:

- `AddGoatToLoad` fingerprints the original request before mutation, but should
  include every client-meaningful mutable source-goat field, including
  `selection_reason` and explicit `warmup_days`, so same-key/different-payload
  replays cannot be silently accepted.
- `AcceptIntake` stores result identity as the load, then replays through
  `listPCHandoffs(load_id)`. That is functionally aligned today, but the
  backend closeout should either use the stored result identity deliberately or
  document that `result_id = load_id` is the replay contract for the aggregate
  handoff result.
- `/vaccination` SOP quick-view currently depends on `listSops({ limit: 200 })`
  plus client-side vaccination filtering. Before completion, the backend/
  frontend contract should provide a category/code filter or a named
  vaccination-SOP binding so a tenant with more than 200 SOPs cannot show a
  false empty state.
- `/operations/audit` should use Admin / Data Ops breadcrumb/navigation copy,
  and disabled pager/buttons should use valid `aria-disabled="true"` semantics.

## System Design Readiness Gate

E2E must wait until the local-machine variant is runnable and the Google
component plan/readiness is reviewed. Local E2E may run on Docker/emulator
components, but do not call the vaccination closure "system ready" until the
Google dev path is either green or explicitly blocked behind a verified
VGoats/approval step.

### Local machine / Docker variant

The local path must run inside the same developer machine environment and use
the same canonical Postgres schema:

- Docker Postgres with all migrations applied.
- Idempotent seed for local actor/scope plus the minimal vaccination trigger
  pack.
- API using that Docker Postgres.
- Admin-web using generated clients against that API.
- Outbox relay or in-process dispatcher that actually delivers `goat.created`
  to the vaccination generation handler.
- Pub/Sub emulator may be used if the Pub/Sub publisher is wired locally.
- Logging-only outbox publish does not count as delivery.
- Generation handler writes real `obligation_instances`.
- Sweeper runner batches due obligations and creates drive/SOP task rows.
- SOP bridge/proof APIs create submission/proof/verification state.
- Verification accept writes `vaccination_completions` and obligation status
  events.
- Audit Log API reads the same `audit_log`.
- UI reads the same Postgres-backed state through backend APIs.

Local E2E is blocked if any part is still "seeded visual only", "outbox row
only", "logging-only relay", "frontend fixture", "manual SQL after UI", or
"handler only tested in isolation".

### Google dev/stage/prod components

Before any Google-backed E2E or release claim, verify and state the active
account, org, folder, project, and repo. Goat OS must target Mesha/VGoats,
`vgoats.com`, and the correct Goat OS project (`goatos-dev`, `goatos-stg`, or
`goatos-prod`), never Heva/Slice/default projects.

Google readiness means:

- Cloud SQL Postgres exists for the target environment and migrations are
  applied through the approved migration job/path.
- Artifact Registry images are built for API/admin-web/workers.
- Cloud Run API/admin-web are configured with the right secrets, database, and
  auth mode.
- Outbox relay worker/job is deployed with `GOATOS_OUTBOX_PUBLISHER=pubsub` and
  a real broker client, not the logging fallback.
- Pub/Sub topic, subscription, DLQ, retry policy, IAM publisher/subscriber
  roles, and service-agent DLQ IAM are present.
- Sweeper worker/job or Scheduler-triggered job is deployed and idempotent.
- Secret Manager versions, service accounts, least-privilege IAM, and Cloud SQL
  connectivity are configured.
- Observability exists for API, relay, sweeper, Pub/Sub publish failures, DLQ,
  generation counts, audit write failures, and queue lag.
- No production/stage resource is created or mutated from an unverified account
  or wrong org/project.

### Local-to-Google component equivalence

Before visual E2E, the backend agent must document which local component proves
the same contract as the Google dev component. Local can use a lighter adapter,
but it must exercise the same boundary and failure semantics where the
vaccination flow depends on them.

| System concern | Local machine equivalent | Google dev equivalent | Required before E2E |
| --- | --- | --- | --- |
| Database | Docker/local Postgres with current migrations | Cloud SQL Postgres | same schema, migrations, seed, indexes, query-plan checks |
| API runtime | local API against the same Postgres | Cloud Run API | same env contract, auth/RBAC mode, generated OpenAPI clients |
| Admin-web | local Next admin-web on `:3300` | Cloud Run/admin-web deploy | generated clients only, no local fixtures, same route behavior |
| Outbox delivery | in-process eventbus or Pub/Sub emulator, never logging-only | Pub/Sub topic/subscription/DLQ | `goat.created` delivered to generation handler, retry/DLQ behavior documented |
| Worker relay | local `outbox-relay` with real publisher mode | relay worker/job with `GOATOS_OUTBOX_PUBLISHER=pubsub` | no fallback to logging when publish is requested |
| Sweeper | local `obligation-sweeper` command/job | Scheduler or worker job | idempotent leasing, replay safe, creates batches/SOP tasks |
| SOP/proof media | local proof adapter or deterministic test proof refs | storage/media adapter and signed/upload path | proof state reaches verification through backend API |
| Audit | local `audit_log` writes/read API | same `audit_log` in Cloud SQL | mutations rollback on audit failure unless explicitly non-critical |
| Secrets/auth | local env/dev token fixtures | Secret Manager/service accounts/IAM | active account/org/project verified before cloud mutation |
| Observability | local logs/metrics or documented probes | Cloud Logging/Monitoring/Error Reporting | API, relay, sweeper, Pub/Sub, DLQ, DB pressure visible |
| Large lists | local indexed query-plan checks | Cloud SQL query-plan/load check | cursor pagination, bounded page sizes, no unbounded scans |

If a Google component is not yet created because of org/billing/IAM approval, do
not block local integration work. Mark it as `verified external blocker` with
the exact missing approval and keep local E2E strictly on the documented local
equivalent. Do not claim Google dev readiness until the Google row is actually
verified in the VGoats context.

### Realtime transport decision

Do not add WebSocket or MQTT for this vaccination closure. The admin-web screens
in this scope need current operational reads, not sub-second streaming:

- Admin / Data Ops Audit Log / Activity Trail reads append-only `audit_log` through
  cursor-paginated HTTP APIs.
- Control Tower, Action Center, Protocol Adherence, Workflows, Vaccination,
  Source Entry, Herd Register, and Goat Passport read Postgres-backed API
  projections.
- The frontend may refetch after mutations, expose a manual refresh, and use
  bounded polling for open operational pages if product needs near-live status.
- Staleness must be visible through top-bar date/as-of copy or row timestamps;
  do not imply a live feed when the page is polling or manually refreshed.

The realtime/event stack for this slice is backend-to-backend:

- Postgres transaction writes canonical rows, audit row, and outbox row.
- Outbox relay publishes to Pub/Sub or local in-process/eventbus delivery.
- Consumers/workers/generation/sweeper update Postgres.
- Admin-web reads the resulting state through generated HTTP clients.

Future push may be added only behind a separate requirement:

- Use SSE or WebSocket for authenticated browser live updates only if operators
  need push latency that bounded polling cannot satisfy.
- Use FCM/mobile push for Goat OS mobile app notifications, not browser MQTT.
- Use MQTT only for device/field telemetry such as RFID readers, scales,
  collars, environmental sensors, or gateway-managed hardware. MQTT messages
  must terminate at a device gateway/telemetry adapter; they must not mutate
  canonical goat, vaccination, audit, or procurement truth directly.
- Any future push channel is a notification/cache invalidation layer. It is not
  source of truth and must never replace Postgres, audit, idempotency, outbox, or
  Pub/Sub worker semantics.

### Million-goat operations invariants

The design must stay safe for one million goats and repeated operator actions:

- Cursor pagination everywhere; no unbounded list endpoints.
- Query-plan validation rejects sequential scans on hot paths such as `goats`,
  `obligation_instances`, `audit_log`, `outbox_messages`,
  `inventory_stock_movements`, and large history/event tables.
- Batch sizes are bounded and resumable for import, generation, backfill,
  sweeper, outbox relay, audit export, and report reads.
- All consumer/generator/sweeper/import operations are idempotent with stable
  keys and safe replay.
- `FOR UPDATE SKIP LOCKED` or equivalent leasing is used for concurrent workers.
- Audit/event tables remain partitioned where already designed; new hot
  time-series tables must follow the same partition/index pattern.
- Bulk import preview never loads entire files into UI state beyond a bounded
  preview window; backend streams or chunks where needed.
- UI tables use server pagination/search/filtering. No client-side filtering
  over all goats/audit rows.
- Browser polling, if used, must be bounded by route visibility, scope filters,
  cursor/page size, and backoff. No whole-audit or whole-herd polling loops.
- Stock reserve/consume/release is ledgered and cannot go negative under
  concurrent drives.
- DLQ/poison messages are visible in audit/ops observability and can be replayed
  safely after fix.
- Load tests for true one-million-goat scale belong in `goatos-stg` or an
  explicit temporary local volume with cleanup, not in everyday local dev data.

## Backend Build Scope

### 1. Herd Register goat creation API

Add a minimal admin write path. Suggested contract:

```text
POST /admin/goats               operationId: createAdminGoat
POST /admin/goats/bulk-preview  operationId: previewAdminGoatBulkImport
POST /admin/goats/bulk-commit   operationId: commitAdminGoatBulkImport
GET  /goats/search?limit=...    existing app-api read path
GET  /goats/{goat_id}           existing app-api read path
GET  /goats/{goat_id}/timeline  existing app-api read path
```

The UI route is Counts -> Herd Register, but the API resource can stay under
`/admin/goats` because it mutates canonical goat identity.

Single create request fields:

- `farm_code` or `farm_id`
- `park_id`
- `shed_id` or `current_location_id`
- `rfid`
- `animal_identifier_1`
- `animal_identifier_2`
- `breed`
- `sex`
- `dob`
- `dob_estimated`
- `weight_kg`
- `dam_identifier`
- `sire_or_lot`
- `origin_type` (`birth`, `procured`, or an explicit approved source/import
  type; unknown origin is not accepted for clean-slate herd animals)
- `origin_ref`
- `photo_url` or evidence reference
- `entry_date`

Minimum validation:

- Require tenant, actor, idempotency key, and stable request fingerprint.
- Current superseding rule: require Animal ID 1 at accepted/canonical creation;
  Animal ID 2 stays optional until double RFID tagging is live, then must become
  mandatory in both application validation and DB constraints.
- Require park and current shed/location for the clean E2E trigger.
- Reject unknown location, retired location, unusable vaccination location, bad
  sex, invalid DOB, negative weight, unknown breed if strict breed lookup exists.
- Normalize Animal ID 1/2 with the global lifetime identifier rules.
- Any identifier value already present in current or historical records belongs
  to exactly one animal forever; a new animal using it is rejected/fixed at
  source, not parked in GoatOS as review/conflict state.

Write in one transaction:

- `goats`
- `goat_identifiers`
- `goat_location_history`
- `goat_identity_events`
- `audit_log`
- `idempotency_keys`
- `outbox_messages` with event type `goat.created`

Response:

- `goat`
- `identifiers`
- `location`
- `decision`
- `events`
- `idempotency`
- `generation_status` (`queued`, `skipped_needs_review`, `skipped_ineligible`)
- `trace_id`

### 2. Bulk import preview and commit

Support the mock drawer without building a full import product.

Preview must:

- Parse CSV rows with the mock columns.
- Validate each row and return row-level errors/warnings.
- Resolve location and existing identifiers.
- Show whether each row will create, conflict, skip, or require review.
- Not write goats.

Commit must:

- Commit only valid rows.
- Be idempotent by file hash + row index + row fingerprint.
- Support partial success with row-level results.
- Write the same events/audit/outbox rows as single create.
- Never hard-delete bad rows. Corrections are new attempts.

### 3. Supplier Holding Farm warmup / accepted-intake path

Cover the screenshot use case as a minimal procurement trigger branch, not as a
full procurement product.

Minimum API/runtime requirements:

- Source Entry board and Load Detail read APIs expose load rows with source
  party/supplier, holding farm location, expected/loaded/arrived/accepted/
  rejected counts, warmup age/days, tagging counts, HF vaccination status,
  health/selection status, owner/ownership state, proof state, next action, and
  cursor pagination.
- Source goat write path accepts or preserves source tag/RFID/temp ID,
  `holding_location_id`, `warmup_started_at`, `warmup_ended_at`, derived or
  supplied `warmup_days`, identity state, ownership state, health state, source
  proof refs, and source vaccination evidence refs.
- Pre-dispatch decision supports accept, reject, defer, and block with reason,
  actor, proof/ref, idempotency key, and immutable audit.
- Reject-before-truck writes procurement history and supplier-credit/audit
  metadata if present, but creates no active herd count, Preventive Care (PC) handoff, active
  vaccination obligation, drive, SOP task, or Goat Passport active-herd row.
- Partial load reject is per goat: accepted goats may proceed; rejected/deferred
  goats remain procurement history/work.
- Arrival review and accepted intake must enforce clean identity, resolved
  ownership, passed/deferred health rules, truck proof, and park/shed assignment.
- Accepted intake emits the same downstream generation path as Herd Register
  create. If the event payload does not carry trusted source evidence, the
  handler must load it from Postgres before deciding next due.
- Procurement holding-park vaccination evidence is stored as trusted vaccination history/imported
  completion evidence only after review/trust. Until trusted, it is visible as a
  pending/conflicting evidence item and must not suppress post-arrival due work.
- Generation uses trusted accepted completion evidence plus trigger/repeat/
  catch-up logic so a trusted procurement holding-park dose avoids
  double-dosing; park quarantine and on-arrival rules are separate post-arrival
  obligations.

Minimum tests/fixtures:

- 4-5 week procurement holding rows are valid and persisted in
  `source_holding_stays`.
- Source purpose remains context and does not create a separate trusted-vaccine
  clock.
- Warmup outside the configured governed range becomes at-risk/overdue work,
  not invalid data loss.
- Rejected-before-truck cannot create or retain active vaccination work.
- Accepted clean intake creates the Preventive Care (PC) handoff/generation input.
- Trusted procurement holding-park vaccination evidence suppresses only the matching due dose and does
  not suppress unrelated vaccines, boosters, quarantine, or on-arrival rules.
- Untrusted, duplicate, conflicting, or mismatched holding-park evidence stays reviewable
  and does not change due basis.
- Replayed pre-dispatch, dispatch, arrival, and accepted-intake operations are
  idempotent and do not duplicate handoffs, audit rows, obligations, or
  completion/history evidence.

### 4. Vaccination generation trigger

Wire the existing generation handler into the runtime used by local E2E.

Build requirements:

- Register `vaccinationapp.NewGoatCreatedHandler(...)` in API/bootstrap or the
  local event dispatcher used by E2E.
- Accepted-intake and Herd Register create must both emit or synchronously call
  the same generation path.
- The handler must only generate from published source-backed protocols.
- Use deterministic obligation idempotency:
  `tenant + protocol_version + rule + goat + due_at + sequence`.
- A replayed `goat.created` event must create zero duplicates.
- Mark procurement handoff/event status from real downstream success/failure.
- For accepted-intake goats, load trusted procurement holding-park vaccination evidence before
  generating due work, then generate only missing obligations from published
  rules.

### 5. Local relay and sweeper readiness

E2E is not ready until there is one documented local sequence that starts:

- Postgres with migrations/seeds
- API
- admin-web
- local outbox relay or in-process dispatcher
- vaccination generation handler
- sweeper runner

Logging an outbox message is not delivery. The local path must deliver to the
handler or use a documented synchronous local app-service path with equivalent
tests.

Also wire the local sequence so the seed/create path can be repeated without
resetting the database:

- same idempotency key returns the original response
- new idempotency key with same RFID returns duplicate/conflict
- relay replay creates no duplicate obligations
- sweeper replay creates no duplicate batches/tasks
- verification replay creates no duplicate completion/stock movement

### 6. Audit infrastructure and business Audit Log read model

Audit must be a generic backend platform concern, similar in spirit to
`backend/internal/platform/observability` for logging, and separately a
business-facing dashboard surface in Admin / Data Ops.

Important distinction:

- Observability/logging is diagnostic telemetry for engineers and operators.
- Backend/platform audit is immutable technical/business history for
  operator/admin/system actions, stored in `audit_log` for debugging, replay,
  idempotency proof, investigations, and traceability.
- CEO/admin Audit Log is a business projection over relevant audit rows. It is a
  visible dashboard feature under Admin / Data Ops, not a raw developer audit
  explorer.
- Logging an action is not audit. Writing audit is not optional for business
  mutations.

Current state to account for:

- The schema already has a shared partitioned `audit_log` with actor, action,
  resource, scope, before/after state, metadata, trace, and monthly partitions.
- Some existing modules still write `audit_log` through local helper functions
  such as `insertAudit`. Do not copy that pattern into the vaccination closure.

Backend build requirement:

- Add or extend a generic audit package, preferably
  `backend/internal/platform/audit`, with a small `Recorder` / `TxRecorder`
  interface and a Postgres adapter over the committed `audit_log`.
- Mutating app services should depend on the audit interface, not direct SQL
  strings. Postgres repositories may use a transaction-bound recorder so the
  audit entry commits or rolls back with the business mutation.
- Keep `authaudit` compatible or route it through the generic recorder, but do
  not create a second auth-only shape for new product audit.
- New vaccination/counts/procurement/SOP/inventory writes must not introduce new
  module-local `insertAudit` helpers.
- Existing local helpers can be left alone if out of scope, but the new closure
  code must establish the reusable path and should document follow-up migration
  of old helpers if not converted now.

Generic audit event contract:

- `tenant_id`
- `actor_id`
- `actor_type` (`human`, `system`, `worker`, `service`)
- `action` stable string, e.g. `goat.create`, `goat.bulk_commit`,
  `vaccination.obligation.generate`, `sop.task.submit`,
  `vaccination.verification.accept`
- `resource_type`
- `resource_id`
- `scope_type`
- `scope_id`
- `before_state`
- `after_state`
- `metadata` containing at minimum domain/module/category, result/status,
  idempotency_key or operation_id, request_id/trace_id if available,
  outbox_event_id if the action emits or consumes one, anomaly flag/reason when
  applicable
- `trace_id`

Robustness rules:

- Business mutation + audit write are one transaction. If the audit write fails,
  rollback the mutation unless the event is explicitly a non-critical auth/session
  audit.
- Idempotent replay of the same mutating operation must not create duplicate
  business audit rows. Return the original response or record a clearly distinct
  replay diagnostic only if product requires it.
- Audit writes must never publish events or call external systems; audit is a
  local transaction sink.
- Metadata must stay bounded. Store enough identifiers for investigation, but do
  not dump unbounded CSV rows, full file payloads, secrets, tokens, or service
  account JSON.
- Use `recorded_at` cursor pagination and partition-prunable filters for read
  APIs. No unbounded audit reads or synchronous large exports.
- The business Audit Log is cross-module: it can be powered by
  domain/module/action/scope metadata, but it is not owned by Preventive Care (PC), People,
  Counts, SOP, Procurement, or an Operations vertical.

Current implementation status: the read-only audit API on the existing
partitioned `audit_log` already exists at `/operations/audit` and
`/operations/audit/summary`, with generated admin-client types and backend
`internal/operationsaudit`. Treat this surface as finish-and-verify, not a
rebuild. The route and operation IDs may keep `/operations/audit` as backend
implementation detail; the visible dashboard IA remains Admin / Data Ops ->
Audit Log.

```text
GET /operations/audit?limit=&cursor=&actor_id=&action=&resource_type=&resource_id=&scope_type=&scope_id=&from=&to=&anomalies_only=
  operationId: listOperationsAudit
GET /operations/audit/summary?as_of=&park_id=
  operationId: getOperationsAuditSummary
```

Architecture decision for this slice: keep backend audit generic and append-only,
then present a business Audit Log view over those rows. The generated API may
return raw audit fields plus bounded metadata; the frontend can map that
generated row into a business view-model for the current slice. Add backend
contract fields only when the UI cannot derive the required business field from
existing row data, and make those changes additive rather than replacing
`internal/operationsaudit`.

Solid architecture bar:

- Backend `audit_log` remains the append-only source of truth for debug, proof,
  replay, and investigation.
- The CEO/admin Audit Log is a read-only product projection, not a second audit
  store and not a developer console.
- Frontend projection/mapping is acceptable for labels, operation-family grouping,
  proof/result display, and role-lens presentation when it uses generated API
  rows and bounded metadata.
- Backend additions are only for missing data, RBAC/scope, query-plan, cursor,
  or deterministic-error gaps; they must be additive and contract-tested.
- Role preview is UX scope preview for superadmin/CEO/COO only. Server RBAC and
  scope checks remain authoritative for every real request.
- Future operation families plug into the same audit row metadata and
  operation-family mapping; they do not create a new vertical, route family, or
  audit table.

Architecture review status:

- The backend module shape is accepted for this slice: `domain`, `ports`, `app`,
  and `adapters/{http,postgres}` with wiring at the bootstrap/composition edge.
  Do not redesign the module structure before E2E.
- The frontend data boundary is accepted for active admin-web: pages and
  features use generated clients through `apps/admin-web/lib/api/*`; no direct
  datastore access or hand-written backend DTOs.
- Current architecture debt is function-level SRP/maintainability, not a
  blocking structural flaw. Large admin-web components and long Go write paths
  can be refactored later, but they are not pre-E2E blockers unless the touched
  code has a concrete bug.
- When refactoring long Go write paths, keep idempotency reservation, business
  writes, audit rows, and outbox/event writes inside the required transaction.
  Extract named steps only; do not split the transactional boundary.
- Fat repository interfaces are acceptable Go-style debt for now. Split
  read/write ports only when a current caller or test needs the narrower
  interface.
- `apps/investor-web-shadow` is a legacy/reference snapshot. Do not use its
  direct BigQuery or local route-handler patterns as architecture guidance for
  active admin-web.

The business view-model must support the dashboard fields from the mock:

- operation family
- operator display and role/lens
- business action
- target type/label/id
- result/status
- proof state/reference
- anomaly flag/reason
- park/scope and role-span applicability
- recorded time and cursor

It must support:

- Operator/admin/system actor.
- Resource type/id.
- Domain/module/category in metadata.
- Scope filtering by hierarchy.
- Export later, but export can stay disabled if not built.
- Anomalies flag from audit metadata for stock mismatch, deletion/void,
  rejection/rework, failed proof, and duplicate identifier conflict.
- Role/span filters that match the top-bar preview lenses for superadmin/CEO/COO
  preview while preserving backend RBAC for real requests.

Do not rebuild or replace `backend/internal/operationsaudit`, the OpenAPI
`/operations/audit` contract, generated client artifacts, or
`apps/admin-web/features/operations-audit` from scratch. Audit them first and
make the smallest scoped change.

Every mutation in this closure must write audit:

- Goat create/import preview/commit.
- Identifier attach/retire.
- Source load/goat add, source health, HF vaccination evidence import/review,
  pre-dispatch decision, dispatch, arrival review, and accepted intake.
- Procurement accept intake.
- Protocol draft/publish/retire.
- SOP save/dry-run/publish.
- SOP task submit.
- Proof upload/complete.
- Verification accept/reject/rework.
- Stock reserve/consume/release.
- Sweeper/system generation events and system skips/deferred results.

Visible Audit Log scope for this pre-E2E slice is current built work only:
Herd Register, vaccination generation/obligation/SOP proof/verification,
Config/SOP authoring where built, Procurement/Source Entry/HF evidence where
contracts exist, and system events that explain those chains. Future mock
operation families must be hidden or explicitly disabled with a reason; do not
invent rows, totals, or local fixture projections for unbuilt business areas.

### 7. Contracts and generated clients

Update OpenAPI and generated TypeScript clients for every new API. Frontend must
not hand-copy DTOs.

### 8. Backend scale and infra proof

Before backend handoff, document:

- whether the local path uses Pub/Sub emulator, in-process dispatcher, or another
  concrete delivery adapter
- how `goat.created` reaches the generation handler
- how sweeper is started locally and in Google environments
- how `outbox-relay` avoids the current logging fallback when Pub/Sub is
  requested
- indexes/query plans for create/search/audit/generation/sweeper hot paths
- bounded page/batch sizes for import/generation/sweeper/backfill/outbox
- DLQ/retry behavior and operator-visible failure state
- generic audit recorder path, transaction behavior, and tests
- seed command/fixture for Variant A/B/C/D above
- migration/backfill behavior for existing canonical local goats
- exact commands used for tests, migrations, SQL plan checks, generated clients,
  and local component smoke

Backend verification before handoff:

Run these commands from `backend/`. The procurement Postgres adapter tests are
Docker-backed integration tests: `pgtest` starts a throwaway Postgres container
and applies committed migrations, so Docker must be available and the test
should not be redirected to a shared dev database.

```bash
go test ./internal/identity ./internal/identity/adapters/http ./internal/identity/adapters/postgres
go test ./internal/platform/audit/...
go test ./internal/vaccination ./internal/vaccination/adapters/postgres ./internal/obligation
go test ./internal/outbox/... ./internal/platform/eventbus/...
go test ./internal/processintegrity/... ./internal/vaccinationexecution/...
go test ./internal/procurement/...
go test ./internal/procurement/adapters/postgres -run TestProcurementIdempotencyReserveSerializesConcurrentSameKey -count=1
go test -race ./internal/procurement/adapters/postgres -run TestProcurementIdempotencyReserveSerializesConcurrentSameKey -count=1
make api-client-check
make sqlc-check
make validate-migrations
make validate-sqlc-plans
```

If `make validate-sqlc-plans` is too slow for a local pass, the backend handoff
must say that explicitly and include the exact hot-path query plans reviewed
instead. Do not omit the scale review silently.

## Frontend Build Scope

### 1. Sidebar additions

Add only these new visible leaves:

- Counts -> Herd Register -> `/counts/herd`
- Admin / Data Ops -> Audit Log -> `/operations/audit`

For Counts, this means the sidebar group has exactly one visible child in this
slice: `Herd Register`. Do not show disabled `Counts overall`, `Count
reconciliation`, `Tagging & identity`, or `Weights & ADG` leaves for mock
fidelity.

Keep the already-reopened Procurement -> Source Entry leaf active for the
supplier warmup / accepted-intake branch and the dependencies that branch
requires:

- Procurement -> Source Entry -> `/procurement/source-entry`
- Procurement Source Entry Load Detail -> `/procurement/source-entry/loads/{load_id}`

Do not add nested procurement command-room leaves. Command lenses stay top-level
with filters such as `/action-center?domain=procurement`.

Keep top-bar park/as-of scope as the single visible owner of scope. Do not add
duplicate page-body park/date chips.

### 2. Counts -> Herd Register screen

Use the mock structure:

- Header crumb `Counts / Herd`.
- Buttons: `Filters`, `Import sheet`, `Register goat`.
- `New report` stays disabled unless backed by a real API.
- KPI cards may be lightweight if backed by `/goats/search` or a small backend
  read model. Do not fake totals.
- Herd table reads real goats, paginated.
- Row click goes to `/goats/{goat_id}`.

Build the basic dependency closure required for this page to work:

- Park/shed/location selectors must use real allowed locations or stay disabled
  with an honest blocker.
- Breed, sex, stage, origin, identifier type, and lifecycle/status choices must
  come from backend contracts, committed lookup constants, or documented
  canonical enums. Do not invent UI-only values.
- Duplicate RFID/old-tag conflict, needs-review, missing-location, inactive/
  exited goat, temp ID, and clean-created states must render distinctly.
- Active herd, review, inactive/exited, and source-only concepts must not be
  merged. Source-only procurement goats do not become active Herd Register rows
  until accepted intake.
- Limited count cards are allowed when they summarize the current backend result
  set or a bounded backend read model. Broad census/reconciliation dashboards
  remain out of scope.
- Entity history/audit links should route to `/operations/audit?...`; goat rows
  should route to contextual Goat Passport.

Register goat drawer/modal:

- Fields match backend minimum.
- Submit calls generated `createAdminGoat`.
- Show backend row-level/conflict errors plainly.
- On success, refresh table and link to Goat Passport.
- Show generation status: queued, skipped, or needs review.

Bulk register drawer:

- Use the mock three-step shape: download template, upload CSV, preview/edit,
  create records.
- Preview calls backend preview; commit calls backend commit.
- `Create 0 records` must stay disabled until there are valid rows.
- No frontend-only HERD array or fixture rows.

### 3. Procurement -> Source Entry supplier warmup UI

Cover the screenshot use case only through backend-backed Source Entry rows.

Source Entry board must include the minimal supplier warmup panel/table:

- Section title: Supplier warmup / Holding Farm, or the nearest mock-faithful
  wording already used in the app.
- Explanatory body may state that purchase/source starts the goat journey, but
  it must not become a marketing/hero block.
- Search and Filters controls use backend query params or stay disabled.
- Table columns map to backend fields: load, holding farm/supplier, animals,
  warmup week/days, tagging, vaccination at HF, health/selection, status.
- Status chips cover warming, cleared to ship, review, partial reject, rejected,
  blocked, blocked, overdue/at-risk, and accepted intake where returned.
- Vaccination-at-HF chips distinguish complete/evidence, due, pending review,
  conflict, and not trusted.
- Row click opens Load Detail; if the generated Load Detail route/client is
  missing, row click is disabled with an honest reason.

Load Detail must expose only the actions needed for the vaccination trigger:

- record/import HF vaccination evidence
- source health/selection result if required by the backend contract
- pre-dispatch accept/reject/defer/block
- dispatch/truck proof
- arrival review
- accepted intake

Every action must call generated clients and show backend validation errors. Do
not fake a completed state by mutating local React arrays.

Preventive Care (PC) / Vaccination screens may show read-only source context for accepted-intake
goats: origin/source, entry/intake date, trusted procurement holding-park evidence used for due basis,
and any defer signal. Preventive Care (PC) / Vaccination must not own source warmup, pre-dispatch
rejection, supplier credit, or arrival discrepancy review actions.

UI no-leak checks:

- Rejected-before-truck/source-only/unresolved goats are visible only in
  Procurement Source Entry or top-level command lenses with `domain=procurement`.
- They do not appear in `/counts/herd` active rows, `/vaccination` active work,
  vaccination execution rows, or Goat Passport active-herd summaries.
- Park-side quarantine/on-arrival work is visually separate from source warmup;
  do not merge both into one ambiguous status chip.

### 4. Admin / Data Ops Audit Log screen

Finish `/operations/audit` as the Admin / Data Ops business Audit Log surface:

- Summary cards: actions in view/today, awaiting verification, proof coverage,
  flagged anomalies.
- Business controls from the mock: operation-family chips, `Viewing as` role/span
  tabs synced to the top-bar role preview, search action/operator or ID,
  All results/Awaiting/Rejected/Proof gaps tabs, Operators/span control,
  Anomalies only, Clear only when filters are active.
- Do not render a large raw debug form for UUID/domain/module/category/status/
  target fields as the main CEO/admin UX. Exact backend filters may be preserved
  in URL params and shown as active chips for entity-history links.
- Table columns: time, operation family, operator, action, target, result, proof.
- Entity/history buttons link back to the same audit route with filters or to
  `/goats/{goat_id}` where appropriate.
- Export button stays disabled until backend export exists.
- Operation chips must cover only real current built families unless a future
  mock family is shown disabled with a clear future reason. Do not show fake
  events or fake counts for unbuilt areas.
- Audit Log is read-only. It may open entity/history/proof context, but it must
  not expose developer-only raw audit editing, replay, or mutation controls.
- The concrete remaining gap is mock fidelity and live proof, not initial route
  creation: sync `Viewing as` to the shared top-bar role-lens model, remove the
  raw debug filter card from the primary UX, preserve generated-client data, and
  prove the page with populated local audit rows.

History side panels:

- Replace mock-only history with real audit/timeline data where available.
- `Open full audit log` should route to `/operations/audit?...`.

### 5. Vaccination surfaces after trigger exists

Existing vaccination screens should not gain fake buttons. They should show:

- Accepted clean Herd Register goat appears only after generation/sweeper.
- Accepted clean procurement goat appears only after accepted intake and real
  generation/sweeper.
- Needs-review or duplicate goats do not appear as active vaccination work.
- Deferred goats show an explained deferred row, not a missing row.
- Trusted procurement holding-park vaccination evidence appears as imported completion/history context
  and adjusts next due only for matching rules.
- Untrusted/conflicting holding-park evidence stays reviewable and does not suppress due
  work.
- Verification queue only shows real proof submissions.

Frontend verification before handoff:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
```

When backend/admin-web can run, capture desktop and narrow visual smoke. Inspect
the screenshots, especially sidebar open/collapsed/mobile states, Herd Register
drawer, bulk import drawer, Source Entry supplier warmup board/load detail, and
Admin / Data Ops Audit Log controls/table.

Frontend completion must include the UI fidelity ledger described in the golden
rule section. Do not mark frontend done from passing lint/typecheck alone.
If automated click coverage is not added in this slice, include a manual click
ledger for every visible control with the result classification and screenshot
path. A visual screenshot without click classification is not enough.

## Edge Cases That Must Not Be Missed

- Duplicate Animal ID 1/2 anywhere in current or historical records -> reject
  the new animal row; identifiers are globally single-use for life and are never
  reused after death, sale, transfer, or broken/fallen tags.
- Missing park/shed/current location -> no active vaccination trigger.
- Temp birth kid -> create only if the configured birth flow issues the two
  required Animal ID values or a documented short-lived tagging task owns the
  missing slot; vaccination must still resolve to one `animal_id`.
- Birth-created newborn -> generated obligations only if published rules match
  age/stage; otherwise no immediate active vaccination work.
- Procured adult with trusted prior vaccination history -> next due from last
  accepted completion/import, not DOB.
- Procurement holding-park warmup 4-5 weeks near the buying region -> valid
  governed source warmup.
- Source purpose remains context and does not create separate 0-day/two-week/
  breeding clocks for trusted vaccination suppression.
- Supplier Holding Farm warmup outside the purpose-specific policy window ->
  at-risk/overdue procurement work, not automatic active Preventive Care (PC) vaccination work.
- HF vaccination evidence untrusted/conflicting/duplicate/mismatched -> visible
  review state; must not suppress a post-arrival dose until trusted.
- HF vaccination evidence trusted -> suppress only the matching already-completed
  dose via imported completion/history basis; do not suppress unrelated vaccine
  rules, boosters, quarantine, or on-arrival obligations.
- Quarantine, ICU, sick, pregnant/lactating, or blocked animals -> visible
  deferred/explained obligation when rules say defer; no silent skip.
- Rejected-before-truck, blocked, arrival-rejected, unresolved, extra
  unknown goats -> procurement history only; never active Preventive Care (PC) vaccination.
- Partial source load reject -> accepted goats proceed independently; rejected
  goats remain procurement history and supplier-credit/audit metadata where
  applicable.
- Shared/pending ownership after advance payment -> cannot become accepted intake
  until ownership truth is resolved or explicitly accepted by policy.
- Source holding location is not the final park/shed -> do not count it as
  current active herd location or Counts/Herd active row.
- Arrival mismatch or extra unknown goat -> arrival gate/identity review only;
  no accepted intake until reconciled.
- Source warmup and park quarantine are distinct phases; the UI and backend must
  not merge them into one status or one obligation source.
- Goat shifted after obligation creation -> recompute to destination shed or
  individual pre-shift dose if destination batch is already complete.
- Dead/sold/lost/missing goat -> cancel active obligations in the same business
  transaction.
- Merged goat -> child writes blocked; passport redirects to survivor.
- Identifier attach/retire alone -> must not duplicate vaccination obligations
  unless the identity state changes from needs-review to clean and emits the
  proper trigger.
- Replayed create/import/accept-intake -> idempotent no-op or original response.
- Bulk import partial failure -> valid rows commit, bad rows return errors; no
  hard delete.
- Stock-out -> visible blocker/anomaly, no negative ledger balance.
- Verification reject/rework -> remains actionable; no completion row accepted.
- Audit write failure on a mutating transaction -> rollback unless explicitly
  documented as non-critical for auth/session audit only.
- Million-goat scale -> pagination, indexed filters, no unbounded live scans.
- Existing local canonical goat rows -> use backfill, not fake `goat.created`
  replay.
- Existing local non-canonical/stale goat rows -> quarantine/fix/seed canonical;
  do not support stale schema at runtime.
- Missing approved source-backed vaccine rules -> no generation readiness claim.
- Pub/Sub requested but relay falls back to logging -> E2E blocked.
- Pub/Sub delivery succeeds but consumer/generation fails -> retry/DLQ/audit
  path visible; no silent success.
- Sweeper runs before SOP version/inventory is ready -> visible blocker, no fake
  task.
- Bulk upload with 10k+ rows -> bounded preview, row-level errors, commit in
  chunks, no frozen browser.
- Audit export over large range -> disabled or async/cursor-based; no giant
  synchronous download.
- Mobile/narrow sidebar/drawer -> no clipped text, no unreachable footer
  actions, no overlay hiding active controls.
- Top-bar scope/date conflict with page filters -> top bar wins; page body does
  not duplicate scope chips.
- Button/link with no real destination/action -> disabled with reason or removed.

## E2E Start Gate

Do not run E2E until all of this is true:

- `POST /admin/goats` creates one clean goat in Counts/Herd.
- Counts/Herd dependency closure is real enough to support that create/import:
  backend-backed location/reference selectors, identifier conflict/review states,
  active/review/inactive row states, bounded count summaries if shown, Passport
  links, bulk preview errors, and audit/history links.
- Source Entry can create or read one procurement holding-park load with 4-5
  week holding, evidence state, and load-row statuses from backend APIs.
- The pre-dispatch reject branch proves rejected/source-only goats do not appear
  in active herd count, active Preventive Care (PC) vaccination work, vaccination execution, or
  Goat Passport active-herd rows.
- The accepted-intake branch proves trusted procurement holding-park vaccination evidence feeds due
  basis and avoids duplicate post-arrival dosing while keeping quarantine/
  on-arrival work separate.
- The create writes goat identity/location/audit/outbox/idempotency in one
  transaction.
- `goat.created` is delivered to the vaccination generation handler.
- Published vaccination protocol + SOP generate obligations for that goat.
- Sweeper creates the drive/batch/SOP task.
- Proof/SOP submission creates a verification item.
- Verification accept writes `vaccination_completions`.
- Admin / Data Ops Audit Log shows the relevant chain.
- New closure writes use the generic audit recorder path, not module-local
  hand-rolled `insertAudit` helpers.
- Control Tower, Action Center, Protocol Adherence, Workflows, `/vaccination`,
  shed drilldown, and Goat Passport all read the same Postgres truth.
- UI fidelity ledger is complete for every visible sidebar leaf, button, table
  action, drawer/modal control, filter, and entity-history link in the built
  slice.
- The ledger explicitly covers hamburger/mobile nav, sidebar group toggles,
  top-bar scope/date/profile/notification/theme controls, page tabs, chips,
  `Clear all`, active filter chips, sortable headers, cursor pagination,
  page-size controls, empty/error/loading states, row clicks, drawer close/
  cancel/submit, and every `done`/status/toggle/action button.
- Every backend-backed table used by those controls has server-side
  pagination/search/filter/sort semantics, page-size caps, stable ordering,
  invalid cursor/sort behavior, empty-page-after-mutation behavior, and
  hot-path query-plan review.
- Every mutating control in scope has an explicit idempotent backend action,
  audit row, permission/scope check, loading/error/retry UI, and double-click/
  replay behavior.
- UI fidelity ledger includes the Supplier warmup / Holding Farm board/table,
  row click, Load Detail actions, HF evidence states, reject path, accepted-intake
  path, and disabled future procurement controls.
- Local Docker/Postgres/API/admin-web/relay-or-dispatcher/sweeper/SOP/audit
  sequence is documented and repeatable from a clean DB and from an existing
  canonical local DB.
- Google dev component readiness is green or explicitly blocked behind verified
  VGoats approval: Cloud SQL, migrations, Cloud Run API/workers,
  Pub/Sub/DLQ/IAM, Scheduler/jobs, secrets, and observability.
- Query-plan/load guardrails for one-million-goat scale are in place or marked
  as explicit blockers.

Only after this gate is green should the visual/Playwright E2E plan run.

## Prompt Hand-Off Rule

The prompts below are launchers, not duplicate specs. Keep detailed scope,
contracts, edge cases, ledgers, verification gates, and stop conditions in this
doc and the linked readiness/gap docs. If requirements change, update the docs
first, then keep the prompts compact enough to paste into two parallel agents.

Architecture guardrails are part of every product finish prompt. "Architecture
debt later" only means broad SRP/refactor cleanup is separate; it does not allow
new code to bypass generated clients, backend ports/adapters, transaction
boundaries, RBAC/scope, audit, or outbox/idempotency rules.

## Backend Prompt

```text
Work in /Users/ravi/mesha/goatos.

Read AGENTS.md, SKILLS.md, context/README.md, docs/phases/README.md,
context/execution/vaccination-trigger-closure-parallel-handoff.md,
context/execution/vaccination-pre-e2e-readiness-audit.md, and
context/frontend/supplier-warmup-vaccination-gaps.md.

Backend only; frontend edits are limited to generated client artifacts. Audit
current code, finish the documented backend pre-E2E gaps, and publish contract /
generated-client changes early so frontend can wire against them.

Run the documented backend verification, update the backend rows of the Full
Interaction Closure Contract artifact, and stop before claiming visual or
business E2E.
```

## Frontend Prompt

```text
Work in /Users/ravi/mesha/goatos.

Read apps/admin-web/AGENTS.md,
context/frontend/current-admin-web-scope.md,
context/execution/vaccination-trigger-closure-parallel-handoff.md,
context/execution/vaccination-pre-e2e-readiness-audit.md,
context/frontend/supplier-warmup-vaccination-gaps.md, and latest
mock/goatos-dashboard-mock.html.

Frontend only. Use generated clients only; no invented DTOs, local route
handlers, fixtures, fake totals, or client-only business mutations. Build the
approved slice from the docs; if a generated-client contract is missing, leave
the control honestly disabled/empty and record the blocker.

Run the documented frontend verification, update the UI fidelity ledger with
screenshot paths, and stop before claiming E2E.
```

## Single Audit IA + Business Projection Prompt

```text
Work in /Users/ravi/mesha/goatos. Read apps/admin-web/AGENTS.md,
context/frontend/current-admin-web-scope.md,
context/execution/vaccination-trigger-closure-parallel-handoff.md,
context/execution/vaccination-pre-e2e-readiness-audit.md,
context/frontend/supplier-warmup-vaccination-gaps.md, and latest
mock/goatos-dashboard-mock.html.

The business Audit Log is already landed: `/operations/audit` list/summary,
generated client, backend `internal/operationsaudit`, and frontend
`features/operations-audit`. Do not rebuild them. You are sole owner of those
Audit Log files for this run; no concurrent agent edits.

Frontend mock-fidelity is primary; backend is verify-only unless you find a
concrete additive contract gap. Close only these gaps: remove the Operations
sidebar vertical, keep Audit Log under Admin / Data Ops, sync Audit `Viewing as`
to the shared top-bar role-lens model, replace raw debug filters as the primary
UX with the mock business controls, and confirm visible events are limited to
current built surfaces.

Follow the accepted architecture while making those changes: generated clients
only, no raw backend URLs, no fake rows/totals/fixtures/invented DTOs, no
client-only business mutations, additive backend contracts only, and no
transaction-boundary changes for idempotency/audit/outbox. This is a
mock-fidelity finish, not an SRP/refactor pass; do not split large components
beyond the extraction needed to share the role-lens model. Done means documented
backend/admin-web gates plus `smoke:visual:live` with populated local audit rows
and ledger screenshots. Green lint/typecheck/build alone is not proof. Stop
before E2E.
```

## Post-Closure Architecture Debt Prompt

Use this only after the pre-E2E product closure is green or in a separate cleanup
lane. It is not part of the Audit/Herd/Register finish run because the finish run
already has to obey the architecture guardrails above.

```text
Work in /Users/ravi/mesha/goatos. Read AGENTS.md, apps/admin-web/AGENTS.md,
context/frontend/current-admin-web-scope.md, and
context/execution/vaccination-trigger-closure-parallel-handoff.md.

Architecture is structurally accepted; do not redesign modules or routes.
Identify only high-risk SRP cleanup in touched files. For frontend, split large
components only where it reduces current maintenance risk. For backend, extract
named helpers inside long write paths without changing the transaction boundary
for idempotency, audit, and outbox/event writes.

No product-scope expansion, no shadow-app patterns, no E2E claims. Run focused
tests/typecheck/lint for touched files and report before/after risk.
```
