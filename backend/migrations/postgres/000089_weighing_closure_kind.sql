-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow tables written by the Weighing planner/mobile/verifier flow; no change to the Vaccination HRMS seed contract
--
-- WHY THE NORMAL COMPLETION PATH NEEDED A NEW COLUMN AND NOT A NEW STATUS.
--
-- Before this migration a weighing bucket had exactly two ways to stop:
--
--   'completed' -- the operator pressed Submit. It says nothing about whether
--                  the verifier ever looked at the video, so a fully verified
--                  bucket and a bucket whose evidence is still queued read
--                  IDENTICALLY. It is a submission marker, not a closure.
--   'closed'    -- a leader ran CloseScope / CloseCampaign. Both
--                  require a REASON, because both are ways of ENDING WORK EARLY.
--
-- So a task whose every animal was weighed, submitted AND verified never reached
-- a terminal "done properly" state at all: the only door to 'closed' was the
-- exception door, and walking a normal completion through it would force a
-- leader to invent a reason for work that finished exactly as intended.
--
-- The fix is NOT a sixth status value. 'closed' is already the terminal,
-- immutable-to-capture state and every predicate in the module
-- (reconcileTerminalWorkItems, the campaign cascade, the reopen eligibility
-- filter, the operator roll-up) is written against the five values the status
-- CHECK constraint admits. Adding a value would fork every one of those
-- predicates and silently leave a bucket in a state half of them do not know
-- about. What was actually missing is not a different terminal state but the
-- ANSWER TO "HOW did this end", which no column carried.
--
-- closure_kind is that answer, and it is the ONLY thing that distinguishes the
-- normal path from the exception path in the record:
--
--   'verified'  -- NORMAL completion. Every SUBMITTED item in the bucket got a
--                  'verified' verdict, and the last of those verdicts closed it
--                  automatically inside the verdict applier's own transaction.
--                  No human ended it, so there is no reason to record and
--                  close_reason stays NULL.
--   'early'     -- CloseScope / CloseCampaign: a leader ended live work,
--                  bypassing the verification gate. Reason mandatory.
--
-- NULL is legal and means a row that reached 'closed' before this column
-- existed. It is deliberately NOT backfilled to 'early': every pre-existing
-- closed row went through the reason-bearing path, and stamping a kind onto a
-- row nobody observed would write a fact we do not have. Readers treat NULL as
-- "closed, kind not recorded".
--
-- Free-flow is preserved: nothing here references goats, herd rosters, expected
-- animal counts or vaccination, and no closure decision anywhere derives from a
-- denominator.

ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS closure_kind text;
ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_closure_kind_check;
ALTER TABLE public.weighing_campaign_sheds
  ADD CONSTRAINT weighing_campaign_sheds_closure_kind_check
  CHECK (closure_kind IS NULL OR closure_kind = ANY (ARRAY['verified','early']));

ALTER TABLE public.weighing_campaigns
  ADD COLUMN IF NOT EXISTS closure_kind text;
ALTER TABLE public.weighing_campaigns
  DROP CONSTRAINT IF EXISTS weighing_campaigns_closure_kind_check;
ALTER TABLE public.weighing_campaigns
  ADD CONSTRAINT weighing_campaigns_closure_kind_check
  CHECK (closure_kind IS NULL OR closure_kind = ANY (ARRAY['verified','early']));

-- The auto-close cascade asks ONE question per verdict: "does this campaign
-- still hold a bucket that is not terminal?". That is a
-- (tenant_id, campaign_id, status) probe on a table whose rows are SHEDS (tens
-- to low hundreds per campaign), and it must not degrade into a scan of every
-- bucket in the tenant as campaign count grows.
CREATE INDEX IF NOT EXISTS weighing_campaign_sheds_campaign_status_idx
  ON public.weighing_campaign_sheds (tenant_id, campaign_id, status);

-- +goose Down
DROP INDEX IF EXISTS public.weighing_campaign_sheds_campaign_status_idx;

ALTER TABLE public.weighing_campaigns
  DROP CONSTRAINT IF EXISTS weighing_campaigns_closure_kind_check;
ALTER TABLE public.weighing_campaigns
  DROP COLUMN IF EXISTS closure_kind;

ALTER TABLE public.weighing_campaign_sheds
  DROP CONSTRAINT IF EXISTS weighing_campaign_sheds_closure_kind_check;
ALTER TABLE public.weighing_campaign_sheds
  DROP COLUMN IF EXISTS closure_kind;
