// Package ports declares the seams the notification-audience module needs: the stored
// per-tenant override, the designation catalog, and the workforce device resolver it hands
// designations to.
package ports

import (
	"context"
	"errors"

	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// ErrUnknownAlert signals an alert key the catalog does not carry.
var ErrUnknownAlert = errors.New("unknown notification alert")

// ErrUnknownDesignation signals a designation code that is not an active designation_catalog
// row. Rejected rather than dropped: silently discarding a tick the admin made would leave an
// alert addressed to nobody without telling anyone.
var ErrUnknownDesignation = errors.New("unknown designation")

// ErrVersionConflict signals that the audience changed between the screen loading it and the
// save arriving. Surfaced as a 409 so the admin reloads and re-decides rather than silently
// clobbering another admin's edit.
var ErrVersionConflict = errors.New("this alert's audience was changed by someone else")

// Audience is one stored override.
type Audience struct {
	AlertKey         string
	DesignationCodes []string
	RowVersion       int
}

// Designation is one designation_catalog row offered as a column on the matrix.
type Designation struct {
	Code  string
	Label string
	Grade string
}

// AudienceRepository owns notification_alert_audiences and reads designation_catalog.
type AudienceRepository interface {
	// ListAudiences returns every customised alert for the tenant, keyed by alert key. An
	// alert with no row is not in the map: absence IS the catalog default.
	ListAudiences(ctx context.Context, tenantID string) (map[string]Audience, error)
	// LoadAudience returns one alert's override. ok=false means not customised.
	LoadAudience(ctx context.Context, tenantID, alertKey string) (Audience, bool, error)
	// ReplaceAudience writes the whole audience for one alert, fenced on the version the
	// screen loaded (0 for a not-yet-customised alert), and validates every code against the
	// active designation catalog inside the transaction.
	ReplaceAudience(ctx context.Context, cmd ReplaceAudienceCommand) (Audience, error)
	// ResetAudience deletes the override so the alert returns to its catalog default.
	ResetAudience(ctx context.Context, tenantID, actorID, alertKey string) error
	// ListDesignations returns the active designation catalog in its display order.
	ListDesignations(ctx context.Context) ([]Designation, error)
}

// ReplaceAudienceCommand is a validated whole-audience replacement for one alert.
type ReplaceAudienceCommand struct {
	TenantID           string
	ActorID            string
	AlertKey           string
	DesignationCodes   []string
	ExpectedRowVersion int
}

// PositionRecipientResolver is the slice of the workforce roster service that turns one
// designation at one scope into reachable devices. It is the SAME resolver every notifier
// already used; this module only decides which designations to ask it about.
type PositionRecipientResolver interface {
	ResolvePositionRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error)
}
