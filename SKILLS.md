# Goat OS Skills

Use the Goat OS build skill for Codex, Claude, and future agents:

```text
.agents/skills/goatos-build/SKILL.md
```

Claude discovers the same skill through:

```text
.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Rules:

- One skill source, no duplicate copies.
- Skill references point back to `context/`.
- Hooks call shared scripts in `tools/agent-hooks/`.
- CI is the hard gate; hooks are fast feedback.
