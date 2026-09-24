# Weighing assumptions are data, edited from one drawer

**Maintainer decisions, 2026-09-19.** Supersedes the "maintainer edits the rows directly" half of
the 2026-09-07 sale-price decision (migration `000363`); keeps its effective-dated table.

## What was decided

1. **Every figure the Weighing area is valued against that someone DECIDED rather than measured
   is configurable from the product**, not from SQL and not from a Go constant:

   | key | was | now |
   |---|---|---|
   | live-weight sale price ₹/kg, goat and sheep | `growth_sale_price_assumptions`, SQL-only | same table, written from the drawer |
   | sale-ready weight line ("Over 35 kg") | `weighing/domain.SaleThresholdUpperKg`, a literal 35 in Farm value and Weights copy | `growth_assumptions.sale_ready_threshold_kg` |
   | load-age alert | `procurement/domain.LoadAgeAlertDays = 90` | `growth_assumptions.load_age_alert_days` |
   | lower sale line ("Over 30 kg") | `weighing/domain.SaleThresholdLowerKg` | `sale_ready_lower_kg` (shed-weights `sale_lower_kg` param) |
   | weight bands 15/20/25/30/35 | `growthdirector/domain.BandLabels`, SQL `width_bucket(...ARRAY[15,20,25,30,35])` | `weight_band_edges_kg` (a LIST; `value_list`) |
   | slow-growth target 200 g/day | `SlowGrowthTargetGPerDay` | `slow_growth_target_g_per_day` |
   | bad-scan cut-off −300 g/day | three SQL literals | `bad_scan_loss_g_per_day` (stored positive) |
   | default period 15 days | `DefaultPeriodDays` | `default_period_days` |

   The Weights pages' LANDING DATE and PICKER FLOOR were asked for in the same breath and are
   deliberately NOT here: main already governs them from the Weighing SOP's `weights_pages` block
   (maintainer request 2026-09-16, fixed date or rolling days), and one figure must have one home.

   Second batch, migration `000365`: the row gained typed `value_list` / `value_date` columns with
   a CHECK that exactly one of the three holds the figure. `growthdirector/domain.GrowthSettings`
   is what the Growth Director and FCR reads take; `SettingsFrom` defaults every key. The band
   labels (`<15`, `15-20`, …, `35+`) are DERIVED from the edges by `BandLabelsFor`, once.

2. **One place to edit them:** the **Assumptions** button top-right of `/weighing/sops` (first
   placed on ADG Analytics, moved to the Weighing SOP page the same day at the maintainer's
   request), opening a same-page drawer (`LocalOverlay`, no navigation). Opening the SOP page
   itself needs `sop.read`, so a person ticked on Weighing alone still needs the Config
   (Protocols & SOPs) module at View.

3. **A change "reflects real time":** every consumer re-reads the rows per request, and the FCR
   read now prices at TODAY's rate rather than the period's end date. The 2026-09-07 design priced
   a window at the rate effective on its last day, which left a price set this morning invisible
   on the default window (it ends yesterday) until tomorrow. The table stays effective-dated for
   the audit trail and the tab prints which row applied.

4. **Access is a per-person HRMS tick, and it decides both halves.** `weighing.assumptions.write`
   is its own permission (never a reuse of `weighing.plan`), held on the `ceo_internal` role and
   carried by the weighing module's **Configure** level, so `/people` grants it per person. The
   PUT route is gated on it AND the `edit_assumptions` page-contract control reads the person's
   held set (`adminui/app.inputAuthorizes`) — the maintainer's words were "who have [the tick]
   should only see it and change it", so the page HIDES the button when the control is off rather
   than greying it. Reading the figures stays on `weighing.monitor`: every Weights reader is owed
   the figures the page is valued at.

## Shape, and why

- `GET/PUT /growth-director/assumptions` (growthdirector module). The write is a whole-set PUT:
  a sale price lands as the row effective today (a same-day re-set overwrites today's row, earlier
  days keep theirs) and is FENCED ON THE PRICE THE DRAWER LOADED (`loaded_price_per_kg_inr`) --
  the price table is append-only and effective-dated, so it has no row_version and the loaded
  figure is the version; a keyed figure is updated in place under a `row_version` fence. Either
  stale is a 409 (PR #320 review: without the price fence two editors who both opened ₹425 landed
  in turn, the second silently overwriting the first). Pinned by
  `TestPutAssumptionsFencesSalePriceOnTheLoadedPrice` (pgtest, mutation-tested). Figures outside their business band (`domain.AssumptionKeys`,
  `SalePriceMinINR..MaxINR`) are **refused with 400**, never clamped or defaulted. Replaying the
  same body lands the same rows. One `audit_log` row per changed figure, in the same transaction.
- `set_by` carries the person's roster **display name**, never a user id, because the drawer
  renders it ("set by Dinakar").
- **Weighing stays isolated.** It does not read `growth_assumptions`; the sale line reaches the
  shed-weights read as `sale_threshold_kg`, exactly the way the margin already does, validated to
  10..80 kg. A caller that names no line gets the 35 kg default. The growth read's own
  `sale_readiness` literal is NOT yet parameterised; no surface renders it today.
- Procurement reads the load-age line inside `OverdueLoadCandidates` and stamps it on each
  `OverdueLoad.ThresholdDays`, so the notification quotes the figure that applied.
- Copy carries a `{kg}` placeholder ("Over {kg} kg"); pages fill it from the SAME assumptions
  read the count was taken against (`features/weighing/assumption-copy.ts`), so a label can never
  name a line the number was not counted at.

## Sale price per stage and sex (maintainer decision 2026-09-24)

"We need per stage and gender of animal configuration." The live-weight sale price was one figure
per species. It can now also be set per **species × management stage × sex**. Three answers the
maintainer gave, each load-bearing:

1. **Keyed species × stage × sex.** Goat and sheep stay apart, as the 2026-09-07 decision asked.
   The existing per-species row (stage and sex both empty) is kept and is the **species default**.
2. **A combination with no price uses the species default.** Nothing that was valued the day
   before this shipped goes blank; the drawer shows the default as each empty box's placeholder.
3. **Both readers value each animal at its own price**, head-weighted: the FCR tab (weight gained,
   per pen: live residents, or the weighed cohort for an emptied pen) and the Load-wise tab (stock
   on hand, per load: its remaining animals).

Shape (migration `000398`): `management_stage` + `sex` columns on
`growth_sale_price_assumptions`, unique on `(tenant, species, stage, sex, effective_from)`. Still
**append-only and effective-dated**: clearing an override is itself a row with a NULL price, so a
past day keeps the price that was in force. A species default can never be NULL (table CHECK and
service validation). An override must name BOTH a stage and a sex, and the stage must be an ACTIVE
`animal_stage_lookup.stage_code` for the tenant (stored in the vocabulary's own spelling so it
matches `goats.management_stage`). The drawer's stage rows come from that vocabulary on the
assumptions response (`stages`), never from a list in the page -- and (same day, second answer:
"only those stages for which weighing done") only the stages weighed animals sit in NOW: the
current stage of every live animal a scanned tag resolves to, and of every live animal in a pen
weighed whole (through the FCR tab's bucket -> pen bridge, since whole-pen buckets name legacy alias
locations). All time, every weighing park. A stage already carrying a price stays listed so it can
be read and cleared. On the 2026-09-24 data this is 5 of 19 stages (ICU-Kid, K3, F2-Male, F2-Female,
Warmup); ICU-Kid is there because three kids that were weighed are in ICU now.

Resolution is one rule in two places that must agree: backend
`growthdirector/domain.SalePrices.PriceForAnimal` (FCR) and admin-web `lib/sale-price.ts`
`salePriceForAnimal` (Load-wise, which already valued stock client-side from backend counts). The
two test files pin the same cases. A pen or load holding an animal whose species has no price at
all is **not valued** rather than valued on its priced part only. `remaining_mix` on the load-wise
reads carries the `(species, stage, sex)` counts; it sums to `remaining`.

Every override save is fenced on the figure the drawer loaded, exactly like a default, and writes
one `growth.sale_price.set` audit row naming species, stage and sex.

## Pinned by

`TestPutAssumptionsRejectsOutOfBandFiguresBeforeTheRepository`,
`TestGrowthAssumptionsWriteIsItsOwnCapability`,
`TestWeighingAssumptionsControlFollowsThePersonsTicks` (mutation-tested: gating on roles alone
turns the ticked-person row red), `TestSaleThresholdsKgTakeTheCallersSaleLine`,
`TestOverdueLoadsJudgeAgainstTheThresholdTheyAreGiven`, and
`features/weighing/weights-assumptions.contract.test.mjs`.

Schema: `000364_growth_assumptions.sql`. Permission: `permissions.WeighingAssumptionsWrite`.
- `TestPenPriceValuesEachAnimalAtItsStageAndSex` (domain) and `lib/sale-price.test.mjs` (web): the
  same resolution cases; both mutation-tested by disabling the override lookup.
- `TestFCRValuesEachPenAtItsAnimalsStageAndSexPrice` and
  `TestPutAssumptionsSetsFencesAndClearsAStageSexOverride` (Postgres).
