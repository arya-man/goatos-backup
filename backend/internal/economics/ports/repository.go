// Package ports declares the Business Economics module's outbound interfaces
// and error taxonomy, mirroring the Growth Director module's shape.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
)

var (
	ErrForbidden       = errors.New("economics: forbidden")
	ErrInvalidArgument = errors.New("economics: invalid argument")
	ErrNotFound        = errors.New("economics: not found")
)

// Repository is the read-only data access the Business Economics service needs.
type Repository interface {
	// ListParks returns all active parks for a tenant, for resolving a
	// tenant-wide caller's "all parks" scope.
	ListParks(ctx context.Context, tenantID string) ([]domain.Park, error)

	// GetBusinessEconomics builds the whole Sales → Economics page for one
	// half-open window [periodStart, periodEnd). parkIDs must be non-empty and
	// every id must already be authorization-checked by the caller: this method
	// does no scoping of its own.
	//
	// Data rules it enforces (see domain docs):
	//   - identity = lower(btrim(scanned_identifier)); blank tags excluded;
	//     rework captures excluded from all growth math
	//   - tag -> goat via goat_identifiers.normalized_value = upper(tag_key)
	//     (lifetime-unique per tenant, 0..1), one-hop merge redirect
	//   - feed quantity_kg NULL = blocked and is never COALESCEd to 0
	//   - a feed item with no purchase on record prices NOTHING and is counted
	//     in pulse.unpriced_feed_items instead of being invented
	//   - deal figures (realized price, sold revenue) are tenant-wide: the
	//     sales ledger records a farm label, not a park id (deal grain only —
	//     there is no per-animal sale panel; see the domain doc)
	GetBusinessEconomics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.BusinessEconomics, error)
}
