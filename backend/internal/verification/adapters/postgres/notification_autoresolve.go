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
// sampled auto-approve, batch closure) writes its outbox event through insertOutboxEvent in the
// SAME transaction, so that is where this runs: the notifications resolve atomically with the
// decision. A row still queued/failed is suppressed so a push asking someone to review a decided
// item is never sent; a row mid-lease ('sending') keeps its delivery status and gets only the
// read stamp, as MarkRead does. The unread counter (migration 000403) follows via its trigger.
//
// SUBMISSION GRAIN: the bridge keys a multi-item submission's pending push on
// 'verification.item.pending:submission:<id>' and the queue's idempotency keeps only the first
// item's rows, under that first item's calendar_event_id. So the notice is resolved only when NO
// item of the submission is still pending, and it is looked up across all of the submission's
// items' calendar ids.
//
// projection-review: membership=notification_requests with tenant_id = $1 AND calendar_event_id
// in the decided item's (or its submission's) 'verification:<item_id>' ids AND notification_type
// = 'verification_pending' AND still unread; grain=delivery rows, all stamped together so every
// device copy of one notice agrees; group_key=none; join_cardinality=scope is 1..N items of one
// submission (bounded by one operator upload), still_open is an EXISTS; pagination=none, bounded
// write; scope=tenant_id on every table.
//
// scale-guard:ignore: bounded write -- one item's (or one submission's) notices, reached by
// notification_requests_event_idx (tenant_id, calendar_event_id, ...).
const sqlResolveDecidedItemNotifications = `
WITH decided AS (
  SELECT vi.item_id, vi.source_submission_id
  FROM verification_items vi
  WHERE vi.tenant_id = $1::uuid AND vi.item_id = $2::uuid
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
still_open AS (
  SELECT 1
  FROM verification_items vi
  JOIN scope s ON s.item_id = vi.item_id
  WHERE vi.tenant_id = $1::uuid
    AND vi.status = 'pending'
    AND vi.closed_at IS NULL
  LIMIT 1
)
UPDATE notification_requests nr
SET read_at = now(),
    status = CASE
      WHEN nr.status IN ('queued', 'failed') THEN 'suppressed'
      WHEN nr.status IN ('sent', 'exhausted', 'suppressed') THEN 'read'
      ELSE nr.status
    END,
    updated_at = now()
WHERE nr.tenant_id = $1::uuid
  AND nr.calendar_event_id IN (SELECT 'verification:' || s.item_id::text FROM scope s)
  AND nr.notification_type = 'verification_pending'
  AND nr.read_at IS NULL
  AND nr.status <> 'read'
  AND NOT EXISTS (SELECT 1 FROM still_open)`

// sqlResolveDriveReadyNotifications: "vaccination drive ready to close" is obsolete once the
// drive is closed. The bridge stamps it calendar_event_id = 'verification:<batch_id>' with
// message_key vaccination.drive.ready_to_close.
//
// projection-review: membership=notification_requests with tenant_id = $1 AND calendar_event_id
// = 'verification:<batch_id>' AND message_key = vaccination.drive.ready_to_close AND unread;
// grain=delivery rows; group_key=none; join_cardinality=none; pagination=none, bounded write;
// scope=tenant_id.
//
// scale-guard:ignore: bounded write -- one drive's notices via notification_requests_event_idx.
const sqlResolveDriveReadyNotifications = `
UPDATE notification_requests nr
SET read_at = now(),
    status = CASE
      WHEN nr.status IN ('queued', 'failed') THEN 'suppressed'
      WHEN nr.status IN ('sent', 'exhausted', 'suppressed') THEN 'read'
      ELSE nr.status
    END,
    updated_at = now()
WHERE nr.tenant_id = $1::uuid
  AND nr.calendar_event_id = 'verification:' || $2::text
  AND nr.context->>'message_key' = 'vaccination.drive.ready_to_close'
  AND nr.read_at IS NULL
  AND nr.status <> 'read'`

// resolveDecidedItemNotifications runs inside the decision's transaction.
func resolveDecidedItemNotifications(ctx context.Context, tx pgx.Tx, tenantID, itemID string) error {
	if _, err := tx.Exec(ctx, sqlResolveDecidedItemNotifications, tenantID, itemID); err != nil {
		return fmt.Errorf("verification: resolve pending notifications: %w", err)
	}
	return nil
}

// resolveDriveReadyNotifications runs inside CloseVaccinationBatch's transaction.
func resolveDriveReadyNotifications(ctx context.Context, tx pgx.Tx, tenantID, batchID string) error {
	if _, err := tx.Exec(ctx, sqlResolveDriveReadyNotifications, tenantID, batchID); err != nil {
		return fmt.Errorf("verification: resolve drive-ready notifications: %w", err)
	}
	return nil
}
