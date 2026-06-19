package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/mortality/domain"
)

var (
	ErrInvalidFilter       = errors.New("invalid mortality filter")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending  = errors.New("idempotency key is not completed")
)

type DashboardParams struct {
	TenantID string
	Period   string
}

type SourceRowsParams struct {
	TenantID string
}

type SourceRow struct {
	SourceRowID     string
	SourceSystem    string
	SourceTable     string
	SourceRowKey    string
	SourceWatermark *string
	PayloadJSON     []byte
	PayloadHash     string
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
	SourceRowsRead           int
	RowsSkipped              int
	DedupCandidateEvents     int
	UnresolvedLocationLabels int
	SourceUnavailable        bool
	UnavailableSources       []string
	SourceWatermark          *string
	Events                   []EventInputRow
	ProjectionRows           []ProjectionInputRow
}

type EventInputRow struct {
	LogicalEventKey            *string
	DedupCandidateKey          *string
	UnresolvedEventOrdinal     *int
	DedupConfidence            string
	EventType                  string
	EventDate                  string
	GoatID                     *string
	SourceGoatIdentifier       *string
	SourceIdentifierKind       *string
	AgeClass                   string
	BreedKey                   *string
	BreedLabel                 *string
	FarmKey                    *string
	FarmLabel                  *string
	HousingKey                 *string
	HousingLabel               *string
	CanonicalFarmLocationID    *string
	CanonicalParkLocationID    *string
	CanonicalShedLocationID    *string
	CanonicalHousingLocationID *string
	LoadKey                    *string
	LoadLabel                  *string
	DeliveryKey                *string
	DeliveryLabel              *string
	Sex                        *string
	SourceRowID                string
	EventHash                  string
	ReviewStatus               string
	IdempotencyKey             string
}

type ProjectionInputRow struct {
	Period                       string
	PeriodStart                  *string
	PeriodEnd                    *string
	Section                      string
	Grain                        string
	DimensionKey                 string
	DimensionLabel               string
	MetricKey                    string
	Numerator                    *float64
	Denominator                  *float64
	DenominatorSourceModule      *string
	DenominatorProjectionVersion *int64
	DenominatorSourceWatermark   *string
	NumeratorSourceComposition   *string
	DenominatorSourceComposition *string
	Value                        float64
	Unit                         string
	SortOrder                    int
	SourceHash                   string
	SourceComposition            string
}

type SyncMutationResult struct {
	Response      domain.MortalitySyncRunResponse
	Replayed      bool
	FirstResultID *string
}

type Repository interface {
	GetDashboard(ctx context.Context, params DashboardParams, traceID string) (*domain.DashboardResponse, error)
	ListSourceRows(ctx context.Context, params SourceRowsParams) ([]SourceRow, error)
	RunSync(ctx context.Context, cmd SyncCommand) (*SyncMutationResult, error)
	GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.MortalitySyncRunResponse, error)
}
