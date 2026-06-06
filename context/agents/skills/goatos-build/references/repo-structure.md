# Repo Structure Reference

Load this when creating/scaffolding `goatos/`, deciding where docs live, adding
agent files, or organizing the monorepo.

Canonical docs:

- `context/execution/target-repo-structure.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/README.md`

Core rule:

```text
goatos/ is a structured monorepo, not a dumping ground.
Existing cloned repos stay outside as reference until specific code is migrated.
```

Expected top-level shape:

```text
goatos/
  context/
  contracts/
  backend/
  apps/admin-web/
  apps/operator-mobile/
  packages/
  analytics/
  infra/
  load-tests/
  tools/
  .claude/skills/goatos-build/
  .agents/skills/goatos-build/
```

Agent files are thin pointers. `SKILL.md` is the reference index. Deep product
truth lives in `context/`.

