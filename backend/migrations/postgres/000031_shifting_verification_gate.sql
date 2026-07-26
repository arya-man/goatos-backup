-- +goose Up
-- Shifting verification gate (maintainer decision, 2026-07-26; SUPERSEDES the 2026-07-19
-- "operator completion applies the move" rule FOR SHIFTING ONLY).
--
-- Old flow (2026-07-19):
--   authorized --complete(operator)--> applied            (relocation + count move at completion)
--
-- New flow (2026-07-26):
--   authorized --complete(operator, MANDATORY video)--> pending_verification   (NOTHING moves)
--   pending_verification --verifier approve--> applied                          (relocation + count move NOW)
--   pending_verification --verifier reject--> authorized                        (bounce; operator re-shoots)
--
-- The count now moves at VERIFIER APPROVAL, not at operator completion. A shifting completion
-- enqueues a generic verification item (category 'shifting_move') carrying the operator's video, and
-- the relocation is applied only when the verifier's verdict.approved event is consumed. Birth/death
-- approvals are UNCHANGED and still apply immediately.
--
-- This migration:
--   1. adds 'pending_verification' to the shifting_events event_status domain (lock-safe
--      DROP -> ADD NOT VALID -> VALIDATE; the new value is a superset so existing rows validate),
--   2. requires a non-blank proof_ref (the mandatory video artifact) for any 'pending_verification'
--      row (NOT VALID: guards every new write; no legacy row carries this new state so nothing to
--      backfill),
--   3. requires a 'pending_verification' row to already be authorized (defense in depth: an
--      operator can only submit a video for a move a manager already permitted).
--
-- shifting_events is an operational/event table (not initial-seed-owned): the seed never writes a
-- 'pending_verification' or 'applied' shifting row by hand -- those states are produced only by the
-- counts execution service + the verification consumer on the production path. No seed command
-- changes; see docs/runbooks/initial-seed-migration-coupling.md classification.

-- The operator's supplied destination cohort tag must survive from completion (submit) to the later
-- verifier-approval relocation, so the profile cross-check (a supplied tag must AGREE with the
-- destination shed's configured cohort) still runs when the move is actually applied. Nullable: for
-- an occupied/configured shed the operator supplies nothing and the cohort is derived server-side.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS completion_destination_tag text;

-- +goose StatementBegin
DO $$
BEGIN
    -- 1. Extend the event_status domain with 'pending_verification'.
    ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_status_check;
    ALTER TABLE public.shifting_events
        ADD CONSTRAINT shifting_events_status_check
        CHECK ((event_status = ANY (ARRAY[
            'pending'::text,
            'authorized'::text,
            'pending_verification'::text,
            'applied'::text,
            'rejected'::text,
            'canceled'::text,
            'unresolved'::text
        ]))) NOT VALID;

    -- 2. A move awaiting verification MUST carry the operator's video (proof_ref). Enforced on every
    --    new write; legacy rows never hold this state, so no backfill/validation is needed.
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_pending_verification_proof_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_pending_verification_proof_check
            CHECK (((event_status <> 'pending_verification'::text)
                 OR ((proof_ref IS NOT NULL) AND (btrim(proof_ref) <> ''::text)))) NOT VALID;
    END IF;

    -- 3. A move awaiting verification must already be authorized (a video cannot be submitted for a
    --    movement no manager permitted).
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_pending_verification_requires_authorization_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_pending_verification_requires_authorization_check
            CHECK (((event_status <> 'pending_verification'::text)
                 OR (authorization_state = 'authorized'::text))) NOT VALID;
    END IF;
END $$;
-- +goose StatementEnd

-- The status domain widening is safe to VALIDATE: every existing row holds one of the prior values,
-- all of which remain in the new allow-list.
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_status_check;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_pending_verification_requires_authorization_check;
    ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_pending_verification_proof_check;
    ALTER TABLE public.shifting_events DROP COLUMN IF EXISTS completion_destination_tag;

    -- Restore the pre-000031 event_status domain. Any rows still in 'pending_verification' would
    -- violate the restored constraint; move them back to 'authorized' (their pre-completion state)
    -- first so the down-migration is safe to run.
    UPDATE public.shifting_events SET event_status = 'authorized'
        WHERE event_status = 'pending_verification'::text;

    ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_status_check;
    ALTER TABLE public.shifting_events
        ADD CONSTRAINT shifting_events_status_check
        CHECK ((event_status = ANY (ARRAY[
            'pending'::text,
            'authorized'::text,
            'applied'::text,
            'rejected'::text,
            'canceled'::text,
            'unresolved'::text
        ])));
END $$;
-- +goose StatementEnd
