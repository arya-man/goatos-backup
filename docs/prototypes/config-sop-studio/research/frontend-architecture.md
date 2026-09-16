# Current admin-web configuration and SOP architecture

Research date: 2026-09-16. Read-only review of **origin/main at `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`** (commit timestamp 2026-09-16 16:05:22 +0530), not the prototype branch. All source references below are repository-relative paths **at that revision**; line numbers refer to `git show <revision>:<path> | nl -ba`. No production/browser/database write was performed. Live database facts are owned by the separate database research report and must not be inferred from code comments or seed counts.

## Main finding

The production system already has several real persisted configuration catalogues and versioned SOP/protocol editors. It does **not** reduce to a static UI with hardcoded business rules. However, the reviewed frontend has no single generic category → subcategory → item registry with reusable module links. Existing catalogues have different identities, scope and execution semantics. A central interface must unify the authoring journey without pretending those domain contracts are interchangeable.

The current prototype proves browser-local interactions. It does not prove production catalogue integration, runtime option loading, typed price execution, or migration of these existing configurations. Earlier broad statements that the mock covered every voice requirement were incomplete, particularly category/subcategory module inheritance and configurable prices in the 17:09 recording.

## Production interfaces and consumers

| Surface | Actual persisted source and authored data | Consumer / important boundary | Evidence at pinned revision |
|---|---|---|---|
| `/feed/config` | Feed-item catalogue, ration rates, feeding sessions, schedules, experiment cells, ration-group and pen-tag vocabularies | Module-owned feed names are fetched from the catalogue for selectors; creating an item does not create a feeding quantity or ration rate | `apps/admin-web/features/feed/feed-config.tsx:344-406`; `feed-config-actions.ts:155-177,201-248`; `apps/admin-web/lib/api/server.ts:1708-1722` |
| `/health/config` | Disease × age-band treatment protocols, separately published and drafted; ordered medication/action/critical-action steps | A health case pins its treatment version. Medicine is still entered as `medicine_name` text in this editor, not selected by a shared item identity | `apps/admin-web/features/health/health-config.tsx:20-41,105-115`; `health-config-editor.tsx:392-396,539-577`; `apps/admin-web/lib/api/server.ts:2549-2563,2589-2602,2628-2641` |
| `/vaccination/plan` | Versioned `protocol_version` / `rule_dsl`; vaccines carry codes/names, disease/type, dose schedule, repeat/late policy and anchors | New versions copy the stored document; publish retires prior overlap. This is a vaccination-specific authoring model, not a generic item taxonomy | `apps/admin-web/features/vaccination-plan/plan-actions.ts:9-15,113-150`; `editor-model.ts:20-89` |
| `/counts/sops` | `form_dsl.follow_up` tracks, module/subject, task types, schedules, dependencies and proof counts | Existing event-follow-up machinery is already richer than the new local three-task demo. Some keys/task types remain engine-bound | `apps/admin-web/features/sops/followup-model.ts:4-61`; `sop-actions.ts:163-184,199-209` |
| `/procurement/sops` | `form_dsl.inspection`: load form + paged animal questions, answer options, media policy and conditions | Authors the inspection the phone consumes. Identity-bearing question keys/options are deliberately locked; stable operational fields are not arbitrary generic labels | `apps/admin-web/features/sops/inspection-model.ts:3-45`; `sop-actions.ts:212-259` |
| `/weighing/sops` | `form_dsl.weighing`: planning modes, cap, removal requirement/instruction/cutoff, proof slots and questions | Tasks already planned retain their SOP version. Legacy calendar metadata is retained for document compatibility but no longer exposed as current calendar controls | `apps/admin-web/features/sops/weighing-model.ts:3-76`; `sop-actions.ts:262-280` |
| `/feed/sops`, `/milk/sops` and other shipped SOP libraries | Real `/admin/sops` definitions and versioned `form_dsl` / proof policy | Module pages share a renderer, but use distinct document editors where needed; a generic form builder is only one branch | `apps/admin-web/features/sops/module-page.tsx:14-23,35-103`; `sop-actions.ts:13,44-80` |
| `/sales/config` | Sales deals/payments/animal allocations and landed load costs; market cities/questions/call time | These are operational facts plus survey configuration. A price recorded on a deal or market observation is not the same as a reusable target-price configuration | `apps/admin-web/features/procurement/sales-config.tsx:47-62,83-96`; `apps/admin-web/lib/api/market-server.ts:26-98` |
| `/sales/farm-value` | Reads backend farm valuation/target-rate output and weighing data; read-only contract | Threshold display and tolerance query behavior are partly frontend-coded; this is not a generic price-authoring screen | `apps/admin-web/features/procurement/sales-farm-value.tsx:33-41,57-64,191-209` |

These references identify code contracts, not a claim that every row described by comments exists in today's staging database.

## Taxonomy and identity: what exists and what was not found

- Feed items are a tenant catalogue addressed through `/feed-config/feed-items`; the create payload includes `feed_item` and optional energy/dry-matter/wastage/display-order attributes. No general category/subcategory/module-link fields are sent by this frontend action. Source: `feed-config-actions.ts:201-231`.
- Health configuration's catalogue is a **protocol catalogue**: disease and adult/kid band are not generic item categories. It lists one keyset page and fetches selected version independently. Source: `health-config.tsx:20-41,98-115`. The comment's historical row count is not live inventory evidence.
- Vaccination has specialized taxonomy enums (`live`, `killed`, `toxoid`, `combo`, review-needed; pathogen/course types), and vaccines inside versioned rules. Source: `vaccine-taxonomy.ts:20-29`; `editor-model.ts:27-59`. These are domain validation vocabularies, not an e-commerce-style category tree.
- The generic SOP model carries `medicine_picker` and `vaccine_batch_picker`, but the builder emits source metadata, not a demonstrated central catalogue relationship. Source: `sop-derive.ts:399-432`.
- A targeted search of all tracked `apps/admin-web` for `subcategory`, `sub_category`, `item_category`, `item_categories`, `master item`, and `item registry` found verification-review subcategory filters only, not a generic master-item configuration interface. Search absence is bounded evidence about this frontend tree, **not proof of no database tables anywhere**.
- SOP domain labels themselves are inferred from code prefixes/keywords; the inspected frontend DTO/derivation does not expose the database category metadata as the domain authority. Source: `sop-derive.ts:42-89,428-432`. **Cross-check correction:** live DB research finds `sop_definitions.category_key`, `subcategory` and `triggers`, plus a `sop_categories` table. The frontend comment saying no backend category is stale/incomplete relative to that schema; it is not proof of schema absence. Do not reuse frontend name inference as the new item module-access authority.

## Current SOP lifecycle and real integration gaps

1. Module renderer loads the in-force published version when available and chooses follow-up, inspection or weighing editors before falling back to the generic builder. Unsupported documents are blocked from lossy generic editing. Source: `module-page.tsx:35-99`.
2. Draft creation calls the real create-definition and create-version APIs; validation reports come back from the backend. Source: `sop-actions.ts:44-80`.
3. Publish uses a row version and an immutable SOP version. Libraries are revalidated across Counts, Feed, Milk, Procurement and Weighing. Source: `sop-actions.ts:13,138-145`.
4. Module filtering currently reads at most 200 SOPs and classifies in memory; latest versions are embedded to avoid per-SOP fanout. Source: `module-page.tsx:105-122`. A central catalogue should not copy this bounded-but-eventually-incomplete filtering scheme at larger scale.
5. Generic builder trigger is DSL metadata, and medicine/vaccine option-source metadata lacks demonstrated live option-source endpoints in the documented integration notes. Source: `sop-derive.ts:428-432`. The operator preview explicitly uses disabled picker stubs, even while ordinary answers are interactive. Source: `builder-preview.tsx:45-48,188-190`. A picker-looking mock is insufficient evidence of real consumption.
6. Existing follow-up documents already model `immediately`, `after_event`, `at_fixed_time`, `series`, `after_step`, `requires`, `hardTimeGate` and `waitForAll`; tracks carry module and subject. Source: `followup-model.ts:12-54`. Cross-module activation must be reconciled with this existing model and backend event ownership, not implemented as an unrelated second workflow system.

## Persisted rules versus executable code

| Class | Concrete example | Consequence for central configuration |
|---|---|---|
| Persisted authored business values | Feed ration/session/experiment quantities; Health course steps; vaccination dose/anchor policy; SOP inspection and weighing fields | Preserve stored values, scope, identity, versioning and consumers; do not replace them with prototype defaults |
| Backend-owned UI contract | Navigation, copy, controls, option groups and enabled state from `/admin-web/bootstrap` | A new Items page needs a backend contract and grants; adding a sidebar button alone is insufficient |
| Frontend mirrors of backend schema | Vaccine taxonomy enums (`vaccine-taxonomy.ts:22-29`), locked inspection keys (`inspection-model.ts:40-45`), supported step types (`sop-derive.ts:399-410`) | These are compatibility constraints, not all business settings suitable for unrestricted end-user editing |
| Engine-bound operations | Follow-up task types `weigh`, `tag`, `record_pen`, `feed_colostrum`, `death_evidence`, `return_to_pen` (`followup-model.ts:58-61`) | A generic action name cannot stand in for these typed operations |
| Hardcoded query/display behavior | Sale-ready window 42 days, max tolerance 1000g, threshold `35 - tolerance/1000` (`sales-farm-value.tsx:33-35,195-203`); keyword domain classifier (`sop-derive.ts:42-89`) | Inventory these separately before promising every value can be centrally authored |
| Backward-compatible defaults/ceilings | Weighing legacy date constants, proof count limits (`weighing-model.ts:20-25,61-76`) | Do not mistake compatibility metadata for current authoritative calendar configuration |
| Specialized chart computation | Feed per-item charts; Health treatment/death/adherence; Counts herd flows | Existing analytics are real module-specific read models, not a generic analytics designer |

Health medication names are free text while dose routes/record types/session choices come from backend option groups (`health-config-editor.tsx:392-396,539-577`). That mixed model is a concrete migration concern: a new stable item ID requires mapping and compatibility handling, not just replacing the text input with a dropdown.

## Analytics scope

- Counts uses one `getHerdAnalytics` read and maps real births/deaths/sold/other-exit series: `features/counts/herd-analytics.tsx:171-173,212-216`.
- Health uses `getHealthAnalytics`, including adherence statuses: `features/health/health-analytics.tsx:196,248-251`.
- Feed uses distinct directed/execution/experiment/stock/pen-feed endpoints based on active sections: `features/feed/feed-analytics.tsx:358-394`.
- Weights uses multiple existing read models: `features/weighing/weights-analytics.tsx:226` onward.
- Market analytics uses `/market/analytics`; survey entry is a different endpoint/surface: `lib/api/market-server.ts:101-119`.

None of these inspected surfaces is a generic visual analytics-authoring tool. The recording mentioning analytics establishes a desired reusable-tool direction, not agreed metrics, aggregation semantics or proof that local run counters satisfy it.

## Relevant last-month changes and drift risks

History window inspected: 2026-08-16 through the pinned revision, path-scoped to admin-web configuration/SOP/analytics code.

| Commit(s) | Relevant change | Research implication |
|---|---|---|
| `be3203ba0`, `0d4812590`, `8416d04da`, `802513a9d` | Herd operator-step authoring, event-relative rounds, session basis and preservation fixes | Existing event and dependency semantics must be reused and preserved |
| `e637634c9`, `606578151`, `f2a9a17ef` | Procurement inspection and load-form authoring; authored media storage/capture corrections | The current source is more capable than older form-only assumptions |
| `acbb15186`, `551b2b596`, `feb608513`, `4b92325dc`, `dad1d38a0` | Weighing rules, authored captures/cutoff and pinned capture safety | Scope/identity/version rules matter to any common configuration integration |
| `80477f0dc`, `2b606afa5`, `fec94827c` | Calendar configuration moved and obsolete SOP controls removed | Do not follow the intermediate SOP-calendar design: current main reads calendar configuration separately |
| `40ec5518b`, `a9250a014` | Publish closes editor and revalidates all module libraries | Cross-module save/publish invalidation is an existing requirement, not cosmetic polish |
| `34e408261`, `08dd1bf77`, `d1839c7b3`, `64aa36722` | Market survey configuration/call time; save-in-place UX and guards | Reusable configuration should preserve inputs and keep success/error attached to the changed row |
| `868cc2239`, `7ff5f2d8c` | Authored experiment quantities and feed-item entry refinement | Feed catalogue exists already and has a distinct quantity model |
| `995ececeb`, `3e9f1f1c1`, `98c108cf4`, `bba6bea8e` | Rule-level vaccination anchors persisted and scheduled atomically | Existing specialised protocol rules are substantial; do not flatten them into arbitrary strings |
| `d0c99e8d8` | Deterministic health observation-to-treatment pipeline | Treatment catalogue changes have downstream diagnosis/runtime consequences |

Commit messages were used as change pointers, then relevant current files were read. This is not a full audit of every changed line in the month.

## Reliability, authorization and media constraints for a later implementation

The admin shell intentionally refuses business UI when bootstrap fails (`components/admin-shell.tsx:14-37`); required page contracts throw `Admin-web contract unavailable` with backend error context (`lib/api/server.ts:850-867`). Existing Sales config serializes bounded bootstrap reads specifically to avoid Cloud Run warmup fanout (`sales-config.tsx:78-96`). A future central registry must preserve tenant/session/grant context, bounded reads and explicit error states. This research does not certify absence of `backend_down`, `The board could not be loaded`, or `Weights could not be loaded` in live use.

The proposed item/config feature should not auto-fetch proof images/videos merely to populate catalogue choices. If later work touches existing media paths, stable identity, bounded cached photo previews, explicit video actions and attribution remain separate required checks. No media runtime or deployment changes were made in this research.

## Unresolved questions before any production design claim

- Separate staging research now confirms specialised stores and SOP taxonomy metadata; reconcile those with exposed frontend/API contracts before proposing migration. Main and live DB migration levels differ (see `staging-database.md`).
- Category/subcategory/item module relation semantics: inheritance is explicitly requested, but union versus replacement/exclusion behavior is not specified by the voice notes.
- Price identity: target configuration versus observed purchase/sale fact, currency/unit, animal/item applicability, date/scope/version and consumer rules are not yet fully specified.
- Mapping old feed names and Health medicine strings to stable central item IDs without breaking published or in-flight work.
- Live option-source API and mobile cache/sync behavior for new shared items.
- Which existing typed operation/rule editors remain specialized under one interface, rather than being inaccurately replaced by a universal string field.

Knowledge conclusion: **reuse the existing persisted foundations; the missing generic authoring interface and cross-module taxonomy remain design/integration work.** No code changes, promotion or staging mutation were performed for this report.
