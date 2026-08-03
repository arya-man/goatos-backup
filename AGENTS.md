# Goat OS Workspace Agent Context

## Local Stack Canonical Ports

For local Goat OS browser/debug work, use one shared local stack unless the user
explicitly asks for an isolated throwaway stack:

```text
Frontend: http://127.0.0.1:3300
Backend:  http://127.0.0.1:8080
Database: postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
Docker DB container: goatos-local-current
```

Before cloning, seeding, importing, or debugging local data, first verify the
running backend's `DATABASE_URL` and make it match the canonical DB above. Do
not infer the local DB from a previous temp worktree, a random Docker port, or a
stale shell variable. If a temp stack is unavoidable, clearly label it as
throwaway and do not call it "the local DB".

When the user says "my local DB" or "local frontend/backend", treat that as:

```text
Chrome -> 127.0.0.1:3300 -> 127.0.0.1:8080 -> 127.0.0.1:5433/goatos
```

For physical Android phone scan/RBAC testing, do not mutate the normal local DB.
Use the reset-first throwaway database and runbook:

```text
Database: postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable
Docker DB container: goatos-phone-qa
Runbook: docs/runbooks/phone-qa-throwaway-rbac.md
```

The phone still reaches the laptop API through `adb reverse tcp:8080 tcp:8080`.
Only the backend process behind `127.0.0.1:8080` changes database target. The
fixture intentionally maps five physical vaccination RFIDs into ten goat
identities across CBE and CPT while preserving the production uniqueness rule on
`goat_identifiers`; Weighing remains free-flow and must keep raw RFID input.

## Fast Lane for Tiny Fixes

When the maintainer asks to make a small, low-risk fix and land it on `main`,
optimize for elapsed time. Do not run the full local CI matrix, mobile install,
cloud deploy, browser proof suite, or graph/document maintenance unless the
change actually touches that surface or the maintainer explicitly asks for it.

Default verification should be the narrowest command that proves the touched
surface still works. Examples:

- Android Kotlin-only UI or view-model edit: run the targeted Gradle compile or
  targeted unit test; install to a physical device only when device behavior is
  the thing being verified.
- Admin-web component/style edit: run the relevant typecheck/test/lint slice or
  a focused browser check, not the whole product suite.
- Docs/copy/config-only edit: inspect the diff and run format/schema validation
  only if that file type has one.

Before pushing, verify repo, remote, active identity, branch/head, and dirty
state. Avoid detached-HEAD limbo for ordinary work: use the current branch when
it is safe, or push the verified commit explicitly with `git push origin
HEAD:main` when the maintainer asked to land directly on `main`. Never include
unrelated proof files, screenshots, temp folders, or local artifacts in the
commit.

Report the verification boundary honestly and briefly. If only a narrow check
was run, say so; do not spend 20 minutes manufacturing confidence for a one-line
change.

## MANDATORY: 4-Layer Lookup on Every Code Question

Work through layers in order. Stop at the layer that answers the question. Do NOT jump to files/grep first.

### Layer 1 — CRG (code structure)
For callers, callees, imports, blast radius, architecture, dead code, test coverage:
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel

Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

### Layer 2 — Graphify (business/product context + technical docs)
Two graphs. Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model:
```
MCP: mesha_docs_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, phase docs, obligation engine, skill references (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
When built, it covers protocol engine, Preventive Care (PC) vaccination, feed direction,
frontend scope, analytics infra, execution plans, observability, auth, SOP
cutover, and skill references.

### Layer 3 — Skill references (architecture decisions, TRDs, phase contracts)
When CRG + Graphify don't cover it — deep implementation rules, phase PRDs/TRDs,
OpenAPI contracts, form DSL, analytics infra, security/ops rules:
```
Load .agents/skills/goatos-build/SKILL.md → pick only the relevant reference doc
Do NOT load all reference docs — let CRG + Graphify narrow which one applies
```

### Layer 4 — Grep/Read (CRG blind spots)
Only for what the graph cannot see:
- HTTP route strings (`r.GET("/api/v1/...")`)
- Middleware wired via reflection or string keys
- Config/env values and constants
- SQL query strings
- Uncommitted/unstaged code
- Any `callers_of = 0` result that seems wrong — verify with grep

## STG Deployment Contract

For Goat OS, STG deploy is NOT GitHub Actions and NOT PR-driven.

GitHub Actions is billing-blocked and must not be used for deployment
(validate only via `make ci-local` on the pushed SHA).
Do not create main→stg PRs as a deploy mechanism.
Do not force-push a `stg` branch and wait for CI.
Do not infer CI deployment from branch names.

Authoritative STG deploy path:
1. Read `docs/runbooks/stg-deploy.md` (short contract) →
   `docs/runbooks/cloud-deploy-staging.md` (full Cloud Deploy mechanics).
2. Use the manual Google Cloud Deploy scripts under
   `tools/deploy/stg-clouddeploy-*.sh`.
3. Verify active account is `ravi@mesha.sg`.
4. Verify target org is `vgoats.com` and environment is Goat OS STG
   (`goatos-stg`).
5. Never use Slice/Heva GitHub identity or cloud project for Goat OS.

If a user asks to "push to STG", "promote STG", or "deploy STG", this means:
manual Google Cloud Deploy from the latest approved `origin/main`, following the
runbook.

Do not ask whether to use GitHub Actions, PR merge, or force-push `stg` unless
the user explicitly asks to change deployment architecture. The machine-readable
form of this contract lives at `context/deploy-contract.json`.

## Business and medical rule changes (maintainer lock)

When the maintainer states a **new working rule, condition, timing, or workflow**
(in chat, WhatsApp screenshots, Preventive Care sign-off, or ad-hoc instructions) that may
**contradict or supersede** existing docs, seeded config, implemented kernel
behavior, or a prior decision in the same thread:

1. **Stop and surface the conflict first** — quote the old rule/source and the new
   instruction side by side. Do **not** silently pick one, blend them, or change
   code/docs on assumption.
2. **Ask explicitly** which rule wins, whether the old rule is retired, or
   whether both apply in different scopes (species, stage, procurement path, etc.).
3. **Implement only after confirmation** — then update the canonical source in the
   same change as the code (`docs/preventive-care-vaccination/vaccination-rules.md`,
   published `rule_dsl`, TRD/ADR, or this file when appropriate).

Ambiguity is not approval. Informal agreement in a screenshot or chat applies to
**that** scenario until it is written into the source contract.

Confirmed Preventive Care (PC) vaccination override: never ask about, model, seed,
import, expose, or schedule from mother-not-vaccinated / unknown-mother status.
The private source/wiki may contain that branch, but GoatOS ignores it. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in `docs/preventive-care-vaccination/vaccination-rules.md`.

Confirmed Preventive Care (PC) ET+TT course rule: ET+TT is a two-dose course
before the 182-day repeat. Dose 2 is due 21 days after dose 1 for both kid and
adult courses. Imported/seeded ET+TT dose 1 must create the dose 2 obligation
first; it must not jump straight to the 182-day repeat. The 182-day repeat
starts only after accepted ET+TT dose 2/course completion. Blue Tongue kid dose
2 remains 28 days after dose 1; pox vaccines still obey the 28-day live-to-live
spacing after PPR.

Hard seed/generation guard: after real vaccination seeding, any accepted
`et_tt_adult_w1` completion without a same-goat `et_tt_adult_w2` obligation or
completion is a broken database, not a warning. Do not report future drives from
`vaccination_drive_assignments` alone; first audit missing required obligations
against `protocol_rules` and accepted history, especially adult ET+TT dose 2.

Confirmed movement rule (maintainer decision 2026-07-19): goats never move
between parks — shed moves exist only within one park; leaving a park is a
terminal transferred/sold exit, never a move. Initial placement is exempt. See
`context/source-findings/goats-and-parks-source-findings.md` → Movement
Semantics.

Confirmed shifting stage-selection and Vaccination handoff rule (maintainer decision
2026-08-03, SUPERSEDING the 2026-07-29 three-mode operator chooser, which in turn
superseded the 2026-07-20 destination `shed_profiles` authority rule): the raiser no
longer chooses a management stage. A movement ADOPTS THE DESTINATION SHED's cohort,
resolved by the BACKEND at raise time and snapshotted onto the request. The mobile form
must not ask; `management_stage_mode` and `target_management_stage` are no longer accepted
from clients and are rejected as unknown fields. The single exception is a FLUSHING
destination: flushing is a nutrition cohort owned by its own workflow, so moving an animal
into a flushing shed keeps that animal's current stage. Because the resolution cannot be
guessed when the destination is ambiguous, three cases also keep the current stage — a shed
holding more than one cohort, an empty shed, and a cohort absent from active
`animal_stage_lookup` (real sheds carry `ICU-Kid`, `ICU-Non-Pregnant`, `Quarantine kids`,
which the relocation cannot write and which would otherwise fail at the SECOND GATE, after
the operator's video and the park head's approval). Keeping the current stage is the
already-shipped empty-target behaviour, never a fabricated cohort; do not "improve" it into
a majority-resident pick, which stamps a stage on thin evidence and flips as animals move.
Canonical rule: `backend/internal/counts/domain.ResolveShiftingDestinationStage`; resolution
happens at RAISE time so the park head approves the same stage the completion applies.
Once Park Head approval and operator completion both exist, the
second-gate transaction must atomically update the goat's `shed_id` and, when selected,
`management_stage`, write identity audit, and publish
per-animal `goat.location.changed` plus `goat.stage_changed` when the stage
changed. Vaccination must consume the result twice: rescope open shed-scoped
work while preserving in-progress/completed history, then re-evaluate clinical
eligibility/schedule. Selecting `Mother` changes only `goats.management_stage`; movement must
not create or modify pregnancy or lactation records, and must not fabricate health,
or other clinical facts; those stay on their authoritative workflows. A real
shifting-completion → Vaccination E2E test is mandatory—separate producer and
consumer tests are not closure.

Confirmed shifting approval + completion gate (maintainer decision 2026-07-28,
SUPERSEDING the 2026-07-26 "verifier approval applies the move" rule): a raised
movement appears in Android Actions immediately. Park Head approval and operator
completion are independent and may arrive in either order; neither first fact
relocates. Operator completion still requires a MANDATORY live-camera video
(`shifting_events.proof_ref`; blank is 422). The transaction recording the SECOND
of approval/completion atomically updates canonical `goats.shed_id` and destination
stage, publishes location/stage events, flips the movement `applied`, and therefore
moves Herd Register / Counts. Verification is post-task evidence review only:
APPROVE marks evidence verified; REWORK creates evidence rework/audit without
changing the applied movement or rolling back goat location/count. Generic
Verification enqueue and verdict consumers remain wired, but verdicts do not own
census truth. Canonical source: `docs/decisions/shifting-verification.md`; forward
migrations `000049_shifting_approval_completion_gate.sql` and
`000050_shifting_actions_index.sql`.

Confirmed high-priority shifting feed-evidence rule (maintainer decision 2026-07-29): low-priority
shifting remains the existing one-live-camera-video flow. High-priority shifting embeds feed packing
and feeding inside Shifting, resolves exact feed type/quantity from active destination Feed Config
matched to the raise-time target management stage and moved animals' ration groups, and requires
THREE live-camera videos: shifting, feed packing, and configured feed being given to the animal(s).
All three proofs are reviewed together in ONE `shifting_move` verification item. Embedded packing
proof is shifting-scoped only and never creates or completes the separate Feed Packing/Feed
Distribution workflows. Park Head approval + operator completion still apply location/stage/counts
on the second gate; verification remains post-task review and rejection creates operator rework
without rollback. Missing config blocks, and a semantic fingerprint shown to the phone is
revalidated under the shifting row lock so changed config returns `feed_config_changed` rather than
guessing. Canonical source: `docs/decisions/shifting-verification.md`; migration
`000053_high_priority_shifting_feed_evidence.sql`.

Confirmed feed-distribution verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "operator marks a shed-session fed (optional video), completed at
submit" contract FOR the feed-DIRECTION operator flow ONLY): a feed-direction
shed-session is completed only after a verifier approves the operator's proof.
The operator submits TWO MANDATORY proofs per session — a feed-distribution VIDEO
(`distribution_proof_ref`) and a water-distribution proof that may be PHOTO OR
VIDEO (`water_proof_ref`); a completion missing either is rejected 422
`proof_required`. That flips a NEW `feed_distribution_completions` row to
`pending_verification` and enqueues ONE `feed_distribution` verification item
carrying BOTH proofs — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedDistribution`) covers both proofs and flips the session to
`completed` (this is when `feed.distribution.completed` is emitted); a REJECT
(`BounceDistributionForRework`) flips it to `rework` for a re-shoot. Applies to
BOTH `normal` and `experiment` workflows. Feed direction is app-only: the
`/feed/direction` admin-web left-bar leaf is removed (keep `/feed/config`).
Wiring is the generic Verification module: producer `feeddirection`
`CompleteDistribution` + `feeddirection/adapters/verificationbridge` enqueue;
consumer `feeddirection/app.FeedDistributionVerificationHandler` on
`verification.verdict.approved`/`.rework`, filtered to `source.module=feed,
ref_type=feed_distribution_completion`. Canonical source:
`docs/decisions/feed-distribution-verification.md`; migration
`000032_feed_distribution_verification_gate.sql`.

Confirmed feed-PACKING verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "FEED PACKING IS DELIBERATELY NOT GATED" rule that the
feed-distribution lock above originally carried): feed PACKING is now gated the
same way as feed direction. The operator completes a packing shed-session with
ONE MANDATORY packing VIDEO (`packing_proof_ref`); a completion missing it is
rejected 422 `proof_required`. That flips a NEW `feed_packing_completions` row to
`pending_verification` and enqueues ONE `feed_packing` verification item carrying
the video — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedPacking`) flips the packing session to `completed` (this is when
`feed.packing.completed` is emitted); a REJECT (`BouncePackingForRework`) flips
it to `rework` for a re-shoot. Applies to BOTH `normal` and `experiment`
workflows; `overlayPackingCompleted` now reads `ListVerifiedPacking`. The gated
flow is a SEPARATE record on a NEW table, never an ALTER of the old packing
table. The OLD instant packing path — `feed_direction_session_completions`
(migration `000030`), `POST /feed-direction/complete`, `feed.direction.completed`,
`CompleteSession`, and the mobile `FeedCompleteScreen`/`feedCompleteRoute` — is
left INERT (no longer navigated to from packing) but not deleted; retiring it is
a separate cleanup. Both feed gates share `source.module=feed` and are kept apart
ONLY by `ref_type` (`feed_packing_completion` vs `feed_distribution_completion`).
Wiring: producer `feeddirection` `CompletePacking` +
`feeddirection/adapters/verificationbridge` `NewPacking`; consumer
`feeddirection/app.FeedPackingVerificationHandler`. Route
`POST /feed-direction/packing/complete` (registered in `permissions/routes.go`
alongside the distribution route, which had been unregistered and would 403).
Canonical source: `docs/decisions/feed-distribution-verification.md`; migration
`000033_feed_packing_verification_gate.sql`.

Confirmed Feed Transport daily verification rule (maintainer decision 2026-07-29,
SUPERSEDING transport session/batch/consolidation wording): at 15:30 IST, create one
today-task per active physical shed. Feed Transport is never per feed session. The
operator records one mandatory fresh in-app-camera video; submit moves the task to
`verification_due`. Verifier APPROVE moves it to `completed`; REJECT moves it to
`rework` assigned to the same operator. Every rework requires a new video and appends
a new proof attempt; rejected proof attempts remain immutable history. Canonical source:
`docs/decisions/feed-transport-verification.md`; migration
`000054_feed_transport_daily_verification.sql`.

Confirmed verifier verdict-exclusivity rule (maintainer decision 2026-08-03, SUPERSEDING the
CEO/CxO `verification.review` override for the DECISION only): approve/reject on a verification
item belongs to the Verifier role ALONE. `verification.verdict` is split out of
`verification.review` and granted to `verifier` only — never `ceo_internal`, `pc_director`,
`growth_director`, `park_head`, or `operator`. Leadership KEEPS `verification.review` (see the
evidence queue, media, and recorded verdicts) and KEEPS `verification.act` (close the work, rework
or reassign the source task); it simply cannot sign the second check itself. Do not "fix" this by
restoring the verdict grant to CEO to satisfy the founder/builder visibility invariant — that
invariant is satisfied by the read, and an independent check the checked party can approve is not
independent. Route `POST /verification/items/{item_id}/verdict` is gated on
`verification.verdict`; `GET /verification/queue` stays on `verification.review`. The admin-web
`record_verdict` control and `isVerifierLensPrincipal` both key on `verification.verdict`.
Canonical source: `context/architecture/verifier-app-and-flow.md` → "Roles (truth table
alignment)"; pinned by `TestVerificationSeparationOfDuty` and
`TestVerdictRouteIsVerifierOnlyWhileQueueReadStaysLeadershipVisible`.

Confirmed verifier admin-web workspace rule (maintainer decision 2026-08-03): the
verifier-only workspace, previously mobile-only, also runs on admin-web with the SAME
five evidence modules as mobile — Vaccination, Weighing, Counts, Feed, Health. `verifier`
now holds `admin_web.bootstrap`, but that opens the SHELL ONLY. A principal holding
`verification.review` and NOT `verification.act` receives the verifier LENS: the sidebar
is composed from the Verification type registry's navigation metadata (one group per
`NavigationModule`, one leaf per `PageKey`, each pointing at
`/actions?category=<disjoint category>`), and EVERY other admin-web page contract is
dropped so a typed URL fails closed at `requireAdminWebPageContract`. Never express this
as a per-role nav template — registering a producer category is the only way to add a
module, and `make nav-composition-guard` still applies. CEO/CxO holds review AND act as
the documented override and therefore keeps the full admin IA; the lens must never narrow
a leadership principal. `/actions` serves both personas, split by the page contract's
controls: `record_verdict` (`verification.review`) vs `request_rework`/`reassign_task`
(`verification.act`). A page rendering from LOCAL literal copy has no contract to
withhold and must gate itself with `adminWebRouteOffered` (`/approvals` does). Known
boundary: the rework/assign routes require `task.verify`/`task.assign` and a verifier
holds `task.verify`, so that half of the split is contract-layer, not a backend lockout on
that shared SOP route. Canonical source: `context/architecture/verifier-app-and-flow.md`
→ "Verifier WEB workspace"; code `backend/internal/adminui/app/verifier_lens.go`.

Confirmed feed-direction shifting-projection timing rule (maintainer decision
2026-07-27, SUPERSEDING the priority-based lead-day rule — normal 2-day /
high-priority 1-day — that the projection previously applied): the feed sheet's
projected shed head count = the live herd PLUS every authorized-but-unexecuted
shifting, with NO lead time and NO priority branch. A shifting is a pending feed
input the moment a park head AUTHORIZES it, so it counts toward the next feed
sheet immediately (destination shed +heads, source shed −heads), affecting ONLY
the feed projection and never the census counts. "Forget high priority": normal
and high-priority movements are treated identically for feed timing (high still
completes same-day operationally, so it lands in the live herd quickly anyway).
A movement stops counting in the projection ONLY when it is `applied`
(verifier-approved), at which point its animals already sit in the destination
shed in canonical `goats` — so the delta must count BOTH `authorized` AND
`pending_verification` (operator completed with proof, not yet approved, animals
NOT yet relocated) and EXCLUDE `applied`, or the shed is either double-fed
(counting applied) or under-fed (dropping pending_verification). The "overdue"
flag fires only when a counted movement was authorized BEFORE the packing day
(feed day − 1) and is still unexecuted, so a freshly authorized move under the
zero lead does not spuriously read as overdue. Canonical source: the pure-Go
spec `backend/internal/counts/domain.FeedShiftingEffectiveBusinessDate` /
`FeedShiftingCountsToward` / `FeedShiftingIsOverdue` and the SQL it mirrors in
`counts/adapters/postgres/feed_projected_counts.go` (`event_status IN
('authorized','pending_verification')`). Proof:
`counts/domain.TestFeedShifting*` and
`counts/adapters/postgres.TestFeedProjectionTimingRule` /
`TestFeedProjectionExcludesAppliedMovements` (includes the pending_verification
case). This changes ONLY the shifting-aware feed projection; the 7:30-style
auto-issue scheduler and the calendar surfacing of next-day feed remain
separate, unbuilt items.

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

## Root-Cause Fixes Only — No Partial / Surface Fixes (Mandatory, Claude AND Codex)

When fixing ANY reported bug (review finding, audit item, regression):

1. **Reproduce the EXACT failure FIRST.** Write a failing test that reproduces the
   precise scenario described (the retry path, the race, the production caller, the
   >cap input), and confirm it FAILS on current code. No fix without a red test that
   models the real failure — not the cited line in isolation.
2. **Fix the ROOT CAUSE, not the symptom.** Trace the actual PRODUCTION path. Do NOT
   patch a sibling method, an adjacent symptom, or the one line quoted and declare
   done. If the production caller invokes a different method than the one you changed,
   you have not fixed it.
3. **A green narrow unit test is NOT proof** if it does not exercise the production
   caller, the retry/partial-failure/edge path, or the concurrency race. Prove the fix
   on the real path.
4. **Never report "fixed" / "already fixed"** without pasting failing-then-passing
   evidence on the real path. "Looks fixed", "compiles + tests pass", and "the guard is
   green" are NOT closure. Verify against the exact failure condition the reviewer gave.
5. Applies to sub-agents too: an orchestrator MUST independently re-verify each
   sub-agent's claim (run the failing test on old code, confirm it fails; on new,
   confirm it passes) before landing — sub-agents have repeatedly done shallow
   "already fixed" passes.

## Consolidated Defect-Ledger Closure (Mandatory)

When asked to fix/continue/close the consolidated audit ledger or its bugs, read
both of these before editing:

- `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`
- `context/repo-audits/consolidated-ledger-defect-closure-program.md`

Select one highest-priority unblocked root defect (or an inseparable cluster),
reconstruct the live count from the file, and follow the closure program across
every affected backend, SQL, API, admin-web, Android, architecture, performance,
memory, retry, pagination, security, E2E, observability, and CI/CD layer. Do not
mark a row fixed until its current-SHA proof packet and independent Claude/Codex
counter-review pass. Merge duplicate-root evidence instead of inflating counts.
`CLAUDE.md` and `CODEX.md` remain thin shims to this shared rule.

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/mobile/README.md` (Goat OS Android app — one common role-aware app for field operator + leadership; native Kotlin + Compose, `apps/goatos-android/`, app id `sg.mesha.goatos`; read before any mobile work)
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Android verification is not allowed to stop at a missing inherited `JAVA_HOME`
or unavailable USB phone. Run `make android-doctor`; repo tooling resolves the
pinned JDK/SDK itself. Run `make android-dev-run`; it prefers an authorized
physical phone and otherwise starts/waits for the configured emulator. See
`docs/runbooks/android-dev-device.md`. JDK 21 runs Gradle/AGP; app bytecode
compatibility remains Java/Kotlin 17.

Historical planning/archive docs were removed from the active tree. If a human
explicitly asks for archaeology, use git history or source material rather than
normal build docs.

Purpose:

- Goat OS is the operating system for mixed-species herd-animal identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Canonical backend/data model/app APIs are built fresh.
- `Goats and Parks.docx` is the base source for herd-animal and park semantics
  across every slice. Any feature touching herd-animal identity, species/breed
  labels, park/shed scope, shed tags, lifecycle/stage, pregnancy/lactation/
  warm-up/fattening, feed safety, weighing, handling, medicine administration,
  park roles, or feed sessions must start from
  `context/source-findings/goats-and-parks-source-findings.md` and must not
  invent conflicting semantics. Feature-specific docs may add stricter
  source-backed rules, but conflicts require an explicit source/owner decision.
- Scope lock: build exactly the user-approved slice, not adjacent product areas
  that the shared platform could theoretically support. Generic foundations are
  allowed only when they serve the approved slice; visible routes, nav, seeded
  cards, mock data, screenshots, and handoff language must not imply another
  vertical is built. For the current admin-web review, the visible slice is Preventive Care (PC)
  Vaccination plus Admin/Data Ops config and vaccination SOP policy.
- Current admin-web frontend scope supersedes the old dashboard/admin product
  surface. For admin-web UI work, read
  `context/frontend/current-admin-web-scope.md`: build the connected Admin
  Config + Preventive Care (PC) Vaccination + vaccination execution context slice (rendered inside
  /vaccination, with shed detail under /vaccination/execution/sheds/{shed_id}),
  with Control Tower summarizing only process
  gaps. Old Operations/Legacy/SOP/counts/import routes are removed from active
  admin-web and must not be rebuilt unless scope is explicitly reopened. Parks is
  NOT a separate vaccination product route or sidebar entry.
- **NON-NEGOTIABLE — the ONLY admin-web UI/UX source of truth is the mock**
  `mock/goatos-dashboard-mock.html`. PORT its layout, structure, table shapes,
  empty states, icon system, spacing, and density. It is **not a color theme**.
  **Never reuse/adapt/recolor old admin UI** (`admin-primitives.tsx`, old
  cyan/slate palette, emoji icons, collapse-to-KPI layouts) — the old admin UI
  is gone; rebuild from scratch to the mock. MANDATORY before any frontend
  `git mesha-push`: `npm --prefix apps/admin-web run check:mock-fidelity` must
  pass + visual compare to the mock.
- Frontend product taxonomy is non-negotiable:
  - **Vertical** = business operating domain/department, such as Preventive Care (PC), Parks,
    Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
    Network. A vertical owns operational context.
  - **Module** = a concrete workflow/product inside a vertical, such as
    Preventive Care (PC) -> Vaccination, Preventive Care (PC) -> future Treatment/Deworming, Procurement -> Source
    Entry, or future Parks modules. Parks is a scope/context dimension for
    vaccination execution, not the owner of a vaccination module.
  - **Command lens** = top-level cross-module screen, not a vertical or module:
    Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows.
  Preventive Care (PC) is a vertical and must not use the syringe/injection icon; the syringe/
  injection icon belongs to the Vaccination module. Counts is a separate
  vertical, so Control Tower must not show raw goat census totals as its own
  KPI. Control Tower is for gaps, adherence, exceptions, escalations, and next
  actions.
- Frontend command-room/authority guardrail: Control Tower, Action Center,
  Calendar, Protocol Adherence, and Workflows are top-level screens only. Config
  and SOP Library are top-level Admin / Data Ops authority screens only. Do not
  duplicate them under procurement/source-entry, Preventive Care (PC), Parks, or any future
  vertical as routes, redirects, tabs, or nav items. A vertical can feed those
  top-level screens through a selected domain/filter/lens such as
  `?domain=procurement` or `?category=vaccination`, but it must not create
  nested routes like
  `/vaccination/adherence`, `/vaccination/config`,
  `/procurement/source-entry/action-center`, `/procurement/source-entry/control-tower`,
  or any `/parks/vaccination` nested command paths. Vaccination execution
  renders INSIDE /vaccination, never as a separate Parks route.
  **Ratified exception (maintainer decision 2026-07-19): `/feed/config`.** Feed
  authors a ration grid (ration group x shed tag x feed item -> grams per head),
  per-shed factors, the session template, and the per-workflow dispatch clock.
  That is a Feed-owned data model served by `/feed-config/*`, not protocol
  `rule_dsl`, and `/config?category=feed_direction` cannot render it. `/config`
  remains the single generic protocol-rule authority screen; `/feed/config` is
  classified `module-surface`, not `authority-screen`. This exception covers
  Config for Feed ONLY. No command lens (Control Tower, Action Center, Calendar,
  Protocol Adherence, Workflows) is exempt for any vertical, and none may be.
  The machine guard carries the same single-entry allowlist in
  `apps/admin-web/scripts/check-ia-guard.mjs`; widening it needs a new recorded
  maintainer decision here first.
- Config / Protocol Rules is a generic Admin / Data Ops authority screen
  (`/config`) for CEO/COO/superadmin users. It is not owned by Preventive Care (PC) / Vaccination.
  Preventive Care (PC) / Vaccination may link to `/config?category=vaccination`, but the Config UI
  must stay category/schema-driven: changing category changes the form fields and
  `rule_dsl`; do not show vaccination fields for `feed_direction`.

Code navigation (graph-first):

- For code-structure questions (callers, callees, dependencies,
  blast-radius/impact, diff review, architecture, hub/dead-code), query the
  `code-review-graph` MCP tools before broad file scans. Read files for what the
  graph cannot see: constants, config values, HTTP route strings, error text,
  and uncommitted code.
- Route by task shape, not ritual:
  - cold/review/diff: `get_minimal_context_tool` first, then one targeted graph
    query;
  - known symbol: go straight to `query_graph_tool`;
  - keyword/domain lookup: `semantic_search_nodes_tool`, then targeted graph;
  - single file/function read: read the file, then graph only for impact.
- Graph is the fast first pass for traversal; native Grep/Read is the fallback
  for graph blind spots. One graph query replaces many grep/read cycles when the
  question is graph-shaped.
- Setup is per-machine and agent-enforced on fresh clones: if `make ai-setup`
  has never run on this clone, the committed `ai-setup-guard` hook blocks the
  first real tool call with bootstrap instructions — run `make ai-setup` first,
  then resume the task (see `docs/ai/README.md`). The graph DB (`.code-review-graph/`) and
  Graphify outputs (`graphify-out/graph.json`, reports, cost files, cache) are
  gitignored and regenerated locally. To enable the portable setup, run
  `make ai-setup`; to rebuild local graphs, run `make ai-rebuild`; to verify the
  clone is wired without committed graph artifacts, run `make ai-doctor`.
- Maintainer-local only: the Graphify Mesha wiki/doc/visual graphs
  (`mesha_docs_graph`, `mesha_visual_graph`) are built from sources outside this
  repo and cannot be reproduced here. Use them if already configured; otherwise
  skip and use Grep/Read.
- **Agent tool choice (human)**: before a non-trivial task, read
  `docs/ai/agent-tool-routing.md` — Cursor for admin-web UI and small fixes;
  Claude Code (terminal `claude` in repo root) for contracts, backend engine,
  migrations, and multi-module work. No second IDE required.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

Organization boundaries:

- Mesha/VGoats, Heva, and Slice are separate businesses and must never be
  mixed in GitHub or Google Cloud operations.
- Goat OS belongs to Mesha/VGoats. Google Cloud work for Goat OS targets the
  `vgoats.com` organization and future `goatos-dev`, `goatos-stg`, and
  `goatos-prod` projects.
- Do not use Heva projects/orgs, Slice projects/orgs, or `hevaplatform` for
  Goat OS work.
- Do not modify or replace the legacy `goatos-sheets` project while creating
  Goat OS projects.
- Before any cloud/GitHub command that creates, updates, deletes, grants IAM,
  links billing, deploys, or changes configuration, verify and state the active
  account, organization, folder, project, and target repo. If the target is not
  Mesha/VGoats for Goat OS work, stop and correct context first.
- If any Google auth surface expires or cannot refresh non-interactively
  (`gcloud`, ADC, Cloud SQL Auth Proxy, Secret Manager, Google Drive/Docs/
  Sheets, or a Google browser session), do not stop at "token refresh failed"
  when the task requires Google access. Use browser-based reauthentication
  immediately: `gcloud auth login ravi@mesha.sg` for CLI user credentials,
  `gcloud auth application-default login` for ADC, or the relevant browser/
  connector sign-in for Drive/Docs/Sheets. After reauth, re-verify the active
  account, organization, project, and target before any write/deploy/config
  mutation. For Goat OS, the expected account is `ravi@mesha.sg` and the
  expected Google Cloud organization is `vgoats.com`.
- For read-only Google-backed data pulls, Cloud SQL queries, dashboard issue
  CSVs, or any request phrased as "use gcloud/browser login", follow
  `docs/runbooks/google-cloud-environments.md` -> `goatos-dev Read-Only Cloud
  SQL Access` before touching Chrome or dashboard UI. The default source is
  gcloud + Secret Manager + Cloud SQL Auth Proxy + Postgres, not dashboard DOM
  scraping.
- For GitHub operations in this repo, use the Mesha/VGoats repository token
  path: `git mesha-push main` for pushes and the `MESHA_GITHUB_PAT`-backed
  remote URL for direct remote/CI verification. Do not rely on whatever `gh`
  account is active; this workspace may also have Heva and Slice GitHub
  accounts configured, and those must not be used for Goat OS repo authority.
- Git commits from this repo must use a Mesha identity only. Before committing
  or landing, `git config user.email` must end in `@mesha.sg`; Heva, Slice,
  gmail, or personal identities are blocked by `make git-identity-guard` and
  the local CI common gate. The expected maintainer identity is
  `Raviteja <ravi@mesha.sg>`.
- **Staging deployment is manual Cloud Deploy only.** Do not create or wait for
  a `main -> stg` pull request, GitHub Actions workflow, or direct `stg` branch
  push as a deployment mechanism. Agents must deploy from a clean checkout at
  the latest approved `origin/main` using `docs/runbooks/stg-deploy.md` and
  `tools/deploy/stg-clouddeploy-*.sh`. Never push any local ref, local `stg`,
  `main`, `HEAD`, agent branch, or refspec directly to remote `stg`; the branch
  is not deployment authority. Run `make ai-setup` so the local guard blocks
  accidental remote `stg` writes. Do not bypass it with `--no-verify`.
- Create Goat OS cloud resources under `vgoats.com`, preferably in a `goat-os`
  folder, or directly under the org if folder creation is not available. Do not
  create Goat OS resources inside `system-gsuite` or `apps-script`.

Do:

- Keep architecture facts in `context/`.
- Treat every test or script labeled E2E as a production-path proof, never a
  seeded readback. E2E fixtures may insert only external/input facts required to
  start the scenario (for example tenant, herd animal, location, workforce,
  inventory, or authored configuration). Obligations, batches, completions,
  verification outcomes, SOP tasks/submissions, notifications/escalations,
  cancellations, and Calendar/process-integrity screen output must be produced
  by the same service, API, durable event consumer, sweeper, canonical-read
  query path, or projector used in production. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the Calendar,
  process-integrity, and vaccination shed/execution/operations screens are
  served at the current 5k-50k envelope directly from canonical indexed SQL —
  the `calendar_event_projections`, `process_integrity_projection_rows`, and
  `vaccination_shed/execution/operations_projection_rows` projection tables are
  retired, not replaced by a seeded stand-in. E2E for those screens must still
  drive the real canonical-read path end to end; if a screen later earns its
  own projection under that ADR's scale-out ladder, this same production-path
  requirement carries over to that projector. A narrower test that
  intentionally seeds derived state must live with the owning package as an
  integration/read-model test and must not appear in an E2E report.
  `tools/agent-hooks/check-e2e-kernel-integrity.sh` enforces this rule for both
  Claude and Codex and in CI.
- Couple migrations to initial seed setup. If a migration changes tenant/goat/
  RFID/location, HRMS/ownership, founder grants, protocol/SOP/capacity,
  obligation/completion/proof, notification/verification, or app-visible
  projection/read-model tables, update the matching seed command,
  seed/projection test, or seed runbook in the same patch. The
  `seed-migration-guard` target is part of `make guardrails` and blocks
  schema/read-model drift where source rows seed correctly but the live app reads
  empty or missing projection tables. See
  `docs/runbooks/initial-seed-migration-coupling.md`.
- Do not make seed scripts hand-fill every new table. Classify setup tables as
  source/canonical, derived/read-model, static catalog/config, or
  operational/audit/event. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the default at
  the current 5k-50k envelope is that a new app-visible surface is served by a
  canonical indexed SQL read — no new projection table, and no closeout wiring,
  for that default case. A derived/read-model table exists only where it
  survives this envelope (the vaccination eligibility rollup and counts
  summaries) or where the ADR's scale-out ladder later adds one for a specific
  measured hot read. Any such surviving or newly-added projection table must
  still be rebuilt from canonical data through `make seed-closeout` /
  `tools/dev/seed-closeout.sh`, and still needs access-pattern indexes,
  freshness/version state, and an explicit partitioning decision at the point
  it is introduced.
- Register projection closeout by app-visible output, not just by executable
  name. If one projector command owns multiple read models, `seed-closeout`
  must pass explicit flags for each output. Any default-false `-project-*` flag
  for a visible read model must appear as `-project-...=true` on the owning
  command invocation in closeout, and the guard must verify the executed
  `tools/dev/seed-closeout.sh --dry-run` output rather than raw shell text. A
  commented, disabled, or uncalled invocation does not count, or the seed can
  claim the projector ran while leaving that table empty.
- Projection-backed operator pages must follow the last-known-good serving
  contract. No first projection, no serving rows, or a requested window outside
  projected coverage may fail closed. A stale/yellow/rebuilding/failed/over-TTL
  projection that still has serving rows covering the request must serve those
  rows with freshness metadata instead of taking the page down. See
  `docs/decisions/high-scale-dashboard-projections.md`.
- Source-backed vaccination seed means the whole executable setup, not goats
  alone: founder grants, HRMS roster, attendance/leave, timetable-backed
  positions, strict shed manager/backup mapping, position duties, published
  `vaccination.matrix` config, trusted vaccination history, generated future
  obligations, generated drive batches, and deterministic closeout. Missing
  HRMS/config is a failed seed, even when goat rows exist. A reseed/import/local
  proof is also failed if it stops after generation and leaves visible-window
  `scheduled`/`due` vaccination obligations unbatched; `tools/dev/seed-closeout.sh`
  must run the obligation sweeper and fail on that condition.
- Every accepted live goat in seed/import/dev data must resolve to a real active
  shed. During the current build phase, missing source placement is completed
  deterministically into an explicit seed-intake park/shed; do not skip the
  animal, leave `shed_id` blank, or fall back to a park/tenant vaccination
  obligation. Goat vaccination obligations are **shed-scoped only**; park is
  the drive execution/grouping scope. Required guards:
  `make goat-shed-scope-guard`; post-seed DB proof:
  `make goat-shed-integrity-db-proof` or `tools/dev/seed-closeout.sh`.
- Vaccination seed shed names must normalize raw partition labels before
  canonical `locations` writes. `Gandhi 1`, `Gandhi 2`, `Gandhi 3` are one
  physical shed `Gandhi` with partitions `1`, `2`, `3`; `Godel 1 - Part 3`
  is physical shed `Godel 1` with partition `Part 3`. Never seed those raw
  partition strings as separate physical shed buildings. Drive planning and UI
  must show physical shed -> partition -> operator assignment, with capacity
  counted as unique animals per assigned operator/day.
- Vaccination drive batching is park-level, animal-first, and safe-window-bound.
  Shed count is never a merge constraint; it is display/proof detail. A 1-2
  animal drive is valid only after proving no compatible same-park animal group
  can join between that group's due/ready date and binding safe-until date.
  Normal per-drive animal caps are soft on the last safe day, but the per-animal
  shot cap remains hard. Reseed/local proof must run
  `make vaccination-drive-clubbing-db-proof` after sweeper closeout; without it,
  Calendar/Full Schedule screenshots are not batching evidence.
- Vaccination source dates are base history anchors, not open due work. A seed
  or reseed must preserve trusted past dates as accepted history, suppress any
  seed-created open work on or before the backend business date, and let the
  vaccination kernel generate only future obligations from that base. Seed code
  must not hand-roll kid/adult path selection; it must use the live vaccination
  schedule-path helper/config so stale source tags such as `origin=birth` or
  `K1/K2` cannot force old kid-course work. Raw source vaccination cells also
  must not be pre-mapped as kid-course history to prove their own schedule path:
  classify first from independent evidence, then persist the source date as the
  selected rule family's history anchor. The concrete checklist lives in
  `docs/runbooks/vaccination-seed-source-date-contract.md`.
- After any destructive seed, bulk import, fixture reset, or large canonical
  backfill, refresh Postgres planner statistics for the touched canonical
  tables before projector recompute or latency gates. The normal source seed
  must `ANALYZE` the freshly loaded location, HRMS, goat, protocol, obligation,
  event, and vaccination-completion tables after commit and before read-model
  projection. This prevents projection timeouts caused by stale empty-table
  planner estimates.
- Do not start local, staging, or production app code against a database that is
  behind that build's migrations. Apply migrations first, seed only canonical
  source truth second, run deterministic closeout/projectors third, then start
  API/admin/workers or mark the environment green.
- Do not serve the normal local API/admin-web from temporary worktrees under
  `/tmp`, `/private/tmp`, or `/var/folders`. Local stack wrappers must fail by
  default there so the browser cannot silently exercise a disposable checkout
  while the canonical repo is stale or dirty. Use
  `GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1` only for explicit throwaway
  experiments, never for handoff.
- Normal local laptop runtime must resolve exactly one Goat OS app database for
  API, admin-web, and mobile. Use the single detected `goatos-local-current`
  Docker DB or the `127.0.0.1:5433/goatos` fallback; if multiple Goat OS app
  Postgres containers are running, local launchers must fail instead of
  guessing. E2E/proof/load scripts must fail closed unless
  `GOATOS_E2E_DATABASE_URL` or `DATABASE_URL` is explicitly passed. Read-only
  E2E checks may target the normal `5433` app DB, but mutating proof/load
  scripts that create goats/proofs, replay outbox, insert history, or run
  migrations must always refuse `5433`. There is no override for mutating the
  normal app DB from E2E. Destructive/load tests must use an isolated DB with
  its own seed/cleanup, such as the explicit local GCP-kernel stack on `55432`;
  that stack must never become the default laptop runtime DB.
- Deploy `goatos-stg` through Cloud Deploy. Build systems may create images and
  Cloud Deploy releases, but Cloud Run service/job mutations for staging belong
  to `deploy/clouddeploy/stg/clouddeploy.yaml` and
  `tools/deploy/stg-clouddeploy-task.sh`. Direct `gcloud run services update`,
  `gcloud run jobs update`, or manual migration execution is break-glass only
  and must be followed by a Cloud Deploy release from the same commit; see
  `docs/runbooks/cloud-deploy-staging.md`.
- Never amend an already-applied Postgres migration or baseline to repair a
  shared environment. Ship the next numbered forward migration, because STG
  records migration checksums and will fail before pending repairs if an earlier
  applied file changed. Before declaring any migration-backed STG fix complete,
  verify `public.goatos_schema_migrations`, the live table/column/data contract,
  and `/readyz`; see `docs/runbooks/stg-deploy.md`.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.
- Golden frontend rule for Codex, Claude, and every developer using this repo:
  admin-web/goatos-android (mobile) are renderers, not product-truth owners. Backend
  OpenAPI/app contracts must own visible navigation, route availability, page
  titles, section/table labels, filter/sort/page-size semantics, chips/tabs,
  row-click params, drawer/action labels, empty/error copy, disabled reasons,
  and summary-vs-detail field sets. Frontend may own layout, CSS, responsive
  density, icon-token rendering, focus/hover state, and local open/closed or
  selected-row state only. If a visible label/control/action is hardcoded in a
  frontend page, either move it into a backend contract plus OpenAPI/generated
  client, or document the temporary exception in `context/frontend/` before
  shipping.
  Backend-owned does not mean backend-code hardcoded live data: tenant/location/
  person/goat/shed/vendor/operator IDs, park codes/names, capacities, role/actor
  scope, permissions, and business-managed dropdown vocabularies must come from
  Postgres/source-backed config and be compiled into the contract by backend.
  Stable UI text that rarely changes (nav/page titles, table/filter labels,
  chips/tabs, empty/error copy, disabled reasons) belongs in the backend
  bootstrap contract; when it needs runtime governance, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  These entries may not relabel live/module-DB-owned options such as parks,
  sheds, breeds, SOP labels, feed items, or role/grant scopes, and may not
  override semantic option metadata such as source-system publishability.
  Frontend must not ship local defaults that later get replaced by async config.
  Backend code may hold only product contract shape, compile mapping, and
  intentional default skeletons for missing optional UI config rows; live/domain
  values stay in canonical module tables.
- User-facing copy firewall for mobile and frontend: CEO, director, and operator
  screens must use farm/product language only. Never show internal implementation,
  debug, test, or roadmap wording in visible UI copy, screenshots, empty states,
  toasts/snackbars, banners, cards, chips, buttons, bottom sheets, drawers, or
  alerts. Banned visible words/patterns include `V1`, `V2`, `debug`, `mock`,
  `fixture`, `Paparazzi`, `Room`, `outbox`, `idempotency`, `groupKey`,
  `payload`, `backend`, `frontend`, `API`, `route`, `PRD`, `TRD`, `TODO`,
  `local`, and `localhost`, unless the screen is explicitly a developer/admin
  diagnostics tool. Technical facts belong in docs, tests, logs, and code
  comments; UI must say the business thing: "Proof uploads in background",
  "Waiting for network", "Already scanned", "Needs proof", "Cannot submit yet",
  "Wrong shed", "Try again", etc. Before handing off any mobile/frontend UI
  change, scan changed strings/screenshot fixtures for internal words and inspect
  rendered screenshots for leaked technical copy.
- Same-page drawers, sidebars, modals, and popovers are client-local UI state.
  Ordinary open/close clicks must not navigate, issue a document/RSC request,
  or trigger page-level loading UI. Use `LocalOverlayLink` and a narrow local
  controller with Back/Escape/outside/X/focus restoration. When detail is not
  present in the list response, open immediately from list summary data and
  fetch only the missing detail inside the drawer through an authenticated
  Server Action/Route Handler. Query-only Next links, native anchor/forms, or
  router pushes used to toggle an overlay are banned. Keep
  `make admin-web-local-overlay-guard` at a zero legacy baseline.
- When the user asks to fix a frontend/UI issue, rendered browser review is part
  of the requested fix for Codex, Claude, and every developer. Do not treat it
  as optional judgment or defer it to the user. Reproduce the user’s route,
  viewport, scope, filters, drawer/modal state, and click path as closely as
  possible; if the user supplied a screenshot, that screenshot is the minimum
  acceptance case. Do not push a frontend fix until the changed screen has been
  opened locally and visually checked, or until you explicitly report why local
  rendering is blocked.
- Raw vaccination config/protocol tokens are never API presentation copy or
  user-facing UI copy. Codes
  such as `et_tt`, `et_tt_adult_w2`, `ppr_booster`, `blue_tongue_first`,
  `goat_pox`, the protocol family name `Preventive Care Vaccination Matrix`,
  and similar backend/config identifiers may exist in backend config,
  raw storage/contracts/DTOs, non-UI tests, or a dedicated display mapper only.
  Backend display fields (`driveName`, `vaccineLabel`, `vaccine_labels`, card
  titles/subtitles, alerts), admin-web, Android screens, Paparazzi screenshot
  fixtures, cards, rows, chips, alerts, logs visible to operators, and generated
  UI galleries must render human labels such as `ET+TT`, `PPR · Booster`,
  `Blue Tongue`, and `Goat Pox`.
  `make ui-vaccine-labels-guard` is part of the standard guardrail/local-CI
  path and must fail any direct UI leak.
- Vaccination proof grain is SOP/backend-owned. Do not hardcode "per goat",
  "shed level", "camera only", or "gallery allowed" in admin-web or Android.
  Backend SOP/form DSL/proof policy decides the proof mode, subject scope,
  minimum/maximum proof count, capture sources, and verifier instruction; clients
  render that contract. Both modes must remain supported: per-goat video proof
  and shed-level video proof. A change from one mode to the other must never
  delete the unused mode, bypass GCS proof upload, skip verifier instructions,
  or invent proof requirements in mobile/frontend state.
- For frontend code changes, perform rendered visual QA before pushing. Open the
  changed local page, capture and inspect screenshots, and compare with the
  authoritative UI/UX source of truth, the mock `mock/goatos-dashboard-mock.html`
  (port its structure, not just its colors). Old dashboard/admin pages are NOT
  the visual target and must not be reused/recolored. Before push, run the
  mandatory gate `npm --prefix apps/admin-web run check:mock-fidelity`.
  Check pixel-level UI quality: sidebar/nav alignment, tab/title spacing,
  typography, color, card padding, chart sizing, labels, icons, empty space,
  overflow, clipping, and desktop/narrow responsive states. Do not accept
  typecheck/build or a `missing_config` page as frontend visual proof. For
  admin-web, run `npm --prefix apps/admin-web run smoke:visual:live` when the
  local backend/admin-web can be started; it captures desktop/narrow
  screenshots and runs layout/a11y/token-leak checks. Open the resulting images
  under `.codex-goatos-render/admin-web-screenshots/` and include the screenshot
  review result in the handoff before pushing. Build passing means only that the
  code compiles; it does not mean the UI ships.
  Route/table/drawer/popover changes also require reproducing the exact changed
  URL and viewport, then visually checking right-edge columns, horizontal
  overflow, clipped or stripped chips, active nav highlight, row-click
  destination, drawer/popup outside-click close, and drawer/popup close button
  behavior. A screenshot supplied by a reviewer/user is a failing visual test
  case until the same route is re-opened and the rendered screen is inspected.
- When a maintainer asks to "fix frontend" or reports a visible UI defect, treat
  frontend review as part of the fix, not as optional agent judgment. Use the
  same rendered lens for every session (Codex, Claude, Cursor, or sub-agent):
  reproduce the route, inspect the changed UI, catch adjacent clipping/overflow/
  close-behavior regressions introduced or exposed by the change, and document
  the visual proof. If a focused frontend fix is green, commit and push that
  focused fix to `main` promptly; do not pile unrelated visual-smoke fallout into
  one end-of-session batch. Split newly discovered adjacent UI bugs into their
  own focused commits unless they block the original fix's visual proof.
- Admin-web route/table/drawer/popover changes require a visual-closeout checklist
  before push. Reproduce the exact URL/viewport from any user screenshot when one
  exists, then verify: no right-edge/status-column clipping; no horizontal page
  overflow unless the table owns it; chips truncate intentionally without
  character-splitting or escaping their cell; drawers and popovers close by their
  close control and by outside click/back navigation; row clicks keep the correct
  module selected in the sidebar; and opened detail views show only the scoped
  real records for the clicked row. If any of these cannot be visually confirmed,
  the change is not ready to land.
- Read wide, write narrow: agents may inspect the whole tree, but edits must stay within declared task scope.
- Treat scale-safe design as a hard requirement on every design, prompt, and
  code change, sized to the current release scale target. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the present
  release envelope is 5,000-50,000 animals, with query-plan proof required at
  the envelope's upper bound (up to ~500k obligation rows); one-million to
  1-5M-animal deployment is the future certification bar, not a present
  release requirement. Regardless of that target, before accepting any new query,
  worker, import path, reporting path, or UI data flow, check the scale shape:
  tenant/run scoped, indexed, chunked or paginated, bounded in memory/
  goroutines, idempotent for retries, and covered by query-plan validation when
  it touches large tables.
- Treat hot API/SSR latency as part of scale safety, not polish. Every
  operator-facing API read, admin-web SSR page bootstrap, dashboard, schedule,
  calendar, worklist, and drawer/list load has a hard sub-500ms budget under the
  API latency policy (`tools/perf/api-latency-policy.mjs`: p90 <= 300ms,
  p95/p99 <= 500ms). A seconds-class load is a bug even when `make ci-local`
  passes; `ci-local` is not latency evidence unless the live latency gate ran
  and recorded samples. Do not fix this with bigger limits, longer timeouts,
  skeletons, prefetch, or frontend caches. Fix the serving shape: narrow endpoint
  for the screen grain/window, indexed/keyset query, batched read, or accepted
  projection/read model.
- NEVER write these scale anti-patterns in `backend/internal/**` (request paths,
  app services, worker repo methods). They are fast at ~1k rows and fatal at 1M.
  Each is machine-blocked by `make scale-guard` (CI `guardrails` job); named,
  explained, and given its approved alternative in
  `docs/decisions/scale-anti-patterns.md`. The rule underneath all of them:
  **compute-on-write (projections), never compute-on-read** — with one scoped
  exemption: per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the five named
  Calendar/process-integrity/vaccination-shed/execution/operations screen reads
  carry a scoped `// scale-guard:ignore: 5k-50k-envelope` annotation and serve
  canonical indexed SQL directly at the current 5k-50k release envelope. The
  guard is NOT globally disabled: compute-on-read stays banned for every other
  path in `backend/internal/**`, and the exemption is removed from a screen the
  moment it earns its own projection under that ADR's scale-out ladder.
  - **compute-on-read / god-CTE** — reconstructing derived state from raw
    event/instance tables per request via a big multi-CTE query. Use a
    materialized read model updated on write; the request does an indexed lookup.
  - **capped read-time rollup presented as truth** — fetching a larger raw page,
    grouping in app/service/frontend state, then clearing pagination/cursor and
    showing the collapsed card/count as business truth. This is banned for
    Calendar, Action Center, and other operator projections. Put the grouped row
    in the projector/read model and prove it with seed/projector E2E.
  - **full (stop-the-world) MV refresh** — `DELETE FROM <projection> WHERE
    tenant_id` + full reinsert. Use incremental (outbox-delta) maintenance, or a
    version-swap; never whole-tenant delete+reinsert.
  - **N+1 query** — a `.Query/.QueryRow/.Exec/.SendBatch` inside a `for`/`range`.
    Use one set-based statement (`UNNEST`, `INSERT ... SELECT`, `CASE` bulk update).
  - **N+1 fan-out (the nested "N+2" case)** — a ctx-taking call to an injected I/O
    dependency (repo/reader/port/client/roster/ownership) inside a `for`/`range`,
    where the real `.Query/.Exec` sits one adapter layer down — invisible to the
    raw-driver **N+1 query** check above. "Small data, still slow": one round trip
    per row, so a 25-row page becomes 51 serial reads. Machine-blocked as the
    `n-plus-one-fanout` rule (`make scale-guard`, distinct from `n-plus-one`;
    baselined debt in `tools/scale-guard/baseline.txt`). Fix by batching to a
    single `*ByIDs` / `= ANY($1)` read (as `ShedSummary` now does with
    `ShedOwnerships`), not by looping a per-item service/port call.
    Vaccination operator availability is explicitly in this class: a sweep or
    preflight may probe many dates, but it must share one session cache at
    `(tenant, park, business_date, cap_per_operator)` grain across capacity
    scoring, `ConductedBy`, effective-cap, and assignment-split helpers. Do not
    call `AvailableVaccinationOperatorsForDrive` from those helpers independently
    or inside park/date loops.
  - **OFFSET pagination** — `LIMIT/OFFSET` with a growable offset. Use keyset/cursor.
  - **non-SARGable predicate** — `lower(col) LIKE '%x%'` / function on an indexed
    column. Use a normalized column, expression index, or `pg_trgm` GIN.
  - **column-side type cast in a predicate** — `indexed_uuid::text =
    ANY($1::text[])` can disable the ordinary index on the stored column. Keep
    the column bare and cast the typed bind array (`indexed_uuid =
    ANY($1::uuid[])`). Add a natural planner proof at a realistic row count;
    forcing `enable_seqscan=off` is not sufficient.
  - **polling full scan / unbounded worker tick** — copy the keyset-chunked
    `FOR UPDATE SKIP LOCKED` claim used by the obligation/idempotency sweepers.
  - **non-terminating pagination loop** — a read-page loop with no cursor advance.
    Guarantee forward progress (monotonic cursor or exclude processed rows).
  If a case is genuinely bounded, annotate it `// scale-guard:ignore: <reason>`;
  do not disable the guard. A green latency gate today means "correct shape", not
  "1M-proven" (gates run at ~1k rows — see the ADR's runtime-gap section).
  - **guard false-green across configuration blocks** — a static Terraform/HCL
    guard must parse/bound the resource and require related `name`/`value` (or
    equivalent) fields in the same block. Every guard self-test must include an
    adversarial sibling-block fixture, and `ci-local` must run both the
    self-test and the real check.
  The admin-web twin lives in `apps/admin-web/**`: a Next.js SSR helper that drains
  a paginated endpoint cursor-by-cursor into one array to compute a KPI (the
  `searchAllGoats` full-herd walk, removed in `810bc1b3`) is machine-blocked by
  `make admin-web-request-reads-guard`
  (`tools/agent-hooks/check-admin-web-request-reads.mjs`); read a projection/summary
  endpoint instead. Rule + rationale in `docs/decisions/scale-anti-patterns.md`.
- Treat every aggregate/projection/card/summary as a grain-and-identity proof,
  not an arithmetic exercise. Before writing or approving a query that combines
  `JOIN` with `COUNT`/`SUM`/`GROUP BY`, identify the canonical membership source,
  use the same stable group key on producer and consumer, prove every join is
  1:1 or deduplicate/pre-aggregate the many side, map farm/park/shed/cohort with
  an explicit scope matrix, and keep totals/reminders independent of UI page
  size. Tests must adversarially cover one-to-many fan-out, shifted
  due-vs-execution dates, every supported scope depth, page boundaries, and the
  live DB status matrix when status buckets exist. Add the nearby
  `projection-review:` evidence marker defined in
  `.agents/skills/goatos-code-review/references/aggregates-and-projections.md`
  and run `make aggregate-projection-guard`; the required CI guard includes
  committed, staged, unstaged, and untracked changes.
- Cross-surface count parity (Claude AND Codex): the SAME business fact must show
  the SAME number on every surface that renders it — admin-web, the mobile app,
  and the API. If two surfaces disagree (e.g. a drive shows 200 doses on the web
  operator schedule but 400 on the mobile calendar), one backend read model is
  wrong even if each query is internally consistent — the frontend/mobile is
  usually faithfully rendering a wrong backend number, so "web fine, mobile
  broken" is really "two backend read models of the same fact disagree". Pick ONE
  authoritative source+grain per business count and reuse it across surfaces
  (for "animals in a drive/day" that is `count(DISTINCT target_id)` over the real
  `vaccination_drive_assignments`, the grain the operator schedule uses). NEVER
  render an estimate/rollup column (`estimated_targets`, `estimated_*`,
  `*_quantity`, cached counters) as a user-facing count while a sibling surface
  reads the actuals. When you add or change a count shown on more than one
  surface, prove parity in the same change next to the `projection-review:`
  marker and add a test asserting the surfaces resolve to the same source/grain.
  Full rule + the 200-vs-400 incident:
  `docs/decisions/scale-anti-patterns.md` -> "Cross-surface count parity".
- Mobile Vaccination Overview current-drive count lock (maintainer decision
  2026-07-29): the mobile Vaccination Overview top summary is NOT a CEO
  adherence KPI, NOT an obligation-history rollup, NOT a shift/carry-forward
  number, and NOT a dose-administration count. It must answer only: for the
  current visible vaccination drive, how many distinct animals are in the drive,
  how many have been vaccinated/submitted, and how many are left. The denominator
  must be the real current drive animal membership
  (`count(DISTINCT target_id)` over the drive assignment/membership grain; for
  the current STG ET+TT example this is 324 animals, never 1296). Do not join
  protocol dimensions/rule rows/dose rows in a way that fans out animals. Do not
  include completed history from older drive dates unless those animals are part
  of the current visible drive membership. Any mobile change touching this card
  must include a regression test for a multi-dimension ET+TT rule where the
  display remains 324 total animals and shows vaccinated vs left from the same
  drive grain.
- E2E publishing rule for Codex, Claude, and every feature agent: any generated
  E2E result for a feature, fix, audit, or scale gate must be committed inside
  this repo and surfaced on the GitHub Pages CI report site before handoff. The
  report must appear as a card/list item on the main CI reports index
  (`https://vgoats.github.io/goatos/`), not only as a standalone deep link.
  If the E2E belongs to an existing category (for example a vaccination kernel
  story belongs inside `/e2e-report/`), add it inside that category's report;
  do not create another root card. Create a new root card only for a genuinely
  new report category, then wire that category in `.github/workflows/pages.yml`
  and document it in `docs/runbooks/github-workflows.md`. The detail page must
  follow the existing E2E report visual contract: self-contained HTML with
  title/subtitle, summary tiles, pass/fail/pending badges, report sections, and
  readable code/evidence blocks. It must explain the test in enough detail for
  a reviewer who did not write the code: what behavior is under test, why it
  matters, setup/data, action/trigger, assertions, evidence source, and any
  certification boundary. One-line headings or test names are not enough. Do
  not publish raw markdown, a bare `<pre>`, screenshots-only evidence, or a
  hidden artifact as the final report.
  Do not leave E2E reports only in `/tmp`, scratchpads, attachments, local
  `.codex/` or `.claude/` folders, or chat. State clearly when a report is local
  E2E only and not staging or production certification. Before handoff, verify
  the live root index and the report URL with `curl`; if GitHub Pages caching is
  in play, include a `?v=<commit-sha>` cache-busting URL plus the workflow run.
- Treat the operational kernel as the golden rule for every feature. Read
  `context/architecture/operational-kernel.md` before designing or implementing
  triggers, obligations, reminders, notifications, deadlines, escalations,
  dashboards, Calendar, Action Center, Protocol Adherence, Workflow, or
  process-integrity views. Every feature must answer: what process was expected,
  was it followed, where did it break, who owns next action, what is due by
  when, what evidence proves it, and what alert/escalation fires when a deadline
  is crossed.
- For any projection-backed serving read behind a freshness/coverage gate
  (Vaccination execution/operations/shed, CT/AC/PA, Calendar), follow the
  Serving-Read Freshness Contract in
  `docs/decisions/high-scale-dashboard-projections.md` (also in the
  `kernel-scale-lens` skill), Claude AND Codex: (1) freshness TTL must exceed the
  projector refresh schedule (jitter headroom) and is AGE-based — never widen the
  TTL to mask a date-coverage bug; (2) date coverage is inclusive-query vs
  exclusive projection `date_to` — project one day beyond the max query range
  (45d ⇒ 46d), calendar/day-based projectors must align default windows to
  business-day boundaries and cover the UI's supported week/month query windows
  (for example Monday-start weeks and previous/current/next first-to-last-day
  month picker requests)
  rather than `now±N` clock instants, and fixed-date tests seed the window
  around their fixed dates; (3) stale last-known-good rows serve with freshness metadata while
  no first projection or an uncovered date/window fails closed; (4) a rebuild
  keeps serving last-known-good and a failed rebuild never clobbers it; (5) reads
  served entirely from a bounded canonical index (completed/accepted history)
  are NOT gated on the hot projection; (6) a prune of
  non-serving versions re-derives `serving_projection_version` inside the DELETE,
  never a version captured before the txn/advisory-lock released.
- Treat every Android READ screen as offline-first with Room as the single source
  of truth for the UI (hard rule — Claude, Codex, and humans). Backend owns the data;
  on-device, the screen renders from Room and the network refresh runs in the
  background (stale-while-revalidate): persist every backend read response to Room,
  have the repository expose a `Flow` the ViewModel observes, refresh-on-open to upsert
  Room (which re-emits), and show a sync/stale indicator — NEVER a blank/loading wall
  on re-entry when cached data exists. A network-only read repository (a thin
  `api.xxx()` pass-through with no Room persistence) is BANNED for screen-facing reads;
  new read models ship with their Room entity + DAO + Flow from day one. Do not call
  the app "offline-first" until the read models are cached (bootstrap + the write
  outbox already are; Calendar/Control-Tower/Execution/Adherence/Insights must be
  migrated). Full rule + the NetworkBoundResource pattern:
  `docs/decisions/android-offline-first.md`; refs the Android data-layer + offline-first
  architecture guides.
- Every Android READ screen must be refresh-on-open (hard rule — Claude, Codex,
  humans): call the shared `sg.mesha.goatos.core.ui.RefreshOnResume { onEvent(XEvent.Refresh) }`
  composable (`core/core-ui/.../RefreshOnResume.kt`, wraps
  `LifecycleEventEffect(Lifecycle.Event.ON_RESUME)`) near the top of the screen's
  composable body so cached Room data shows instantly and a background refresh
  fires automatically every time the user lands on or returns to the screen — a
  retained ViewModel on the nav backstack must never show data that was only
  fetched once at ViewModel creation. Never rely on a manual sync button/icon as
  the only way to see fresh data; a visible sync affordance is allowed as a
  supplementary manual trigger, not the primary refresh path. Skip this only for
  screens where a resume-triggered refresh would disrupt in-progress user input
  (scan-capture flows, forms, mid-entry screens) — the ViewModel's `refresh()`
  itself must stay non-blocking (upsert Room on success, leave cache visible on
  failure) so this never produces a loading wall. See
  `docs/decisions/android-offline-first.md`.
- Every Android READ screen's sync/refresh icon must show a spinning animation while a refresh
  is in flight and become non-clickable to prevent duplicate refresh triggers (hard rule —
  Claude, Codex, humans). Use the shared `SyncIconButton` composable
  (`sg.mesha.goatos.core.ui.SyncIconButton`, `core/core-ui/.../SyncIconButton.kt`) on every
  screen with a manual refresh affordance: pass `isSyncing = state.isRefreshing` (or
  `refreshInFlight` if the screen names it differently) and `onSync = { onEvent(XEvent.Refresh)
  }`. When `isSyncing` is true, the icon continuously rotates 360° and the button is disabled,
  so duplicate taps are ignored. When false, the icon is static and clickable. Applies to all
  read screens with visible refresh buttons: Calendar, Sheds, Counts, Approval, Verify (queue),
  Leadership (all three screens), and any future read screens that expose manual sync. This is
  consistent across the app and prevents race conditions from overlapping refresh requests.
- Treat every Android Room schema change as an installed-APK upgrade contract, never just a
  fresh-install schema (hard rule — Claude, Codex, humans). Room builds a DB two ways: a fresh
  install runs `createAllTables` (every @Entity), but an in-place upgrade runs ONLY the registered
  `Migration` objects and then validates against the @Entity set — so an @Entity added to a
  @Database with no migration to CREATE its table compiles, works on fresh installs, and CRASHES
  every upgrade on open (`Migration didn't properly handle <table>`). This actually shipped
  (roster_timetable_cache / roster_coverage_cache, MOB-007) and a plain in-memory Room test is
  blind to it. Required: `exportSchema = true` + committed `schemas/<db>/<version>.json`; every
  version bump ships its `Migration(N-1, N)` that creates exactly the new tables/columns/indices;
  additive + non-destructive (no `fallbackToDestructiveMigration` — the outbox holds unsynced
  operator writes, the cache is the offline SSOT); and BOTH a schema-equivalence `*MigrationTest`
  AND an upgrade-crash `*UpgradeCrashTest` (seed an old-version file via a test-only old @Database,
  reopen with current schema + real migrations, assert no crash + data preserved). Machine-blocked
  by `make room-migration-guard` (`tools/agent-hooks/check-room-migration-safety.mjs`, diff-scoped,
  in the CI `guardrails` job). Full rule: `docs/decisions/room-migration-safety.md`.
- NEVER fetch more than one screen-page of rows on mobile/web (hard rule — Claude,
  Codex, humans). A phone viewport holds ~7-10 items; pulling 50/200/1000 rows to
  render is the mobile twin of compute-on-read. Machine-blocked by `make mobile-guard`
  (`tools/agent-hooks/check-mobile-list-fetch.mjs`, diff-scoped in CI so a commit with
  no mobile code passes instantly); rule + rationale in
  `docs/decisions/mobile-data-fetch-anti-patterns.md`. The rules:
  - **Calendar week/month overview = DOTS ONLY** — one per-day marker (a drive exists;
    optional tone) from a backend day-marker set (`includeDateMarkers` /
    `CalendarDateMarkerDto`). Never fetch or parse a day's events to draw the grid.
  - **Every drill level paginates** — L1 day list, L2 sheds, L3 vaccine-capture
    (done/pending/skipped animals) are each a keyset page of **~20** with infinite
    scroll (prefetch next at item ~17-18). Never request > ~20 rows in one page,
    and never show a tappable "Load more" row/button for normal mobile work
    queues. Pagination is app-owned viewport behavior; users should see only the
    work list plus a passive loading footer while the next page is already in flight.
  - **A vaccination drive is a park visit with a mix of SHEDS, never grouped by
    vaccine** — one drive can contain one or many sheds. Coverage-by-vaccine is a
    metric, not the drive grouping.
  - **Vaccination date moves/reverts are kernel writes, not read-model sidecars** —
    moving a vaccine from a mixed operator-cap drive must update raw
    `vaccination_drive_assignments` membership so the old date loses only that
    vaccine and the target date gains it. Reverting by selecting the original
    date must cancel the active override and restore the original raw
    assignment membership. Never declare this fixed from frontend banners or
    read-time `COALESCE(override_date)` behavior; E2E must assert raw DB rows
    across move and revert while preserving all clinical rule outputs
    (kid/adult, boosters, live/killed spacing, sick/ICU/pregnant/dead/cull
    deferrals) and operator animal caps.
  - Parse/transform each field ONCE (never re-parse inside `.find`/`.filter` → O(n^2)),
    off the Main thread (`Dispatchers.Default`, ideally in the repo via `flowOn`).
  - **Room is the single source of truth, so pagination binds BOTH layers** — the network
    fetch AND the Room read the UI observes use the same keyset + ~20 page size. NEVER
    `SELECT *` / `observeAll()` / an ever-growing accumulated blob; the observed read is a
    bounded keyset window (Room `PagingSource`; Paging 3 + `RemoteMediator` for large lists).
    Otherwise the over-fetch just moves from network to DB.
  If a case is genuinely bounded (e.g. a fixed 7-cell week loop) annotate the line
  `// mobile-guard:ignore: <reason>`; do not disable the guard.
  Android navigation has a separate structural invariant: backend-composed root
  destinations are L0 and alone own the bottom bar/drawer. Every L1/L2/L3/L4
  drill is a distinct hosted `NavHost` destination with Up/Back and no root
  chrome; exact route membership is mandatory, prefix matching and reusing an
  L0 route as a drill target are forbidden, and structural details must not be
  disguised as modal sheets. Machine-blocked by
  `make android-navigation-stack-guard`; canonical decision:
  `docs/decisions/android-navigation-stack.md`.
  The retention twin is memory, not fetch size: an in-heap cache/accumulator that
  grows with no cap/TTL/eviction, or a DAO reading a whole table into memory
  (`observeAll` `SELECT *`), OOMs the phone at scale (fixed in `7058fff2` +
  `d58acac2`). Machine-blocked by `make android-bounded-memory-guard`
  (`tools/agent-hooks/check-android-bounded-memory.mjs`, diff-scoped) — use an
  `LruCache` or a Room `JsonBlobCacheDao` with `readCachedJson` (TTL) +
  `enforceCacheBounds` (row/byte cap), or filter the DAO read to active rows
  (`WHERE status IN (...)`) / a `LIMIT` window.
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
- CI availability is never a closure blocker (Claude AND Codex). A GitHub Actions
  billing/spending/platform failure — the synthetic `BuildFailed` /
  `(Unknown event)` / zero-job `startup_failure` runs — must NOT be recorded as
  an external blocker or used to defer a fix. When remote GitHub Actions cannot
  execute, run the SAME affected-component gates LOCALLY via `make ci-local`.
  The default classifier compares the candidate to `origin/main`, always runs
  common repository guards, and adds backend, admin-web, and/or Android jobs only
  when their owned paths or shared contracts changed. Unmapped paths and changes
  to CI workflows, CI scripts, agent hooks, or the Makefile force the full suite;
  `make ci-local MODE=all` is the explicit full-suite command. Treat a green
  `make ci-local` on the exact pushed SHA as the authoritative gate. Record the
  `make ci-local` SHA + result as the current-SHA proof. Restoring org Actions
  billing stays a separate maintainer task, tracked but never blocking closure.

- **Postgres tests are explicit opt-in only**: Default `make ci-local`, every
  `JOB=...`/`MODE=all` invocation, pull-request workflow, push workflow, and
  scheduled workflow must not start Postgres or run Docker-backed DB tests.
  A local database run requires `GOATOS_RUN_POSTGRES_TESTS=1`. Hosted DB gates
  are not a Goat OS staging deploy path while GitHub Actions is unavailable.
  `MODE=all` means all affected component jobs, not Postgres.
  `GOATOS_REQUIRE_DOCKER=1` may make an explicitly requested DB run fail closed,
  but it must never opt a default run into Postgres by itself.

- **Exact-SHA local-CI push gate (main)**: Only a complete green `make ci-local`
  on the exact commit SHA authorizes a push to `main`. The pre-push hook installed by
  `make ai-setup` enforces this via a machine-local SHA-bound receipt
  (`goatos-ci-local-receipt.json` in the worktree git directory). The receipt
  is either mode `all`, or mode `scoped` bound to the exact remote-main base,
  component-rule hash, and complete classifier-selected job list. The hook
  recomputes scoped coverage at push time; a changed base, stale rules, missing
  component, or newly-full diff is rejected. Explicit partial
  `JOB=...` runs intentionally write NO receipt and never authorize a push. Every
  new machine guardrail MUST be registered in `tools/ci/guardrail-manifest.json`
  and wired into both `Makefile:guardrails` and `tools/ci/run-local-ci.sh` (the
  `guardrail-registration-guard` fails closed if any guardrail is missing,
  unvalidated, or unwired). See `docs/runbooks/local-release-evidence.md` →
  "Exact-SHA Local-CI Push Gate (Main)" and `docs/runbooks/local-ci.md` →
  "Guardrail registration and exact-SHA push evidence" for the full flow. Do not
  bypass the hook with `--no-verify`.
- **Mandatory automatic main landing (Codex and Claude)**: When the requested
  outcome includes pushing to `main`, run **`make land-main`** instead of composing
  `git fetch` / `git rebase` / `make ci-local` / `git mesha-push` by hand. The
  target refuses a dirty worktree, fetches fresh `origin/main`, rebases the
  candidate before CI, runs the complete affected-component `make ci-local`,
  fetches main again, and reruns rebase + CI if main moved before pushing the
  exact certified SHA. Agent hooks block direct agent-issued pushes to `main`,
  and the Git pre-push hook independently rejects a candidate that does not
  contain the current remote-main SHA. Do not auto-rebase at session start:
  sessions may open on dirty/shared worktrees with other agents' changes. Commit
  only the scoped work and use a clean isolated worktree for landing. Standalone
  `make ci-local` remains valid for development/hosted CI; `make land-main` is
  the release path that mutates history and pushes.
- Treat Goat OS time semantics as India-business-calendar semantics. Physical
  storage may use `timestamptz`/absolute instants, but every business meaning
  derived from those instants — scheduling, due/missed buckets, reminder keys,
  reporting groups, audit-log display, and UI labels — must convert to
  `Asia/Kolkata` first. UTC must never define a Goat OS business day.
- **VACCINATION TIME GRAIN IS THE BUSINESS DAY — NEVER HOURS (maintainer rule,
  Claude AND Codex).** A vaccination drive is a business DAY in `Asia/Kolkata`.
  It is not an instant, not a timestamp, and never "now ± N hours". This is a
  business rule about how the farm actually works, not a test-hygiene
  preference: operators work a day, a drive is planned for a day,
  `planned_date` is a `DATE`, and two vaccination facts on the same business day
  are the same day no matter how many hours separate their timestamps.
  - Never place a drive, due value, safe window, or query window with hour or
    minute arithmetic. No `time.Now().Add(-2 * time.Hour)`, no
    `dueAt.Add(-1 * time.Hour)`, no `now±N` clock instants — in production code,
    fixtures, or assertions.
  - Anchor to `biztime.BusinessDayStart(...)` / `biztime.BusinessDate(...)`, or
    to a fixed business date. Compare business DATES, not instants.
  - Never widen an hour tolerance to make a same-day comparison pass. If a
    same-day check fails because two timestamps differ by hours, the DAY is the
    correct unit and whichever side compares instants is the defect.
  - Why this is a hard rule: hour-anchored fixtures shipped a defect class where
    15 calendar tests passed or failed depending on the time of day they ran — a
    batched park drive anchors at 00:00 IST, the fixtures asked for `dueAt - 1h`,
    and that fell outside the window except during a ~1-hour slice of each day.
    Sub-day precision on a vaccination date is always a bug in the making.
  - If a specific case genuinely needs sub-day precision, it needs a recorded
    maintainer decision first. Ambiguity is not approval.
- Pinned-clock tests must derive time-sensitive fixture fields such as
  `valid_from`, `valid_to`, due instants, and recipient eligibility from the
  same pinned anchor. Never mix a pinned application clock with SQL `now()` or
  a second `time.Now()` when the fixture is evaluated against that anchor.
- For dashboards or reports that slice data by month, date, breed, farm, shed,
  load, category, status, gender, operator, source, or similar dimensions, use
  the canonical rule in `docs/decisions/high-scale-dashboard-projections.md`
  before coding.
- Add observability for new APIs/workers: latency, errors, DB pressure, queue lag, DLQ, and media failures.
- Goat identifiers (RFID, old tag, breed, farm, shed, partition) are operational
  livestock business data, NOT PII. Log them in diagnostics so a failure is
  traceable to the exact goat/row. The only logging redaction rule is secrets:
  never log credentials, tokens, or service-account JSON. Repo hygiene is
  separate and still applies: do not commit raw private source files or row
  dumps to git.
- Exception: Goat OS STG tester/demo login credentials that are intentionally
  documented in `docs/runbooks/stg-operator-login-credentials.md` are approved
  committed runbook data, not a review finding. Do not flag those STG
  email/password rows as leaked secrets unless the maintainer says they are no
  longer approved, they include production credentials, or they expose tokens,
  service-account JSON, API keys, private keys, or other non-demo secrets.
- Construct backend loggers via `backend/internal/platform/observability`
  (env sink `GOATOS_OBS_SINK`: `stdout_json`/`otlp`/`gcm`); do not hand-roll
  `slog.New` in new code. Log once at boundaries with trace/request/tenant/
  import_run_id context, and recover-and-log panics at goroutine edges. See
  `docs/decisions/observability.md`.
- Keep committed project docs role-based rather than person-based. Use labels
  such as data owner, reviewer, operator, CEO/internal admin, or vendor instead
  of individual names unless a legal/contract artifact explicitly requires a
  named person.

Do not:

- Shared local-stack identity and isolation (Codex and Claude): the browser-visible
  stack is exactly one atomic trio — admin-web `127.0.0.1:3300`, API
  `127.0.0.1:8080`, and database `goatos` in the named `goatos-local-current`
  container. FE and BE must run from the same clean checkout at **exact origin/main**.
  Start/recover it only through the persistent service wrapper;
  the supervisor owns both ports, discards ambient `DATABASE_URL` /
  `GOATOS_E2E_DATABASE_URL`, includes the `libpq` tools required by closeout,
  pins local query headroom so the canonical Calendar read cannot false-fail at
  the production-oriented 3-second deadline during local build load, and must
  verify an authenticated Calendar data-plane read in addition to `/readyz`.
  It must stop/restart FE+BE together if either child fails or `origin/main`
  advances. Never point the shared UI at a feature-worktree API or an alternate
  database, accept `/readyz` alone as proof that page data works, or start only
  half the shared pair.
- An **isolated E2E** stack is a separate test appliance with its own non-shared
  FE/BE ports and explicit throwaway database. It is intentionally independent
  of the shared exact-main stack. Shared-stack recovery must never stop, reuse,
  migrate, seed, fast-forward, or delete an isolated E2E process/container/DB.
  Conversely, E2E scripts must never claim `3300`, `8080`, or mutate the normal
  `5433/goatos` database. Inspect exact port owners and database targets before
  cleanup; do not infer that every local Goat OS process belongs to the shared
  stack. Enforcement: `make local-stack-service-guard`, required by normal
  `make ci-local`; operating contract:
  `docs/runbooks/local-full-stack-rehearsal.md`.
- Local dev servers (`:3300` admin-web, `:8080` backend): the workspace owner has
  granted agents (Codex and Claude) STANDING authority to stop, restart, re-port,
  or `next build` over them WITHOUT asking — just do it when the work needs it
  (clean build, or an expired local token making routes redirect to `/login`).
  Admin-web must be restarted through the local wrapper:
  `npm --prefix apps/admin-web run dev` / `dev:local`, including custom isolated
  ports like `npm --prefix apps/admin-web run dev -- --port 3318`. Plain
  `next dev` is forbidden because it bypasses `GOATOS_AUTH_*` env and makes
  `/admin-web/bootstrap` fail with `invalid_bearer_token`. Do not pause to ask
  permission for a restart/rebuild. The only
  discipline: restore the server on the SAME port, never silently change ports,
  don't run `next build` concurrently with a live `next dev` on the same `.next`
  (stop it first), and if you break it, restore it. See
  `apps/admin-web/AGENTS.md` → "Local Dev Server Safety" for the full rule. This
  applies to every agent (Codex and Claude).
- Always-on local stack rule (Codex and Claude): when a task needs any local
  frontend, backend, worker, importer, proxy, emulator, database container, or
  other Goat OS service, first check whether it is already running and do not
  stop it just because the immediate command is done. Prefer the persistent
  service wrapper (`make dev-local-service-start`, `make dev-local-service-status`,
  `make dev-local-service-logs`) over foreground one-off terminals for long-lived
  stack work. Leave required services running at the end of the turn/session
  unless the user explicitly asks to stop them or stopping is required to prevent
  machine damage/data loss. If code/env changes require a restart, restart on the
  same ports and health-check before reporting done. Do not finish with a needed
  app stack stopped, and do not leave required servers as active Codex terminal
  sessions that block the final response; use the service wrapper/supervisor and
  logs. Status/final updates must name what is running plus the URL/port. If a
  service cannot be kept running, state the blocker and the exact restore command.
- Founder/builder visibility invariant: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` are the
  platform-owner leadership cohort. In local, staging, and production seed/
  provisioning paths they must be granted `role='ceo_internal'`,
  tenant scope, and the RBAC grants needed for every built visible module.
  New features or visible route changes are incomplete until leadership
  seed commands, bootstrap/nav tests, and docs include the module. RBAC-based
  route visibility applies to non-founder operators, not to these five builder
  accounts.
  **A STG (or any) seed is INCOMPLETE until this grant is MATERIALIZED, not
  merely pending.** `auth_pending_email_grants` rows only become an active
  `user_scope_grants` row via the `/auth/session-events` runtime claim path,
  and admin-web Google SSO does not reliably trigger that path on the first
  login after a fresh seed — the observed failure is `403 permission_denied`.
  For STG, run `make seed-stg-9-person-login`
  (`backend/cmd/seed-stg-login-grants`), which materializes the ACTIVE grant
  directly: tenant scope for the 5 leadership accounts, park scope for named
  park staff/operators, keyed by `platformauth.StableSubjectID(issuer,
  firebase_uid)` — the same derivation the backend uses at request time. Verify
  with `make verify-stg-9-person-login` and
  `docs/runbooks/stg-9-person-login-verification.md` before declaring the seed
  done. `docs/runbooks/stg-login-seed-contract.md` is the canonical personnel
  rule this command implements.
  **Leadership log in with Google SSO OR Firebase email/password** (maintainer
  decision 2026-07-24; the prior SSO-only rule is retired — password convention
  `<FirstName>@2026`, maintainer sets the Firebase password). **All 9 accounts,
  leadership included, also need an active `workforce_members` profile**: the
  mobile `/app/bootstrap` hard-requires a profile row and returns
  `403 operator_profile_missing` without one, so leadership could open admin-web
  but got "Couldn't load your workspace" on the Android app until
  `ensureLeadershipMember` (in `seed-stg-login-grants`) created their
  `auth:<uid>` profile. Materialized grant alone is not enough; the profile is
  part of the completion bar.
- Operator scope invariant: no real operator may receive `scope_type='tenant'`.
  Operators belong to exactly one park (`scope_type='park'`, `scope_id=<park
  location_id>`) plus their explicit shed/task assignments. Tenant scope is
  allowed for platform leadership (`ceo_internal`) and director visibility
  roles (`pc_director`, future director aliases) when they must see both parks.
  If a director also needs to execute scanning work, give that person explicit
  operator-style park/task execution assignment; do not make the operator grant
  tenant-wide. STG login seed changes must pass
  `make stg-operator-scope-guard`; if this guard fails, fix the seed source
  instead of relying on downstream task filtering.
- Current active RBAC roles are documented in
  `docs/runbooks/current-active-rbac-roles.md`. Treat roles outside that list
  (for example `director_preventive_care`, `director_breeding`,
  `manager_feed`, `head_health`, `am_growth`) as dormant catalog scaffolding,
  not live STG/mobile personas. Do not grant or document them as current access
  without also shipping backend permission behavior, Android role handling,
  seed docs, and tests in the same change.
- CPT operator-drive rehearsal seed invariant: the committed packet at
  `fixtures/vaccination-cpt-operator-drive-2026-07-23/` is CPT/Channapatna only
  and uses business date `2026-07-23`. Do not synthesize CBE/Coimbatore rows.
  Seed Amit Kumar, Darshan Talwar, and Sagar Mahoor as equal vaccination
  operators with `200` unique animals/day/operator; seed Chandrakant as
  director-only monitoring scope. The `Adult` source filenames do not narrow
  the vaccination kernel: kid/adult/booster/clinical/combo-spacing/safe-window
  rules still come from backend vaccination rules.
  **Materialized grant + department binding is part of this invariant, not a
  separate concern.** Amit, Darshan, Sagar (operator role) and Chandrakant
  (`pc_director` role) are only real, working STG logins once their
  `user_scope_grants` row is `status='active'` AND their existing named
  `workforce_members` roster row (seeded by `seed-roster-real` /
  `seed-vaccination-cpt-operator-drive`) is bound to `user_id` with
  `department_id = preventive_care`, so `department_module_grants` gives them
  the vaccination bottom bar. `make seed-stg-9-person-login` is wired as a
  required final step of `seed-vaccination-source-full` and
  `seed-vaccination-cpt-operator-drive` when `GOATOS_ENV=stg` — do not seed CPT
  operator-drive rehearsal data on STG without it, and do not declare the
  rehearsal seeded until `make verify-stg-9-person-login` passes.
- Leadership assistant coverage invariant: every leadership-relevant table,
  read API, OpenAPI contract, admin-web route, mobile workflow, reporting view,
  domain event, or official KPI must resolve to a Cube governed metric, a
  `ceo_ai.*` view, an MCP Toolbox tool, a mapped Mesha read API, or a documented
  exclusion in `docs/ceo-ai/coverage-matrix.md` — in the same change. The guard
  is STRUCTURED, not keyword-based (tightened 2026-07-23): a new `CREATE TABLE`
  migration, a new OpenAPI `/path`, or a new exported read handler must ship a
  real coverage artifact (`ceo_ai.*` view / MCP tool / Cube binding / wired
  `Set*DataReader`) or a coverage-matrix row/exclusion NAMING that surface in the
  same commit; a bare keyword-bearing doc touch no longer satisfies it, and pure
  refactors pass without a coverage file. The
  read-path routing is Cube-first (official KPI → Cube; then read APIs → MCP
  Toolbox `ceo_ai.*` tools → read-only SQL fallback). The planner → catalog →
  wiring → reader chain must be LIVE and CLOSED end-to-end (ROUTE-CLOSURE rule):
  every tool name must resolve in the runtime registry (Cube binding, executor spec,
  toolbox tool, or fallback alias), every RouteAPI target must have a wired reader or
  fallback alias, and every coverage row must reference a golden eval question. HOW-TO:
  `.agents/skills/goatos-leadership-assistant/SKILL.md` (includes ROUTE-CLOSURE rules).
  Scaffold: `node tools/ceo-ai/scaffold-coverage.mjs <module>`. Enforced by
  `make leadership-assistant-coverage-guard` + `make assistant-route-closure-guard`
  (local CI + PostToolUse nudge for Claude and Codex).
- Do not reintroduce old staging labels as architecture.
- Do not commit generated Graphify/CRG graphs. `graphify-out/graph.json`,
  `manifest.json`, `GRAPH_REPORT.md`, `graph.html`, `cost.json` and the
  `.code-review-graph/` DB are gitignored and machine-regenerated locally. Commit
  ONLY the setup docs, rules, hooks, and generation scripts — never the graph
  artifacts themselves. Run `make ai-doctor` before pushing AI-tooling changes.
- Do not let the `ceo_ai` reporting/assistant namespace sit between the core
  Backend <-> Frontend <-> Mobile layers. `ceo_ai` is the leadership-assistant
  chatbot (`backend/internal/ceoai/**`, `/api/ceo-ai/*`) plus its read-only
  reporting SQL schema (`ceo_ai.*` views/functions read by Cube and the
  assistant). Data flows ONE way: core BE is the operator source of truth, and
  the assistant/Cube CONSUME it via Mesha read APIs, the MCP Toolbox, or
  read-only SQL. A core operator read path must never join `ceo_ai.*` or read a
  `ceo_ai_*` table for its own runtime data (this once 500'd Control Tower when
  the schema was absent — SQLSTATE 3F000). Shared display/derivation logic (e.g.
  vaccine labels) lives in a neutral core package such as
  `backend/internal/vaccination/domain`, read by both operator screens and the
  assistant. Frontend/mobile core pages must not route their data through
  `/api/ceo-ai/*` or import an assistant client module; the global assistant
  bubble in `MeshaShell` is allowed chrome, not a data path. Machine-gated by
  `make ceo-ai-boundary-guard` (backend SQL schema/table access, matched across
  newlines; plus FE/mobile `/api/ceo-ai` route + assistant-import coupling
  outside assistant-owned dirs); full rule in
  `docs/decisions/ceo-ai-reporting-boundary.md`.
- Do not let frontend/mobile read BigQuery, Sheets, Firestore, GCS, or operational databases directly.
- Do not spread vendor SDK calls through product code.
- Do not modify current live dashboard repos while building Goat OS copies.
- Do not add unbounded goroutines, full-table/full-herd API scans, direct media proxying through APIs, or dashboard raw BigQuery scans.
- Do not use direct gRPC for browser/React Native product clients without a new written ADR.
- Do not duplicate architecture decisions across random docs.
- Do not put individual staff/founder/vendor names into PRDs, TRDs, runbooks,
  prompts committed as docs, status files, or skill references when a role label
  is enough. The founder/builder visibility invariant above is the narrow
  exception because those exact accounts are provisioning seed truth.

Validation expectation:

- Run the narrowest relevant typecheck/build/test command for changed code.
- For DB query or migration changes on large tables, verify the indexed access
  path and add/update `make validate-sqlc-plans` coverage when the query is on a
  hot path or can touch import/goat/event/counter rows at scale.
- At phase closeout, compare code/contracts/migrations/tests against PRD/TRD and
  update context/skills/agent references if implementation changed the truth.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.

Morning README update expectation:

- For Goat OS work sessions that start in the morning, check whether `README.md`
  reflects the latest pushed project status before moving deep into new
  implementation work.
- If phase progress, shipped backend/frontend pieces, deploy gates, real-data
  import status, or next-step priorities changed, update `README.md` with
  executive status wording and push it.
- Be precise: do not call Phase 1 shippable until auth/RBAC, frontend screens,
  production event egress, and real data-run gaps are actually closed.

Workflow documentation expectation:

- If GitHub Actions workflows or CI guardrail scripts change, update
  `docs/runbooks/github-workflows.md` with clear project-facing wording in the
  same change.
- The runbook must explain what each workflow does, when it runs, what temporary
  services it starts, and what common failures mean.

Mock auto-push expectation (Codex AND Claude):

- Standing order (2026-06-25): whenever you edit the ops-console mock
  `mock/goatos-dashboard-mock.html`, commit and push it IMMEDIATELY — do not wait
  for confirmation, so the pushed copy is never behind local edits.
- Run `tools/agent-hooks/push-mock.sh` after editing the mock (Claude also wires
  it to a Stop hook). The script commits ONLY the mock file and pushes `main` via
  `git mesha-push` — it never `git add -A`, so unrelated in-flight work is left
  untouched. It no-ops when the mock is clean.
- This applies only to the mock. Other code/doc changes follow the normal
  review-and-push flow.

<!-- BEGIN TELEMETRY GUARDRAIL (generated by telemetry-guard lane; do not hand-edit inline, extend docs/observability/TELEMETRY_GUARDRAILS.md instead) -->
## TELEMETRY GUARDRAIL (mandatory)

Whenever you add or modify a user-facing surface — an Android screen,
viewmodel, or flow in `apps/goatos-android`; an admin-web route in
`apps/admin-web`; or a new product-meaningful event emitted anywhere — you
MUST wire:

1. **Firebase Analytics event(s)** — `AnalyticsPort.track(...)` with an
   `AnalyticsEvents` constant (never an inline string) on Android; a
   Faro event (`faro`/`trackEvent`/`pushEvent`) or route error-boundary
   coverage on admin-web.
2. **Crashlytics fatal + non-fatal logging** on error paths that surface can
   hit (Android; wiring itself is tracked as TODO — see the doc below).
3. **The relevant funnel/journey step**, when the surface sits on a tracked
   journey (`login → bootstrap → drive-open → scan → vaccination-capture →
   submit`, or a future documented funnel).

Run `make telemetry-guard` (or `python3 tools/telemetry-guard/telemetry-guard.py`)
before committing — it is part of `make guardrails`, the compatibility
`make ci-local JOB=guardrails`, and the affected admin-web/Android component
jobs. It is diff-scoped against `origin/main` so unrelated commits pass instantly.

Use `// telemetry:exempt <reason>` only with a real justification (internal
debug-only screen, pure presentational component, route fully covered by a
parent error boundary) — it is a reviewer-facing escape hatch, not a rubber
stamp.

Full rule, rationale, required symbol names (including what is wired today vs
TODO), compliant/non-compliant examples, and how the guard works:
`docs/observability/TELEMETRY_GUARDRAILS.md`.
<!-- END TELEMETRY GUARDRAIL -->
