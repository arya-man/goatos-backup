package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
)

type Repository interface {
	List(ctx context.Context, q domain.Query) ([]domain.AuditRow, *domain.Cursor, error)
	Summary(ctx context.Context, q domain.Query) (domain.SummaryResponse, error)
}
