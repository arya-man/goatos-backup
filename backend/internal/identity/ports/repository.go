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
	// ErrBulkDecisionNotApplicable: the decision is invalid for at least one
	// selected conflict (wrong group, no single clean legacy value, etc.).
	ErrBulkDecisionNotApplicable = errors.New("bulk decision not applicable to a selected conflict")
	// ErrBulkBreedNotCanonical: use_legacy breed does not map to an approved
	// canonical breed; resolve the breed catalog before bulk-resolving.
	ErrBulkBreedNotCanonical = errors.New("legacy breed is not an approved canonical breed")
)

type SearchGoatsParams struct {
	TenantID       string
	Limit          int
	Cursor         *string
	Query          *string
	GoatID         *string
	IdentifierType *string
	ScopeKey       *string
	Breed          *string
	Sex            *string
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
	ReviewGroup  *string
}

// BulkResolveConflictsCommand resolves many conflicts with one human decision.
// DecisionType is one of keep_passport_value, use_legacy_value,
// acknowledge_lifecycle_flag. RowVersions maps conflict_id -> expected
// row_version (optimistic concurrency, per conflict). The whole batch aborts if
// any conflict is missing, stale, or invalid for the decision.
type BulkResolveConflictsCommand struct {
	TenantID      string
	ActorID       string
	TraceID       string
	BulkRequestID string
	DecisionType  string
	ConflictIDs   []string
	RowVersions   map[string]int
	Reason        string
}

type ListCandidatesParams struct {
	TenantID string
	Limit    int
	Cursor   *string
}

type ListImportRunRowsParams struct {
	TenantID        string
	ImportRunID     string
	Limit           int
	Cursor          *string
	ProcessingState *string
	ReasonCode      *string
}

type ListImportRunsParams struct {
	TenantID string
	Limit    int
}

type GetGoatTimelineParams struct {
	TenantID string
	GoatID   string
	Limit    int
	Cursor   *string
}

type ListCorrectionRequestsParams struct {
	TenantID  string
	Limit     int
	Cursor    *string
	CreatedBy *string
	State     *string
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
	BulkResolveConflicts(ctx context.Context, cmd BulkResolveConflictsCommand) (*domain.BulkResolveConflictsResult, error)
	GetConflict(ctx context.Context, tenantID, conflictID string) (*domain.ConflictDetailResult, error)
	ListCandidates(ctx context.Context, params ListCandidatesParams) ([]domain.CandidateSummary, *string, error)
	// CountReviewQueues returns the total open-conflict and actionable-candidate
	// counts for a tenant (dashboard cards). Indexed, bounded — not a herd scan.
	CountReviewQueues(ctx context.Context, tenantID string) (openConflicts int, openCandidates int, err error)
	GetImportRun(ctx context.Context, tenantID, importRunID string) (*domain.ImportRun, error)
	ListImportRuns(ctx context.Context, params ListImportRunsParams) ([]domain.ImportRun, error)
	ListImportRunRows(ctx context.Context, params ListImportRunRowsParams) ([]domain.ImportRunRow, *string, error)
	GetGoatTimeline(ctx context.Context, params GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error)
	ListCorrectionRequests(ctx context.Context, params ListCorrectionRequestsParams) ([]domain.CorrectionRequest, *string, error)
	CreateCorrectionRequest(ctx context.Context, cmd CreateCorrectionRequestCommand) (*CreateCorrectionRequestResult, error)
	ResolveCorrectionRequest(ctx context.Context, cmd ResolveCorrectionRequestCommand) (*ResolveCorrectionRequestResult, error)
	AddGoatIdentifier(ctx context.Context, cmd AddGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	RetireGoatIdentifier(ctx context.Context, cmd RetireGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	ResolveConflict(ctx context.Context, cmd ResolveConflictCommand) (*ResolveConflictResult, error)
	RejectCandidate(ctx context.Context, cmd RejectCandidateCommand) (*RejectCandidateResult, error)
	Ping(ctx context.Context) error
}
