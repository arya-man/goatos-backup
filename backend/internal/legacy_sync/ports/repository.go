package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
)

var (
	ErrNotFound      = errors.New("legacy sync record not found")
	ErrInvalidCursor = errors.New("invalid legacy sync cursor")
	ErrWriteConflict = errors.New("legacy sync write conflict")
)

type SourceFilter struct {
	TenantID string
	Domain   string
}

type RunFilter struct {
	TenantID string
	Limit    int
}

type CreateRunParams struct {
	TenantID           string
	ActorID            string
	Mode               string
	Domain             string
	Status             string
	ColdStart          bool
	SourceWindowStart  *time.Time
	SourceWindowEnd    *time.Time
	ETASeconds         *int
	Summary            domain.RunSummary
	CountersRebuilt    bool
	CounterCheckStatus string
	FreshnessStatus    string
	BlockedReason      *string
	TraceID            string
	Steps              []CreateStepParams
}

type CreateStepParams struct {
	SourceID    *string
	StepName    string
	Status      string
	RowsRead    int
	RowsPlanned int
	RowsApplied int
	RowsSkipped int
	Details     map[string]any
	Completed   bool
}

type UpdateRunParams struct {
	TenantID string
	RunID    string
	Status   string
	Reason   *string
}

type Repository interface {
	ListSources(ctx context.Context, filter SourceFilter) ([]domain.Source, error)
	OverallCounterStatus(ctx context.Context, tenantID string) (domain.CounterStatus, error)
	HasSuccessWatermark(ctx context.Context, tenantID, domain string) (bool, error)
	CreateRun(ctx context.Context, params CreateRunParams) (*domain.Run, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]domain.Run, error)
	GetRun(ctx context.Context, tenantID, runID string) (*domain.RunDetailResponse, error)
	CancelRun(ctx context.Context, tenantID, runID string) (*domain.Run, error)
}
