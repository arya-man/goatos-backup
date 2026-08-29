-- +goose Up
-- seed-fixture-guard:ignore: data repair on operational shifting_events rows; no seed contract change.
--
-- SHIFTING REJECT NEVER REACHED THE EVENT ROW (defect, reported 2026-08-29).
--
-- DecideApprovalRequest flipped only counts_approval_requests to 'rejected'; nothing ever wrote
-- shifting_events, so a movement the approver refused stayed authorization_state='pending' /
-- event_status='pending' and sat on the raiser's read-only Pending tab forever. It also kept
-- matching the raised-counts branch of the feed projection (authorization_state='pending' AND
-- event_status='pending'), so a refused movement kept feeding its destination shed — the
-- 2026-08-10 afternoon-correction rule says REJECTION is the one thing that stops that clock.
--
-- The write path now rejects the event row inside the decision transaction
-- (rejectShiftingEventInTx). This migration repairs the rows stranded before that fix: any
-- still-pending shifting event whose approval paperwork ended rejected, with no live (pending or
-- approved) request left. Idempotent: zero stranded rows means zero updates.
--
-- The event_status CASE mirrors the write path: only a still-'pending' movement becomes
-- 'rejected'; a legacy row completed before approval keeps its status while the authorization
-- records the refusal.
SET lock_timeout = '5s';

UPDATE shifting_events se
SET authorization_state = 'rejected',
    event_status = CASE WHEN se.event_status = 'pending' THEN 'rejected' ELSE se.event_status END,
    updated_at = now(),
    row_version = se.row_version + 1
WHERE se.authorization_state = 'pending'
  AND EXISTS (
        SELECT 1
        FROM counts_approval_requests ar
        WHERE ar.tenant_id = se.tenant_id
          AND ar.shifting_event_id = se.shifting_event_id
          AND ar.status = 'rejected')
  AND NOT EXISTS (
        SELECT 1
        FROM counts_approval_requests live
        WHERE live.tenant_id = se.tenant_id
          AND live.shifting_event_id = se.shifting_event_id
          AND live.status IN ('pending', 'approved'));

-- +goose Down
-- Data repair; the pre-repair 'pending' state was the defect, so there is nothing to restore.
SELECT 1;
