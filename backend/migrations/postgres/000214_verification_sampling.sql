-- +goose Up
-- seed-fixture-guard:ignore: additive verification sampling policy config + two derived columns on
-- verification_items; no vaccination/HRMS seed contract change and no new app-visible projection
-- table (the Randomization panel is a bounded aggregate over verification_items).
--
-- RANDOMIZED VERIFICATION SAMPLING (maintainer decision 2026-08-26).
--
-- The CEO sets, per verification category, the PERCENTAGE of that category's proof videos the
-- verifier actually has to watch. At 40%, four videos in ten reach her queue and the other six are
-- settled by the policy. Her day is therefore complete when she has cleared HER SHARE, not the
-- whole day's capture: 40% reviewed IS 100% of her work.
--
-- Three properties this schema exists to guarantee:
--
-- 1. THE SAMPLE IS DETERMINISTIC AND MONOTONIC. sampling_bucket is a GENERATED column: a stable
--    0..99 draw from the item's own id, computed by the database for every row that has ever
--    existed and every row that ever will, with no write-path change and no backfill to forget.
--    An item is in sample when bucket < percent, so RAISING the percentage mid-day only ADDS
--    videos -- it can never retract one already sitting in her queue or already reviewed. That is
--    what makes "same day, immediately" safe to offer.
--
-- 2. A PAST DAY KEEPS THE PERCENTAGE IT WAS RUN AT. The policy is effective-dated: a change writes
--    a row at TODAY's business date, and any day resolves to the newest row on or before it. So
--    yesterday's queue and yesterday's progress do not silently rewrite themselves when the CEO
--    changes today's number.
--
-- 3. AN UNSAMPLED VIDEO IS SETTLED, NEVER STRANDED. Verifier approval is not merely review for
--    feed and weighing -- it is the gate that COMPLETES the work (a feed pen-session stays
--    pending_verification until approved; a weighing bucket cannot close while verification is
--    pending, ledger D-5, and that gate is unconditional). Hiding an unsampled item would stall
--    those workflows forever. Instead the closeout stage approves it and stamps
--    auto_resolution = 'not_sampled', so the ordinary verification.verdict.approved event fires and
--    every producer's consumer applies exactly as it does today. verified_by stays NULL: no person
--    made that decision, and the per-verifier analytics group by verified_by, so a waived item can
--    never be counted as work she did.
CREATE TABLE IF NOT EXISTS public.verification_sampling_policies (
    tenant_id               uuid    NOT NULL,
    category                text    NOT NULL,
    effective_business_date date    NOT NULL,
    sample_percent          smallint NOT NULL,
    set_by                  uuid,
    set_at                  timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT verification_sampling_policies_pkey
        PRIMARY KEY (tenant_id, category, effective_business_date),
    CONSTRAINT verification_sampling_policies_category_check
        CHECK (btrim(category) <> ''),
    -- 0 is a real setting (review nothing in this category this day) and so is 100 (review
    -- everything, which is also what NO row means).
    CONSTRAINT verification_sampling_policies_percent_check
        CHECK (sample_percent BETWEEN 0 AND 100)
);

COMMENT ON TABLE public.verification_sampling_policies IS
    'CEO-set per-category verification sampling percentage, effective-dated in Asia/Kolkata business days. Absent row = 100 (verify everything).';

-- The draw. md5 of the item id -> 24 bits -> 0..99. Immutable, so it is a valid STORED generated
-- expression; identical for a given item on every read, in SQL and in Go
-- (verification/domain.SamplingBucket mirrors it byte-for-byte and is pinned by a test).
ALTER TABLE public.verification_items
    ADD COLUMN IF NOT EXISTS sampling_bucket smallint
        GENERATED ALWAYS AS (mod(('x' || substr(md5(item_id::text), 1, 6))::bit(24)::int, 100)) STORED;

-- Why an item was approved without a verifier. NULL for every ordinary verdict.
ALTER TABLE public.verification_items
    ADD COLUMN IF NOT EXISTS auto_resolution text;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'verification_items_auto_resolution_check'
    ) THEN
        ALTER TABLE public.verification_items
            ADD CONSTRAINT verification_items_auto_resolution_check
            CHECK (auto_resolution IS NULL OR auto_resolution = 'not_sampled');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'verification_items_auto_resolution_status_check'
    ) THEN
        -- A waived item is an APPROVAL, never a rejection: sampling decides what gets WATCHED, and
        -- a video nobody watched can never be evidence that the work was wrong.
        ALTER TABLE public.verification_items
            ADD CONSTRAINT verification_items_auto_resolution_status_check
            CHECK (auto_resolution IS NULL OR status = 'approved');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'verification_items_auto_resolution_verifier_check'
    ) THEN
        -- Nobody signed it. This is the constraint that keeps a waived item out of every
        -- per-verifier aggregate, which all group by verified_by.
        ALTER TABLE public.verification_items
            ADD CONSTRAINT verification_items_auto_resolution_verifier_check
            CHECK (auto_resolution IS NULL OR verified_by IS NULL);
    END IF;
END$$;

-- The closeout stage's claim read: pending items whose business day has closed. Partial on
-- status = 'pending' so it stays small -- the table is overwhelmingly settled rows.
CREATE INDEX IF NOT EXISTS verification_items_sampling_closeout_idx
    ON public.verification_items (tenant_id, captured_at)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS verification_items_sampling_closeout_idx;
ALTER TABLE public.verification_items
    DROP CONSTRAINT IF EXISTS verification_items_auto_resolution_verifier_check,
    DROP CONSTRAINT IF EXISTS verification_items_auto_resolution_status_check,
    DROP CONSTRAINT IF EXISTS verification_items_auto_resolution_check;
ALTER TABLE public.verification_items
    DROP COLUMN IF EXISTS auto_resolution,
    DROP COLUMN IF EXISTS sampling_bucket;
DROP TABLE IF EXISTS public.verification_sampling_policies;
