package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
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
	summaries  *liveCohortCache[domain.Summary]

	riskFloor   time.Duration // re-evaluation floor (riskReevaluationFloor; tests may shorten)
	penMedianMu sync.Mutex
	penMedianAt map[string]time.Time // last live pen-median aggregate per tenant (classifier)
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
		summaries:  newLiveCohortCache[domain.Summary](),
		riskFloor:  riskReevaluationFloor,
	}
}

// InvalidateLive marks the cached live cohorts of one tenant stale; wired to the herd_signals_live
// NOTIFY so the next read revalidates.
func (s *Service) InvalidateLive(tenantID string) {
	s.summaries.invalidate(tenantID)
}

// InvalidateAllLive marks every cached live cohort stale (LISTEN reconnect catch-up).
func (s *Service) InvalidateAllLive() {
	s.summaries.invalidateAll()
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

	// Envelope-level gateway_seen_at is the RELAY time (when the gateway forwarded this batch to
	// us), used as a fallback only. It must never overwrite a packet's own gateway timestamp
	// (that was the bug: every packet in a batch of up to ~200 was stamped with this single
	// value, collapsing per-packet gateway clock data to one number, off by up to minutes).
	gatewaySeen, err := time.Parse(time.RFC3339, req.GatewaySeen)
	if err != nil {
		return domain.IngestResponse{}, fmt.Errorf("invalid gateway_seen_at: %w", err)
	}

	// Convert request packets to domain packets.
	//
	// serverNow is stamped ONCE for this whole ingest call and used as ReceivedAt for every
	// packet in it (security review, HIGH): staleness, gap detection, ordering, the
	// advance-only "latest" guard, and the packet dedup identity in this module all key off
	// ReceivedAt, so it must never come from the caller. A far-future caller-supplied value
	// would otherwise permanently freeze a tag's live state (the advance-only guard rejects
	// every subsequent real packet as "not newer"). All packets in one call share serverNow
	// because they were, in fact, received together -- that is a truthful timestamp, not an
	// approximation.
	serverNow := time.Now().UTC()

	packets := make([]domain.Packet, 0, len(req.Packets))
	for _, p := range req.Packets {
		// The caller's own claimed capture time is kept only for troubleshooting (DeviceSeenAt) and
		// as the dedup key's identity of "the same physical packet" (see migration 000196) --
		// never for a staleness/gap/ordering decision. A malformed or absent value degrades to
		// "no diagnostic timestamp available", not a reason to drop real sensor data: dropping
		// the packet would make the device's own clock a DoS lever over data we no longer trust
		// it for anyway.
		var deviceSeenAt *time.Time
		if parsed, err := time.Parse(time.RFC3339, p.SeenAt); err == nil {
			deviceSeenAt = &parsed
		} else {
			s.log.Warn("packet has invalid or missing seen_at; ingesting with no diagnostic device timestamp", "seen_at", p.SeenAt, "tag_id", p.TagID)
		}

		// Per-packet gateway timestamp when the caller sends one; falls back to the envelope's
		// batch relay time only when absent (older firmware). Diagnostic only, same as
		// DeviceSeenAt -- never used for ordering/gap/staleness decisions.
		packetGatewaySeen := gatewaySeen
		if p.GatewaySeenAt != nil && *p.GatewaySeenAt != "" {
			if parsed, err := time.Parse(time.RFC3339, *p.GatewaySeenAt); err == nil {
				packetGatewaySeen = parsed
			} else {
				s.log.Warn("invalid per-packet gateway_seen_at, falling back to envelope relay time", "gateway_seen_at", *p.GatewaySeenAt, "tag_id", p.TagID)
			}
		}

		packets = append(packets, domain.Packet{
			PacketID:              "", // DB will generate
			TenantID:              actor.TenantID,
			GatewayID:             &req.GatewayID,
			Source:                "gateway",
			TagID:                 p.TagID,
			TagMAC:                &p.TagMAC,
			ReceivedAt:            serverNow,
			DeviceSeenAt:          deviceSeenAt,
			GatewaySeenAt:         &packetGatewaySeen,
			RSSIdbm:               p.RSSI,
			BatteryMV:             p.Battery,
			TagTemperatureC:       p.TagTemperature,
			MotionCount:           p.MotionCount,
			SensorState:           p.SensorState,
			TemperatureSensorOK:   p.TemperatureSensorOK,
			AccelerometerSensorOK: p.AccelerometerSensorOK,
			PktSN:                 p.PktSN,
			RawAdv:                p.RawAdv,
			RawPayload:            rawPayloadOrEmpty(p.RawPayload),
		})
	}

	if len(packets) == 0 {
		return domain.IngestResponse{}, fmt.Errorf("no valid packets in request")
	}

	// The batch's highest scan-report sequence number drives the gateway's packet-loss
	// accounting (000198): the repository compares it to the stored value to accrue missed
	// reports on a forward jump, or count a gateway reboot on a decrease. Max, not last: packets
	// within one call are not ordered by sequence, and the anchor must only ever move forward
	// within a batch.
	var maxPktSN *int64
	for _, p := range packets {
		if p.PktSN == nil {
			continue
		}
		if maxPktSN == nil || *p.PktSN > *maxPktSN {
			v := *p.PktSN
			maxPktSN = &v
		}
	}

	gw := domain.Gateway{
		TenantID:   actor.TenantID,
		GatewayID:  req.GatewayID,
		Status:     "active",
		LastSeenAt: &gatewaySeen,
		LastPktSN:  maxPktSN,
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
func (s *Service) ListLive(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, liveState, mappingState, pattern, riskState, q *string, cursor string, limit int, sort domain.LiveSort) (domain.LiveResponse, error) {
	if actor.TenantID == "" {
		return domain.LiveResponse{}, fmt.Errorf("actor tenant_id required")
	}

	if riskState != nil && strings.HasPrefix(cursor, "risk.v1.") {
		cursor = "" // cursor from the retired in-memory risk pager: restart at the first page
	}
	// risk_state is the persisted classification (000402, written by RecomputeRisk): an indexed
	// predicate with keyset paging, never a whole-cohort walk.
	tags, nextCursor, err := s.repo.ListTagsLatestKeyset(
		ctx, actor.TenantID, parkID, shedID, movementState, liveState, mappingState, pattern, riskState, q, cursor, limit, sort,
	)
	if err != nil {
		s.log.Error("failed to list tags latest", "error", err)
		return domain.LiveResponse{}, fmt.Errorf("list tags failed: %w", err)
	}
	// Whole-filter summary: one aggregate query, cached per (tenant, filter) and invalidated by
	// the live NOTIFY, so N viewers / polls inside the window share one aggregate.
	summaryKey := liveCohortKey(actor.TenantID, parkID, shedID, mappingState, pattern, riskState, q)
	summary, err := s.summaries.get(ctx, summaryKey, domain.FreshLiveRead(ctx), func(ctx context.Context) (domain.Summary, error) {
		return s.repo.LiveSummary(ctx, actor.TenantID, parkID, shedID, mappingState, pattern, riskState, q)
	})
	if err != nil {
		s.log.Error("failed to compute live summary", "error", err)
		return domain.LiveResponse{}, fmt.Errorf("live summary failed: %w", err)
	}

	// Pen-group comparison baselines come from ONE whole-cohort aggregate query; only the
	// requested page is enriched. (This used to walk and enrich up to 50k tags per request just
	// to derive two medians per pen.)
	// Risk state, score, reasons and the deltas behind them all come from the persisted
	// classification (one source); nothing is re-scored per request.
	items := s.enrichTagsBatch(ctx, actor.TenantID, tags, nil, true)
	applyPersistedRisk(items, tags)

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
		s.log.Warn("failed to resolve gateway pen locations", "error", err)
		locations = map[string]ports.ShedLocation{}
	}

	tagStats, err := s.repo.GetGatewayTagStats(ctx, actor.TenantID)
	if err != nil {
		s.log.Warn("failed to compute gateway tag stats", "error", err)
		tagStats = map[string]ports.GatewayTagStats{}
	}

	windowStats, err := s.repo.GetGatewayWindowStats(ctx, actor.TenantID)
	if err != nil {
		s.log.Warn("failed to compute gateway window stats", "error", err)
		windowStats = map[string]ports.GatewayWindowStats{}
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

		item := domain.GatewayItem{
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
			Status: gatewayStatus(gw.LastSeenAt, s.thresholds),
		}
		// Populate computed stats, converting int to *int.
		if stat, ok := tagStats[gw.GatewayID]; ok {
			item.TagsSeenRecently = &stat.TagsSeenRecently
			item.WeakTags = &stat.WeakTags
			item.UnmappedTags = &stat.UnmappedTags
		}
		// Populate 15-minute window aggregates from herd_signal_activity_windows.
		if wstat, ok := windowStats[gw.GatewayID]; ok {
			item.TagsSeenInWindow = wstat.TagsSeenInWindow
			item.DistinctMotionDeltas = wstat.DistinctMotionDeltas
			item.PacketsReceivedInWindow = wstat.PacketsReceivedInWindow
		}
		items[i] = item
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

	// Label (Title Case), Formula (plain-English, not SQL) and SignalType (badge) below are ported
	// verbatim from the mock's INSIGHTS array (mock/herd-signals-mock.html) card-for-card, per
	// AGENTS.md design-authority rule -- admin-web renders this copy verbatim, so drift here is
	// drift on screen. Caveat text stays the richer backend-authored safety copy; the mock's own
	// caveat lines are shorter paraphrases of the same facts, not a stricter source of truth.
	cards := []domain.InsightCard{
		{
			Key: "tags_live_now", Label: "Tags Live Now", Value: fmt.Sprintf("%d", d.TagsLiveNow), Unit: "tags",
			SignalType: "derived", Formula: "distinct tags with last_seen_at within 5 min",
			Caveat: "Counts tags that have sent a packet recently; a tag with no packet in 30+ minutes is excluded, not shown as zero.",
		},
		{
			Key: "missing_signal", Label: "Missing Signal", Value: fmt.Sprintf("%d", d.MissingSignalCount), Unit: "tags",
			SignalType: "inferred", Formula: "mapped smart-tag animals not seen for 30+ min",
			Caveat: "No packet received for 30+ minutes. Distinct from inactive: inactive tags are still transmitting.",
		},
		{
			Key: "low_movement_watch", Label: "Low Movement Watch", Value: fmt.Sprintf("%d", d.LowMovementWatchCount), Unit: "tags",
			SignalType: "inferred", Formula: "60 min motion delta below pen baseline",
			Caveat: "Duration-based pattern over history, not a single reading. Not a health or behavior diagnosis.",
		},
		{
			Key: "high_movement_spike", Label: "High Movement Spike", Value: fmt.Sprintf("%d", d.HighMovementSpikeCount), Unit: "tags",
			SignalType: "inferred", Formula: "15m delta far above the animal/pen baseline",
			Caveat: "Baseline is per-animal; a naturally active animal's spike threshold is higher than a naturally quiet animal's.",
		},
		{
			Key: "shed_signal_coverage", Label: "Pen Signal Coverage", Value: fmt.Sprintf("%d/%d", d.ShedsWithCoverage, d.ShedsTotal), Unit: "sheds",
			SignalType: "correlated", Formula: "live mapped tags / smart-tag mapped animals per pen",
			Caveat: "A pen with no gateway deployed yet is excluded from the denominator, not counted as zero coverage.",
		},
		{
			Key: "weak_signal_tags", Label: "Weak Signal Tags", Value: fmt.Sprintf("%d", d.WeakSignalTagsCount), Unit: "tags",
			SignalType: "derived", Formula: "RSSI at or below the provisional threshold",
			Caveat: "RSSI reflects gateway placement and obstruction as much as tag health.",
		},
		{
			Key: "battery_attention", Label: "Battery Attention", Value: fmt.Sprintf("%d", d.BatteryAttentionCount), Unit: "tags",
			SignalType: "direct", Formula: "battery below configured mV threshold",
			Caveat: "Threshold is a provisional placeholder pending vendor discharge-curve confirmation.",
		},
		{
			Key: "post_vaccination_movement_watch", Label: "Post-Vaccination Movement Watch", Value: fmt.Sprintf("%d", d.PostVaccinationWatchCount), Unit: "animals",
			SignalType: "correlated", Formula: "0-24h activity delta after vaccination vs prior baseline",
			Caveat: "Correlation only -- reduced movement after vaccination is not a diagnosis, and is expected for many animals.",
		},
		{
			Key: "health_case_activity_trend", Label: "Health Case Activity Trend", Value: fmt.Sprintf("%d", d.HealthCaseActivityCount), Unit: "animals",
			SignalType: "correlated", Formula: "activity trend before / during an open treatment",
			Caveat: "Correlation only. An open health case does not mean the movement change is caused by it, or vice versa.",
		},
		{
			Key: "feed_activity", Label: "Feed × Activity", Value: fmt.Sprintf("%d", d.FeedActivityShedsCount), Unit: "sheds",
			SignalType: "correlated", Formula: "pen activity 2h before vs 2h after fed_at",
			Caveat: "Pen-grain only: this cannot attribute a single tag's motion to feeding.",
		},
		{
			Key: "weight_activity", Label: "Weight × Activity", Value: fmt.Sprintf("%d", d.WeightActivityTagsCount), Unit: "tags",
			SignalType: "correlated", Formula: "low ADG or weight drop together with low activity",
			Caveat: "Correlation only, by raw scanned string -- weighing is free-flow and never resolves a scan to goat identity (AGENTS.md), and this card does not either.",
		},
		{
			Key: "unmapped_smart_tags", Label: "Unmapped Smart Tags", Value: fmt.Sprintf("%d", d.UnmappedSmartTagsCount), Unit: "tags",
			SignalType: "derived", Formula: "BLE tags seen with no active smart-tag-capable identifier",
			Caveat: "A tag that has never been assigned to an active goat_identifiers row, or whose identifier is not smart_tag_capable.",
		},
	}

	return domain.InsightsResponse{Cards: cards}, nil
}

// enrichTagsBatch enriches a page of tags with animal mapping and location data using batched
// lookups (one query per lookup kind for the whole page, never one per row -- AGENTS.md
// operational read model contract).
func (s *Service) enrichTagsBatch(ctx context.Context, tenantID string, tags []domain.TagLatest, groupStats map[string]riskGroupStats, includeMotionDelta24h bool) []domain.LiveItem {
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
		s.log.Warn("failed to batch-fetch pen locations", "error", err)
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

	// Battery voltage trend: same batched-query discipline as baseline_delta above, one query
	// for the whole page.
	batteryHistory, err := s.repo.GetBatteryHistory(ctx, tenantID, tagIDs, s.thresholds.BatteryTrendWindowDays)
	if err != nil {
		s.log.Warn("failed to batch-fetch battery history", "error", err)
		batteryHistory = map[string]ports.BatteryHistoryPoint{}
	}

	for _, tag := range tags {

		// Voltage trend + the four-value battery_state (maintainer decision, replacing the
		// removed remaining-life estimate): compose the ABSOLUTE state already stored on the
		// tag (tag.BatteryState, from domain.BatteryStateFromVoltage at ingest) with the
		// RELATIVE trend and the read-time missing-signal state.
		var batteryTrendResp *domain.BatteryTrendResponse
		var trend *domain.BatteryTrend
		if hist, ok := batteryHistory[tag.TagID]; ok {
			firstMV, lastMV := hist.FirstMV, hist.LastMV
			firstAt, lastAt := hist.FirstAt, hist.LastAt
			trend = domain.BatteryTrendFromHistory(&firstMV, &lastMV, &firstAt, &lastAt, s.thresholds)
			if trend != nil {
				batteryTrendResp = &domain.BatteryTrendResponse{
					Direction: trend.Direction, WindowDays: trend.WindowDays,
					FirstMV: trend.FirstMV, FirstAt: trend.FirstAt,
					LastMV: trend.LastMV, LastAt: trend.LastAt,
				}
			}
		}
		composedBatteryState := domain.BatteryStateWithTrend(tag.BatteryState, trend, tag.PatternState == "missing")

		item := domain.LiveItem{
			TagID:                   tag.TagID,
			GatewayID:               tag.GatewayID,
			LastSeenAt:              tag.LastSeenAt.Format(time.RFC3339),
			RSSIdbm:                 tag.LastRSSIdbm,
			SignalState:             nullableEnum(tag.SignalState),
			BatteryMV:               tag.BatteryMV,
			BatteryState:            nullableEnum(composedBatteryState),
			BatteryTrend:            batteryTrendResp,
			TagTemperatureC:         tag.TagTemperatureC,
			MotionCount:             tag.MotionCount,
			LastPacketMotionDelta:   tag.LastPacketMotionDelta,
			LastPacketWindowSeconds: tag.LastPacketWindowSeconds,
			MotionDelta30s:          tag.MotionDelta30s,
			MotionDelta60s:          tag.MotionDelta60s,
			MotionDelta5m:           tag.MotionDelta5m,
			MotionDelta:             tag.MotionDelta,
			MotionDelta1h:           tag.MotionDelta1h, // real 1h (3600s-tier) delta -- defect 4 fix, was wrongly aliased to the 15m value
			MotionWindowSeconds:     tag.MotionWindowSeconds,
			MovementState:           nullableEnum(tag.MovementState),
			PatternState:            nullableEnum(tag.PatternState),
			SensorState:             sensorStateSummary(tag.TemperatureSensorOK, tag.AccelerometerSensorOK),
			TemperatureSensorOK:     tag.TemperatureSensorOK,
			AccelerometerSensorOK:   tag.AccelerometerSensorOK,
			MappingState:            tag.MappingState,
			GapDelta:                tag.GapDelta,
			RiskReasons:             []string{},
		}
		if tag.LastMovedAt != nil {
			lastMoved := tag.LastMovedAt.Format(time.RFC3339)
			item.LastMovedAt = &lastMoved
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
				item.AnimalIdentifier1 = gd.AnimalIdentifier1
				item.MappedBy = gd.MappedBy1
				item.MappedAt = gd.MappedAt1
				item.AnimalIdentifier2 = gd.AnimalIdentifier2
				item.Breed = gd.Breed
				item.Sex = gd.Sex
				item.AgeDays = gd.AgeDays
				item.ParkID = gd.ParkID
				item.ShedID = gd.ShedID
				if gd.ShedID != nil && *gd.ShedID != "" {
					// Risk pen = the partition the animal resides in (goat_shed_partitions via
					// GetGoatsByIDs), keyed like every other partition grouping.
					item.PenKey = penKey(*gd.ShedID, gd.PartitionLabel)
				}
				if gd.ShedID != nil {
					if loc, ok := shedLocations[*gd.ShedID]; ok {
						shedName := loc.ShedName
						item.ShedName = &shedName
						partitionLabel := loc.PartitionLabel
						if gd.PartitionLabel != nil && *gd.PartitionLabel != "" {
							partitionLabel = *gd.PartitionLabel
						}
						if partitionLabel != "" {
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
						opLoc := oploc.OperationalLocation{ShedName: loc.ShedName, PartitionLabel: partitionLabel}
						display := opLoc.Display()
						item.OperationalLocationDisplay = &display
					}
				}
			}
		}

		items = append(items, item)
	}

	if groupStats != nil {
		applyRiskSignals(items, groupStats)
	}
	if includeMotionDelta24h {
		s.populateMotionDeltas24h(ctx, tenantID, items)
	}
	return items
}

func (s *Service) populateMotionDeltas24h(ctx context.Context, tenantID string, items []domain.LiveItem) {
	if len(items) == 0 {
		return
	}
	tagIDs := make([]string, 0, len(items))
	for _, item := range items {
		tagIDs = append(tagIDs, item.TagID)
	}
	motionDeltas24h, err := s.repo.GetMotionDeltas24h(ctx, tenantID, tagIDs)
	if err != nil {
		s.log.Warn("failed to batch-fetch 24h motion deltas", "error", err)
		return
	}
	for i := range items {
		if delta24h, ok := motionDeltas24h[items[i].TagID]; ok {
			d := delta24h
			items[i].MotionDelta24h = &d
		}
	}
}

type riskGroupStats struct {
	motionMedian *float64
	tempMedian   *float64
}

func applyRiskSignals(items []domain.LiveItem, groupStats map[string]riskGroupStats) {
	for i := range items {
		reasons := make([]string, 0, 4)
		score := 0
		item := &items[i]

		if item.MotionDelta != nil && item.BaselineDelta != nil && *item.BaselineDelta > 0 && !item.GapDelta {
			baselineWindow := float64(*item.BaselineDelta) * 3
			if item.MotionWindowSeconds != nil && *item.MotionWindowSeconds > 0 {
				baselineWindow = float64(*item.BaselineDelta) * (float64(*item.MotionWindowSeconds) / 300)
			}
			pct := (float64(*item.MotionDelta) - baselineWindow) / baselineWindow * 100
			item.OwnMotionDeltaPct = &pct
			if pct <= -70 {
				score += 2
				reasons = append(reasons, "motion far below own baseline")
			} else if pct >= 150 {
				score++
				reasons = append(reasons, "motion spike vs own baseline")
			}
		}

		if item.PenKey != "" {
			if stats, ok := groupStats[item.PenKey]; ok {
				if item.MotionDelta != nil && stats.motionMedian != nil && *stats.motionMedian > 0 && !item.GapDelta {
					pct := (float64(*item.MotionDelta) - *stats.motionMedian) / *stats.motionMedian * 100
					item.GroupMotionDeltaPct = &pct
					if pct <= -70 {
						score++
						reasons = append(reasons, "motion lower than pen group")
					}
				}
				if item.TagTemperatureC != nil && stats.tempMedian != nil {
					delta := *item.TagTemperatureC - *stats.tempMedian
					item.GroupTempDeltaC = &delta
					if delta >= 1.5 {
						score++
						reasons = append(reasons, "tag temperature high vs pen group")
					}
				}
			}
		}

		if item.PatternState != nil {
			switch *item.PatternState {
			case "inactive", "missing":
				score += 2
				reasons = append(reasons, "persistent abnormal activity")
			case "quiet_watch", "spike":
				score++
				reasons = append(reasons, "activity pattern needs watch")
			}
		}
		if item.SensorState != nil && *item.SensorState == "abnormal" {
			score++
			reasons = append(reasons, "sensor abnormal")
		}

		if score <= 0 {
			continue
		}
		state := "low"
		if score >= 3 {
			state = "high"
		} else if score == 2 {
			state = "watch"
		}
		item.RiskState = &state
		item.RiskReasons = reasons
		item.RiskScore = score
	}
}

func medianFloat(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sort.Float64s(values)
	mid := len(values) / 2
	var out float64
	if len(values)%2 == 0 {
		out = (values[mid-1] + values[mid]) / 2
	} else {
		out = values[mid]
	}
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return nil
	}
	return &out
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

// rawPayloadOrEmpty defaults a packet's optional diagnostic RawPayload to an empty object
// (previous behaviour) rather than storing a JSON null.
func rawPayloadOrEmpty(p map[string]interface{}) map[string]interface{} {
	if p == nil {
		return map[string]interface{}{}
	}
	return p
}

// applyPersistedRisk sets each item's risk from its tag's persisted classification (000402):
// state, score, reasons and the own/pen deltas they were scored on, all from one source. A tag
// the classifier has not evaluated yet is marked RiskClassifying with no risk fields -- matching
// the risk_state filter and summary, which exclude it.
func applyPersistedRisk(items []domain.LiveItem, tags []domain.TagLatest) {
	for i := range items {
		t := tags[i]
		it := &items[i]
		it.OwnMotionDeltaPct, it.GroupMotionDeltaPct, it.GroupTempDeltaC = nil, nil, nil
		it.RiskState, it.RiskReasons, it.RiskScore = nil, []string{}, 0
		if t.RiskEvaluatedAt == nil {
			it.RiskClassifying = true
			continue
		}
		it.RiskClassifying = false
		it.RiskState = t.RiskState
		it.RiskReasons = append([]string{}, t.RiskReasons...)
		if t.RiskScore != nil {
			it.RiskScore = int(*t.RiskScore)
		}
		it.OwnMotionDeltaPct = t.RiskOwnMotionDeltaPct
		it.GroupMotionDeltaPct = t.RiskGroupMotionDeltaPct
		it.GroupTempDeltaC = t.RiskGroupTempDeltaC
	}
}

// penKey is the risk comparison group key: oploc.OperationalLocation.Key() of the animal's
// shed + its own partition (undivided sheds, 'whole', ” and NULL all key as the whole shed).
// ListLivePenMedians' SQL key mirrors it exactly.
func penKey(shedID string, partitionLabel *string) string {
	label := ""
	if partitionLabel != nil {
		label = *partitionLabel
	}
	return oploc.OperationalLocation{ShedID: shedID, PartitionLabel: label}.Key()
}
