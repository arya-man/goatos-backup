package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
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

	// Upsert gateway last_seen_at
	gw := domain.Gateway{
		TenantID:   actor.TenantID,
		GatewayID:  req.GatewayID,
		Status:     "active",
		LastSeenAt: &gatewaySeen,
	}
	if err := s.repo.UpsertGateway(ctx, actor.TenantID, gw); err != nil {
		s.log.Error("failed to upsert gateway", "gateway_id", req.GatewayID, "error", err)
		return domain.IngestResponse{}, fmt.Errorf("upsert gateway failed: %w", err)
	}

	// Ingest packets and update tag latest
	stored, latestUpdated, err := s.repo.IngestPackets(ctx, actor.TenantID, packets)
	if err != nil {
		s.log.Error("failed to ingest packets", "gateway_id", req.GatewayID, "packet_count", len(packets), "error", err)
		return domain.IngestResponse{}, fmt.Errorf("ingest failed: %w", err)
	}

	return domain.IngestResponse{
		Accepted:      len(req.Packets),
		Stored:        stored,
		LatestUpdated: latestUpdated,
		TraceID:       "", // TODO: wire from request context
	}, nil
}

// ListLive fetches the current tag status with optional filters and pagination.
func (s *Service) ListLive(ctx context.Context, actor domain.Actor, parkID, shedID, movementState *string, mapped *bool, cursor string, limit int) (domain.LiveResponse, error) {
	if actor.TenantID == "" {
		return domain.LiveResponse{}, fmt.Errorf("actor tenant_id required")
	}

	tags, summary, nextCursor, err := s.repo.ListTagsLatest(
		ctx, actor.TenantID, parkID, shedID, movementState, mapped, cursor, limit,
	)
	if err != nil {
		s.log.Error("failed to list tags latest", "error", err)
		return domain.LiveResponse{}, fmt.Errorf("list tags failed: %w", err)
	}

	// Enrich items with animal mapping and location data
	items := make([]domain.LiveItem, 0, len(tags))
	for _, tag := range tags {
		item := s.enrichTagLatest(ctx, actor.TenantID, tag)
		items = append(items, item)
	}

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
		return domain.TimelineResponse{}, fmt.Errorf("invalid from timestamp: %w", err)
	}

	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return domain.TimelineResponse{}, fmt.Errorf("invalid to timestamp: %w", err)
	}

	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}

	windows, err := s.repo.ListActivityWindows(ctx, actor.TenantID, tagID, fromTime, toTime, bucketSeconds)
	if err != nil {
		s.log.Error("failed to list activity windows", "tag_id", tagID, "error", err)
		return domain.TimelineResponse{}, fmt.Errorf("list windows failed: %w", err)
	}

	tlWindows := make([]domain.TimelineWindow, len(windows))
	for i, w := range windows {
		tlWindows[i] = domain.TimelineWindow{
			BucketStart:   w.BucketStart,
			BucketSeconds: w.BucketSeconds,
			MotionDelta:   w.MotionDelta,
			PacketCount:   w.PacketCount,
			AvgRSSIdbm:    w.AvgRSSIdbm,
			FirstSeenAt:   w.FirstSeenAt,
			LastSeenAt:    w.LastSeenAt,
		}
	}

	return domain.TimelineResponse{
		TagID:   tagID,
		Windows: tlWindows,
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

	items := make([]domain.GatewayItem, len(gws))
	for i, gw := range gws {
		locDisplay := ""
		if gw.ShedID != nil {
			// TODO: resolve shed name from location
			locDisplay = *gw.ShedID
		}

		items[i] = domain.GatewayItem{
			GatewayID:       gw.GatewayID,
			Label:           &gw.Label,
			ParkID:          gw.ParkID,
			ShedID:          gw.ShedID,
			NetworkMode:     &gw.NetworkMode,
			LocationDisplay: &locDisplay,
			LastSeenAt:      gw.LastSeenAt,
			Status:          gw.Status,
		}
	}

	return domain.GatewaysResponse{Gateways: items}, nil
}

// enrichTagLatest enriches a tag with animal mapping and location data.
func (s *Service) enrichTagLatest(ctx context.Context, tenantID string, tag domain.TagLatest) domain.LiveItem {
	item := domain.LiveItem{
		TagID:                      tag.TagID,
		TagMAC:                     "",
		GoatID:                     nil,
		DisplayID:                  nil,
		ParkID:                     nil,
		ShedID:                     nil,
		ShedName:                   nil,
		PartitionLabel:             nil,
		OperationalLocationDisplay: nil,
		GatewayID:                  tag.GatewayID,
		LastSeenAt:                 tag.LastSeenAt.Format(time.RFC3339),
		RSSIdbm:                    tag.LastRSSIdbm,
		SignalState:                tag.SignalState,
		BatteryMV:                  tag.BatteryMV,
		BatteryState:               tag.BatteryState,
		TagTemperatureC:            tag.TagTemperatureC,
		MotionCount:                tag.MotionCount,
		MotionDelta:                tag.MotionDelta,
		MotionWindowSeconds:        tag.MotionWindowSeconds,
		MovementState:              tag.MovementState,
		TemperatureSensorOK:        tag.TemperatureSensorOK,
		AccelerometerSensorOK:      tag.AccelerometerSensorOK,
		MappingState:               tag.MappingState,
	}

	if tag.TagMAC != nil {
		item.TagMAC = *tag.TagMAC
	}

	// Resolve tag to animal (by tag_id or tag_mac)
	goatID, err := s.resolveGoatID(ctx, tenantID, &tag.TagID, tag.TagMAC)
	if err != nil {
		s.log.Warn("failed to resolve goat", "tag_id", tag.TagID, "error", err)
	}

	if goatID != nil {
		item.GoatID = goatID
		// TODO: Resolve display_id and location from goat
	}

	return item
}

// resolveGoatID attempts to map a tag to a goat_id.
func (s *Service) resolveGoatID(ctx context.Context, tenantID string, tagID, tagMAC *string) (*string, error) {
	_, goatID, err := s.repo.ResolveTagMapping(ctx, tenantID, tagID, tagMAC)
	return goatID, err
}
