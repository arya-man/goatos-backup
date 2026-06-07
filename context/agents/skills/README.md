# Goat OS Agent Skill Bundles

This folder holds canonical source for Goat OS agent skills.

Rule: skill bundles route agents to the right context and playbooks. They do not
replace `context/` as the source of architecture truth.

The active repo skill source is now:

```text
goatos/.agents/skills/
```

Claude discovers the same skill through:

```text
goatos/.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Do not hand-maintain duplicate skill copies.
