// Package ports declares the Growth Director module's outbound interfaces and
// error taxonomy, mirroring the weighing module's shape.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
)

var (
	ErrForbidden       = errors.New("growthdirector: forbidden")
	ErrInvalidArgument = errors.New("growthdirector: invalid argument")
	ErrNotFound        = errors.New("growthdirector: not found")
)

// Repository is the read-only data access the Growth Director service needs.
type Repository interface {
	// ListParks returns all active parks for a tenant, for resolving a
	// tenant-wide monitor's "all parks" scope.
	ListParks(ctx context.Context, tenantID string) ([]domain.Park, error)

	// GetGrowthDirectorWeights builds all six widgets for one half-open window
	// [periodStart, periodEnd). parkIDs must be non-empty and every id must
	// already be authorization-checked by the caller: this method does no
	// scoping of its own.
	//
	// Data rules it enforces (see domain docs):
	//   - identity = lower(btrim(scanned_identifier)); blank tags excluded
	//   - weighing_observations.animal_id / mismatch_status are NEVER referenced
	//     (both columns dropped)
	//   - tag -> goat via goat_identifiers.normalized_value = upper(tag_key)
	//     (normalized_value is UPPER; lifetime-unique per tenant, 0..1)
	//   - weighing_shed_observations reads filter withdrawn_at IS NULL
	//   - verification_status='rework' excluded from growth math, included in trust
	//   - feed quantity_kg NULL = blocked and is never COALESCEd to 0
	GetGrowthDirectorWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthDirectorWeights, error)
}
