package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AUTO-RESOLVE: a decided item's "please verify" notifications are obsolete.
//
// The notification bridge turns verification.item.pending into verification_pending
// notification_requests rows (verifier: <module>.proof.pending.verifier; leadership:
// <module>.proof.pending.leadership), stamped calendar_event_id = 'verification:<item_id>'.
// Nothing ever marked them read, so unread history -- and the bell badge -- grew with every
// proof ever filed (stg 2026-09-24: 85% of the worst member's 96k rows).
//
// Every transition that ends an item's decidability (approve, rework, closeout, withdraw,
// sampled auto-approve, batch closure) calls resolveDecisionNotifications in the SAME
// transaction, so the notifications resolve atomically with the decision. This covers the
// leadership "verification pending" copy as well as the verifier's (product note: leadership's
// "proof arrived" notice disappears from unread once the item is decided). A row still queued/failed is suppressed so a push asking someone to review a decided
// item is never sent; a row mid-lease ('sending') keeps its delivery status and gets only the
// read stamp, as MarkRead does. The unread counter (migration 000403) follows via its trigger.
//
// SUBMISSION GRAIN: the bridge keys a multi-item submission's pending push on
// 'verification.item.pending:submission:<id>' and the queue's idempotency keeps only the first
// item's rows, under that first item's calendar_event_id. One notice therefore stands for the
// whole submission and resolves only when NO item of it is still pending. A notice keyed to its
// OWN item ('verification.item.pending:<item_id>', every item without a submission) resolves the
// moment that item is decided. The two are told apart by the notice's own event_key, not by the
// item, so a per-item notice is never held back by siblings.
//
// ONE STATEMENT PER TRANSACTION -- THIS IS A LOCKING RULE, NOT STYLE. Every notification write
// fires the 000403 counter trigger, which locks the affected members' counter rows (sorted)
// until COMMIT. Two statements in one transaction over different member sets can invert that
// order against the bridge's multi-member QueueRoleNotifications and deadlock (reproduced by
// TestMultiItemDecisionDoesNotDeadlockWithAMultiMemberProducer). So every decision path
// collects ALL its decided item ids (and the closed drive, if any) and calls
// resolveDecisionNotifications exactly once, as the LAST write before commit so the counter row
// locks -- including the hot leadership members' rows -- are held for the shortest time.
//
// projection-review: membership=notification_requests with tenant_id = $1 AND calendar_event_id
// in the decided items' (and their submissions' sibling items') 'verification:<item_id>' ids AND
// notification_type = 'verification_pending' AND still unread, plus -- when $3 is set -- the
// closed drive's 'verification:<batch_id>' vaccination.drive.ready_to_close notice; grain=delivery
// rows, all stamped together so every device copy of one notice agrees; group_key=none;
// join_cardinality=scope is the decided items plus their submissions' siblings (bounded by one
// operator upload / one drive), open_submissions is DISTINCT per submission; pagination=none,
// bounded write; scope=tenant_id on every table.
//
// scale-guard:ignore: bounded write -- one decision's notices, reached by
// notification_requests_event_idx (tenant_id, calendar_event_id, ...).
const sqlResolveDecisionNotifications = `
WITH decided AS (
  SELECT vi.item_id, vi.source_submission_id
  FROM verification_items vi
  WHERE vi.tenant_id = $1::uuid AND vi.item_id = ANY($2::uuid[])
),
scope AS (
  SELECT d.item_id FROM decided d
  UNION
  SELECT sib.item_id
  FROM decided d
  JOIN verification_items sib
    ON sib.tenant_id = $1::uuid
   AND sib.source_submission_id = d.source_submission_id
  WHERE d.source_submission_id IS NOT NULL
),
open_submissions AS (
  SELECT DISTINCT vi.source_submission_id
  FROM verification_items vi
  JOIN scope s ON s.item_id = vi.item_id
  WHERE vi.tenant_id = $1::uuid
    AND vi.source_submission_id IS NOT NULL
    AND vi.status = 'pending'
    AND vi.closed_at IS NULL
),
open_items AS (
  SELECT vi.item_id
  FROM verification_items vi
  JOIN scope s ON s.item_id = vi.item_id
  WHERE vi.tenant_id = $1::uuid
    AND vi.status = 'pending'
    AND vi.closed_at IS NULL
),
-- The rows to resolve are found by PROBING notification_requests_event_idx (tenant_id,
-- calendar_event_id) once per event id, and only then updated by primary key. Do not fold this
-- back into one WHERE on notification_requests: written as
-- "calendar_event_id IN (SELECT ... FROM scope) OR ($3 <> '' AND ...)" the planner turns the IN into
-- a hashed SubPlan filter and SEQ-SCANS the whole table on every approve (stg 2026-10-01: 246k
-- rows / 709 MB, verdict p50 0.10 s -> 0.49 s, p99 to 9 s after 000403 shipped).
targets AS (
  SELECT nr.notification_request_id
  FROM scope s
  JOIN notification_requests nr
    ON nr.tenant_id = $1::uuid
   AND nr.calendar_event_id = 'verification:' || s.item_id::text
  WHERE nr.read_at IS NULL
    AND nr.status <> 'read'
    AND nr.notification_type = 'verification_pending'
    AND CASE
      WHEN nr.context->>'event_key' LIKE 'verification.item.pending:submission:%'
        THEN NOT EXISTS (
          SELECT 1 FROM open_submissions o
          WHERE nr.context->>'event_key' = 'verification.item.pending:submission:' || o.source_submission_id::text)
      ELSE NOT EXISTS (
          SELECT 1 FROM open_items oi
          WHERE nr.calendar_event_id = 'verification:' || oi.item_id::text)
    END
  UNION
  SELECT nr.notification_request_id
  FROM notification_requests nr
  WHERE $3::text <> ''
    AND nr.tenant_id = $1::uuid
    AND nr.calendar_event_id = 'verification:' || $3::text
    AND nr.read_at IS NULL
    AND nr.status <> 'read'
    AND nr.context->>'message_key' = 'vaccination.drive.ready_to_close'
)
UPDATE notification_requests nr
SET read_at = now(),
    status = CASE
      WHEN nr.status IN ('queued', 'failed') THEN 'suppressed'
      WHEN nr.status IN ('sent', 'exhausted', 'suppressed') THEN 'read'
      ELSE nr.status
    END,
    updated_at = now()
FROM targets t
WHERE nr.notification_request_id = t.notification_request_id
  AND nr.tenant_id = $1::uuid`

// resolveDecisionNotifications runs ONCE, last, inside a decision's transaction (see above).
// driveBatchID is "" unless the transaction closed a vaccination drive.
func resolveDecisionNotifications(ctx context.Context, tx pgx.Tx, tenantID string, itemIDs []string, driveBatchID string) error {
	if len(itemIDs) == 0 && driveBatchID == "" {
		return nil
	}
	if itemIDs == nil {
		itemIDs = []string{}
	}
	if _, err := tx.Exec(ctx, sqlResolveDecisionNotifications, tenantID, itemIDs, driveBatchID); err != nil {
		return fmt.Errorf("verification: resolve decided notifications: %w", err)
	}
	return nil
}
