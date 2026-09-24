-- Persisted Herd Signals risk classification.
--
-- risk_state used to be computed per request by enriching the WHOLE filtered live cohort
-- (24h p75 baselines + pen medians) and filtering in memory: ~1-3s cold at 20k-50k tags. It is
-- now computed by the herd-signals risk classifier (app.Service.RecomputeRisk, batched keyset
-- pages, run under an advisory lock on an interval) with the exact classifier the live page
-- uses, and the risk_state filter is an indexed predicate with keyset paging.
--
-- Additive only: NULL risk_evaluated_at means "not classified yet" and the read path falls back
-- to the live per-page classification for display.

-- +goose Up

SET lock_timeout = '5s';

ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS risk_state text,
  ADD COLUMN IF NOT EXISTS risk_score smallint,
  ADD COLUMN IF NOT EXISTS risk_reasons text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS risk_evaluated_at timestamptz;

ALTER TABLE public.herd_signal_tag_latest
  DROP CONSTRAINT IF EXISTS herd_signal_tag_latest_risk_state_check;
ALTER TABLE public.herd_signal_tag_latest
  ADD CONSTRAINT herd_signal_tag_latest_risk_state_check
  CHECK (risk_state IS NULL OR risk_state IN ('low', 'watch', 'high'));

COMMENT ON COLUMN public.herd_signal_tag_latest.risk_state IS
  'Persisted risk classification (low/watch/high); NULL = no risk signal or not yet classified (see risk_evaluated_at).';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_score IS
  'Score behind risk_state: own-baseline + pen-group motion/temperature + pattern + sensor points.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_reasons IS
  'Human-readable reasons behind risk_state, in classifier order.';
COMMENT ON COLUMN public.herd_signal_tag_latest.risk_evaluated_at IS
  'When the risk classifier last evaluated this tag; NULL = never.';

-- risk_state filter (attention = any non-null state) + default last_seen keyset order.
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_risk_idx
  ON public.herd_signal_tag_latest (tenant_id, last_seen_at DESC, tag_id DESC)
  WHERE risk_state IS NOT NULL;
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_risk_state_idx
  ON public.herd_signal_tag_latest (tenant_id, risk_state, last_seen_at DESC, tag_id DESC)
  WHERE risk_state IS NOT NULL;

-- Per-pen comparison medians the classifier scored against, so a live page reads its pens'
-- baseline by key instead of aggregating the whole tenant per request. Upserted per pass;
-- pens that vanished are deleted by key, never a whole-tenant delete + reinsert.
CREATE TABLE IF NOT EXISTS public.herd_signal_pen_medians (
  tenant_id     uuid        NOT NULL,
  shed_id       uuid        NOT NULL,
  motion_median double precision,
  temp_median   double precision,
  computed_at   timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, shed_id)
);

COMMENT ON TABLE public.herd_signal_pen_medians IS
  'Herd Signals risk classifier pen baselines: median non-gap 15m motion_delta and tag temperature of mapped tags per pen.';

-- +goose Down

DROP TABLE IF EXISTS public.herd_signal_pen_medians;

DROP INDEX IF EXISTS public.herd_signal_tag_latest_risk_state_idx;
DROP INDEX IF EXISTS public.herd_signal_tag_latest_risk_idx;
ALTER TABLE public.herd_signal_tag_latest
  DROP CONSTRAINT IF EXISTS herd_signal_tag_latest_risk_state_check,
  DROP COLUMN IF EXISTS risk_evaluated_at,
  DROP COLUMN IF EXISTS risk_reasons,
  DROP COLUMN IF EXISTS risk_score,
  DROP COLUMN IF EXISTS risk_state;
