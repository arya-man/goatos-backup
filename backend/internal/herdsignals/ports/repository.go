package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// Repository defines the data access interface for herd signals.
type Repository interface {
	// UpsertGateway updates or inserts a gateway.
	UpsertGateway(ctx context.Context, tenantID string, gw domain.Gateway) error

	// GetGatewaysByTenant fetches all gateways for a tenant.
	GetGatewaysByTenant(ctx context.Context, tenantID string) ([]domain.Gateway, error)

	// IngestPackets stores raw BLE packets and updates tag latest state.
	// Returns number of packets stored, number of latest rows updated, and error.
	IngestPackets(ctx context.Context, tenantID string, packets []domain.Packet) (stored, latestUpdated int, err error)

	// GetTagLatest fetches the current state of a single tag.
	GetTagLatest(ctx context.Context, tenantID, tagID string) (*domain.TagLatest, error)

	// ListTagsLatest fetches tags with optional filters, keyset pagination, and summary counts.
	// parkID, shedID: optional location filters. movementState: filter by "moving", "low", "quiet", "not_moving", "stale".
	// mapped: if true, only mapped tags; if false, only unmapped; nil = both.
	ListTagsLatest(ctx context.Context, tenantID string, parkID, shedID, movementState *string, mapped *bool, cursor string, limit int) (
		items []domain.TagLatest,
		summary domain.Summary,
		nextCursor *string,
		err error,
	)

	// ListActivityWindows fetches bucketed motion data for a tag over a date range.
	// bucketSeconds: defaults to 60 if 0.
	ListActivityWindows(ctx context.Context, tenantID, tagID string, from, to time.Time, bucketSeconds int) (
		windows []domain.ActivityWindow,
		err error,
	)

	// GetGoatIdentifier resolves a tag_id or tag_mac to a goat_id and metadata.
	// Returns nil, nil if not found. Requires smart_tag_capable=true.
	GetGoatIdentifier(ctx context.Context, tenantID, normalizedValue string) (*GoatIdentifierResult, error)

	// ResolveTagMapping attempts to resolve a tag to an animal.
	// Matching rules: tag_id OR tag_mac match against goat_identifiers.normalized_value,
	// same tenant_id, status='active', smart_tag_capable=true.
	// Returns "mapped" (single goat), "unmapped" (no match), "conflict" (multiple goats).
	ResolveTagMapping(ctx context.Context, tenantID string, tagID, tagMAC *string) (mappingState string, goatID *string, err error)

	// GetGoatsByIDs fetches display_id and location for multiple goats.
	GetGoatsByIDs(ctx context.Context, tenantID string, goatIDs []string) (
		goatData map[string]GoatData,
		err error,
	)

	// GetLocationsByIDs fetches shed and partition info for multiple location IDs.
	GetLocationsByIDs(ctx context.Context, tenantID string, locationIDs []string) (
		locData map[string]LocationData,
		err error,
	)
}

// GoatIdentifierResult is a single goat identifier match.
type GoatIdentifierResult struct {
	GoatID          string
	NormalizedValue string
	SmartTagCapable *bool
}

// GoatData is location and display info for a goat.
type GoatData struct {
	DisplayID string
	ShedID    *string
	ParkID    *string
}

// LocationData is shed/partition info for a location.
type LocationData struct {
	ShedName       string
	PartitionLabel *string
	ParkID         *string
}
