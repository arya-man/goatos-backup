package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// Repository implements ports.Repository using Postgres.
type Repository struct {
	db *pgxpool.Pool
}

// NewRepository creates a new Postgres repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// UpsertGateway updates or inserts a gateway registration.
func (r *Repository) UpsertGateway(ctx context.Context, tenantID string, gw domain.Gateway) error {
	query := `
		INSERT INTO public.herd_signal_gateways (
			tenant_id, gateway_id, label, park_id, shed_id, location_id,
			wifi_mac, ble_mac, network_mode, status, last_seen_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (tenant_id, gateway_id) DO UPDATE
		SET label = COALESCE($3, label),
		    park_id = COALESCE($4, park_id),
		    shed_id = COALESCE($5, shed_id),
		    location_id = COALESCE($6, location_id),
		    wifi_mac = COALESCE($7, wifi_mac),
		    ble_mac = COALESCE($8, ble_mac),
		    network_mode = COALESCE($9, network_mode),
		    status = COALESCE($10, status),
		    last_seen_at = COALESCE($11, last_seen_at),
		    updated_at = now()
	`
	_, err := r.db.Exec(ctx, query,
		tenantID, gw.GatewayID, gw.Label, gw.ParkID, gw.ShedID, gw.LocationID,
		gw.WifiMAC, gw.BLEMAC, gw.NetworkMode, gw.Status, gw.LastSeenAt,
	)
	return err
}

// GetGatewaysByTenant fetches all gateways for a tenant.
func (r *Repository) GetGatewaysByTenant(ctx context.Context, tenantID string) ([]domain.Gateway, error) {
	query := `
		SELECT tenant_id, gateway_id, label, park_id, shed_id, location_id,
		       wifi_mac, ble_mac, network_mode, status, last_seen_at, created_at, updated_at
		FROM public.herd_signal_gateways
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gws []domain.Gateway
	for rows.Next() {
		var gw domain.Gateway
		if err := rows.Scan(
			&gw.TenantID, &gw.GatewayID, &gw.Label, &gw.ParkID, &gw.ShedID, &gw.LocationID,
			&gw.WifiMAC, &gw.BLEMAC, &gw.NetworkMode, &gw.Status, &gw.LastSeenAt,
			&gw.CreatedAt, &gw.UpdatedAt,
		); err != nil {
			return nil, err
		}
		gws = append(gws, gw)
	}
	return gws, rows.Err()
}

// activityWindowTiers are the tiers packets are rolled up into on every ingest. Must match
// domain.SupportedBucketSeconds.
var activityWindowTiers = []int{60, 300, 3600}

// IngestPackets stores raw packets and updates tag latest state in ONE transaction: packet
// insert, gateway last_seen_at upsert, activity-window rollups (all tiers), and tag_latest
// (movement_state/pattern_state/signal_state/battery_state) all commit or roll back together.
//
// Idempotent / out-of-order safe: activity windows key on (tag_id, bucket_start,
// bucket_seconds) and recompute first/last motion_count and packet_count from the packets in
// THIS call plus the stored bucket, not by blindly incrementing; tag_latest only advances when
// the new packet's received_at is strictly newer than the currently stored last_seen_at for
// that tag (see updateTagLatest's guarded UPDATE), so a replayed or late packet cannot move
// the "latest" snapshot backwards or double-apply a delta.
// Returns (stored count, latest_updated count, error).
func (r *Repository) IngestPackets(ctx context.Context, tenantID string, gw domain.Gateway, packets []domain.Packet) (int, int, error) {
	if len(packets) == 0 {
		return 0, 0, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if _, err := tx.Exec(ctx, `
		INSERT INTO public.herd_signal_gateways (
			tenant_id, gateway_id, label, park_id, shed_id, location_id,
			wifi_mac, ble_mac, network_mode, status, last_seen_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (tenant_id, gateway_id) DO UPDATE
		SET label = COALESCE($3, public.herd_signal_gateways.label),
		    park_id = COALESCE($4, public.herd_signal_gateways.park_id),
		    shed_id = COALESCE($5, public.herd_signal_gateways.shed_id),
		    location_id = COALESCE($6, public.herd_signal_gateways.location_id),
		    wifi_mac = COALESCE($7, public.herd_signal_gateways.wifi_mac),
		    ble_mac = COALESCE($8, public.herd_signal_gateways.ble_mac),
		    network_mode = COALESCE($9, public.herd_signal_gateways.network_mode),
		    status = COALESCE($10, public.herd_signal_gateways.status),
		    last_seen_at = COALESCE($11, public.herd_signal_gateways.last_seen_at),
		    updated_at = now()
	`,
		tenantID, gw.GatewayID, gw.Label, gw.ParkID, gw.ShedID, gw.LocationID,
		gw.WifiMAC, gw.BLEMAC, gw.NetworkMode, gw.Status, gw.LastSeenAt,
	); err != nil {
		return 0, 0, fmt.Errorf("upsert gateway: %w", err)
	}

	stored := 0

	batch := &pgx.Batch{}
	for _, p := range packets {
		batch.Queue(`
			INSERT INTO public.herd_signal_packets (
				tenant_id, gateway_id, source, tag_id, tag_mac, received_at,
				gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c,
				motion_count, sensor_state, temperature_sensor_ok,
				accelerometer_sensor_ok, raw_adv, raw_payload
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (tenant_id, tag_id, received_at, motion_count) DO NOTHING
		`,
			tenantID, p.GatewayID, p.Source, p.TagID, p.TagMAC, p.ReceivedAt,
			p.GatewaySeenAt, p.RSSIdbm, p.BatteryMV, p.TagTemperatureC,
			p.MotionCount, p.SensorState, p.TemperatureSensorOK,
			p.AccelerometerSensorOK, p.RawAdv, p.RawPayload,
		)
	}

	// newPackets holds only the packets that were actually inserted this call (ON CONFLICT DO
	// NOTHING skips exact replays). Activity-window rollup and tag_latest update must operate on
	// this subset, not the full request payload -- otherwise a replayed batch would double-count
	// packet_count and motion deltas even though the duplicate packet rows themselves were
	// correctly deduped.
	newPackets := make([]domain.Packet, 0, len(packets))

	results := tx.SendBatch(ctx, batch)
	for i := 0; i < len(packets); i++ {
		ct, err := results.Exec()
		if err != nil {
			results.Close()
			return stored, 0, fmt.Errorf("insert packet %d: %w", i, err)
		}
		if ct.RowsAffected() > 0 {
			stored++
			newPackets = append(newPackets, packets[i])
		}
	}
	if err := results.Close(); err != nil {
		return stored, 0, fmt.Errorf("close packet insert batch: %w", err)
	}

	if len(newPackets) == 0 {
		// Entire batch was a replay: nothing new to roll up or advance tag_latest with.
		if err := tx.Commit(ctx); err != nil {
			return stored, 0, fmt.Errorf("commit ingest tx (no-op replay): %w", err)
		}
		return stored, 0, nil
	}

	// Roll up packets to activity windows for each tier. Must happen before tag_latest update
	// so the 24h/300s history is available for pattern classification.
	for _, bucketSeconds := range activityWindowTiers {
		if err := r.upsertActivityWindowsTx(ctx, tx, tenantID, newPackets, bucketSeconds); err != nil {
			return stored, 0, fmt.Errorf("upsert activity windows (%ds): %w", bucketSeconds, err)
		}
	}

	// Update tag latest state for each unique tag. Group packets by tag_id since a single
	// ingest batch may carry several packets per tag.
	tagPackets := make(map[string][]domain.Packet)
	for _, p := range newPackets {
		tagPackets[p.TagID] = append(tagPackets[p.TagID], p)
	}

	latestUpdated := 0
	for tagID, tagPkts := range tagPackets {
		advanced, err := r.updateTagLatest(ctx, tx, tenantID, tagID, tagPkts)
		if err != nil {
			return stored, latestUpdated, err
		}
		if advanced {
			latestUpdated++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return stored, latestUpdated, fmt.Errorf("commit ingest tx: %w", err)
	}

	return stored, latestUpdated, nil
}

// updateTagLatest updates the latest state for a single tag based on new packets, computing
// movement_state, pattern_state, signal_state and battery_state for real (defect #1 fix: these
// were previously hardcoded placeholders with no production caller).
//
// Returns advanced=true only when the snapshot's last_seen_at actually moved forward, so a
// caller replaying an already-applied or out-of-order-late packet does not get counted as a
// fresh update.
func (r *Repository) updateTagLatest(ctx context.Context, tx pgx.Tx, tenantID, tagID string, packets []domain.Packet) (bool, error) {
	if len(packets) == 0 {
		return false, nil
	}

	thresholds := domain.DefaultThresholds()

	// Most recent packet in THIS batch, by received_at (out-of-order safe within the batch).
	latestPkt := packets[0]
	for i := 1; i < len(packets); i++ {
		if packets[i].ReceivedAt.After(latestPkt.ReceivedAt) {
			latestPkt = packets[i]
		}
	}

	// Lock and read the existing snapshot (if any) so the advance-only-if-newer guard and the
	// delta computation see a consistent prior state under concurrent ingests for the same tag.
	var existing *domain.TagLatest
	row := tx.QueryRow(ctx, `
		SELECT last_seen_at, motion_count, pattern_state, mapping_state
		FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1 AND tag_id = $2
		FOR UPDATE
	`, tenantID, tagID)
	var lastSeenAt time.Time
	var motionCount *int64
	var patternState, mappingState string
	err := row.Scan(&lastSeenAt, &motionCount, &patternState, &mappingState)
	switch {
	case err == pgx.ErrNoRows:
		existing = nil
	case err != nil:
		return false, fmt.Errorf("lock existing tag_latest: %w", err)
	default:
		existing = &domain.TagLatest{LastSeenAt: lastSeenAt, MotionCount: motionCount, PatternState: patternState, MappingState: mappingState}
	}

	if existing != nil && !latestPkt.ReceivedAt.After(existing.LastSeenAt) {
		// Late/replayed packet: it was still stored above (append-only packet log and
		// activity-window rollup already handled it idempotently), but it must not move the
		// "latest" snapshot backwards.
		return false, nil
	}

	previousMotionCount := (*int64)(nil)
	previousSeenAt := (*time.Time)(nil)
	previousPattern := string(domain.PatternUnknown)
	mapping := "unmapped"
	if existing != nil {
		previousMotionCount = existing.MotionCount
		prevSeen := existing.LastSeenAt
		previousSeenAt = &prevSeen
		previousPattern = existing.PatternState
		mapping = existing.MappingState
	}

	delta, _ := domain.MotionDelta(latestPkt.MotionCount, previousMotionCount)

	// movement_state and pattern_state are computed over history, not the single ingest-batch
	// delta (AGENTS.md: compare like grain to like grain). Pull the trailing 15-minute window
	// from the 60s tier (already upserted above in this same tx) for the movement-state delta,
	// and the trailing 24h of 300s-tier windows for the pattern-state baseline.
	windowDelta, err := r.sumActivityWindowDeltaTx(ctx, tx, tenantID, tagID, 60, latestPkt.ReceivedAt.Add(-15*time.Minute), latestPkt.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("sum 15m activity window delta: %w", err)
	}
	movementState := domain.MovementStateFromDelta(windowDelta, 900, thresholds)
	// Stale overrides an otherwise-computed movement state: no packet in 30+ minutes.
	if time.Since(latestPkt.ReceivedAt) > time.Duration(thresholds.StalePacketMinutes)*time.Minute {
		movementState = "stale"
	}

	history, err := r.listActivityWindowsTx(ctx, tx, tenantID, tagID, 300, latestPkt.ReceivedAt.Add(-24*time.Hour), latestPkt.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("load 24h pattern history: %w", err)
	}
	seenAt := latestPkt.ReceivedAt
	patternStateComputed := domain.PatternStateFromHistory(windowDelta, &seenAt, time.Now().UTC(), history, previousPattern, thresholds)

	signalState := domain.SignalStateFromRSSI(latestPkt.RSSIdbm, nil, thresholds)
	batteryState := domain.BatteryStateFromMillivolts(latestPkt.BatteryMV, thresholds)

	// Tag->animal mapping is resolved on ingest so live/timeline reads never pay for the join.
	// Re-resolved on every ingest (cheap point lookup) rather than only once, since
	// smart_tag_capable / identifier status can change after the tag was first seen.
	mapping, _, err = r.ResolveTagMapping(ctx, tenantID, &tagID, latestPkt.TagMAC)
	if err != nil {
		return false, fmt.Errorf("resolve tag mapping: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO public.herd_signal_tag_latest (
			tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
			last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
			motion_count, motion_delta, previous_motion_count, previous_seen_at,
			motion_window_seconds, movement_state, pattern_state, temperature_sensor_ok,
			accelerometer_sensor_ok, mapping_state, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, now())
		ON CONFLICT (tenant_id, tag_id) DO UPDATE
		SET tag_mac = COALESCE($3, public.herd_signal_tag_latest.tag_mac),
		    gateway_id = COALESCE($4, public.herd_signal_tag_latest.gateway_id),
		    source = COALESCE($5, public.herd_signal_tag_latest.source),
		    last_seen_at = $6,
		    last_rssi_dbm = COALESCE($7, public.herd_signal_tag_latest.last_rssi_dbm),
		    signal_state = $8,
		    battery_mv = COALESCE($9, public.herd_signal_tag_latest.battery_mv),
		    battery_state = $10,
		    tag_temperature_c = COALESCE($11, public.herd_signal_tag_latest.tag_temperature_c),
		    motion_count = COALESCE($12, public.herd_signal_tag_latest.motion_count),
		    motion_delta = $13,
		    previous_motion_count = $14,
		    previous_seen_at = $15,
		    motion_window_seconds = $16,
		    movement_state = $17,
		    pattern_state = $18,
		    temperature_sensor_ok = COALESCE($19, public.herd_signal_tag_latest.temperature_sensor_ok),
		    accelerometer_sensor_ok = COALESCE($20, public.herd_signal_tag_latest.accelerometer_sensor_ok),
		    mapping_state = $21,
		    updated_at = now()
	`,
		tenantID, tagID, latestPkt.TagMAC, latestPkt.GatewayID, latestPkt.Source, latestPkt.ReceivedAt,
		latestPkt.RSSIdbm, signalState,
		latestPkt.BatteryMV, batteryState,
		latestPkt.TagTemperatureC,
		latestPkt.MotionCount, delta,
		previousMotionCount, previousSeenAt,
		900, movementState,
		patternStateComputed,
		latestPkt.TemperatureSensorOK, latestPkt.AccelerometerSensorOK, mapping,
	)
	if err != nil {
		return false, fmt.Errorf("upsert tag_latest: %w", err)
	}
	return true, nil
}

// sumActivityWindowDeltaTx sums motion_delta across non-gap buckets of the given tier that fall
// within [from, to], for use as the movement_state delta. Bounded by the tier's own bucket
// grain -- callers pass a range appropriate to the tier (15 minutes of 60s buckets here).
func (r *Repository) sumActivityWindowDeltaTx(ctx context.Context, tx pgx.Tx, tenantID, tagID string, bucketSeconds int, from, to time.Time) (int64, error) {
	var sum *int64
	err := tx.QueryRow(ctx, `
		SELECT SUM(motion_delta)
		FROM public.herd_signal_activity_windows
		WHERE tenant_id = $1 AND tag_id = $2 AND bucket_seconds = $3
		      AND bucket_start >= $4 AND bucket_start <= $5 AND packet_count > 0
	`, tenantID, tagID, bucketSeconds, from, to).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if sum == nil {
		return 0, nil
	}
	return *sum, nil
}

// listActivityWindowsTx loads activity windows for pattern-state history computation inside the
// ingest transaction (mirrors ListActivityWindows but reads through the tx, not the pool).
func (r *Repository) listActivityWindowsTx(ctx context.Context, tx pgx.Tx, tenantID, tagID string, bucketSeconds int, from, to time.Time) ([]domain.ActivityWindow, error) {
	rows, err := tx.Query(ctx, `
		SELECT bucket_start, bucket_seconds, motion_delta, packet_count
		FROM public.herd_signal_activity_windows
		WHERE tenant_id = $1 AND tag_id = $2 AND bucket_seconds = $3
		      AND bucket_start >= $4 AND bucket_start <= $5
		ORDER BY bucket_start ASC
	`, tenantID, tagID, bucketSeconds, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var windows []domain.ActivityWindow
	for rows.Next() {
		var w domain.ActivityWindow
		if err := rows.Scan(&w.BucketStart, &w.BucketSeconds, &w.MotionDelta, &w.PacketCount); err != nil {
			return nil, err
		}
		w.IsGap = w.PacketCount == 0
		windows = append(windows, w)
	}
	return windows, rows.Err()
}

// GetTagLatest fetches the current state of a single tag.
func (r *Repository) GetTagLatest(ctx context.Context, tenantID, tagID string) (*domain.TagLatest, error) {
	query := `
		SELECT tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
		       last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
		       motion_count, motion_delta, previous_motion_count, previous_seen_at,
		       motion_window_seconds, movement_state, pattern_state, temperature_sensor_ok,
		       accelerometer_sensor_ok, mapping_state, updated_at
		FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1 AND tag_id = $2
	`
	var tag domain.TagLatest
	err := r.db.QueryRow(ctx, query, tenantID, tagID).Scan(
		&tag.TenantID, &tag.TagID, &tag.TagMAC, &tag.GatewayID, &tag.Source, &tag.LastSeenAt,
		&tag.LastRSSIdbm, &tag.SignalState, &tag.BatteryMV, &tag.BatteryState, &tag.TagTemperatureC,
		&tag.MotionCount, &tag.MotionDelta, &tag.PreviousMotionCount, &tag.PreviousSeenAt,
		&tag.MotionWindowSeconds, &tag.MovementState, &tag.PatternState, &tag.TemperatureSensorOK,
		&tag.AccelerometerSensorOK, &tag.MappingState, &tag.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListTagsLatest fetches tags with optional filters, keyset pagination, and summary counts.
// tagLocationJoin resolves each tag's current park_id/shed_id through its mapped animal.
// herd_signal_tag_latest deliberately stores no park_id/shed_id of its own: an animal's shed
// changes over time (moves, reclassification) and the tag table would silently go stale if it
// cached location. The join is LATERAL + LIMIT 1 (cheap point lookup via the
// goat_identifiers normalized_value index) rather than a second denormalized copy of location.
const tagLocationJoin = `
	LEFT JOIN LATERAL (
		SELECT gi.goat_id
		FROM public.goat_identifiers gi
		WHERE gi.tenant_id = tl.tenant_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		  AND gi.normalized_value IN (tl.tag_id, COALESCE(tl.tag_mac, ''))
		LIMIT 1
	) mapped_goat ON true
	LEFT JOIN public.goats g ON g.tenant_id = tl.tenant_id AND g.goat_id = mapped_goat.goat_id
`

// ListTagsLatest fetches tags with optional filters, keyset pagination, and a whole-filter
// server-side summary aggregate (never summed from the returned page -- AGENTS.md operational
// read model contract rule 3).
func (r *Repository) ListTagsLatest(ctx context.Context, tenantID string, parkID, shedID, movementState *string, mapped *bool, cursor string, limit int) (
	[]domain.TagLatest, domain.Summary, *string, error,
) {
	whereClause := "WHERE tl.tenant_id = $1"
	args := []interface{}{tenantID}
	argIndex := 2

	if parkID != nil && *parkID != "" {
		whereClause += fmt.Sprintf(" AND g.park_id = $%d", argIndex)
		args = append(args, *parkID)
		argIndex++
	}

	if shedID != nil && *shedID != "" {
		whereClause += fmt.Sprintf(" AND g.shed_id = $%d", argIndex)
		args = append(args, *shedID)
		argIndex++
	}

	if movementState != nil && *movementState != "" {
		whereClause += fmt.Sprintf(" AND tl.movement_state = $%d", argIndex)
		args = append(args, *movementState)
		argIndex++
	}

	if mapped != nil {
		if *mapped {
			whereClause += " AND tl.mapping_state = 'mapped'"
		} else {
			whereClause += " AND tl.mapping_state = 'unmapped'"
		}
	}

	if cursor != "" {
		whereClause += fmt.Sprintf(" AND (tl.last_seen_at, tl.tag_id) < (SELECT last_seen_at, tag_id FROM public.herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = $%d)", argIndex)
		args = append(args, cursor)
		argIndex++
	}

	query := fmt.Sprintf(`
		SELECT tl.tenant_id, tl.tag_id, tl.tag_mac, tl.gateway_id, tl.source, tl.last_seen_at,
		       tl.last_rssi_dbm, tl.signal_state, tl.battery_mv, tl.battery_state, tl.tag_temperature_c,
		       tl.motion_count, tl.motion_delta, tl.previous_motion_count, tl.previous_seen_at,
		       tl.motion_window_seconds, tl.movement_state, tl.pattern_state, tl.temperature_sensor_ok,
		       tl.accelerometer_sensor_ok, tl.mapping_state, tl.updated_at
		FROM public.herd_signal_tag_latest tl
		%s
		%s
		ORDER BY tl.last_seen_at DESC, tl.tag_id DESC
		LIMIT $%d
	`, tagLocationJoin, whereClause, argIndex)
	args = append(args, limit+1) // Fetch one extra to detect if there are more

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, domain.Summary{}, nil, err
	}
	defer rows.Close()

	var tags []domain.TagLatest
	for rows.Next() {
		var tag domain.TagLatest
		if err := rows.Scan(
			&tag.TenantID, &tag.TagID, &tag.TagMAC, &tag.GatewayID, &tag.Source, &tag.LastSeenAt,
			&tag.LastRSSIdbm, &tag.SignalState, &tag.BatteryMV, &tag.BatteryState, &tag.TagTemperatureC,
			&tag.MotionCount, &tag.MotionDelta, &tag.PreviousMotionCount, &tag.PreviousSeenAt,
			&tag.MotionWindowSeconds, &tag.MovementState, &tag.PatternState, &tag.TemperatureSensorOK,
			&tag.AccelerometerSensorOK, &tag.MappingState, &tag.UpdatedAt,
		); err != nil {
			return nil, domain.Summary{}, nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, domain.Summary{}, nil, err
	}

	var nextCursor *string
	if len(tags) > limit {
		tags = tags[:limit]
		lastTag := tags[len(tags)-1]
		nextCursor = &lastTag.TagID
	}

	// Summary: the SAME filter (park/shed/mapped), WITHOUT the movement_state predicate or the
	// cursor/limit, aggregated server-side in one query -- never derived from the returned page.
	summaryWhere := "WHERE tl.tenant_id = $1"
	summaryArgs := []interface{}{tenantID}
	sArgIndex := 2
	if parkID != nil && *parkID != "" {
		summaryWhere += fmt.Sprintf(" AND g.park_id = $%d", sArgIndex)
		summaryArgs = append(summaryArgs, *parkID)
		sArgIndex++
	}
	if shedID != nil && *shedID != "" {
		summaryWhere += fmt.Sprintf(" AND g.shed_id = $%d", sArgIndex)
		summaryArgs = append(summaryArgs, *shedID)
		sArgIndex++
	}
	if mapped != nil {
		if *mapped {
			summaryWhere += " AND tl.mapping_state = 'mapped'"
		} else {
			summaryWhere += " AND tl.mapping_state = 'unmapped'"
		}
	}

	summary, err := r.computeSummary(ctx, tagLocationJoin, summaryWhere, summaryArgs)
	if err != nil {
		return nil, domain.Summary{}, nil, err
	}

	return tags, summary, nextCursor, nil
}

// computeSummary computes the whole-filter aggregate counts in a single bounded query.
func (r *Repository) computeSummary(ctx context.Context, join, whereClause string, args []interface{}) (domain.Summary, error) {
	query := fmt.Sprintf(`
		SELECT count(*) FILTER (WHERE true),
		       count(*) FILTER (WHERE tl.mapping_state = 'mapped'),
		       count(*) FILTER (WHERE tl.mapping_state = 'unmapped'),
		       count(*) FILTER (WHERE tl.movement_state = 'moving'),
		       count(*) FILTER (WHERE tl.movement_state = 'quiet'),
		       count(*) FILTER (WHERE tl.movement_state = 'not_moving'),
		       count(*) FILTER (WHERE tl.movement_state = 'stale'),
		       count(*) FILTER (WHERE tl.signal_state = 'weak'),
		       count(*) FILTER (WHERE tl.battery_state = 'low'),
		       count(*) FILTER (WHERE tl.temperature_sensor_ok IS FALSE OR tl.accelerometer_sensor_ok IS FALSE)
		FROM public.herd_signal_tag_latest tl
		%s
		%s
	`, join, whereClause)

	var s domain.Summary
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&s.TagsSeen, &s.MappedAnimals, &s.UnmappedTags,
		&s.Moving, &s.Quiet, &s.NotMoving, &s.Stale,
		&s.WeakSignal, &s.LowBattery, &s.SensorAbnormal,
	)
	return s, err
}

// ListActivityWindows fetches bucketed motion data for a tag.
func (r *Repository) ListActivityWindows(ctx context.Context, tenantID, tagID string, from, to time.Time, bucketSeconds int) ([]domain.ActivityWindow, error) {
	query := `
		SELECT tenant_id, tag_id, bucket_start, bucket_seconds, first_motion_count,
		       last_motion_count, motion_delta, packet_count, avg_rssi_dbm,
		       min_rssi_dbm, max_rssi_dbm, first_seen_at, last_seen_at
		FROM public.herd_signal_activity_windows
		WHERE tenant_id = $1 AND tag_id = $2 AND bucket_start >= $3 AND bucket_start <= $4
		      AND bucket_seconds = $5
		ORDER BY bucket_start ASC
	`
	rows, err := r.db.Query(ctx, query, tenantID, tagID, from, to, bucketSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var windows []domain.ActivityWindow
	for rows.Next() {
		var w domain.ActivityWindow
		if err := rows.Scan(
			&w.TenantID, &w.TagID, &w.BucketStart, &w.BucketSeconds, &w.FirstMotionCount,
			&w.LastMotionCount, &w.MotionDelta, &w.PacketCount, &w.AvgRSSIdbm,
			&w.MinRSSIdbm, &w.MaxRSSIdbm, &w.FirstSeenAt, &w.LastSeenAt,
		); err != nil {
			return nil, err
		}
		windows = append(windows, w)
	}
	return windows, rows.Err()
}

// GetGoatIdentifier resolves a tag_id or tag_mac to a goat_id.
func (r *Repository) GetGoatIdentifier(ctx context.Context, tenantID, normalizedValue string) (*ports.GoatIdentifierResult, error) {
	query := `
		SELECT goat_id, normalized_value, smart_tag_capable
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND normalized_value = $2 AND status = 'active' AND smart_tag_capable IS TRUE
		LIMIT 1
	`
	var result ports.GoatIdentifierResult
	err := r.db.QueryRow(ctx, query, tenantID, normalizedValue).Scan(
		&result.GoatID, &result.NormalizedValue, &result.SmartTagCapable,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ResolveTagMapping resolves a tag to a goat (mapped, unmapped, conflict).
func (r *Repository) ResolveTagMapping(ctx context.Context, tenantID string, tagID, tagMAC *string) (string, *string, error) {
	// Try to match tag_id or tag_mac against goat_identifiers
	if tagID == nil && tagMAC == nil {
		return "unmapped", nil, nil
	}

	// Normalize values
	var values []string
	if tagID != nil && *tagID != "" {
		values = append(values, *tagID)
	}
	if tagMAC != nil && *tagMAC != "" {
		values = append(values, *tagMAC)
	}

	if len(values) == 0 {
		return "unmapped", nil, nil
	}

	// Query for matching goats
	query := `
		SELECT DISTINCT goat_id
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND normalized_value = ANY($2) AND status = 'active' AND smart_tag_capable IS TRUE
	`
	rows, err := r.db.Query(ctx, query, tenantID, values)
	if err != nil {
		return "unmapped", nil, err
	}
	defer rows.Close()

	var goatIDs []string
	for rows.Next() {
		var goatID string
		if err := rows.Scan(&goatID); err != nil {
			return "unmapped", nil, err
		}
		goatIDs = append(goatIDs, goatID)
	}

	if len(goatIDs) == 0 {
		return "unmapped", nil, nil
	} else if len(goatIDs) == 1 {
		return "mapped", &goatIDs[0], nil
	} else {
		// Multiple goats match (conflict)
		return "conflict", nil, nil
	}
}

// GetGoatsByIDs fetches display_id and location for multiple goats.
func (r *Repository) GetGoatsByIDs(ctx context.Context, tenantID string, goatIDs []string) (map[string]ports.GoatData, error) {
	if len(goatIDs) == 0 {
		return make(map[string]ports.GoatData), nil
	}

	query := `
		SELECT goat_id, display_id, shed_id, park_id
		FROM public.goats
		WHERE tenant_id = $1 AND goat_id = ANY($2)
	`
	rows, err := r.db.Query(ctx, query, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]ports.GoatData)
	for rows.Next() {
		var goatID, displayID string
		var shedID, parkID *string
		if err := rows.Scan(&goatID, &displayID, &shedID, &parkID); err != nil {
			return nil, err
		}
		result[goatID] = ports.GoatData{
			DisplayID: displayID,
			ShedID:    shedID,
			ParkID:    parkID,
		}
	}
	return result, rows.Err()
}

// GetLocationsByIDs fetches shed and partition info for multiple locations.
func (r *Repository) GetLocationsByIDs(ctx context.Context, tenantID string, locationIDs []string) (map[string]ports.LocationData, error) {
	if len(locationIDs) == 0 {
		return make(map[string]ports.LocationData), nil
	}

	query := `
		SELECT location_id, name, partition_label, park_id
		FROM public.locations
		WHERE tenant_id = $1 AND location_id = ANY($2)
	`
	rows, err := r.db.Query(ctx, query, tenantID, locationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]ports.LocationData)
	for rows.Next() {
		var locID, shedName string
		var partitionLabel *string
		var parkID *string
		if err := rows.Scan(&locID, &shedName, &partitionLabel, &parkID); err != nil {
			return nil, err
		}
		result[locID] = ports.LocationData{
			ShedName:       shedName,
			PartitionLabel: partitionLabel,
			ParkID:         parkID,
		}
	}
	return result, rows.Err()
}

// ResolveTagsBatch resolves many tag_id/tag_mac values to goat_id in ONE query so
// GET /herd-signals/live never issues one identifier lookup per returned row (AGENTS.md
// operational read model contract). Callers with a conflicting (multi-goat) value must have
// already filtered it via ResolveTagMapping/mapping_state; this batch form returns the first
// match per value and is used only to attach a display goat_id to rows already known 'mapped'.
func (r *Repository) ResolveTagsBatch(ctx context.Context, tenantID string, values []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(values) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT normalized_value, goat_id
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND status = 'active' AND smart_tag_capable IS TRUE
		      AND normalized_value = ANY($2)
	`, tenantID, values)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value, goatID string
		if err := rows.Scan(&value, &goatID); err != nil {
			return nil, err
		}
		if _, exists := result[value]; !exists {
			result[value] = goatID
		}
	}
	return result, rows.Err()
}

// GetShedLocations resolves many shed location_ids to their operational location (park id/name,
// shed name, and an at-most-one active partition per AGREE-OR-GO-BARE) in ONE query. Mirrors
// oploc.ShedScopedLocationSQL's per-shed logic, batched with ANY() instead of one query per
// shed, for the same reason ResolveTagsBatch is batched.
func (r *Repository) GetShedLocations(ctx context.Context, tenantID string, shedIDs []string) (map[string]ports.ShedLocation, error) {
	result := make(map[string]ports.ShedLocation)
	if len(shedIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT shed.location_id,
		       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
		       COALESCE(park.location_id::text, ''),
		       COALESCE(park.name, ''),
		       COALESCE((
		         SELECT min(sp.partition_label)
		         FROM shed_partitions sp
		         WHERE sp.tenant_id = shed.tenant_id
		           AND sp.shed_id = shed.location_id
		           AND sp.status = 'active'
		           AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
		         HAVING count(*) = 1
		       ), '')
		FROM public.locations shed
		LEFT JOIN public.locations park
		       ON park.tenant_id = shed.tenant_id AND park.location_id = shed.parent_location_id
		WHERE shed.tenant_id = $1 AND shed.location_id = ANY($2::uuid[])
	`, tenantID, shedIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var shedID string
		var loc ports.ShedLocation
		if err := rows.Scan(&shedID, &loc.ShedName, &loc.ParkID, &loc.ParkName, &loc.PartitionLabel); err != nil {
			return nil, err
		}
		result[shedID] = loc
	}
	return result, rows.Err()
}

// GetInsightsData computes the raw counts behind the 12 GET /herd-signals/insights cards. Each
// card is its own small, tenant-scoped, indexed, time-bounded query -- never one
// compute-on-read god CTE across every table (AGENTS.md scale anti-patterns).
func (r *Repository) GetInsightsData(ctx context.Context, tenantID string) (ports.InsightsData, error) {
	var d ports.InsightsData

	// Card 1-4, 6-7, 12: all direct/derived from herd_signal_tag_latest, one indexed,
	// tenant-scoped aggregate query.
	err := r.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE movement_state <> 'stale'),
			count(*) FILTER (WHERE pattern_state = 'missing_signal'),
			count(*) FILTER (WHERE pattern_state IN ('quiet_watch', 'inactive')),
			count(*) FILTER (WHERE pattern_state = 'spike'),
			count(*) FILTER (WHERE signal_state = 'weak'),
			count(*) FILTER (WHERE battery_state = 'low'),
			count(*) FILTER (WHERE mapping_state = 'unmapped')
		FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&d.TagsLiveNow, &d.MissingSignalCount, &d.LowMovementWatchCount, &d.HighMovementSpikeCount,
		&d.WeakSignalTagsCount, &d.BatteryAttentionCount, &d.UnmappedSmartTagsCount,
	)
	if err != nil {
		return d, fmt.Errorf("insights core aggregate: %w", err)
	}

	// Card 5: shed_signal_coverage. Denominator = distinct sheds with a gateway deployed
	// (herd_signal_gateways.shed_id, indexed). Numerator = distinct sheds holding a live
	// (non-stale) tag, resolved through the mapped animal, same as the live view's join.
	err = r.db.QueryRow(ctx, `SELECT count(DISTINCT shed_id) FROM public.herd_signal_gateways WHERE tenant_id = $1 AND shed_id IS NOT NULL`, tenantID).Scan(&d.ShedsTotal)
	if err != nil {
		return d, fmt.Errorf("insights sheds total: %w", err)
	}
	err = r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(DISTINCT g.shed_id)
		FROM public.herd_signal_tag_latest tl
		%s
		WHERE tl.tenant_id = $1 AND tl.movement_state <> 'stale' AND g.shed_id IS NOT NULL
	`, tagLocationJoin), tenantID).Scan(&d.ShedsWithCoverage)
	if err != nil {
		return d, fmt.Errorf("insights sheds with coverage: %w", err)
	}

	// Card 8: post_vaccination_movement_watch. Bounded to the last 24h of accepted
	// vaccination_completions (indexed by tenant_id), joined to a mapped tag currently watched.
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT vc.goat_id)
		FROM public.vaccination_completions vc
		JOIN public.goat_identifiers gi
		  ON gi.tenant_id = vc.tenant_id AND gi.goat_id = vc.goat_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = vc.tenant_id
		  AND (tl.tag_id = gi.normalized_value OR tl.tag_mac = gi.normalized_value)
		WHERE vc.tenant_id = $1
		  AND vc.status = 'accepted'
		  AND vc.administered_at >= now() - interval '24 hours'
		  AND tl.pattern_state IN ('quiet_watch', 'inactive', 'missing_signal')
	`, tenantID).Scan(&d.PostVaccinationWatchCount)
	if err != nil {
		return d, fmt.Errorf("insights post-vaccination watch: %w", err)
	}

	// Card 9: health_case_activity_trend. Bounded to currently-active health_cases.
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT hc.goat_id)
		FROM public.health_cases hc
		JOIN public.goat_identifiers gi
		  ON gi.tenant_id = hc.tenant_id AND gi.goat_id = hc.goat_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = hc.tenant_id
		  AND (tl.tag_id = gi.normalized_value OR tl.tag_mac = gi.normalized_value)
		WHERE hc.tenant_id = $1
		  AND hc.status = 'active'
		  AND tl.pattern_state IN ('quiet_watch', 'inactive')
	`, tenantID).Scan(&d.HealthCaseActivityCount)
	if err != nil {
		return d, fmt.Errorf("insights health case activity: %w", err)
	}

	// Card 10: feed_activity. Bounded to the last 4h of feed_direction_completions, shed grain:
	// a shed counts once if it was fed AND has at least one live mapped tag.
	err = r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(DISTINCT fdc.shed_id)
		FROM public.feed_direction_completions fdc
		WHERE fdc.tenant_id = $1
		  AND fdc.status IN ('recorded', 'accepted')
		  AND fdc.fed_at >= now() - interval '4 hours'
		  AND EXISTS (
		    SELECT 1
		    FROM public.herd_signal_tag_latest tl
		    %s
		    WHERE tl.tenant_id = fdc.tenant_id AND g.shed_id = fdc.shed_id
		  )
	`, tagLocationJoin), tenantID).Scan(&d.FeedActivityShedsCount)
	if err != nil {
		return d, fmt.Errorf("insights feed activity: %w", err)
	}

	// Card 11: weight_activity. Bounded to the last 24h of weighing_observations, joined to a
	// mapped tag by animal_id.
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT wo.animal_id)
		FROM public.weighing_observations wo
		JOIN public.goat_identifiers gi
		  ON gi.tenant_id = wo.tenant_id AND gi.goat_id = wo.animal_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = wo.tenant_id
		  AND (tl.tag_id = gi.normalized_value OR tl.tag_mac = gi.normalized_value)
		WHERE wo.tenant_id = $1
		  AND wo.animal_id IS NOT NULL
		  AND wo.accepted_at >= now() - interval '24 hours'
	`, tenantID).Scan(&d.WeightActivityTagsCount)
	if err != nil {
		return d, fmt.Errorf("insights weight activity: %w", err)
	}

	return d, nil
}

// upsertActivityWindows rolls up packets into time-bucketed activity windows.
// Idempotent: replayed packets will be counted only once (same motion_count and timestamp).
// Out-of-order safe: uses first/last motion counts from the bucket, not arrival order.
func (r *Repository) upsertActivityWindowsTx(ctx context.Context, tx pgx.Tx, tenantID string, packets []domain.Packet, bucketSeconds int) error {
	if len(packets) == 0 {
		return nil
	}

	// Group packets by tag_id and bucket_start
	type bucketKey struct {
		tagID       string
		bucketStart time.Time
	}
	type bucketData struct {
		firstMotionCount *int64
		lastMotionCount  *int64
		rssiValues       []*int16
		firstSeenAt      time.Time
		lastSeenAt       time.Time
		packetCount      int
	}

	buckets := make(map[bucketKey]*bucketData)
	bucketDuration := time.Duration(bucketSeconds) * time.Second

	for _, p := range packets {
		// Truncate received_at to bucket boundary
		bucketStart := p.ReceivedAt.Truncate(bucketDuration)
		key := bucketKey{tagID: p.TagID, bucketStart: bucketStart}

		if _, exists := buckets[key]; !exists {
			buckets[key] = &bucketData{
				firstSeenAt: p.ReceivedAt,
				lastSeenAt:  p.ReceivedAt,
			}
		}

		bd := buckets[key]
		bd.packetCount++
		bd.lastSeenAt = p.ReceivedAt // Always update to latest packet time

		// Track first/last motion count (for out-of-order safety)
		if bd.firstMotionCount == nil && p.MotionCount != nil {
			bd.firstMotionCount = p.MotionCount
		}
		if p.MotionCount != nil {
			// Update last_motion_count: always use the one with the latest timestamp
			if bd.lastMotionCount == nil || p.ReceivedAt.After(buckets[key].lastSeenAt.Add(-time.Duration(bucketSeconds)*time.Second)) {
				bd.lastMotionCount = p.MotionCount
			}
		}

		if p.RSSIdbm != nil {
			bd.rssiValues = append(bd.rssiValues, p.RSSIdbm)
		}
	}

	// Upsert each bucket
	batch := &pgx.Batch{}
	for key, bd := range buckets {
		// Compute motion_delta
		var motionDelta int64
		if bd.firstMotionCount != nil && bd.lastMotionCount != nil {
			if *bd.lastMotionCount >= *bd.firstMotionCount {
				motionDelta = *bd.lastMotionCount - *bd.firstMotionCount
			}
			// If counter reset (lastMotionCount < firstMotionCount), delta = 0
		}

		// Compute RSSI statistics
		var avgRSSI *float64
		var minRSSI *int16
		var maxRSSI *int16

		if len(bd.rssiValues) > 0 {
			sum := float64(0)
			min := *bd.rssiValues[0]
			max := *bd.rssiValues[0]
			for _, rssi := range bd.rssiValues {
				if rssi != nil {
					sum += float64(*rssi)
					if *rssi < min {
						min = *rssi
					}
					if *rssi > max {
						max = *rssi
					}
				}
			}
			avg := sum / float64(len(bd.rssiValues))
			avgRSSI = &avg
			minRSSI = &min
			maxRSSI = &max
		}

		// Upsert query: on conflict, update counts and RSSI stats
		// Important: use ON CONFLICT DO UPDATE to handle replayed packets idempotently
		batch.Queue(`
			INSERT INTO public.herd_signal_activity_windows (
				tenant_id, tag_id, bucket_start, bucket_seconds,
				first_motion_count, last_motion_count, motion_delta,
				packet_count, avg_rssi_dbm, min_rssi_dbm, max_rssi_dbm,
				first_seen_at, last_seen_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO UPDATE
			SET last_motion_count = COALESCE($6, EXCLUDED.last_motion_count),
			    motion_delta = $7,
			    packet_count = packet_count + EXCLUDED.packet_count,
			    avg_rssi_dbm = COALESCE($9, avg_rssi_dbm),
			    min_rssi_dbm = CASE WHEN $10 IS NULL THEN min_rssi_dbm
			                         WHEN min_rssi_dbm IS NULL THEN $10
			                         ELSE LEAST(min_rssi_dbm, $10) END,
			    max_rssi_dbm = CASE WHEN $11 IS NULL THEN max_rssi_dbm
			                         WHEN max_rssi_dbm IS NULL THEN $11
			                         ELSE GREATEST(max_rssi_dbm, $11) END,
			    last_seen_at = $13
		`,
			tenantID, key.tagID, key.bucketStart, bucketSeconds,
			bd.firstMotionCount, bd.lastMotionCount, motionDelta,
			bd.packetCount, avgRSSI, minRSSI, maxRSSI,
			bd.firstSeenAt, bd.lastSeenAt,
		)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(buckets); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("upsert bucket %d: %w", i, err)
		}
	}

	return nil
}
