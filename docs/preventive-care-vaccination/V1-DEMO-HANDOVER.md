# Vaccination V1 Demo Handover

Date: 2026-07-01
Branch: `vaccination-v1-close`
Worktree: local GoatOS checkout

## Status

V1 vaccination is demo-ready for the local Preventive Care (PC) vaccination slice.

Fresh evidence:

- Full E2E smoke: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- SOP/Config authoring smoke: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- Click/interlink matrix: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- Config page UX follow-up full E2E smoke: `CONFIG-PAGE-V2-20260701`
- Config page authoring smoke: `AUTHORING-2026-07-01T18-35-03-998Z`
- Config page click/interlink matrix: `CONFIG-PAGE-V2-20260701-R2`
- Source nuance rules: `docs/preventive-care-vaccination/source-nuances-rules.md`
- Goats and Parks tracked source extract: `context/source-findings/goats-and-parks-source-extract.md`
- Nuance kernel/config package proof: `go test ./internal/adminui/app ./internal/protocol/app ./internal/vaccination/app -count=1`
- Older-goat anti-flood tests: `go test ./internal/vaccination/app -run 'TestOlderGoat' -count=1`
- Calendar drive-collapse test: `go test ./internal/calendar/adapters/postgres -run 'TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive' -count=1`
- Chain proof: `vaccination-chain-proof stamp=1782915162`
- Trusted-history existing-vaccination proof: `vaccination-trusted-history-proof stamp=1782917268`
- Rework proof: `vaccination-rework-proof stamp=1782899439`
- Visual screenshots: `.codex-goatos-render/admin-web-screenshots/2026-07-01T14-12-57-572Z`
- Rework Goat Passport screenshot: `.codex-goatos-render/admin-web-screenshots/2026-07-01T09-04-55-269Z/desktop-goat-passport-rework-proof.png`

What is closed for the V1 demo:

1. SOP builder creates, validates, dry-runs, publishes, reopens, edits, and republishes a vaccination SOP with all supported question types.
2. Config creates the source-backed Nuance Rules vaccination matrix, previews impact, saves draft, publishes ET+TT, PPR, Goat Pox, FMD, and HS as separate protocol versions, and reopens the published drawer.
3. Config captures the source nuance policy: vaccine type/pathogen class, course type, source schedule, dose, vial, revaccination, breed/stage/sex rows, live/killed gap metadata, same-day compatibility metadata, procurement warm-up, adult/source-vaccination policy, pregnancy skip/post-delivery policy, and clinical defer states.
4. The V1 kernel enforces V1-local nuance gates proven in tests: clinical holds, pregnancy/reproductive exclusion when generation or backfill reads current goat state, procurement warm-up due offset, older-goat catch-up anti-flood, trusted-history next-dose advancement, Nuance schedule due dates, live-live source spacing, and drive-first Calendar aggregation after batching.
5. Goat creation/import/intake data paths generate vaccination obligations through the kernel chain.
6. Sweeper groups due work into shed drive/SOP task work.
7. Proof upload, accepted verification, completion, booster generation, and replay idempotency are proven.
8. Rejected proof -> rework -> corrected resubmission -> accepted verification is proven in the data plane.
9. Goat Passport shows open due rows, rejected and accepted history, and links back to Workflow / Action Center using the real workflow row key.
10. Current visible V1 click paths pass for shell/sidebar, Vaccination, Action Center, Protocol Adherence, Workflows, Config, and SOP Library.
11. Desktop/narrow visual smoke passes across the active routes.
12. Older goats with unknown or untrusted history do not flood Calendar / Action Center with every missed historical dose; the generator creates one safe catch-up/review action first, while trusted first-dose evidence advances to the next missing dose. The live trusted-history smoke seeds an accepted/verified ET+TT 4-week history row, suppresses that old dose, and generates/batches only the next ET+TT 7-week obligation.
13. Calendar is drive-first after V1 batching: before batching it can project per-goat due work, but once due rows are attached to a shed drive batch, active Calendar suppresses the batched per-goat `vaccination_dose_due` rows and shows one `vaccination_drive` item with the goat count. Per-goat rows remain detail data for Passport / Protocol Adherence / Vaccination drilldowns.

Outside this V1 demo contract:

- Later route/resource optimizer: cross-shed route planning, worker/stock/cold-chain assignment, smarter wait/run decisions, multi-day plans, small-shed fairness, and replan scoring.
- Million-goat load/permutation proof beyond the focused V1 source-matrix fixtures.
- Business-facing user/shed administration UI; local proof uses seeded grant/local shed setup plus real goat/config/SOP/proof flows.
- Full browser-driven field-worker rework walkthrough; reject -> rework -> resubmit -> accept is proven through backend/API and Passport UI evidence.

## URL And Login

- Admin web: `http://127.0.0.1:3300`
- API: `http://127.0.0.1:8080`
- Login route: `http://127.0.0.1:3300/login`
- Tenant: `00000000-0000-4000-8000-000000000001`
- Local user: `90000000-0000-4000-8000-000000000101`
- Role shown: `Superadmin / CEO / COO`

## Demo Order

Use this path:

1. `Config`
   - Show source-backed vaccination protocol rules and published status.
2. `SOP Library`
   - Show vaccination SOP policy, then the builder if you want to show authoring.
3. `Counts -> Herd Register` or the created goat path
   - Explain that canonical goats trigger vaccination generation.
4. `Vaccination`
   - Show matrix/status, generated work, cohort/detail drawers, and shed execution.
5. `Action Center`
   - Show actionable proof/verification work.
6. `Protocol Adherence`
   - Show expected vs actual and gap state.
7. `Workflows`
   - Show config -> obligation -> drive -> SOP -> proof -> verify -> completion.
8. `Goat Passport`
   - Show vaccination open due rows and history on the same goat.
9. `Control Tower`
   - Show summary of current gaps/process state.

## Proof Artifacts

Full smoke:

- `.codex-goatos-render/e2e-smoke/NUANCE-RULES-20260701-V1-MATRIX-R2`
- `.codex-goatos-render/e2e-smoke/CONFIG-PAGE-V2-20260701`
- `vaccination-chain-proof.log`
- `procurement-vaccination-e2e-matrix.log`
- `open-vaccination-owner-chain.log`
- `open-visual-goat.log`
- `smoke-visual-live.log`
- `summary.env`

Authoring:

- `.codex-goatos-render/vaccination-authoring/NUANCE-RULES-20260701-V1-MATRIX-R2/authoring.md`
- `.codex-goatos-render/vaccination-authoring/AUTHORING-2026-07-01T18-35-03-998Z/authoring.md`

Click matrix:

- `.codex-goatos-render/vaccination-click-matrix/NUANCE-RULES-20260701-V1-MATRIX-R2/matrix.md`
- `.codex-goatos-render/vaccination-click-matrix/CONFIG-PAGE-V2-20260701-R2/matrix.md`

Visual:

- `.codex-goatos-render/admin-web-screenshots/2026-07-01T14-12-57-572Z`
- `.codex-goatos-render/admin-web-screenshots/2026-07-01T18-35-52-753Z`
- `.codex-goatos-render/config-page/config-authoring-desktop.png`
- `.codex-goatos-render/config-page/config-authoring-mobile-after.png`
- `.codex-goatos-render/admin-web-screenshots/2026-07-01T09-04-55-269Z/desktop-goat-passport-rework-proof.png`

Trusted-history existing-vaccination proof IDs from `vaccination-trusted-history-proof stamp=1782917268`:

- Goat: `35832307-c6ce-4624-b3ca-7bd05bf4d07d`
- Accepted 4-week history obligation: `349b8a98-2ec3-457d-b0d1-b8aad2288708`
- Accepted 4-week completion: `743d1e25-6c76-4624-b76d-f7f102ccf2ab`
- Next 7-week obligation: `6b13edf0-feca-4513-a5af-7605110b716a`
- Shed-drive batch: `3f17e9b6-1809-4693-87e8-4edae0306050`
- SOP task: `94277d65-1116-4d78-b4e9-8d816c264b2b`
- Nuance ET+TT protocol version: `ce809b51-4262-47cb-bc88-370adff3aea0`
- Surface proof: Calendar `shed_drive_batch=HIT`, Calendar per-goat old 4-week `MISS`; Action Center next obligation `HIT`, old 4-week `MISS`; Workflow task `HIT`; Passport history and next obligation `HIT`; Control Tower and Protocol Adherence summaries `HIT`.

Fresh chain IDs from `vaccination-chain-proof stamp=1782915162`:

- Goat: `5e5dbff7-493a-4916-b586-09c18e8eebdc`
- Primary obligation: `95b0df2f-6e4e-4ee7-81a0-b5ccc75182c2`
- Booster obligation: `e5b73456-07cf-46fb-8d4f-07ee45899a1e`
- Batch: `1c2b68f7-bf5d-44c8-8633-686aa140a889`
- SOP task: `2e773b09-0c60-44b1-b59f-4a3655c1561a`
- Submission: `05501f8c-4ec4-45d4-a1ee-ef5b2b6a04fd`
- Completion: `1f6055d1-034f-47f4-be16-c76311a51c19`

Rework proof IDs:

- Goat: `8f77b2a9-e969-4313-96b9-b6a8a5c30482`
- Obligation: `06eb74a3-d44d-48e7-bb8f-8a025e071b11`
- Booster obligation: `912b95a0-33f2-426a-92f2-e6ca2df20eed`
- Batch: `13658fc0-6c6f-4620-bc75-ac2213b31e7a`
- Task: `9c52ebc9-c631-47c6-8156-131551f76ef2`
- Rejected submission: `08f3e736-cd89-4d3a-80e9-e1ace09c198e`
- Rejected completion: `2e3b57ce-fa73-4b12-985e-015dba4d2966`
- Corrected submission: `9f5ebba1-e129-478b-a832-d74fe46acf94`
- Accepted completion: `7d7fa44e-84aa-417a-b669-38df6b05d39b`
- Passport workflow row: `batch:13658fc0-6c6f-4620-bc75-ac2213b31e7a:rule:00000000-0000-4000-8000-00000000b052:shed:131bb187-a514-42aa-89e3-284eb0bd215a`

## Commands

```bash
bash tools/dev/run-local-stack-supervised.sh
```

```bash
GOATOS_E2E_RUN_ID=NUANCE-RULES-20260701-V1-MATRIX-R2 bash tools/dev/admin-web-e2e-smoke.sh
```

```bash
GOATOS_AUTHORING_RUN_ID=NUANCE-RULES-20260701-V1-MATRIX-R2 npm --prefix apps/admin-web run smoke:vaccination-authoring:live
```

```bash
GOATOS_CLICK_MATRIX_RUN_ID=NUANCE-RULES-20260701-V1-MATRIX-R2 npm --prefix apps/admin-web run smoke:vaccination-click-matrix:live
```

```bash
./tools/dev/vaccination-rework-proof.sh
```

```bash
./tools/dev/vaccination-trusted-history-proof.sh
```

```bash
go test ./internal/vaccination/app ./cmd/generate-vaccination-obligations ./internal/passport/app ./internal/obligation/app
```

```bash
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck -- --pretty false
npm --prefix apps/admin-web run check:mock-fidelity
```

## Demo Script

Say:

> V1 is demo-ready for the Preventive Care (PC) vaccination workflow. Source-backed SOP policy and multi-row vaccination config capture the nuance rules for breed, stage, sex, vaccine class, course type, dose, vial size, revaccination, source schedule, live/killed spacing, clinical defer, procurement warm-up, and pregnancy policy; the kernel generates safe due work, the sweeper groups it into shed execution, the operator submits proof, verification accepts or rejects it, completion updates the goat passport, and replay does not duplicate work.

Also say:

> Calendar is V1-safe for the demo: after the sweeper batches goat due rows into a shed drive, Calendar shows the drive item, not 100 duplicate goat-dose rows. Goat-level due status stays in Passport, Protocol Adherence, and Vaccination detail.

Also say:

> This handover is the V1 Preventive Care (PC) vaccination demo contract. V1 captures the Nuance Rules matrix and proves the local rule gates, including source compatibility spacing. Route/resource optimization and million-goat load/permutation work are separate production programs, not hidden claims inside this demo.
