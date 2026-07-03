package ports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

var (
	ErrNotFound                       = errors.New("identity record not found")
	ErrIdempotencyConflict            = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending             = errors.New("idempotency key is not completed")
	ErrWriteConflict                  = errors.New("identity write conflict")
	ErrInvalidReference               = errors.New("identity referenced record is invalid")
	ErrInvalidCursor                  = errors.New("invalid pagination cursor")
	ErrGuardrailRequired              = errors.New("identity critical transition requires guardrail")
	ErrCriticalDeathGuardrailRequired = fmt.Errorf("%w: death exit", ErrGuardrailRequired)
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

type GetGoatTimelineParams struct {
	TenantID string
	GoatID   string
	Limit    int
	Cursor   *string
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

type MoveGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ToParkID             string
	ToShedID             string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
	GuardrailApproved    bool
}

type ExitGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	LifecycleStatus      string
	ExitReason           string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
	GuardrailApproved    bool
}

type StageGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ManagementStage      string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type HealthGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	HealthStatus         string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type AdminGoatCreateIdentifier struct {
	IdentifierType  string
	IdentifierValue string
	NormalizedValue string
	ScopeKey        string
	IsPrimary       bool
}

type ValidateAdminGoatCreateCommand struct {
	TenantID             string
	StoredIdempotencyKey string
	RequestHash          string
	Identifiers          []AdminGoatCreateIdentifier
	FarmID               *string
	FarmCode             *string
	ParkID               *string
	ParkCode             *string
	ShedID               *string
	ShedCode             *string
	ManagementStage      *string
}

type AdminGoatCreateValidation struct {
	CustodianPartyID string
	FarmID           *string
	ParkID           string
	ShedID           string
	Conflicts        []domain.FieldError
	Warnings         []domain.Warning
}

type CreateAdminGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	Identifiers          []AdminGoatCreateIdentifier
	CustodianPartyID     string
	FarmID               *string
	ParkID               string
	ShedID               string
	Species              string
	Breed                *string
	Sex                  string
	DOB                  *time.Time
	DOBEstimated         bool
	OriginType           string
	EntryDate            time.Time
	ManagementStage      *string
	HealthStatus         *string
	WeightKg             *float64
	DamID                *string
	SireOrLot            *string
	PhotoURL             *string
	SourceRecordID       *string
	EvidenceRefs         []domain.EvidenceRef
}

type AdminGoatMutationResult struct {
	Goat             domain.GoatSummary
	Identifiers      []domain.GoatIdentifier
	Decision         domain.DecisionRecordSummary
	Events           []domain.EventSummary
	GenerationStatus string
	Replayed         bool
	FirstResultID    *string
}

type Repository interface {
	GetGoatByID(ctx context.Context, tenantID, goatID string) (*domain.GoatPassport, error)
	GetGoatByDisplayID(ctx context.Context, tenantID, displayID string) (*domain.GoatPassport, error)
	SearchGoats(ctx context.Context, params SearchGoatsParams) ([]domain.GoatSummary, *string, error)
	FindIdentifierMatches(ctx context.Context, params ResolveIdentifierParams) ([]domain.IdentifierMatch, error)
	FindOpenConflictForIdentifier(ctx context.Context, tenantID, identifierType, normalizedValue, scopeKey string) (*string, error)
	GetGoatTimeline(ctx context.Context, params GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error)
	AddGoatIdentifier(ctx context.Context, cmd AddGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	RetireGoatIdentifier(ctx context.Context, cmd RetireGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	MoveGoat(ctx context.Context, cmd MoveGoatCommand) (*AdminGoatMutationResult, error)
	ExitGoat(ctx context.Context, cmd ExitGoatCommand) (*AdminGoatMutationResult, error)
	StageGoat(ctx context.Context, cmd StageGoatCommand) (*AdminGoatMutationResult, error)
	HealthGoat(ctx context.Context, cmd HealthGoatCommand) (*AdminGoatMutationResult, error)
	Ping(ctx context.Context) error
}
