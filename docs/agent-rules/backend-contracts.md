# Backend Contracts: Events, Read Models, Guardrails, Grain, Write Paths

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Domain Event Integration Is Mandatory

Backend, admin-web, and mobile business mutations all use the same domain-event
architecture. Any CRUD/import/sheet/mobile-offline/worker path that creates,
moves, closes, reclassifies, or consumes business state must register producer,
event, consumer, replay/DLQ behavior, and E2E proof in
`context/architecture/domain-event-registry.json`, following
`context/architecture/domain-event-integration-contract.md`. Run
`make domain-event-architecture-guard`. This guard is part of the mandatory
`run_common` path in `make ci-local`; an optional compatibility job or a textual
mention elsewhere is not accepted as CI wiring.

Vaccination FCM is part of that event spine, not a client feature flag. Every
push-facing vaccination state must have an explicit contract for trigger,
audience source, cadence/SLA, message summary, and tap route. Token delivery
targets come from active `workforce_member_devices`; tenant leadership
recipients (`ceo_internal`, `pc_director`, future CXO/director aliases) resolve
from active role grants/profile truth, never from the single-seat
`workforce_positions` table alone. Routine day-start/afternoon operator nudges
stay field-scoped; the 20:30 IST due-today checkpoint includes PC director/CEO
leadership when scheduled sheds are still not submitted. Shed proof submission
immediately notifies the park verifier(s) and leadership with role-specific
routes: verifier to video review, leadership to Vaccination overview. Run
`make fcm-recipient-routing-guard` with local CI for any notification change.

**WHO HEARS A LEADERSHIP PUSH IS CONFIG, PER DESIGNATION (maintainer decision 2026-09-08).**
Every UPWARD push -- the daily low-stock and overdue-load alerts, the proof
pending/approved/rework copies to directors, the weighing lifecycle notices, the
missed-work escalation, the 20:30 vaccination checkpoint -- resolves its audience
through ONE resolver (`notificationbridge.AudienceResolver`, implemented by
`notificationaudience/app.Resolver`) keyed by an ALERT KEY in
`notificationaudience/domain`'s catalog. The tenant's stored override
(`notification_alert_audiences`, edited on People / HRMS -> Notifications) replaces
the catalog default for that alert; ABSENCE IS THE DEFAULT, and the default of every
alert is byte-for-byte the audience the notifier resolved by hand before, so deploying
it changed nobody's phone. It is keyed by DESIGNATION (the job title), never by
person: an alert is addressed to a desk, and whoever holds the desk hears it. An
EMPTY stored audience is a decision ("nobody") and is never confused with "not
customised". Park desks (park_head, verifier, operator, procurement_manager) resolve
at the alert's park and to nobody when the alert carries none. Pushes ADDRESSED to a
named person that leadership still wants switchable (the CXO a task names, the verifier
on duty for a video) are rows with the `Resolver.Addressed` rule: the addressee is kept
while their own title is ticked, other ticked titles get a copy. Two things stay OUT of
the catalog on purpose: operator WORK pushes (the operator whose work bounced, the packer
whose bag was reopened, the park head whose pen visit is due) and the Slack channel posts. Do NOT add a new upward push with a hand-resolved position code:
add a catalog row and ask the resolver. Every production construction site of an
upward consumer/notifier MUST chain `.WithAudience(NewStoredAudience(...))` -- a
constructor builds a defaults-only resolver so tests keep their roster fakes, but a
production site that forgets it serves defaults forever and the matrix silently does
nothing for it. Pinned by `TestEveryUpwardNotifierIsWiredToTheStoredAudience`,
`TestEveryVerificationModuleHasAnAudienceCatalogRow`,
`TestDefaultsReproduceThePreCatalogAudiences` and the Postgres round trip
`TestAudienceRoundTripThroughTheStoredOverride`. Canonical prose:
`docs/decisions/notification-designation-audiences.md`.

## Operational Read Model Contract Is Mandatory

Shared command surfaces (Calendar, Control Tower, Action Center, Protocol
Adherence, Workflows, admin-web detail pages, Android execution/proof screens,
and CEO/AI reporting) are renderers of backend-owned operational read contracts;
they must not invent private business truth or recompute whole-result totals
from page-local rows. Every new vertical or module, including shifting, counts,
breeding, weighing, feed, procurement, and future preventive-care modules, must
plug into the pattern in
`docs/architecture/operational-read-model-contract.md` before it is exposed on a
shared surface.

Mandatory rules for Claude, Codex, and human developers:

1. Name the grain of every shared count and status bucket (`animal`,
   `obligation`, `completion`, `proof`, `verification`, `shed`, `partition`,
   `drive`, `park_day`, `task`, `alert`, etc.).
2. State whether buckets are disjoint or overlapping. Do not add overlapping
   counts in UI unless the contract explicitly defines a union count.
3. Summaries are whole-filter aggregates unless explicitly named `page_*`.
   Pagination changes rows only, never summary truth.
4. Selected operational scope must use stable identity. Rule ID alone is not a
   drive selector when rules recur across dates, sheds, partitions, operators,
   or batches.
5. Backend response structs, OpenAPI, generated TypeScript clients, Android
   DTOs, admin-web renderers, and mobile renderers must move together.
6. A new vertical is not pluggable until it declares its canonical write owner,
   work-item identity, scope grain, time grain, state machine, evidence model,
   shared summaries, Calendar representation, Control Tower representation, and
   mobile/admin surface contract.
7. Cross-surface golden fixtures are the proof: the same fixture must make
   Calendar, Action Center, Protocol Adherence, Control Tower, Workflows, Admin
   Web, Android, and reporting agree on the facts they share.

Run `make operational-read-model-contract-guard` for any change touching shared
read models, OpenAPI, admin-web command lenses, Android execution/proof screens,
or new vertical/module onboarding. This discoverability/static-text guard is
part of local CI, but it is not a semantic Go/OpenAPI/Kotlin/frontend drift
checker yet.

## Critical Animal Action Guardrails Are Mandatory

Quarantine, ICU, death, contagious-disease isolation, high-risk movement, and
sale/allocation blockers are critical animal actions, not ordinary CRUD. Until a
complete policy-pack module owns a transition, every route/UI/action must fail
closed or return a deterministic guardrail-required reason as described in
`docs/features/critical-animal-action-guardrails.md`. Run
`make critical-animal-action-availability-guard` for movement, health,
vaccination defer/reopen, or Goat Passport changes.

Shared vaccination drive tasks are aggregate bookkeeping only. A hidden park/
batch-level `sop_tasks.state` must not be used as per-shed submitted/proof/
verification truth in WF, CT, AC, Calendar, Android, or verifier queues. Shed
grain state comes from shed-scoped facts: `sop_submissions`,
`sop_submission_items`, `vaccination_completions`, and `proof_artifacts`, joined
by the active shed/submission/batch grain. The mobile shed-submit idempotency key
must include the active shed scope, and backend submit must stay idempotent when
another shed on the same shared parent already submitted. That sibling allowance
stops at `needs_review`: once the shared parent is `accepted`, fresh submit keys
must fail before writing any new submission, fanout, audit, or movement side
effect; only exact idempotency replay may read back the existing result.
Write-path grain is part of the same rule, not a separate implementation detail:
a shed-level proof submission may receive broad scan captures for the shared
parent task, but it must filter `SubmissionItems` to goats whose current
`goats.shed_id` matches the shed `subject_id` in completed `proof_refs` before
inserting `sop_submission_items` or `vaccination_completions`. Never "fix" a
WF/CT/AC/Calendar/Android review leak by changing display precedence while the
shared parent write still materializes sibling sheds. The mandatory regression is
an adversarial two-shed submit where one proof carries shed A, the command also
contains shed B scan items, and shed B writes zero submission items/completions.
Run `make goat-shed-scope-guard` and the targeted Postgres SOP/PI tests for any
submit, proof, verification, or projection change.

Frontend/mobile render backend-owned contracts and send idempotent commands; they
do not create private business follow-up pipelines. Direct live-animal table
writes are allowed only through registered canonical producers or approved seed
closeout paths. Future shifting, dead-birth, feed-direction, procurement, and
vaccination changes must plug into this same event spine.

Register BOTH ends, every time (Claude AND Codex): a producer with no consumer
on both durable buses is a silent drop, a consumer with no producer is dead
code, and a payload KEY no consumer parses is an accept-and-discard that reads
to the next author as already honored. Delete the unread key and its struct
field, or name the handler that reads it in the registry. See
`.agents/skills/domain-event-architecture/SKILL.md` and
`docs/decisions/scale-anti-patterns.md` -> "Operator-cascade wiring
anti-patterns".

## Grain Predicates and Executable Gates (Mandatory, Claude AND Codex)

Three defect classes recur across unrelated modules and must be checked on every
change that reads a plan/aggregate row, writes a rule into a doc, or loads a
committed fixture:

1. **Write the grain proof down; the grain rule itself already exists.** The
   aggregate rule above ("identify the canonical membership source, use the same
   stable group key on producer and consumer, prove every join is 1:1 or
   pre-aggregate the many side") is not new, and FIVE instances shipped anyway —
   so this is an adherence failure, not a missing rule, and restating the
   principle a sixth time fixes nothing. What is mandatory now is the written
   proof, next to the `projection-review:` marker: (a) the producer's unique
   column list and the consumer's match/group column list, side by side; (b) the
   row multiplicity of every joined side; (c) for any ratio or cap check, the key
   set each of numerator and denominator ranges over, shown identical. Check all
   three of `WHERE`, `GROUP BY`, and the compared-against key set — a complete
   predicate with a collapsed `GROUP BY` is the same defect one clause over
   (BUG-027: cohort spans N dates, `GROUP BY a.operator_id` collapses cap to one
   operator-day). If those three lines cannot be written, the query is not
   reviewable. `ORDER BY ... LIMIT 1` over rows the producer can legitimately
   duplicate fabricates an answer — the fix is an exact membership source, not a
   better ranking. Both sub-shapes, all five sites, and the mandatory
   mixed-vaccine / two-partition / two-date fixture:
   `docs/decisions/scale-anti-patterns.md` -> "Read-model grain is not the grain
   the consumer assumes".
2. **A documented rule with no executable check is not a gate.** When a runbook,
   validation doc, or fixture README states an automatic-failure condition or a
   required step, grep for the code that enforces it in the same change. If
   there is none, the finding is the missing check. Enforcement belongs in the
   `make` target that performs the mutation.
3. **Fixture/contract loaders must fail loud on unknown keys.** `encoding/json`
   drops unmatched keys silently, so a fixture block with no struct field seeds
   nothing and still reports success. Loaders of committed fixtures use
   `Decoder.DisallowUnknownFields()` or an explicit schema pass.

A guard is only as strong as what it can see. A literal-token grep sold as an
architectural boundary enforces the string from the original incident, not the
rule; when the rule is "package A must not depend on package B", check the
import graph, and state every remaining blind spot in the guard's own header
comment with a self-test fixture for each.

- Treat idempotency as a mandatory write-path contract for every mutating API,
  worker, importer, webhook, state transition, outbox producer/consumer, server
  action, and UI-triggered write. Each write path must accept or derive a stable
  idempotency key or operation identity, persist that key and a semantic request
  fingerprint in the same transaction as the side effects, return the original
  result for an exact replay without rerunning side effects or outbox work, and
  reject a same-key different-payload replay or return the original result with
  no new side effects. The SQL pattern `ON CONFLICT DO UPDATE` with only
  `idempotency_key = EXCLUDED.idempotency_key` is not sufficient when later code
  can still mutate state. Tests must cover first call, exact replay, same-key
  different-payload replay, and downstream duplicate prevention.
- Treat a state transition and the sync of any derived read model it OWNS as ONE
  atomic transaction. A record must never reach its published/committed state
  while a read model it is the sole writer of failed to save. Do the derived
  upsert AND any post-write parity/verification check INSIDE the same DB
  transaction as the state change, so a sync failure rolls the whole transition
  back — no status flip, no outbox event, no audit row, no partially-written read
  model. A post-commit "best-effort" sync is allowed ONLY as a fallback for an
  already-committed replay or an adapter without transactional support, never as
  the first-commit path. Canonical case: publishing a vaccination protocol version
  upserts + parity-checks `rule_dsl.capacity` into `vaccination_capacity_config`
  inside the publish transaction (`PublishVersionWithCapacity` /
  `PublishVersionWithDerivedRules`), and a parity mismatch
  (`ports.ErrCapacityParityMismatch`) rolls the publish back. Every such flow needs
  a rollback regression test — failed sync ⇒ source stays in its prior state with
  zero side effects; see `TestPublishVersionWithCapacityRollsBackOnSyncFailure`.
- Treat authored config/business values as validate-or-reject, never
  silently-default. A field that is PRESENT but out of range (e.g.
  `rule_dsl.capacity.max_per_day < 1`, `max_buffer_days < 0`) must FAIL the
  publish/save with a clear error, not be rewritten to a default business value
  the author never entered; defaults apply ONLY to genuinely-absent fields.
  Frontends must keep a cleared field distinct from an explicit `0` (a blank input
  publishes the declared default; an explicit out-of-range value is sent verbatim
  so the backend rejects it) — never coerce blank to `0` or to an invented value,
  and never let a React default become authored business truth.
- Treat the clinical defer set as a mandatory medical safety block, never an
  authored subset (C35-010). The four clinical states `sick`, `under_treatment`,
  `quarantine`, `icu` are non-optional postponement rules per
  `docs/preventive-care-vaccination/vaccination-rules.md`: an animal in any of them
  must have its open vaccination work DEFERRED (held for recovery), never cancelled
  or left scheduled — a wrong medical action is P0 regardless of how cleanly it
  compiles. A published rule's `eligibility.defer_states` may only ADD states; it
  may never drop one of the four. Enforce on BOTH layers: publish/validation must
  REJECT a present, non-empty `defer_states` that omits any mandatory clinical
  state (an empty/absent list maps to the safe full default), and the generator
  must union the mandatory set in regardless of the authored list so an
  already-published partial rule is still safe at runtime. The single source of
  truth is `backend/internal/protocol/domain.MandatoryClinicalDeferStates`
  (`EffectiveClinicalDeferStates` / `MissingMandatoryClinicalDeferStates`) — do not
  re-hardcode the set elsewhere; the SQL siblings
  (`vaccination_eligibility_rollups` usable flag,
  `ListRecoverableDeferredVaccinationGoatIDs`) must stay in sync with it. Mechanical
  backstop: `make clinical-defer-guard` (required in CI).

## Jobs, Retention and List Endpoints (perf budget, 2026-09-24)

Canonical catalog: `.agents/skills/scale-anti-patterns/SKILL.md` ("STG latency catalog", P1-P25).
- **Job safety (P7).** Every Cloud Run job / worker: a watermark (never rescan
  all history), outside dependencies non-fatal (log + metric + degraded
  success), bounded retries with backoff, a Postgres conn cap (<=3). Bad:
  analytics-rollup rescanned `analytics.app_events` then died on a BigQuery 403
  and was refired every ~40 min. Evidence: a test with the dependency failing.
- **Event/log retention (P2, P18).** A new event, log, audit, history or
  telemetry table ships in the same PR with its retention job, which archives to
  GCS cold storage (Parquet) before pruning, and per-table autovacuum tuning if
  it is high-churn. Bad: `app_events` 2.6GB, delivered outbox ~1GB, never pruned.
- **Lists are paginated server-side (P9).** Every list an API returns to
  admin-web or Android: keyset/cursor (no OFFSET on large tables), server-enforced
  default (e.g. 50) and hard max (e.g. 200) page size, stable sort with a unique
  tiebreaker, totals from a stored counter or separate cheap count. "Show
  all"/export is an async export job. Bad: `ListLive` walking 50k tags for
  `limit=1`; vaccination command reading all 86k obligations.
