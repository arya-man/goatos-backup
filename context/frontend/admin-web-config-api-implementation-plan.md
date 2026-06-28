# Admin-Web Config API Implementation Plan

Date: 2026-06-27

Status: implemented v1 for admin-web bootstrap compiler and DB-backed stable UI
config overlays; Redis compiled-cache adapter remains the production cache
follow-up.

Purpose: turn the current backend-owned admin-web contract into a production
config API with stable revisions, cache keys, invalidation, and rollout gates.
This plan is for Admin Web first, but the same contract rules should later apply
to operator-mobile.

## Decision

Admin Web must not render business UI from frontend hardcoded defaults and then
replace them after an async config fetch. Business UI renders after the backend
contract is available through SSR. Before that, frontend may show only auth,
loading, skeleton, or contract-unavailable states.

Postgres is canonical. Redis/Memorystore and in-process maps may cache compiled
contract JSON only as acceleration. A cache miss must always be reconstructable
from Postgres plus backend code-owned product contract shape.

## Current State

Current endpoint:

```text
GET /admin-web/bootstrap
```

Current backend source:

```text
backend/internal/adminui/*
contracts/openapi/app-api.yaml#/components/schemas/AdminWebBootstrapResponse
packages/api-client/src/generated/app-api.ts
```

Current frontend consumers:

```text
apps/admin-web/components/admin-shell.tsx
apps/admin-web/components/mesha-shell.tsx
apps/admin-web/lib/admin-ui-contract.ts
apps/admin-web/lib/api/server.ts#getAdminWebBootstrap
apps/admin-web/features/**/*
```

Current contract owns shell navigation, page copy, table contracts, option
groups, action copy, route labels, and disabled reasons. The v1 compiler is now
request-aware: it receives authenticated tenant/actor/grants from backend auth
middleware, compiles DB-backed families for locations, selected config
vocabularies, SOP labels, permissions, and stable UI config key/value entries
into `/admin-web/bootstrap`, and returns deterministic `contract_revision`,
`family_hashes`, and `cache_policy`.

## What Belongs In The Config API

The Config API carries small, stable contract data that tells the frontend how
to render business UI.

| Family | Examples | Source of truth |
| --- | --- | --- |
| `chrome` | product name, nav groups, route labels, top-bar labels, footer, icon tokens | backend product contract shape plus `admin_ui_config_entries`, permissions |
| `page:<route_id>` | title, subtitle, sections, tables, filters, sort keys, page sizes, row-click rules, drawer anatomy | backend product contract shape plus route-scoped `admin_ui_config_entries` |
| `copy:<route_id>` | empty states, action labels, disabled reasons, field labels, validation copy | route-scoped `admin_ui_config_entries` over backend product contract shape |
| `options:<family>` | bounded status/severity/proof/work-state chips, procurement state, calendar tabs, and optional live-entity display overrides | backend enums/config, DB lookup tables, locations |
| `defaults:<route_id>` | default tab, default sort, default page size, default scope mode | backend product contract |
| `permissions` | visible/hidden/enabled/disabled actions and disabled reasons for actor role/scope | permissions module, role grants |
| `locations` | park display chips, location labels, default park scope, scope selectors, aliases | `locations`, `location_aliases`, related location profile tables |
| `protocols:<category>` | protocol rule option vocab, source/review statuses, publish gates | protocol tables and backend product contract |
| `sops:<domain>` | SOP builder options, proof types, subject scopes, version states | SOP tables and backend product contract |

Option keys must be stable backend-emitted identifiers. Do not key options by
mutable display text. Split option groups by cardinality:

- Bounded vocabularies are strict. Status chips key by backend enum/config value
  emitted by the data API; animal-stage chips key by `animal_stage_id` or
  `stage_code`, not local text.
- Live tenant entities are not strict enums. Park chip overrides key by
  `park_id` or canonical `location_code`, not `park_name`, but the row/object
  must carry the backend display label and render it if no optional override is
  present.
- Do not make the frontend own a fallback label for live data. The fallback is
  backend row data, not a React constant.

Stable UI strings that rarely change, such as nav titles, page titles, table
labels, filter labels, empty states, disabled reasons, and chip/dropdown labels,
are stored as tenant-scoped key/value rows in `admin_ui_config_entries` when
they need runtime governance. The compiler applies those entries to the
bootstrap contract once per request/cache miss. The frontend never fetches those
keys independently per page.

## What Does Not Belong In The Config API

The Config API must not carry volatile or high-cardinality data:

| Data | Correct source |
| --- | --- |
| table rows, cards, alerts, counts | domain/read-model APIs |
| live nav counts | count/read-model API with short TTL or no cache |
| goat/vendor/operator/inventory searchable lists | paginated domain APIs |
| media/proof references | media/proof APIs |
| full protocol/SOP bodies for unrelated pages | route-specific domain APIs |
| form input values for a selected object | selected object's domain API |
| raw analytics/KPI data | analytics/read-model boundary |

## Endpoint Shape

### Phase 1 Endpoint

Keep the existing endpoint and add metadata without breaking current consumers:

```text
GET /admin-web/bootstrap
```

Response adds:

```json
{
  "source": "api",
  "schema_version": "admin-web-ui-v1",
  "contract_revision": "3a5c...",
  "family_hashes": {
    "chrome": "9b19...",
    "permissions": "1f20...",
    "locations": "62aa...",
    "config": "bb70...",
    "db:locations": "b0e4..."
  },
  "cache_policy": {
    "etag": "W/\"3a5c...\"",
    "in_process_ttl_sec": 60,
    "redis_ttl_hint_sec": 600,
    "revision_source": "tenant-role-family-hashes"
  },
  "navigation": {},
  "route_labels": [],
  "top_bar": {},
  "role_lenses": [],
  "pages": [],
  "copy": {},
  "display_rules": []
}
```

Headers:

```text
ETag: "admin-web-ui-v1:<hash>"
Cache-Control: private, max-age=0, must-revalidate
Vary: Authorization, X-GoatOS-Tenant, Accept-Language
```

### Phase 2 Endpoint Split

Split only when the compiled bootstrap becomes too large or slow. Ownership
does not change.

```text
GET /admin-web/bootstrap
  shell/chrome, route index, global option groups, family hashes

GET /admin-web/pages/{route_id}/contract
  route-specific page contract, copy, tables, drawers, option groups

GET /admin-web/config-index
  cheap family hash/revision freshness check
```

Optional later endpoint for lazy option groups:

```text
GET /admin-web/option-groups?families=locations,protocols:vaccination
```

This endpoint must still be backend-owned and strict. It is not a frontend
fallback mechanism.

## Contract Metadata

Add these generated-client schemas:

```text
AdminWebBootstrapResponse
  schema_version
  contract_revision
  family_hashes
  cache_policy

AdminWebContractCachePolicy
  etag
  in_process_ttl_sec
  redis_ttl_hint_sec
  revision_source
```

`contract_revision` is the composite hash of the family hashes included in the
response. The hash must be deterministic over normalized JSON so equivalent
contracts produce the same ETag.

Implemented v1 sources:

- `backend/internal/adminui/app` owns the compiler and stable product shape.
- `backend/internal/adminui/adapters/postgres` reads tenant-scoped DB families:
  active parks, protocol categories, published SOP labels, active/review goat
  breeds, active status definitions for health/reproductive/defer options, and
  active stable UI config entries from `admin_ui_config_entries`.
- `backend/internal/adminui/adapters/http` passes authenticated request context
  from `httpmiddleware` into the compiler.
- `apps/admin-web/components/admin-shell.tsx` derives top-bar parks from
  bootstrap instead of making a second `/admin/locations` read for shell chrome.

## Revision Sources

Use explicit family revisions instead of guessing from response text.

| Family | Revision input |
| --- | --- |
| `chrome` | backend contract version, route registry hash, permissions route map hash |
| `page:<route_id>` | backend page contract hash plus relevant option family revisions |
| `locations` | max `row_version`/`updated_at` from active location tables and aliases for tenant |
| `permissions` | role/capability route map hash plus max grant/policy revision for actor scope |
| `protocols:<category>` | max protocol definition/version/rule/trigger row version for category/scope |
| `sops:<domain>` | max SOP definition/version row version for domain/scope |
| `animal_stages` | max `animal_stage_lookup.row_version` or `updated_at` for tenant |
| `source_vocab:<domain>` | max governed source vocabulary revision |
| `admin-ui-config` | max `admin_ui_config_entries.row_version`/`updated_at` plus `admin_ui_config_family_revisions` for the tenant |

If a table lacks `row_version`, use `updated_at` for v1 and add row versions in
the owning module later. Do not introduce a frontend-side cache key to hide a
missing backend revision.

## Remaining Production Cache Follow-Up

The current implementation uses an in-process 60 second cache, publishes a Redis
TTL hint in the contract, and includes DB revision inputs in the cache key so
stale in-process content misses after DB family changes. Production
Redis/Memorystore should cache by:

```text
admin-web:<schema_version>:tenant:<tenant_id>:roles:<role_hash>:families:<contract_revision>
```

Writes to locations, permissions, protocol config, SOP publishing, animal
stages, status vocabularies, feed items, and `admin_ui_config_entries` bump
family revision rows and emit `config.changed` outbox events. Redis is still a
future compiled-cache adapter; Postgres remains canonical and the in-process TTL
bounds staleness if revision reads fail.

## Database Plan

Implemented v1 computes revisions from existing tables/backend contract hashes
and adds a tenant-scoped stable UI config table:

```sql
CREATE TABLE admin_ui_config_entries (
  tenant_id uuid NOT NULL,
  locale text NOT NULL DEFAULT 'default',
  route_id text NOT NULL DEFAULT '',
  config_key text NOT NULL,
  config_value text NOT NULL,
  value_kind text NOT NULL DEFAULT 'text',
  status text NOT NULL DEFAULT 'active',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, locale, route_id, config_key)
);
```

Stable UI config key patterns:

```text
navigation.footer
top_bar.product_name
top_bar.logo_text
top_bar.park_selector.label
top_bar.date_range_selector.label
top_bar.scope_mode.<option_key>.<label|title|disabled_reason>
chrome.copy.<copy_key>
nav.primary.<id>.label
nav.group.<id>.label
nav.leaf.<id>.label
page.<route_id>.<title|subtitle>
page.title
page.subtitle
copy.<copy_key>
section.<section_id>.title
table.<table_id>.title
table.<table_id>.column.<column_key>.label
table.<table_id>.filter.<filter_key>.label
option.<stable_ui_group_id>.<option_key>.<field_allowed_by_backend_policy>
```

`option.*` overlays are accepted only for option groups and fields classified
in backend code as stable UI/product presentation. Presentation groups may allow
`label`, `title`, `tone`, and/or `disabled_reason`; authoring/domain groups get
no UI-config overlay unless a typed backend policy says that field is visual
only. Config rows must not rewrite live or module-DB-owned groups such as park
display chips, rule scopes, breeds, health statuses, SOP labels, or feed items;
those labels come from canonical module tables and row/object data. Config rows
also must not rewrite semantic metadata such as `source_systems.tone`, because
that value drives publishability in the authoring UI.

Implemented v1 also adds the lightweight revision ledger:

```sql
CREATE TABLE admin_ui_config_family_revisions (
  tenant_id uuid NOT NULL,
  family_key text NOT NULL,
  revision bigint NOT NULL DEFAULT 1,
  content_hash text NOT NULL DEFAULT '',
  changed_at timestamptz NOT NULL DEFAULT now(),
  changed_by uuid NULL,
  source text NOT NULL DEFAULT 'system',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (tenant_id, family_key)
);
```

Rules:

- The ledger is an invalidation pointer, not the source of displayed values.
- Canonical live/domain values remain in module tables.
- Canonical stable UI config values live in `admin_ui_config_entries` when
  governance/runtime edits are needed; backend code owns only the compile shape
  and default product skeleton for missing optional overrides.
- Module write paths bump their affected family key inside the same transaction
  or emit an outbox event consumed by a config revision worker.
- Published immutable versions can be cached long because a new publish creates
  a new revision instead of mutating old truth.

Family keys:

```text
chrome
page:action-center
page:calendar
admin-ui-config
locations
permissions
protocols:vaccination
sops:vaccination
animal-stages
```

## Backend Components

Add these packages under the existing admin-ui module boundary:

```text
backend/internal/adminui/domain
  contract metadata types

backend/internal/adminui/app
  ContractCompiler
  FamilyRevisionResolver
  ContractHasher
  ContractCache

backend/internal/adminui/ports
  RevisionRepository
  Cache
  Clock

backend/internal/adminui/adapters/postgres
  revision queries
  DB-backed option groups: locations, animal stages, permissions, protocol/SOP families
  DB-backed stable UI config entries: nav/page/copy/table/filter/chip labels

backend/internal/adminui/adapters/redis
  compiled contract cache

backend/internal/adminui/adapters/http
  bootstrap
  page contract
  config index
```

The compiler should take:

```text
tenant_id
actor_id
role_lens / capabilities
locale
route_id optional
```

and return deterministic JSON plus metadata.

Production bootstrap accepts request context, authenticates the actor, resolves
tenant/role/capabilities, and compiles only the nav, pages, actions, park scope
controls, and disabled reasons valid for that actor. Static person names such
as role-preview actors must not appear; the role preview uses authenticated
actor metadata or a generic role-lens label when no person is available.

## Cache Strategy

### In-Process Cache

Use for very hot compiled contracts:

```text
key = adminui:mem:<tenant_id>:<actor_role>:<locale>:<contract_revision>
ttl = 30-120 seconds
```

Invalidate by revision miss. Optional best-effort process-local clearing on
`config.changed` events is allowed but not required for correctness.
Current v1 probes `admin_ui_config_family_revisions` before full family loading,
then serves the compiled in-process contract when tenant/role revisions are
unchanged.

### Redis / Memorystore Cache

Use for compiled JSON blobs:

```text
adminui:v1:bootstrap:<tenant_id>:<actor_role>:<locale>:<revision_hash>
adminui:v1:page:<tenant_id>:<actor_role>:<locale>:<route_id>:<revision_hash>
adminui:v1:index:<tenant_id>:<actor_role>:<locale>:<revision_hash>
```

Suggested TTLs:

| Cache kind | TTL |
| --- | --- |
| compiled bootstrap | 10-60 minutes |
| compiled page contract | 10-60 minutes |
| config index | 30-120 seconds |
| immutable published protocol/SOP family | 1-24 hours, keyed by version/revision |
| current pointer family | 30-120 seconds or event-invalidated |

No Redis value may be canonical. If Redis is down, backend compiles from
Postgres/backend contract and logs degraded cache state.

## Invalidation

Every config write or publish action must identify affected families.

Examples:

| Write | Families bumped |
| --- | --- |
| location display/alias update | `locations`, pages using park display chips |
| animal stage update | `animal-stages`, `protocols:vaccination`, config page |
| protocol version publish | `protocols:vaccination`, Action Center, Calendar, Vaccination, Workflows, Config |
| SOP version publish | `sops:vaccination`, Config, SOP Library, PHC execution pages |
| role/capability change | `permissions`, `chrome`, every page with action availability |
| backend contract deployment | code contract hash changes; no DB bump required |

Implementation choices:

1. Same-transaction bump for direct module writes.
2. Outbox `config.changed` event for derived/cross-module invalidation.
3. Background worker recomputes family hash and optionally prewarms Redis.

Correctness comes from revisioned keys. Redis deletion is an optimization.

## Frontend Plan

Frontend remains a strict renderer.

Required changes:

- Keep `requireAdminWebPageContract(route_id)` strict.
- Add metadata-aware `getAdminWebBootstrap()` but keep SSR blocking behavior.
- Support HTTP 304/ETag only inside server-side fetch helpers.
- When Phase 2 split lands, fetch shell bootstrap and active page contract in
  SSR for each route.
- Do not add local copy defaults for business UI.
- Keep icon mapping and layout local.
- Keep contract-unavailable emergency screens local because the contract request
  failed.

Frontend guard updates:

- Flag visible string literals in business route renderers.
- Flag option lookups keyed by mutable label fields such as `park_name`.
- Flag strict option lookups against live entity IDs unless the row/object label
  is used as the backend-owned display path on miss.
- Require every option group used by a page to exist in that route contract.
- E2E should compare rendered labels/chips/columns to the fetched contract.

## OpenAPI Plan

Update `contracts/openapi/app-api.yaml` with:

```text
AdminWebContractMeta
AdminWebContractFamily
AdminWebBootstrapResponse.contract_revision
AdminWebBootstrapResponse.etag
AdminWebBootstrapResponse.generated_at
AdminWebBootstrapResponse.families
AdminWebPageContractResponse
AdminWebConfigIndexResponse
```

Regenerate:

```text
packages/api-client/src/generated/app-api.ts
```

Contract drift check must fail if generated clients are stale.

## Testing Plan

Backend unit tests:

- deterministic hash for equivalent contract JSON
- family revisions compose into stable `contract_revision`
- missing option for emitted key fails contract validation
- Redis down compiles from Postgres/backend contract
- stale Redis revision is not used after family revision changes
- permission change changes action availability/disabled reasons

Backend integration tests:

- no static seed UUIDs, CBE/CPT codes, or person names appear in
  `/admin-web/bootstrap`
- active park display overrides and park scope options are compiled from
  `locations`/aliases keyed by stable location ID
- an Action Center row for a third/unlisted park renders the backend row
  `park_name` without crashing
- animal stages come from `animal_stage_lookup`
- protocol publish bumps `protocols:vaccination`
- SOP publish bumps `sops:vaccination`
- location alias/name change bumps `locations`
- ETag returns 304 only for matching tenant/role/locale/revision
- role/capability changes alter nav/action availability and disabled reasons

Frontend checks:

- `check:ui-contract` rejects local business literals
- typecheck covers generated schema changes
- visual smoke still captures all active routes
- E2E asserts nav/page/chip/table labels are present in the backend contract
- E2E asserts business UI does not render when bootstrap is unavailable

Scale tests:

- no full-herd scans for config bootstrap
- location/permission/protocol revision queries are indexed
- config index is bounded by tenant/role/locale
- large searchable lists remain outside bootstrap

## Rollout Phases

### Phase 0 - Freeze Rules

- Keep current `/admin-web/bootstrap`.
- Land this plan and the backend-contract ownership guard.
- Keep frontend SSR-blocking on bootstrap.

Done gate: every new UI page has page contract before visible UI.

### Phase 1 - Metadata Without Behavior Change

- Add `contract_revision`, `etag`, `generated_at`, and `families` to bootstrap.
- Compute family hashes from current backend contract and DB revision probes.
- Add deterministic contract hasher.
- Update OpenAPI and generated client.

Done gate: current UI renders the same, generated clients pass drift checks.

### Phase 2 - DB-Backed Family Revisions

- Add `admin_ui_config_family_revisions`.
- Add `admin_ui_config_entries` for stable UI key/value labels and copy.
- Wire same-transaction bumps or `config.changed` outbox events for locations,
  protocol publish, SOP publish, permissions, and animal stages.
- Add config index endpoint.

Done gate: changing any canonical config family changes the relevant revision
without a backend deploy.

Current status: DB table, revision ledger, outbox event emission, and bootstrap
compiler integration are implemented. A separate config-index endpoint remains
optional/future because `/admin-web/bootstrap` already includes family hashes
and cache metadata.

### Phase 3 - Redis Compiled Cache

- Add cache port and Redis adapter.
- Cache compiled bootstrap/page JSON by tenant/role/locale/revision.
- Preserve Postgres/backend compile fallback when Redis is unavailable.

Done gate: cache hit/miss/degraded metrics exist, and stale cache cannot render
after revision bump.

### Phase 4 - Page Contract Split

- Add `GET /admin-web/pages/{route_id}/contract`.
- Make bootstrap carry shell, route index, global groups, and family hashes.
- SSR routes fetch only active page contract plus shell bootstrap.

Done gate: payload size remains bounded as pages grow.

### Phase 5 - Full Contract Validation

- Add backend validator proving every emitted option key for seeded/active data
  is present in the relevant option group.
- Add E2E contract conformance assertions for all active routes.
- Add CI guard for literal leaks and mutable option keys.

Done gate: a new backend-emitted key without a contract option fails CI before
runtime.

## Acceptance Criteria

The Config API plan is complete only when:

1. Business UI renders only after SSR contract success.
2. All visible labels/chips/actions/dropdowns/disabled reasons come from
   backend contract or backend data.
3. Stable identifiers are used for option keys.
4. Bootstrap/page contracts include revision metadata and ETags.
5. Redis is optional acceleration, not source of truth.
6. Config writes/publishes bump affected family revisions.
7. Large/high-cardinality lists stay out of bootstrap.
8. OpenAPI and generated clients publish the contract.
9. E2E validates rendered UI against backend contract values.
10. New pages cannot land without contract, tests, and guard coverage.

## First Implementation Slice

Implement this narrow slice first:

1. Add metadata fields and family schema to `AdminWebBootstrapResponse`.
2. Add deterministic JSON hasher.
3. Add family hash computation for:
   - `chrome`
   - `page:<route_id>`
   - `options:process-integrity`
   - `locations`
   - `permissions`
4. Add ETag header on `GET /admin-web/bootstrap`.
5. Add tests that bootstrap has no static seed UUIDs/CBE/CPT/person names, then
   wire DB-compiled location options keyed by `park_id` plus an unlisted active
   park row that renders from backend row data.
6. Regenerate OpenAPI client.
7. Extend `check:ui-contract` to reject mutable option-key lookups.

This slice gives correctness and observability before Redis is introduced.

## Explicit Non-Goals

- No frontend business-copy fallback.
- No Redis as canonical config source.
- No direct frontend reads from Postgres, BigQuery, Sheets, Firestore, or Redis.
- No large object/list payloads in bootstrap.
- No user-editable copy CMS until there is a governed approval/versioning model.
- No unversioned mutation of published protocol/SOP/config truth.
