# Admin Web Vaccination Slice Mock Fidelity Ledger

Date: 2026-06-25

Mock source: `/Users/ravi/mesha/goatos/mock/goatos-dashboard-mock.html`

Live screenshot set:
`/Users/ravi/mesha/goatos/.codex-goatos-render/admin-web-screenshots/2026-06-25T15-18-51-155Z`

Live proof command:

```bash
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3311 \
GOATOS_API_BASE_URL=http://127.0.0.1:8080 \
GOATOS_BEARER_TOKEN=<fresh local token> \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
npm --prefix apps/admin-web run smoke:visual:live
```

Important distinction: `smoke:visual:live` is not a full mock-fidelity claim.
This ledger is the mock comparison record for the approved vaccination slice and
the screens needed to run it. The smoke script now also clicks the core Herd and
Vaccination overlays so dead modal/drawer controls cannot pass unnoticed.

## In-Scope Surfaces

- Shell, top bar, and approved left nav on every built screen.
- `/vaccination` including status matrix, cohort detail, and execution board.
- `/action-center`, `/protocol-adherence`, and `/workflows` as top-level command
  screens used by vaccination operations.
- `/procurement/source-entry` and `/procurement/source-entry/loads/:id` for
  supervised procurement holding-park vaccination evidence before arrival.
- `/counts/herd` and contextual `/goats/:id` drilldown because vaccination
  cohorts and source-entry rows depend on goat identity, shed, park, and health
  state.
- `/config?category=vaccination`, `/sops`, and `/operations/audit` as supporting
  Admin/Data Ops surfaces.

Out of current scope by approved product boundary: mock-only global Calendar,
Insights, global Goat Passport search, generic Health, Breeding, Parks,
Inventory, HR/People, and Farmer Network nav. These should not be copied into
the live sidebar until the product scope is explicitly widened.

## Screen Ledger

| Surface | Mock path / screenshot | Live screenshot | Visible comparison | Classification | Files changed / reason |
| --- | --- | --- | --- | --- | --- |
| Shell, top bar, sidebar | `mock/goatos-dashboard-mock.html`; user screenshots 2026-06-25 18:25-18:51 | `desktop-counts-herd.png`, `desktop-vaccination.png`, `desktop-procurement-source-entry.png` | Top bar now uses Park-wise, `CBE · all sheds`, Last 30 days, data date, freshness, same-height controls. Sidebar is mock-styled but intentionally limited to approved screens. Group headers expand/collapse by click and keyboard. | fix now + intentional scope limit | `apps/admin-web/components/mesha-shell.tsx`, `apps/admin-web/app/mesha-theme.css`; future mock modules intentionally omitted. |
| Herd Register `/counts/herd` | Mock Herd table in `mock/goatos-dashboard-mock.html`; user herd screenshots | `desktop-counts-herd.png` | Table toolbar now has search rows, Filters, row count, and `10 / page`. Columns now match mock: Goat ID, Park, Shed, Breed, Sex, WT, Lifecycle, Health, Breeding. Goat ID is the contextual passport drilldown; no extra Passport action column. Filters, Register goat, and Import sheet all open/close real overlays. | fix now | `apps/admin-web/features/counts/herd-register.tsx`, `apps/admin-web/features/counts/herd-filters-modal-client.tsx`, `apps/admin-web/features/counts/herd-actions-ui.tsx`; backend contract below. |
| Herd backend contract | Mock needs separate Park, Shed, WT | Verified through `/goats/search` rendered rows | API no longer forces the UI to guess from one combined location string. It returns park/shed labels and nullable weight. | fix now | `backend/internal/identity/domain/types.go`, `backend/internal/identity/adapters/postgres/repository.go`, `contracts/openapi/app-api.yaml`, `contracts/openapi/admin-api.yaml`, `packages/api-client/src/generated/*`. |
| Herd KPI cards | Mock has scoped herd context | `desktop-counts-herd.png` | Header now shows scoped context; KPI totals remain honest `--` because Counts aggregate read-model is not in the generated client. | backend-blocked but visible-honest | No fake totals added. Needs Counts aggregate read model before showing Active/Adults/Kids/Untagged totals. |
| Vaccination operation `/vaccination` | Mock vaccination first screen | `desktop-vaccination.png` | Target, Group, Route, Execute band, supplier warmup / Holding-Farm context, status matrix, per-cohort detail, and PPR drive shed events are present in mock layout style. SOP, Import sheet, New drive, and all four Filters controls open/close real overlays. | fix now | `apps/admin-web/features/preventive-care-vaccination/operations.tsx`, `supplier-warmup-context.tsx`, `vaccination-action-dialogs.tsx`, `vaccination-filter-modal.tsx`, `status-matrix.tsx`, `cohort-detail.tsx`, `apps/admin-web/features/vaccination-execution/execution-board.tsx`. |
| Vaccination supplier warmup context | Mock Supplier warmup / Holding Farm table | `desktop-vaccination.png` | `/vaccination` now shows the pre-arrival procurement holding-park evidence panel before the matrix: Load, Holding farm / supplier, Purpose, Animals, Warmup, Tagging, Vaccination at HF, Health / Selection, Status. `HF` is a legacy UI/table label meaning our supervised procurement holding park only; outside-source claims do not suppress PC work. It is read-only in Preventive Care (PC) and links to Source Entry for writes. | fix now + ownership boundary | `apps/admin-web/features/preventive-care-vaccination/supplier-warmup-context.tsx`; Source Entry owns writes per `context/frontend/current-admin-web-scope.md`. |
| Vaccination clicks | Mock cells and shed rows open operational action | Captured by routes `/vaccination`, `/vaccination#execution`, `/action-center` | Matrix/detail/status links route to Action Center with scope preserved instead of being dead UI. Search controls stay visibly disabled where backend contracts are missing; filter buttons now open a modal with the backend-gap reason and real Action Center link. | fix now + backend-blocked where noted | Same frontend files; disabled reasons shown rather than fake filtering. |
| Vaccination data | Mock shows named cohorts and clean statuses | `desktop-vaccination.png` | Live uses real obligations from local backend. Current local rows show generated cohorts/sheds and `owner_missing` where source data lacks owner assignment. | backend/data gap, not faked | No fake K3/F2 rows added. Needs assignment/owner data and dose-specific protocol catalog expansion to match final business data. |
| Action Center `/action-center` | Mock command/action panels | `desktop-action-center.png` | Uses the same mock shell/topbar/sidebar and shows real vaccination work items surfaced from backend state. | fix now | `apps/admin-web/components/mesha-shell.tsx`; existing action-center surface retained as top-level command screen. |
| Protocol Adherence `/protocol-adherence` | Mock authority/protocol screen style | `desktop-protocol-adherence.png` | Same shell/topbar/sidebar; screen remains top-level and vaccination-scope aligned. | fix now | Shell/theme changes apply across this route; no nested vaccination route created. |
| Workflows `/workflows` | Mock workflow command screen style | `desktop-workflows.png` | Same shell/topbar/sidebar; route remains a top-level command lens. | fix now | Shell/theme changes apply across this route; no out-of-scope module nav added. |
| Source Entry `/procurement/source-entry` | Mock Supplier warmup/Holding Farm section | `desktop-procurement-source-entry.png` | Rows are sourced from real procurement loads. Columns match the mock intent: Load, Holding farm / supplier, Purpose, Animals, Warmup, Tagging, Vaccination at HF, Health/Selection, Status. | fix now | `apps/admin-web/features/procurement/source-entry-board.tsx`; backend load display contract below. |
| Source Entry backend contract | Mock needs supplier, holding farm, purpose, procurement holding-park vaccination evidence | Board and load detail render from API | Load responses include source party and source location display labels; evidence import/review endpoints and permissions are wired. Trust requires our supervised procurement holding park under SOP/video/physical validation. | fix now | `backend/internal/procurement/domain/types.go`, `backend/internal/procurement/adapters/postgres/repository.go`, `backend/internal/permissions/routes.go`, `backend/internal/permissions/permissions_test.go`, `backend/migrations/postgres/000085_procurement_hf_vaccination_evidence.sql`, `contracts/openapi/admin-api.yaml`, generated admin client. |
| Source Entry data | Mock shows supplier-specific holding farms and vaccine names like PPR / Enterotox | `desktop-procurement-source-entry.png` | Local proof data has four real loads and one trusted procurement holding-park evidence row. Holding-farm label is whatever local source-location data contains; current local seed has `Trigger Gate Farm` rather than mock supplier-specific HF names. | data gap, not faked | No cosmetic seed labels invented in UI. Needs source-location seed/master data cleanup for final labels. |
| Load Detail `/procurement/source-entry/loads/:id` | Mock right-side source/load action drawer and evidence actions | `desktop-procurement-load-detail.png` | Detail page no longer crashes on nullable backend arrays. HF evidence import/review forms are real, labeled, and submit to backend actions. | fix now | `apps/admin-web/features/procurement/load-detail.tsx`, `load-forms.tsx`, `actions.ts`, `apps/admin-web/lib/api/procurement*.ts`. |
| Goat Passport `/goats/:id` | Mock contextual passport side panel | `desktop-goat-passport.png` | Contextual passport drilldown opens from table/goat links and keeps mock drawer styling. No global Goat Passport search was added because current scope forbids it. | fix now + intentional scope limit | Herd table links changed; shell nav keeps Goat Passport out of global nav. |
| Config `/config?category=vaccination` | Mock Admin/Data Ops support surface | `config-b3/desktop-config.png`, `config-b3/narrow-config.png` (2026-06-26) | Protocol-rules table now lists REAL backend versions via `GET /protocols?category=…` (B3): local/dev render shows `Demo Preventive Care (PC) Vaccination` (Draft · not source-backed) and `Enterotoxaemia K1 Primary` (Published, 1 rule, park scope, linked SOP) after the source-derived seed is refreshed. Header count, status chips, scope tag, effective date, linked SOP, rule count are backend truth; amber left-border on non-published rows. No error band, no empty state. Empty state and a failed-read error band are both wired. | fix now (real-read) | `apps/admin-web/features/config/protocol-rules-page.tsx` (async, maps `ProtocolConfigItem`→`ConfigRuleRow`, error band), `apps/admin-web/lib/api/server.ts` (`listProtocolConfigs`); backend `internal/protocol` list endpoint + app-api contract + regenerated client. Publish stays gated; no fake rows. |
| SOP Library `/sops` | Mock SOP support surface | `desktop-sops.png` | Route remains vaccination-scope aligned; not broadened into generic SOP library. | intentional scope limit | Existing route preserved; visible scope stays vaccination-only. |
| Audit Log `/operations/audit` | Mock Admin/Data Ops audit surface | `desktop-operations-audit.png` | Audit Log appears under Admin/Data Ops, not as a random hidden route; shared shell matches. | fix now | `apps/admin-web/components/mesha-shell.tsx`, `apps/admin-web/features/operations-audit/audit-log.tsx`. |

## Backend/Data Truths Not Covered Up

- Counts KPI totals need a Counts aggregate read model before showing mock-like
  business totals.
- Some herd rows still show blank health, breeding, or WT values when the real
  source events do not contain those fields.
- The local vaccination protocol catalog has the source-derived ET/K1/day-21
  rule set; additional dose-specific labels such as PPR/FMD/HS/BQ should come
  from protocol seed/catalog work, not hardcoded UI text.
- Vaccination rows that show owner or assignment gaps are exposing real missing
  local assignment data.
- Source-entry holding-farm names depend on source-location master data; the UI
  now renders the backend value instead of inventing mock labels.

## Validation

Green in this pass:

- `npm --prefix packages/api-client run generate`
- `npm --prefix apps/admin-web run typecheck`
- `npm --prefix apps/admin-web run lint`
- `npm --prefix apps/admin-web run build`
- `go test ./internal/procurement/... ./internal/identity/... ./internal/vaccination/... ./internal/obligation/...`
- `go test ./internal/procurement/... ./internal/permissions`
- `GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3311 npm --prefix apps/admin-web run smoke:visual:live`

Interaction proof inside `smoke:visual:live`:

- `/counts/herd`: opens/closes Filters, Register goat, Import sheet.
- `/vaccination`: opens/closes SOP, Import sheet, New drive, supplier-warmup
  Filters, status-matrix Filters, per-cohort Filters, and shed-event Filters.
- Close buttons are checked as topmost before click, so top-bar/sidebar overlays
  cannot silently intercept modal clicks again.

Final-check commands in this pass:

- `npm --prefix apps/admin-web run check:mock-fidelity`
- `git diff --check`

---

## 2026-06-26 — Record / verify drawer pass (all vaccination clickable items)

Mock source: `mock/goatos-dashboard-mock.html` → `vaccRecordModal()` (line ~2915)
and the `actModal` field engine (line ~3199): `.fld`+label, `select`, `.chipset`
> `.chip.on`, `.videobox` > `.thumb`.

Defect fixed: every clickable vaccination item (status-matrix cell, per-cohort
detail row, shed-event execution row) opened a flat read-only `.metagrid` drawer
that punted to the Action Center — a plainer substitute, not the mock's
record/verify FORM. Now all three open the same mock-shaped Record / verify
drawer.

New shared component: `features/preventive-care-vaccination/record-verify-drawer.tsx`
(`VaccinationRecordVerifyDrawer` + exported `VaccinationRecordFormFields`).
Consumers: `status-matrix.tsx`, `cohort-detail.tsx`,
`features/vaccination-execution/execution-board.tsx` (shed-event drawer).

Mock anatomy ported in the drawer body:

- `.dh` header: `fic` syringe + `mt` "VACCINE" + h2 "Record / verify vaccination".
- `.dc`: sub `note` → RECORD `.metagrid` (real cohort/protocol context: cohort·shed,
  vaccine, animals, age band, last dose, next due, status tag, park) → live
  obligation/completion `counts` chips → the mock vaccine FORM (`.fld`):
  Cohort/shed, Vaccine (`select`), Batch (FEFO), Dose & route/site, Cold-chain
  (`.chipset` Yes/No), Adverse reaction (None/Mild/Severe), Capture proof
  (`.videobox`).
- `.df` footer: Record + verify (primary) / Open Action Center / Protocol
  Adherence / Cancel (wraps, no clip).

Backend-backed vs disabled-with-reason:

- Real backend data: cohort × protocol context and all `counts`
  (overdue/due/in-progress/proof-pending/accepted/rejected) from
  `GET /vaccination/operations`; the shed-event drawer's shed/owner/status from
  `GET /vaccination/execution`.
- DISABLED with honest reason (intentional divergence): the entire record FORM
  (vaccine/batch/cold-chain/dose/route/adverse/proof). Reason: a vaccination dose
  is RECORDED by the shed operator through the SOP task + proof upload
  (`submitAppTask` / `createProofUpload`, TaskExecute) — admin-web exposes no
  record route. The cohort × protocol cell is a status rollup with no single
  `completion_id`, `sop_task_id`, or `sop_task_row_version`, so SOP task
  verify/rework (which needs the per-goat verification queue's review handle)
  cannot be fired from this rollup. The drawer routes verification to the Action Center
  where the real obligation/completion rows live. No fake submit, no fake rows,
  no client-only mutation.

Cleanup: deleted dead `components/dialog-modal.tsx` (0 importers, carried the
banned old-admin `#0b0f15` / `#293241` cyan/slate palette). Action dialogs
(`vaccination-action-dialogs.tsx` New drive / Import) re-anatomied from a
hand-rolled `.hd` inline-styled panel to the mock `.veil` + `.drawer.on` +
`.dh`/`.dc` shell.

Live proof (this session, against the running `:3300` dev server via browser):

- status-matrix cell → drawer opens, real data (Unknown · Gandhi 1 / Trigger Gate
  Preventive Care (PC) Vaccination / Animals 4 / overdue · 4 / next due 2026-06-25), 7 form fields,
  4 disabled inputs, Record+verify disabled.
- cohort-detail row → same drawer, vaccine "All cohort protocols" (rollup).
- shed-event row → same form under RECORD context (Shed event/Shed/Owner→assist/
  Stock FEFO/Status), footer Record+verify (disabled)/Open Action Center/Shed
  detail/Close.

Gates this pass: `typecheck` ✓, `lint` ✓, `check:mock-fidelity` ✓ (IA guard +
old-admin palette scan), no hardcoded hex leaks in `features/preventive-care-vaccination`
or `features/vaccination-execution`. `next build` deliberately NOT run — another
session's `next dev` owns `.next`; a concurrent build would corrupt it.

Remaining intentional disabled-with-reason controls (report-out): the record
FORM sub-controls above, on all three drawers, pending an admin-side record route
or a completion-id-bearing cell contract.

### Maintainer Correction After Drawer Review

The disabled-form conclusion above is not sufficient closure for actionable
vaccination rows. Treat it as a visual-fidelity milestone only.

- Rollup cells/cohort rows may stay disabled-with-reason when they truly have no
  single `completion_id`, `task_id`, or `obligation_id`.
- Shed/execution rows, Action Center rows, and verification-queue rows must be
  wired where those IDs exist, or the read model must be enriched/routed to the
  actionable row that has them.
- Admin-web proof media is in scope as web file upload. Do not build browser
  camera capture here; native camera capture belongs to mobile/field app.
- `Upload / capture proof` must become a real file upload control where a real
  proof scope/task exists, using `createProofUpload`, `PUT` to the returned
  `upload_url` (local dev maps to `uploadProofLocal`), and
  `completeProofUpload`. Do not leave a dead videobox.
- `/vaccination` execution table still needs mock table closure: toolbar,
  filters/search, row count, sort affordance, pagination/cursor footer where
  capped, compact density, aligned tags, hover/active states, and corrected
  action labels.

Current implementation follow-up is tracked in
`context/execution/vaccination-ui-proof-upload-current-handoff.md`.

### 2026-06-26 15:23 IST — Current P0 Render Blocker

After the drawer pass, Claude restarted `:3300` and tested a clean tab. The
click issue remained. Current observed state:

- shell/root app tree is still `display:none` after clean restart
- body has multiple app/root trees
- one skeleton route/loading tree is `.screen.card.pad` at `opacity:0`
- the real app tree containing sidebar + matrix is hidden
- clickable cells/links can measure zero-size or detached

This means the previous "real click works" claim is not accepted as closure.
Before continuing with pagination/filter/proof-upload work, fix the shell/render
state so a clean browser tab has one visible app tree and real pointer clicks on
matrix cells, cohort rows, and shed/execution rows open the intended drawer.

Source cross-check for the follow-up:

- Preventive Care (PC) PRD requires scan/administer/record dose/upload shed + vial video/verify
  quantity; verifier reviews proof.
- TRD maps shed drives to `obligation_batches` + one `sop_task`, and completions
  carry lot, dose, route/site, cold-chain, adverse reaction, and idempotency.
- Admin-web proof media is web file upload only; camera-native capture belongs
  to mobile/field app.
- The current dense mock vaccine grid is sample data. Do not fake extra
  protocols/cohorts/dates to match it. Fix UI controls/density/action wiring,
  and leave source roster/data gaps documented honestly.

---

## 2026-06-26 (pass 2) — render-state resolved + proof=file-upload + pagers

**Click / render closure (resolves the "not accepted" note above).** The
`display:none` shell + multiple body trees reproduce ONLY in the MCP Chrome
extension browser, which never commits the React Server Component payload and so
stays stuck on the Next `loading.tsx` Suspense fallback (`.screen.card.pad`,
`opacity:0`) with the real tree hidden — zero console errors. A real browser does
not hit this: `npm run smoke:visual:live` (host Playwright, fresh HS256 token)
rendered ALL 14 routes with ONE visible app tree, and the workspace owner
confirmed the drawer opens on click. Proof set:
`.codex-goatos-render/admin-web-screenshots/2026-06-26T10-07-43-314Z/`
(`desktop-vaccination.png`, `desktop-vaccination-execution.png`, + narrow). The
earlier per-tab `display:none` reads were an extension-browser RSC-streaming
artifact, not an app shell/hydration bug.

**Proof control = file upload (no camera).** record-verify-drawer.tsx now renders
a real `<input type="file" accept="video/*,image/*" multiple>` (was a camera-styled
`.videobox`). Label "Proof — shed + vial video (file upload)" per the SOP
`proof_policy`. Disabled with the exact reason below (no `task_id` to attach to).

**Pagers.** Mock `.pager2` footer (new `features/preventive-care-vaccination/table-pager.tsx`)
on the status matrix, per-cohort detail, and per-park shed-events tables — "N …
· all in-scope shown" + disabled Previous/Next, because the read-models return the
full in-scope set in one response (no cursor/total; same million-goat COUNT(*)
rule as herd). Execution `.tbar` search + Filters already present.

**EXACT disabled-reasons (contract gap — not fakeable):**
- Record / proof upload needs `task_id` → `POST /app/tasks/{task_id}/submissions`
  + `/app/proofs/*`. No /vaccination read-model returns it: `VaccinationExecutionRow`
  has only `driveId` + status enums; the operations cell has only `counts`.
- Verify/rework needs `completion_id`, `sop_task_id`, and `sop_task_row_version`,
  exposed only by `/vaccination/verification-queue` / Action Center per-goat
  rows, never on the cohort rollup / execution row.

**Backend follow-up to make in-drawer proof-upload + accept/reject WORK** (only
honest path): extend `/vaccination/execution` (and/or the operations cell) to
return the obligation `task_id` + `completion_id`(s), then wrap `/app/proofs/*`,
`/app/tasks/{id}/submissions`, and accept/reject in a server action. Until then
write controls stay mock-shaped + disabled-with-reason; working actions are the
footer route-out Links.

---

## 2026-06-26 (pass 3) — backend-driven matrix columns + id contract + close-out

CLOSED (proven):
- **Vaccine columns from backend/config, not hardcoded UI.** Earlier pass seeded
  multi-protocol test catalog rows via the real config API
  (define→version+rules→publish), then generated obligations via the real
  `GenerationService`
  (`backend/cmd/generate-vaccination-obligations` — new CLI mirroring
  obligation-sweeper; there was no committed GenerateForVersion trigger) + swept.
  Proof: `GET /vaccination/operations` → `protocols.length == 6`, 6 cells/cohort,
  all real `obligation_instances`. Root cause of earlier 1-column: publish ≠
  generate (generation wired only to `goat.created`); `birth_age` skips null-DOB
  goats → used `post_arrival`.
- **Execution id contract.** `/vaccination/execution` rows now expose nullable
  `obligationId`/`batchId`/`sopTaskId`/`completionId` (OpenAPI + read-model SQL +
  regen client). `lib/api/server.ts` got `createProofUpload`/`uploadProofLocal`/
  `completeProofUpload`/`submitAppTask` wrappers (file upload only).
- **Drawer actions wired** (`features/vaccination-execution/shed-event-actions.tsx`
  + `lib/api/vaccination-actions.ts`): proof file-upload (no camera) enabled only
  when `sopTaskId`+`obligationId` non-null; accept/reject enabled only when
  `completionId` non-null; else disabled-with-exact-reason. Cohort/matrix rollups
  stay fully disabled (no single id).
- Pager wording honest ("N returned rows · no cursor exposed"); fidelity gates
  green; `backend go build ./...` ✓; `go test ./internal/vaccinationexecution/...` ✓
  (incl. postgres integration); admin-web lint ✓; check:mock-fidelity ✓.

BLOCKED / NOT exercised:
- **Enabled proof/verify is not exercisable on the current fixture** — every
  execution row has null `sopTaskId`/`completionId` (per-shed-drive rollup; no
  drive advanced, no dose recorded). Path is built + typechecks; deferred (no
  drive-advance / no verification-queue work this thread, per owner).
- **typecheck + `next build` RED** — 27 errors, ALL procurement-contract. A
  contract agent `git restore`d a parallel procurement session's UNCOMMITTED
  `app-api.yaml` HF-vaccination-evidence schemas and regenerated the shared
  `packages/api-client/src/generated/app-api.ts` over its dirty copy. Recovery
  (owner-approved): the procurement session re-applies its OpenAPI changes +
  regenerates the client, preserving the vaccination execution-id fields already
  in `app-api.yaml`. No vaccination file has any typecheck error.
- `smoke:visual:live` halts early on a pre-existing **control-tower** a11y
  color-contrast violation (`.ct`/`.gct`) — unrelated to vaccination; matrix
  column proof is via the operations API instead.

Artifacts: removed the 12 MB `backend/generate-vaccination-obligations` binary;
gitignored it + `.goatos-local-media/` (backend/.gitignore). Kept the CLI source.

COMMIT STATUS: NOT committed. The shared contract (app-api.yaml + generated
client) is tangled (vaccination ids present, procurement schemas removed by the
collision); committing it as-is would break procurement. Commit the vaccination
scope only AFTER the procurement session restores its schemas + regen makes
typecheck/build green. Group C (procurement) is the other session's to commit.
