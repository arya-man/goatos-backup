# Pen Reconciliation — the register is truth, weighing is the detector

Maintainer decision, 2026-09-02.

## The problem

Animals stray between pens: a gate is opened, a pen is worked, and one animal ends up next
door. The farm's only systematic "which animals are physically in this pen" read is the
individual weighing session, where the operator scans every animal in the pen before
submitting.

## The rule

**The herd register (DB) is truth.** When an individual weighing bucket is submitted, every
scanned tag that resolves to a live animal whose registered pen (shed + partition) differs
from the pen it was weighed in raises **one card** in a new **Reconcile** tab of the Herd
Operations (Counts) phone module. The operator physically **returns the animal to its
registered pen**, records a **mandatory video**, and submits. The video goes to the tenant
**verifier** (approve = completed, reject = rework, exactly like other proof reviews).

Locked properties:

1. **No approver step.** Unlike shifting there is no Park Head gate anywhere on this surface —
   the verifier's evidence review is the only gate. The routes carry `CountsWrite` only.
2. **Nothing ever rewrites the register.** Neither the raise, the completion, nor the verdict
   touches `goats.shed_id` / `goat_shed_partitions`. The card is closed by moving the animal,
   not the row. If the register itself is wrong, that is a shifting raise, not a reconcile.
3. **Individual scans only.** A lump-sum bucket carries no tags and raises nothing
   (`weighing_category = 'individual_animal'` predicate); a tag that resolves to no live
   animal raises nothing — free-flow weighing capture stays untouched.
4. **Any mismatch cards.** Different shed, different pen, or different partition — always
   within one park, because animals never cross parks.
5. **One open card per animal** ("one piece one card"): enforced by the partial unique index
   `pen_reconciliation_cards_one_open_goat_uidx (tenant_id, goat_id) WHERE status <>
   'completed'`, which also makes the event-driven raise idempotent across duplicate bus
   deliveries. After a card completes, a later mismatched weighing may card the animal again.

## Mechanism

- **Trigger**: `counts/app.PenReconciliationRaiser` consumes the durable
  `weighing.shed_submission.completed` event and calls one set-based idempotent SQL statement
  (`RaisePenReconciliationCards`). Weighing emits its ordinary event and knows nothing about
  this consumer — weighing isolation is untouched; this is the recorded outward-only kernel
  consumption of weighing's durable events and source rows.
- **Pen comparison is canonical, not raw ids.** The bucket may point at a legacy ALIAS
  location row (`Castro 1` as its own `locations` row) while the register stores the physical
  shed + partition; the raise resolves the alias to its physical sibling and compares
  partitions with the scrubbed key (`Part 1` == `1`), the same idiom
  `weight_demographics.go` and `sex_scope.go` use. An undivided physical shed whose name ends
  in a number (`Ho Chi Minh 1`) is NEVER split — the trailing digits of a location name count
  as a partition only on the resolved-alias branch.
- **Card store**: `pen_reconciliation_cards` (migration `000243`), counts-owned. States:
  `open → pending_verification → completed`, reject → `rework`. The completion is
  idempotency-keyed and fingerprinted like every mutating write.
- **Verification**: category `pen_reconciliation` (module `counts`, ref_type
  `pen_reconciliation_card`, page "Reconcile" beside Birth/Death/Shifting in the verifier's
  Counts lens). The enqueue idempotency key carries the proof
  (`counts-pen-reconciliation-verification:<card>:<proof>`) so a rework re-shoot mints a
  replacement item while retries collapse. Verdicts apply through
  `eventwiring.RegisterVerificationAppliers`, the single shared registration.
- **Operator surface**: bottom-bar tab **Reconcile** (`/counts/reconcile`), composed by the
  backend nav registry; the queue is a keyset page of 20 with whole-filter status counts, and
  every pen label is backend-composed via `oploc`.

## Pinned by

- `TestPenReconciliationRaiseFindsOnlyTheMismatchedAnimal` (match/mismatch/unknown-tag, raise
  idempotency, backend-owned labels)
- `TestPenReconciliationPartitionSpellingsCompareScrubbed` (`Part 1` == `1`)
- `TestPenReconciliationAliasBucketResolvesToThePhysicalPen` (legacy alias rows never
  false-card a pen)
- `TestPenReconciliationCompletionAndVerdictCycle` (proof gate, replay, conflict, rework,
  re-shoot, approve, and the fresh-card-after-completion rule)
- `TestPenReconciliationCompleteEnqueuesVerificationWithProofKeyedIdempotency` and siblings
  (service seam, fail-closed enqueuer)
- `TestPenReconciliationVerdictRouting` (shifting verdicts sharing module=counts never
  cross-fire)
