---
name: crg
description: CRG-first code investigation for Goat OS. Runs code-review-graph tools to answer structural questions, find callers/callees, assess blast radius, and orient before reading files. Invoke for any code question in this repo.
version: 0.1.0
user-invocable: true
argument-hint: "[query: what you want to find/understand]"
---

# CRG Scan Skill

Mandatory CRG-first pass for any code question in this repo. Run these tools
before opening files or grepping.

## Step 1 — Orient (always run first)

Run in parallel:
- `get_architecture_overview_tool` (repo_root: /Users/ravi/mesha/goatos)
- `list_communities_tool` (repo_root: /Users/ravi/mesha/goatos)

## Step 2 — Locate (based on the user's question)

Run `semantic_search_nodes_tool` with the key terms from the question.
repo_root: /Users/ravi/mesha/goatos

If the question is about a specific function/class name, use
`query_graph_tool` with pattern=callers_of or callees_of.

## Step 3 — Impact (if code is changing or being reviewed)

Run `get_impact_radius_tool` on affected nodes.
Run `detect_changes_tool` if there is a diff or recent edit.
Run `get_affected_flows_tool` to see which execution paths are touched.

## Step 4 — Context (fill gaps the graph cannot see)

After graph pass: use Read/Grep ONLY for:
- HTTP route strings, middleware registration
- Config values, constants, error messages
- Uncommitted or unstaged code

## Rules

- NEVER skip Step 1 even for "simple" questions — orientation is cheap
- NEVER read files first without running CRG — graph replaces most file reads
- One graph query replaces 5-10 grep/read cycles
- repo_root is ALWAYS /Users/ravi/mesha/goatos for this project

## Graph blind spots (fall back to native)

- HTTP route strings (`/api/v1/...`)
- Config/env values
- Middleware wired via reflection
- String-keyed feature flags
- Exact caller counts (graph can undercount ~30%)
