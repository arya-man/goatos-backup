package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
)

var (
	ErrInvalidFilter       = errors.New("invalid locations filter")
	ErrNotFound            = errors.New("location not found")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending  = errors.New("idempotency key is not completed")
	ErrWriteConflict       = errors.New("locations write conflict")
	ErrBlockingUsage       = errors.New("location has blocking usage")
)

type ListParams struct {
	TenantID         string
	LocationType     string
	Status           string
	ParentLocationID string
	Search           string
	Alias            string
	Limit            int
	Offset           int
}

type ReviewParams struct {
	TenantID   string
	Status     string
	ReviewType string
	Limit      int
}

type SourceLabelQuery struct {
	TenantID              string
	SourceContext         string
	NormalizedSourceLabel string
}

type SourceLabelAliasMatch struct {
	Alias    domain.LocationAlias
	Location domain.LocationSummary
}

type IdempotencyResult struct {
	Replayed      bool
	FirstResultID *string
}

type CreateLocationCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationType         string
	LocationCode         *string
	Name                 string
	ParentLocationID     *string
	Status               string
	Country              string
	StateRegion          *string
	District             *string
	Pincode              *string
	Lat                  *float64
	Lng                  *float64
	Timezone             string
	Operational          domain.OperationalAttributes
}

type UpdateLocationCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	LocationType         *string
	LocationCode         *string
	Name                 *string
	ParentLocationID     *string
	ClearParent          bool
	Status               *string
	Country              *string
	StateRegion          *string
	District             *string
	Pincode              *string
	Lat                  *float64
	Lng                  *float64
	Timezone             *string
	Operational          *domain.OperationalAttributes
	RowVersion           int
}

type RetireLocationCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	Reason               string
	RowVersion           int
}

type DeleteLocationCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	Reason               string
	RowVersion           int
}

type CreateLocationAliasCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	AliasCode            string
	SourceContext        string
	Notes                *string
}

type UpdateLocationAliasCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	AliasID              string
	AliasCode            string
	SourceContext        string
	Notes                *string
	RowVersion           int
}

type RetireLocationAliasCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	AliasID              string
	Reason               string
	RowVersion           int
}

type DeleteLocationAliasCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	AliasID              string
	Reason               string
	RowVersion           int
}

type CreateLocationCapacityCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	CapacityKind         string
	CapacityValue        int
	EffectiveFrom        string
	EffectiveTo          *string
	Source               string
	SourceRef            *string
	Notes                *string
}

type UpdateLocationCapacityCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	CapacityRecordID     string
	CapacityKind         string
	CapacityValue        int
	EffectiveFrom        string
	EffectiveTo          *string
	Source               string
	SourceRef            *string
	Notes                *string
	RowVersion           int
}

type DeleteLocationCapacityCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	LocationID           string
	CapacityRecordID     string
	Reason               string
	RowVersion           int
}

type CreateLocationReviewItemCommand struct {
	TenantID              string
	ActorID               string
	ClientIdempotencyKey  string
	StoredIdempotencyKey  string
	IdempotencyScope      string
	RequestHash           string
	TraceID               string
	ReviewType            string
	SourceContext         *string
	SourceLabel           *string
	NormalizedSourceLabel *string
	CanonicalLocationID   *string
	CandidateLocationIDs  []string
	EvidenceJSON          []byte
	EvidenceHash          string
}

type OpenLocationReviewItemCommand struct {
	TenantID              string
	ActorID               string
	TraceID               string
	ReviewType            string
	SourceContext         string
	SourceLabel           string
	NormalizedSourceLabel string
	CanonicalLocationID   *string
	CandidateLocationIDs  []string
	EvidenceJSON          []byte
	EvidenceHash          string
}

type ResolveLocationReviewItemCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	ReviewID             string
	Status               string
	CanonicalLocationID  *string
	ResolutionNotes      string
	RowVersion           int
}

type LocationMutationResult struct {
	Location      domain.LocationSummary
	Replayed      bool
	FirstResultID *string
}

type LocationAliasMutationResult struct {
	Alias         domain.LocationAlias
	Replayed      bool
	FirstResultID *string
}

type LocationCapacityMutationResult struct {
	Capacity      domain.LocationCapacityRecord
	Replayed      bool
	FirstResultID *string
}

type LocationDeleteMutationResult struct {
	ResourceType  string
	ResourceID    string
	LocationID    *string
	Replayed      bool
	FirstResultID *string
}

type LocationReviewMutationResult struct {
	ReviewItem    domain.LocationReviewItem
	Replayed      bool
	FirstResultID *string
}

type OpenLocationReviewItemResult struct {
	ReviewItem domain.LocationReviewItem
	Reused     bool
}

type Repository interface {
	ListLocations(ctx context.Context, params ListParams) ([]domain.LocationSummary, error)
	GetLocation(ctx context.Context, tenantID, locationID string) (domain.LocationSummary, error)
	ListChildren(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationSummary, error)
	ListAliases(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationAlias, error)
	ListCapacity(ctx context.Context, tenantID, locationID string, limit int) ([]domain.LocationCapacityRecord, error)
	ListReviewItems(ctx context.Context, params ReviewParams) ([]domain.LocationReviewItem, error)
	Usage(ctx context.Context, tenantID, locationID string) (domain.LocationUsageResponse, error)
	FindActiveAliases(ctx context.Context, query SourceLabelQuery) ([]SourceLabelAliasMatch, error)
	CreateLocation(ctx context.Context, cmd CreateLocationCommand) (*LocationMutationResult, error)
	UpdateLocation(ctx context.Context, cmd UpdateLocationCommand) (*LocationMutationResult, error)
	RetireLocation(ctx context.Context, cmd RetireLocationCommand) (*LocationMutationResult, error)
	DeleteLocation(ctx context.Context, cmd DeleteLocationCommand) (*LocationDeleteMutationResult, error)
	CreateLocationAlias(ctx context.Context, cmd CreateLocationAliasCommand) (*LocationAliasMutationResult, error)
	UpdateLocationAlias(ctx context.Context, cmd UpdateLocationAliasCommand) (*LocationAliasMutationResult, error)
	RetireLocationAlias(ctx context.Context, cmd RetireLocationAliasCommand) (*LocationAliasMutationResult, error)
	DeleteLocationAlias(ctx context.Context, cmd DeleteLocationAliasCommand) (*LocationDeleteMutationResult, error)
	CreateLocationCapacity(ctx context.Context, cmd CreateLocationCapacityCommand) (*LocationCapacityMutationResult, error)
	UpdateLocationCapacity(ctx context.Context, cmd UpdateLocationCapacityCommand) (*LocationCapacityMutationResult, error)
	DeleteLocationCapacity(ctx context.Context, cmd DeleteLocationCapacityCommand) (*LocationDeleteMutationResult, error)
	CreateLocationReviewItem(ctx context.Context, cmd CreateLocationReviewItemCommand) (*LocationReviewMutationResult, error)
	OpenLocationReviewItem(ctx context.Context, cmd OpenLocationReviewItemCommand) (*OpenLocationReviewItemResult, error)
	ResolveLocationReviewItem(ctx context.Context, cmd ResolveLocationReviewItemCommand) (*LocationReviewMutationResult, error)
}
