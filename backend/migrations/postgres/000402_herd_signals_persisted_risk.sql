-- Persisted Herd Signals risk classification.
--
-- risk_state used to be computed per request by enriching the WHOLE filtered live cohort
-- (24h p75 baselines + pen medians) and filtering in memory: ~1-5s cold at 20k-50k tags. It is
-- now computed by the change-driven herd-signals risk classifier (app.Service.RunRiskClassifier:
-- only tags never classified, or reporting again after risk_due_at (at most one re-evaluation
-- per tag per 5 minutes), tags in pens whose median moved,
-- tags that just went stale, plus an hourly aging sweep; bounded batches, each in a short
-- transaction under pg_try_advisory_xact_lock) with the exact classifier the live page used.
-- The live page, its risk_state filter/summary and the CSV export all read these columns.
--
-- Indexes for these columns are built CONCURRENTLY in 000404 (no-transaction migration).
-- Additive only: NULL risk_evaluated_at means "not classified yet" (shown as classifying).

-- +goose Up

SET lock_timeout = '5s';

ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS risk_state text,
  ADD COLUMN IF NOT EXISTS risk_score smallint,
  ADD COLUMN IF NOT EXISTS risk_reasons text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS risk_own_motion_delta_pct double precision,
  ADD COLUMN IF NOT EXISTS risk_group_motion_delta_pct double precision,
  ADD COLUMN IF NOT EXISTS risk_group_temp_delta_c double precision,
  ADD COLUMN IF NOT EXISTS risk_evaluated_at timestamptz,
  ADD COLUMN IF NOT EXISTS risk_due_at timestamptz;

-- NOT VALID then VALIDATE: the ADD takes only a brief lock; VALIDATE scans under SHARE UPDATE
-- EXCLUSIVE, which does not block ingest writes.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'herd_signal_tag_latest_risk_state_check'
      AND conrelid = 'public.herd_signal_tag_latest'::regclass
  ) THEN
    ALTER TABLE public.herd_signal_tag_latest
      ADD CONSTRAINT herd_signal_tag_latest_risk_state_check
      CHECK (risk_state IS NULL OR risk_state IN ('low', 'watch', 'high')) NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE public.herd_signal_tag_latest
  VALIDATE CONSTRAINT herd_signal_tag_latest_risk_state_check;

COMMENT ON COLUMN public.herd_signal_tag_latest.risk_state IS
  'Persisted risk classification (low/watch/high); NULL with risk_evaluated_at set = no risk signal.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_score IS
  'Classifier score behind risk_state: own-baseline + pen-group motion/temperature + pattern + sensor points.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_reasons IS
  'Human-readable reasons behind risk_state, in classifier order.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_own_motion_delta_pct IS
  'Motion vs own 24h baseline (percent) the classifier scored; NULL when not computable.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_group_motion_delta_pct IS
  'Motion vs pen median (percent) the classifier scored; NULL when not computable.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_group_temp_delta_c IS
  'Tag temperature minus pen median (C) the classifier scored; NULL when not computable.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_evaluated_at IS
  'Set on EVERY classifier evaluation of this tag (changed or not); NULL = never classified or queued for re-classification.';

COMMENT ON COLUMN public.herd_signal_tag_latest.risk_due_at IS
  'Earliest time a new packet re-queues this tag for classification (risk_evaluated_at + the minimum re-evaluation interval). Every tag in a live herd reports every few seconds, so "seen since last evaluation" alone would re-classify the whole herd every tick.';

-- Per-pen comparison medians the classifier scored against. A pen is the partition the animal
-- resides in (maintainer decision 2026-09-24): shed_id + the normalized goat_shed_partitions
-- label, 'whole' for an undivided shed -- the oploc.OperationalLocation.Key() grain, never the
-- shed alone and never a name. Upserted by key when a pen's median moves; vanished pens deleted
-- by key, never a whole-tenant delete + reinsert.
CREATE TABLE IF NOT EXISTS public.herd_signal_pen_medians (
  tenant_id       uuid        NOT NULL,
  shed_id         uuid        NOT NULL,
  partition_key   text        NOT NULL,
  partition_label text,
  motion_median   double precision,
  temp_median     double precision,
  computed_at     timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, shed_id, partition_key)
);

COMMENT ON TABLE public.herd_signal_pen_medians IS
  'Herd Signals risk classifier pen baselines: median non-gap 15m motion_delta and tag temperature of mapped tags per pen (shed + partition the animal resides in). partition_key is the oploc NormalizePartition matching key, never display.';

-- +goose Down

DROP TABLE IF EXISTS public.herd_signal_pen_medians;
ALTER TABLE public.herd_signal_tag_latest
  DROP CONSTRAINT IF EXISTS herd_signal_tag_latest_risk_state_check,
  DROP COLUMN IF EXISTS risk_due_at,
  DROP COLUMN IF EXISTS risk_evaluated_at,
  DROP COLUMN IF EXISTS risk_group_temp_delta_c,
  DROP COLUMN IF EXISTS risk_group_motion_delta_pct,
  DROP COLUMN IF EXISTS risk_own_motion_delta_pct,
  DROP COLUMN IF EXISTS risk_reasons,
  DROP COLUMN IF EXISTS risk_score,
  DROP COLUMN IF EXISTS risk_state;
