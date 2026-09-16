# Backend architecture discovery

Snapshot: `origin/main` = `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`, inspected 2026-09-16. All source references below are paths and line numbers **at this revision**, read with `git show origin/main:path` / `git grep origin/main`; they do not refer to potentially dirty working-tree contents. The primary checkout has unrelated changes and was not modified. No database access, writes, migrations, runtime tests or promotion were performed. This is a source architecture map, not a live deployment certification.

## Principal finding

The backend already has multiple real configuration stores and versioned SOP consumers. A single generic item form cannot replace them without an explicit mapping. Shared reusable vocabulary, authored operational policy, runtime task state, UI presentation configuration and derived analytics are distinct concepts. The mock should expose these distinctions rather than imply that every displayed number is a configurable item or that selecting a module automatically implements a consumer.

## Existing vocabulary and ownership

| Concept | Existing canonical shape | Implication for a generic configuration interface |
|---|---|---|
| Inventory item | `backend/internal/inventory/domain/types.go:6-25`: item ID, tenant, code, name, category, base unit, status. `backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql:3161-3175`: stored in `inventory_items`, category constraint vaccine/dewormer/medicine/feed/supplement/consumable/other, contextual JSON and row version. | A needle fits a consumable inventory definition, but module visibility and subcategory are not first-class fields in this inspected model. Do not claim they already exist as a general registry API. |
| Feed item | Baseline migration `13495-13511`: separate `feed_item_catalog`, normalized key, energy kcal/kg, dry-matter factor, wastage factor, order and active/retired status. | Specialized feed properties and existing references must be retained when surfacing items generically. An inventory item is not automatically the same identity as a feed catalog row. |
| SOP category | `backend/migrations/postgres/000308_sop_driven_herd_operations.sql:40-52,79-89`: tenant category registry, active/retired; seeded commodity/problem/event/action/equipment. | These categories classify procedure subject matter. They are different from the inventory category enum. |
| SOP task type | Same migration `54-76,91-110`: tenant registry with category scope, answer kind, engine hook, parameter schema and status. | An item, an operator question and an engine action are distinct. A generic editor can discover task types but must respect their supported hooks and parameter schema. |
| Stage reference data | `backend/internal/protocol/domain/types.go:205-226`: stages from `animal_stage_lookup`, age-band classification, optional age bounds and assignable-as-cohort flag. | Clinical tags are not freely assignable cohort options; stage lookup semantics should drive consumers instead of copied lists. |
| SOP document | `backend/internal/sop/domain/types.go:29-60`: definition identity plus version, form DSL, proof policy, compatibility, validation, publication and retirement. | Reuse must preserve stable ID, version and lifecycle, not simply a shared label. |

A source search for `subcategory` / `sub_category` under `backend/internal` returned no matches in this snapshot. That is a bounded finding about inspected source, not proof that no external or JSON-only taxonomy exists.

## Real rules, separated by storage and meaning

| Example | Classification and owner | Source evidence |
|---|---|---|
| Feed grams/head, effective dates, shed factors | Persisted authored configuration; feedconfig owns writes and Feed direction consumes it. Zero is valid; absent must block. | `backend/internal/feedconfig/domain/types.go:1-31,91-100` |
| Direction/correction/transport dispatch times | Persisted effective-dated park/workflow configuration, local Asia/Kolkata business clocks. Missing transport time is unknown, not no deadline. | Same file `412-429` |
| Experimental feed quantities and population | Authored quantity belongs to a park/shed/partition/item; live headcount is derived from current pen population, not authored. | Same file `490-515` |
| Weighing planner modes and default daily cap | Authored SOP planning policy; cap is a default applied when creation supplies none. | `backend/internal/weighing/domain/sop.go:120-126` |
| Feed/water removal cutoff, instructions, evidence slots and questions | Authored weighing SOP policy; blank own cutoff resolves to farm-wide removal configuration, and effective value is sent to the phone. | Same file `129-144` |
| Individual weighing video and lump-sum proof counts | Individual video remains a locked required invariant; lump-sum min/max configurable within policy bounds. | Same file `220-236` |
| Never-published weighing behavior | Explicit embedded fallback seed: required removal, both clips, cap 100, 1–5 lump-sum videos. It is a fallback, not evidence of tenant-authored values. | Same file `239-265` |
| Calendar start and rolling window | **Dedicated persisted tenant configuration, independent of SOP publication**; fixed date, rolling weeks or rolling days; revision bump trigger. This supersedes older SOP page-settings design. | `backend/migrations/postgres/000319_weighing_calendar_db_config.sql:2-22,25-46` |
| Health duration and medicine route | Typed clinical protocol data; duration default 3 with bound 1–90 in schema; session duration is separately stored. | `backend/migrations/postgres/000098_health_workflows.sql:12,41,68,141,164` |
| Health protocol edits | Draft/publish/retire versioned configuration, one open draft per disease/age-band, immutable published version, transaction-local idempotency/audit ledger. Existing treatment retains original version. | `backend/migrations/postgres/000121_health_protocol_authoring.sql:15-29,50-71,86-128` |
| Sale-weight reporting thresholds | Hardcoded Weighing domain constants: 30/35 kg; tolerance maximum 1000 g and default 0. These are reporting thresholds, not evidence of a generic editable sale eligibility policy. | `backend/internal/weighing/domain/shed_weights.go:260-263` |
| Farm valuation assumptions | Hardcoded SQL rate/weight matrix: fattening 450/kg; adult female 40kg at 600/kg; adult male 60kg at 500/kg; K0/K1 3kg, K2 8kg, K3 15kg at 500/kg. | `backend/internal/sales/adapters/postgres/overview_repository.go:617-625` |
| Realized sale price/kg | Derived revenue divided by measured live weight; average band price derived from band revenue/weight. It must not be edited as policy. | `backend/internal/sales/domain/overview_build.go:134-149` |
| Birth/mother follow-up schedules | Authored seeded SOP steps demonstrate timed repeated colostrum sessions, prerequisites, tag wait-for-all, second ORS 50 minutes after first. These are examples from published seed documents, not confirmed live configuration. | `backend/internal/tasks/domain/sopseed/counts_birth.json:15-16,27` |

The 35kg and 450/kg examples are only two cases within this broader configuration landscape. The backend currently also distinguishes species/cohort assumptions, unit-bearing quantities, effective dates, evidence constraints, timing rules, stage lookups, immutable clinical versions, and presentation settings.

## SOP execution, cross-module events and synchronization boundaries

1. `backend/internal/tasks/adapters/postgres/sop_template.go:15-45` reads the published SOP by tenant/code plus active task type registry. Compilation pins `sop_version_id` on workflow open. Never-authored tenants can use an explicit seed; an invalid published document fails closed rather than silently reverting.
2. `backend/internal/tasks/adapters/postgres/repository.go:184,215-217,245-255` persists workflow pin and action metadata: task type, answer type, engine hook, proof minimums, hard-time gate, wait-for-all, requires keys and after-step offset. This is richer than three checkbox tasks.
3. `backend/internal/tasks/domain/sop_followup.go:113-115,337` exposes gate/dependency fields and validates required predecessors within a track. A free-form dependency editor must map to these constraints rather than assume arbitrary graph execution is supported.
4. `backend/internal/eventwiring/workflows.go:18-61` centralizes durable and in-process workflow consumer registration. Current concrete producers include reported/rejected deaths, created births, exited animals, identifier events and verification verdicts. Evidence routing filters source module/ref type. This demonstrates actual event-linked workflows but does **not** establish a general persisted arbitrary module-to-module trigger registry.
5. `backend/internal/tasks/app/service.go:34,157` documents downstream verification idempotency and mandatory action-write idempotency. A production dependency design must preserve replay identity and existing event/verification semantics, not merely suppress repeated UI clicks.
6. `backend/internal/sop/domain/types.go:65-78` distinguishes latest from published version and batches list version facets to avoid N+1 detail fetches. The operator runs published content; the editor must not accidentally build on a later retired or unrelated draft version.

For transit → preparation → arrival, the inspected engine provides useful primitives (scoped subject, pinned SOP, prerequisites, timing, proof and event consumers). A complete integration still needs an explicit producer event and subject identity, mapping to published target SOP/track, task ownership, completion projection, replay/cancellation behavior and destination gate. The mock's editable event strings and SOP names are illustrative references, not existing backend contracts.

## Presentation configuration is not business policy

`backend/internal/adminui/adapters/postgres/repository.go:35-80` assembles contract families from parks, protocol categories, breeds, statuses, SOP labels, weighing page configuration, feed items, task types and UI config, tracking family revision inputs. Thus module dropdowns already draw on multiple backend-owned sources.

`backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql:1980-1997` stores route/locale config entries whose allowed kinds are text/label/title/copy/tone/disabled_reason. This is not a catch-all table for clinical dosage or eligibility policy.

`backend/internal/appconfig/domain/types.go:1-8,29-33` explicitly limits mobile remote configuration to presentation and bounded operational knobs; business/medical policy is excluded, with a read-only policy revision echo. `backend/internal/appconfig/app/service.go:16-17,55-58,144-175` clamps environment-supplied runtime defaults and compiles revision/ETag. A shared configuration UI must route writes to the authoritative domain, not bypass this separation through mobile config.

## Last-month architecture changes that affect interpretation

History window inspected: since 2026-08-16 on the above `origin/main` revision, scoped to SOP, tasks, procurement, sales, feed configuration, weighing and admin contract paths. This is relevant-history sampling, not every repository commit.

- `2886e8a3c` moved herd operations workflow opening to published SOP content; `3e4f1baba` refined rework, backend-owned labels and proof behavior. Older code-template documentation is superseded for these workflows.
- `e637634c9` introduced SOP-authored procurement inspection; `606578151` extended it to the load form and enforced capture kind server-side, using the published version as editor baseline.
- `39e63287c` added weighing SOP planning/removal/proof behavior; `8eec65cd5` expanded authored photo/video/either slots; `feb608513` added own cutoff with shared fallback; `bb5958b5c` rejected unknown document keys; `1eb883120` bounded cached version reads and forwarded answers to verification.
- `4ddfc9bf0` / `80477f0dc` put calendar page settings on SOP; later `2b606afa5` / `c5305ee8e` moved calendar configuration directly to DB. The final snapshot must take precedence over the earlier design.
- `f53c8c714`, `837711f55`, `beb9e240e` developed multi-line sales aggregation with grain and manure/live-target guards. Generic analytics cannot assume one deal equals one product row or count all product lines as live animals.
- `f6d4e196c`, `33856d26c`, `7f23cfedf` corrected farm valuation cohort membership, ICU inclusion and sex breakdown. Hardcoded valuation rates must be separated from evolving derived membership rules.

## Design consequences for the next mock iteration

- Show item identity/category/subcategory/units and intended module availability, but label new generic mappings as proposed until a canonical registry/consumer contract is chosen.
- Represent source ownership and consumers as metadata. Consumer availability must not imply ownership transfer or duplicate domain tables.
- Distinguish configuration types: catalog/reference data; authored policy with version/effective date; locked invariant; default/fallback; derived result; presentation copy.
- Make publishing/version pinning, effective dates, unit validation, missing-versus-zero and incompatible module use visible in representative flows.
- Use existing engine task types and proof/timing/dependency vocabulary when demonstrating SOPs. Preserve specialized hooks rather than model every task as an unconstrained button.
- Present analytics definitions with grain, population, source and version. Show computed metrics, not editable copies of derived values.
- Keep any broad editor as an authoring shell over existing owners; do not propose one generic JSON persistence endpoint as though it already satisfies domain validation, revision and audit contracts.

Remaining evidence: live staging rows, deployed revision, actual route write permissions, registry-authoring API breadth, Android sync implementation and transit producer contract require separate verification. No staging values or availability claims are made here.
