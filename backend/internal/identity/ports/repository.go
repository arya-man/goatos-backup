package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

var (
	ErrNotFound            = errors.New("identity record not found")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending  = errors.New("idempotency key is not completed")
	ErrWriteConflict       = errors.New("identity write conflict")
)

type SearchGoatsParams struct {
	TenantID       string
	Limit          int
	Cursor         *string
	Query          *string
	IdentifierType *string
	ScopeKey       *string
	FarmID         *string
	ParkID         *string
	LocationID     *string
	Status         *string
}

type ResolveIdentifierParams struct {
	TenantID        string
	IdentifierType  string
	NormalizedValue string
	ScopeKey        *string
	FarmID          *string
	ParkID          *string
	LocationID      *string
}

type ListConflictsParams struct {
	TenantID     string
	Limit        int
	Cursor       *string
	State        *string
	ConflictType *string
}

type CountParams struct {
	Grain              string
	TenantID           string
	CustodianPartyID   *string
	FarmID             *string
	ParkID             *string
	ShedID             *string
	CohortID           *string
	LifecycleStatus    *string
	ReproductiveStatus *string
	GrowthCohortTag    *string
	ManagementStage    *string
	HealthStatus       *string
	IdentityState      *string
	BreedID            *string
	Sex                *string
}

type CreateCorrectionRequestCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	RequestType          string
	GoatID               *string
	IdentifierType       *string
	IdentifierValue      *string
	LocationScope        domain.LocationScope
	Description          string
	EvidenceRefs         []domain.EvidenceRef
}

type CreateCorrectionRequestResult struct {
	CorrectionRequest domain.CorrectionRequest
	Replayed          bool
	FirstResultID     *string
}

type ResolveCorrectionRequestCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	CorrectionRequestID  string
	TargetState          string
	Reason               string
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type ResolveCorrectionRequestResult struct {
	CorrectionRequest domain.CorrectionRequest
	Decision          domain.DecisionRecordSummary
	Replayed          bool
	FirstResultID     *string
}

type AddGoatIdentifierCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	IdentifierType       string
	IdentifierValue      string
	NormalizedValue      string
	ScopeKey             string
	IsPrimaryForGoat     bool
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
	Reason               string
}

type RetireGoatIdentifierCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	IdentifierID         string
	Reason               string
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type AdminGoatMutationResult struct {
	Goat          domain.GoatSummary
	Identifiers   []domain.GoatIdentifier
	Decision      domain.DecisionRecordSummary
	Events        []domain.EventSummary
	Replayed      bool
	FirstResultID *string
}

type Repository interface {
	GetGoatByID(ctx context.Context, tenantID, goatID string) (*domain.GoatPassport, error)
	GetGoatByDisplayID(ctx context.Context, tenantID, displayID string) (*domain.GoatPassport, error)
	SearchGoats(ctx context.Context, params SearchGoatsParams) ([]domain.GoatSummary, *string, error)
	FindIdentifierMatches(ctx context.Context, params ResolveIdentifierParams) ([]domain.IdentifierMatch, error)
	FindOpenConflictForIdentifier(ctx context.Context, tenantID, identifierType, normalizedValue, scopeKey string) (*string, error)
	ListConflicts(ctx context.Context, params ListConflictsParams) ([]domain.ConflictSummary, *string, error)
	GetConflict(ctx context.Context, tenantID, conflictID string) (*domain.ConflictDetailResult, error)
	ListIdentityCounts(ctx context.Context, params CountParams) ([]domain.IdentityCount, domain.Freshness, error)
	CreateCorrectionRequest(ctx context.Context, cmd CreateCorrectionRequestCommand) (*CreateCorrectionRequestResult, error)
	ResolveCorrectionRequest(ctx context.Context, cmd ResolveCorrectionRequestCommand) (*ResolveCorrectionRequestResult, error)
	AddGoatIdentifier(ctx context.Context, cmd AddGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	RetireGoatIdentifier(ctx context.Context, cmd RetireGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	Ping(ctx context.Context) error
}
