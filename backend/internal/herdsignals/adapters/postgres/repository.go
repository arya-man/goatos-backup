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

// IngestPackets stores raw packets and updates tag latest state.
// Returns (stored count, latest_updated count, error).
func (r *Repository) IngestPackets(ctx context.Context, tenantID string, packets []domain.Packet) (int, int, error) {
	if len(packets) == 0 {
		return 0, 0, nil
	}

	stored := 0
	latestUpdated := 0

	// Insert packets in batch
	batch := &pgx.Batch{}
	for _, p := range packets {
		batch.Queue(`
			INSERT INTO public.herd_signal_packets (
				tenant_id, gateway_id, source, tag_id, tag_mac, received_at,
				gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c,
				motion_count, sensor_state, temperature_sensor_ok,
				accelerometer_sensor_ok, raw_adv, raw_payload
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		`,
			tenantID, p.GatewayID, p.Source, p.TagID, p.TagMAC, p.ReceivedAt,
			p.GatewaySeenAt, p.RSSIdbm, p.BatteryMV, p.TagTemperatureC,
			p.MotionCount, p.SensorState, p.TemperatureSensorOK,
			p.AccelerometerSensorOK, p.RawAdv, p.RawPayload,
		)
	}

	results := r.db.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(packets); i++ {
		ct, err := results.Exec()
		if err != nil {
			return stored, latestUpdated, fmt.Errorf("insert packet %d: %w", i, err)
		}
		if ct.RowsAffected() > 0 {
			stored++
		}
	}

	// Update tag latest state for each unique tag
	// Group packets by tag_id to compute deltas
	tagPackets := make(map[string][]domain.Packet)
	for _, p := range packets {
		tagPackets[p.TagID] = append(tagPackets[p.TagID], p)
	}

	for tagID, tagPkts := range tagPackets {
		if err := r.updateTagLatest(ctx, tenantID, tagID, tagPkts); err != nil {
			return stored, latestUpdated, err
		}
		latestUpdated++
	}

	return stored, latestUpdated, nil
}

// updateTagLatest updates the latest state for a single tag based on new packets.
func (r *Repository) updateTagLatest(ctx context.Context, tenantID, tagID string, packets []domain.Packet) error {
	if len(packets) == 0 {
		return nil
	}

	// Find the most recent packet for this tag
	latestPkt := packets[0]
	for i := 1; i < len(packets); i++ {
		if packets[i].ReceivedAt.After(latestPkt.ReceivedAt) {
			latestPkt = packets[i]
		}
	}

	query := `
		INSERT INTO public.herd_signal_tag_latest (
			tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
			last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
			motion_count, motion_delta, previous_motion_count, previous_seen_at,
			motion_window_seconds, movement_state, temperature_sensor_ok,
			accelerometer_sensor_ok, mapping_state, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, now())
		ON CONFLICT (tenant_id, tag_id) DO UPDATE
		SET tag_mac = COALESCE($3, tag_mac),
		    gateway_id = COALESCE($4, gateway_id),
		    source = COALESCE($5, source),
		    last_seen_at = $6,
		    last_rssi_dbm = COALESCE($7, last_rssi_dbm),
		    signal_state = $8,
		    battery_mv = COALESCE($9, battery_mv),
		    battery_state = $10,
		    tag_temperature_c = COALESCE($11, tag_temperature_c),
		    motion_count = COALESCE($12, motion_count),
		    motion_delta = $13,
		    previous_motion_count = CASE WHEN motion_count != $12 THEN motion_count ELSE previous_motion_count END,
		    previous_seen_at = CASE WHEN motion_count != $12 THEN last_seen_at ELSE previous_seen_at END,
		    motion_window_seconds = COALESCE($16, motion_window_seconds),
		    movement_state = $17,
		    temperature_sensor_ok = COALESCE($18, temperature_sensor_ok),
		    accelerometer_sensor_ok = COALESCE($19, accelerometer_sensor_ok),
		    updated_at = now()
	`

	// TODO: Compute motion_delta, signal_state, battery_state based on thresholds

	_, err := r.db.Exec(ctx, query,
		tenantID, tagID, latestPkt.TagMAC, latestPkt.GatewayID, latestPkt.Source, latestPkt.ReceivedAt,
		latestPkt.RSSIdbm, "ok", // signal_state
		latestPkt.BatteryMV, "ok", // battery_state
		latestPkt.TagTemperatureC,
		latestPkt.MotionCount, 0, // motion_delta
		nil, nil, // previous_motion_count, previous_seen_at
		60, "unknown", // motion_window_seconds, movement_state
		latestPkt.TemperatureSensorOK, latestPkt.AccelerometerSensorOK, "unmapped",
	)
	return err
}

// GetTagLatest fetches the current state of a single tag.
func (r *Repository) GetTagLatest(ctx context.Context, tenantID, tagID string) (*domain.TagLatest, error) {
	query := `
		SELECT tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
		       last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
		       motion_count, motion_delta, previous_motion_count, previous_seen_at,
		       motion_window_seconds, movement_state, temperature_sensor_ok,
		       accelerometer_sensor_ok, mapping_state, updated_at
		FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1 AND tag_id = $2
	`
	var tag domain.TagLatest
	err := r.db.QueryRow(ctx, query, tenantID, tagID).Scan(
		&tag.TenantID, &tag.TagID, &tag.TagMAC, &tag.GatewayID, &tag.Source, &tag.LastSeenAt,
		&tag.LastRSSIdbm, &tag.SignalState, &tag.BatteryMV, &tag.BatteryState, &tag.TagTemperatureC,
		&tag.MotionCount, &tag.MotionDelta, &tag.PreviousMotionCount, &tag.PreviousSeenAt,
		&tag.MotionWindowSeconds, &tag.MovementState, &tag.TemperatureSensorOK,
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
func (r *Repository) ListTagsLatest(ctx context.Context, tenantID string, parkID, shedID, movementState *string, mapped *bool, cursor string, limit int) (
	[]domain.TagLatest, domain.Summary, *string, error,
) {
	// Build WHERE clause
	whereClause := "WHERE tenant_id = $1"
	args := []interface{}{tenantID}
	argIndex := 2

	if parkID != nil && *parkID != "" {
		whereClause += fmt.Sprintf(" AND park_id = $%d", argIndex)
		args = append(args, *parkID)
		argIndex++
	}

	if shedID != nil && *shedID != "" {
		whereClause += fmt.Sprintf(" AND shed_id = $%d", argIndex)
		args = append(args, *shedID)
		argIndex++
	}

	if movementState != nil && *movementState != "" {
		whereClause += fmt.Sprintf(" AND movement_state = $%d", argIndex)
		args = append(args, *movementState)
		argIndex++
	}

	if mapped != nil {
		if *mapped {
			whereClause += " AND mapping_state = 'mapped'"
		} else {
			whereClause += " AND mapping_state = 'unmapped'"
		}
	}

	if cursor != "" {
		whereClause += fmt.Sprintf(" AND (last_seen_at, tag_id) < (SELECT last_seen_at, tag_id FROM public.herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = $%d)", argIndex)
		args = append(args, cursor)
		argIndex++
	}

	// Fetch tags
	query := fmt.Sprintf(`
		SELECT tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
		       last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
		       motion_count, motion_delta, previous_motion_count, previous_seen_at,
		       motion_window_seconds, movement_state, temperature_sensor_ok,
		       accelerometer_sensor_ok, mapping_state, updated_at
		FROM public.herd_signal_tag_latest
		%s
		ORDER BY last_seen_at DESC, tag_id DESC
		LIMIT $%d
	`, whereClause, argIndex)
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
			&tag.MotionWindowSeconds, &tag.MovementState, &tag.TemperatureSensorOK,
			&tag.AccelerometerSensorOK, &tag.MappingState, &tag.UpdatedAt,
		); err != nil {
			return nil, domain.Summary{}, nil, err
		}
		tags = append(tags, tag)
	}

	// Check if there are more
	var nextCursor *string
	if len(tags) > limit {
		tags = tags[:limit]
		lastTag := tags[len(tags)-1]
		nextCursor = &lastTag.TagID
	}

	// Compute summary
	summary := r.computeSummary(ctx, tenantID, &whereClause, args[:len(args)-1]) // Exclude limit arg

	return tags, summary, nextCursor, nil
}

// computeSummary computes summary counts.
func (r *Repository) computeSummary(ctx context.Context, tenantID string, whereClause *string, args []interface{}) domain.Summary {
	// For now, return empty summary. TODO: Implement full aggregation
	return domain.Summary{}
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
