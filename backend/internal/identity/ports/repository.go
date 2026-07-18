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
	ErrInvalidChronology              = errors.New("identity dob must be on or before entry_date")
	ErrFutureAnchor                   = errors.New("identity dob/entry_date cannot be in the future")
	ErrCriticalDeathGuardrailRequired = fmt.Errorf("%w: death exit", ErrGuardrailRequired)
	// ErrCrossParkMove: goats never move between parks (maintainer decision 2026-07-19).
	// Shed-to-shed movement exists only WITHIN one park; leaving a park is a terminal
	// exit (transferred/sold), never a move. Initial placement (a goat with no prior
	// park) is not a move and is never blocked by this rule.
	ErrCrossParkMove = errors.New("identity cross-park goat movement does not exist")
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

type ReproductiveGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ReproductiveStatus   string
	// BreedingDate and LastDeliveryDate are optional pregnancy-timing facts set on the same
	// transition. Nil means "leave the stored value untouched"; a value overwrites it. These feed
	// vaccination pregnancy defer/catch-up windows (see internal/vaccination schedule_policy).
	BreedingDate     *time.Time
	LastDeliveryDate *time.Time
	Reason           string
	OccurredAt       time.Time
	EvidenceRefs     []domain.EvidenceRef
	RowVersion       int
}

// IdentityGoatCommand corrects a goat's DOB and/or entry_date (a later data-entry fix). At least
// one of DOB / EntryDate must be set; nil means "leave the stored value untouched". Correcting these
// anchors changes vaccination age-routing (schedulePathForGoat) and post_arrival/birth_age due dates,
// so this command durably emits goat.identity.changed for the vaccination recheck consumer.
type IdentityGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	DOB                  *time.Time
	EntryDate            *time.Time
	Reason               string
	// OccurredAt is the SERVER processing instant used for recomputation, decision, and updated_at.
	// EffectiveAt is the optional client-supplied business effective date, retained for audit only —
	// it never drives the generation as_of or persisted timestamps (VACC-REV-07).
	OccurredAt   time.Time
	EffectiveAt  *time.Time
	EvidenceRefs []domain.EvidenceRef
	RowVersion   int
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

// ResolveReproductiveMatchCommand asks whether a bulk-import row's identifiers
// resolve to a single existing goat eligible for a reproductive update.
type ResolveReproductiveMatchCommand struct {
	TenantID    string
	Identifiers []AdminGoatCreateIdentifier
}

// ReproductiveMatchResult is the resolution outcome. Matched is true only for a
// single, active (non-merged, non-exited) goat. Blocked marks a matched-but-
// ineligible goat (merged or exited) so the caller can surface a clear reason.
type ReproductiveMatchResult struct {
	Matched                   bool
	Blocked                   bool
	BlockReason               string
	GoatID                    string
	CurrentReproductiveStatus string
	RowVersion                int
	MatchConfidence           float64
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
	ReproductiveGoat(ctx context.Context, cmd ReproductiveGoatCommand) (*AdminGoatMutationResult, error)
	IdentityGoat(ctx context.Context, cmd IdentityGoatCommand) (*AdminGoatMutationResult, error)
	Ping(ctx context.Context) error
}
