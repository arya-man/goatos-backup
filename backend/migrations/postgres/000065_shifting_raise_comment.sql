-- +goose Up
-- Maintainer decision 2026-07-31: the operator raising a shifting request may leave an
-- optional free-text note explaining WHY the animals are being moved. It is read by the
-- park head deciding the approval and by the verifier reviewing the evidence afterwards.
--
-- Nullable with no default and no backfill: an absent comment means the raiser did not
-- write one, which is a different fact from an empty string, and every movement raised
-- before this migration genuinely has none. ADD COLUMN with no default is metadata-only
-- in Postgres 11+, so it does not rewrite this append-only header table.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS raise_comment text;

-- NOT VALID, deliberately. shifting_events is a hot table, so a validating CHECK would take
-- ACCESS EXCLUSIVE and scan every row (validate-postgres-migrations.sh enforces this). No
-- separate VALIDATE step is scheduled and none is needed: NOT VALID still enforces the bound
-- on every INSERT and UPDATE from this point on, and the only rows it skips are the
-- pre-existing ones, whose raise_comment is NULL and therefore already satisfies the check.
-- The bound is 1000 CHARACTERS (char_length, not bytes) to match the API's rune-counted
-- maxShiftingCommentRunes, so a Kannada or Telugu note is measured the same on both sides.
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_raise_comment_length_check
    CHECK (raise_comment IS NULL OR char_length(raise_comment) <= 1000) NOT VALID;

COMMENT ON COLUMN public.shifting_events.raise_comment IS
    'Optional operator note captured when the movement was raised. Shown to the approving park head and to the verifier during evidence review. NULL means no note was written.';

-- +goose Down
-- Dropping the column removes its CHECK with it, so there is no separate DROP CONSTRAINT
-- statement here -- an explicit one would be a bare DROP CONSTRAINT on a hot table.
ALTER TABLE public.shifting_events
    DROP COLUMN IF EXISTS raise_comment;
