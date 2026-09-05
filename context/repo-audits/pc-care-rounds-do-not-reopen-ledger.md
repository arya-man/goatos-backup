# PC Care Rounds — Do Not Reopen

**Scope:** the round-grain PC Care plan (`pc_care_rounds`, `pc_care_removal_pen_proofs`) added
2026-09-05.
**Status:** CLOSED. Each entry below was raised in review, checked against a live database, and
either fixed or closed as working-as-designed with a test that now guards it.

---

## A-1 — The removal pair CHECK allows PARTIAL capture. Working as designed.

**Raised:** PR review, 2026-09-05, as P1 — *"`pc_care_removal_pen_proofs_pair` requires
`feed_proof_ref` and `water_proof_ref` to be null together or non-empty together, but
`RegisterRemovalPenProof` updates only one column per call, so the first video for every pen
violates the check and the round-grain removal flow is unusable."*

**Verdict:** does not reproduce. The reasoning is sound about the SQL as read aloud, and wrong
about what Postgres does with it.

**Why it passes.** A CHECK constraint is satisfied unless it evaluates to **FALSE**; NULL
counts as satisfied. With `feed_proof_ref` set and `water_proof_ref` still NULL:

```
(feed IS NULL AND water IS NULL)              -> FALSE
(btrim(feed) <> '' AND btrim(water) <> '')    -> TRUE AND NULL -> NULL
FALSE OR NULL                                 -> NULL          -> satisfied
```

**Proven three ways on a live database (2026-09-05):**

1. the single-column `UPDATE` succeeds;
2. the same call through the real route answers `200 {"status":"recorded"}` and the row reads
   `feed = aa000000-…f1, water = (null)`;
3. the constraint is **not vacuous** — writing empty strings still fails with
   `violates check constraint "pc_care_removal_pen_proofs_pair"`.

**And the design the review asked for is what ships.** It proposed "allow partial capture, then
submit-time validation keeps enforcing both videos". That is exactly the split already in place:

```
PUT feed_video  -> 200 recorded            (water still null)
POST submit     -> 422 removal_proof_incomplete
                   "every pen needs both its feed and its water video"
PUT water_video -> 200 recorded
POST submit     -> 200 pending_verification
```

The rule lives at submit because that is where it belongs: a pen holding one video is
mid-capture — the operator shoots the feed clip, walks the pen, shoots the water clip — while a
pen SUBMITTED with one video is a lie about the evening's work.

**What guards it now:** `TestRemovalPenPartialCaptureIsAllowedAndSubmitStillDemandsBoth`
(`backend/internal/pccare/adapters/postgres/removal_pen_partial_capture_test.go`) pins all four
behaviours against real Postgres. Mutation-tested by tightening the constraint to the
NULL-safe both-or-neither shape the review believed was there: the test then fails on the first
video with precisely the error the review predicted, which is the proof that the shipped
constraint is the thing keeping the flow usable. The constraint itself and
`RegisterRemovalPenProof` both carry a comment pointing here.

**If you are about to raise this again,** run that test first. A real defect here would look
like: the submit gate accepting a pen with one video, or an empty/blank ref being stored.

---

## A-2 — A pen's removal video was filed as an ANIMAL's scan proof. Real, fixed.

**Raised:** the same review, as P2 — the recovery path
`PcCareTaskViewModel.reconcileSlotRegistrations()` only retried task-proof rows whose
`fieldKey` was exactly one of `pcCareTaskProofSlotKeys`, so a pen-scoped removal key would not
be reattached after an interrupted upload.

**Verdict:** real, and worse than reported. A round removal slot is keyed
`<gated task id>::<slot>`, which **contains a colon**, so the ANIMAL branch
(`fieldKey.contains(':')`) claimed it first and called:

```
registerSlotProof(task, tag = "<gated task id>", slot = ":feed_video")
```

filing a pen's removal video as some animal's scan proof, with a task id for a tag and a
malformed slot. Not a missed retry — a wrong write.

**Fix:** the removal shape is matched FIRST, on the unambiguous `::` split, and routed to
`registerTaskProof` with its pen. Reproduced red before the fix (the failure printed that exact
bogus call), then green, then mutation-tested by disabling the new branch.

**Guarded by:** `PcCareRemovalPenSlotsTest > an interrupted pen removal video is reattached to
its pen, never to an animal`.

**Lesson worth carrying:** a composite key whose separator is a superset of an existing key's
separator will be claimed by the older parser. `::` and `:` are not distinct to
`contains(':')` — order the branches, or pick a separator that cannot collide.
