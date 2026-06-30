// Package ports defines Counts/Shifting repository boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

var (
	ErrIdempotencyConflict   = errors.New("counts: idempotency key reused with different payload")
	ErrIdempotencyInProgress = errors.New("counts: idempotency key is still in progress")
	ErrLogicalKeyConflict    = errors.New("counts: logical shifting event key reused with different payload")
)

type Repository interface {
	RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (id string, replay bool, err error)
	RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (id string, replay bool, err error)
	ProjectionInputs(ctx context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error)
	CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (id string, err error)
	CountAsOf(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	ProjectedCountFor(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	Readiness(ctx context.Context, tenantID string) (domain.Readiness, error)
}
