# Goat OS Skills

Use this file as the human-readable index for Codex, Claude, and future coding
agents. It follows the same pattern as the Heva skill index: one entry point,
then focused reference files by topic.

## Primary Skill

Use the Goat OS build skill for all product, architecture, phase, contract,
backend, frontend, mobile, analytics, infra, and review work:

```text
.agents/skills/goatos-build/SKILL.md
```

Claude discovers the same skill through a symlink:

```text
.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Do not hand-maintain two copies. `.agents/skills/goatos-build/` is the source.

## Agent tool routing (human)

Before starting work, read **`docs/ai/agent-tool-routing.md`**:

- **Cursor** — admin-web UI, mock fidelity, small single-file fixes.
- **Claude Code** (`claude` in repo root) — OpenAPI, backend engine, migrations,
  sqlc, protocol/scheduling, multi-module refactors.

## Context Files

Always start with:

```text
AGENTS.md
context/README.md
.agents/skills/goatos-build/SKILL.md
```

Then load only the needed reference:

```text
.agents/skills/goatos-build/references/repo-structure.md
.agents/skills/goatos-build/references/architecture.md
.agents/skills/goatos-build/references/forms-sop.md
.agents/skills/goatos-build/references/contracts-events.md
.agents/skills/goatos-build/references/frontend-mobile.md
.agents/skills/goatos-build/references/analytics-infra.md
.agents/skills/goatos-build/references/execution-plan.md
.agents/skills/goatos-build/references/phase-prd-trd.md
.agents/skills/goatos-build/references/existing-repos.md
.agents/skills/goatos-build/references/security-ops.md
```

## Phase Workflow

Before implementing any phase:

```text
1. Read docs/phases/README.md.
2. Read the active PRD/TRD listed there.
3. Load .agents/skills/goatos-build/SKILL.md.
4. Load the reference files relevant to the changed area.
5. If the phase introduces a new permanent rule, module, tool, API pattern, or
   workflow, update the relevant skill reference before coding.
6. Keep deep product truth in context/ and docs/phases/.
7. Keep skill references short routing/playbook files, not duplicate specs.
```

Current override:

```text
For Protocol Engine Phase 0 (PHC vaccination, feed direction, protocol config,
obligations, inventory ledger), load .agents/skills/goatos-build/SKILL.md and
then docs/protocol-engine/* and docs/phc-vaccination/*.
```

After implementing any phase:

```text
1. Compare actual code, migrations, contracts, tests, adapters, and workflows
   against the phase PRD/TRD.
2. Update PRD/TRD if implementation intentionally changed scope or behavior.
3. Update context/ if the change affects long-lived architecture/product truth.
4. Update .agents/skills/goatos-build/references/ if agents need new routing or
   rules for future work.
5. Update AGENTS.md only for new always-on repo rules.
6. Keep CLAUDE.md and CODEX.md as shims unless the agent tool itself requires
   a new shim.
7. Run guardrails and leave docs/code in sync before marking the phase done.
```

## Rules

- One skill source, no duplicate copies.
- `CLAUDE.md` and `CODEX.md` are shims to `AGENTS.md`.
- Skill references point back to `context/` and active phase docs.
- Admin-web/operator UI copy/options/navigation/table/filter/chip/drawer truth is
  backend-contract owned; see `AGENTS.md`, `apps/admin-web/AGENTS.md`, and
  `context/frontend/admin-web-backend-ui-contract.md` before frontend work.
- Hooks call shared scripts in `tools/agent-hooks/`.
- CI is the hard gate; hooks are fast feedback.
- Do not create many skills up front. Add a new skill only when the trigger is
  truly independent from `goatos-build`.
