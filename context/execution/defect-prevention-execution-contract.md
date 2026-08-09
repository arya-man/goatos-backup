# Defect Prevention and Remediation Execution Contract

Status: mandatory execution contract for bug fixes, audit-ledger work, kernel
milestones, new features, migrations, and cross-surface contract changes.

This contract exists because a correct sentence in a design document did not
stop the same defect family from reappearing in another module, adapter, client,
or release path. A change is complete only when it fixes the current failure and
makes the same failure difficult to reintroduce.

## Operational task-kernel non-deviation lock

Maintainer decision, 2026-08-10: Goat OS remains one event-driven,
interlinked task/ticketing waterfall. This is the product architecture, not an
optional implementation pattern:

```text
business event
  -> canonical domain transaction + audit + outbox
  -> owned task node with a policy-pinned clock
  -> bounded parent/child rollup
  -> reminder and acknowledgement-gated contact waterfall
  -> proof
  -> separately owned verification/sign-off task
  -> close/reopen propagation
  -> Today, Calendar, Action Center, Protocol Adherence, Workflows, and reporting
```

Modules keep authoritative domain facts and state machines, but they must attach
human work to this shared coordination chain. No module may create, retain as
canonical, preserve, or exempt a private task table as its app-visible
coordination authority, private scheduler, private owner fallback, private
overdue calculation, private reminder/escalation ladder, private verification
queue, or screen-only follow-up state. Existing local mechanisms are
compatibility sources to adapt and retire. A strict module
execution boundary changes the adapter direction; it never exempts the module
from shared coordination.

Every activated, app-visible work item is linked by stable source identity, has
a real owner, has an explicit clock/timing policy, and participates in the
bounded hierarchy when it has a parent. A materialization exception is a
separate durable operational failure owned by a real configuration/operations
resolver; it is never a substitute owner or a hidden task. Operator work and
verification/sign-off are separate sibling leaves under a work-unit container.
Acknowledgement stops contacts, not the work clock. Authorized descendant reopen
propagates upward. The owning transaction always records the domain change,
audit, idempotency, and outbox atomically. A direct transaction-aware adapter
may write task coordination there too. A module with a recorded strict
isolation boundary instead emits a complete durable event there, and a
shared-kernel consumer materializes task coordination in a separate,
receipt-backed transaction with version fencing, loud lag/failure outcomes,
replay, and source reconciliation. Screen-only and best-effort coordination are
forbidden in both shapes.

No feature may bypass this chain because the generic adapter is unfinished. Keep
the feature shadowed or blocked until its source behavior, ownership, clock,
adapter, reconciliation, and cutover proof are complete. Any proposed exception
or change to this architecture requires an explicit maintainer decision and a
same-change update to `context/architecture/operational-kernel.md`,
`context/execution/operational-task-kernel-remediation-plan.md`, this contract,
the relevant guard, and its adversarial tests. Ambiguity is not approval.

## Completion law

For every affected invariant:

```text
behavioral fix
+ failing-before production-path regression
+ recurrence control
+ ordinary local-CI wiring
+ recovery and observability where failure can survive deployment
+ current-SHA proof and independent review
= eligible for closure
```

Code that fixes the symptom but omits an applicable term remains open. A source
change may be labelled `source-fixed, closure-pending`; it must not be called
closed, shipped, or used as a prerequisite for a later cutover.

Not every defect needs a new static grep. The control must match the root cause:

- database constraints, foreign keys, transaction fences, and version checks
  protect persistent invariants;
- types, schemas, generated clients, and compatibility tests protect contracts;
- structural guards protect mechanically detectable dependency, configuration,
  registration, and banned-pattern boundaries;
- production-path tests protect runtime behavior and cross-layer handoffs;
- bounded reconcilers, durable outcomes, metrics, and alerts protect failures
  that depend on external state or can occur after deployment.

If a static guard would be brittle or would only recognize the original literal,
record why and use the stronger runtime/contract control. “No guard added” is not
an exemption from regression proof, CI routing, or failure visibility.

## Required batch record before implementation

Every remediation or feature batch records the following in its plan, PR, or
current-SHA proof packet before code is changed:

1. stable finding IDs or the canonical feature/decision source;
2. fresh `origin/main` SHA, exact branch/worktree, and owned file boundaries;
3. the exact failing scenario and the production reader/writer/caller;
4. root cause, sibling occurrences, and every layer the fact crosses;
5. invariant owner: module, shared kernel, platform, contract, client, or release;
6. authority, scope, grain, state machine, clock, idempotency, event, offline,
   migration, observability, and recovery impact;
7. recorded maintainer decisions or an explicit blocker for unresolved policy;
8. the prevention matrix below;
9. internal dependency and commit order inside the single integration PR;
10. the commands and artifacts that will prove closure.

The batch must scan the whole production pattern, not only the cited line. Use
the code graph and targeted search to enumerate callers, writers, readers,
generated mirrors, clients, seeds, repair paths, and tests. Merge symptoms with
one root; do not split one root cause across competing owners.

## Prevention matrix

Every root batch fills every applicable row. `N/A` needs a concrete reason.

| Layer | Required answer |
|---|---|
| Canonical rule | Which ADR, contract, state machine, schema, or module owns the invariant? |
| Implementation | Which production transaction/path enforces it, and which sibling paths were audited? |
| Regression | Which test fails on the pre-fix code and passes on the candidate through the real caller? |
| Persistent safety | Which DB constraint, lock, version fence, idempotency key, or compatibility rule prevents invalid state? |
| Mechanical prevention | Which structural guard detects recurrence, or why runtime proof is stronger? |
| Guard proof | Which adversarial self-test proves the guard catches aliases, multiline forms, sibling blocks, and renamed literals relevant to the rule? |
| CI routing | Which standard `make ci-local` job runs the real check and self-test for an ordinary affected diff? |
| Cross-surface parity | Which OpenAPI/generated-client/web/Android/Room fixture proves all consumers agree? |
| Operations | Which metric, alert, durable outcome, DLQ/repair, or reconciler exposes and repairs a deployed failure? |
| Agent memory | Which always-loaded rule, build/review skill, anti-pattern chapter, or module instruction must change? |
| Closure | Which exact SHA, commands, results, migration/deploy state, and independent counter-review close the item? |

For any operational feature, the prevention matrix must also name the business
event, stable task source identity, owner resolution, clock policy, parent/work
unit, proof, verifier/sign-off leaf, contact policy, acknowledgement behavior,
close/reopen behavior, shared read surfaces, and reconciliation path. Missing
one of these is a kernel-integration gap, not a later enhancement.

## Guardrail authoring contract

When a failure is mechanically detectable, its guard ships in the same batch as
the fix. A new guard is incomplete unless all of these are true:

1. It checks the semantic structure where practical. Import boundaries use the
   import graph; configuration rules bind related fields inside the same parsed
   block; producer registries enumerate raw literals and aliases as well as named
   constants. A file-wide substring is not architectural proof.
2. It has a positive fixture and adversarial negative fixtures for the original
   failure plus realistic evasions: alias imports, multiline syntax, raw
   literals, sibling blocks, renamed helpers, and empty/default cases as
   applicable.
3. Its self-test first demonstrates that the forbidden fixture fails, then that
   the allowed fixture passes. A self-test that only runs the happy path is not a
   self-test.
4. It is registered in `tools/ci/guardrail-manifest.json` with an owning doc,
   real check, adversarial self-test, Make target, standard CI step, and
   `requiredInCI` classification.
5. Required guards run from the normal affected `run_common` or component job in
   `tools/ci/run-local-ci.sh` and from `Makefile:guardrails`; compatibility-only
   `JOB=guardrails` reachability does not count.
6. A high-risk edit boundary also gets fast hook feedback when appropriate, but
   a hook never replaces local CI.
7. Diff-scoped guards with existing whole-tree debt use a shrink-only ratchet.
   New debt is fixed or explicitly exempted with a reviewed reason; it is not
   hidden by increasing a baseline.
8. The guard's own header states its blind spots. Production-path tests and
   runtime monitoring cover what static analysis cannot see.

When guard or CI files change, update `docs/runbooks/local-ci.md` and
`docs/runbooks/github-workflows.md` as applicable and run the registration
meta-guard. A green guard never substitutes for the failing-before test.

## Regression and proof contract

For a bug, the exact failure is a red test on the base before the fix. For a
net-new feature, the acceptance behavior is absent or fails on the base. For a
documentation/policy-only change, use structural validation and diff proof
rather than inventing a runtime failure. Preserve the applicable base evidence
in the proof packet. Green runtime proof must exercise the production entry
point, not a sibling helper or seeded derived row.

### Proof packet and receipt

Each internal batch owns a committed packet under
`context/execution/proof-packets/operational-kernel/<batch-id>.md` containing the
base SHA, stable IDs, scenario, invariant, commands, artifact hashes, recovery,
limitations, internal commit range, and independent-review disposition. It does
not claim its own commit SHA. Exact candidate identity and command results live
in a generated, immutable SHA-keyed local-CI receipt and the single PR's proof
index. F0 must define the receipt schema, signature/hash, storage, expiry,
required gate classes, and verifier. The integration landing gate consumes the
receipt; prose checkboxes do not satisfy it. After landing, the ledger records
the exact main SHA without rewriting historical packets.

Apply these axes when relevant:

- exact replay, same-key/different-payload, duplicate delivery, and stale version;
- concurrent close/create, claim/ack/send, cancellation, lease expiry, and
  restart/interruption;
- cross-tenant, cross-park, cross-grant, missing-owner, no-device, and ambiguous
  scope;
- populated PostgreSQL upgrade, lock contention, restart at every autocommit
  boundary, rollback/refusal, and old/new binary overlap;
- keyset pagination, upper-envelope query plan, bounded memory, and fair worker
  progress;
- OpenAPI generation, TypeScript/Kotlin consumer compatibility, Room migration,
  process death, offline replay, and logout session generation;
- late event, DLQ/recovery, no-recipient/no-route, reconciliation drift, and
  deployed schema/revision verification;
- rendered browser/device proof for user-visible behavior.

If the required harness does not exist, building the harness is part of the
batch. A mock, source grep, screenshot, successful enqueue, or skipped test is
not a substitute.

## Anti-pattern and agent-instruction closeout

Every fixed root cause is classified at closeout:

- one-off implementation mistake already covered by an existing invariant;
- repeated defect family needing a new or strengthened anti-pattern rule;
- missing architectural contract;
- missing guard or false-green guard;
- missing production-path test fixture;
- missing operational detection/recovery;
- missing task brief or skill routing.

Repeated patterns update the closest canonical anti-pattern or decision doc,
not a random status file. Add only the concise always-on rule to `AGENTS.md`;
put execution detail in this contract, the owning decision, and the relevant
build/review reference. Update module-level `AGENTS.md` files when the boundary
is local. Update phase PRD/TRD and `SKILLS.md` when the rule changes how future
work is routed.

Every delegated implementation-agent brief names the absolute repo path, base
SHA, owned files, applicable invariants, forbidden patterns, required red/green
tests, guard/CI work, and the rule that the agent returns a commit plus proof but
never lands or edits the shared ledger. Independent review agents are read-only:
they return findings and evidence, never implementation commits. The
coordinator independently reruns closure evidence.

Each accepted root batch gets a builder pass and a separate hostile judge pass.
When available, use `gpt-5.6-sol` at `xhigh` reasoning (or a strictly newer,
stronger successor) for the judge; record the model/reasoning and reviewed SHA
in the proof receipt. A builder never self-approves. Model strength does not
waive production-path proof, and a review that did not inspect the cumulative
integration diff cannot authorize the next dependent batch.

## Pull-request and multi-session topology

The maintainer is not an integration operator. Run the complete approved
program through **one externally visible draft PR against `main`**. Internal
agent branches, worktrees, commits, and review passes are implementation
machinery; do not open separate milestone or stacked PRs that require the
maintainer to order, rebase, or merge them.

The single PR is not permission for one unreviewable commit. Preserve small,
ordered, reversible commits and internal milestone gates so every root batch can
be reviewed, reverted, and proven independently before the next dependent batch
enters the integration branch.

Persist continuation state on that branch and in the PR proof index: original
base SHA, latest main-sync merge, integration head, accepted batch IDs and
commit ranges, next-ready dependencies, migration reservations, proof receipts,
current ledger tally, conflict resolutions, and blocked-with-evidence rows. A
new session resumes from this state, never from chat memory. A genuine policy or
external-authority blocker keeps only its adapter/cutover fail-closed; the
coordinator continues every independent ready batch and never asks the
maintainer to schedule agents, rebase, resolve routine conflicts, review, or
merge.

### Internal parallel branches

F0/N0, S0, module-owned D0 corrections, W0, and disjoint M0 sub-batches may run
in parallel on isolated internal branches/worktrees. They create no external PR.
Parallel work is allowed only with disjoint ownership. One coordinator owns the
integration branch, migration number sequence, OpenAPI roots, shared registries,
ledger, and other conflict-heavy files. Agents return focused commits and proof;
the coordinator reviews and integrates them in dependency order.

Before dependent work, a rejected batch is reverted as a unit. After dependent
work, revert its dependency closure. Applied database changes use forward
repair, not destructive rollback. Cutovers stay default-off per tenant/module,
retain kill switches and reconciliation/backfill ledgers, and keep compatible
legacy reads through the acceptance window.

### Internal dependency stack

The task-kernel dependency remains:

```text
K0 prerequisites accepted into the integration branch
  -> K1 schema, owner/duty/clock, shadow writes
      -> K2 Today cutover and escalation
          -> K3 hierarchy and sign-off activation
```

Agents may prepare dependent internal branches, but the coordinator integrates
K1, K2, and K3 only after the previous internal gate is green. Review each
batch's incremental commit range and the cumulative integration-branch diff
against fresh `origin/main`. Merge fresh `origin/main` into the one integration
branch at controlled checkpoints so reviewed, reversible commits are not
rewritten. Resolve conflicts centrally, rerun affected CI and prior contract
tests, and re-review the changed cumulative result. Channel work remains behind
the escalation schema and policy gates.

### Coordinator obligations

The coordinator maintains the dependency map, assigns non-overlapping file and
migration ownership, collects reviewed commits into the single PR branch,
reconciles stable IDs, runs combined impact review, verifies every agent claim,
keeps the draft PR description/proof current, and lands the final candidate
through the repository-owned `make land-integration-pr PR=<number>` exact-head
gate introduced by F0. That gate verifies Mesha/VGoats authority, one open
same-repo program PR with base `main`, expected base/head SHAs, local HEAD equal
to the remote PR head, `origin/main` as an ancestor, exact-head CI/proof/review
receipts, and a clean integration branch. It refetches main, then uses the
existing guarded Mesha fast-forward push so the tested PR head itself becomes
`main`; a race fails rather than creating a server-side merge commit. Finally it
requires `origin/main` to equal the tested head and the PR to report merged.
Until the gate and adversarial tests pass, the program PR cannot land. A
worker branch, chat, or internal milestone is not the common integration point;
the one draft PR and its current-SHA cumulative proof packet are.

## Entry and exit gates

The prevention packet, independent-review receipt, and program-PR landing rules
are not machine-enforced on the documentation-foundation SHA. F0 is the first
implementation batch and must make them executable before any remediation item
can be marked closed. Ordinary `make ci-local` also omits PostgreSQL/E2E by
default; database, migration, device, browser, deploy, and live-state proof is a
separate mandatory certification gate whenever the prevention matrix selects
it. A skipped lane is absence of proof, not success.

Before implementation:

- fetch fresh `origin/main` and re-adjudicate selected findings and migration
  tail;
- choose the root batch and internal dependency;
- fill the prevention matrix;
- record unresolved product decisions as blockers;
- prove file/migration ownership is disjoint from parallel work;
- write and run the failing regression.

Before review:

- implement every applicable prevention-matrix row;
- run the narrow red/green proof, guard self-tests, and affected local-CI jobs;
- update contracts, clients, seeds, runbooks, skills, anti-pattern docs, and
  operational recovery in the same change;
- produce a current-SHA proof packet with limitations.

Before closure or cutover:

- obtain independent counter-review against fresh `origin/main`;
- run full affected `make ci-local` on the exact candidate SHA;
- re-review the cumulative integration diff after every main-sync merge,
  conflict-resolution commit, or internal dependency reorder;
- verify deployment revision, schema, repair/reconciliation, and observability
  where the finding concerns live state;
- keep the finding open if any applicable prevention or recovery leg is absent.

## Exception rule

A prevention item may move out of the batch only through a recorded maintainer
exception naming the missing control, reason, owner, expiry, risk, and blocking
follow-up ID. An exception cannot waive a security, medical, tenant-isolation,
data-loss, migration-safety, or logout-isolation invariant. It also cannot mark
the source finding closed; the row remains `closure-pending` until the control
lands and is proven.
