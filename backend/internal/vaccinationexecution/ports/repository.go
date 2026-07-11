// Package ports defines vaccination-execution repository boundaries.
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type Repository interface {
	ListVaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error)
	VaccinationOperations(ctx context.Context, q domain.OperationsQuery) ([]domain.OperationsRow, error)
	ScanRoster(ctx context.Context, q domain.ScanRosterQuery) ([]domain.ScanRosterRow, error)
	// VaccinationGaps returns the bounded, keyset-paginated per-animal exclusion rows for the mobile data
	// gaps overlay. VaccinationGapsSummary returns the cheap scoped reason-count aggregate (<= 2 groups).
	VaccinationGaps(ctx context.Context, q domain.GapsQuery) ([]domain.GapProjectionRow, error)
	VaccinationGapsSummary(ctx context.Context, q domain.GapsQuery) ([]domain.GapReasonCount, error)
	// ShedSummary returns the filtered, offset-paginated shed-wise rollup rows with ANIMAL-LEVEL Due
	// counts and a window total for pagination. Manager/Backup are attached by the service via the
	// ShedOwnershipReader port, not by this query.
	ShedSummary(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error)
	// ShedAnimals returns the shed's alive animals (Display ID + the two tag identities + an animal-level
	// status), keyset-paginated by goat_id, for the shed-detail roster.
	ShedAnimals(ctx context.Context, q domain.ShedAnimalQuery) ([]domain.ShedAnimalRow, error)
	// CapacityConfig returns the tenant's daily vaccination cap config (falls back to the code default
	// when no row is authored). Drives session-splitting in ShedSummary and the shed-detail plan.
	CapacityConfig(ctx context.Context, tenantID string) (domain.CapacityConfig, error)
}
