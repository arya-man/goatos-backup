# Decisions

Use this folder for future ADRs when a decision changes the locked architecture.

Do not rewrite history silently. If a frozen decision changes, add an ADR and
link it from `context/README.md`.

Active ADRs:

- `docs/decisions/go-backend-stack.md` - Go backend stack: net/http or chi,
  pgx + sqlc-style typed SQL, goose-style SQL migrations, explicit wiring, no
  ORM/DI container by default, no Phase 1 gRPC/protobuf.
