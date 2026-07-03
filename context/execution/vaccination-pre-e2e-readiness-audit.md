# Vaccination Pre-E2E Readiness Audit

Date: 2026-06-25

Purpose: define what must be built and checked before visual or Playwright E2E is meaningful for the current Goat OS admin-web vaccination flow.

Do not run E2E as discovery. E2E is the last proof after the business chain and every reachable click are already honest.

## Architecture Posture

The current backend/frontend architecture is accepted for the pre-E2E slice. New
pre-E2E code must follow these guardrails, but do not turn E2E readiness into a
broad redesign or SOLID cleanup pass.

- Backend keeps the hexagonal module shape: `domain`, `ports`, `app`, and
  `adapters/{http,postgres}` with dependencies pointing inward and wiring at the
  bootstrap edge.
- Active admin-web keeps the generated-client boundary through
  `apps/admin-web/lib/api/*`. No raw backend URLs, direct datastore access,
  hand-written DTOs, or shadow-app route-handler patterns.
- Function-level SRP debt is real but non-blocking for this E2E gate. Split a
  large component/function only when the current pre-E2E fix touches it and the
  extraction reduces risk.
- Long backend write paths may be decomposed into named helpers, but the
  idempotency reservation, mutation, audit, and outbox/event work must stay
  inside the required transaction.
- `apps/investor-web-shadow` is legacy/reference only; do not model active
  admin-web architecture on its direct BigQuery or local API-route patterns.

## Source Anchors

Business/wiki evidence found through Graphify:

- `Preventive Care Director handbook source` page 2: Preventive Care (PC) weekly operations, daily checklist tasks, stock control and record management.
- `Preventive Care Director handbook source` page 5: vaccine storage and cold-chain integrity, plus health data documentation.
- `Preventive Care Director handbook source` page 6: shed sanitization context.
- `Preventive Care Director handbook source` page 6 text graph: Health Data Recorder enters vaccination, deworming, and health data into software.

Committed Goat OS source of truth:

- `context/frontend/current-admin-web-scope.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/procurement-vaccination-e2e-plan.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`

## Non-Negotiable Business Chain

E2E can start only after this chain works from current source:

```text
source-backed vaccination protocol published
  -> vaccination SOP published with cold-chain, vaccine batch, proof, verification, repeat-per-goat semantics
  -> accepted-intake goat enters clean Preventive Care (PC) scope
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

Rejected-before-truck, source-only, owner-missing, extra-unknown, unresolved, arrival-rejected, dead, sold, lost, and blocked goats must never appear as active Preventive Care (PC) vaccination or vaccination execution work.

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
- `/counts/herd` write UI is wired (B7 CLOSED): the table is real
  `/goats/search`, and `Register goat` / `Import sheet` open real drawers that
  post `createAdminGoat` / `previewAdminGoatBulkImport` / `commitAdminGoatBulkImport`
  through the generated admin client (`features/counts/herd-actions.ts`). Only
  `New report` stays disabled (no API in this slice).
- `/operations/audit` now has generated-client backed list/summary endpoints and
  a real Admin / Data Ops business Audit Log page. Mock fidelity is still
  partial: export is disabled, and role/span-of-control semantics are page-level
  filters over backend audit rows, not the full mock interaction model. It must
  not appear as a separate Operations sidebar vertical or as a raw developer
  audit/debug form. Audit Log `Viewing as` must mirror the top-bar
  superadmin/CEO/COO role-preview lenses, and the visible events must stay
  limited to built business surfaces until future domains land. Treat backend
  `internal/operationsaudit` and generated `/operations/audit` contracts as
  landed; verify them, but do not rebuild unless a concrete additive contract
  gap is found.
- `outbox-relay` now supports `GOATOS_OUTBOX_PUBLISHER=eventbus|local|inprocess`
  and registers the `goat.created` vaccination generation handler. The default
  logging publisher still does not count as delivery.
- `obligation-sweeper` now exists as a command and can create batches/SOP tasks
  when invoked with tenant, SOP version, actor, and optional vaccine item.
- Procurement accepted-intake now enqueues a `goat.created` outbox message and
  writes audit through `platform/audit`, but `procurement_pc_handoffs.event_status
  = emitted` currently means "event row enqueued", not "downstream generation and
  read models succeeded".
- Procurement idempotency reserve is regression-tested under same-key
  concurrency: `TestProcurementIdempotencyReserveSerializesConcurrentSameKey`
  asserts one transaction claims the key, the second blocks until commit, then
  replays the original `result_id`. The focused test has passed normally and
  under `-race`.
- `seed-vaccination-trigger` seeds protocol/inventory/SOP/lot trigger fixtures,
  and the full local scenario is now PROVEN end-to-end in one run (B1/B2/B5
  CLOSED): Herd Register create -> goat.created outbox -> eventbus relay ->
  generation -> sweeper batch/SOP task -> 3 proofs + SOP submission -> completion
  -> verification accept -> CT/AC/PA/WF/Vaccination/shed/Passport read models.
  Repeatable via `tools/dev/vaccination-chain-proof.sh`; captured IDs and per-
  surface results are in `docs/runbooks/vaccination-local-business-chain.md`.
  (One local-DB gap surfaced and was fixed via the approved goose/psql path:
  migration 000082 fanout tables were unapplied on the docker DB.)
- Supplier warmup / Holding Farm panel is covered for the current scope (B8
  CLOSED) — purpose/classification + HF dose import/review and the trusted-
  evidence suppression path exist; see
  `context/frontend/supplier-warmup-vaccination-gaps.md`.

## 2026-06-26 Non-E2E Local/Code Closure — Pending-Item Classification

Closing the remaining NON-E2E / NON-Google / NON-prod vaccination items honestly.
"Local/code closure" = local code, contracts, Config UI, publish gate, SOP
binding, proof-policy validation, docs, and gates are honest and green.
"Source-derived dev baseline" = existing wiki/PRD/SOP/legacy evidence has been
turned into local/dev config choices: ET K1 day-21 as the schedule-bearing row
and K2=42. The 2026-06-26 roster expansion pass found labels only for
PPR/FMD/HS/BQ, so all four are `label-only closed` and no new schedule-backed
rows were added.

Done in this pass:

- **Backend proof-shape hardening (CLOSED).** Version-level `proof_policy` must be
  an OBJECT carrying a real proof token under a recognized array key
  (`required_proofs`/`types`/`required`); a bare array (`["video"]`), `{}`,
  `{"required_proofs":[]}`, blank tokens, scalar (`{"required":true}`), and
  metadata-only objects (`{"subject_scope":"batch"}`) are all rejected. Row-level
  `schedule[].proof_policy` may be a bare array of non-blank tokens OR the object
  shape; a present-but-blank row proof is rejected and does NOT silently fall back
  to the version proof. Split into `versionProofHasContent` / `rowProofHasContent`
  in `backend/internal/protocol/app/publish.go`; tests in `publish_test.go` now
  drive row-level proof through `rule_dsl.schedule[].proof_policy`, not by putting
  an array in `Version.ProofPolicy`.
- **Config publish honesty (VERIFIED).** Publish is blocked when: not CEO/COO; no
  saved draft; inputs changed since last Save (the saved version is stale); no
  executable SOP version selected; no real proof token; or the source gate fails —
  in that priority order with an explained title
  (`apps/admin-web/features/config/rule-editor-modal.tsx`). The executable SOP is
  the real version-level `sop_version_id` chosen from published SOP Library rows
  (`features/config/protocol-rules-page.tsx` filters `active_sop_version_id`); the
  per-dose dropdown emits `sop_label` for display only
  (`features/config/rule-dsl.ts`), never an executable `schedule[].sop_version`.
  No `sopVersion:"vacc-sop v2"` literal reaches an executable field; no fake SOP
  UUIDs.
- **K1/K2/stage config now backend-driven (CLOSED).** The Config authoring stage
  picker no longer uses hardcoded `K0/K1/K2` frontend literals. A new tenant-scoped,
  bounded, indexed read endpoint `GET /protocols/animal-stages`
  (`backend/internal/protocol/...`; `query.sql` `ListActiveAnimalStages`, ordered by
  `sort_order`, capped at 200) lists active `animal_stage_lookup` rows; the Config
  SSR page loads them via `listAnimalStages()` and passes `AnimalStageOption[]` into
  the editor (`features/config/{protocol-rules-page,config-console,rule-editor-modal}.tsx`).
  The default stage is the first backend band, never a hardcoded `K1`. The only
  stage literal the UI owns is the `ALL_STAGES` filter (a UI scope, explicitly NOT an
  `animal_stage_lookup` row). Three distinct states (no outage hidden as missing
  config): loaded+non-empty → normal picker; loaded+empty → honest seed-state
  (**disabled-with-reason** "Data Ops must seed animal_stage_lookup", all-stages
  draft still allowed); **read FAILED (403/500/down)** → a `stagesError` surfaces as
  an error band (page + inside the modal), the picker is disabled, and **Save +
  Publish are blocked** (highest-priority gate reason) since the true stage set is
  unknown. It never silently falls back to hardcoded bands. Satisfies the Preventive Care (PC)
  vaccination TRD rule that stage bands live in `animal_stage_lookup`, not in code,
  and the AGENTS rule that API failures surface as an error state, not empty data.
- **Reference-read failures are error-vs-empty distinct for BOTH stages AND SOPs
  (CLOSED).** Same fix applied to the executable SOP picker: a failed
  `listSops({status:"active"})` read no longer collapses to `[]` (which read as "no
  published SOP version"). `sopsError` now surfaces an error band (page + inside the
  modal), disables the SOP picker, and blocks Save + Publish — while a genuinely
  empty-but-OK list stays the honest "no published SOP version — publish a SOP first"
  state. Threaded `stagesError`/`sopsError` through
  `protocol-rules-page → config-console → rule-editor-modal`; gate priority is
  stage-read → SOP-read → role → saved → dirty → SOP-selected → proof → source.
- **TRD schema sketch reconciled to the shipped migration (CLOSED).**
  `docs/preventive-care-vaccination/TRD.md` `animal_stage_lookup` no longer claims a `stage_id`/
  `label`/weight-band/hardcoded-`stage_code`-CHECK shape; it now matches the committed
  `000071_location_profiles.sql` (`animal_stage_id` UUID PK, `stage_code text` with NO
  static enum, `name`, age bands, `status`, `sort_order`) and names the migration as
  source of truth. Runtime was already correct; only the doc overclaimed exactness.
- **Docs reconciled (CLOSED).** `docs/protocol-engine/obligation-engine.md`,
  `docs/preventive-care-vaccination/TRD.md`, and
  `context/execution/sop-vaccination-backend-handoff.md` now state that the
  executable SOP binds at `protocol_versions.sop_version_id`, the per-dose
  `schedule[].sop_label` is display-only, and a genuine per-dose executable
  override uses `protocol_rules.sop_version_id` (a real UUID) — the real backend
  capability is preserved, not erased.
- **Preventive Care (PC) roster + K1/K2 (source-derived dev baseline CLOSED).** See
  `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`: K2=42 wins
  over the legacy mock 45 for local/dev; ET/K1/day-21 is the schedule-bearing
  row; the 2026-06-26 roster-expansion follow-up closed PPR/FMD/HS/BQ as
  `label-only closed`.
- **Test coverage (backend CLOSED; frontend gated out honestly).** The publish
  execution-contract gate (`publish_test.go`) and the new
  `GET /protocols/animal-stages` endpoint (`handler_test.go`, incl. the empty-list
  honest-empty case and nil age bands) are covered by Go tests. The pure frontend
  gate helpers (`buildProofPolicy`/`hasProofRequirement`/`buildProtocolRuleRows`
  and the Save→dirty→Publish gating) are NOT unit-tested: `apps/admin-web` ships no
  JS test runner (no vitest/jest, zero `*.test.ts(x)`), so adding tests would mean
  standing up a whole test toolchain — out of scope for this closure. They are
  guarded by `tsc --noEmit` + lint + `next build` only. Standing up a frontend test
  runner is a separate follow-up.

Remaining pending items classified:

| Item | Classification | Basis / exact paths |
| --- | --- | --- |
| Rich SOP DSL semantics (step-type evaluator, conditional/branching form logic) | **Out of current local/code closure.** No DSL evaluator exists and we do NOT fake one. Not on the current Config publish path — publish gates on source + `sop_version_id` + object `proof_policy` only (`backend/internal/protocol/app/publish.go`), never on DSL evaluation. SOP builder emits native step types (`apps/admin-web/features/sops/sop-derive.ts`); no evaluator is claimed. | `backend/internal/protocol/app/publish.go`; `apps/admin-web/features/sops/sop-derive.ts`; `docs/protocol-engine/obligation-engine.md` §5 |
| `as_of` residuals (point-in-time obligation status reconstruction) | **Out of current local/code closure.** Current code does not overclaim: `as_of`/`asOf` is a point-in-time read param threaded into the projection reads (`features/control-tower`, `workflows-landing`, `protocol-adherence`); the top-bar as-of selector is point-in-time only and range choices are disabled (Click Matrix → Top bar). The deeper effective-status reconstruction work is tracked in memory `goatos-phase1-asof-correctness` and is NOT a publish/proof-gate blocker. | `apps/admin-web/features/control-tower/index.tsx:55`; `features/process-integrity/workflows-landing.tsx:95`; `context/execution/vaccination-pre-e2e-readiness-audit.md` Click Matrix |
| Source Entry search / media-capture limits | **Out of current local/code closure.** Visible UI does not overclaim — media upload and advanced search are disabled-with-reason, documented in `context/frontend/supplier-warmup-vaccination-gaps.md`. Disabled-with-reason is acceptable for this slice. | `context/frontend/supplier-warmup-vaccination-gaps.md`; readiness audit B8 (CLOSED) |
| SOP taxonomy / category-filter API gap | **Out of current local/code closure — future scale/API cleanup.** The Config SOP picker shows only real published SOP versions (`active_sop_version_id`); the `/vaccination` and `/sops` quick-views use `listSops({ limit: 200 })` + client-side vaccination filtering (`apps/admin-web/app/(admin)/sops/page.tsx:14`, `features/preventive-care-vaccination/operations.tsx:19`). It cannot fake SOPs, but a tenant with >200 SOPs could under-show. Tracked as a category/code-filter or named-binding follow-up in the trigger-closure handoff backlog; not a publish-path blocker. | `apps/admin-web/app/(admin)/sops/page.tsx:14`; `features/preventive-care-vaccination/operations.tsx:19`; `context/execution/vaccination-trigger-closure-parallel-handoff.md` (SOP quick-view backlog) |
| Preventive Care (PC) / vaccine roster + K1/K2/stage values | **Source-derived dev baseline CLOSED.** K2=42 is selected from wiki/glossary/legacy seed, ET/K1/day-21 is selected from the PRD, and the 2026-06-26 PPR/FMD/HS/BQ expansion pass closed all four as `label-only closed` with no schedule-backed rows added. Later production expansion is normal source-backed versioning, not a local/code blocker. | `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`; `context/execution/vaccination-roster-expansion-followup.md`; `docs/preventive-care-vaccination/TRD.md:64,:145` |

No vague "pending" items remain: each is fixed, classified out-of-scope with a
path, or moved to later source-backed versioning/provisioning.

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
- `Preventive Care (PC) / Vaccination` -> `/vaccination`
- `Procurement / Source Entry` -> `/procurement/source-entry`
- `Admin / Data Ops / Config` -> `/config`
- `Admin / Data Ops / Audit Log` -> `/operations/audit`
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
- Verify, Reject, and Request rework submit real server actions against SOP task review routes (`/admin/tasks/{task_id}/verify|rework`) with `row_version`.
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
- Drilldown links go to Goat Passport, vaccination execution detail with the
  same shed/tag context, Action Center, and Protocol Adherence.
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
- Shed rows navigate to the UI route `/vaccination/execution/sheds/[shedId]`.
- Park attention links go to `/action-center?park={park_id}`.

Missing before E2E:

- Operator execution controls are not present here. If execution belongs to field app, the admin UI must link/label that clearly and E2E must cover the field-app/API path.

### `/vaccination/execution/sheds/[shedId]`

Required:

- Read backend API `/vaccination/execution/sheds/{shed_id}`.
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

- ~~There is no protocol list endpoint wired to the Config table.~~ DONE
  (2026-06-26): `GET /protocols?category=…` (`listProtocolConfigs`) is wired to
  the Config table through the generated client; after save/publish the page
  shows real draft/published/retired rows with source-review state, not a blanket
  "No protocol rules yet." See B3 (CLOSED).
- DONE: the local/dev source-derived ET protocol can publish and generate
  obligations. The Config screen displays it through the real list endpoint; more
  PPR/FMD/HS/BQ are either `label-only closed` or later source-backed versioning,
  not a UI blocker.

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

- Accepted intake currently creates `procurement_pc_handoffs`, but the handoff does not itself prove vaccination obligations were generated.
- Build either a production-equivalent event path or a documented synchronous local app-service path from accept-intake to vaccination generation.

## Hard Blockers Before E2E

> Data-plane status (2026-06-26): B1/B2/B5 are CLOSED for the data plane. The
> assembled local chain — Herd Register create -> goat.created outbox -> eventbus
> relay -> generation -> sweeper batch/SOP task -> proofs + SOP submission ->
> completion -> verification accept -> CT/AC/PA/WF/Vaccination/shed/Passport read
> models — was captured in one run with concrete IDs and idempotent replay. See
> `docs/runbooks/vaccination-local-business-chain.md` and the repeatable
> `tools/dev/vaccination-chain-proof.sh`. The default entry path used was Herd
> Register `POST /admin/goats`; the accepted-intake variant remains an alternate
> entry (B1 note below). Remaining outside E2E is Google/prod provisioning and
> optional production roster expansion beyond the local/dev ET baseline.

### B1. Accepted Intake Trigger — generation path proven via Herd Register entry

Current observation:

- `AcceptIntake` updates goat location/state, inserts `procurement_pc_handoffs`,
  writes a `goat.created` identity event, writes audit, and inserts an outbox
  message.
- API bootstrap and the local outbox relay eventbus mode both register the
  `goat.created` generation handler.
- `procurement_pc_handoffs.event_status = emitted` is currently set when the
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

### B2. Local Outbox/Consumer/Sweeper — CLOSED (captured 2026-06-26)

Current observation:

- `outbox-relay` delivers to the in-process eventbus when
  `GOATOS_OUTBOX_PUBLISHER=eventbus|local|inprocess` (verified live: relay
  claimed/published the `goat.created` row -> generation created the obligation).
- The default empty/logging publisher is logging-only and is not an E2E delivery
  path (unchanged).
- `obligation-sweeper` was run live in the captured chain: it created the
  obligation batch + SOP task; a re-run reported `batches=0` (idempotent). The
  full fresh-goat -> relay -> obligation -> sweep -> SOP task run IS captured in
  `docs/runbooks/vaccination-local-business-chain.md`.

Build:

- Provide one documented command or script sequence for local Postgres, API, admin-web, outbox relay, consumer/event dispatcher, and sweeper.
- Relay must deliver to handlers, not only mark/log.
- Sweeper must create obligation batches and SOP tasks from due obligations.
- Tests must assert downstream read models changed.

### B7. Herd Register Write UI — CLOSED (2026-06-26)

Current observation:

- Backend and generated clients have create/bulk goat operations.
- `/counts/herd` `Register goat` and `Import sheet` are WIRED: they open real
  drawers posting `createAdminGoat` / `previewAdminGoatBulkImport` /
  `commitAdminGoatBulkImport` through the generated admin client
  (`features/counts/herd-actions.ts`, `herd-actions-ui.tsx`). Only `New report`
  stays disabled (no API). The data-plane chain proof now uses this create path
  via `POST /admin/goats`.
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

### B8. Supplier Warmup / Procurement Holding-Park Evidence — CLOSED (current scope)

Current trust rule: the old `HF` label means our supervised procurement holding
park only. Evidence is trusted only when our team administered or validated the
dose under SOP/video/physical validation in our park or procurement holding
park. Third-party/vendor/source claims outside that lifecycle remain notes and
must not suppress Preventive Care (PC) obligations.

Current observation:

- V1 procurement holding classification is supervised 4-5 week holding near the
  buying region. Older purpose-specific warmup duration notes are superseded;
  source purpose remains context, not a separate trust clock.
- Procurement holding-park vaccination evidence import/review endpoints exist
  (`POST /procurement/source-entry/goats/{goat_id}/hf-vaccination-evidence`,
  `.../hf-vaccination-evidence/{evidence_id}/review`), and trusted procurement
  holding-park evidence feeds the generation suppression path
  (`CompletionEvidenceReader.HasTrustedCompletionEvidence`, test
  `TestGoatCreatedTrustedHFEvidenceSuppressesMatchingObligation`).
- `/vaccination` shows a read-only Supplier warmup / Holding-Farm panel linking
  back to Source Entry for write actions.

See `context/frontend/supplier-warmup-vaccination-gaps.md`. Remaining limits
(search/advanced filters, media capture, real roster values) are documented there.

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
- The latest mock's business Audit Log and reusable table controls add real
  obligations for `/operations/audit` and every in-scope table: summary/anomaly
  cards, operation-family chips, operator/span filters, top/bottom pagination,
  active chips, `Clear all`, sorting/header behavior, row/entity-history clicks,
  and export state. Raw developer fields can power URL filters but must not be
  the primary dashboard UI.
- Audit Log completion is not proven by green lint/typecheck/build alone. It
  needs live visual smoke with populated local audit rows plus ledger screenshots
  showing the Admin / Data Ops IA, role-lens/`Viewing as` behavior, operation
  chips, activity trail, empty/error states, and disabled export.
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

### B3. Config Authority Screen Cannot Show Existing Protocol Rules — CLOSED (2026-06-26)

Closed in this pass:

- New tenant/category-scoped list endpoint `GET /protocols?category=…`
  (`operationId: listProtocolConfigs`, `protocol.read` permission) in app-api,
  backed by sqlc query `ListProtocolConfigsForCategory` →
  `protocol.ports.Repository.ListConfigs` → `protocol.app.Service.ListConfigs` →
  `protocol/adapters/http.Handler.ListConfigs`. Returns every version
  (draft/published/retired) of every definition in the category with rule-row
  count, source-review state (lifted from `rule_dsl.source`), linked SOP,
  effective window, and publisher/updated metadata. Bounded by a fixed
  `configListLimit` (protocol versions per tenant/category are inherently small).
- TS client regenerated (`ProtocolConfigListResponse` / `ProtocolConfigItem`).
- `/config` table wired to the real list through `lib/api/server.listProtocolConfigs`
  + `features/config/protocol-rules-page.tsx`. The hardcoded `rules: []` is gone;
  rows are real or the table shows the honest empty state. A failed read surfaces
  an error band (not a silent empty table).
- Publish stays gated: the row's status is `Published`, `Draft · source-backed`,
  `Draft · not source-backed`, or `Retired`, computed from the same source-backed
  rule the backend enforces (`protocol/app/publish.go` `ValidatePublishable`:
  source_system ∈ vaccinations_db/pc/vet + source_ref + review_status=approved +
  approved_by). Drafts that are not source-backed render as such; they cannot
  publish.
- Tests: `internal/protocol/adapters/http` `TestListConfigsReturnsItemsAndDefaultsCategory`
  (200, default category=vaccination, item shape, category passthrough).
- Live SSR proof on `127.0.0.1:3300/config?category=vaccination`: real rows
  rendered — `Demo Preventive Care (PC) Vaccination` (Draft · not source-backed) and
  `Enterotoxaemia K1 Primary` (Published, 1 rule, park scope, linked SOP) —
  no error band, no empty state. Screenshots:
  `.codex-goatos-render/admin-web-screenshots/config-b3/{desktop,narrow}-config.png`.

Source-derived local/dev content is no longer a blocker: the seeded
ET/K1/day-21 protocol is source-backed enough for local/dev and can publish and
generate obligations. The 2026-06-26 roster-expansion follow-up closed
PPR/FMD/HS/BQ as `label-only closed`; none became a source-backed schedule
version.

### B4. SOP/Proof Execution Loop Is Not Click-Complete

Current observation:

- Backend app task, proof upload, proof complete, and SOP submission APIs exist.
- Admin vaccination screens show SOP/proof/verify state but do not provide start SOP, proof upload, or submit answers controls.
- Verification queue can accept/reject/rework existing completions.

Decision (2026-06-26): the pre-E2E execution path is **direct generated API
seed**, not an admin-web operator console and not operator-mobile.

- Rationale: operator execution belongs to the field/operator app, which is out
  of the current admin-web slice; admin-web must NOT grow fake "start SOP / upload
  proof / submit answers" buttons (that would be a fake-action defect). The
  pre-E2E proof therefore drives the SOP/proof/verification loop through the
  existing generated app APIs (proof record/complete + SOP submission), then
  accepts/rejects through the existing verification-queue APIs.
- Admin-web stays honest: it READS SOP/proof/verify state and links to the work;
  it does not render execution-write controls. The verification queue
  accept/reject/rework actions it does expose are real generated-client actions.
- The repeatable command sequence for this path is documented in
  `docs/runbooks/vaccination-local-business-chain.md`.

Build / remaining:

- Whichever path is chosen must create proof through the backend proof API,
  submit SOP answers with proof refs, and produce the verification queue item.
  The per-segment behavior is test-proven (see B2 / the local-business-chain
  runbook); the remaining gap is one captured end-to-end local run.
- Admin-web must not show fake execution buttons. (Held: no execution-write
  controls exist on admin-web today.)

### B5. Seeded Scenario — CLOSED for the happy-path clean goat (2026-06-26)

The deterministic local scenario is captured for the clean goat: seeded
source-backed (test) protocol `b011` + published SOP `b0..0002` + vaccine
stock/lot `b002` + cold-chain/proof requirements + CBE park/shed. A clean goat
created via Herd Register reaches Preventive Care (PC) vaccination, execution context, Action
Center, Workflows, and Passport, and completes through verification. See
`tools/dev/vaccination-chain-proof.sh` and the runbook.

Still to add for the FULL four-goat negative matrix (not a data-plane blocker,
covered by tests for the exclusion rules):

- Four-goat fixture: clean accepted intake, rejected before truck, owner
  missing/unresolved, extra unknown arrival — asserting only the clean goat
  reaches Preventive Care (PC) work and the rest stay out, in one captured run. The exclusion
  behavior is unit/integration-tested; the assembled negative run is the
  remaining nicety.

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

Start E2E only when every item below is true (data-plane items DONE 2026-06-26 —
captured live, see `docs/runbooks/vaccination-local-business-chain.md`):

- Backend route smoke passes for all vaccination routes.
- Config can list the source-backed published protocol. (Endpoint DONE
  2026-06-26 — `GET /protocols?category=…`; satisfied for the seeded
  source-derived ET dev baseline.)
- SOP Library can list the published vaccination SOP. (Published SOP `b0..0002`
  exists and is linked from the trigger rule.)
- DONE: clean-goat entry generates a vaccination obligation (Herd Register
  create path proven; accepted-intake is the alternate entry).
- DONE: sweeper creates the obligation batch + SOP task.
- DONE: SOP/proof submission creates a recorded completion + verification-queue item.
- DONE: verification accept creates the completion (accepted) and completes the obligation.
- DONE: CT/AC/PA/WF/Vaccination/shed/Passport read the same updated Postgres state.
- Still required for E2E green: four-goat negative matrix in one run (clean only
  reaches Preventive Care (PC) work), and the full click-matrix coverage below.
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

After the gate is green, run the E2E plan in
`context/execution/procurement-vaccination-e2e-plan.md` and the broader
admin-web click/contract checklist in
`context/execution/admin-web-e2e-checklist.md`.
