# Workspace-root `/code-review` delegator (reproduction template)

This repo ships the real review skill at
`.claude/skills/goatos-code-review/` (a symlink to
`.agents/skills/goatos-code-review/`). When you open Claude Code **with this repo
as the session root**, that skill registers natively and `/code-review` works —
you do **not** need anything in this doc.

This doc exists only for the **multi-repo workspace** case: a maintainer who opens
Claude Code at a parent directory that contains this `goatos` checkout alongside
other repos. At that parent root, a skill living in the `goatos/` subdir is **not**
auto-registered, so a thin **delegator** skill at the workspace root is needed to
(a) shadow the generic ECC / claude-plugins-official `code-review` and (b) route
`/code-review` into this repo's Goat OS review gate.

That delegator is a maintainer-local file that lives **outside** this repo (it sits
above the `goatos/` checkout, in an ungit-tracked workspace root), so it is not
committed here. This template is the version-backed source of truth for recreating
it if the workspace is rebuilt.

## When you need the delegator

| You run Claude Code at… | Need the delegator? |
|---|---|
| this `goatos` repo root (normal clone) | **No** — the in-repo skill registers natively |
| a parent workspace that contains `goatos/` + other repos | **Yes** — recreate it from the template below |

## Install location

Create the file at your workspace root (the directory that contains your `goatos`
checkout), NOT inside this repo:

```
<MESHA_WORKSPACE>/.claude/skills/code-review/SKILL.md
```

Replace `<MESHA_WORKSPACE>` with your actual workspace root path (the parent of
your `goatos` checkout). The delegator uses absolute paths on purpose: the
workspace-root session needs to reach the in-repo skill by full path.

## Template

Copy the block below into that file, substituting `<MESHA_WORKSPACE>` with your
workspace root path in the three path references:

````markdown
---
name: code-review
description: Review or audit a Goat OS / Mesha change (diff, branch, PR, or path) for kernel correctness, 1-5M-animal scale safety, hexagonal boundaries, backend + frontend + Android-mobile architecture (Room SSOT / offline / pagination / memory), DB-schema/migration lock-safety, and vaccination/obligation business-rule fidelity — applying root-cause-vs-band-aid, anti-pattern, and blast-radius lenses and returning a bug list (or approval). In the Mesha workspace this REPLACES the generic ECC/official code-review skill. Orchestrates CRG, Graphify, RTK, and repowise. Use when reviewing code, auditing a diff, or gating a change before push.
version: 0.1.0
user-invocable: true
argument-hint: "[target: diff | branch | PR | path — what to review]"
---

# Mesha `/code-review` → Goat OS Code Review

In the Mesha workspace (`<MESHA_WORKSPACE>`), `/code-review` runs the **Goat OS
review gate** — NOT the generic ECC / claude-plugins-official `code-review`.

This is a thin delegator. When invoked, do exactly this:

1. **Load the canonical skill, then the references its scope-detection selects.**
   Read `<MESHA_WORKSPACE>/goatos/.claude/skills/goatos-code-review/SKILL.md`
   first, then load only the reference(s) its Scope-detection / Proportionality
   rules select for the review target (progressive disclosure — a small isolated
   change loads one lens; a cross-layer or wide-blast-radius change loads several,
   including the consumer lens for a contract/DTO change). Available references
   under `.../goatos-code-review/references/`: `toolchain.md` (always),
   `kernel-and-scale.md`, `backend.md`, `frontend.md`, `mobile.md`,
   `business-rules.md`. Follow the skill's **Output contract** for the result
   (bug list only, or approval when clean).

2. **Follow that skill exactly** for the review target given in `$ARGUMENTS`.
   If no target is given, review the current working diff (`git diff` /
   `git status` in the relevant repo, defaulting to `<MESHA_WORKSPACE>/goatos`).

3. **Do not** fall back to generic ECC `code-review` behavior. The Goat OS skill
   is authoritative: it drives CRG, Graphify, RTK, and repowise, knows every
   layer, and holds the scale + kernel + vaccination-rule checklists.

> Why this file exists: the committed skill lives in the `goatos/` subdir
> (`goatos/.claude/skills/goatos-code-review`), so it is not registered when a
> session starts at the workspace root. This root-level `code-review` skill both
> shadows the official plugin skill and routes to the Goat OS gate.
````

## Keeping this in sync

If the in-repo skill's reference set or Output contract changes, update the
`Available references` line and any behavior summary in **both** the live
workspace delegator and this template, so a rebuild reproduces current behavior.
The delegator is a router only — all review logic stays in
`.agents/skills/goatos-code-review/`.
