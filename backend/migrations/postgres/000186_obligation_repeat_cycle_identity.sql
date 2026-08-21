-- +goose Up
-- +goose NO TRANSACTION
-- Repeat-cycle identity: label every repeat obligation with the vaccination that caused it.
--
-- Obligation identity currently includes the DUE DATE (generation.go's obligation key and
-- booster.go's repeat key). For a dose anchored to when the previous one was actually given
-- that date legitimately moves, so a re-run inserts a SECOND row beside the first instead of
-- moving it. Measured in the staging baseline: 222 (animal, dose, version) groups hold more
-- than one scheduled obligation, 207 of them et_tt_revac, pairs 0-1 days apart -- the same
-- cycle minted twice across a day boundary.
--
-- The fix is to anchor identity to the immutable CAUSE rather than the mutable effect.
--
-- Columns are nullable and inert until a writer populates them, so this migration changes no
-- behaviour on its own and can be deployed ahead of the code that uses it.
SET lock_timeout = '3s';

ALTER TABLE obligation_instances
  ADD COLUMN IF NOT EXISTS repeat_cycle_source text,
  ADD COLUMN IF NOT EXISTS repeat_cycle_source_ref text,
  ADD COLUMN IF NOT EXISTS repeat_cycle_anchor_obligation_id uuid,
  ADD COLUMN IF NOT EXISTS repeat_cycle_anchor_at timestamptz,
  ADD COLUMN IF NOT EXISTS repeat_cycle_due_at timestamptz;

RESET lock_timeout;

-- ONE OPEN SUCCESSOR PER ANCHOR -- not one ever.
--
-- "Open", not "ever", is the hinge of the whole design. Global uniqueness would burn the
-- anchor the first time a successor is cancelled or superseded, and the cycle could never
-- restart.
--
-- `missed` is OUTSIDE the open set, and that is deliberate rather than incidental.
-- ReopenDeferredObligationForKey gates on status='deferred' only (see
-- obligation/adapters/postgres/sqlc/commands.sql), and docs/protocol-engine/state-machines.md
-- states that completed, waived, canceled, superseded AND missed rows are immutable history
-- whose correction "creates new work ... it does not rewrite the closed row". A missed
-- successor is therefore closed history and must FREE its anchor so the next pass can mint
-- new work. Had missed stayed inside, the first missed dose would have held that anchor
-- permanently and the animal would silently stop being scheduled -- the exact failure this
-- design exists to prevent, reintroduced through its own predicate.
--
-- A CONCURRENTLY build that fails leaves an INVALID index behind, and IF NOT EXISTS then
-- SKIPS it on the retry: goose marks the migration applied, everyone believes duplicates are
-- being refused, and nothing is enforced at all. Both leftovers are dropped so a retry
-- actually rebuilds. (Same guard as 000185.)
DO $$
DECLARE
  idx text;
BEGIN
  FOREACH idx IN ARRAY ARRAY[
    'obligation_repeat_cycle_open_anchor_unique_idx',
    'obligation_repeat_cycle_open_source_unique_idx'
  ] LOOP
    IF EXISTS (
      SELECT 1 FROM pg_class c
      JOIN pg_index i ON i.indexrelid = c.oid
      WHERE c.relname = idx AND NOT i.indisvalid
    ) THEN
      EXECUTE format('DROP INDEX %I', idx);
    END IF;
  END LOOP;
END $$;

-- rule_id is part of the key because one administration can legitimately cause work under
-- more than one rule: a combo vaccine drives its own revac rule and a shared-component rule
-- from the same dose. Keyed on the anchor alone, the second rule's successor would be
-- rejected outright -- and, since the insert guard is itself rule-scoped, rejected as a hard
-- error rather than an idempotent skip. One open successor per (cause, rule) is the real
-- invariant.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS obligation_repeat_cycle_open_anchor_unique_idx
  ON obligation_instances (tenant_id, rule_id, repeat_cycle_anchor_obligation_id)
  WHERE repeat_cycle_anchor_obligation_id IS NOT NULL
    AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

-- The anchor index alone is not enough. Repeat work minted from accepted or imported history
-- has NO completed obligation to anchor to -- its source is trusted_history:<id> or
-- imported_history:<id> -- so that index skips it entirely and generation could still
-- duplicate it. Uniqueness therefore also covers the source ref.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS obligation_repeat_cycle_open_source_unique_idx
  ON obligation_instances (
    tenant_id, protocol_version_id, rule_id, target_type, target_id,
    repeat_cycle_source, repeat_cycle_source_ref
  )
  WHERE repeat_cycle_source_ref IS NOT NULL
    AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS obligation_repeat_cycle_open_source_unique_idx;
DROP INDEX CONCURRENTLY IF EXISTS obligation_repeat_cycle_open_anchor_unique_idx;

SET lock_timeout = '3s';
ALTER TABLE obligation_instances
  DROP COLUMN IF EXISTS repeat_cycle_due_at,
  DROP COLUMN IF EXISTS repeat_cycle_anchor_at,
  DROP COLUMN IF EXISTS repeat_cycle_anchor_obligation_id,
  DROP COLUMN IF EXISTS repeat_cycle_source_ref,
  DROP COLUMN IF EXISTS repeat_cycle_source;
RESET lock_timeout;
