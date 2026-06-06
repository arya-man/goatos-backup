# Backend Agent Context

Read first:

- `../context/README.md`
- `../context/architecture/final-architecture.md`
- `../context/execution/next-contracts.md`

Purpose:

- Go modular monolith, workers, migrations, and platform adapters.

Do:

- Keep handlers thin and domain logic inside modules.
- Keep module tables owned by their module.
- Use transactions, idempotency keys, outbox, bounded workers, and OpenTelemetry.

Do not:

- Do not scan the full herd in API paths.
- Do not write another module's tables directly.
- Do not proxy video bytes through APIs.
