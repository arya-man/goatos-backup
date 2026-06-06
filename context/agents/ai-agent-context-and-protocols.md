# AI Agent Context And API Protocol Architecture

This document defines how Codex, Claude, and future AI agents get context across Goat OS repos, and how frontend/mobile/backend contracts are exposed.

## Final Call

```text
AI context:
  every repo gets AGENTS.md + CLAUDE.md
  both point back to context/README.md
  detailed facts live once under /context, not copied everywhere

External app clients:
  REST/JSON over HTTP
  OpenAPI contracts
  generated TypeScript clients for Next.js and React Native

Internal contracts:
  JSON Schema for form DSL, submissions, analytics events, and DLQ repair payloads
  Protobuf allowed for high-volume device telemetry or future internal services
  gRPC allowed behind the API boundary, not directly to browser/RN clients
```

Do not serve web/mobile clients directly with gRPC as the default product API.

## Why Not gRPC Directly To Clients

gRPC and protobuf are different choices:

- Protobuf is a schema/message format.
- gRPC is a network protocol/runtime around service calls.

For Goat OS app clients, direct gRPC is the wrong default:

- Browser clients need gRPC-Web or a proxy layer.
- React Native gRPC adds native dependency/build complexity.
- Debugging field-app traffic is harder than JSON.
- Offline submission queues are easier with plain JSON request bodies.
- Product APIs need normal HTTP semantics: auth headers, caching, retries, upload URLs, gateway logs, curlability.
- The backend is a modular monolith, so service-to-service gRPC is not needed for module calls.

Use REST/JSON for app APIs:

```text
Next.js dashboard/admin -> OpenAPI generated TS client -> app-api / analytics-api
React Native operator   -> OpenAPI generated TS client -> app-api / media-api
Public website          -> OpenAPI generated TS client -> public-api
```

Use protobuf only where it earns its place:

```text
device-gateway -> raw high-volume telemetry
future split service -> internal service calls
AI/media workers -> structured batch jobs if binary contracts help
```

Even then, the public app boundary stays REST/JSON unless a measured workload proves otherwise.

## Contract Stack

```text
OpenAPI
  external app APIs
  admin/mobile/dashboard/public endpoints
  source for generated TypeScript clients

JSON Schema
  Goat OS form DSL
  form submissions
  outbox/domain event payloads
  DLQ repair payloads
  analytics event envelope

Protobuf
  selected internal high-volume streams
  selected device telemetry contracts
  selected internal services after they split out
```

The event envelope must stay explicit and versioned either way:

```text
event_id
event_type
event_version
source_context
subject_type
subject_id
occurred_at
recorded_at
idempotency_key
actor
farm_scope
payload
schema_ref
trace_id
```

## Agent Context Goals

Agents should be able to open any repo and know:

- What product they are in.
- Which architecture docs are authoritative.
- Which files are safe to edit.
- Which commands validate changes.
- Which contracts must not be bypassed.
- Which terms are forbidden/stale.

The structure must be greppable and small enough that agents actually read it.

## Required Files

At workspace root:

```text
<mesha-workspace>/AGENTS.md
<mesha-workspace>/CLAUDE.md
context/README.md
```

At each repo/app root:

```text
AGENTS.md
CLAUDE.md
```

At each major backend module once code exists:

```text
internal/<module>/AGENTS.md
```

At each shared package once code exists:

```text
packages/<package>/AGENTS.md
```

## Drift Rule

`AGENTS.md` is the canonical agent entry file.

`CLAUDE.md` should usually contain only:

```text
@AGENTS.md
```

That keeps Claude and Codex aligned without duplicating instructions.

Repo-level `AGENTS.md` files should be short. They should point to central context docs instead of copying architecture decisions.

## Standard AGENTS.md Shape

```text
# <Repo Or Module> Agent Context

Read first:
- context/README.md
- <repo/module-specific docs>

Purpose:
- what this repo/module owns

Stack:
- runtime/framework/database/vendor SDKs

Do:
- implementation rules
- commands to run
- local patterns to preserve

Do not:
- forbidden shortcuts
- stale architecture
- direct DB/vendor coupling

Key paths:
- important folders/files

Contracts:
- APIs/events/schemas this layer consumes or exposes
```

## Grep Tags

Use these tags in docs and future code comments when helpful:

```text
CTX:
  context/agent notes

ADR:
  architecture decision

PORT:
  adapter interface boundary

EVENT:
  domain/outbox event

FORM:
  form DSL construct

METRIC:
  governed Cube metric

DLQ:
  dead-letter/repair workflow

AUTHZ:
  permission/policy rule

SYNC:
  offline/mobile sync rule

MEDIA:
  proof/video/photo storage rule

DEVICE:
  RFID/camera/scale/ultrasound/collar integration
```

Examples:

```text
ADR: app clients use OpenAPI REST, not direct gRPC.
PORT: MediaStorage hides GCS/S3 implementation.
SYNC: every mobile submission requires idempotency_key.
METRIC: official KPIs must come through Cube.
```

## Skill Reference Structure

Use the Heva-style skill bundle pattern: one `SKILL.md` entry point with a
reference table, plus topic-specific `references/*.md` files. The skill is a
navigation layer, not a second architecture source.

```text
context/agents/skills/goatos-build/
  SKILL.md
  references/
    repo-structure.md
    architecture.md
    forms-sop.md
    contracts-events.md
    frontend-mobile.md
    analytics-infra.md
    execution-plan.md
    existing-repos.md
    security-ops.md
```

When `goatos/` is scaffolded, this canonical skill bundle should be copied or
generated into both agent surfaces:

```text
goatos/.claude/skills/goatos-build/
  SKILL.md
  references/*.md

goatos/.agents/skills/goatos-build/
  SKILL.md
  references/*.md
```

`SKILL.md` is the single place an agent sees all available references and their
"load when" triggers. Each `references/*.md` file is a scoped pointer/playbook
for one topic and links back to canonical `context/` docs.

Do not fork into separate skills such as `goatos-analytics` or `goatos-forms`
unless the trigger surface becomes truly independent. Goat OS is one product;
`goatos-build` is the main navigation skill.

Do not let skills become another place for architecture truth. Skills route and
execute rules; `/context` owns facts.

## Backend Module Agent Files

Every backend module should eventually have:

```text
internal/vaccination/AGENTS.md
internal/tasks/AGENTS.md
internal/forms/AGENTS.md
internal/verification/AGENTS.md
internal/media/AGENTS.md
internal/devices/AGENTS.md
```

Each one should list:

- Owned tables.
- Owned commands.
- Owned queries.
- Emitted events.
- Consumed events.
- Ports used.
- Forbidden cross-module access.
- Tests to run.

Example:

```text
# Vaccination Module Agent Context

Owns:
- vaccination schedule templates
- due vaccination tasks
- vaccination typed events

Does not own:
- goat identity
- workforce roster
- media proof storage
- verification decision

Consumes:
- goat_created
- goat_purchased
- goat_birth_recorded

Emits:
- vaccination_due_generated
- vaccination_recorded
- vaccination_blocked

Ports:
- TaskScheduler
- VaccineInventoryReader
- VerificationRequester
```

## Frontend Package Agent Files

Shared packages should also have agent files:

```text
packages/forms-dsl/AGENTS.md
packages/api-client/AGENTS.md
packages/analytics-client/AGENTS.md
packages/media-client/AGENTS.md
packages/device-client/AGENTS.md
packages/ui/AGENTS.md
```

Each should describe:

- Public exports.
- Compatibility rules.
- Generated-file rules.
- Test commands.
- Examples.

## Non-Negotiables For Agents

- Read `context/README.md` before architecture changes.
- Do not reintroduce old staging labels as architecture.
- Do not bypass app APIs from frontend/mobile.
- Do not put vendor SDK calls throughout product code.
- Do not add direct BigQuery queries to React pages.
- Do not add direct Firestore/GCS writes to the operator app.
- Do not add gRPC as the browser/mobile API without a written ADR replacing this decision.
- Do not duplicate architecture facts in multiple places.

## Summary

Use AI-agent files everywhere, but keep them small and pointer-based.

Use OpenAPI REST/JSON for web/mobile clients.

Use JSON Schema for form/event/analytics contracts.

Use protobuf/gRPC only behind the boundary where high-volume internal workloads justify it.
