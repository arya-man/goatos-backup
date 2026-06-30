// Package ports defines Counts/Shifting repository boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

var (
	ErrIdempotencyConflict         = errors.New("counts: idempotency key reused with different payload")
	ErrIdempotencyInProgress       = errors.New("counts: idempotency key is still in progress")
	ErrLogicalKeyConflict          = errors.New("counts: logical shifting event key reused with different payload")
	ErrProjectionExceptionNotFound = errors.New("counts: projection exception not found")
	ErrProjectionExceptionClosed   = errors.New("counts: projection exception already closed")
)

type Repository interface {
	RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (id string, replay bool, err error)
	RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (id string, replay bool, err error)
	ScanCountMismatches(ctx context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error)
	ProjectionInputs(ctx context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error)
	CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (id string, err error)
	CountAsOf(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	ProjectedCountFor(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error)
	ListProjectionExceptions(ctx context.Context, req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionList, error)
	ResolveProjectionException(ctx context.Context, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, error)
	Readiness(ctx context.Context, tenantID string) (domain.Readiness, error)
}
