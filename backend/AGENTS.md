# Backend Agent Context

Read first:

- `../context/README.md`
- `../context/architecture/final-architecture.md`
- `../context/execution/next-contracts.md`
- `../docs/decisions/go-backend-stack.md`
- `../.agents/skills/goatos-build/references/backend-impl.md`
- `../docs/phases/phase-01-goat-passport/BUILD-STATUS.md`

Purpose:

- Go modular monolith, workers, migrations, and platform adapters.

Do:

- Keep handlers thin and domain logic inside modules.
- Keep module tables owned by their module.
- Use transactions, idempotency keys, outbox, bounded workers, and OpenTelemetry.
- Use net/http or chi for HTTP adapters, pgx + sqlc-style typed SQL for
  Postgres adapters, goose-style plain SQL migrations, and explicit constructor
  wiring.

Do not:

- Do not scan the full herd in API paths.
- Do not write another module's tables directly.
- Do not proxy video bytes through APIs.
- Do not add Gin, GORM/ORM, runtime DI containers, direct client gRPC, or
  protobuf in Phase 1 without a new ADR.
