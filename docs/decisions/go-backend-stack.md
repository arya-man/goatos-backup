# Go Backend Stack Decision

Status: accepted

Date: 2026-06-08

## Context

Goat OS needs Spring-grade layering and operational rigor, but the Go backend
should not depend on a large application framework to create those layers.
Architecture boundaries are enforced through packages, interfaces, contracts,
database constraints, tests, and module ownership.

This decision applies to Goat OS backend implementation unless a later ADR
explicitly replaces it.

## Decision

Use a Go modular monolith with explicit layers:

```text
domain/
app/ or usecase/
ports/
adapters/http/
adapters/postgres/
platform/
cmd/api/
cmd/worker/
```

For the stack:

```text
HTTP transport: stdlib net/http or chi
API contracts: OpenAPI REST/JSON for web/mobile/dashboard clients
Database access: pgx + sqlc-style typed SQL
Migrations: goose-style plain SQL migrations
Dependency wiring: explicit constructors in bootstrap/wiring code
Observability: slog + OpenTelemetry
gRPC/protobuf: none in Phase 1; internal-only later by ADR if a workload proves it
```

Do not use by default:

```text
Gin/Fiber/Echo as the architecture surface
GORM or another ORM for canonical operational data
Runtime DI containers such as fx/dig to mimic Spring autowiring
Direct gRPC for browser or React Native clients
NoSQL as canonical truth for Phase 1 identity data
```

## Rationale

Goat OS must support 100k to 1M goats with explicit query control, idempotency,
partitions, deferrable triggers, outbox workers, and reconciliation workflows.
Those requirements favor visible SQL and explicit transactions over ORM magic.

`pgx + sqlc` maps tables to Go through query files:

```text
Postgres table -> SQL query -> generated Go row/params -> repository adapter -> domain/app model
```

This keeps query shape reviewable, index-aware, and testable. It also handles
custom Postgres features such as partitions, `SKIP LOCKED`, upserts, generated
columns, triggers, and deferrable constraints without fighting an ORM.

`net/http` or chi keeps HTTP handlers as thin transport adapters. Framework
context types must not leak into domain or app layers.

Plain SQL migrations keep Phase 1 schema work transparent because the schema
uses custom DDL: sequences, partitioned tables, default/backstop partitions,
partial unique indexes, deferrable triggers, and seed/reference data.

Explicit constructor wiring makes adapters replaceable without a runtime DI
container. This matches the existing ports/adapters architecture and keeps
agent-generated code easier to grep and review.

## Consequences

- Handlers stay thin and call app/usecase services.
- Domain/app layers must not import HTTP framework, database driver, cloud SDK,
  or vendor packages.
- Postgres adapters own SQL and map generated rows to domain/app DTOs.
- Module boundaries matter more than framework conventions.
- Any move to Gin, GORM, a DI container, client gRPC, protobuf contracts, or
  NoSQL canonical identity storage requires a new ADR.
