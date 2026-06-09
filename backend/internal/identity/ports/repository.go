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
	ErrInvalidCursor       = errors.New("invalid pagination cursor")
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

type ListCandidatesParams struct {
	TenantID string
	Limit    int
	Cursor   *string
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

type ResolveConflictCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	ConflictID           string
	DecisionType         string
	DecisionResult       string
	SurvivorGoatID       string
	AffectedGoatIDs      []string
	IdentifierActions    []domain.IdentifierAction
	EvidenceRefs         []domain.EvidenceRef
	Reason               string
	RowVersion           int
}

type RejectCandidateCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	CandidateID          string
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

type ResolveConflictResult struct {
	ConflictID    string
	State         string
	Decision      domain.DecisionRecordSummary
	Merge         *domain.MergeResult
	Events        []domain.EventSummary
	Replayed      bool
	FirstResultID *string
}

type RejectCandidateResult struct {
	Candidate     domain.CandidateSummary
	Decision      domain.DecisionRecordSummary
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
	ListCandidates(ctx context.Context, params ListCandidatesParams) ([]domain.CandidateSummary, *string, error)
	CreateCorrectionRequest(ctx context.Context, cmd CreateCorrectionRequestCommand) (*CreateCorrectionRequestResult, error)
	ResolveCorrectionRequest(ctx context.Context, cmd ResolveCorrectionRequestCommand) (*ResolveCorrectionRequestResult, error)
	AddGoatIdentifier(ctx context.Context, cmd AddGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	RetireGoatIdentifier(ctx context.Context, cmd RetireGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	ResolveConflict(ctx context.Context, cmd ResolveConflictCommand) (*ResolveConflictResult, error)
	RejectCandidate(ctx context.Context, cmd RejectCandidateCommand) (*RejectCandidateResult, error)
	Ping(ctx context.Context) error
}
