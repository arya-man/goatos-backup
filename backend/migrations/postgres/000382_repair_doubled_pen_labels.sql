-- +goose Up
-- MARKERS ADDED 2026-09-21. The file shipped with NO `-- +goose Up` / `-- +goose Down`
-- annotations, so the migrator refused to parse it ("missing -- +goose Up section") and HALTED
-- THE WHOLE CHAIN -- this repair never ran, and neither did anything numbered after it.
-- `make validate-migrations` does not check for the markers, which is why it passed review.
--
-- Repair the doubled pen labels written by the feed-&-water removal submit path.
-- seed-migration-guard:ignore owner=codex issue=pr-340-pen-label-repair reason=live-display-copy-repair-only-no-initial-seed-path expiry=2026-12-31
--
-- The submit composed its label as Display(weighing_campaign_sheds.display_name, partition_label).
-- `display_name` is a bucket's PLANNING label and already carries the pen for every partitioned
-- row, so whenever partition_label was also populated the pen was appended twice and the result
-- was WRITTEN DOWN: "Godel 2 - Part 1 - Part 1", "Mandela 2 - Part 6 - Part 6". The code fix routes
-- that call through oploc.ResolveComposedName; this repairs what the old path already stored.
--
-- FORWARD-ONLY and TEXT-ONLY. Both columns are display copy: neither is a predicate, a join key,
-- an ordering key, nor part of any unique constraint (the evidence row's conflict key is
-- (tenant_id, fasting_task_id, campaign_shed_id), and the verifier item's is its idempotency_key).
-- No weighing, verification, proof or scheduling FACT is touched -- not a weight, an animal, a pen
-- assignment, an operator, a date, a status or a count.
--
-- The rewrite is ANCHORED, not a blanket de-duplication: it collapses only a pen phrase that is
-- immediately followed by the SAME pen phrase, via a backreference. A pen legitimately named
-- "Part 1 - Part 2" (a merged pen) would not match, because the two halves differ.

-- Evidence rows: the label the operator and the verifier both read.
-- Bounded lock: verification_items is a hot table. These UPDATEs touch only the doubled labels
-- (a handful of rows), but a migration must never wait unboundedly on a row lock a live verdict
-- holds. Failing fast and being re-run is the safe outcome. Added with the goose markers above:
-- the file could not previously be parsed, so validate-migrations never reached these statements.
SET lock_timeout = '5s';

UPDATE weighing_fasting_shed_proofs
SET shed_label = regexp_replace(shed_label, '( - Part ([0-9A-Za-z]+))\1$', '\1'),
    updated_at = now()
WHERE shed_label ~ '( - Part ([0-9A-Za-z]+))\1$';

-- The bare-numeric convention doubles as " Castro 1 1"; same anchor, space separator.
UPDATE weighing_fasting_shed_proofs
SET shed_label = regexp_replace(shed_label, '( [0-9]+)\1$', '\1'),
    updated_at = now()
WHERE shed_label ~ '( [0-9]+)\1$';

-- The verifier's subject line, which carries the same label inside a sentence
-- ("Remove feed & water · Godel 2 - Part 1 - Part 1"), so it is anchored at end-of-string too.
-- seed-migration-guard:ignore owner=codex issue=pr-340-pen-label-repair reason=live-display-copy-repair-only-no-initial-seed-path expiry=2026-12-31
UPDATE verification_items
SET subject_label = regexp_replace(subject_label, '( - Part ([0-9A-Za-z]+))\1$', '\1'),
    updated_at = now()
WHERE category = 'weighing_fasting'
  AND subject_label ~ '( - Part ([0-9A-Za-z]+))\1$';

-- seed-migration-guard:ignore owner=codex issue=pr-340-pen-label-repair reason=live-display-copy-repair-only-no-initial-seed-path expiry=2026-12-31
UPDATE verification_items
SET subject_label = regexp_replace(subject_label, '( [0-9]+)\1$', '\1'),
    updated_at = now()
WHERE category = 'weighing_fasting'
  AND subject_label ~ '( [0-9]+)\1$';

-- WHAT IS DELIBERATELY NOT REPAIRED, and why -- audited against live STG on 2026-09-21, where the
-- doubled string reached six tables:
--
--   weighing_fasting_shed_proofs   12 rows   REPAIRED above (the live label an operator reads)
--   verification_items             12 rows   REPAIRED above (the live line a verifier reads)
--   notification_requests         162 rows   NOT repaired -- HISTORY OF WHAT WAS SENT
--   audit_log                      12 rows   NOT repaired -- immutable event history
--   outbox_messages                12 rows   NOT repaired -- immutable event history
--   proof_artifacts                26 rows   NOT repaired -- captured-at-the-time metadata
--   weighing_idempotency_records   12 rows   NOT repaired -- stored RESPONSE snapshots
--
-- The line is LIVE COPY versus RECORD OF THE PAST. The first two are read fresh on every screen,
-- so they must be right. The rest record what the system actually did at a past instant: 162
-- pushes really were delivered to a verifier's phone reading "Godel 2 - Part 2 - Part 2", and a
-- replay cache really did return that string. Rewriting them would make the audit trail state
-- something that did not happen, and would make an idempotent replay return a value the original
-- call never returned. They are never rendered as a live label and they age out on their own.

-- +goose Down
-- Intentionally empty. This is a one-way data repair: the doubled labels carried no information
-- the un-doubled ones lack, and re-doubling them would restore a rendering defect.
SELECT 1;
