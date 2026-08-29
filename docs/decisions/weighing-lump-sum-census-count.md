# Weighing lump-sum head count is a herd-register snapshot (maintainer decision 2026-08-24)

## The decision

Operators kept typing wrong animal counts on lump-sum weighing submissions, so
the maintainer ruled:

1. **The operator no longer enters the head count.** A lump-sum submit carries
   only the shed/pen's total weight and the video(s).
2. **The backend snapshots the count at submit.** Inside the submit
   transaction, weighing reads the bucket's live resident count from the herd
   register (`goats` + `goat_shed_partitions`, `lifecycle_status='alive' AND
   exited_at IS NULL`, pen-matched under the counts module's label
   normalization) and stores it in `weighing_shed_observations.animal_count`,
   deriving `average_weight_kg` from it.
3. **The snapshot is FROZEN FOREVER.** No later herd move recomputes it, an
   idempotent replay returns the original value, and nobody — the verifier
   included — can edit it. The verifier's weight correction is now WEIGHT ONLY
   on both grains; a correction naming a count is refused
   (`animal_count_not_applicable`), and the recomputed average uses the frozen
   count.
4. **A register-empty shed refuses the submit** (422 `shed_count_unavailable`,
   `ports.ErrShedCountUnavailable`): inventing a head count would store an
   average nobody measured. The remedy is fixing the herd register, then
   resubmitting.
5. **Historical rows keep their operator-entered counts.** No backfill —
   today's census is not the census at their submit time.

## What this supersedes

- The operator-entered-count half of the 2026-08-03 "WEIGHING IS SCAN-AND-SUBMIT"
  lump-sum contract ("total weight, animal count, video(s)"). Everything else
  in that lock stands: individual capture stays free-flow, no shed↔RFID
  validation, no roster, no progress denominator.
- The head-count-editing half of the 2026-08-17 verifier weight-correction
  decision (migration 000172's `operator_animal_count` rationale). The columns
  stay for history; new corrections never write a count.
- The weighing-isolation rule gains its SECOND file-scoped exemption:
  `backend/internal/weighing/adapters/postgres/lump_sum_census.go`, allowlisted
  by name in `check-weighing-free-flow-guard.mjs` (`HERD_JOIN_EXEMPT_FILES`),
  reading only `goats` + `goat_shed_partitions` for one COUNT. This is
  knowingly a write-path read — recorded explicitly in the guard header rather
  than relying on its cross-file blind spot.

## Client contract

- `RecordWeighingShedObservationRequest.animal_count` / `average_weight_kg` are
  deprecated and IGNORED (kept on the wire so installed APKs keep working; they
  still feed the idempotency fingerprint so old replays stay byte-identical).
- `WeighingWeightCorrectionRequest.animal_count`: any non-zero value is refused
  on both grains.
- The weighing verification measurement spec no longer declares
  `CountLabel`/`CountRefTypes`; both clients render the count input only when
  `count_label` arrives, so the field disappears from the phone and the
  admin-web drawer without a client release, and
  `measurement_count_not_supported` refuses a count from a stale client.
- Android's lump-sum form drops the count field and the client-side average
  preview; an old queued outbox row that recorded a count still decodes and
  replays.

## Proof

- `backend/internal/weighing/adapters/postgres/lump_sum_census_integration_test.go`
  (real-Postgres, production write path): snapshot ignores the typed count,
  DB round-trip, frozen across herd moves and idempotent replays, pen-scoped
  bucket counts only its pen ("Part 2" == "2"), empty register refuses with
  nothing written, weight correction recomputes the average on the frozen count.
- `backend/internal/weighing/domain/weight_correction_test.go`,
  `backend/internal/weighing/app/weight_correction_test.go`,
  `backend/internal/weighing/app/service_test.go` — count refused on both
  grains / never validated on submit.
- Guard self-test fixtures in `check-weighing-free-flow-guard.mjs` — the census
  read passes ONLY in its exempt file.

## Follow-up (maintainer decision 2026-08-25): lump-sum sheds join the gain charts

The Breed-wise daily gain chart (and the gain-by-breed/sex/stage buckets) used
to count same-animal pairs only, so a shed weighed lump-sum every week never
appeared in it. The maintainer directed: include lump-sum — take the shed's
average, and keep every animal of that shed in that range.

- A lump-sum shed's gain is its own average-weight change between its first and
  latest weigh in the selected window (`lump_span.g_per_day`).
- When the shed's live cohort is HOMOGENEOUS for the reported dimension, ALL of
  its animals (the frozen census count of the latest weigh) land in the ONE
  band that average falls into, and join the dimension's gain bucket as a
  weighted mean alongside the same-animal pairs.
- A MIXED shed still joins no breed/sex row — the standing rule stands: one
  shed average is never split across a mix.
- A shed weighed once contributes no gain (a single average is a level).

Pinned by `lump_sum_gain_bands_integration_test.go`
(`TestBreedGainBandsIncludeHomogeneousLumpSumShedsAtShedAverage`,
`TestSingleLumpSumWeighContributesNoGain`). The coverage note on the Weights
page (`note.demographics.coverage`) says the new rule in farm words.
