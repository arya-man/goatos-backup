# Additive publish — editing one vaccine must not reschedule the rest

## The rule this document exists to protect

A vaccination plan version holds **every** vaccine. Publishing a new version is
routine: it happens when a director adds a vaccine, corrects one interval, or
switches a vaccine off. The version number always moves. **What the animals owe
must not.**

Stated as the maintainer stated it:

> If there are already 5 vaccine rules in the previous version and in the new
> version I add a 6th without touching the past 5, all 5 stay the same — no
> change, no rescheduling. Only the protocol version upgrades. If I change the
> revaccination interval (or any other rule) on 1 of the 5, only that 1 changes
> and the other 4 continue as they are under the updated version.

So, for a publish from `V(n)` to `V(n+1)`:

| Rule in V(n+1) | Obligations |
|---|---|
| **Added** (no counterpart in V(n)) | generated fresh |
| **Unchanged** (same identity, same content) | **carried over** — same `obligation_id`, same `due_at`, same status, rebound to V(n+1) |
| **Edited** (same identity, different content) | old work canceled `protocol_version_replaced`, regenerated under the new content |
| **Removed** (no counterpart in V(n+1)) | old work canceled `protocol_version_replaced`, not regenerated |

"Carried over" is literal: the obligation row is **not** deleted and re-inserted.
Its `obligation_id` survives, so anything pointing at it — a task, a batch, a
verification item, an operator's phone showing it in a list — keeps pointing at
the same thing across the publish.

## Why this needed building

Before this change, [`supersedeRetiredPlanWork`](../../backend/internal/vaccination/app/generation.go)
canceled **every** open obligation belonging to a version that was no longer
effective, and generation re-minted them under the new version. That is
cancel-and-re-mint for the whole plan, on every publish. Adding a 6th vaccine
churned all five untouched ones: new obligation ids, new rows, cancel events in
the audit log for work nobody had changed.

Cause-anchored obligation identity (`vaccine|administered-at|dose`) stopped the
re-mint producing **duplicates**. It did not stop the re-mint.

## How a rule is identified across versions

`protocol_rules.rule_id` is a fresh UUID per version — publishing rewrites every
rule row — so it cannot answer "is this the same rule as last version". Two
derived values do:

- **`identity_key`** — *which rule is this, in business terms*:
  `vaccine_code | dose_code | sequence`, lowercased and trimmed. Stable across
  versions. Two rules with the same identity key are the same rule at different
  points in time.
- **`content_fingerprint`** — *what does this rule say*: a SHA-256 over every
  field that can change what an animal owes or when — `trigger_type`,
  `offset_days`, `due_window_days`, `min_gap_days`, `repeat`,
  `repeat_until_after_age`, `catch_up`, `eligibility_json`, `proof_policy`,
  `withdrawal_days`, `sop_version_id`.

Both are computed **at publish**, in Go, and stored in `protocol_rule_lineage`,
keyed by `(tenant_id, rule_id)`. Carry-over happens only when `identity_key`
matches **and** `content_fingerprint` matches.

They live in their own table rather than as columns on `protocol_rules`, and the
separation is the point. `protocol_rules` holds **sourced** configuration — what
the spreadsheets say a plan is, which the seed pipeline validates row by row.
Lineage is **derived**: the publisher computes it from the rule it just wrote, no
fixture supplies it, and no validator could check it against a sheet. Mixing the
two would tie every future change here to a seed-source contract it has nothing
to do with, and the seed fixture guard says so — it refused the column form of
this change, correctly.

### Fields deliberately excluded from the fingerprint

`sort_order` and `created_at` change nothing about what is owed. Reordering the
vaccine list in the editor must not reschedule anything.

## Obligations follow the rule's identity, not the version

The mistake this design corrects is treating a version pointer as identity.
Obligations were keyed by `(goat, protocol_version_id, rule_id, due_at,
sequence)`. Publishing rewrites every rule row, so the same ET+TT rule is a
different `rule_id` in every version, and the animal's existing work looked —
to the database — like it belonged to something that no longer exists.
Cancel-and-re-mint follows directly from that.

An obligation therefore carries `rule_identity_key`: the same
`vaccine|dose|sequence` value the publisher writes to lineage. A version becomes
what it always should have been — history — and "is this the animal's existing
ET+TT work?" stops depending on which version is current.

**At most one open obligation per `(target, identity, sequence)`**, enforced by
`obligation_open_rule_identity_unique_idx` rather than by generation remembering
to check.

### Reconcile before insert

A rule's content can be unchanged while the date an animal owes it moves,
because the due date is computed from the **animal's** history too — a dose
recorded late, a correction applied afterwards. Keeping the old row untouched
leaves a stale date; inserting a new one books the same dose twice. Neither is
acceptable, and double-booking is the worse of the two: churn loses continuity,
this books medical work twice.

So before generation inserts, it looks for the animal's open work under that
identity — ignoring version, rule UUID and due date, none of which say whether
this is the same piece of work — and **moves what it finds**: same
`obligation_id`, new due date, new version and rule pointers, and the
idempotency key generation now owns. Only when nothing is found does it insert.

In-flight work is claimed but not moved. An operator part-way through a drive
keeps the dose they are administering; generation still does not write a second
row beside it.

## Order of operations at generation

Carry-over runs **before** supersede, and supersede before generation:

1. **Carry over.** For each retired version, pair its rules to the effective
   version's rules by `identity_key`; where `content_fingerprint` also matches,
   rebind the goat's open obligations to the new `protocol_version_id` /
   `rule_id`. `due_at`, `status`, and the `repeat_cycle_*` columns are untouched.
2. **Supersede.** Whatever open work still points at a retired version is
   genuinely stale — its rule was edited or removed — and is canceled
   `protocol_version_replaced`.
3. **Generate.** Runs under the new version. Carried-over obligations are already
   open at their original `due_at`, so cause-anchored reconciliation recognises
   them and generates nothing for those rules.

The order is not cosmetic. Rebinding after generation would collide with
`obligation_instances_dup_guard`
(`UNIQUE NULLS NOT DISTINCT (tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)`)
because generation would already have minted a row at that key.

## Guardrails

These are the invariants a reviewer or a future change must not break.

1. **A publish that changes no rule content changes no obligation.** Republishing
   an identical plan is a no-op for every animal: zero cancels, zero inserts,
   only `protocol_version_id` moves.
2. **Blast radius equals the edit.** Editing rule R cancels/regenerates work for
   R and for nothing else. Adding rule R generates work for R and for nothing
   else.
3. **Carry-over preserves the obligation id.** It is an `UPDATE`, never a
   delete-and-insert. A test asserts the id survives, because everything
   downstream keys off it.
4. **Carry-over never moves a due date.** `due_at` is not in the update. If a due
   date should move, the rule content changed, and the edited path — cancel and
   regenerate — is the correct one.
5. **Unknown lineage fails safe.** A rule with no `protocol_rule_lineage` row
   does not carry over. It takes the old cancel-and-re-mint path. Failing safe
   means falling back to the previous behaviour, never to a silent carry-over of
   a rule whose content nothing has verified.

### Open E2E item from procurement-purpose clarification

Maintainer screenshot, 2026-08-25: while procuring animals, breeding and
fattening vaccine requirements differ. That is not closed by the basic
add-vaccine or edit-existing-vaccine publish tests.

Before this work is called complete, a clean staging-clone E2E must prove how
additive publish behaves for procurement animals whose purpose is `breeding`,
`fattening`, or `non_breeding`. At minimum, the proof must show that trusted
procurement vaccination evidence and future generated work are scoped to the
right procurement purpose, and that adding or editing one vaccine does not
silently carry over/suppress obligations for a purpose whose vaccine rule
actually changed.

## Deploying this — the backfill is not optional

Only rules published *after* the lineage table shipped get a row from the
publisher. Every rule already live has none, so without a backfill the **first**
publish — the one where a director adds a vaccine to a live plan — cannot prove
the other vaccines are unchanged and churns all of them. Measured on a restored
staging clone, same publish either way:

| deploy-day publish | obligations cancelled | carried over |
|---|---|---|
| without the backfill | **7,289** | 0 |
| with the backfill | **0** | 7,289 |

So the order is:

```bash
# 1. migrate (creates the lineage table and the obligation identity column)
# 2. label the rules that already exist
DATABASE_URL=... go run ./backend/cmd/backfill-protocol-rule-lineage -tenant-id <tenant>          # dry run
DATABASE_URL=... go run ./backend/cmd/backfill-protocol-rule-lineage -tenant-id <tenant> -apply
# 3. only then publish
```

### Duplicates already in the data are a rollout gate

An animal that already holds **more than one open obligation for a single dose** cannot be brought
under the invariant: labelling one asserts something the data contradicts, and choosing which to
keep cancels somebody's scheduled vaccination. On a staging clone that was **561 groups**,
pre-existing and unrelated to this work.

Generation refuses to guess. It fails **that animal**, reports `ambiguous_open_work` separately
from `failed_goats`, prints the query to list the groups, and continues with the rest of the run.
No re-run clears it — the data has to change.

Resolving them is a deliberate, human-invoked step, never part of a deploy script:

```bash
# report only (default): shows what would be closed
go run ./backend/cmd/backfill-protocol-rule-lineage -tenant-id <tenant> -resolve-duplicates keep-earliest
# actually close them: BOTH flags required
go run ./backend/cmd/backfill-protocol-rule-lineage -tenant-id <tenant> -resolve-duplicates keep-earliest -apply
```

`keep-earliest` keeps the date the animal was already told it owed and **cancels** the later rows —
cancel, never delete, so the audit trail keeps what was there and a wrong call is readable back. It
skips `in_progress` work outright: an operator is administering that dose right now. It runs only
when named explicitly *and* combined with `-apply`; there is no default that closes clinical work.

**Rollout order, then, is: migrate → backfill → resolve duplicates → verify
`ambiguous_open_work=0` → publish.** Measured end to end on a staging clone: after cleanup,
`failed_goats=0`, `ambiguous_open_work=0`, `reconciled=6319`, zero unlabelled open rows, and
publishing a new vaccine left 6,792 of 6,792 obligations with their original id and nothing
cancelled.

### When a date cannot move

`obligation_instances_dup_guard` spans every status, terminal rows included, so the key an
obligation would move to can already be held by its own canceled or completed twin. The obligation
is still **claimed** — the animal keeps exactly one open row and nothing is duplicated — but its
date does not move that pass. That is reported as `date_blocked` rather than passed over in
silence: a count that stays high across runs is telling you something.

The backfill also stamps the identity onto existing OPEN obligations, without
which reconciliation cannot see work written before the column existed — which is
the double-booking case again. Animals that already hold two open obligations for
one dose are reported and left unlabelled: choosing which of them to cancel is a
clinical decision, not a migration's. On a staging clone that was 561 groups,
pre-existing and unrelated to this change; they keep behaving exactly as they do
today until somebody closes one of each pair.

The backfill computes the fingerprint with the **same domain helpers the
publisher uses**, from the stored rule row. Deriving it in SQL would risk a
fingerprint differing from the publisher's by a byte, which is worse than no
lineage at all: every rule would read as changed, forever, and never carry over.
It is idempotent, and a rule whose content cannot be fingerprinted is skipped
rather than given a value nothing would reproduce.
6. **Terminal work is never touched.** `completed`, `canceled` and `missed` are
   history and are immutable. Both sweeps act only on open work.

7. **In-flight work is rebound but never cancelled.** The two sweeps take
   deliberately different status sets, and the difference is not an oversight:

   | | statuses acted on |
   |---|---|
   | carry-over | `scheduled`, `due`, `deferred`, **`in_progress`** |
   | supersede | `scheduled`, `due`, `deferred` |

   Carry-over includes `in_progress` because rebinding is non-destructive — an
   operator part-way through a drive keeps the same obligation, and it stays
   attached to the version that is now live. Supersede excludes it because
   cancelling work somebody is physically doing, mid-drive, is worse than the
   staleness it would fix. That exclusion predates this change and is unchanged
   by it.

   The consequence, stated plainly so nobody discovers it as a surprise: if a
   rule is **edited** while an animal has `in_progress` work on it, that animal
   keeps the old rule until the in-flight work closes. The edit reaches every
   other animal immediately, and reaches this one on the next generation pass
   after its current dose is recorded. A future change that wants the edit to
   land sooner has to decide what happens to the operator's half-finished drive
   — it is a product decision, not a bug to quietly patch.

## What "unchanged" does not cover

A rule whose content is identical can still legitimately produce different work
when the *animal* changed — a goat that aged into a new stage, moved shed, or
entered a clinical hold. Carry-over does not override eligibility: generation
still evaluates the animal against the rule afterwards. The guarantee is about
the **plan** not churning, not about freezing an animal's life.
