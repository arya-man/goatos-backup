package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// ListTagsLatestPage is the ROW half of ListTagsLatest: the same filter builder
// (herdSignalsLiveFilter), the same location join, the same keyset order, and no summary
// aggregate.
//
// GET /herd-signals/export.csv walks the entire filtered result one page at a time through this
// method. Running ListTagsLatest per page instead would re-run the whole-filter summary
// aggregate for every page -- hundreds of full aggregates to produce a number the CSV does not
// even carry. Sharing the filter builder is the point: an export whose WHERE clause is a second
// hand-written copy of the view's is an export that quietly stops matching the screen.
func (r *Repository) ListTagsLatestPage(ctx context.Context, tenantID string, parkID, shedID, movementState, mappingState, pattern, q *string, cursor string, limit int) ([]domain.TagLatest, error) {
	whereClause, args, argIndex := herdSignalsLiveFilter(tenantID, parkID, shedID, movementState, mappingState, pattern, q)

	if cursor != "" {
		whereClause += fmt.Sprintf(" AND (tl.last_seen_at, tl.tag_id) < (SELECT last_seen_at, tag_id FROM public.herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = $%d)", argIndex)
		args = append(args, cursor)
		argIndex++
	}

	query := fmt.Sprintf(`
		SELECT tl.tenant_id, tl.tag_id, tl.tag_mac, tl.gateway_id, tl.source, tl.last_seen_at,
		       tl.last_rssi_dbm, tl.signal_state, tl.battery_mv, tl.battery_state, tl.tag_temperature_c,
		       tl.motion_count, tl.motion_delta, tl.motion_delta_1h, tl.previous_motion_count, tl.previous_seen_at,
		       tl.motion_window_seconds, `+effectiveMovementStateExpr+`, `+effectivePatternStateExpr+`, tl.temperature_sensor_ok,
		       tl.accelerometer_sensor_ok, tl.mapping_state, tl.gap_delta, tl.updated_at
		FROM public.herd_signal_tag_latest tl
		%s
		%s
		ORDER BY tl.last_seen_at DESC, tl.tag_id DESC
		LIMIT $%d
	`, tagLocationJoin, whereClause, argIndex)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]domain.TagLatest, 0, limit)
	for rows.Next() {
		var tag domain.TagLatest
		if err := rows.Scan(
			&tag.TenantID, &tag.TagID, &tag.TagMAC, &tag.GatewayID, &tag.Source, &tag.LastSeenAt,
			&tag.LastRSSIdbm, &tag.SignalState, &tag.BatteryMV, &tag.BatteryState, &tag.TagTemperatureC,
			&tag.MotionCount, &tag.MotionDelta, &tag.MotionDelta1h, &tag.PreviousMotionCount, &tag.PreviousSeenAt,
			&tag.MotionWindowSeconds, &tag.MovementState, &tag.PatternState, &tag.TemperatureSensorOK,
			&tag.AccelerometerSensorOK, &tag.MappingState, &tag.GapDelta, &tag.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
