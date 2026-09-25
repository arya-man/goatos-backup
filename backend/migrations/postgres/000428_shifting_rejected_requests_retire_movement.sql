-- +goose Up
-- seed-migration-guard:ignore owner=counts issue=approvals-review-2026-09-25 reason=forward-data-repair-of-live-shifting-rows-no-seed-owned-table-or-seed-path-changes expiry=2027-03-31
-- seed-fixture-guard:ignore: a one-time data repair of rows the rejected-approval defect stranded; it seeds no vocabulary and no fixture.
--
-- REJECTING A PEN-MOVE APPROVAL NOW RETIRES THE MOVEMENT (2026-09-25). Until this release a reject
-- flipped only counts_approval_requests.status to 'rejected' and left the governed shifting_events
-- row at authorization_state='pending', event_status='pending' for ever: the raiser's read-only
-- Pending tab kept listing a movement nobody would approve, and the feed projection (which counts a
-- RAISED movement until it is REJECTED) kept feeding the destination pen for animals that never
-- came. The decide transaction now flips the event to rejected/rejected
-- (counts/adapters/postgres.rejectShiftingEventInTx); this repairs the rows rejected before it.
--
-- Exactly the same write as the runtime fix, and exactly the same fence: only a movement still
-- pending/pending whose governing request is 'rejected' is touched, so an authorized, applied,
-- canceled or already-rejected movement is never rewritten. Tenant-safe (the request and the event
-- are joined on tenant_id AND shifting_event_id). Idempotent: a second run matches nothing.
SET lock_timeout = '5s';
UPDATE public.shifting_events se
SET authorization_state = 'rejected',
    event_status = 'rejected',
    updated_at = now(),
    row_version = se.row_version + 1
FROM public.counts_approval_requests ar
WHERE ar.tenant_id = se.tenant_id
  AND ar.shifting_event_id = se.shifting_event_id
  AND ar.request_type = 'shifting'
  AND ar.status = 'rejected'
  AND se.authorization_state = 'pending'
  AND se.event_status = 'pending'
  -- A movement that ALSO has a live (pending or approved) request is not retired: the rejected row
  -- was superseded, not the movement.
  AND NOT EXISTS (
      SELECT 1 FROM public.counts_approval_requests live
      WHERE live.tenant_id = se.tenant_id
        AND live.shifting_event_id = se.shifting_event_id
        AND live.status IN ('pending', 'approved')
  );
RESET lock_timeout;

-- +goose Down
-- No-op: the repaired rows are exactly the state the runtime now writes on a reject, and reverting
-- them would put refused movements back into the feed projection.
SELECT 1;
