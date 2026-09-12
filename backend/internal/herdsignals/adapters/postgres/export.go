package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

type liveCursor struct {
	Key   string `json:"key"`
	Dir   string `json:"dir"`
	Value string `json:"value"`
	TagID string `json:"tag_id"`
	Null  bool   `json:"null"`
}

type liveSortSpec struct {
	key        string
	dir        string
	expr       string
	valueKind  string
	defaultKey bool
}

func normalizeLiveSort(sort []domain.LiveSort) liveSortSpec {
	spec := liveSortSpec{key: "last_seen", dir: "desc", expr: "tl.last_seen_at", valueKind: "time", defaultKey: true}
	if len(sort) == 0 {
		return spec
	}
	switch sort[0].Key {
	case "smart_tag":
		spec = liveSortSpec{key: "smart_tag", dir: "asc", expr: "tl.tag_id", valueKind: "text"}
	case "tag_temp":
		spec = liveSortSpec{key: "tag_temp", dir: "asc", expr: "tl.tag_temperature_c", valueKind: "float"}
	case "motion_count":
		spec = liveSortSpec{key: "motion_count", dir: "asc", expr: "tl.motion_count", valueKind: "int"}
	case "delta_15m":
		spec = liveSortSpec{key: "delta_15m", dir: "asc", expr: "tl.motion_delta", valueKind: "int"}
	case "delta_1h":
		spec = liveSortSpec{key: "delta_1h", dir: "asc", expr: "tl.motion_delta_1h", valueKind: "int"}
	case "last_seen":
		spec = liveSortSpec{key: "last_seen", dir: "asc", expr: "tl.last_seen_at", valueKind: "time"}
	case "":
		// Keep the historic backend ordering for callers that have not opted into table sorting.
	default:
		return spec
	}
	if sort[0].Dir == "desc" {
		spec.dir = "desc"
	}
	return spec
}

func liveCursorFromTag(tag domain.TagLatest, spec liveSortSpec) string {
	cursor := liveCursor{Key: spec.key, Dir: spec.dir, TagID: tag.TagID}
	switch spec.key {
	case "smart_tag":
		cursor.Value = tag.TagID
	case "tag_temp":
		if tag.TagTemperatureC == nil {
			cursor.Null = true
		} else {
			cursor.Value = strconv.FormatFloat(*tag.TagTemperatureC, 'g', -1, 64)
		}
	case "motion_count":
		if tag.MotionCount == nil {
			cursor.Null = true
		} else {
			cursor.Value = strconv.FormatInt(*tag.MotionCount, 10)
		}
	case "delta_15m":
		if tag.MotionDelta == nil {
			cursor.Null = true
		} else {
			cursor.Value = strconv.FormatInt(*tag.MotionDelta, 10)
		}
	case "delta_1h":
		if tag.MotionDelta1h == nil {
			cursor.Null = true
		} else {
			cursor.Value = strconv.FormatInt(*tag.MotionDelta1h, 10)
		}
	default:
		cursor.Value = tag.LastSeenAt.Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal(cursor)
	return "v1." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLiveCursor(raw string, spec liveSortSpec) (liveCursor, bool) {
	if raw == "" {
		return liveCursor{}, false
	}
	// Backward compatibility for old last_seen cursors and CSV export's tag-id walk.
	if !strings.Contains(raw, ".") {
		return liveCursor{Key: spec.key, Dir: spec.dir, TagID: raw}, false
	}
	encoded := strings.TrimPrefix(raw, "v1.")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return liveCursor{}, false
	}
	var cursor liveCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return liveCursor{}, false
	}
	if cursor.Key != spec.key || cursor.Dir != spec.dir || cursor.TagID == "" {
		return liveCursor{}, false
	}
	return cursor, true
}

func liveCursorArg(cursor liveCursor, spec liveSortSpec) interface{} {
	switch spec.valueKind {
	case "int":
		v, _ := strconv.ParseInt(cursor.Value, 10, 64)
		return v
	case "float":
		v, _ := strconv.ParseFloat(cursor.Value, 64)
		return v
	case "time":
		v, _ := time.Parse(time.RFC3339Nano, cursor.Value)
		return v
	default:
		return cursor.Value
	}
}

// ListTagsLatestPage is the ROW half of ListTagsLatest: the same filter builder
// (herdSignalsLiveFilter), the same location join, the same keyset order, and no summary
// aggregate.
//
// GET /herd-signals/export.csv walks the entire filtered result one page at a time through this
// method. Running ListTagsLatest per page instead would re-run the whole-filter summary
// aggregate for every page -- hundreds of full aggregates to produce a number the CSV does not
// even carry. Sharing the filter builder is the point: an export whose WHERE clause is a second
// hand-written copy of the view's is an export that quietly stops matching the screen.
func (r *Repository) ListTagsLatestPage(ctx context.Context, tenantID string, parkID, shedID, movementState, mappingState, pattern, q *string, cursor string, limit int, sort ...domain.LiveSort) ([]domain.TagLatest, error) {
	whereClause, args, argIndex := herdSignalsLiveFilter(tenantID, parkID, shedID, movementState, mappingState, pattern, q)
	spec := normalizeLiveSort(sort)

	if cursor != "" {
		if c, ok := decodeLiveCursor(cursor, spec); ok {
			if c.Null {
				whereClause += fmt.Sprintf(" AND (%s IS NULL AND tl.tag_id > $%d)", spec.expr, argIndex)
				args = append(args, c.TagID)
				argIndex++
			} else if spec.dir == "desc" {
				whereClause += fmt.Sprintf(" AND ((%s IS NOT NULL AND (%s < $%d OR (%s = $%d AND tl.tag_id > $%d))) OR %s IS NULL)", spec.expr, spec.expr, argIndex, spec.expr, argIndex, argIndex+1, spec.expr)
				args = append(args, liveCursorArg(c, spec), c.TagID)
				argIndex += 2
			} else {
				whereClause += fmt.Sprintf(" AND ((%s IS NOT NULL AND (%s > $%d OR (%s = $%d AND tl.tag_id > $%d))) OR %s IS NULL)", spec.expr, spec.expr, argIndex, spec.expr, argIndex, argIndex+1, spec.expr)
				args = append(args, liveCursorArg(c, spec), c.TagID)
				argIndex += 2
			}
		} else {
			whereClause += fmt.Sprintf(" AND (tl.last_seen_at, tl.tag_id) < (SELECT last_seen_at, tag_id FROM public.herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = $%d)", argIndex)
			args = append(args, cursor)
			argIndex++
		}
	}
	orderDir := "ASC"
	if spec.dir == "desc" {
		orderDir = "DESC"
	}
	orderBy := fmt.Sprintf("ORDER BY (%s IS NULL) ASC, %s %s, tl.tag_id ASC", spec.expr, spec.expr, orderDir)
	if spec.defaultKey {
		orderBy = "ORDER BY tl.last_seen_at DESC, tl.tag_id DESC"
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
			%s
			LIMIT $%d
		`, tagLocationJoin, whereClause, orderBy, argIndex)
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
