# Randomized verification sampling

Maintainer decision, 2026-08-26.

## The rule

The CEO sets, per verification category, the **percentage of that category's proof videos the
verifier actually has to watch**. The rest are settled by the policy without her. Her day is
complete when she has cleared **her share**: at 40% on feed packing, reviewing those 40% is
**100% of her work**.

The setting lives in one place — the **Randomization** section on `/verify`, gated on
`permissions.VerificationSampling`.

## The four decisions behind it

**1. An unsampled video is AUTO-ACCEPTED, never left hanging.** This was the decision that had to
be made first, because verifier approval is not merely review for some modules — it is the gate
that *completes* the work. A feed pen-session stays `pending_verification` until an approve lands,
and a weighing bucket cannot close while any verification is pending (ledger D-5, and that gate is
unconditional). Simply hiding 60% of the videos would have stalled those workflows forever. So the
unsampled ones are approved with `verification_items.auto_resolution = 'not_sampled'` and **no
verifier attached**, emitting the ordinary `verification.verdict.approved` event — every producer's
consumer applies exactly as it does for a human approve. There is no second apply path.

Sampling decides what gets *watched*. A video nobody watched can never be evidence that the work
was wrong, so a waived item is always an approval and never a rejection.

**2. The percentage is per CATEGORY, tenant-wide.** One row per registered category (feed
distribution, feed packing, feed transport, vaccination, weighing, birth, death, shifting, milk,
PC care, health), composed from the verification type registry — so a producer that registers a new
category gets a Randomization row with no code change. Which verifier covers which module is
unchanged and read-only for now; the maintainer expects to assign modules per person later, and the
percentage is keyed by category so that assignment can be layered on without moving this setting.

**3. It takes effect the SAME DAY, and a past day keeps the percentage it ran at.** The policy is
effective-dated: a change writes a row at today's Asia/Kolkata business date, and any day resolves
to the newest row on or before it. Raising 40% → 60% at 15:00 pulls more of *today's*
already-captured videos into her queue immediately.

That is safe only because the draw is **deterministic and monotonic**. `sampling_bucket` is a
GENERATED column — a stable 0..99 draw from the item's own id, computed by the database for every
row that has ever existed — and an item is in sample when `bucket < percent`. Raising the
percentage therefore only ever *adds*; it can never retract a video she is already holding or has
already reviewed.

**4. A category whose approve must CARRY a number cannot be sampled.** Feed packing and feed
wastage are locked at 100%, and the write is refused with `sampling_not_available`. There the
operator submits a video **and no number**: the verifier reads the packed quantities / the leftover
weight off the clip, and the producer's own applier refuses an approve carrying none. Waiving one
of those videos would either complete a pen-day with no quantity recorded at all or strand the item
mid-apply. This is derived from `MeasurementCorrection.RequiredForApprove` — the registry entry the
producer already wrote — never a hardcoded category list, so a future producer that declares a
required measurement is locked automatically. Those rows still appear in the panel, at 100% with a
backend-owned reason, so the CEO sees *why* rather than wondering where the module went.

## The share is a floor, not a ceiling

Maintainer decision 2026-08-27, raised in review of this feature.

A verifier can reach the verdict route for an item the policy did **not** draw — a drawer still open
after the CEO *lowered* the share, an older push, a direct API call. That verdict is **accepted**,
and recorded as what it is: a human verdict, `verified_by` set, `auto_resolution` left NULL.

The share is a floor on the review she is **required** to do, never a ceiling on the review she is
**permitted** to do. Failing the write closed was considered and rejected, because it would:

- **make bad work unreportable** — she watches an undrawn video, sees the work was done wrong, and
  the rejection is refused, so the work proceeds to `completed`; and
- **discard a review she already performed** — the only way an item leaves her queue mid-review is a
  LOWERED share, since the draw is monotonic, so this lands on someone who has just watched a full
  video.

Allowing it mislabels nothing. `auto_resolution = 'not_sampled'` is the contract for a video
**nobody** reviewed; this one was reviewed. `Reviewed` and `Selected` on the panel are both
share-scoped, so an extra review cannot push her day past 100% or invent work she was never given,
and `SettleUnsampledItems` skips any item a verifier already decided — the same precedence, on the
write side.

Sampling is also not an authorization boundary. The category/duty check on that route is; she
already holds verdict authority for the category, so acting outside the share grants her nothing she
is not entitled to do. What genuinely takes an item out of her reach is it leaving `pending` — the
closeout settling it, or a producer withdrawing it — which `RecordVerdict` enforces, exactly as
withdrawal does for superseded work.

Pinned by `TestAVerdictOnAnUndrawnItemIsHersToCast`, and stated in the verdict handler itself so the
next reader finds it at the line they would ask the question. It was reported as a P1 in review and
closed as working-as-decided:
`context/repo-audits/verification-randomization-do-not-reopen-ledger.md` -> B-1 carries the full
reasoning, the shape of a real defect here, and the two stricter variants that were costed and not
taken.

## Why the closeout waits for the day to end

Decisions 1 and 3 pull against each other: a video waived the moment it arrived could not be
recruited back by a raise that afternoon. So waiving is deferred to
`kernelstages.VerificationSamplingCloseoutStage`, which settles only business days that have
**closed** — and with them, the percentage that can no longer change. Consequence to know: an
unsampled feed pen-session completes early the next morning rather than at capture time.

## What it does to every surface that counts a proof video

Verification is a spine, not a screen. Sampling splits it into two populations — **what a person
still owes** and **what a person actually did** — and every number that reports either one had to be
told which. This table is the audit; each row is asserted in the E2E story.

| Surface | Behaviour | Why |
| --- | --- | --- |
| Verifier's queue | Only the drawn videos | The point of the feature. |
| Her status badge, park and shed filters | Narrowed with the queue | A badge that counts hidden work, or a shed filter that opens an empty board, is a filter lying about where the work is. |
| Leadership's queue (`verification.oversee`) | **Unchanged — every video** | The principal who sets the share must be able to audit what it waived. |
| KPI: videos waiting, age buckets, backlog by module | Drawn items only | `EstDaysToClearBacklog` divides this by review throughput; counting videos no human will review would divide one population by another's rate. |
| KPI: verdicts/day, median review latency, reject rate, "reviewed" trend | Human verdicts only | A settled item carries the closeout's `verified_at`. Counting it would report a review latency for a review that never happened, and a 40% share would **collapse the reject rate** by padding its denominator with approvals nobody decided. |
| Per-verifier activity table | Unchanged | It has always keyed on `verified_by`, which a settled item does not carry. |
| Vaccination live tracker ("awaiting review" / "Verified today") | Same two rules, from the **same shared predicate** | Cross-surface count parity: "videos awaiting review" must be one number everywhere. `verification/samplingsql` owns the definition so this card and the KPI strip cannot drift apart. |
| Feed / weighing / shifting completion | Completes normally, via the ordinary approved event | The whole reason unsampled items are settled rather than hidden. |
| Verifier push notification | Suppressed for an undrawn video; **leadership's half still fires** | Telling her to review a video her queue does not contain sends her to an empty screen. The park head is still told proof arrived from his park. |
| People directory proof stats | New `proof_not_reviewed`; `proof_approved` counts **human** approvals only | Otherwise an operator reads "50 uploaded, 50 approved" when 30 were never watched — and his rejection rate is diluted by proofs nobody judged. |
| Video log | Unchanged | It logs what *arrived*, and every video still arrives. |
| Stale-push cancellation | Unchanged, and now also cancels for settled items | A pending push whose item is settled is correctly stale. |

The one thing deliberately **not** surfaced is a stalled closeout. It shows on the Randomization
panel instead: a past day with `captured > drawn` and `settled` still 0 is exactly that condition.

## Capability

`permissions.VerificationSampling` is **CEO-only**, and narrower than every other capability on
`/verify`. `pc_director` holds `VerificationOversee` and does **not** hold this. Oversight
*watches* the verification workload; randomization *decides how much of it a human is required to
watch*, and a director setting that for work his own department produces is the same separation of
duty that keeps `VerificationVerdict` off every leadership role. It carries no verdict authority:
the CEO still cannot approve or reject an item.

The verifier's queue is narrowed by the policy (`ports.ListQueueParams.SamplingApplied`, keyed on
the absence of `VerificationOversee`); leadership's is **not** — the principal who sets the
percentage must be able to audit what it waived, and a queue narrowed by his own setting could not
show him that.

## Where it lives

| Piece | Location |
| --- | --- |
| Rule + draw | `backend/internal/verification/domain/sampling.go` |
| Use-cases | `backend/internal/verification/app/sampling.go` |
| Storage, queue predicate, closeout claim | `backend/internal/verification/adapters/postgres/sampling.go` |
| Routes | `GET /verification/sampling`, `PUT /verification/sampling/{category}` |
| Closeout | `backend/internal/kernelstages/verification_sampling.go` (operational lane, 5 min) |
| Schema | `backend/migrations/postgres/000214_verification_sampling.sql` |
| Page contract control | `randomization` in `compileVerificationReviewControls` |
| Screen | `apps/admin-web/features/verification-review/randomization*.tsx` |
| Category catalog (shared by API + worker) | `backend/internal/verificationcatalog` |

`verificationcatalog` exists because the category set is now read by **two** processes. A worker
holding a hand-copied subset would not fail loudly — it would silently never settle the categories
it was missing, and those producers' records would wait forever on a review the policy already
decided nobody would do. `TestBootstrapDeclaresNoCategoryOfItsOwn` keeps that from being
reintroduced.

## End-to-end proof

`backend/tests/e2e/story_verification_randomization_test.go`
(`TestKernelStory_VerificationRandomization`), in the kernel-story report. It drives the production
path only — the shared category catalog, the real producer seam, the real queue reads, the real
closeout, the real outbox → domain-consumer chain — and asserts the table above, including that a
feed pen-session whose video was never drawn still reaches `completed` with no verifier in the loop.

It earned its keep immediately: it caught a defect no unit test could see. The policy write's
idempotency key is derived from (category, business day, share), so setting 40% → 80% → **back to
40%** on one day reuses the first key, and the implementation treated the reused key as a replay and
skipped the write — the panel reported 40% while the queue still ran at 80%. The reservation is now
the conflict detector only; the value-idempotent upsert always runs. Regression:
`TestReturningToAnEarlierShareTakesEffect`, failing-before/passing-after on a real database.

## Pinned by

- `TestSamplingBucketMatchesTheGeneratedColumn` — the Go draw equals the SQL generated column.
- `TestRaisingTheShareOnlyEverAddsVideos` — monotonicity, the property behind "same day".
- `TestHerShareFullyReviewedReadsOneHundredPercent` — the maintainer's own sentence.
- `TestACategoryWhoseApproveCarriesTheNumberCannotBeSampled` — the lock, mutation-tested.
- `TestTheShareTakesEffectTodayAndLeavesEarlierDaysAlone` — server-owned effective date,
  mutation-tested.
- `TestAShareOutsideTheRangeIsRefusedNotClamped` — present-but-invalid is a refusal.
- `TestTheClosetOutSettlesOnlyClosedDaysAndOnlyWaivableCategories` — both closeout narrowings.
- `TestVerifyPageRandomizationControlIsCapabilityGated` — CEO-only, mutation-tested against
  granting it to `pc_director`.
- `TestReturningToAnEarlierShareTakesEffect` — the replay-suppression defect, on a real database.
- `TestSettleUnsampledLeavesTodayAndTheLockedCategoriesAlone` — the closeout's two narrowings, on
  real rows.
- `TestKernelStory_VerificationRandomization` — the cross-surface table above, end to end.
- `TestAVerdictOnAnUndrawnItemIsHersToCast` — the floor-not-ceiling rule: an undrawn item stays
  decidable while pending, the verdict is recorded as human, the closeout leaves it alone, and the
  share's own arithmetic is untouched.
- `TestSamplingDayStatsOneToManyDoesNotFanOut`, `...PageBoundary`, `...DateShift`, `...ParkScope`,
  `...StatusMatrix` — adversarial coverage of the day aggregate.
