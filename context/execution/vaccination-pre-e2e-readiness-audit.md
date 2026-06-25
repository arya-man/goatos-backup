# Vaccination Pre-E2E Readiness Audit

Date: 2026-06-25

Purpose: define what must be built and checked before visual or Playwright E2E is meaningful for the current Goat OS admin-web vaccination flow.

Do not run E2E as discovery. E2E is the last proof after the business chain and every reachable click are already honest.

## Source Anchors

Business/wiki evidence found through Graphify:

- `Handbooks/PHC_Director.pdf` page 2: PHC weekly operations, daily checklist tasks, stock control and record management.
- `Handbooks/PHC_Director.pdf` page 5: vaccine storage and cold-chain integrity, plus health data documentation.
- `Handbooks/PHC_Director.pdf` page 6: shed sanitization context.
- `Handbooks/PHC_Director.pdf` page 6 text graph: Health Data Recorder enters vaccination, deworming, and health data into software.

Committed Goat OS source of truth:

- `context/frontend/current-admin-web-scope.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/procurement-vaccination-e2e-plan.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`
- `docs/phc-vaccination/PRD.md`
- `docs/phc-vaccination/TRD.md`

## Non-Negotiable Business Chain

E2E can start only after this chain works from current source:

```text
source-backed vaccination protocol published
  -> vaccination SOP published with cold-chain, vaccine batch, proof, verification, repeat-per-goat semantics
  -> accepted-intake goat enters clean PHC scope
  -> goat.created or accepted-intake event is delivered to the registered vaccination generation handler
  -> vaccination obligations are generated
  -> sweeper creates shed drive / obligation batch / SOP task
  -> backend proof API records proof
  -> SOP task submission records answers + proof refs
  -> verifier accepts / rejects / requests rework
  -> accepted verification creates vaccination_completion and updates obligation/completion state
  -> booster / next-dose basis is generated when applicable
  -> Control Tower, Action Center, Protocol Adherence, Workflows, /vaccination, shed drilldown, and Goat Passport read the same Postgres truth
```

Rejected-before-truck, source-only, owner-missing, extra-unknown, unresolved, arrival-rejected, dead, sold, lost, and blocked goats must never appear as active PHC vaccination or vaccination execution work.

## Current Green Checks

These checks passed during the audit:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
go test ./internal/permissions ./internal/platform/eventbus ./internal/processintegrity/app
go test ./internal/processintegrity/adapters/http ./internal/vaccination/adapters/http ./internal/vaccinationexecution/adapters/http ./internal/sop/adapters/http ./internal/procurement/adapters/http
```

These are not E2E proof. They only show route shape, compile health, and lightweight handler coverage.

## 2026-06-25 Repo Review Update

Verified from the current working tree after the trigger-closure forward work:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
go test ./...
go test -race ./internal/procurement/adapters/postgres -run TestProcurementIdempotencyReserveSerializesConcurrentSameKey -count=1
```

The build also required a clean admin-web dev-server restore on `127.0.0.1:3300`;
after restart, `/`, `/counts/herd`, `/operations/audit`, `/vaccination`, and
`/action-center` returned HTTP 200.

Current corrected status:

- `POST /admin/goats`, `/admin/goats/bulk-preview`, and
  `/admin/goats/bulk-commit` now exist in OpenAPI/generated client/backend.
  The backend create path writes goat identity/location/history, audit, idempotency,
  and a `goat.created` outbox row.
- `/counts/herd` is still read-only in admin-web: the table is real
  `/goats/search`, but `Register goat` and `Import sheet` are intentionally
  disabled until drawer/actions are wired to the generated admin client.
- `/operations/audit` now has generated-client backed list/summary endpoints and
  a real admin-web page. Mock fidelity is still partial: export is disabled, and
  role/span-of-control semantics are page-level filters over backend audit rows,
  not the full mock interaction model.
- `outbox-relay` now supports `GOATOS_OUTBOX_PUBLISHER=eventbus|local|inprocess`
  and registers the `goat.created` vaccination generation handler. The default
  logging publisher still does not count as delivery.
- `obligation-sweeper` now exists as a command and can create batches/SOP tasks
  when invoked with tenant, SOP version, actor, and optional vaccine item.
- Procurement accepted-intake now enqueues a `goat.created` outbox message and
  writes audit through `platform/audit`, but `procurement_phc_handoffs.event_status
  = emitted` currently means "event row enqueued", not "downstream generation and
  read models succeeded".
- Procurement idempotency reserve is regression-tested under same-key
  concurrency: `TestProcurementIdempotencyReserveSerializesConcurrentSameKey`
  asserts one transaction claims the key, the second blocks until commit, then
  replays the original `result_id`. The focused test has passed normally and
  under `-race`.
- `seed-vaccination-trigger` seeds protocol/inventory trigger fixtures, but the
  full local scenario is not yet proven because the frontend create drawer,
  published vaccination SOP/task/proof path, relay run, sweeper run, verification
  completion, and CT/AC/PA/WF/Vaccination read-model assertions have not been
  executed end-to-end in one local run.
- The latest mock's Supplier warmup / Holding Farm panel is not implemented:
  no purpose/classification field, no HF dose import/review/completion contract,
  no no-double-dose imported-completion reconciliation proof, and no
  Procurement-backed panel matching the mock table.

## Click Matrix

Every clickable control must do exactly one of these:

- call a real generated backend API
- submit a real server action
- navigate to a real route
- open/close local UI state such as a modal or disclosure
- be visibly disabled with an honest reason

Before E2E, this matrix must cover more than headline buttons. It must include
every visible sidebar group/leaf, top-bar menu item, page-body control, table
control, drawer/modal control, and entity/history link in the approved slice.

Required coverage:

- Navigation: desktop rail, mobile hamburger, sidebar groups/leaves, badges,
  active state, disabled state, mobile scrim, outside-click/Escape close.
- Top bar: scope mode, park selector, date/as-of selector, theme toggle,
  notifications, profile/role menu, and every menu item.
- Tables: server search, filters, active filter chips, `Clear all`, sortable
  headers, page size, next/previous cursor, no rows, first page, last page,
  one-page result, empty page after mutation, invalid cursor, invalid sort.
- Forms/drawers/modals: every input/select/chip/checkbox/toggle/upload/paste
  field, close/cancel/submit footer, validation, loading, success, error, retry,
  Escape/backdrop behavior, and mobile footer reachability.
- Actions: every `done`, `verify`, `reject`, `request rework`, `accept`,
  `defer`, `block`, `publish`, `save draft`, `dry run`, `submit`, `upload
  proof`, `commit`, `clear`, and `export` affordance.

Backend-backed controls must have generated-client contracts, RBAC/scope
checks, idempotency/replay behavior, audit writes, deterministic error handling,
and server-side pagination/search/filter/sort for large lists. Frontend-only
state is allowed only for UI chrome such as open/closed menus, theme preview, or
form edits before submit; it must not mutate business truth.

### Shell And Navigation

- `Control Tower` -> `/`
- `Action Center` -> `/action-center`
- `Protocol Adherence` -> `/protocol-adherence`
- `Workflows` -> `/workflows`
- `PHC / Vaccination` -> `/vaccination`
- `Procurement / Source Entry` -> `/procurement/source-entry`
- `Admin / Data Ops / Config` -> `/config`
- `Admin / Data Ops / SOP Library` -> `/sops`
- Desktop hamburger collapses/expands nav; mobile hamburger opens/closes nav.
- Park top-bar menu changes `?park=` using backend-safe location IDs while displaying human labels.
- As-of date menu is point-in-time only; inactive range choices are disabled.
- Theme toggle changes theme locally.
- Notifications must stay disabled until a real notifications route/action exists.
- Role preview menu is local preview state only.

### `/` Control Tower

Required:

- Read `/control-tower/vaccination`.
- Show only broken or at-risk vaccination process gaps.
- Config/SOP blocker links go to `/config?category=vaccination` and `/sops`.
- Alert rows and next-action links go to `/workflows/{row_id}`.
- Footer links go to `/action-center`, `/protocol-adherence`, `/workflows`, `/vaccination`, and `/vaccination#execution`.

### `/action-center`

Required:

- Status board reads `/vaccination/action-center`.
- SOP queues read `/vaccination/verification-queue`.
- Work-state and severity chips preserve top-bar scope and call server-side filters.
- Verify, Reject, and Request rework submit real server actions against `/vaccination/completions/{completion_id}/accept|reject`.
- Passport links go to `/goats/{goat_id}`.
- Work cards go to `/workflows/{row_id}`.
- `My tasks` stays disabled until owner filtering exists.
- Advanced `Filters` stays disabled until a real drawer or query contract exists.

Missing before E2E:

- A real operator execution path for start SOP, upload proof, submit SOP answers, and return to verification. This can be admin-web or field-app driven, but E2E must exercise it through generated APIs, not fixture rows.

### `/protocol-adherence`

Required:

- Read `/vaccination/adherence`.
- Severity chips preserve top-bar scope.
- Table must show Expected, Actual, Gap, Severity, Owner, Next action, Evidence.
- Next-action links go to `/workflows/{row_id}`.
- Config/SOP setup links go to `/config?category=vaccination` and `/sops`.
- Deferred/explained rows must be visible, not hidden.

### `/workflows` And `/workflows/{row_id}`

Required:

- `/workflows` reads `/vaccination/action-center` for live workflow instances.
- Catalog rows navigate to `/workflows/{row_id}`.
- Drilldown reads `/vaccination/workflows/{row_id}`.
- Drilldown links go to Goat Passport, shed execution detail, Action Center, and Protocol Adherence.
- Chain must show config -> obligation -> drive -> SOP -> proof -> verification -> completion.

Missing before E2E:

- Data must be produced by the real generation + sweeper + SOP/proof/verification loop, not hand-seeded process-integrity rows.

### `/vaccination`

Required:

- Read `/vaccination/operations`.
- Header SOP button opens the real vaccination SOP quick-view from `/admin/sops`.
- Import sheet is disabled until bulk drive import exists.
- New drive links to `/config?category=vaccination` because drives are generated from published config, not manual CRUD.
- Matrix links to Protocol Adherence and Action Center.
- Execution section reads `/vaccination/execution`.
- Execution filters preserve top-bar scope.
- Shed rows navigate to `/vaccination/execution/sheds/{shed_id}`.
- Park attention links go to `/action-center?park={park_id}`.

Missing before E2E:

- Operator execution controls are not present here. If execution belongs to field app, the admin UI must link/label that clearly and E2E must cover the field-app/API path.

### `/vaccination/execution/sheds/{shed_id}`

Required:

- Read `/vaccination/execution/sheds/{shed_id}`.
- Back links return to `/vaccination#execution`.
- Show work-state summary, drives, owner chain, blockers/deferred reasons, SOP/proof/verification state, and next action.

### `/config`

Required:

- Generic Admin/Data Ops authority screen.
- Category changes form fields and `rule_dsl`.
- Vaccination fields include eligibility, dose rows, booster/catch-up/missed-dose policy, defer states, SOP/proof policy, stock/cold-chain/lot requirements, source/review approval.
- Save draft calls real protocol create/version/rule APIs.
- Publish calls real source-backed publish API and must be disabled until source/review gate passes.
- Impact preview calls real vaccination impact endpoint.

Missing before E2E:

- There is no protocol list endpoint wired to the Config table. After saving/publishing, the page can still render "No protocol rules yet." Build a real list/read model and generated client binding before E2E.
- Seed or author a source-backed, approved vaccination protocol that can publish and generate obligations.

### `/sops`

Required:

- Read `/admin/sops` and `/admin/sops/{sop_id}`.
- Show vaccination SOPs only.
- Search filters only real returned SOPs.
- New SOP opens the builder.
- Save draft, dry-run, and publish call real admin SOP APIs.
- Proof policy stays photo/video only and uses canonical `subject_scope`.
- Non-vaccination SOP domains stay hidden or disabled.

### `/goats/{goat_id}`

Required:

- Contextual drilldown only. No global goat search.
- Identity passport reads real goat/timeline APIs.
- Identifier add/retire actions submit real admin APIs with idempotency keys.
- Vaccination section reads `/goats/{goat_id}/passport`.

### `/procurement/source-entry` And Load Detail

Required for the procurement -> vaccination bridge:

- Board reads `/procurement/source-entry/loads`.
- New load submits real create-load action.
- Load detail reads `/procurement/source-entry/loads/{load_id}`.
- Add source goat, source health, pre-dispatch decision, dispatch, arrival review, and accept intake submit real backend actions.
- Media upload remains disabled with an honest reason until proof capture is in this slice.

Missing before E2E:

- Accepted intake currently creates `procurement_phc_handoffs`, but the handoff does not itself prove vaccination obligations were generated.
- Build either a production-equivalent event path or a documented synchronous local app-service path from accept-intake to vaccination generation.

## Hard Blockers Before E2E

### B1. Accepted Intake Trigger Is Enqueued But Not E2E-Proven

Current observation:

- `AcceptIntake` updates goat location/state, inserts `procurement_phc_handoffs`,
  writes a `goat.created` identity event, writes audit, and inserts an outbox
  message.
- API bootstrap and the local outbox relay eventbus mode both register the
  `goat.created` generation handler.
- `procurement_phc_handoffs.event_status = emitted` is currently set when the
  outbox event is enqueued, not after the relay/generation/sweeper/read-model
  chain succeeds.

Build:

- Prove accepted-intake clean goat creates vaccination obligations through the
  same relay/handler runtime used locally.
- Add a post-delivery status or separate reconciliation/audit signal if
  `event_status` must represent downstream success; otherwise document `emitted`
  strictly as "outbox enqueued".
- Test accepted-intake clean goat creates vaccination obligations and rejected/
  unresolved goats do not.

### B2. Local Outbox/Consumer/Sweeper Is Partially Ready, Not Yet Proven

Current observation:

- `outbox-relay` can deliver to the in-process eventbus when
  `GOATOS_OUTBOX_PUBLISHER=eventbus|local|inprocess`.
- The default empty/logging publisher is still logging-only and is not an E2E
  delivery path.
- `obligation-sweeper` exists and compiles; app/repository tests pass, but a
  full local run from fresh seed -> created goat -> relay -> obligations ->
  sweep -> SOP task has not been captured.

Build:

- Provide one documented command or script sequence for local Postgres, API, admin-web, outbox relay, consumer/event dispatcher, and sweeper.
- Relay must deliver to handlers, not only mark/log.
- Sweeper must create obligation batches and SOP tasks from due obligations.
- Tests must assert downstream read models changed.

### B7. Herd Register Write UI Is Not Wired

Current observation:

- Backend and generated clients have create/bulk goat operations.
- `/counts/herd` still disables `Register goat` and `Import sheet` with an
  honest reason, so the trigger can be tested only through backend/API or a
  seed/backfill path until the frontend drawer/actions land.
- The active route is not enough by itself. Herd Register also needs the
  basic dependency closure that makes goat creation/import honest: location/
  park/shed selection, lookup choices, identifier validation/conflict states,
  bulk preview row errors, active/review/inactive row states, Passport links,
  and audit/history links.
- Unrelated Counts leaves are not needed for this trigger path. Do not show
  `Tagging & identity`, `Weights & ADG`, `Counts overall`, or `Count
  reconciliation` as active or disabled sidebar placeholders.

Build:

- Wire the mock-shaped `Register goat` drawer to generated `createAdminGoat`.
- Wire the mock-shaped bulk drawer to generated preview/commit endpoints.
- Refresh `/counts/herd`, `/operations/audit`, and vaccination read screens after
  mutation, and show `generation_status` honestly.
- Build required setup dependencies inside `/counts/herd` using current GoatOS
  contracts and generated clients. Do not revive old `/herd`, legacy Counting DB
  runtime shapes, old import-review, unrelated Counts dashboard/module code, or
  disabled sidebar placeholders for future Counts modules.

### B8. Supplier Warmup / HF Evidence Is Still A Required Gap

Current observation:

- The latest mock includes `Supplier warmup - Holding Farm` with purpose,
  warmup policy by purpose, HF vaccination evidence, and no-double-dose copy.
- Repo code still has universal 45-70 warmup copy/rules and only raw
  `trusted_vaccination_history` passthrough on accepted intake.

Build:

- Add source-goat purpose/classification to backend contract and frontend form.
- Add HF dose import/review/completion contract and generated client.
- Reconcile trusted HF evidence as imported completion/history before generation
  suppresses matching post-arrival doses; untrusted/conflicting evidence must not
  suppress due work.

### B9. Full UI Control Closure Is Not Yet Proven

Current observation:

- Build/typecheck gates prove compilation, not whether every visible control is
  wired honestly.
- Some controls are correctly disabled today, but there is no complete ledger
  yet proving all sidebar, top-bar, page-body, table, drawer/modal, and
  entity-history controls are real, disabled, or removed.
- Latest mock checked for the handoff is
  `mock/goatos-dashboard-mock.html` with local mtime `2026-06-25 12:21:51`.
  If the mock changes after that, refresh the ledger before implementation.
- The latest mock's operation-axis audit and reusable table controls add real
  obligations for `/operations/audit` and every in-scope table: summary/anomaly
  cards, operation/operator/span filters, top/bottom pagination, active chips,
  `Clear all`, sorting/header behavior, row/entity-history clicks, and export
  state.
- The Counts nav boundary is now mechanically checked:
  `apps/admin-web/scripts/check-ia-guard.mjs` fails if the shell exposes
  anything under Counts other than `Herd Register` for this slice.

Build:

- Complete the full interaction ledger from
  `context/execution/vaccination-trigger-closure-parallel-handoff.md`.
- Add or verify backend list/action contracts for every table/action in the
  ledger: cursor pagination, whitelisted sort, server filters/search, clear
  semantics, idempotency, audit, permission/scope checks, and deterministic
  error behavior.
- Add or verify frontend behavior for menus, sort, clear, pagination, toggles,
  done/status buttons, loading/error/retry states, double-click protection,
  Escape/outside-click close, mobile drawers, and disabled future controls.
- Treat any visible button/link/control with no real destination, no generated
  client, no disabled reason, or client-only business mutation as an E2E blocker.

### B3. Config Authority Screen Cannot Show Existing Protocol Rules

Current observation:

- Protocol create/version/rule/publish APIs exist.
- No `GET /protocols` or equivalent list endpoint exists for the Config table.
- Admin-web currently hardcodes `rules: []`.

Build:

- Add a tenant/category-scoped protocol list/read endpoint.
- Regenerate client types.
- Wire `/config` table to show drafts/published/retired versions, source review state, linked SOP, effective date, and publisher.

### B4. SOP/Proof Execution Loop Is Not Click-Complete

Current observation:

- Backend app task, proof upload, proof complete, and SOP submission APIs exist.
- Admin vaccination screens show SOP/proof/verify state but do not provide start SOP, proof upload, or submit answers controls.
- Verification queue can accept/reject/rework existing completions.

Build:

- Decide whether the current E2E drives execution through admin-web, operator-mobile, or direct generated API seed.
- Whichever path is chosen must create proof through the backend proof API, submit SOP answers with proof refs, and produce the verification queue item.
- Admin-web must not show fake execution buttons.

### B5. Seeded Scenario Is Not Yet Proven

Build a deterministic local scenario:

- Source-backed vaccination protocol.
- Published vaccination SOP.
- Vaccine stock/batch and cold-chain/proof requirements.
- Locations: source holding, CBE park, CBE shed.
- Four goats: clean accepted intake, rejected before truck, owner missing/unresolved, extra unknown arrival.
- Accepted goat only reaches PHC vaccination, execution context, Action Center, Protocol Adherence, Workflows, and Passport.
- Rejected/unresolved/extra goats stay out of PHC vaccination work.

### B6. Procurement Domain Lenses Are Not The Default E2E Target

Current admin-web command screens default to vaccination. Do not add nested procurement command routes.

If procurement command-lens E2E is included, build it through the existing top-level routes:

```text
/action-center?domain=procurement
/protocol-adherence?domain=procurement
/workflows?domain=procurement
/workflows/{row_id}?domain=procurement
```

Otherwise keep procurement proof limited to Source Entry Board and Load Detail, plus the accepted-intake handoff into vaccination.

## E2E Start Gate

Start E2E only when every item below is true:

- Backend route smoke passes for all vaccination routes.
- Config can list the source-backed published protocol.
- SOP Library can list the published vaccination SOP.
- Accepted-intake path generates vaccination obligations for the clean goat only.
- Sweeper creates a shed drive, batch, and SOP task.
- SOP/proof submission creates a verification queue item.
- Verification accept creates a completion and updates obligation state.
- CT/AC/PA/WF/Vaccination/shed/Passport read the same updated Postgres state.
- Every visible button/link/control in the click matrix is real, local-state only, navigational, or honestly disabled.
- The click matrix includes menus, nav groups, top-bar controls, pagination,
  sort, filters, clear, toggles, done/status actions, drawer footers, entity
  history links, loading/error/empty states, and mobile/narrow controls.
- Backend/API behavior for those controls is covered by generated clients,
  idempotency, audit, RBAC/scope checks, deterministic errors, server-side
  pagination/search/filter/sort, and hot-path query-plan review. Same-key
  concurrent idempotency reserve behavior must remain covered by
  `TestProcurementIdempotencyReserveSerializesConcurrentSameKey` and a focused
  `-race` run.
- Desktop and narrow visual screenshots are inspected only after the data chain is green.

After the gate is green, run the E2E plan in `context/execution/procurement-vaccination-e2e-plan.md`.
