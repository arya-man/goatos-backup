package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Service implements the herd signals business logic.
type Service struct {
	repo       ports.Repository
	log        *slog.Logger
	thresholds domain.Thresholds
}

// NewService creates a new herd signals service.
func NewService(repo ports.Repository, log ...*slog.Logger) *Service {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Service{
		repo:       repo,
		log:        l,
		thresholds: domain.DefaultThresholds(),
	}
}

// WithThresholds overrides the default thresholds.
func (s *Service) WithThresholds(t domain.Thresholds) *Service {
	s.thresholds = t
	return s
}

// IngestPackets ingests a batch of raw BLE packets and updates tag latest state.
func (s *Service) IngestPackets(ctx context.Context, actor domain.Actor, req domain.IngestRequest) (domain.IngestResponse, error) {
	// Validate actor
	if actor.TenantID == "" || actor.UserID == "" {
		return domain.IngestResponse{}, fmt.Errorf("actor tenant_id and user_id required")
	}

	// Parse gateway_seen_at timestamp
	gatewaySeen, err := time.Parse(time.RFC3339, req.GatewaySeen)
	if err != nil {
		return domain.IngestResponse{}, fmt.Errorf("invalid gateway_seen_at: %w", err)
	}

	// Convert request packets to domain packets
	packets := make([]domain.Packet, 0, len(req.Packets))
	for _, p := range req.Packets {
		seenAt, err := time.Parse(time.RFC3339, p.SeenAt)
		if err != nil {
			s.log.Warn("skipping packet with invalid seen_at", "seen_at", p.SeenAt, "tag_id", p.TagID)
			continue
		}

		packets = append(packets, domain.Packet{
			PacketID:              "", // DB will generate
			TenantID:              actor.TenantID,
			GatewayID:             &req.GatewayID,
			Source:                "gateway",
			TagID:                 p.TagID,
			TagMAC:                &p.TagMAC,
			ReceivedAt:            seenAt,
			GatewaySeenAt:         &gatewaySeen,
			RSSIdbm:               p.RSSI,
			BatteryMV:             p.Battery,
			TagTemperatureC:       p.TagTemperature,
			MotionCount:           p.MotionCount,
			SensorState:           p.SensorState,
			TemperatureSensorOK:   p.TemperatureSensorOK,
			AccelerometerSensorOK: p.AccelerometerSensorOK,
			RawAdv:                p.RawAdv,
			RawPayload:            map[string]interface{}{},
		})
	}

	if len(packets) == 0 {
		return domain.IngestResponse{}, fmt.Errorf("no valid packets in request")
	}

	gw := domain.Gateway{
		TenantID:   actor.TenantID,
		GatewayID:  req.GatewayID,
		Status:     "active",
		LastSeenAt: &gatewaySeen,
	}

	// Gateway upsert, packet insert, activity-window rollup, and tag_latest update all happen in
	// ONE transaction inside the repository (see postgres.Repository.IngestPackets).
	traceID := httpmiddleware.TraceIDFromContext(ctx)
	stored, latestUpdated, err := s.repo.IngestPackets(ctx, actor.TenantID, gw, packets)
	if err != nil {
		s.log.Error("failed to ingest packets", "gateway_id", req.GatewayID, "packet_count", len(packets), "error", err)
		return domain.IngestResponse{}, fmt.Errorf("ingest failed: %w", err)
	}

	return domain.IngestResponse{
		Accepted:      len(req.Packets),
		Stored:        stored,
		LatestUpdated: latestUpdated,
		TraceID:       traceID,
	}, nil
}

// ListLive fetches the current tag status with optional filters and pagination.
func (s *Service) ListLive(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, mappingState, pattern, q *string, cursor string, limit int) (domain.LiveResponse, error) {
	if actor.TenantID == "" {
		return domain.LiveResponse{}, fmt.Errorf("actor tenant_id required")
	}

	tags, summary, nextCursor, err := s.repo.ListTagsLatest(
		ctx, actor.TenantID, parkID, shedID, movementState, mappingState, pattern, q, cursor, limit,
	)
	if err != nil {
		s.log.Error("failed to list tags latest", "error", err)
		return domain.LiveResponse{}, fmt.Errorf("list tags failed: %w", err)
	}

	items := s.enrichTagsBatch(ctx, actor.TenantID, tags)

	return domain.LiveResponse{
		Summary:    summary,
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetTimeline fetches motion history for a tag.
func (s *Service) GetTimeline(ctx context.Context, actor domain.Actor, tagID, from, to string, bucketSeconds int) (domain.TimelineResponse, error) {
	if actor.TenantID == "" {
		return domain.TimelineResponse{}, fmt.Errorf("actor tenant_id required")
	}

	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return domain.TimelineResponse{}, fmt.Errorf("invalid from timestamp: %v: %w", err, domain.ErrValidation)
	}

	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return domain.TimelineResponse{}, fmt.Errorf("invalid to timestamp: %v: %w", err, domain.ErrValidation)
	}

	if toTime.Before(fromTime) {
		return domain.TimelineResponse{}, fmt.Errorf("to must not be before from: %w", domain.ErrValidation)
	}

	// Defect fix: the timeline used to hardcode bucket_seconds=60 regardless of range. Select
	// the tier from the requested range when the caller did not pin one; when the caller DID
	// pin one, it must be a tier activity windows are actually stored at (60/300/3600) or the
	// request is rejected rather than silently coerced.
	if bucketSeconds == 0 {
		bucketSeconds = domain.SelectBucketTier(fromTime, toTime)
	} else if !domain.IsSupportedBucketSeconds(bucketSeconds) {
		return domain.TimelineResponse{}, fmt.Errorf("unsupported bucket_seconds %d: must be one of %v: %w", bucketSeconds, domain.SupportedBucketSeconds, domain.ErrValidation)
	}

	// Bound the bucket count regardless of range/tier combination (AGENTS.md scale
	// anti-patterns: never serve an unbounded range).
	requestedBuckets := int(toTime.Sub(fromTime)/(time.Duration(bucketSeconds)*time.Second)) + 1
	if requestedBuckets > domain.MaxTimelineBuckets {
		return domain.TimelineResponse{}, fmt.Errorf("requested range spans %d buckets at %ds resolution, exceeds max %d: narrow the range or request a coarser bucket_seconds: %w", requestedBuckets, bucketSeconds, domain.MaxTimelineBuckets, domain.ErrValidation)
	}

	windows, err := s.repo.ListActivityWindows(ctx, actor.TenantID, tagID, fromTime, toTime, bucketSeconds)
	if err != nil {
		s.log.Error("failed to list activity windows", "tag_id", tagID, "error", err)
		return domain.TimelineResponse{}, fmt.Errorf("list windows failed: %w", err)
	}

	// The stored windows are sparse (a bucket with no packets has no row). Densify: a caller
	// must be able to tell "no packets" (is_gap=true) apart from "packets arrived, zero
	// movement" (packet_count>0, motion_delta=0) -- that distinction is the whole product
	// requirement, so it must never collapse into a missing array entry.
	byBucket := make(map[int64]domain.ActivityWindow, len(windows))
	for _, w := range windows {
		byBucket[w.BucketStart.Unix()] = w
	}

	bucketDur := time.Duration(bucketSeconds) * time.Second
	start := fromTime.Truncate(bucketDur)
	tlWindows := make([]domain.TimelineWindow, 0, requestedBuckets)
	for t := start; !t.After(toTime); t = t.Add(bucketDur) {
		if w, ok := byBucket[t.Unix()]; ok {
			tlWindows = append(tlWindows, domain.TimelineWindow{
				BucketStart:      w.BucketStart,
				BucketSeconds:    w.BucketSeconds,
				FirstMotionCount: w.FirstMotionCount,
				LastMotionCount:  w.LastMotionCount,
				MotionDelta:      w.MotionDelta,
				PacketCount:      w.PacketCount,
				AvgRSSIdbm:       w.AvgRSSIdbm,
				MinRSSIdbm:       w.MinRSSIdbm,
				MaxRSSIdbm:       w.MaxRSSIdbm,
				FirstSeenAt:      w.FirstSeenAt,
				LastSeenAt:       w.LastSeenAt,
				IsGap:            w.PacketCount == 0,
				// Three distinct facts, never collapsed (maintainer decision on offline
				// behaviour): a GAP bucket (this branch is false, the OTHER branch below fires
				// instead -- no row at all), a ZERO-delta bucket (packets arrived, no movement:
				// IsGap=false, GapDelta=false, MotionDelta=0), and a RECONNECT bucket (packets
				// arrived carrying a TOTAL across a prior gap: IsGap=false, GapDelta=true).
				GapDelta: w.GapDelta,
			})
			continue
		}
		tlWindows = append(tlWindows, domain.TimelineWindow{
			BucketStart:   t,
			BucketSeconds: bucketSeconds,
			IsGap:         true,
		})
	}

	return domain.TimelineResponse{
		TagID:   tagID,
		Buckets: tlWindows,
	}, nil
}

// ListGateways fetches all gateways for the tenant.
func (s *Service) ListGateways(ctx context.Context, actor domain.Actor) (domain.GatewaysResponse, error) {
	if actor.TenantID == "" {
		return domain.GatewaysResponse{}, fmt.Errorf("actor tenant_id required")
	}

	gws, err := s.repo.GetGatewaysByTenant(ctx, actor.TenantID)
	if err != nil {
		s.log.Error("failed to list gateways", "error", err)
		return domain.GatewaysResponse{}, fmt.Errorf("list gateways failed: %w", err)
	}

	shedIDs := make([]string, 0, len(gws))
	seen := make(map[string]struct{})
	for _, gw := range gws {
		if gw.ShedID != nil && *gw.ShedID != "" {
			if _, ok := seen[*gw.ShedID]; !ok {
				seen[*gw.ShedID] = struct{}{}
				shedIDs = append(shedIDs, *gw.ShedID)
			}
		}
	}
	locations, err := s.repo.GetShedLocations(ctx, actor.TenantID, shedIDs)
	if err != nil {
		s.log.Warn("failed to resolve gateway shed locations", "error", err)
		locations = map[string]ports.ShedLocation{}
	}

	tagStats, err := s.repo.GetGatewayTagStats(ctx, actor.TenantID)
	if err != nil {
		s.log.Warn("failed to compute gateway tag stats", "error", err)
		tagStats = map[string]ports.GatewayTagStats{}
	}

	items := make([]domain.GatewayItem, len(gws))
	for i, gw := range gws {
		label := gw.Label
		networkMode := gw.NetworkMode
		wifiMAC := gw.WifiMAC
		bleMAC := gw.BLEMAC

		var shedName, partitionLabel, parkID, parkName *string
		var locDisplay string
		if gw.ShedID != nil {
			if loc, ok := locations[*gw.ShedID]; ok {
				shedName = &loc.ShedName
				if loc.PartitionLabel != "" {
					partitionLabel = &loc.PartitionLabel
				}
				if loc.ParkID != "" {
					parkID = &loc.ParkID
				}
				if loc.ParkName != "" {
					parkName = &loc.ParkName
				}
				opLoc := oploc.OperationalLocation{ShedName: loc.ShedName, PartitionLabel: loc.PartitionLabel}
				locDisplay = opLoc.Display()
			}
		}

		items[i] = domain.GatewayItem{
			GatewayID:       gw.GatewayID,
			Label:           &label,
			ParkID:          parkID,
			ParkName:        parkName,
			ShedID:          gw.ShedID,
			ShedName:        shedName,
			PartitionLabel:  partitionLabel,
			LocationDisplay: &locDisplay,
			NetworkMode:     &networkMode,
			WifiMAC:         &wifiMAC,
			BLEMAC:          &bleMAC,
			LastSeenAt:      gw.LastSeenAt,
			// Status is computed "online"/"offline" from last_seen_at freshness (contract type
			// HerdGatewayStatus), not the raw stored active/inactive/error text -- a gateway
			// that stopped heartbeating 2 hours ago is "offline" to an operator regardless of
			// what its last-known stored status string was.
			Status:           gatewayStatus(gw.LastSeenAt, s.thresholds),
			TagsSeenRecently: tagStats[gw.GatewayID].TagsSeenRecently,
			WeakTags:         tagStats[gw.GatewayID].WeakTags,
			UnmappedTags:     tagStats[gw.GatewayID].UnmappedTags,
		}
	}

	return domain.GatewaysResponse{Gateways: items}, nil
}

// GetInsights computes the 12 GET /herd-signals/insights cards. Backend owns the label/unit/
// signal_type/formula/caveat copy for every card (AGENTS.md backend-owns-labels rule);
// admin-web renders it verbatim.
func (s *Service) GetInsights(ctx context.Context, actor domain.Actor) (domain.InsightsResponse, error) {
	if actor.TenantID == "" {
		return domain.InsightsResponse{}, fmt.Errorf("actor tenant_id required")
	}

	d, err := s.repo.GetInsightsData(ctx, actor.TenantID)
	if err != nil {
		s.log.Error("failed to compute insights data", "error", err)
		return domain.InsightsResponse{}, fmt.Errorf("insights failed: %w", err)
	}

	cards := []domain.InsightCard{
		{
			Key: "tags_live_now", Label: "Tags live now", Value: fmt.Sprintf("%d", d.TagsLiveNow), Unit: "tags",
			SignalType: "direct", Formula: "count(herd_signal_tag_latest) where last_seen_at within stale window",
			Caveat: "Counts tags that have sent a packet recently; a tag with no packet in 30+ minutes is excluded, not shown as zero.",
		},
		{
			Key: "missing_signal", Label: "Missing signal", Value: fmt.Sprintf("%d", d.MissingSignalCount), Unit: "tags",
			SignalType: "direct", Formula: "count(tag_latest) where pattern_state = missing",
			Caveat: "No packet received for 30+ minutes. Distinct from inactive: inactive tags are still transmitting.",
		},
		{
			Key: "low_movement_watch", Label: "Low movement watch", Value: fmt.Sprintf("%d", d.LowMovementWatchCount), Unit: "tags",
			SignalType: "derived", Formula: "count(tag_latest) where pattern_state in (quiet_watch, inactive)",
			Caveat: "Duration-based pattern over history, not a single reading. Not a health or behavior diagnosis.",
		},
		{
			Key: "high_movement_spike", Label: "High movement spike", Value: fmt.Sprintf("%d", d.HighMovementSpikeCount), Unit: "tags",
			SignalType: "derived", Formula: "count(tag_latest) where pattern_state = spike (current 15m delta > 2.5x this tag's own 24h p75 baseline)",
			Caveat: "Baseline is per-animal; a naturally active animal's spike threshold is higher than a naturally quiet animal's.",
		},
		{
			Key: "shed_signal_coverage", Label: "Shed signal coverage", Value: fmt.Sprintf("%d/%d", d.ShedsWithCoverage, d.ShedsTotal), Unit: "sheds",
			SignalType: "derived", Formula: "count(distinct shed_id with >=1 live tag) / count(distinct shed_id with any gateway)",
			Caveat: "A shed with no gateway deployed yet is excluded from the denominator, not counted as zero coverage.",
		},
		{
			Key: "weak_signal_tags", Label: "Weak signal tags", Value: fmt.Sprintf("%d", d.WeakSignalTagsCount), Unit: "tags",
			SignalType: "direct", Formula: "count(tag_latest) where signal_state = weak (rssi <= -75 dBm)",
			Caveat: "RSSI reflects gateway placement and obstruction as much as tag health.",
		},
		{
			Key: "battery_attention", Label: "Battery attention", Value: fmt.Sprintf("%d", d.BatteryAttentionCount), Unit: "tags",
			SignalType: "direct", Formula: "count(tag_latest) where battery_state = low (< 2800 mV)",
			Caveat: "Threshold is a provisional placeholder pending vendor discharge-curve confirmation.",
		},
		{
			Key: "post_vaccination_movement_watch", Label: "Post-vaccination movement watch", Value: fmt.Sprintf("%d", d.PostVaccinationWatchCount), Unit: "animals",
			SignalType: "correlated", Formula: "count(distinct goat_id) with an accepted vaccination_completions row in the last 24h AND a mapped live tag currently in (quiet_watch, inactive, missing)",
			Caveat: "Correlation only -- reduced movement after vaccination is not a diagnosis, and is expected for many animals.",
		},
		{
			Key: "health_case_activity_trend", Label: "Health case activity trend", Value: fmt.Sprintf("%d", d.HealthCaseActivityCount), Unit: "animals",
			SignalType: "correlated", Formula: "count(distinct goat_id) with an active health_cases row AND a mapped live tag currently in (quiet_watch, inactive)",
			Caveat: "Correlation only. An open health case does not mean the movement change is caused by it, or vice versa.",
		},
		{
			Key: "feed_activity", Label: "Feed activity (sheds fed, tags covered)", Value: fmt.Sprintf("%d", d.FeedActivityShedsCount), Unit: "sheds",
			SignalType: "correlated", Formula: "count(distinct shed_id) with a feed_direction_completions row in the last 4h AND >=1 live tag",
			Caveat: "Shed-grain only: this cannot attribute a single tag's motion to feeding.",
		},
		{
			Key: "weight_activity", Label: "Weight activity", Value: fmt.Sprintf("%d", d.WeightActivityTagsCount), Unit: "tags",
			SignalType: "correlated", Formula: "count(distinct scanned_identifier) in weighing_observations in the last 24h matching a live tag's own id/MAC",
			Caveat: "Correlation only, by raw scanned string -- weighing is free-flow and never resolves a scan to goat identity (AGENTS.md), and this card does not either.",
		},
		{
			Key: "unmapped_smart_tags", Label: "Unmapped smart tags", Value: fmt.Sprintf("%d", d.UnmappedSmartTagsCount), Unit: "tags",
			SignalType: "direct", Formula: "count(tag_latest) where mapping_state = unmapped",
			Caveat: "A tag that has never been assigned to an active goat_identifiers row, or whose identifier is not smart_tag_capable.",
		},
	}

	return domain.InsightsResponse{Cards: cards}, nil
}

// enrichTagsBatch enriches a page of tags with animal mapping and location data using batched
// lookups (one query per lookup kind for the whole page, never one per row -- AGENTS.md
// operational read model contract).
func (s *Service) enrichTagsBatch(ctx context.Context, tenantID string, tags []domain.TagLatest) []domain.LiveItem {
	items := make([]domain.LiveItem, 0, len(tags))
	if len(tags) == 0 {
		return items
	}

	// Batch-resolve tag_id/tag_mac -> goat_id for every row already marked 'mapped' at ingest.
	values := make([]string, 0, len(tags)*2)
	seenValue := make(map[string]struct{})
	for _, tag := range tags {
		if tag.MappingState != "mapped" {
			continue
		}
		if _, ok := seenValue[tag.TagID]; !ok {
			seenValue[tag.TagID] = struct{}{}
			values = append(values, tag.TagID)
		}
		if tag.TagMAC != nil && *tag.TagMAC != "" {
			if _, ok := seenValue[*tag.TagMAC]; !ok {
				seenValue[*tag.TagMAC] = struct{}{}
				values = append(values, *tag.TagMAC)
			}
		}
	}
	goatByValue, err := s.repo.ResolveTagsBatch(ctx, tenantID, values)
	if err != nil {
		s.log.Warn("failed to batch-resolve tag mappings", "error", err)
		goatByValue = map[string]string{}
	}

	goatIDs := make([]string, 0, len(goatByValue))
	seenGoat := make(map[string]struct{})
	for _, goatID := range goatByValue {
		if _, ok := seenGoat[goatID]; !ok {
			seenGoat[goatID] = struct{}{}
			goatIDs = append(goatIDs, goatID)
		}
	}
	goatData, err := s.repo.GetGoatsByIDs(ctx, tenantID, goatIDs)
	if err != nil {
		s.log.Warn("failed to batch-fetch goat data", "error", err)
		goatData = map[string]ports.GoatData{}
	}

	shedIDs := make([]string, 0, len(goatData))
	seenShed := make(map[string]struct{})
	for _, gd := range goatData {
		if gd.ShedID != nil && *gd.ShedID != "" {
			if _, ok := seenShed[*gd.ShedID]; !ok {
				seenShed[*gd.ShedID] = struct{}{}
				shedIDs = append(shedIDs, *gd.ShedID)
			}
		}
	}
	shedLocations, err := s.repo.GetShedLocations(ctx, tenantID, shedIDs)
	if err != nil {
		s.log.Warn("failed to batch-fetch shed locations", "error", err)
		shedLocations = map[string]ports.ShedLocation{}
	}

	// baseline_delta: one windowed query for the whole page (defect fix -- it was always nil
	// because the only alternative was a per-row 24h scan, which AGENTS.md's scale
	// anti-patterns forbid).
	tagIDs := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagIDs = append(tagIDs, tag.TagID)
	}
	baselines, err := s.repo.GetBaselineDeltas(ctx, tenantID, tagIDs)
	if err != nil {
		s.log.Warn("failed to batch-fetch baseline deltas", "error", err)
		baselines = map[string]int64{}
	}

	for _, tag := range tags {
		batteryEstimate := domain.BatteryLifeEstimate(tag.BatteryMV, s.thresholds)

		item := domain.LiveItem{
			TagID:                 tag.TagID,
			GatewayID:             tag.GatewayID,
			LastSeenAt:            tag.LastSeenAt.Format(time.RFC3339),
			RSSIdbm:               tag.LastRSSIdbm,
			SignalState:           nullableEnum(tag.SignalState),
			BatteryMV:             tag.BatteryMV,
			BatteryState:          nullableEnum(tag.BatteryState),
			BatteryLifeEstimate:   nullableEnum(batteryEstimate),
			TagTemperatureC:       tag.TagTemperatureC,
			MotionCount:           tag.MotionCount,
			MotionDelta:           tag.MotionDelta,
			MotionDelta1h:         tag.MotionDelta1h, // real 1h (3600s-tier) delta -- defect 4 fix, was wrongly aliased to the 15m value
			MotionWindowSeconds:   tag.MotionWindowSeconds,
			MovementState:         nullableEnum(tag.MovementState),
			PatternState:          nullableEnum(tag.PatternState),
			SensorState:           sensorStateSummary(tag.TemperatureSensorOK, tag.AccelerometerSensorOK),
			TemperatureSensorOK:   tag.TemperatureSensorOK,
			AccelerometerSensorOK: tag.AccelerometerSensorOK,
			MappingState:          tag.MappingState,
			GapDelta:              tag.GapDelta,
		}

		if baseline, ok := baselines[tag.TagID]; ok {
			b := baseline
			item.BaselineDelta = &b
		}

		if tag.TagMAC != nil {
			item.TagMAC = *tag.TagMAC
		}

		// ResolveTagsBatch's result map is keyed by the NORMALIZED value (defect 2), so the
		// lookup key must go through the same normalizer as the raw tag_id/tag_mac.
		var goatID string
		if id, ok := goatByValue[domain.NormalizeTagIdentifier(tag.TagID)]; ok {
			goatID = id
		} else if tag.TagMAC != nil {
			if id, ok := goatByValue[domain.NormalizeTagIdentifier(*tag.TagMAC)]; ok {
				goatID = id
			}
		}

		if goatID != "" {
			gid := goatID
			item.GoatID = &gid
			if gd, ok := goatData[goatID]; ok {
				displayID := gd.DisplayID
				item.DisplayID = &displayID
				item.ParkID = gd.ParkID
				item.ShedID = gd.ShedID
				if gd.ShedID != nil {
					if loc, ok := shedLocations[*gd.ShedID]; ok {
						shedName := loc.ShedName
						item.ShedName = &shedName
						if loc.PartitionLabel != "" {
							partitionLabel := loc.PartitionLabel
							item.PartitionLabel = &partitionLabel
						}
						if loc.ParkName != "" {
							parkName := loc.ParkName
							item.ParkName = &parkName
						}
						if loc.ParkID != "" && item.ParkID == nil {
							parkID := loc.ParkID
							item.ParkID = &parkID
						}
						opLoc := oploc.OperationalLocation{ShedName: loc.ShedName, PartitionLabel: loc.PartitionLabel}
						display := opLoc.Display()
						item.OperationalLocationDisplay = &display
					}
				}
			}
		}

		items = append(items, item)
	}

	return items
}

// nullableEnum converts a computed state string to a pointer, treating "" and the domain
// "unknown"-equivalent sentinel as JSON null rather than a literal value the admin-web
// contract's TS enum types (HerdSignalTone, HerdSignalBatteryState, HerdSignalMovementState,
// HerdSignalPatternState, ...) do not declare.
func nullableEnum(v string) *string {
	if v == "" || v == "unknown" {
		return nil
	}
	out := v
	return &out
}

// sensorStateSummary computes the "ok"/"abnormal" summary the contract's sensor_state field
// carries (HerdSignalSensorState) -- never the raw device sensor_state bitfield, which is
// stored but not exposed on this endpoint. Unknown (both flags nil) reports nil, matching the
// nullable contract field rather than guessing "ok".
func sensorStateSummary(temperatureOK, accelerometerOK *bool) *string {
	if temperatureOK == nil && accelerometerOK == nil {
		return nil
	}
	ok := (temperatureOK == nil || *temperatureOK) && (accelerometerOK == nil || *accelerometerOK)
	state := "abnormal"
	if ok {
		state = "ok"
	}
	return &state
}

// gatewayStatus computes the contract's "online"/"offline" status from last_seen_at freshness.
func gatewayStatus(lastSeenAt *time.Time, thresholds domain.Thresholds) string {
	if lastSeenAt == nil {
		return "offline"
	}
	if time.Since(*lastSeenAt) > time.Duration(thresholds.StalePacketMinutes)*time.Minute {
		return "offline"
	}
	return "online"
}
