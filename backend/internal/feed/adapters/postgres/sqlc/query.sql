-- name: RecordFeedDirection :one
-- Idempotent on (tenant_id, idempotency_key); returns no row on replay.
INSERT INTO feed_direction_completions (
  tenant_id, obligation_id, batch_id, shed_id, ration_protocol_version_id, sop_submission_item_id,
  feed_inventory_lot_id, quantity_fed, quantity_unit, head_count, fed_at, status, recorded_by, idempotency_key
) VALUES (
  @tenant_id, @obligation_id, @batch_id, @shed_id, @ration_protocol_version_id, @sop_submission_item_id,
  @feed_inventory_lot_id, @quantity_fed, @quantity_unit, @head_count, @fed_at, @status, @recorded_by, @idempotency_key
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING completion_id::text AS completion_id;

-- name: AcceptFeedDirection :many
-- SM-5 verify (accept). Acts only on a still-recorded row, returning its verification context so the
-- caller can complete the obligation + (later) consume feed stock. Idempotent: an already-accepted/
-- rejected row matches nothing → no rows → caller no-ops.
UPDATE feed_direction_completions
SET status = 'accepted', verified_by = @verified_by, verified_at = now(),
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded'
RETURNING obligation_id::text AS obligation_id,
          shed_id::text AS shed_id,
          COALESCE(batch_id::text, '')::text AS batch_id,
          COALESCE(feed_inventory_lot_id::text, '')::text AS feed_inventory_lot_id,
          COALESCE(quantity_fed::text, '')::text AS quantity_fed,
          COALESCE(quantity_unit, '')::text AS quantity_unit,
          fed_at;

-- name: RejectFeedDirection :execrows
-- SM-5 verify (rework). Acts only on a still-recorded row. Idempotent: returns 0 rows on replay.
UPDATE feed_direction_completions
SET status = 'rejected', verified_by = @verified_by, verified_at = now(),
    rejection_reason = @rejection_reason, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded';

-- name: ListFeedDirectionsByShed :many
-- Per-shed feed history (most recent first). Uses feed_direction_completions_shed_history_idx.
SELECT completion_id::text AS completion_id, obligation_id::text AS obligation_id,
       COALESCE(batch_id::text, '')::text AS batch_id, fed_at, status,
       COALESCE(quantity_fed::text, '')::text AS quantity_fed, COALESCE(quantity_unit, '')::text AS quantity_unit,
       COALESCE(head_count, 0)::int AS head_count
FROM feed_direction_completions
WHERE tenant_id = @tenant_id AND shed_id = @shed_id
ORDER BY fed_at DESC, completion_id DESC
LIMIT @row_limit;

-- name: ListRecordedFeedDirections :many
-- Verification queue: directions awaiting review (status='recorded'), earliest fed first.
-- Uses feed_direction_completions_review_idx (tenant_id, fed_at) WHERE status='recorded'.
SELECT completion_id::text AS completion_id, obligation_id::text AS obligation_id,
       shed_id::text AS shed_id, COALESCE(batch_id::text, '')::text AS batch_id, fed_at,
       COALESCE(quantity_fed::text, '')::text AS quantity_fed, COALESCE(quantity_unit, '')::text AS quantity_unit,
       COALESCE(head_count, 0)::int AS head_count
FROM feed_direction_completions
WHERE tenant_id = @tenant_id AND status = 'recorded'
  AND fed_at IS NOT NULL
ORDER BY fed_at ASC, completion_id ASC
LIMIT @row_limit;
