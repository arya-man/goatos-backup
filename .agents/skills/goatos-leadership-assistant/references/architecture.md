# Leadership Assistant — Architecture map

Full production shape the coverage work plugs into. Detailed capability list and
build state live in `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md`,
`docs/ceo-ai/mcp-toolbox-plan.md`, and
`docs/ceo-ai/leadership-assistant-developer-guide.md`.

## Ports & flow (hexagonal, server-owned brain)

```
admin-web chat  ──HTTP/SSE──▶  backend/internal/ceoai
                                  app/orchestrator ─▶ planner (Vertex/Gemini)
                                  app/router (4-tier):
                                     1 cube/      → Cube Core (governed metrics)
                                     2 http/      → Mesha read APIs
                                     3 toolbox/   → MCP Toolbox (ceo_ai.* tools)
                                     4 postgres/  → read-only SQL (mesha_ceo_readonly)
                                  app/guard (injection, moderation, sqlguard)
                                  adapters/{vertex,cube,toolbox,postgres,cache}
                                  persistence (conversations, messages, audit)
```

- **Gemini never** holds DB creds, executes SQL, or decides permissions. It
  classifies domain, chooses an allowed tool, extracts params, and (only for
  tier-4) drafts SQL the server validates.
- Tenant + role are injected **server-side** from the session; user text and tool
  output are data, never instructions.

## Agentic loop bound

The multi-tool loop is bounded by `MESHA_AI_MAX_STEPS`: per-step deadline,
deterministic stop condition, step-count metric. Prevents unbounded re-plan cost
against Vertex + Cube + DB.

## Safety layers

1. Prompt-injection guard (strip/deny override patterns; enforce session scope).
2. Vertex safety settings + pre/post moderation (off-domain → scoped refusal).
3. Read-only SQL guard (single SELECT, tenant predicate, `LIMIT<=100`,
   `ceo_ai.*` allowlist, no DML/DDL/multi-statement).
4. Answer-grounding validator: every number/label in the answer must trace to a
   citation-backed tool result, else it is stripped / downgraded to "cannot
   ground". Never fabricate zero-as-success.

## Persistence

`ceo_ai_conversation` / `ceo_ai_message` / `ceo_ai_feedback` / `ceo_ai_audit` /
`ceo_ai_usage` / `ceo_ai_trace`, tenant+user scoped, keyset-paginated,
soft-delete + purge policy. `conversation_id` and `citations` are allowed
response fields; step traces are NOT.

## Streaming & response contract

SSE token stream + a terminal event. User response fields are exactly:
`answer, source, mode, request_id, (tokens), citations, conversation_id`.
Step traces / tool timeline are internal (admin-only debug surface).

## Resilience & bounds

Per-dependency timeout, bounded retry+backoff, circuit breaker, fallback ladder
(Cube down → API → Toolbox → friendly degrade — never empty-swallow). Rate limit
+ token/cost budget per tenant/user. Concurrency semaphore on Vertex + the
readonly pool.

## Eval & observability

`tools/ceo-ai/eval` golden set (census, vaccination, feed/shifting/procurement,
ops/workforce, adversarial) with SQL oracles + a grounding assertion, run against
a seeded local DB and wired into `make ci-local`. Spans/counters (requests,
tokens, cost, tier-hit, cache-hit, safety-block) via
`backend/internal/platform/observability`.

## Secrets

All secrets in Google Secret Manager (project `goatos-stg`, org `vgoats.com`) +
GitHub Actions secrets (repo `vgoats/goatos`), including `mesha_ceo_readonly` /
`mesha_cube_readonly`. Local dev pulls into a gitignored `.env.ceo-ai.local` via
a documented gcloud fetch. Never commit a secret value — names + steps only.
