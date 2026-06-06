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

Do not:

- Do not hand-maintain the same DTO shape in Go and TypeScript.
- Do not create one mega universal Goat schema.
