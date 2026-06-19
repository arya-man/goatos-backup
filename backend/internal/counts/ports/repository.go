package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

var (
	ErrInvalidFilter       = errors.New("invalid counts filter")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending  = errors.New("idempotency key is not completed")
	ErrSyncRunNotFound     = errors.New("counts sync run not found")
)

type DashboardParams struct {
	TenantID     string
	View         string
	SnapshotDate string
}

type SourceRowsParams struct {
	TenantID     string
	SnapshotDate string
}

type SourceRow struct {
	SourceRowID         string
	SourceSystem        string
	SourceID            string
	SourceTable         string
	SourceRowKey        string
	SourceWatermarkDate *string
	PayloadJSON         []byte
	PayloadHash         string
}

type SyncCommand struct {
	TenantID                 string
	ActorID                  string
	ClientIdempotencyKey     string
	StoredIdempotencyKey     string
	IdempotencyScope         string
	RequestHash              string
	TraceID                  string
	Mode                     string
	SnapshotDate             *string
	SourceRowsRead           int
	RowsSkipped              int
	UnresolvedLocationLabels int
	SourceUnavailable        bool
	UnavailableSources       []string
	SourceWatermark          *string
	SummarySourceDate        *string
	SnapshotRows             []SnapshotInputRow
	ProjectionRows           []ProjectionInputRow
}

type SnapshotInputRow struct {
	SnapshotDate         string
	SourceMode           string
	RowKind              string
	TabScope             *string
	FarmKey              *string
	FarmLabel            *string
	FarmID               *string
	ParkID               *string
	ShedKey              *string
	ShedLabel            *string
	ShedID               *string
	ResolvedLocationID   *string
	ResolvedLocationType *string
	StatusKey            *string
	StatusLabel          *string
	BreedKey             *string
	BreedLabel           *string
	AgeClass             *string
	Sex                  *string
	MetricName           string
	CountValue           *int64
	SourceRowID          string
	LogicalFactKey       string
	ProjectionInputHash  string
}

type ProjectionInputRow struct {
	ViewID                  string
	SnapshotDate            string
	SummarySourceDate       *string
	Section                 string
	Grain                   string
	DimensionKey            string
	DimensionLabel          string
	SecondaryDimensionKey   *string
	SecondaryDimensionLabel *string
	MetricKey               string
	CountValue              *int64
	NumericValue            *float64
	Unit                    string
	Denominator             *float64
	SortOrder               int
	SourceHash              string
	SourceComposition       string
}

type SyncMutationResult struct {
	Response      domain.CountsSyncRunResponse
	Replayed      bool
	FirstResultID *string
}

type Repository interface {
	GetDashboard(ctx context.Context, params DashboardParams, traceID string) (*domain.DashboardResponse, error)
	ListSourceRows(ctx context.Context, params SourceRowsParams) ([]SourceRow, error)
	RunSync(ctx context.Context, cmd SyncCommand) (*SyncMutationResult, error)
	GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.CountsSyncRunResponse, error)
}
