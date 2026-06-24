# Vaccination v1 — Foundation Spec (locked)

**Date:** 2026-06-24 · **Status:** locked for build · **Owner surface:** PHC vertical → Vaccination module.

This spec locks the architecture + scope for Vaccination v1. Config IA is now
settled separately: Protocol Rules lives under generic Admin / Data Ops at
`/config`, while PHC/Vaccination links to it filtered as `category=vaccination`.
The vaccination-specific invariant in this spec is that eligibility must not be
modelled with a generic `shed_status` text field. Authoritative business source:
`wiki/goatOS.docx` (§1 locations, §6 vaccination, §11 shiftings) + committed
migrations `000071`–`000075`. Visual source: `mock/goatos-dashboard-mock.html`.

## 1. Locked decisions

- **PHC is the vertical. Vaccination is a module under PHC.** Vaccination never
  moves under Parks.
- **One generic protocol/obligation engine internally** (already committed:
  `protocol_definitions/versions/rules`, `obligation_instances/batches`,
  `vaccination_completions`). Vaccination is category 1; feed/deworming reuse it
  later. Do not fork a vaccination-only schema; do not build a parallel system.
- **Config UX is generic Admin / Data Ops authority.** The visible Config screen
  is `/config` (`Admin / Data Ops -> Config — Protocol Rules`), CEO/COO/
  superadmin only. PHC/Vaccination may link to `/config?category=vaccination`,
  but Config is not a Vaccination-owned screen.
- **Eligibility stage comes from the goat's current shed**, never a free-text
  field:

  ```text
  goat → current shed (location) → shed_profiles.animal_stage_id → animal_stage_lookup
  ```

  `animal_stage_lookup` (K0, K1, K2, K3, F2, Mother, Pregnant, Non-Pregnant,
  Buck, ICU, …) drives vaccination eligibility. `min_age_days`/`max_age_days` on
  the lookup tie age band to stage.
- **`shed_lifecycle_status` (active/inactive/retired) is separate** and must
  NOT drive vaccine dosing.
- **Reuse the committed Locations foundation.** Park = parent location, Shed =
  child location. Add only missing shed-profile/backfill/recompute pieces; do
  not rebuild Locations, do not invent a new capacity field (`shed_profiles.capacity`
  already exists).
- **Do not build full Parks modules now:** no Sanitation, no PHC & Biosecurity
  Parks page, no Feed Direction, no Feed Execution, no Ground Team module, no
  Park Inventory screen. **Feed Direction stays paused** until PHC/Vaccination UI
  + this foundation are reviewed and approved.

## 2. Engine ↔ doc mapping (already committed — reconcile vocab, don't rebuild)

| goatOS.docx §6 | Committed table | Notes |
| --- | --- | --- |
| `vaccine_config` (rule) | `protocol_rules` + `protocol_versions.rule_dsl` | rule_dsl carries category + eligibility + schedule[] + source |
| `vaccination_schedule` (per-goat) | `obligation_instances` (target=goat) | due/window/status |
| `vaccination_shed_events` (drive) | `obligation_batches` (scope=shed) | planned date/window, reserved/used qty, lot |
| `vaccination_completion` | `vaccination_completions` (000075) | dose_ml_given, lot, FEFO, cold chain, adverse |
| `vaccine_stock` (FEFO) | `vaccines` + `inventory_stock` (000072) | partial FEFO expiry index exists |
| `vaccination_sop_steps` | `sop_versions` form_dsl/proof_policy (seeded 000075) | shed video + vial video + dose/qty/lot |

**Rule eligibility fields (reconciled to the doc):** `animal_stage` (from
`animal_stage_lookup`) · `sex` · `defer_states` (ICU/quarantine/sick) · vaccine ·
`config_type` (primary/booster) · `dose_number` · `dose_ml` · trigger
(`age_based`/`post_arrival`/`calendar`) + offset/window. Stored in `rule_dsl`
(structured columns for stage/sex/dose_ml/config_type; jsonb for the rest).

## 3. v1 minimum foundation (build only this)

1. **Sheds foundation** — active sheds are locations under parent parks; every
   active shed has `animal_stage_id`; include `sex` grouping + `has_icu` /
   quarantine / defer metadata (existing `shed_profiles` columns + `context`
   jsonb). Use existing `capacity`. **Gap:** seed `animal_stage_lookup` +
   `shed_lifecycle_status_lookup`; backfill `shed_profiles.animal_stage_id` for
   active sheds.
2. **Current goat location** — every goat resolves to a current shed/location;
   `goat_location_history` stays intact. **Gap:** reliable current-shed
   resolution + backfill (verify/derive a current pointer without rewriting history).
3. **Minimal shifting recompute** — when a goat changes shed, recompute/rescope
   only **pending/due** vaccination obligations (re-stage, re-batch to the
   destination shed drive; pre-shift / individual completion where the drive
   already ran). **Never** rewrite completed/accepted/rejected history.
4. **Minimal ownership/assignment** — enough to route a shed vaccination drive to
   the correct worker/manager (reuse workforce 000050). No full Ground Team module.
5. **Inventory hooks** — vaccine lots, expiry, FEFO reserve/consume/release on
   completion. No Park Inventory UI.
6. **Quarantine/ICU defer** — use as a defer/eligibility state (shed has_icu /
   ICU stage / context). Full Quarantine module can wait.

## 4. Procurement DB [Goats].xlsx — evidence only

Include in PHC/Vaccination source review as **arrival/intake/history evidence**:
load/vendor/breed/gender/weight/moved-to location/tag update, selection health
fields, unloading/transit records, historical vaccination-at-procurement evidence.

Supports: procured-goat backfill · `post_arrival` vaccination triggers · imported
vaccination history/completion evidence · intake health/defer signals.

**Must NOT** replace the shed-stage eligibility model. **Do NOT** build the full
Procurement vertical for v1.

## 5. Build order (each slice ends with a running local URL for review)

1. Seed/backfill `animal_stage_lookup` + active sheds' `animal_stage_id` (+ shed_lifecycle_status_lookup).
2. Current goat location resolution + backfill (history preserved).
3. Config UI fields aligned to the `animal_stage` model (screen at `/config?category=vaccination`, mock-faithful).
4. Vaccination generation engine using current shed stage (birth / post_arrival / age-cron).
5. Minimal shifting recompute for pending/due obligations only.
6. FEFO inventory reserve/consume/release + quarantine/ICU defer hooks.

Keep all backend/API/RBAC/schema work. Run the narrowest typecheck/build/test
per slice; for migrations on hot tables, verify indexed plans
(`make validate-sqlc-plans`). Do not resume full Feed Direction operational
screens; the generic Config editor still exposes Feed Direction's distinct
schema fields so the protocol engine is not hardcoded to vaccination.

## 6. Frontend acceptance

Routes in scope: `/login · / · /vaccination · /config ·
/vaccination/adherence · /goats/{goat_id}`. Mesha green/black · compact sidebar ·
PHC vertical with Vaccination nested · Admin/Data Ops with Config · Parks/Sheds as foundation only · no
Operations/Legacy/SOP/cyan nav · no huge fonts · no horizontal clipping · clean
theme toggle · Goat Passport contextual/detail only (no global goat search).
Config screen matches `mock/goatos-dashboard-mock.html` (table + New-draft-rule +
editor modal: left form / right `rule_dsl` JSON / sticky footer). "mock" is a
visual reference only — never a name in product code/UI.
