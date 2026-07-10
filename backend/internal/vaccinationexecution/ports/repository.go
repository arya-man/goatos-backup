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
}
