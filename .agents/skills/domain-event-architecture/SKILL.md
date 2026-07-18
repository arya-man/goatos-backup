---
name: domain-event-architecture
description: Use when adding or reviewing any Goat OS backend, frontend, or mobile feature that creates, imports, updates, moves, closes, or consumes business state through domain events, outbox, CRUD, sheet imports, mobile offline writes, shifting, dead birth, feed direction, vaccination, or future operational modules.
version: 0.1.0
user-invocable: true
argument-hint: "[feature/module/event]"
---

# Domain Event Architecture Lens

Load this lens before changing any business mutation path in backend, admin-web,
or mobile. It fronts the canonical contract:

```text
context/architecture/domain-event-integration-contract.md
context/architecture/domain-event-registry.json
.agents/skills/goatos-build/references/contracts-events.md
.agents/skills/goatos-code-review/references/kernel-and-scale.md
```

Also load `frontend.md` for admin-web changes and `mobile.md` for Android/offline
changes.

Hard rule:

- Register the producer, event, consumer, replay/DLQ behavior, and E2E proof in
  `context/architecture/domain-event-registry.json`.
- Backend canonical state, audit when relevant, and transactional outbox must be
  one unit.
- Frontend/mobile send idempotent commands and render backend-owned contracts;
  they do not create business follow-up state.
- Direct animal-table writers must already be registered, or the guard fails.

Required check:

```bash
make domain-event-architecture-guard
```
