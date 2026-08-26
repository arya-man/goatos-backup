# Verification Randomization Do-Not-Reopen Ledger

**Date:** 2026-08-27
**Purpose:** Stop settled randomization-sampling decisions being re-reported as defects
**Scope:** Randomized verification sampling (maintainer decision 2026-08-26) — the CEO-set share of
proof videos a verifier must watch. Canonical prose:
`docs/decisions/verification-randomization-sampling.md`.

Each entry: **what was reported** | **why it is not a defect** | **what a real defect here would
look like** | **guards**.

---

## B-1: "Unsampled items can still be verdicted via a direct or stale POST"

**Reported:** 2026-08-27, PR review of the sampling change. `RecordVerdict`'s HTTP handler resolves
the item, checks park scope and category duty, and calls through — it never re-checks that the item
is currently IN SAMPLE. So a verifier can approve or reject an undrawn item from a stale open
drawer, an older push/deep link, or a direct API call. Proposed fix: fail closed unless the item is
in-sample for its capture business day.

**Status: NOT A DEFECT. Working as decided (maintainer decision 2026-08-27).**

**The rule: the share is a FLOOR on the review a verifier is REQUIRED to do, never a ceiling on the
review she is PERMITTED to do.** Her verdict on an undrawn item is accepted and recorded as what it
is — a human verdict, `verified_by` set, `auto_resolution` left NULL.

**Why failing closed was rejected**, both costs load-bearing:

1. **It makes bad work unreportable.** She watches an undrawn video, sees the work was done wrong,
   and the rejection is refused — so the work proceeds to `completed`. Sampling decides what must be
   WATCHED; it must never decide what may be REPORTED.
2. **It discards a review already performed.** The draw is monotonic (`bucket < percent`), so
   RAISING a share can never drop an item she is holding. The only way an item leaves her queue
   mid-review is the CEO LOWERING it — which lands the refusal on someone who has just watched a
   full video.

**Why allowing it mislabels nothing** — the three things a reader worries about, each measured on a
real database by `TestAVerdictOnAnUndrawnItemIsHersToCast`:

- `auto_resolution = 'not_sampled'` is the contract for a video **nobody** reviewed. This one was
  reviewed, so stamping it would be a lie about who decided it.
- The panel's `Reviewed` and `Selected` are BOTH share-scoped, so an extra review cannot push her
  day past 100% or invent work she was never given.
- `SettleUnsampledItems` skips any item a verifier already decided — its "her verdict wins" branch
  encodes the same precedence on the write side, so there is no double-handling and no waiver
  stamped over a human decision.

**Two structural reasons it is not a hole:**

- **Sampling is not an authorization boundary.** `authorizeSingleCategory` is. She already holds
  verdict authority for that category, so acting outside the share grants her nothing she is not
  entitled to do. Contrast `verification.oversee`, which DOES gate data and is enforced on the read.
- **The repo's own pattern for "this is no longer your work" is to leave `pending`,** not a
  read-time predicate at verdict time — `WithdrawItemsBySource` sets `status='withdrawn'` for
  superseded work, and `RecordVerdict` refuses anything not `pending`. An undrawn item is still
  legitimately pending until the closeout settles it.

**What a REAL defect here would look like** (report these):

- An undrawn item that receives a verdict and ALSO ends up carrying `auto_resolution` — that would
  mean the closeout stamped a waiver over a human decision.
- `Reviewed` or `Selected` counting an undrawn review, i.e. progress able to exceed 100%, or a
  verifier's extra work inflating the share she was set.
- A verdict accepted for a category or park the caller holds no duty for — that IS an authorization
  hole and is a different check entirely.
- An undrawn item that can no longer be REJECTED, which would be this decision implemented
  backwards.

**Guards:**

- `backend/internal/verification/adapters/postgres/sampling_integration_test.go` →
  `TestAVerdictOnAnUndrawnItemIsHersToCast` — approve AND reject both accepted, both recorded as
  human verdicts, closeout leaves them alone, share arithmetic unmoved.
- The rule is stated in `backend/internal/verification/adapters/http/handler.go` at the verdict
  call itself, so the next reviewer finds the answer at the line that prompts the question.
- `docs/decisions/verification-randomization-sampling.md` → "The share is a floor, not a ceiling".

**If the stricter contract is ever wanted**, the two costed variants are recorded here so they are
not re-derived from scratch: (a) refuse APPROVE, allow REJECT — keeps the waiver contract clean and
never blocks reporting bad work, at the cost of the producer's record completing only at end-of-day
closeout rather than immediately; (b) fail closed entirely. Both are maintainer decisions, not
developer convenience.
