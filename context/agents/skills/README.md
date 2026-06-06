# Goat OS Agent Skill Bundles

This folder holds canonical source for Goat OS agent skills.

Rule: skill bundles route agents to the right context and playbooks. They do not
replace `context/` as the source of architecture truth.

When `goatos/` is scaffolded, these bundles should be copied or generated into:

```text
goatos/.claude/skills/
goatos/.agents/skills/
```

Both generated copies must stay identical, or CI should fail.

