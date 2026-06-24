// Package ports defines vaccination-execution repository boundaries.
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type Repository interface {
	ListVaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error)
	VaccinationOperations(ctx context.Context, q domain.OperationsQuery) ([]domain.OperationsRow, error)
}
