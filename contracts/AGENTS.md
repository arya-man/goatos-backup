# Contracts Agent Context

Read first:

- `../context/execution/next-contracts.md`
- `../context/agents/ai-agent-context-and-protocols.md`

Purpose:

- OpenAPI, JSON Schema, protobuf, examples, and generated-client source truth.

Do:

- Keep one source per contract family.
- Generate clients from contracts.
- Version events, forms, decisions, and submissions.
- Treat backend-owned UI presentation as an app API contract. If frontend needs
  visible navigation, page/section/table labels, filters, sort keys, chips,
  drawer/action copy, disabled reasons, or summary/detail field sets, publish it
  through OpenAPI and regenerate clients before handoff.

Do not:

- Do not hand-maintain the same DTO shape in Go and TypeScript.
- Do not let React constants become the canonical source for product labels,
  route availability, table/filter semantics, action availability, or disabled
  reasons.
- Do not create one mega universal Goat schema.
