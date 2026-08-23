package postgres

import (
	"context"
	"fmt"
	"strings"
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
			wifi_mac, ble_mac, network_mode, status, last_seen_at, last_pkt_sn, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::bigint, now())
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
		// $12 -- the report sequence number. The query references it three times for
		// loss/reboot detection; without it every ingest failed with
		// "could not determine data type of parameter $12" (SQLSTATE 42P08).
		gw.LastPktSN,
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
		    -- pkt_sn accounting (000198). This is the ONLY packet-loss instrument the gateway
		    -- protocol gives us. A FORWARD jump means reports we never received: missed =
		    -- new - last - 1, accrued into packets_missed_total. A DECREASE means the GATEWAY
		    -- REBOOTED (its counter restarted) -- exactly the motion_count reset case -- so it
		    -- counts a reboot and re-anchors, and NEVER subtracts: loss can never go negative.
		    packets_missed_total = public.herd_signal_gateways.packets_missed_total
		        + CASE WHEN $12::bigint IS NOT NULL
		                AND public.herd_signal_gateways.last_pkt_sn IS NOT NULL
		                AND $12::bigint > public.herd_signal_gateways.last_pkt_sn + 1
		               THEN $12::bigint - public.herd_signal_gateways.last_pkt_sn - 1 ELSE 0 END,
		    pkt_sn_reboot_count = public.herd_signal_gateways.pkt_sn_reboot_count
		        + CASE WHEN $12::bigint IS NOT NULL
		                AND public.herd_signal_gateways.last_pkt_sn IS NOT NULL
		                AND $12::bigint < public.herd_signal_gateways.last_pkt_sn
		               THEN 1 ELSE 0 END,
		    last_pkt_sn = COALESCE($12::bigint, public.herd_signal_gateways.last_pkt_sn),
		    updated_at = now()
	`,
		tenantID, gw.GatewayID, gw.Label, gw.ParkID, gw.ShedID, gw.LocationID,
		gw.WifiMAC, gw.BLEMAC, gw.NetworkMode, gw.Status, gw.LastSeenAt, gw.LastPktSN,
	); err != nil {
		return 0, 0, fmt.Errorf("upsert gateway: %w", err)
	}

	stored := 0

	batch := &pgx.Batch{}
	for _, p := range packets {
		// received_date buckets received_at (server-stamped, see 000198) to its UTC calendar day.
		// It is herd_signal_packets' partition key (000200) and, deliberately, NOT a generated
		// column: PostgreSQL disallows a generated column as a partition key, and a BEFORE INSERT
		// trigger cannot populate it either -- partition routing happens before row triggers fire,
		// so a trigger-written value that disagrees with the routed partition is rejected
		// ("moving row to another partition during a BEFORE FOR EACH ROW trigger is not
		// supported", verified on a scratch database while building 000200). The application must
		// supply it explicitly, computed from the SAME p.ReceivedAt used for the dedup key below,
		// so routing and dedup identity never disagree about which day a packet belongs to.
		// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=partition-key-stable-timezone-independent expiry=2027-12-31
		receivedDate := p.ReceivedAt.UTC().Truncate(24 * time.Hour)
		batch.Queue(`
			INSERT INTO public.herd_signal_packets (
				tenant_id, gateway_id, source, tag_id, tag_mac, received_at, received_date,
				device_seen_at, gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c,
				motion_count, sensor_state, temperature_sensor_ok,
				accelerometer_sensor_ok, pkt_sn, raw_adv, raw_payload
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
			-- Dedup identity is (tenant_id, tag_id, device_seen_at, motion_count) (000196);
			-- received_date is in the index ONLY because PostgreSQL requires the partition key
			-- column in every unique index on a partitioned table (000200). received_at itself is
			-- server-stamped fresh per ingest call (security fix, 000198), so it cannot be part of
			-- the dedup key -- a retried batch would get a NEW received_at and never collide.
			-- received_date, bucketed to the UTC calendar day, still collides correctly for the
			-- ordinary retry case (seconds to low minutes later, same day); only a retry that
			-- straddles UTC midnight escapes this index (see 000200's migration header).
			ON CONFLICT (tenant_id, tag_id, device_seen_at, motion_count, received_date) WHERE device_seen_at IS NOT NULL DO NOTHING
		`,
			tenantID, p.GatewayID, p.Source, p.TagID, p.TagMAC, p.ReceivedAt, receivedDate,
			p.DeviceSeenAt, p.GatewaySeenAt, p.RSSIdbm, p.BatteryMV, p.TagTemperatureC,
			p.MotionCount, p.SensorState, p.TemperatureSensorOK,
			p.AccelerometerSensorOK, p.PktSN, p.RawAdv, p.RawPayload,
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
		// scale-guard:ignore: pgx.SendBatch result iteration; all statements are pre-batched, not individual round-trips
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

	// Maintainer decision on offline behaviour: the gateway does not buffer scan reports through
	// a WAN outage, so a reception gap this long means we received NOTHING for that period, and
	// this delta is a TOTAL over the gap with unknown time distribution, not ordinary movement.
	// received_at (server clock) is the ONLY input -- never gateway_seen_at, which is the
	// gateway's own uncorrected clock and would fabricate or hide gaps.
	gapDelta := domain.IsGapDelta(previousSeenAt, latestPkt.ReceivedAt, thresholds)
	// The TOTAL across the gap: current cumulative minus the last one we saw, floored at 0 on a
	// counter reset (domain.MotionDelta's existing guard, reused rather than duplicated). This is
	// distinct from the reconnect bucket's own naturally-computed intra-bucket delta (which is 0,
	// or close to it, since the bucket usually holds only the single reconnect packet) -- the
	// bucket's row is overwritten with THIS total below, specifically because attributing it to
	// "first packet in bucket minus itself" would silently drop the whole point of gap_delta.
	rawDelta, _ := domain.MotionDelta(latestPkt.MotionCount, previousMotionCount)

	if gapDelta {
		// Flag the RECONNECT bucket -- the bucket containing latestPkt.ReceivedAt -- in every
		// tier's activity-window row, AND overwrite its motion_delta with rawDelta (the TOTAL
		// across the gap). This never smears the gap across the buckets it spans: those buckets
		// simply have no row (packets never arrived for them, so upsertActivityWindowsTx never
		// wrote them), which the timeline already renders as is_gap=true. Only the ONE bucket
		// that actually received the reconnect packet carries the lump.
		//
		// Overwriting motion_delta here is deliberate, not a bug to "fix" later: the bucket's own
		// naturally-computed intra-bucket delta (last packet in bucket minus first packet in
		// bucket, from upsertActivityWindowsTx above) is 0 or near-0, because the bucket usually
		// holds only this single reconnect packet -- comparing it to itself cannot see the gap.
		// rawDelta compares against the tag's last KNOWN state before the gap, which is the only
		// number that actually represents "how much movement happened somewhere in that hole".
		// first_motion_count/last_motion_count are left as whatever this bucket's own packets
		// produced (not rewritten to match) -- they remain "what this bucket observed", while
		// motion_delta on a gap_delta row means something different ("what accrued since we last
		// heard from this tag"). That asymmetry is intentional.
		for _, bucketSeconds := range activityWindowTiers {
			bucketStart := latestPkt.ReceivedAt.Truncate(time.Duration(bucketSeconds) * time.Second)
			// scale-guard:ignore: 3 = fixed bound; activityWindowTiers = [60s, 300s, 3600s] per migration 000197, domain.SupportedBucketSeconds
			if _, err := tx.Exec(ctx, `
				UPDATE public.herd_signal_activity_windows
				SET gap_delta = true, motion_delta = $5
				WHERE tenant_id = $1 AND tag_id = $2 AND bucket_start = $3 AND bucket_seconds = $4
			`, tenantID, tagID, bucketStart, bucketSeconds, rawDelta); err != nil {
				return false, fmt.Errorf("flag reconnect bucket (%ds): %w", bucketSeconds, err)
			}
		}
	}

	// Tag->animal mapping is resolved on ingest so live/timeline reads never pay for the join.
	// Re-resolved on every ingest (cheap point lookup) rather than only once, since
	// smart_tag_capable / identifier status can change after the tag was first seen.
	//
	// Resolved HERE, before the history load below, because the monitoring boundary it carries
	// decides how far back that history is allowed to reach.
	mapping, _, err = resolveTagMapping(ctx, tx, tenantID, &tagID, latestPkt.TagMAC)
	if err != nil {
		return false, fmt.Errorf("resolve tag mapping: %w", err)
	}

	// THE MONITORING BOUNDARY (migration 000197). A tag is commissioned, powered up and
	// broadcasting long before it is attached to an animal; on staging none of them are mapped at
	// all. Everything emitted before the mapping instant is telemetry ABOUT A DEVICE -- someone
	// carrying it in a pocket, jostling a bench, driving it to a farm -- and must never be blended
	// into an animal's history. NULL means unmapped: device telemetry only, and no
	// animal-attributed value may be produced for the tag at all.
	monitoringSince, err := monitoringBoundaryTx(ctx, tx, tenantID, tagID, latestPkt.TagMAC)
	if err != nil {
		return false, err
	}

	// movement_state and pattern_state are computed over history, not the single ingest-batch
	// delta (AGENTS.md: compare like grain to like grain). Pull the trailing 15-minute window
	// from the 60s tier (already upserted above in this same tx) for the movement-state delta,
	// and the trailing 24h of 300s-tier windows for the pattern-state baseline.
	//
	// motion_delta is stored as THIS 15-minute windowDelta, not the raw ingest-batch-vs-previous-
	// snapshot delta (defect 4 / scale review M5): the old code computed `delta` from a single
	// point-to-point comparison that could span 5 seconds or 3 days depending on ingest cadence,
	// wrote motion_window_seconds=900 alongside it anyway, and then served that same number
	// again as motion_delta_1h -- two differently-named fields with an identical, mislabeled
	// value. motion_delta_1h below is now a REAL 1-hour (3600s-tier) sum.
	windowDelta, err := r.sumActivityWindowDeltaTx(ctx, tx, tenantID, tagID, 60, latestPkt.ReceivedAt.Add(-15*time.Minute), latestPkt.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("sum 15m activity window delta: %w", err)
	}
	hourDelta, err := r.sumActivityWindowDeltaTx(ctx, tx, tenantID, tagID, 3600, latestPkt.ReceivedAt.Add(-time.Hour), latestPkt.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("sum 1h activity window delta: %w", err)
	}
	movementState := domain.MovementStateFromDelta(windowDelta, 900, thresholds)
	// Stale overrides an otherwise-computed movement state: no packet in 30+ minutes.
	if time.Since(latestPkt.ReceivedAt) > time.Duration(thresholds.StalePacketMinutes)*time.Minute {
		movementState = "stale"
	}

	// The pattern window (quiet_watch / inactive / spike, and the p75 baseline the spike
	// comparison uses) is an ANIMAL-attributed judgement, so it may not look back past the
	// mapping instant. Bench movement from a tag rattling in a box must never enter a real
	// animal's baseline. For an unmapped tag the boundary is NULL and the window is unchanged --
	// there is no animal to attribute anything to, and the device's own history is legitimately
	// its own.
	historyFrom := latestPkt.ReceivedAt.Add(-24 * time.Hour)
	if monitoringSince != nil && monitoringSince.After(historyFrom) {
		historyFrom = *monitoringSince
	}
	history, err := r.listActivityWindowsTx(ctx, tx, tenantID, tagID, 300, historyFrom, latestPkt.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("load 24h pattern history: %w", err)
	}
	seenAt := latestPkt.ReceivedAt
	// PatternStateFromHistory computes elapsed time since last packet for staleness/missing-signal detection.
	// This is elapsed-time comparison (now.Sub(*lastPacketAt)), not a date boundary, so UTC is correct.
	// india-date-guard:ignore: owner=ravi issue=herd-signals-phase1 scope=elapsed-time-for-staleness-detection expiry=2026-12-31
	patternStateComputed := domain.PatternStateFromHistory(windowDelta, &seenAt, time.Now().UTC(), history, previousPattern, gapDelta, thresholds)

	signalState := domain.SignalStateFromRSSI(latestPkt.RSSIdbm, nil, thresholds)
	batteryState := domain.BatteryStateFromVoltage(latestPkt.BatteryMV, thresholds)

	_, err = tx.Exec(ctx, `
		INSERT INTO public.herd_signal_tag_latest (
			tenant_id, tag_id, tag_mac, gateway_id, source, last_seen_at,
			last_rssi_dbm, signal_state, battery_mv, battery_state, tag_temperature_c,
			motion_count, motion_delta, motion_delta_1h, previous_motion_count, previous_seen_at,
			motion_window_seconds, movement_state, pattern_state, temperature_sensor_ok,
			accelerometer_sensor_ok, mapping_state, gap_delta, animal_monitoring_since, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, now())
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
		    motion_delta_1h = $14,
		    previous_motion_count = $15,
		    previous_seen_at = $16,
		    motion_window_seconds = $17,
		    movement_state = $18,
		    pattern_state = $19,
		    temperature_sensor_ok = COALESCE($20, public.herd_signal_tag_latest.temperature_sensor_ok),
		    accelerometer_sensor_ok = COALESCE($21, public.herd_signal_tag_latest.accelerometer_sensor_ok),
		    mapping_state = $22,
		    gap_delta = $23,
		    -- Denormalised from goat_identifiers.smart_tag_mapped_at so the hot read path never
		    -- joins to decide whether a number may be attributed to an animal. Assigned, not
		    -- COALESCEd: an UNMAP must be able to push this back to NULL.
		    animal_monitoring_since = $24,
		    updated_at = now()
	`,
		tenantID, tagID, latestPkt.TagMAC, latestPkt.GatewayID, latestPkt.Source, latestPkt.ReceivedAt,
		latestPkt.RSSIdbm, signalState,
		latestPkt.BatteryMV, batteryState,
		latestPkt.TagTemperatureC,
		latestPkt.MotionCount, windowDelta, hourDelta,
		previousMotionCount, previousSeenAt,
		900, movementState,
		patternStateComputed,
		latestPkt.TemperatureSensorOK, latestPkt.AccelerometerSensorOK, mapping, gapDelta,
		monitoringSince,
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
		SELECT bucket_start, bucket_seconds, motion_delta, packet_count, gap_delta
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
		if err := rows.Scan(&w.BucketStart, &w.BucketSeconds, &w.MotionDelta, &w.PacketCount, &w.GapDelta); err != nil {
			return nil, err
		}
		w.IsGap = w.PacketCount == 0
		windows = append(windows, w)
	}
	return windows, rows.Err()
}

// GetTagLatest fetches the current state of a single tag.
func (r *Repository) GetTagLatest(ctx context.Context, tenantID, tagID string) (*domain.TagLatest, error) {
	// movement_state/pattern_state computed at READ time from last_seen_at freshness (defect 1),
	// same as ListTagsLatest -- aliased `tl` so effectiveMovementStateExpr/
	// effectivePatternStateExpr apply unchanged.
	query := `
		SELECT tl.tenant_id, tl.tag_id, tl.tag_mac, tl.gateway_id, tl.source, tl.last_seen_at,
		       tl.last_rssi_dbm, tl.signal_state, tl.battery_mv, tl.battery_state, tl.tag_temperature_c,
		       tl.motion_count, tl.motion_delta, tl.motion_delta_1h, tl.previous_motion_count, tl.previous_seen_at,
		       tl.motion_window_seconds, ` + effectiveMovementStateExpr + `, ` + effectivePatternStateExpr + `, tl.temperature_sensor_ok,
		       tl.accelerometer_sensor_ok, tl.mapping_state, tl.gap_delta, tl.updated_at
		FROM public.herd_signal_tag_latest tl
		WHERE tl.tenant_id = $1 AND tl.tag_id = $2
	`
	var tag domain.TagLatest
	err := r.db.QueryRow(ctx, query, tenantID, tagID).Scan(
		&tag.TenantID, &tag.TagID, &tag.TagMAC, &tag.GatewayID, &tag.Source, &tag.LastSeenAt,
		&tag.LastRSSIdbm, &tag.SignalState, &tag.BatteryMV, &tag.BatteryState, &tag.TagTemperatureC,
		&tag.MotionCount, &tag.MotionDelta, &tag.MotionDelta1h, &tag.PreviousMotionCount, &tag.PreviousSeenAt,
		&tag.MotionWindowSeconds, &tag.MovementState, &tag.PatternState, &tag.TemperatureSensorOK,
		&tag.AccelerometerSensorOK, &tag.MappingState, &tag.GapDelta, &tag.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// effectiveMovementStateExpr / effectivePatternStateExpr compute movement_state/pattern_state at
// READ TIME from last_seen_at freshness, rather than trusting the column written at the tag's
// LAST INGEST (maintainer correctness review, defect 1 -- "stale and missing can never fire").
//
// A tag that stops transmitting never gets another ingest, so a write-time-only movement_state/
// pattern_state keeps whatever it was computed as on the last packet FOREVER: a tag last seen a
// week ago still reads whatever it read a week ago, never "stale"/"missing". Confirmed against
// the OCI database: all 20 tags carried a stale write-time state despite hours-old last_seen_at.
// 30 minutes matches domain.DefaultThresholds().StalePacketMinutes /
// .MissingSignalMinutes (both 30, provisional); duplicated here as a literal because this
// package has no DB-level access to the domain Thresholds value without threading it through
// every query, and the interval only needs to change if that provisional default does.
const staleAfterInterval = "interval '30 minutes'"

var effectiveMovementStateExpr = "CASE WHEN now() - tl.last_seen_at > " + staleAfterInterval + " THEN 'stale' ELSE tl.movement_state END"
var effectivePatternStateExpr = "CASE WHEN now() - tl.last_seen_at > " + staleAfterInterval + " THEN 'missing' ELSE tl.pattern_state END"

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
		  -- defect 2 (normalization): tl.tag_id/tl.tag_mac are raw device strings (a BLE MAC
		  -- conventionally arrives lowercase); goat_identifiers.normalized_value is already
		  -- UPPER(TRIM(...)) per the identity module's canonical normalizer. Wrap the small,
		  -- already-tenant-narrowed tl side rather than the indexed gi.normalized_value column,
		  -- so the (tenant_id, normalized_value) index on goat_identifiers stays usable.
		  AND gi.normalized_value IN (UPPER(BTRIM(tl.tag_id)), UPPER(BTRIM(COALESCE(tl.tag_mac, ''))))
		LIMIT 1
	) mapped_goat ON true
	LEFT JOIN public.goats g ON g.tenant_id = tl.tenant_id AND g.goat_id = mapped_goat.goat_id
	LEFT JOIN public.locations shed_loc ON shed_loc.tenant_id = tl.tenant_id AND shed_loc.location_id = g.shed_id
`

// ListTagsLatest fetches tags with optional filters, keyset pagination, and a whole-filter
// server-side summary aggregate (never summed from the returned page -- AGENTS.md operational
// read model contract rule 3).
// herdSignalsLiveFilter builds the shared WHERE clause + args for GET /herd-signals/live's row
// query AND its summary aggregate, so the two can never drift (AGENTS.md operational read
// model contract rule 3: summary must be a whole-filter aggregate, never derived from the page).
// movementState is nil for the summary call: the summary reports the BREAKDOWN across movement
// states for the current park/shed/mapping/pattern/q filter, so movement_state itself must not
// also be a predicate.
func herdSignalsLiveFilter(tenantID string, parkID, shedID, movementState, mappingState, pattern, q *string) (string, []interface{}, int) {
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
		whereClause += fmt.Sprintf(" AND ("+effectiveMovementStateExpr+") = $%d", argIndex)
		args = append(args, *movementState)
		argIndex++
	}

	if mappingState != nil && *mappingState != "" {
		whereClause += fmt.Sprintf(" AND tl.mapping_state = $%d", argIndex)
		args = append(args, *mappingState)
		argIndex++
	}

	if pattern != nil && *pattern != "" {
		// "not_normal" is a sentinel, not a literal pattern_state value: it is the whole-fleet
		// "alerting" partition the Alerts tab needs server-side (a page can be 10k+ rows and
		// entirely non-alerting, so selecting the alerting subset client-side from one fetched
		// page is wrong at scale — see herd-signals-board.tsx AlertsTab). Every other value is
		// still an exact pattern_state match.
		//
		// The partition is NOT simply "pattern_state <> normal": bare no_movement (delta 0 in the
		// CURRENT 15-minute window) is the ordinary state of a resting animal and is not itself
		// alert-worthy (mock/herd-signals-mock.html renderAlerts never emits a row for it alone —
		// only the duration-based inactive/quiet_watch patterns, a spike, or a recovery do), so it
		// is excluded here. Conversely pattern_state alone cannot see a weak radio, a low/critical
		// battery, an abnormal accelerometer, or a mapping conflict — those are independent per-tag
		// conditions the Alerts tab must also surface (herd-signals-board.tsx buildAlertConditions),
		// so they are OR'd in explicitly rather than left for pattern_state to (fail to) express.
		if *pattern == "not_normal" {
			whereClause += " AND ((" + effectivePatternStateExpr + ") NOT IN ('normal', 'no_movement')" +
				" OR tl.signal_state = 'weak'" +
				" OR tl.battery_state IN ('low', 'critical')" +
				" OR tl.accelerometer_sensor_ok IS FALSE" +
				" OR tl.mapping_state = 'conflict')"
		} else {
			whereClause += fmt.Sprintf(" AND ("+effectivePatternStateExpr+") = $%d", argIndex)
			args = append(args, *pattern)
			argIndex++
		}
	}

	if q != nil && strings.TrimSpace(*q) != "" {
		needle := "%" + strings.TrimSpace(*q) + "%"
		whereClause += fmt.Sprintf(` AND (
			tl.tag_id ILIKE $%d OR tl.tag_mac ILIKE $%d OR tl.gateway_id ILIKE $%d
			OR g.display_id ILIKE $%d OR shed_loc.name ILIKE $%d
		)`, argIndex, argIndex, argIndex, argIndex, argIndex)
		args = append(args, needle)
		argIndex++
	}

	return whereClause, args, argIndex
}

func (r *Repository) ListTagsLatest(ctx context.Context, tenantID string, parkID, shedID, movementState, mappingState, pattern, q *string, cursor string, limit int) (
	[]domain.TagLatest, domain.Summary, *string, error,
) {
	// Fetch one extra row to detect whether another page exists. The row query itself lives in
	// ListTagsLatestPage (export.go) so GET /herd-signals/export.csv walks the SAME filtered,
	// keyset-ordered result this endpoint returns -- the export can never drift from the view.
	tags, err := r.ListTagsLatestPage(ctx, tenantID, parkID, shedID, movementState, mappingState, pattern, q, cursor, limit+1)
	if err != nil {
		return nil, domain.Summary{}, nil, err
	}

	var nextCursor *string
	if len(tags) > limit {
		tags = tags[:limit]
		lastTag := tags[len(tags)-1]
		nextCursor = &lastTag.TagID
	}

	// Summary: the SAME filter (park/shed/mapping_state/pattern/q), WITHOUT the movement_state
	// predicate or the cursor/limit, aggregated server-side in one query -- never derived from
	// the returned page (AGENTS.md operational read model contract rule 3).
	summaryWhere, summaryArgs, _ := herdSignalsLiveFilter(tenantID, parkID, shedID, nil, mappingState, pattern, q)

	summary, err := r.computeSummary(ctx, tagLocationJoin, summaryWhere, summaryArgs)
	if err != nil {
		return nil, domain.Summary{}, nil, err
	}

	return tags, summary, nextCursor, nil
}

// computeSummary computes the whole-filter aggregate counts in a single bounded query.
func (r *Repository) computeSummary(ctx context.Context, join, whereClause string, args []interface{}) (domain.Summary, error) {
	// movement_state is read-time-effective (defect 1: a tag that stopped transmitting must
	// count as stale/missing here, not whatever it read at its last ingest) -- all rows are
	// packet-derived, so this counts unmapped tags exactly like mapped ones; only
	// mapped_animals/unmapped_tags themselves are mapping-derived.
	query := fmt.Sprintf(`
		SELECT count(*) FILTER (WHERE true),
		       count(*) FILTER (WHERE tl.mapping_state = 'mapped'),
		       count(*) FILTER (WHERE tl.mapping_state = 'unmapped'),
		       count(*) FILTER (WHERE (`+effectiveMovementStateExpr+`) = 'moving'),
		       count(*) FILTER (WHERE (`+effectiveMovementStateExpr+`) = 'quiet'),
		       count(*) FILTER (WHERE (`+effectiveMovementStateExpr+`) = 'not_moving'),
		       count(*) FILTER (WHERE (`+effectiveMovementStateExpr+`) = 'stale'),
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
		       min_rssi_dbm, max_rssi_dbm, first_seen_at, last_seen_at, gap_delta
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
			&w.MinRSSIdbm, &w.MaxRSSIdbm, &w.FirstSeenAt, &w.LastSeenAt, &w.GapDelta,
		); err != nil {
			return nil, err
		}
		// A bucket with no packets is a HOLE, not a quiet animal. The activity correlation refuses
		// to compute a before/after comparison across one, so this flag has to be set here as well
		// as on the transactional read -- it was set only there, so the correlation path could never
		// see a gap and would happily return a percentage computed over missing data. That is the
		// exact dishonesty the incomplete-window rule exists to prevent.
		w.IsGap = w.PacketCount == 0
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
	return resolveTagMapping(ctx, r.db, tenantID, tagID, tagMAC)
}

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx. resolveTagMapping is written against
// it so the SAME query runs either against the pool (read-only API callers) or against an
// in-flight ingest transaction (updateTagLatest) -- see the H2 fix note at updateTagLatest: a
// pool query issued while a tx holds a `FOR UPDATE` row lock acquires a SECOND connection from
// the pool, which self-deadlocks under pool saturation with concurrent gateway posts. That bug
// is why this function no longer hardcodes r.db.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func resolveTagMapping(ctx context.Context, q pgxQuerier, tenantID string, tagID, tagMAC *string) (string, *string, error) {
	// Try to match tag_id or tag_mac against goat_identifiers
	if tagID == nil && tagMAC == nil {
		return "unmapped", nil, nil
	}

	// Normalize exactly like the identity module's own normalizer (defect 2: a raw lowercase
	// device MAC must match the uppercase-stored goat_identifiers.normalized_value).
	var values []string
	if tagID != nil && *tagID != "" {
		values = append(values, domain.NormalizeTagIdentifier(*tagID))
	}
	if tagMAC != nil && *tagMAC != "" {
		values = append(values, domain.NormalizeTagIdentifier(*tagMAC))
	}

	if len(values) == 0 {
		return "unmapped", nil, nil
	}

	// Query for matching goats. normalized_value is already the identity module's canonical
	// form, so comparing it bare (no UPPER/TRIM wrapper on this side) keeps the tenant_id +
	// normalized_value index usable -- only the small, already-narrowed input `values` side is
	// normalized, in Go, above.
	query := `
		SELECT DISTINCT goat_id
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND normalized_value = ANY($2) AND status = 'active' AND smart_tag_capable IS TRUE
	`
	rows, err := q.Query(ctx, query, tenantID, values)
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

// monitoringBoundaryTx reads the instant animal monitoring starts for this tag: the earliest
// smart_tag_mapped_at across the ACTIVE, smart-tag-capable identifiers whose normalized_value is
// the tag's id or MAC. nil means the tag is not bound to an animal.
//
// Deliberately the same predicate resolveTagMapping uses, and deliberately the same normalizer
// (domain.NormalizeTagIdentifier -> the identity module's canonical
// strings.ToUpper(strings.TrimSpace(v))), so a tag can never be "mapped" with no boundary or
// carry a boundary while unmapped.
func monitoringBoundaryTx(ctx context.Context, tx pgx.Tx, tenantID, tagID string, tagMAC *string) (*time.Time, error) {
	values := []string{domain.NormalizeTagIdentifier(tagID)}
	if tagMAC != nil {
		if n := domain.NormalizeTagIdentifier(*tagMAC); n != "" && n != values[0] {
			values = append(values, n)
		}
	}
	var since *time.Time
	err := tx.QueryRow(ctx, `
		SELECT min(smart_tag_mapped_at)
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND normalized_value = ANY($2)
		      AND status = 'active' AND smart_tag_capable IS TRUE
	`, tenantID, values).Scan(&since)
	if err != nil {
		return nil, fmt.Errorf("read monitoring boundary: %w", err)
	}
	return since, nil
}

// GetGoatsByIDs fetches display_id and location for multiple goats.
func (r *Repository) GetGoatsByIDs(ctx context.Context, tenantID string, goatIDs []string) (map[string]ports.GoatData, error) {
	if len(goatIDs) == 0 {
		return make(map[string]ports.GoatData), nil
	}

	query := `
		SELECT g.goat_id, g.display_id, g.shed_id, g.park_id, ident1.animal_identifier_1, prov.mapped_by, to_char(prov.mapped_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SSZ') AS mapped_at, ident2.animal_identifier_2, prov.mapped_by, to_char(prov.mapped_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SSZ')
		FROM public.goats g
		-- Provenance belongs to the BINDING, not to the animal's ear tag.
		--
		-- mapped_by/mapped_at are stamped on the rows THIS MODULE creates to carry a BLE binding
		-- (source_system = 'herd_signals'). The animal's own ear-tag rows are pre-existing identity
		-- this module never created, so they carry NULL and always will. Reading provenance off the
		-- ear tag therefore returned NULL for every row while the binding right beside it held the
		-- actor and timestamp -- the columns looked unwired when they were merely being read from
		-- the wrong place.
		LEFT JOIN LATERAL (
			SELECT gi.mapped_by, gi.mapped_at
			FROM public.goat_identifiers gi
			WHERE gi.tenant_id = g.tenant_id
			  AND gi.goat_id = g.goat_id
			  AND gi.status = 'active'
			  AND gi.smart_tag_capable IS TRUE
			ORDER BY gi.mapped_at DESC NULLS LAST, gi.created_at DESC
			LIMIT 1
		) prov ON true
		LEFT JOIN LATERAL (
			SELECT identifier_value AS animal_identifier_1, mapped_by AS mapped_by_1, mapped_at AS mapped_at_1
			FROM public.goat_identifiers gi
			WHERE gi.tenant_id = g.tenant_id
			  AND gi.goat_id = g.goat_id
			  AND gi.identifier_type = 'animal_identifier_1'
			  AND gi.status = 'active'
			  AND gi.smart_tag_capable IS NOT TRUE
				ORDER BY gi.created_at DESC
			LIMIT 1
		) ident1 ON true
		LEFT JOIN LATERAL (
			SELECT identifier_value AS animal_identifier_2, mapped_by AS mapped_by_2, mapped_at AS mapped_at_2
			FROM public.goat_identifiers gi
			WHERE gi.tenant_id = g.tenant_id
			  AND gi.goat_id = g.goat_id
			  AND gi.identifier_type = 'animal_identifier_2'
			  AND gi.status = 'active'
			  AND gi.smart_tag_capable IS NOT TRUE
			ORDER BY gi.created_at DESC
			LIMIT 1
		) ident2 ON true
		WHERE g.tenant_id = $1 AND g.goat_id = ANY($2)
	`
	rows, err := r.db.Query(ctx, query, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]ports.GoatData)
	for rows.Next() {
		var goatID, displayID string
		var animalIdentifier1, animalIdentifier2, shedID, parkID *string
		var mappedBy1, mappedAt1, mappedBy2, mappedAt2 *string
		if err := rows.Scan(&goatID, &displayID, &shedID, &parkID, &animalIdentifier1, &mappedBy1, &mappedAt1, &animalIdentifier2, &mappedBy2, &mappedAt2); err != nil {
			return nil, err
		}
		result[goatID] = ports.GoatData{
			DisplayID:         displayID,
			AnimalIdentifier1: animalIdentifier1,
			MappedBy1:         mappedBy1,
			MappedAt1:         mappedAt1,
			AnimalIdentifier2: animalIdentifier2,
			MappedBy2:         mappedBy2,
			MappedAt2:         mappedAt2,
			ShedID:            shedID,
			ParkID:            parkID,
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
	// Normalize every input the same way resolveTagMapping does (defect 2). The result map is
	// keyed by the NORMALIZED value -- callers must look it up with
	// domain.NormalizeTagIdentifier(tagID/tagMAC), not the raw device string.
	normalized := make([]string, len(values))
	for i, v := range values {
		normalized[i] = domain.NormalizeTagIdentifier(v)
	}
	rows, err := r.db.Query(ctx, `
		SELECT normalized_value, goat_id
		FROM public.goat_identifiers
		WHERE tenant_id = $1 AND status = 'active' AND smart_tag_capable IS TRUE
		      AND normalized_value = ANY($2)
	`, tenantID, normalized)
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

// GetBaselineDeltas computes the p75 24h/300s-tier baseline motion_delta for many tags in ONE
// windowed query (mirrors domain.Baseline75's definition: p75 over non-gap buckets, never
// median -- a resting animal's median bucket is 0). Bounded to the tag_id list the caller
// already fetched (a single live page), so this never scans the whole tenant's tag population.
// GetBatteryHistory computes the first/last battery_mv reading (and timestamps) within the
// trend window for many tags in ONE query, using a LATERAL + LIMIT 1 per tag (same pattern as
// tagLocationJoin/ResolveTagsBatch) against herd_signal_packets' existing
// (tenant_id, tag_id, received_at DESC) index -- never a per-row historical scan.
// CRITICAL: herd_signal_packets is partitioned on received_date, so both the received_at
// range predicate and the received_date partition key must be present to enable partition
// pruning. Without received_date, queries scan every retained partition.
func (r *Repository) GetBatteryHistory(ctx context.Context, tenantID string, tagIDs []string, windowDays int) (map[string]ports.BatteryHistoryPoint, error) {
	result := make(map[string]ports.BatteryHistoryPoint)
	if len(tagIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
		WITH tags AS (SELECT unnest($2::text[]) AS tag_id)
		SELECT t.tag_id, first_pkt.battery_mv, first_pkt.received_at, last_pkt.battery_mv, last_pkt.received_at
		FROM tags t
		LEFT JOIN LATERAL (
			SELECT battery_mv, received_at
			FROM public.herd_signal_packets
			WHERE tenant_id = $1 AND tag_id = t.tag_id AND battery_mv IS NOT NULL
			      AND received_at >= now() - make_interval(days => $3::int)
			      AND received_date >= (now()::date - ($3::int || ' days')::interval)::date
			ORDER BY received_at ASC
			LIMIT 1
		) first_pkt ON true
		LEFT JOIN LATERAL (
			SELECT battery_mv, received_at
			FROM public.herd_signal_packets
			WHERE tenant_id = $1 AND tag_id = t.tag_id AND battery_mv IS NOT NULL
			      AND received_at >= now() - make_interval(days => $3::int)
			      AND received_date >= (now()::date - ($3::int || ' days')::interval)::date
			ORDER BY received_at DESC
			LIMIT 1
		) last_pkt ON true
		WHERE first_pkt.battery_mv IS NOT NULL
	`, tenantID, tagIDs, windowDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tagID string
		var p ports.BatteryHistoryPoint
		if err := rows.Scan(&tagID, &p.FirstMV, &p.FirstAt, &p.LastMV, &p.LastAt); err != nil {
			return nil, err
		}
		result[tagID] = p
	}
	return result, rows.Err()
}

func (r *Repository) GetBaselineDeltas(ctx context.Context, tenantID string, tagIDs []string) (map[string]int64, error) {
	result := make(map[string]int64)
	if len(tagIDs) == 0 {
		return result, nil
	}
	// gap_delta = false: a reconnect lump is a total over an unknown span, not a sample of this
	// animal's normal per-bucket movement (maintainer decision on offline behaviour, mirrors
	// domain.Baseline75's Go-side exclusion for the same reason). Never remove this predicate to
	// "smooth" the baseline -- that is exactly the mistake this exclusion exists to prevent.
	// THE MONITORING BOUNDARY (000197) is enforced here, not just documented. The baseline is
	// the most animal-attributed number in this module -- it is what a spike is measured
	// against -- so:
	//   * a tag with a NULL boundary (unmapped) gets NO baseline at all. Not a zero, not a
	//     default: there is no animal, so there is nothing to attribute a baseline to.
	//   * buckets EARLIER than the boundary are excluded. Bench movement from a tag rattling in
	//     a box must never enter a real animal's baseline, and without this predicate the first
	//     thing the system would tell a farm about a newly tagged goat is derived from exactly
	//     that.
	rows, err := r.db.Query(ctx, `
		SELECT w.tag_id, percentile_disc(0.75) WITHIN GROUP (ORDER BY w.motion_delta)
		FROM public.herd_signal_activity_windows w
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = w.tenant_id AND tl.tag_id = w.tag_id
		WHERE w.tenant_id = $1 AND w.tag_id = ANY($2)
		      AND w.bucket_seconds = 300 AND w.packet_count > 0 AND w.gap_delta = false
		      AND w.bucket_start >= now() - interval '24 hours'
		      AND tl.animal_monitoring_since IS NOT NULL
		      AND w.bucket_start >= tl.animal_monitoring_since
		GROUP BY w.tag_id
	`, tenantID, tagIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tagID string
		var baseline int64
		if err := rows.Scan(&tagID, &baseline); err != nil {
			return nil, err
		}
		result[tagID] = baseline
	}
	return result, rows.Err()
}

// GetGatewayTagStats computes tags_seen_recently/weak_tags/unmapped_tags for every gateway in
// ONE grouped query. "Recently" uses the same 30-minute staleness window as
// effectiveMovementStateExpr, so a gateway's "tags seen recently" count agrees with which of its
// tags the live view itself would still call non-stale.
func (r *Repository) GetGatewayTagStats(ctx context.Context, tenantID string) (map[string]ports.GatewayTagStats, error) {
	result := make(map[string]ports.GatewayTagStats)
	rows, err := r.db.Query(ctx, `
		SELECT gateway_id,
		       count(*) FILTER (WHERE now() - last_seen_at <= `+staleAfterInterval+`),
		       count(*) FILTER (WHERE signal_state = 'weak'),
		       count(*) FILTER (WHERE mapping_state = 'unmapped')
		FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1 AND gateway_id IS NOT NULL
		GROUP BY gateway_id
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var gatewayID string
		var stats ports.GatewayTagStats
		if err := rows.Scan(&gatewayID, &stats.TagsSeenRecently, &stats.WeakTags, &stats.UnmappedTags); err != nil {
			return nil, err
		}
		result[gatewayID] = stats
	}
	return result, rows.Err()
}

// GetGatewayWindowStats computes 15-minute window aggregates per gateway from herd_signal_activity_windows.
// Returns unique tag count, distinct motion delta count, and total packet count for each gateway.
func (r *Repository) GetGatewayWindowStats(ctx context.Context, tenantID string) (map[string]ports.GatewayWindowStats, error) {
	result := make(map[string]ports.GatewayWindowStats)

	// The 15-minute window: from now minus 15 minutes to now.
	// herd_signal_activity_windows is pre-bucketed at 60s, 300s, and 3600s tiers.
	// Query the 300s tier to cover 15 minutes efficiently (5-minute buckets).
	// projection-review: membership=herd_signal_activity_windows.tag_id; group_key=gateway_id; join_cardinality=one tag is joined to exactly one tag_latest row per tag_id; pagination=none whole-result aggregate time-windowed to 15 minutes; scope=tenant_id with gateway_id filter
	rows, err := r.db.Query(ctx, `
		SELECT
			tl.gateway_id,
			count(DISTINCT hw.tag_id),
			count(DISTINCT CASE WHEN hw.motion_delta > 0 THEN hw.tag_id END),
			sum(hw.packet_count)
		FROM public.herd_signal_activity_windows hw
		JOIN public.herd_signal_tag_latest tl ON tl.tenant_id = hw.tenant_id AND tl.tag_id = hw.tag_id
		WHERE hw.tenant_id = $1
		  AND hw.bucket_start >= now() - interval '15 minutes'
		  AND hw.bucket_seconds = 300
		  AND tl.gateway_id IS NOT NULL
		GROUP BY tl.gateway_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get gateway window stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var gatewayID *string
		var tagsSeen, motionCount, packetCount *int64
		if err := rows.Scan(&gatewayID, &tagsSeen, &motionCount, &packetCount); err != nil {
			return nil, err
		}
		if gatewayID != nil && *gatewayID != "" {
			// Convert int64 to int for the response
			tagsSeenInt := int(*tagsSeen)
			motionCountInt := int(*motionCount)
			packetCountInt := int(*packetCount)
			result[*gatewayID] = ports.GatewayWindowStats{
				TagsSeenInWindow:        &tagsSeenInt,
				DistinctMotionDeltas:    &motionCountInt,
				PacketsReceivedInWindow: &packetCountInt,
			}
		}
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
			count(*) FILTER (WHERE (`+effectiveMovementStateExpr+`) <> 'stale'),
			count(*) FILTER (WHERE (`+effectivePatternStateExpr+`) = 'missing'),
			count(*) FILTER (WHERE (`+effectivePatternStateExpr+`) IN ('quiet_watch', 'inactive')),
			count(*) FILTER (WHERE (`+effectivePatternStateExpr+`) = 'spike'),
			count(*) FILTER (WHERE tl.signal_state = 'weak'),
			count(*) FILTER (WHERE tl.battery_state = 'low'),
			count(*) FILTER (WHERE tl.mapping_state = 'unmapped')
		FROM public.herd_signal_tag_latest tl
		WHERE tl.tenant_id = $1
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
		WHERE tl.tenant_id = $1 AND (`+effectiveMovementStateExpr+`) <> 'stale' AND g.shed_id IS NOT NULL
	`, tagLocationJoin), tenantID).Scan(&d.ShedsWithCoverage)
	if err != nil {
		return d, fmt.Errorf("insights sheds with coverage: %w", err)
	}

	// Card 8: post_vaccination_movement_watch. Bounded to the last 24h of accepted
	// vaccination_completions (indexed by tenant_id), joined to a mapped tag currently watched.
	// projection-review: membership=vaccination_completions.goat_id; group_key=count(DISTINCT vc.goat_id); join_cardinality=one goat can have multiple active smart-tag identifiers and each can be joined to herd_signal_tag_latest; pagination=none whole-result aggregate; scope=tenant_id
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT vc.goat_id)
		FROM public.vaccination_completions vc
		JOIN public.goat_identifiers gi
		  ON gi.tenant_id = vc.tenant_id AND gi.goat_id = vc.goat_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = vc.tenant_id
		  AND (UPPER(BTRIM(tl.tag_id)) = gi.normalized_value OR UPPER(BTRIM(tl.tag_mac)) = gi.normalized_value)
		WHERE vc.tenant_id = $1
		  AND vc.status = 'accepted'
		  AND vc.administered_at >= now() - interval '24 hours'
		  -- Monitoring boundary (000197): a correlated card is an animal-attributed claim, so it
		  -- may only consider a tag that IS bound to an animal, and only events that happened
		  -- after that binding. A vaccination recorded while the tag was still on a bench says
		  -- nothing about the animal now wearing it.
		  AND tl.animal_monitoring_since IS NOT NULL
		  AND vc.administered_at >= tl.animal_monitoring_since
		  AND (`+effectivePatternStateExpr+`) IN ('quiet_watch', 'inactive', 'missing')
	`, tenantID).Scan(&d.PostVaccinationWatchCount)
	if err != nil {
		return d, fmt.Errorf("insights post-vaccination watch: %w", err)
	}

	// Card 9: health_case_activity_trend. Bounded to currently-active health_cases.
	// projection-review: membership=health_cases.goat_id; group_key=count(DISTINCT hc.goat_id); join_cardinality=one goat can have multiple active smart-tag identifiers and each can be joined to herd_signal_tag_latest; pagination=none whole-result aggregate; scope=tenant_id
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT hc.goat_id)
		FROM public.health_cases hc
		JOIN public.goat_identifiers gi
		  ON gi.tenant_id = hc.tenant_id AND gi.goat_id = hc.goat_id
		  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = hc.tenant_id
		  AND (UPPER(BTRIM(tl.tag_id)) = gi.normalized_value OR UPPER(BTRIM(tl.tag_mac)) = gi.normalized_value)
		WHERE hc.tenant_id = $1
		  AND hc.status = 'active'
		  -- Monitoring boundary (000197), same rule as the vaccination card above.
		  AND tl.animal_monitoring_since IS NOT NULL
		  AND (`+effectivePatternStateExpr+`) IN ('quiet_watch', 'inactive')
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
		      -- Monitoring boundary (000197): the shed only counts as covered by a tag that is
		      -- actually bound to an animal in it.
		      AND tl.animal_monitoring_since IS NOT NULL
		  )
	`, tagLocationJoin), tenantID).Scan(&d.FeedActivityShedsCount)
	if err != nil {
		return d, fmt.Errorf("insights feed activity: %w", err)
	}

	// Card 11: weight_activity. Weighing is FREE-FLOW and ISOLATED (AGENTS.md): it dropped
	// weighing_observations.animal_id outright (migration 000078) and never resolves a scan to
	// goat identity on ANY path, in EITHER direction. This card must not reintroduce that
	// resolution from the herd-signals side either -- it correlates by the RAW scanned string
	// against the tag's own id/MAC, exactly the same un-resolved shape weighing itself stores,
	// with no goat_identifiers join at all.
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT wo.scanned_identifier)
		FROM public.weighing_observations wo
		JOIN public.herd_signal_tag_latest tl
		  ON tl.tenant_id = wo.tenant_id
		  AND (UPPER(BTRIM(tl.tag_id)) = UPPER(BTRIM(wo.scanned_identifier))
		       OR UPPER(BTRIM(tl.tag_mac)) = UPPER(BTRIM(wo.scanned_identifier)))
		WHERE wo.tenant_id = $1
		  AND btrim(wo.scanned_identifier) <> ''
		  AND wo.accepted_at >= now() - interval '24 hours'
		  -- Monitoring boundary (000197): still no goat_identifiers join and still correlated by
		  -- the RAW scanned string (weighing is free-flow and never resolves a scan to identity),
		  -- but a weighing that happened before this tag was bound to an animal is bench history,
		  -- not an observation of the animal now wearing it.
		  AND tl.animal_monitoring_since IS NOT NULL
		  AND wo.accepted_at >= tl.animal_monitoring_since
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
	// firstMotionAt/lastMotionAt track the received_at of the packet that CURRENTLY holds
	// firstMotionCount/lastMotionCount, so an out-of-order packet within the same ingest batch
	// is compared by TIME, not by iteration/arrival order.
	//
	// M4 fix (maintainer scale review): the previous condition compared a packet's received_at
	// against `lastSeenAt - bucketSeconds`, a value that is nearly always in the past relative
	// to any packet inside the bucket, so it was effectively always true -- last_motion_count
	// silently became "whichever packet iterated last" (Go map/slice order), not the
	// chronologically last one. That corrupted motion_delta, which feeds movement_state,
	// pattern_state, and the p75 baseline.
	type bucketData struct {
		firstMotionCount *int64
		firstMotionAt    time.Time
		lastMotionCount  *int64
		lastMotionAt     time.Time
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

		bd, exists := buckets[key]
		if !exists {
			bd = &bucketData{firstSeenAt: p.ReceivedAt, lastSeenAt: p.ReceivedAt}
			buckets[key] = bd
		}

		bd.packetCount++
		if p.ReceivedAt.Before(bd.firstSeenAt) {
			bd.firstSeenAt = p.ReceivedAt
		}
		if p.ReceivedAt.After(bd.lastSeenAt) {
			bd.lastSeenAt = p.ReceivedAt
		}

		if p.MotionCount != nil {
			if bd.firstMotionCount == nil || p.ReceivedAt.Before(bd.firstMotionAt) {
				bd.firstMotionCount = p.MotionCount
				bd.firstMotionAt = p.ReceivedAt
			}
			if bd.lastMotionCount == nil || !p.ReceivedAt.Before(bd.lastMotionAt) {
				bd.lastMotionCount = p.MotionCount
				bd.lastMotionAt = p.ReceivedAt
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

		// Upsert query: on conflict, MERGE across batches rather than overwrite.
		//
		// M4 fix (maintainer scale review): the previous ON CONFLICT clause (a) replaced
		// avg_rssi_dbm with the incoming batch's average instead of a packet-count-weighted
		// merge, (b) always overwrote last_motion_count/last_seen_at from the incoming batch
		// regardless of whether it was actually chronologically later (an out-of-order-EARLY
		// retried batch could move last_seen_at backwards), and (c) never reconsidered
		// first_motion_count/first_seen_at at all, so a late-arriving-but-chronologically-EARLIER
		// packet could never correct the bucket's start point. All three drift motion_delta,
		// which feeds movement_state, pattern_state, AND the p75 baseline -- this is silent data
		// corruption, not a cosmetic bug.
		//
		// first_seen_at/last_seen_at only move outward (LEAST/GREATEST); first_motion_count/
		// last_motion_count are re-derived from whichever side (stored vs incoming) actually owns
		// the new first_seen_at/last_seen_at; motion_delta is recomputed from the merged pair
		// rather than trusted from a single batch, and only when both sides of the merged pair
		// are known (a battery/temperature-only packet updating last_seen_at with no
		// motion_count present leaves motion_delta untouched rather than corrupting it toward
		// zero).
		batch.Queue(`
			INSERT INTO public.herd_signal_activity_windows (
				tenant_id, tag_id, bucket_start, bucket_seconds,
				first_motion_count, last_motion_count, motion_delta,
				packet_count, avg_rssi_dbm, min_rssi_dbm, max_rssi_dbm,
				first_seen_at, last_seen_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO UPDATE
			SET first_motion_count = CASE WHEN $12 < public.herd_signal_activity_windows.first_seen_at
			                               THEN $5 ELSE public.herd_signal_activity_windows.first_motion_count END,
			    last_motion_count  = CASE WHEN $13 > public.herd_signal_activity_windows.last_seen_at
			                               THEN COALESCE($6, public.herd_signal_activity_windows.last_motion_count)
			                               ELSE public.herd_signal_activity_windows.last_motion_count END,
			    motion_delta = CASE
			      WHEN (CASE WHEN $12 < public.herd_signal_activity_windows.first_seen_at THEN $5
			                 ELSE public.herd_signal_activity_windows.first_motion_count END) IS NOT NULL
			       AND (CASE WHEN $13 > public.herd_signal_activity_windows.last_seen_at
			                 THEN COALESCE($6, public.herd_signal_activity_windows.last_motion_count)
			                 ELSE public.herd_signal_activity_windows.last_motion_count END) IS NOT NULL
			      THEN GREATEST(0,
			             (CASE WHEN $13 > public.herd_signal_activity_windows.last_seen_at
			                   THEN COALESCE($6, public.herd_signal_activity_windows.last_motion_count)
			                   ELSE public.herd_signal_activity_windows.last_motion_count END)
			             - (CASE WHEN $12 < public.herd_signal_activity_windows.first_seen_at THEN $5
			                     ELSE public.herd_signal_activity_windows.first_motion_count END))
			      ELSE public.herd_signal_activity_windows.motion_delta
			    END,
			    packet_count = public.herd_signal_activity_windows.packet_count + $8,
			    avg_rssi_dbm = CASE
			      WHEN $9 IS NULL THEN public.herd_signal_activity_windows.avg_rssi_dbm
			      WHEN public.herd_signal_activity_windows.avg_rssi_dbm IS NULL THEN $9
			      ELSE (public.herd_signal_activity_windows.avg_rssi_dbm * public.herd_signal_activity_windows.packet_count + $9 * $8)
			           / (public.herd_signal_activity_windows.packet_count + $8)
			    END,
			    min_rssi_dbm = CASE WHEN $10 IS NULL THEN public.herd_signal_activity_windows.min_rssi_dbm
			                         WHEN public.herd_signal_activity_windows.min_rssi_dbm IS NULL THEN $10
			                         ELSE LEAST(public.herd_signal_activity_windows.min_rssi_dbm, $10) END,
			    max_rssi_dbm = CASE WHEN $11 IS NULL THEN public.herd_signal_activity_windows.max_rssi_dbm
			                         WHEN public.herd_signal_activity_windows.max_rssi_dbm IS NULL THEN $11
			                         ELSE GREATEST(public.herd_signal_activity_windows.max_rssi_dbm, $11) END,
			    first_seen_at = LEAST(public.herd_signal_activity_windows.first_seen_at, $12),
			    last_seen_at  = GREATEST(public.herd_signal_activity_windows.last_seen_at, $13)
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
		// scale-guard:ignore: pgx.SendBatch result iteration; all upsert statements are pre-batched, not individual round-trips
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("upsert bucket %d: %w", i, err)
		}
	}

	return nil
}
