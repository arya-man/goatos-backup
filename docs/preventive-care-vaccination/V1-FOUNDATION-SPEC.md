# Vaccination v1 — Matrix + Due Engine Spec (locked)

**Date:** 2026-06-24 · **Status:** locked for build · **Owner surface:** Preventive Care (PC) vertical → Vaccination module.

This spec locks the architecture + scope for Vaccination v1. V1 is not just an
engine foundation or a booster toggle; it must prove a reviewable
vaccine-animal matrix that can generate per-animal due work for new goats,
purchased/intake animals, existing-animal backfill, stage changes, shed changes,
health/defer changes, and accepted-completion next doses. Config IA is settled
separately: Protocol Rules lives under generic Admin / Data Ops at `/config`,
while Preventive Care (PC) / Vaccination links to it filtered as `category=vaccination`. The
vaccination-specific invariant in this spec is that eligibility must not be
modelled with a generic `shed_status` text field. Authoritative source hierarchy:
committed migrations `000071`-`000075`, `wiki/goatOS.docx` (§1 locations, §6
vaccination, §11 shiftings), `context/source-findings/goats-and-parks-source-findings.md`
for stage/tag semantics, voice-note matrix requirements,
`docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md` for vaccine
timing/dose/vial/repeat/gap rules, and the accepted source findings in
`context/source-findings/preventive-care-vaccination-roster-stage-proposal.md` plus
`context/source-findings/live-legacy-critical-guardrails-2026-06-28.md`.
Visual source: `mock/goatos-dashboard-mock.html`.

## 1. Locked decisions

- **Preventive Care (PC) is the vertical. Vaccination is a module under Preventive Care (PC).** Vaccination never
  moves under Parks.
- **One generic protocol/obligation engine internally** (already committed:
  `protocol_definitions/versions/rules`, `obligation_instances/batches`,
  `vaccination_completions`). Vaccination is category 1; feed/deworming reuse it
  later. Do not fork a vaccination-only schema; do not build a parallel system.
- **One vaccination ruleset family, scoped by company or park.** The stable
  Config family is `vaccination.matrix`; ET+TT, PPR, Goat Pox, FMD, and HS are
  cells/rules inside that matrix, not separate top-level protocol definitions.
  For vaccination V1, only one company version can be active and only one park
  override can be active per park. A park override excludes that park from the
  company version, while the company version continues applying to every park
  without an active override.
- **Config UX is generic Admin / Data Ops authority.** The visible Config screen
  is `/config` (`Admin / Data Ops -> Config — Protocol Rules`), CEO/COO/
  superadmin only. Preventive Care (PC) / Vaccination may link to `/config?category=vaccination`,
  but Config is not a Vaccination-owned screen.
- **Eligibility stage comes from the animal's current shed/tag**, never a free-text
  field:

  ```text
  herd_animal → current shed (location) → shed_profiles.animal_stage_id → animal_stage_lookup
  ```

  `animal_stage_lookup` (K0, K1, K2, K3, F2, Mother, Pregnant, Non-Pregnant,
  Buck, ICU, …) drives vaccination eligibility. `min_age_days`/`max_age_days` on
  the lookup tie source shed-tag age policy to stage.
- **V1 target identity is Path B mixed herd animals.** Canonical storage,
  contracts, domain types, events, and UI language use `herd_animals` /
  `animal_id`. The old physical `goats` / `goat_id` names are legacy model
  debt, not the target. Seed goat and sheep in `species_catalog`; goats and
  sheep may share the same shed/tag while vaccination rows distinguish species.
- **`shed_lifecycle_status` (active/inactive/retired) is separate** and must
  NOT drive vaccine dosing.
- **Reuse the committed Locations foundation.** Park = parent location, Shed =
  child location. Add only missing shed-profile/backfill/recompute pieces; do
  not rebuild Locations, do not invent a new capacity field (`shed_profiles.capacity`
  already exists).
- **Do not build full Parks modules now:** no Sanitation, no Preventive Care (PC) & Biosecurity
  Parks page, no Feed Execution, no Ground Team module, no Park Inventory
  screen. **Status update, 2026-06-30:** the old Feed Direction pause is
  satisfied for sequencing by the local Goal 1 vaccination/kernel closure. Feed
  Direction may resume as its own build goal; Google dev rollout remains
  separate Goal 2 and must not be claimed from local evidence.

## 2. Engine ↔ doc mapping (already committed — reconcile vocab, don't rebuild)

| goatOS.docx §6 | Committed table | Notes |
| --- | --- | --- |
| `vaccine_config` (rule) | `protocol_rules` + `protocol_versions.rule_dsl` | rule_dsl carries category + eligibility + schedule[] + compatibility/defer policy; version audit lives on `protocol_versions` |
| `vaccination_schedule` (per-animal) | `obligation_instances` (target=herd_animal) | due/window/status |
| `vaccination_shed_events` (drive) | `obligation_batches` (scope=shed) | planned date/window, reserved/used qty, lot |
| `vaccination_completion` | `vaccination_completions` (000075) | dose_ml_given, lot, FEFO, cold chain, adverse |
| `vaccine_stock` (FEFO) | `vaccines` + `inventory_stock` (000072) | partial FEFO expiry index exists |
| `vaccination_sop_steps` | `sop_versions` form_dsl/proof_policy (seeded 000075) | `000075` is a draft skeleton (`shed_video`, `vial_lot`, `cold_chain`, `dose`, `route_site`, `administered_at`, `adverse_reaction`, `est_vs_used`, `verifier_review`); source parity also requires animal scan, medicine batch/vial-lot, proof media, adverse-reaction notes/follow-up, and verifier/park-head review. |

**Rule eligibility fields (reconciled to the doc):** species · breed/breed group ·
`animal_stage` / shed tag (from `animal_stage_lookup`) · age days · `sex` · lifecycle ·
reproductive state · `defer_states` (ICU/quarantine/sick) · vaccine · vaccine
type/class (live/killed/toxoid/combo/unknown-review-needed) · dose row ·
`config_type` (primary/booster/catch-up/annual) · `dose_number` · `dose_ml` ·
route/site where required · trigger
(`birth_age`/`post_arrival`/`calendar`/`manual_campaign`/`after_previous_completion`)
+ earliest/ideal/latest window · min gap · max delay · repeat/lifetime policy ·
missed-dose/course-lapse policy · proof/SOP binding · protocol version/audit.
Stored in `rule_dsl` (structured columns for stable selectors where present;
jsonb for matrix-specific eligibility and compatibility metadata). Herd-animal
facts are not copied into this JSON; they come from `herd_animals`, `locations`,
`shed_profiles`, `animal_stage_lookup`, procurement handoffs/evidence, and
`vaccination_completions`.

## 3. v1 acceptance scope

1. **CEO/COO-approved vaccine-animal matrix** — each schedule-bearing vaccine row
   must say which species/breed/shed-tag/age cohort receives which dose, under
   what pregnancy/lactation/health restrictions, inside what safe window, with
   what booster/repeat/course-lapse policy, and with what SOP/proof/version
   audit. `PPR`, `FMD`, `HS`, `BQ`, Goat Pox, and ET+TT-style labels are not
   enough by themselves; they become real V1 config only when their matrix rows
   are authored from the tracked nuance/source docs and published by CEO/COO.
2. **Sheds foundation** — active sheds are locations under parent parks; every
   active shed has `animal_stage_id`; include source age range/normalized age
   days, `sex` grouping + `has_icu` /
   quarantine / defer metadata (existing `shed_profiles` columns + `context`
   jsonb). Use existing `capacity`. **Gap:** seed `animal_stage_lookup` +
   `shed_lifecycle_status_lookup`; backfill `shed_profiles.animal_stage_id` for
   active sheds.
3. **Current animal location** — every herd animal resolves to a current shed/location;
   `animal_location_history` stays intact. **Gap:** reliable current-shed
   resolution + backfill (verify/derive a current pointer without rewriting history).
4. **Matrix-driven generation and backfill** — animal creation/import,
   purchase/intake, existing-animal backfill after publish, stage change, shed
   change, health/defer exit, and accepted completion must all re-run the matrix
   for only the affected animal/cohort and produce/cancel/defer the right open
   obligations.
5. **Minimal shifting recompute** — when an animal changes shed, recompute/rescope
   only **pending/due** vaccination obligations (re-stage, re-batch to the
   destination shed drive; pre-shift / individual completion where the drive
   already ran). **Never** rewrite completed/accepted/rejected history.
6. **Minimal ownership/assignment** — enough to route a shed vaccination drive to
   the correct worker/manager (reuse workforce 000050). No full Ground Team module.
7. **Inventory hooks** — vaccine lots, expiry, FEFO reserve/consume/release on
   completion. No Park Inventory UI.
8. **Quarantine/ICU defer** — use as a defer/eligibility state (shed has_icu /
   ICU stage / context). Full Quarantine module can wait.

## 4. Procurement DB [Goats].xlsx — evidence only, not completion truth

Include in Preventive Care (PC) / Vaccination evidence review as **arrival/intake/history evidence**:
load/vendor/breed/gender/weight/moved-to location/tag update, selection health
fields, unloading/transit records, and any source-row vaccination mention.

Procurement may expose vaccination headers/notes, but the live legacy guardrail
found no first-class vaccination evidence field in the cleaned BigQuery tables.
Treat procurement vaccination mentions as source facts with confidence/proof
semantics, not as administered-dose truth.

Supports: procured-animal backfill · `post_arrival` vaccination triggers · intake
health/defer signals · source references/confidence for any vaccination mention.
It does **not** by itself support imported vaccination completion state.

**Must NOT** replace the shed-stage eligibility model. **Do NOT** build the full
Procurement vertical for v1.

## 4.1 Legacy parity floor and import/replay mapping

- Binding matrix rules (non-negotiable): every v1 obligation/rule generation,
  seed, test, and rendered board MUST follow the two Binding Rules at the top of
  `docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`: (a) kid-course rendering
  (4w/7w/12w/16w/20w due points only; nothing before 4w; steady-state repeats
  after 20w) and (b) Goat Pox source-conflict (16w raw / 20w derived when PPR is
  at 16w; keep both truths, label the derivation, never drop 16w raw).
- Preserve tracked SOP/vocabulary labels (`PPR`, `ET`, `FMD`, `HS`, `BQ`), but
  labels alone are not V1 completion. Schedule-bearing vaccine rows now come
  from `docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`: ET+TT, PPR, Goat Pox,
  Sheep Pox, FMD, HS, and Blue Tongue with species split, dose, vial, repeat,
  and gap rules. BQ remains label-only until a later reviewed source adds
  schedule-bearing values.
- Preserve the matrix interpretation: kid boards show only actual due ages
  (4w, 7w, 12w, 16w, and applicable 20w follow-through), while adult/fattening
  steady state follows repeat intervals from accepted completions and adult
  procurement follows its own first-step/+4w path.
- Preserve the SOP proof shape: scheduled date, operator, animal scan, vaccine
  name, medicine batch/vial-lot, dose ml, administered date/time, proof media,
  adverse reaction + notes/follow-up, and verifier/park-head review.
- Known gaps to close: empty medicine batch blocks submission; adverse reaction
  requires notes/follow-up; proof/review confidence is explicit instead of
  inferred from a row existing.
- Import/replay mapping: reliable historical vaccination records, if later
  proven, go through staging, reconcile into `vaccination_completions`, complete
  matching obligations, and schedule boosters from actual `administered_at`.
  Missing or untrusted history must not create completions; older animals whose
  historical windows are already past get one safe catch-up/review action first,
  not every old dose as same-day work. After Preventive Care (PC) approval it becomes
  baseline/catch-up shed drives per
  `docs/protocol-engine/migration-and-cutover.md`.

## 5. Build order (each slice ends with a running local URL for review)

1. Seed/backfill `species_catalog`, breed/species references, and `animal_stage_lookup` + active sheds' `animal_stage_id` (+ shed_lifecycle_status_lookup) with Goats and Parks age ranges.
2. Migrate `goats`/`goat_id` to `herd_animals`/`animal_id`; current animal location resolution + backfill (history preserved).
3. Configure the vaccine-animal matrix rows from the tracked nuance/source docs, not just vaccine labels.
4. Config UI fields aligned to the `animal_stage` model (screen at `/config?category=vaccination`, mock-faithful).
5. Vaccination generation engine using current animal facts and the matrix (birth / post_arrival / calendar / after_previous_completion).
6. Existing-animal backfill and changed-fact recheck for the same matrix.
7. Minimal shifting recompute for pending/due obligations only.
8. FEFO inventory reserve/consume/release + quarantine/ICU defer hooks.

Keep all backend/API/RBAC/schema work. Run the narrowest typecheck/build/test
per slice; for migrations on hot tables, verify indexed plans
(`make validate-sqlc-plans`). Do not resume full Feed Direction operational
screens; the generic Config editor still exposes Feed Direction's distinct
schema fields so the protocol engine is not hardcoded to vaccination.

## 6. Frontend acceptance

Routes in scope: `/login · / · /action-center · /protocol-adherence ·
/workflows · /vaccination · /config · /animals/{animal_id}`. Mesha green/black ·
compact sidebar · Preventive Care (PC) vertical with Vaccination as an operational module ·
Admin/Data Ops with Config · Parks/Sheds as foundation only · no
Operations/Legacy/SOP/cyan nav · no huge fonts · no horizontal clipping · clean
theme toggle · Animal Passport contextual/detail only (no global animal search).
Config screen matches `mock/goatos-dashboard-mock.html` for the table and
authoring anatomy, but the table contract changes from one-vaccine rows to
scoped ruleset rows: Company-wide default first, then active park overrides,
then history inside each row's drawer. `New draft rule` opens a normal
`/config?new_rule=1` authoring page with breadcrumb, left form, right `rule_dsl`
JSON rail, and sticky footer instead of an oversized modal. "mock" is a visual
reference only — never a name in product code/UI.

For the corrected matrix UI/JSON handoff, use
[Rule Matrix Authoring Handoff](./RULE-MATRIX-AUTHORING-HANDOFF.md).
