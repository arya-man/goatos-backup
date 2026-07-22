# Mesha Leadership Assistant — Access Policy (AUTHORITATIVE)

This document is the single source of truth for WHO uses the leadership assistant
and WHAT they can see. It applies to the in-app chat, the backend assistant API,
and the external MCP server (ChatGPT / Claude connectors). Code, guards, docs,
skills, and agents (Claude and Codex) must conform to this policy. Do not
re-litigate or re-ask these decisions — they are settled.

## 1. Who — exactly 5 users, no one else

The assistant is used ONLY by the CEO/leadership cohort (`ceo_internal`). Current
authorized users (seed values; the list is editable at runtime, see §4):

- ravi@mesha.sg
- manohark@mesha.sg
- manju@mesha.sg
- abhishek@mesha.sg
- aryaman@mesha.sg

No other user gets access — not operators, not managers, not the public. Any
identity outside the explicit allowlist is rejected with zero access.

## 2. What they see — EVERYTHING, no restriction anywhere

These users are CEO-level. There is **NO information restriction of any kind**:

- Every module, every metric, every read: counts, vaccination, feed, procurement,
  workforce/HRMS, inventory, SOP, verification, audit, workflows — all of it.
- Every scope: all parks, all sheds, all cohorts, all dates the tenant holds.
- The **entire governed tool catalog** is exposed (Cube metrics + all read APIs +
  all MCP tools + the ask-leadership tool + SQL fallback).
- Do NOT add per-module hiding, per-scope narrowing, "safe subset", redaction of
  business numbers, or any artificial limit on breadth for these users.

If a future change would narrow what these 5 can see, that change is WRONG unless
this document is explicitly amended first.

## 3. The only invariants (these are NOT information restrictions)

These are correctness/security guarantees, not limits on what the 5 can see:

- **Read-only** — the assistant never writes business data. Writing is not
  "information"; all reads are fully available.
- **Audit** — every request/answer/tool call is logged internally (admin/eng
  debug + audit table). Internal step-trace is never shown in the chat answer.
- **Rate-limit + cost budget** — bounded request rate; never trims what data is
  returned, only how fast.
- **Tenant scope** — answers are scoped to the authenticated tenant (prevents
  cross-tenant bleed). Within the tenant, the 5 see everything.

## 4. Allowlist mechanism — per-email, any domain, runtime-editable

- The allowlist is an **explicit list of full email addresses** (exact,
  case-insensitive match). It is **NOT** a `@mesha.sg` domain-suffix rule —
  multiple workspace domains exist and the set changes over time.
- Stored in config / Secret Manager / DB (not hardcoded). Adding or removing an
  authorized user is a config edit — no code change, ideally no redeploy.
- External MCP (ChatGPT/Claude): access is gated by the OAuth identity presented
  to the Mesha MCP server (server-side), which must match the allowlist,
  regardless of which email the ChatGPT/Claude app itself is logged in with.

## 5. Distinction from the git-identity guard

The commit git-identity guard correctly enforces `@mesha.sg` for repo commits.
That is unrelated to this policy. Assistant/MCP access here is a per-email
allowlist and is domain-agnostic.

## 6. Enforcement

- Backend role gate: `ceo_internal` + allowlist (server-side session / OAuth),
  never a client-side name check.
- `docs/ceo-ai/coverage-matrix.md` + `make leadership-assistant-coverage-guard`
  ensure every leadership read is exposed (no accidental gaps) — the guard exists
  to guarantee COVERAGE, i.e. that the 5 see everything, not to restrict.
- This policy is referenced from AGENTS.md and the `goatos-leadership-assistant`
  skill so Claude and Codex apply it on every future change.
