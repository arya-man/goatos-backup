# Staging data mapping and proposed persistence

## Fresh read-only evidence
Queried the real `goatos-stg:asia-south1:goatos-stg-core-db` through Cloud SQL Auth Proxy on2026-09-16 at12:53UTC (18:23IST). `current_database=goatos`, transaction_read_only=on. Secret Manager supplied credentials without printing them. No database writes. Latest applied migration remains000317_verification_items_media_meta.

Fresh counts: inventory_items7 active vaccine records in dose units; feed_item_catalog17 records (4active,13retired); admin_ui_config_entries0 rows.12active SOP definitions have separate category_key/subcategory/triggers fields. Family revisions include config54, sops:procurement13, sops:weighing3, protocols:vaccination31. These are cache/sync revisions, not business configuration values.

## Where data should go
| Data entered through proposed interface | Existing authoritative storage / proposed addition | Required handling |
|---|---|---|
| Physical item such as Needle | Existing inventory_items identity for stock-managed consumables; new catalogue mapping is proposed | Preserve tenant/item_id, base_unit, status, row_version. Inventory category remains constrained consumable/medicine/vaccine/etc; arbitrary display categories must not replace this enum. Creating an item must not create stock. |
| Arbitrary category/subcategory and module applicability | New tenant-scoped catalogue taxonomy and relation storage/API required; table names are not decided | Stable category IDs and parent relation; stable catalogue item mapping; explicit category/subcategory/item module links. Compute union with provenance. Module applicability does not grant user permission, stock access or FEFO override. |
| Feed identity and nutritional properties | Existing feed_item_catalog | Preserve feed_item_id, energy/dry-matter/wastage, status and existing foreign-key consumers. Do not duplicate into inventory and assume IDs match. |
| Feed ration, schedules, experimental amounts, removal time | Existing feed domain stores described in staging-database.md | Route writes through existing domain validation; keep park/group/stage/item grain and effective dates; preserve zero versus missing. Mock scope text is not sufficient persistence. |
| Health medicine/protocol instructions | Existing health_protocol_versions and health_protocol_steps | Catalogue identity is distinct from dose, route, denominator, day/session and critical actions. Draft/publish lifecycle and original treatment version stay pinned. |
| Vaccination eligibility, dose/gap/repeat/capacity | Existing protocol version/rule/dimension stores and vaccination capacity config | Preserve typed clinical rules and validators. No generic scalar overwrite. |
| Operator SOP questions/actions and configured references | Existing sop_definitions plus sop_versions form_dsl/proof_policy/compatibility | New reference schema/compiler support needed before real writes. Store stable tenant-scoped item/config reference and publication version; immutable snapshots for active runs. Existing typed engine hooks remain authoritative. |
|30/35kg reporting thresholds and450/kg valuation | New domain-owned persisted policy proposal; existing inspected code hardcodes them | Separate Weighing reporting from Sales valuation and actual recorded transactions. Add validated domain schema/API/consumer migration before treating values as live config. No universal35kg sale ban established. |
| Purchase/selling-price configuration | Proposed typed domain policy with currency/per-unit basis, applicable subject and effective period | Define owner and resolver; preserve dated observed market prices and recorded actual transaction amounts separately. Do not rewrite past sales from configuration changes. |
| Transit starts preparation / arrival gate | Existing event/task/SOP primitives plus new explicit integration contract | Define real producer event, stable subject/context, published target SOP, assignee, idempotency, completion/cancellation and gate. Mock strings are illustrative, not DB-ready executable references. |
| Button titles/labels/copy | admin_ui_config_entries presentation family, if needed by existing API | Allowed presentation kinds only. Never store clinical/business rules here because it looks generic. |

## Minimum production contract before inserting proposed records
This is a mapping proposal, not a migration or seed to run. Resolve canonical registry identity and domain ownership first. Existing adapters must validate tenant, permissions, units, lifecycle, row-version/concurrent edits and effective dates. New taxonomy/module links need a migration and reference lookup contract. Publishing must reject inaccessible/archived/incompatible references while preserving existing pinned runs. Configuration-family revisions/ETags and Android cached contract/outbox behavior must be updated through existing mechanisms. Verify the exact deployed migration/API state before any write.

The mock's sample item IDs, string module names, generated config keys and descriptive scope notes must not be inserted directly into staging. The current task builds and reviews the mock; no schema migration, seed, configuration write or deployment has been performed.

## Fresh deployment distinction — 19:33 IST refresh

Parent repeated read-only queries against real `goatos-stg:asia-south1:goatos-stg-core-db` on2026-09-16 at14:03UTC (19:33IST); transaction_read_only=on. This judge records the parent's query receipt and did not independently repeat the connection.

Observed latest applied migration remains317. Inventory remains7 active vaccine records in dose units; feed catalogue17 rows (4active,13retired); admin UI configuration0 rows. Recorded configuration: removal cutoff21:00, market call08:00, vaccination capacity200 with tenant scope, buffer7 and maximum3shots. These are observed existing domain settings, not proposed generic catalogue seeds.

In this query, `alert_rule_config` and `alert_event_rules` are absent. Current fetched source main aa057776567c44cb8941974d723aa6ee1efd62b9 includes migrations through326, including Alerts320–322. Therefore source supports an Alerts configuration design that this staging database has not yet acquired. Do not label Alerts thresholds/event rules as observed staging rows, seed them through the mock, or claim source/deployed parity. Current source detail is in `current-code-refresh.md`.

No migration, seed, configuration write or deployment was performed. The mapping above remains a proposed implementation plan over existing authoritative stores plus explicit new contracts.
