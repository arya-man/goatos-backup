# Internal Ops Assistant PRD

Status: draft for implementation review.

## Summary

The Internal Ops Assistant is a private Goat OS assistant for monitoring and
operating the Mesha/VGoats herd-animal business. It lets authorized internal users ask
natural-language questions such as:

```text
Which animals missed yesterday's PPR drive in Shed A?
When is my next vaccination drive?
Which parks have overdue preventive care work today?
Which operators own unresolved blockers?
Show me the animals that need catch-up action this week.
Why is this animal not eligible for this drive?
```

The assistant is not a public chatbot and not a standalone intelligence layer.
It is a role-aware command lens over Goat OS APIs, read models, policy docs,
audit trails, and controlled internal read tools. It must answer with source
filters, source routes or rows, timestamps, and scope assumptions so leadership
and field teams can trust the result.

Build relationship:

```text
Internal assistant
  -> tool planner and answer composer
  -> Goat OS OpenAPI tools
  -> controlled ops-query APIs for gaps
  -> canonical Postgres/read models, SOP docs, and analytics boundaries
```

## References

- `context/architecture/operational-kernel.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/analytics/final-analytics-infra.md`
- `docs/decisions/high-scale-dashboard-projections.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`
- `docs/features/critical-animal-action-guardrails.md`
- Existing vaccination APIs in `contracts/openapi/app-api.yaml`
- Existing permission routes in `backend/internal/permissions/routes.go`

## Product Goals

- Give CEO, COO, directors, park heads, verifiers, and operators a fast way to
  ask operational questions across Goat OS without learning every dashboard.
- Answer process-integrity questions: what was expected, what happened, where it
  broke, who owns the next action, what is due by when, and what evidence exists.
- Start with read-only monitoring and drilldown, then add tightly gated actions
  such as nudge, snooze, assign, acknowledge, or create follow-up task.
- Use existing Goat OS OpenAPI endpoints first.
- Add controlled backend ops-query APIs where no good endpoint exists.
- Keep role and scope enforcement inside backend tools, never inside prompts.
- Make ambiguous questions safe by applying deterministic scope defaults or
  asking a clarification question.
- Use the fixed Goat OS business calendar, currently Asia/Kolkata, for relative
  operational dates such as "today" and "yesterday" until a future timezone ADR
  replaces the India-only calendar.
- Return honest "not available yet" answers when the needed domain API or read
  model does not exist.
- Preserve auditability: answer provenance, tool calls, filters, actor, role,
  tenant, timestamp, and data freshness must be inspectable.

## Non-Goals

- Do not expose the assistant to customers, investors, vendors, or public users.
- Do not let the LLM run arbitrary production SQL.
- Do not let the LLM create canonical truth, medical decisions, identity merges,
  protocol schedules, or high-risk animal state changes.
- Do not replace dashboards, Action Center, Calendar, Control Tower, Passport,
  or SOP execution. The assistant links to and summarizes those surfaces.
- Do not use frontend state, screenshots, Slack memory, Sheets, or model memory
  as operational truth.
- Do not expose retired, ignored, or forbidden source-rule branches through
  retrieval. In Preventive Care vaccination, dam/mother vaccination status must
  not be surfaced as a branch, question, option, or scheduling input.
- Do not rebuild every Goat OS domain in v1. Domains become answerable when
  their canonical APIs or ops-query views exist.
- Do not treat analytics warehouses as product truth. Official KPIs must flow
  through governed analytics boundaries.

## Users

```text
CEO / COO / superadmin
  company-wide monitoring, exception review, drilldown, trend questions,
  cross-park accountability, and escalation follow-up.

Vertical director
  domain-wide monitoring for a vertical such as Preventive Care, Feed,
  Procurement, Breeding, Sales, or Finance, within granted scope.

Park head
  park-scoped monitoring, unresolved blockers, operator ownership,
  upcoming drives, shed exceptions, and catch-up planning.

Verifier
  verification queues, disputed evidence, rework, completion history, and
  exception aging within assigned scope.

Operator / health worker
  assigned shed or task scope, today's due work, next drive, missed work,
  required proof, and personal action list through an explicit scoped operator
  assistant-read path. V1 must not solve this by granting broad tenant-level
  vaccination/calendar/obligation read access to all operators.

Engineer / internal admin
  evals, tool coverage, trace review, data-quality gaps, and safe read-only
  debugging in non-production or approved admin contexts.
```

## Question Classes

The assistant must support broad routing categories instead of hundreds of
hand-authored intents:

| Class | Examples |
| --- | --- |
| Status lookup | "What is due today in CPT?", "Which animals missed yesterday's PPR drive?" |
| Next action | "Who owns this blocker?", "What should Shed A do next?" |
| Schedule | "When is my next drive?", "What drives are coming this week?" |
| Entity drilldown | "Show animal 123 passport", "Why is this animal overdue?" |
| Exception review | "Show missed, deferred, or overdue work by park." |
| Policy explanation | "Why was this animal skipped?", "What is the PPR catch-up rule?" |
| History and evidence | "Who marked this complete?", "What proof was uploaded?" |
| Analytics summary | "Which parks have the most overdue work this month?" |
| Safe action | "Nudge the owner", "Snooze this drive", "Assign follow-up." |
| Handoff | "I cannot answer because no API exists yet", "Ask which shed." |

## Source Of Truth

Runtime answer authority follows this order:

1. Goat OS backend APIs and generated OpenAPI clients.
2. Backend-owned ops-query APIs/read models for assistant-shaped questions.
3. Canonical Postgres tables behind module-owned repositories.
4. Governed analytics semantic layer for official KPI questions.
5. Versioned SOP/protocol/policy docs for explanation only.
6. Model inference only for wording, routing, and summarization.

The assistant may use private source docs and knowledge graphs during design and
evals, but production answers about the current business must come from runtime
Goat OS data surfaces.

## Existing Vaccination Coverage

The first production domain should be Preventive Care vaccination because it
already has process-integrity APIs and clear examples.

| Need | Existing surface |
| --- | --- |
| Missed/due/overdue work | `GET /vaccination/action-center` |
| Protocol adherence | `GET /vaccination/adherence` |
| Leadership summary | `GET /control-tower/vaccination` |
| Drive execution list | `GET /vaccination/execution` |
| Shed drilldown | `GET /vaccination/execution/sheds/{shed_id}` |
| Protocol/cohort matrix | `GET /vaccination/operations` |
| Calendar drives | `GET /calendar/vaccination/events` |
| Calendar drive detail | `GET /calendar/vaccination/events/{event_id}` |
| Drive targets | `GET /calendar/vaccination/events/{event_id}/targets` |
| Drive history | `GET /calendar/vaccination/events/{event_id}/history` |
| Nudge/snooze/escalation | `POST /calendar/vaccination/events/{event_id}/*` |
| Verification queue | `GET /vaccination/verification-queue` |
| Animal passport / vaccination status | `GET /goats/{goat_id}/passport` legacy route name |

Known gap: there is not yet one perfect chatbot-shaped endpoint for every
natural-language question. For example, "Which animals missed yesterday's PPR
drive in Shed A?" may need event lookup, target lookup, protocol/vaccine
resolution, and as-of missed-state reconstruction. Production should add a thin
ops-query facade for these compound questions instead of exposing raw SQL to the
LLM.

## Required Ops-Query Facade

When an existing domain API is too dashboard-shaped or requires risky client
stitching, build a backend-owned ops-query endpoint/tool.

Required v1 vaccination tools:

```text
resolve_business_scope(actor, phrase?)
resolve_location(phrase, allowed_scope)
resolve_vaccine_or_protocol(phrase, date?)
get_missed_vaccination_targets(date, vaccine_or_protocol?, park_id?, shed_id?)
get_next_vaccination_drives(scope, park_id?, shed_id?, vaccine_or_protocol?)
get_vaccination_catchup_plan(animal_id, vaccine_or_protocol?)
get_vaccination_catchup_candidates(scope, park_id?, shed_id?, vaccine_or_protocol?, week?)
get_vaccination_exception_summary(scope, date_range, group_by)
```

These tools may read from Postgres through module-owned repositories or
assistant-specific read models, but they must enforce tenant, RBAC, scope,
pagination, row limits, query timeouts, and audit at the backend boundary.

## Correctness Requirements

Date-specific questions must be answered as of the requested business date, not
from whatever the current row status happens to be when the user asks.

For missed vaccination questions:

- "Missed yesterday" means the target was due inside the requested business-day
  window and, by answer time, has no accepted completion whose
  `administered_at` belongs to that business-day window or the accepted catch-up
  window for that obligation. Verification/acceptance may happen later than
  administration; do not classify an animal missed for day D only because the
  D administration was accepted on D+1.
- The query must reconstruct effective state from due windows, accepted
  completions keyed by `administered_at`, recorded-but-not-accepted completion
  evidence, and obligation status events.
- It must not key date-specific completion on verification time,
  `verified_at`, or obligation `completed_at`.
- It must not rely only on today's `status = missed` row value.
- Late completion after the requested date must not erase the historical missed
  answer for that date, unless the accepted completion's `administered_at`
  proves the dose was administered inside the requested window.
- Un-swept overdue work must not be reported as missed unless the effective
  missed rule proves it was missed.

Per-animal vaccination status answers must keep these states non-overlapping:

```text
scheduled
due
overdue
in_progress
missed
deferred
waived
completed
```

`blocked` is a drive/event-level or process-blocker state, not a per-animal
obligation status bucket. If a drive is blocked, the assistant must say the
drive is blocked and still report per-animal rows using the per-animal status
partition above.

Counts must label what they count:

- `obligation_count` counts due doses/obligations.
- `distinct_animal_count` counts unique herd animals.
- group summaries must not present obligation counts as animal counts.
- species defaults to all species in the assistant chat layer. Colloquial
  wording such as "goats" or "sheep" is not treated as a species filter, so
  "which goats missed vaccination" is answered across every species the pack
  supports. The chat layer narrows by species only on an explicit, unambiguous
  request. This all-species default lives only in natural-language
  interpretation; ops-query params, tool schemas, read models, and the data
  model keep species exact and are never loosened by chat wording.

Every paginated answer must distinguish full visible totals from returned rows:

```text
full_visible_count
returned_count
result_truncated
pagination_cursor
```

The assistant must say "showing N of M" when a result is truncated.

## Scope And Ambiguity Rules

The assistant must never assume wider access than the backend grants.

Default scope behavior:

| User context | Missing scope behavior |
| --- | --- |
| CEO / COO / superadmin | Default company-wide and group by park, then shed. |
| Vertical director | Default to vertical-wide granted scope and group by park/shed. |
| Park head | Default to assigned park and group by shed. |
| Verifier | Default to assigned verification scope. Ask if multiple scopes exist. |
| Operator | Default to assigned shed/task scope only after a scoped operator assistant-read grant/tool path exists. Ask if multiple active assignments exist. |
| Multi-scope user | Ask a clarification unless a single safe default is configured. |
| No matching grant | Refuse with unauthorized or no-data wording from backend result. |

If a user omits date, location, vaccine, animal identifier, or metric period,
the assistant may use deterministic defaults only when the product spec defines
them. Otherwise it asks one short clarification.

Examples:

```text
"Which animals missed yesterday's PPR drive?"
CEO/COO: answer company-wide grouped by park and shed.
Park head: answer for that park grouped by shed.
Operator with one shed: answer for assigned shed.
Operator with multiple sheds: ask which shed.

"When is my next drive?"
Operator: assigned next drive.
Park head: next park drive, grouped by shed if multiple.
CEO/COO: next company drives, grouped by park.
```

## Answer Requirements

Every operational answer must include:

- Short answer first.
- Scope used: tenant, park, shed, role default, and filters.
- Time basis: Goat OS business calendar, currently Asia/Kolkata, and resolved
  date range.
- Freshness envelope for every read-model, projection, or analytics-backed
  answer: `as_of` or `last_success_at`, `freshness_status`, `serving_state`,
  `stale`, `rebuild_required`, `source_watermark`, `unavailable_sources`,
  `conflict_count`, `projection_version`, and `source_composition` when
  applicable. This envelope is a v1 assistant contract; today it physically
  exists only for counts/mortality-style projection state. Vaccination v1 serves
  live from obligation/read-model queries, so wrapped vaccination answers render
  missing envelope fields as `not_projection_backed` or null, synthesize only
  the `as_of` they actually used, and never claim projection freshness the
  source did not provide.
- Source route/tool names and key identifiers.
- Count taxonomy, including obligation count versus distinct animal count where
  both can differ.
- Full visible count, returned count, truncation flag, pagination, and link to
  drilldown for long results.
- Owner of next action, due time, evidence/proof status, and history link for
  process-integrity answers.
- Explicit caveat when a domain API is missing or data is incomplete.
- Safe next action when relevant.

The assistant must distinguish:

```text
No matching data found
Unauthorized for this scope
Domain not implemented yet
Tool/API temporarily failed
Question ambiguous
Model cannot infer safely from available data
```

## Safe Actions

V1 is read-only except for already-built low-risk calendar actions if product
explicitly enables them.

Action rules:

- Mutating tool calls require user confirmation.
- Tool schemas must require idempotency keys.
- Backend checks permissions and scoped grants again.
- Assistant shows what will change before calling the tool.
- Assistant records tool result and links to audit/history.
- High-risk actions such as quarantine, death, movement, protocol activation,
  identity merge, medicine administration, sale/allocation blocker override, and
  financial approval are out of scope until their deterministic guardrail policy
  pack owns the transition.

## Capability Packs

The assistant grows by capability pack. A pack is production-ready only when it
has APIs/read models, permission rules, examples, eval cases, and fallback
behavior.

| Pack | V1 status |
| --- | --- |
| Preventive Care vaccination | Primary v1 pack. |
| Animal passport / identity lookup | Supported via existing passport and identity APIs where available. |
| Locations and scope | Required support pack for all answers. |
| Action Center / Control Tower | Required v1 monitoring pack. |
| SOP tasks / proof / verification | Read-only in v1 where APIs exist; actions later. |
| Health / quarantine / ICU | Future pack; must use critical-action guardrails. |
| Feed and inventory | Future pack once APIs/read models are stable. |
| Procurement and arrivals | Future pack once source-entry APIs are stable. |
| Breeding / pregnancy / kidding | Future pack. |
| Counts / mortality / movement | Future pack, not legacy-dashboard parity by default. |
| Sales / allocation / customer promise | Future pack with high-risk guardrails. |
| Finance / costs | Future pack through governed analytics and finance APIs. |

## Evaluation And Quality

The assistant must ship with a golden eval set before production use.

Eval dimensions:

- Role and scope matrix.
- Ambiguous scope and clarification behavior.
- Missing API and incomplete data behavior.
- Vaccination edge cases: missed, deferred, waived, catch-up, overdue, next
  drive, shed-level drilldown, animal passport explanation.
- Historical as-of cases: administered on day D and accepted on D+1, completed
  after the requested date, missed then recovered, un-swept overdue, and
  status-event reconstruction.
- Prompt injection, data-borne injection through notes/reasons/tool outputs, and
  data exfiltration attempts.
- Forbidden-claim evals for ignored source-rule branches such as dam/mother
  vaccination status in Preventive Care scheduling.
- Truncation and full-count evals.
- Species evals: chat defaults to all species, colloquial "goats"/"sheep" is not
  a species filter, and explicit species narrowing still works where the pack
  supports goat and sheep.
- Tool failure and retry behavior.
- Answer provenance and source link coverage.
- No hallucinated animal IDs, counts, vaccines, dates, or owners.

Minimum launch bar:

```text
read-only vaccination evals pass
RBAC/scope evals pass
scoped operator assistant-read path exists before operator smoke is accepted
prompt injection evals pass
forbidden-claims evals pass
all answers cite tool provenance
no free-form production SQL access
operator and leadership examples verified against seeded Postgres data
```

## Rollout

1. Internal dev: local and dev environment with seeded vaccination data.
2. Read-only pilot: CEO/COO and engineering, vaccination-only.
3. Park pilot: one park head plus limited operator scope.
4. Operator pilot only after the scoped operator assistant-read permission/tool
   path and tests are implemented.
5. Broader internal rollout: add more packs only after evals pass.
6. Safe actions: enable nudge/snooze/acknowledge after action audit and
   idempotency tests pass.
7. Analytics questions: enable only through governed semantic metrics.

## Success Metrics

- Percent of internal questions answered with cited source tools.
- Percent of answers requiring clarification.
- Tool failure rate and p95 answer latency.
- RBAC denial correctness in evals.
- Hallucination or unsupported-claim incident count.
- Reduction in manual dashboard navigation for vaccination exception review.
- Time to identify owner and next action for missed/overdue process work.
- User feedback: correct, useful, incomplete, unauthorized, or confusing.

## Open Product Decisions

- Name and entry points: admin-web assistant only, Slack bot, mobile assistant,
  or all three.
- First non-vaccination capability pack after v1.
- Whether CEO/COO company-wide defaults should return full tables or grouped
  summaries with explicit "show details" follow-up.
- Which safe actions are allowed in v1 after read-only launch.
- Retention period for assistant transcripts and tool-call audit rows.
- Whether production uses OpenAI, Gemini/Vertex AI, or both behind model
  adapters.
- Exact operator v1 permission model: shed-scoped module reads versus a narrow
  ops-query-only assistant read path.
