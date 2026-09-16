// Package ports declares what the Alerts service needs from storage and from the modules
// whose frozen rows it reads.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
)

// ErrIdempotencyConflict is a config write replayed under a known key with a different
// payload.
var ErrIdempotencyConflict = errors.New("alerts: idempotency key reused with a different payload")

// ConfigStore holds the per-tenant rule rows.
type ConfigStore interface {
	ListRuleConfig(ctx context.Context, tenantID string) ([]domain.StoredRuleConfig, error)
	UpsertRuleConfig(ctx context.Context, in domain.SetRuleConfig) error
}

// FeedSheetReader rolls one park's frozen feed sheet up to the pen grain for a feed day.
// A day with no issued sheet returns (nil, "", nil).
type FeedSheetReader interface {
	PenFeedDay(ctx context.Context, tenantID, parkID, feedDay string) (pens []domain.PenFeedDay, issuedAt string, err error)
}

// MovementReader lists the shifting events that touched a park between two instants.
type MovementReader interface {
	PenMovements(ctx context.Context, tenantID, parkID, fromInstant, toInstant string) ([]domain.PenMovement, error)
}

// LowStockReader is the feed module's own low-stock read, threshold in days.
type LowStockReader interface {
	LowStock(ctx context.Context, tenantID string, withinDays int) ([]domain.LowStockFeed, error)
}

// ParkNameReader resolves park ids to their names for farm-grain rows.
type ParkNameReader interface {
	ParkNames(ctx context.Context, tenantID string, parkIDs []string) (map[string]string, error)
}

// EventRuleStore holds the per-tenant user-defined event rules.
type EventRuleStore interface {
	ListEventRules(ctx context.Context, tenantID string) ([]domain.EventRule, error)
	// UpsertEventRule creates (blank ID) or updates one rule and returns it as stored.
	UpsertEventRule(ctx context.Context, in domain.SetEventRule) (domain.EventRule, error)
	DeleteEventRule(ctx context.Context, tenantID, ruleID string) error
}

// EventReader lists one kind's durable events for a park and Asia/Kolkata business day.
type EventReader interface {
	Events(ctx context.Context, tenantID, parkID string, kind domain.EventKind, businessDate string) (domain.EventPage, error)
}
