# Real staging configuration inspection

Inspected 2026-09-16 around 17:22 IST. Target: `goatos-stg:asia-south1:goatos-stg-core-db`, database `goatos`, through Cloud SQL Auth Proxy with refreshed authorized user credentials. Every query used `default_transaction_read_only=on` and 15-second statement timeout; `current_setting('transaction_read_only')` returned `on`. No data changes. Credentials and personal/operator identifiers excluded.

## Revision distinction

Live `goatos_schema_migrations` latest recorded migration: `000317_verification_items_media_meta`, applied 2026-09-16 01:50 UTC. Source audit is `origin/main` `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`, which includes subsequent calendar migrations. These are different states. The live database has no dedicated weighing calendar configuration table; the published weighing SOP still carries weights_pages. No claim that the deployed application matches origin/main.

## Observed configured data

| Family | Observed persisted shape / values | Mock implication |
|---|---|---|
| Inventory | 7 rows, all active vaccine/dose records: Blue Tongue, ET+TT, FMD, Goat Pox, HS, PPR, Sheep Pox; `context={}`. Fields include code/name/category/base_unit/status/version. | Preserve inventory identity and unit; medicine/needle generic examples are proposed catalogue additions, not existing inventory rows. |
| Feed catalogue | 17 rows, 4 active: Dry Masoor Bhusa, UHT Milk, Mesha Kids Concentrate, Mesha Adult Concentrate. Energy/dry-matter/wastage fields null on inspected entries. | Null means unconfigured, not zero. Retired rows remain history. Feed catalogue identity is separate from inventory. |
| Feed ration | Effective-dated rows keyed by park, ration group, pen stage/tag, feed item. Current examples: Boer/Buck/Dry Masoor Bhusa 1900 g/head; several other breed groups/Buck 1700 g/head. Historic concentrate rows include explicit 0. | Model scope and effective dates. Do not turn every row into an unscoped price-like scalar. |
| Feed schedule | Normal direction 07:00, correction 14:00, transport 15:30; experiment direction/correction 14:00 and transport15:30. Four current rows across parks. | Workflow and park scope with time fields. |
| Removal cutoff | `feed_water_removal_config.cutoff_time=21:00`. | Shared timing consumed across Feed/Weighing. |
| Weighing published SOP | `weighing.session` v1: planning individual_animal/per_shed_partition, default cap100; individual video required; lump-sum video min1/max5; removal required with separate feed and water video proofs. `weights_pages.earliest_date=2026-08-01`, fixed default_from_date2026-08-03. | Typed policy/proof fields and published version; no claim newer dedicated-calendar schema is live. |
| Preventive Care | `vaccination.matrix` published v9, one draft, eight retired versions. Rule DSL includes vaccine schedule, dose amount/unit/route, ages, gaps, catch-up, course/repeat rules, capacity. | Versioned clinical configuration is richer than a generic item. Keep medicine identity separate from protocol dosage/schedule. |
| Capacity | max_per_day200; capacity_scope tenant; max_buffer_days7; max_shots_per_animal_per_drive3; overflow policy split_within_safe_window_last_safe_may_exceed_cap. | Distinct dimensions/units, preserve actual owner. |
| Health | Published per-disease adult/kid versions; inspected duration examples abscesses7 days, acidosis2, anemia4, diarrhea3, fever3. Steps distinguish medication/action/critical_action, route IM/SQ/IV/Oral, denominator kg/none. | Display existing typed fields, never manufacture clinical changes or collapse routes/dosage into catalogue text. |
| Verification | feed_distribution75% effective2026-09-11; vaccination_proof25% effective2026-09-05. | Effective-dated percentage rules, separate from proof capture requirements. |
| Market survey | Call time08:00 persisted. Prices in `market_price_entries` are dated recorded observations; `sales_market_benchmarks.market_price_per_kg` exists separately. | Market observation, valuation assumption and actual transaction price are different concepts. |

## Taxonomy and SOP metadata

Live `sop_categories`: commodity, problem, event, action, equipment. `sop_definitions` **does contain** category_key, subcategory and triggers JSON. Twelve definitions inspected; birth/death/reconcile/shifting use action, others have blank category; all subcategories blank and triggers empty arrays. This is SOP metadata, not proof of an existing generic inventory category/subcategory inheritance model.

Live task registry includes yes/no, select, multiselect, number, text, do-and-confirm, photo/video, weigh, tag, record pen, colostrum, death evidence, return to pen, verify, administer, inspect, transfer. Fields include category_scope, answer_kind, engine_hook, parameter_schema. Category applicability is not module sharing.

Published SOP versions inspected: procurement.animal_purchase v7, shifting v2, other ten definitions v1. SOP documents include fields/rules plus specialized inspection/weighing/follow_up shapes. Existing runtime should consume typed published documents.

## The two initial examples

The schema/column inventory did not reveal a central sale eligibility or valuation-rate configuration store. Source evidence locates 30/35kg thresholds in Weighing constants and ₹450/kg (plus ₹600/₹500 and cohort weights) in Sales valuation SQL. This is **not** proof that a recorded sale is prohibited below35kg, nor that ₹450/kg is every sale's actual price. Treat exposing these as editable config as proposed mock capability, keep reporting threshold/valuation assumption/actual transaction separate.

## Query coverage and limitations

Queried information_schema tables/columns for config/catalog/category/rule/protocol/policy/SOP/price/threshold families; inspected the listed catalogue, ration, schedule, SOP, health, protocol, capacity and verification records with bounded queries. Did not export animal, buyer, employee, auth or transaction data. Did not read every JSON value or every tenant operational row. Live UI and source audits complete the consumer mapping; source findings must not be labelled live DB settings without evidence here.
