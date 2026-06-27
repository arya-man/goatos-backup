# Admin-Web Config API Implementation Plan

Date: 2026-06-27

Status: implementation plan.

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
from Postgres plus backend code-owned product contract definitions.

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

Current contract already owns shell navigation, page copy, table contracts,
option groups, action copy, route labels, and disabled reasons. The missing
piece is production-grade revisioning and cache/invalidation around the
compiled contract.

## What Belongs In The Config API

The Config API carries small, stable contract data that tells the frontend how
to render business UI.

| Family | Examples | Source of truth |
| --- | --- | --- |
| `chrome` | product name, nav groups, route labels, top-bar labels, footer, icon tokens | backend product contract, permissions |
| `page:<route_id>` | title, subtitle, sections, tables, filters, sort keys, page sizes, row-click rules, drawer anatomy | backend product contract, page metadata |
| `copy:<route_id>` | empty states, action labels, disabled reasons, field labels, validation copy | backend product contract; later optional DB overrides only through governed workflow |
| `options:<family>` | status chips, severity chips, proof state, work state, procurement state, calendar tabs, park display chips | backend enums/config, DB lookup tables, locations |
| `defaults:<route_id>` | default tab, default sort, default page size, default scope mode | backend product contract |
| `permissions` | visible/hidden/enabled/disabled actions and disabled reasons for actor role/scope | permissions module, role grants |
| `locations` | park display chips, location labels, scope selectors, aliases | `locations`, `location_aliases`, related location profile tables |
| `protocols:<category>` | protocol rule option vocab, source/review statuses, publish gates | protocol tables and backend product contract |
| `sops:<domain>` | SOP builder options, proof types, subject scopes, version states | SOP tables and backend product contract |

Option keys must be stable backend-emitted identifiers. Do not key options by
mutable display text. Examples:

- Park chips key by `park_id` or canonical `location_code`, not `park_name`.
- Animal stage chips key by `animal_stage_id` or `stage_code`, not local text.
- Status chips key by backend enum/config value emitted by the data API.

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
  "contract_revision": "rev_...",
  "etag": "sha256:...",
  "generated_at": "2026-06-27T00:00:00Z",
  "tenant_id": "00000000-0000-4000-8000-000000000001",
  "actor_role": "ceo_internal",
  "locale": "en-IN",
  "families": [
    {
      "id": "chrome",
      "revision": "rev_...",
      "hash": "sha256:...",
      "source": "backend_contract",
      "ttl_seconds": 3600
    }
  ],
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
AdminWebContractMeta
  schema_version
  contract_revision
  etag
  generated_at
  tenant_id
  actor_id
  actor_role
  locale
  cache_status
  families[]

AdminWebContractFamily
  id
  revision
  hash
  source
  source_tables[]
  ttl_seconds
  changed_at
```

`contract_revision` is the composite hash of the family hashes included in the
response. The hash must be deterministic over normalized JSON so equivalent
contracts produce the same ETag.

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

If a table lacks `row_version`, use `updated_at` for v1 and add row versions in
the owning module later. Do not introduce a frontend-side cache key to hide a
missing backend revision.

## Database Plan

Phase 1 can compute revisions from existing tables and backend code hashes.
Phase 2 should add a lightweight revision ledger:

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
- Canonical values remain in module tables or backend contract code.
- Module write paths bump their affected family key inside the same transaction
  or emit an outbox event consumed by a config revision worker.
- Published immutable versions can be cached long because a new publish creates
  a new revision instead of mutating old truth.

Family keys:

```text
chrome
page:action-center
page:calendar
options:process-integrity
options:procurement
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

## Cache Strategy

### In-Process Cache

Use for very hot compiled contracts:

```text
key = adminui:mem:<tenant_id>:<actor_role>:<locale>:<contract_revision>
ttl = 30-120 seconds
```

Invalidate by revision miss. Optional best-effort process-local clearing on
`config.changed` events is allowed but not required for correctness.

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

- active CBE and CPT parks appear in `park_display_chips` keyed by stable ID
- animal stages come from `animal_stage_lookup`
- protocol publish bumps `protocols:vaccination`
- SOP publish bumps `sops:vaccination`
- location alias/name change bumps `locations`
- ETag returns 304 only for matching tenant/role/locale/revision

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
- Wire same-transaction bumps or `config.changed` outbox events for locations,
  protocol publish, SOP publish, permissions, and animal stages.
- Add config index endpoint.

Done gate: changing any canonical config family changes the relevant revision
without a backend deploy.

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
5. Add tests for CBE/CPT park chips keyed by `park_id`.
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
