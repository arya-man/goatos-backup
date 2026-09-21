-- +goose Up
-- seed-fixture-guard:ignore: removes stale policy rows for a category that can no longer be
-- sampled; no schema change, no seed contract change, no app-visible projection table.
--
-- BLIND WEIGHING VERIFICATION LOCKS WEIGHING SAMPLING AT 100% (maintainer decision 2026-09-21).
--
-- Weighing's approve now REQUIRES the verifier's own weight reading
-- (verificationcatalog.Weighing -> MeasurementCorrection.RequiredForApprove). That flips weighing
-- to NON-WAIVABLE in domain.CategoryDefinition.SamplingWaivable, which is derived from exactly
-- that field, so three things changed at once and the third strands work:
--
--   1. The Randomization panel now shows weighing LOCKED at 100% and ignores any stored row.
--   2. SetSamplingPolicy now REFUSES a weighing percentage (sampling_not_available).
--   3. The closeout no longer settles unsampled weighing items -- it must not, because
--      auto-approving one would complete a weighing bucket with no verifier reading at all.
--
-- But the QUEUE predicate (verification/samplingsql.InSample) knows nothing about waivability: it
-- reads whatever row verification_sampling_policies holds for the category. So a tenant whose CEO
-- had set weighing to, say, 40% before today would keep a queue narrowed to 40% while the other
-- 60% of weighing items are no longer settled by anyone -- pending forever, each one holding its
-- weighing bucket open against the close gate, which is unconditional by design (ledger D-5).
--
-- The rows are DELETED rather than rewritten to 100. A stored row is the record of a choice the
-- CEO is no longer allowed to make, and leaving one at 100 would read to the next author as a
-- setting rather than a lock. With no row the predicate's own COALESCE resolves to 100, which is
-- the behaviour the queue had before sampling existed. The write path refuses new weighing rows,
-- so this cannot come back.
--
-- Past days are not being rewritten in any way that matters: sampling is effective-dated so a
-- closed day keeps the share it ran at, and every item captured on those days has already been
-- drawn, settled or decided. What this removes is the share applied to items captured from now on.
--
-- GENERAL RULE THIS RECORDS, because the next category to declare a required measurement will hit
-- the same edge: making a category non-waivable is not complete until its stored
-- verification_sampling_policies rows are removed in the same change. The write path stops new
-- ones; only a forward migration clears the ones already written.
-- The literal is the CATEGORY token, not the module name: weighing's category is 'weighing_proof'
-- (weighing/domain.VerificationCategoryWeighing), while 'weighing' is its navigation module key.
-- Writing the module name here would delete nothing and read as done -- which is exactly what the
-- first draft of this migration did. TestSamplingLockMigrationNamesTheRealWeighingCategory pins
-- this literal against the Go constant so the two cannot drift.
DELETE FROM public.verification_sampling_policies
WHERE category = 'weighing_proof';

-- ---------------------------------------------------------------------------------------------
-- THE STORED LABELS ALREADY IN THE QUEUE.
--
-- subject_label is COMPOSED AT ENQUEUE and STORED on the row. The code change stops NEW items
-- carrying the weight, but every item already waiting keeps the sentence it was enqueued with --
-- on the QA clone that is 75 pending weighing proofs reading
--
--     Yashoda 1 · Tag 901007000503975 · 22.3 kg
--
-- so the verifier would still be shown the operator's weight on every one of them, and the very
-- first item she opens after the deploy defeats the whole decision. Found by reading the real
-- rows; no test could have caught it, because the composer is already correct.
--
-- PENDING ONLY, deliberately. An approved or rejected item is a DECIDED record, and its label is
-- what the verifier was actually looking at when she decided; rewriting those would falsify the
-- trail. A rejected weigh comes back as a NEW item at a new evidence round, composed by the new
-- code, so nothing that returns to her queue keeps an old label.
--
-- The three shapes, all produced by the same composer:
--     'Pen · Tag 123 · 22.3 kg'        -> 'Pen · Tag 123'
--     'Tag 123 · 27.1 kg'              -> 'Tag 123'            (pen unresolved)
--     'Pen · 732.0 kg · 31 goats'      -> 'Pen · 31 goats'     (lump sum keeps the frozen count)
-- A label that was ONLY a weight has nothing left and falls back to the composer's own bare
-- forms, so no row is left blank.
-- Bounded lock: verification_items is a hot table and these two statements touch only the
-- PENDING weighing rows (75 on the QA clone), but a migration must never wait unboundedly on a
-- row lock a live verdict is holding. Failing fast and being re-run is the safe outcome.
SET lock_timeout = '5s';

-- seed-migration-guard:ignore owner=codex issue=pr-343-blind-weighing-label-repair reason=pending-verification-queue-label-scrub-only-new-seeds-compose-blind-labels-from-code expiry=2026-12-31
UPDATE public.verification_items
SET subject_label = NULLIF(
      regexp_replace(
        -- drop ' · <n> kg' anywhere, then a leading '<n> kg · ' or a bare trailing '<n> kg'
        regexp_replace(subject_label, '[[:space:]]*·[[:space:]]*[0-9]+(\.[0-9]+)?[[:space:]]*kg', '', 'g'),
        '^[[:space:]]*[0-9]+(\.[0-9]+)?[[:space:]]*kg[[:space:]]*(·[[:space:]]*)?', '', 'g'),
      '')
WHERE category = 'weighing_proof'
  AND status = 'pending'
  AND subject_label ~ '[0-9][[:space:]]*kg';

-- Anything the strip emptied (a label that was nothing but a weight) gets the composer's bare
-- form for its grain, so no queue row renders blank.
SET lock_timeout = '5s';

-- seed-migration-guard:ignore owner=codex issue=pr-343-blind-weighing-label-repair reason=pending-verification-queue-label-scrub-only-new-seeds-compose-blind-labels-from-code expiry=2026-12-31
UPDATE public.verification_items
SET subject_label = CASE WHEN source_ref_type = 'weighing_shed_observation'
                         THEN 'Whole pen' ELSE 'Individual weigh' END
WHERE category = 'weighing_proof'
  AND status = 'pending'
  AND subject_label IS NULL;

-- +goose Down
-- Intentionally empty. The deleted rows were settings for a category that can no longer be
-- sampled, and restoring them would re-strand weighing items behind a share nothing applies.
SELECT 1;
