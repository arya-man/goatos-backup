// Package ports declares procurement/source-entry persistence boundaries.
package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

var (
	ErrNotFound          = errors.New("procurement: not found")
	ErrInvalidTransition = errors.New("procurement: invalid transition")
	// ErrIdempotencyConflict is returned when an idempotency key is replayed with a different request
	// payload (semantic fingerprint mismatch). The write must be rejected without mutating state.
	ErrIdempotencyConflict = errors.New("procurement: idempotency key reused with different payload")
)

type Repository interface {
	Ping(ctx context.Context) error
	ListLoads(ctx context.Context, q domain.LoadQuery) (domain.LoadListResult, error)
	CreateLoad(ctx context.Context, in CreateLoad) (domain.Load, error)
	GetLoadDetail(ctx context.Context, tenantID, loadID string) (domain.LoadDetail, error)
	AddGoatToLoad(ctx context.Context, in AddGoatToLoad) (domain.LoadGoat, error)
	RecordSourceHealth(ctx context.Context, in SourceHealth) (domain.SourceHealthCheck, error)
	RecordDecision(ctx context.Context, in Decision) (domain.Decision, error)
	DispatchLoad(ctx context.Context, in DispatchLoad) (domain.TransitHandoff, error)
	RecordArrivalReview(ctx context.Context, in ArrivalReview) (domain.ArrivalReview, error)
	AcceptIntake(ctx context.Context, in AcceptIntake) ([]domain.PHCHandoff, error)
	ListWorkRows(ctx context.Context, q domain.WorkQuery) (domain.WorkListResult, error)
	GetWorkRow(ctx context.Context, q domain.WorkQuery, rowID string) (domain.WorkRow, bool, error)
}

type CreateLoad struct {
	TenantID         string
	SourcePartyID    string
	SourceLocationID *string
	ExpectedCount    int
	PurchaseDate     *time.Time
	PlannedDispatch  *time.Time
	Notes            string
	Context          json.RawMessage
	IdempotencyKey   string
	ActorID          *string
}

type AddGoatToLoad struct {
	TenantID          string
	LoadID            string
	GoatID            *string
	SourceTag         *string
	SourceRFID        *string
	TemporaryID       *string
	SelectionState    string
	SelectionReason   string
	CurrentState      string
	IdentityState     string
	IdentityReviewRef *string
	OwnershipState    string
	HealthState       string
	WarmupStartedAt   *time.Time
	WarmupEndedAt     *time.Time
	WarmupDays        *int
	HoldingLocationID *string
	ProofRefs         json.RawMessage
	Metadata          json.RawMessage
	ActorID           *string
	IdempotencyKey    string
}

type SourceHealth struct {
	TenantID       string
	GoatID         string
	LoadID         string
	HealthState    string
	Reason         string
	CheckedBy      *string
	CheckedAt      time.Time
	ProofRefID     *string
	SOPTaskID      *string
	IdempotencyKey string
}

type Decision struct {
	TenantID        string
	GoatID          string
	LoadID          string
	DecisionStage   string
	DecisionType    string
	Reason          string
	DecidedBy       *string
	DecidedAt       time.Time
	ProofRefID      *string
	SOPTaskID       *string
	OwnerID         *string
	ResumeCondition *string
	Metadata        json.RawMessage
	IdempotencyKey  string
}

type DispatchLoad struct {
	TenantID       string
	LoadID         string
	FromLocationID *string
	ToLocationID   string
	GoatIDs        []string
	DispatchedAt   time.Time
	ArrivedAt      *time.Time
	ProofRefID     *string
	IdempotencyKey string
	ActorID        *string
}

type ArrivalReview struct {
	TenantID       string
	LoadID         string
	ParkLocationID string
	ExpectedCount  int
	LoadedCount    int
	ArrivedCount   int
	MatchedCount   int
	MissingCount   int
	ExtraCount     int
	RejectedCount  int
	HealthFlags    json.RawMessage
	WeightFlags    json.RawMessage
	MediaProofID   *string
	Status         string
	ReviewedBy     *string
	ReviewedAt     time.Time
	IdempotencyKey string
	Goats          []ArrivalGoat
}

type ArrivalGoat struct {
	GoatID       *string
	TemporaryID  *string
	SourceTag    *string
	ArrivalState string
	HealthFlag   *string
	WeightFlag   *string
	ProofRefID   *string
	Notes        string
}

type AcceptIntake struct {
	TenantID                  string
	LoadID                    string
	GoatIDs                   []string
	ParkLocationID            string
	ShedLocationID            string
	AcceptedAt                time.Time
	EntryDate                 time.Time
	TrustedVaccinationHistory json.RawMessage
	IntakeHealthSignal        *string
	IdempotencyKey            string
	ActorID                   *string
}

type VaccinationCanceler interface {
	CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error)
}
