---
name: crg
description: CRG-first code investigation for Goat OS. Routes by task shape through code-review-graph tools to answer structural questions, find callers/callees, assess blast radius, and avoid broad file scans.
version: 0.1.0
user-invocable: true
argument-hint: "[query: what you want to find/understand]"
---

# CRG Scan Skill

Mandatory CRG-first pass for graph-shaped code questions in this repo. Choose
the cheapest entry point that matches the task shape before opening many files
or grepping broadly.

## Step 1 — Route by task shape

- Cold orientation, diff review, or unclear task: run `get_minimal_context_tool`
  first, then one targeted graph query.
- Known symbol/function/class: skip orientation and run `query_graph_tool`
  directly with `callers_of`, `callees_of`, `imports_of`, or `tests_for`.
- Keyword/domain lookup: run `semantic_search_nodes_tool`, then a targeted
  `query_graph_tool`.
- Single file/function body request: read the file directly; use CRG afterward
  only if impact or callers matter.

repo_root is the absolute path of your goatos checkout:
`git rev-parse --show-toplevel`

## Step 2 — Impact (if code is changing or being reviewed)

Run `detect_changes_tool` if there is a diff or recent edit.
Run `get_impact_radius_tool` on affected nodes.
Run `get_affected_flows_tool` to see which execution paths are touched.

## Step 3 — Context (fill gaps the graph cannot see)

After graph pass: use Read/Grep ONLY for:
- HTTP route strings, middleware registration
- Config values, constants, error messages
- Uncommitted or unstaged code

## Rules

- Do not run architecture overview/community listing as a default ritual.
- Do not use graph for a plain one-file read unless impact is relevant.
- One graph query replaces 5-10 grep/read cycles when the question is traversal-shaped.
- repo_root is the absolute path of your goatos checkout (`git rev-parse --show-toplevel`)

## Graph blind spots (fall back to native)

- HTTP route strings (`/api/v1/...`)
- Config/env values
- Middleware wired via reflection
- String-keyed feature flags
- Exact caller counts (graph can undercount ~30%)
