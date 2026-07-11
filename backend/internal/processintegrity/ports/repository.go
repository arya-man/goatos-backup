// Package ports declares persistence boundaries for process-integrity reads.
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

type Repository interface {
	ListRows(ctx context.Context, q domain.Query) (domain.ListResult, error)
	CountByWorkState(ctx context.Context, q domain.Query) ([]domain.CountByWorkState, error)
	GetRow(ctx context.Context, q domain.Query, rowID string) (domain.Row, bool, error)
}
