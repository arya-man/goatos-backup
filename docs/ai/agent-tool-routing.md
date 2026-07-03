# Agent Tool Routing — Cursor vs Claude Code

Human-facing playbook for Goat OS. Read this **before starting a task** to pick
the right agent surface. You do **not** need another IDE — stay in this repo and
use two tools from the same checkout.

| Tool | Where | Repo wiring |
| --- | --- | --- |
| **Cursor** | This IDE (Agent / Plan / Chat) | `.cursor/rules/`, `.cursor/hooks.json` |
| **Claude Code** | Terminal: `claude` in repo root | `.claude/settings.json`, `.claude/skills/` |

Both tools share the same rules (`AGENTS.md`), hooks (`tools/agent-hooks/`), and
skills (`.agents/skills/goatos-build/`). Claude Code’s hook wall is older and
slightly more battle-tested for long backend sessions; Cursor now has equivalent
project hooks — use Claude Code when the **task shape** below says so, not
because Cursor is “missing hooks.”

---

## Quick decision

```
Is the task mostly UI/layout/mock fidelity in apps/admin-web?
  └─ YES → Cursor

Is it a single small fix in one file (< ~50 lines, one module)?
  └─ YES → Cursor

Does it touch OpenAPI + backend compile + generated client + frontend contract?
  └─ YES → Claude Code

Does it touch protocol engine, obligations, scheduling, migrations, or sqlc hot paths?
  └─ YES → Claude Code

Does it span 3+ packages or need blast-radius / impact analysis before editing?
  └─ YES → Claude Code

Are you planning architecture, phase closure, or “review the whole slice”?
  └─ YES → Claude Code (Plan first, then implement)
```

When unsure: **start in Cursor Plan mode** for scope; if the plan touches
contracts + backend + multiple modules, **hand implementation to Claude Code**.

---

## Use Cursor (stay in this IDE)

Best for fast iteration with files open and visual feedback.

| Task | Why Cursor |
| --- | --- |
| Admin-web UI, CSS, mock fidelity, drawer/modal anatomy | Inline preview, `@` file context, screenshot compare |
| Single-component or single-page fix | Low coordination overhead |
| Copy/label wiring when bootstrap contract already exists | Read generated types + one feature folder |
| Local dev server restart, quick typecheck/lint | Terminal + editor in one place |
| Docs edits in one file | Simple diff review |
| “Where is X?” orientation (after CRG/graph query) | Chat is enough |

**Cursor gates before push (admin-web):**

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
```

---

## Use Claude Code (terminal in this repo)

Best for depth, cross-module consistency, and contract-first work.

| Task | Why Claude Code |
| --- | --- |
| OpenAPI / `contracts/openapi/*` changes + regenerate clients | Fewer missed codegen steps |
| `backend/internal/adminui` bootstrap / compiler changes | Large surface; contract drift risk |
| Protocol engine, Preventive Care vaccination scheduling, obligation kernel | Multi-module + docs (`docs/protocol-engine/`, `docs/preventive-care-vaccination/`) |
| Migrations + sqlc + repository changes on hot paths | Needs query-plan / idempotency discipline |
| Refactors across backend modules | CRG + hooks + long context |
| Phase PRD/TRD closure (code vs spec diff) | Wide read, narrow write |
| Process-integrity, Action Center, Calendar, Workflow backends | Operational-kernel rules span many packages |
| Feed direction, counts projections, inventory ledger (when in scope) | Generic engine + domain tables |

**How to start:**

```bash
cd /path/to/goatos   # repo root
claude               # Claude Code CLI (install from Anthropic if missing)
```

Paste a scoped prompt (template below). Claude loads `.claude/settings.json`
hooks automatically.

---

## Do not use

| Approach | Why |
| --- | --- |
| Another IDE “for Claude” | Loses Cursor UI workflow and duplicate hook setup |
| Bare `go run ./cmd/api` without env | Fails; see `docs/ai/README.md` / `make dev-local` |
| One mega chat for unrelated verticals | Scope drift; start fresh per slice |
| Frontend-only agent for bootstrap copy changes | Labels must come from backend contract first |

---

## Prompt templates

### Cursor (light task)

```text
Slice: [e.g. Config protocol rules editor only]
Scope lock: do not touch feed-direction / unrelated verticals
Read: apps/admin-web/AGENTS.md + mock section for [screen]
Task: [specific change]
Gate: check:mock-fidelity if UI changes
```

### Claude Code (heavy task)

```text
Slice: [e.g. vaccination scheduling algorithm — backend only]
Read first: docs/preventive-care-vaccination/vaccination-scheduling-algorithm-review.md
            backend/AGENTS.md + .agents/skills/goatos-build/SKILL.md
4-layer lookup: CRG impact before editing
Task: [specific outcome + acceptance tests]
Do not: scan full herd, hardcode UI labels, expand scope beyond slice
Handoff: list files changed, tests run, contract/OpenAPI regen if any
```

---

## Current Goat OS slices (reference)

| Slice | Prefer |
| --- | --- |
| Admin-web mock-faithful UI | **Cursor** |
| `/config` protocol rules + bootstrap copy | **Claude** (backend contract) then **Cursor** (render) |
| Preventive Care vaccination execution read models | **Claude** |
| Scheduling / calendar due-work algorithm | **Claude** |
| OpenAPI + generated TS client | **Claude** |
| Migrations / sqlc | **Claude** |
| Single bugfix in known file | **Cursor** |

---

## One-time machine setup (both tools)

```bash
make ai-setup
make ai-rebuild AI_BACKEND=auto
make ai-doctor
```

Restart Cursor after pulling hook changes. Run `make ai-rebuild` after large
merges so CRG/Graphify stay useful as the repo grows.

---

## Related docs

- `docs/ai/README.md` — hooks, graphs, local setup
- `AGENTS.md` — always-on product and engineering rules
- `SKILLS.md` — skill index
- `.cursor/rules/goatos-core.mdc` — Cursor router pointer
