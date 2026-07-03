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
	ErrStaleWrite        = errors.New("procurement: stale row version")
	// ErrIdempotencyConflict is returned when an idempotency key is replayed with a different request
	// payload (semantic fingerprint mismatch). The write must be rejected without mutating state.
	ErrIdempotencyConflict = errors.New("procurement: idempotency key reused with different payload")
	// ErrProofRequired is returned when HF vaccination evidence is reviewed as trusted but carries no
	// proof_ref_id. Trusted evidence can suppress a real post-arrival dose, so unproven evidence must
	// not be trusted.
	ErrProofRequired = errors.New("procurement: proof reference required")
	// ErrInvalidReference is returned when a write references a tenant-scoped entity that does not exist
	// (protocol version, rule, goat, load, or proof). Surfaced as a 400 instead of a raw FK 500.
	ErrInvalidReference = errors.New("procurement: referenced entity does not exist")
	// ErrSexMismatch is returned when source-entry tries to attach an existing canonical goat with a sex
	// that contradicts the canonical goat record. Sex is a hard female/male invariant, never inferred.
	ErrSexMismatch = errors.New("procurement: sex does not match existing goat")
)

type Repository interface {
	Ping(ctx context.Context) error
	ListLoads(ctx context.Context, q domain.LoadQuery) (domain.LoadListResult, error)
	CreateLoad(ctx context.Context, in CreateLoad) (domain.Load, error)
	GetLoadDetail(ctx context.Context, tenantID, loadID string) (domain.LoadDetail, error)
	AddGoatToLoad(ctx context.Context, in AddGoatToLoad) (domain.LoadGoat, error)
	GoatOnLoad(ctx context.Context, tenantID, loadID, goatID string) (bool, error)
	RecordSourceHealth(ctx context.Context, in SourceHealth) (domain.SourceHealthCheck, error)
	RecordDecision(ctx context.Context, in Decision) (domain.Decision, error)
	RecordHFVaccinationEvidence(ctx context.Context, in HFVaccinationEvidence) (domain.HFVaccinationEvidence, error)
	ReviewHFVaccinationEvidence(ctx context.Context, in ReviewHFVaccinationEvidence) (domain.HFVaccinationEvidence, error)
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
	AnimalIdentifier1 *string
	AnimalIdentifier2 *string
	Species           string
	Sex               string
	SelectionState    string
	SelectionReason   string
	Purpose           string
	CurrentState      string
	SourceEntryState  string
	SourceEntryRef    *string
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

type HFVaccinationEvidence struct {
	TenantID          string
	LoadID            string
	GoatID            string
	ProtocolVersionID string
	RuleID            string
	DoseCode          string
	AdministeredAt    time.Time
	VaccineName       string
	LotNumber         string
	ProofRefID        *string
	SourceRef         string
	Metadata          json.RawMessage
	ImportedBy        *string
	IdempotencyKey    string
}

type ReviewHFVaccinationEvidence struct {
	TenantID           string
	EvidenceID         string
	ExpectedRowVersion int
	ReviewStatus       string
	ReviewReason       string
	ReviewedBy         *string
	ReviewedAt         time.Time
	ReviewedAtSet      bool // true when the client supplied reviewed_at (participates in the fingerprint)
	IdempotencyKey     string
}

type SourceHealth struct {
	TenantID       string
	GoatID         string
	LoadID         string
	HealthState    string
	Reason         string
	CheckedBy      *string
	CheckedAt      time.Time
	CheckedAtSet   bool // true when the client supplied checked_at (so it participates in the fingerprint)
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
	DecidedAtSet    bool // true when the client supplied decided_at (participates in the fingerprint)
	ProofRefID      *string
	SOPTaskID       *string
	OwnerID         *string
	ResumeCondition *string
	Metadata        json.RawMessage
	IdempotencyKey  string
}

type DispatchLoad struct {
	TenantID        string
	LoadID          string
	FromLocationID  *string
	ToLocationID    string
	GoatIDs         []string
	DispatchedAt    time.Time
	DispatchedAtSet bool // true when the client supplied dispatched_at (participates in the fingerprint)
	ArrivedAt       *time.Time
	ProofRefID      *string
	IdempotencyKey  string
	ActorID         *string
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
	ReviewedAtSet  bool // true when the client supplied reviewed_at (participates in the fingerprint)
	IdempotencyKey string
	Goats          []ArrivalGoat
}

type ArrivalGoat struct {
	GoatID            *string
	AnimalIdentifier1 *string
	AnimalIdentifier2 *string
	ArrivalState      string
	HealthFlag        *string
	WeightFlag        *string
	ProofRefID        *string
	Notes             string
}

type AcceptIntake struct {
	TenantID                  string
	LoadID                    string
	GoatIDs                   []string
	ParkLocationID            string
	ShedLocationID            string
	AcceptedAt                time.Time
	AcceptedAtSet             bool // true when the client supplied accepted_at (participates in the fingerprint)
	EntryDate                 time.Time
	TrustedVaccinationHistory json.RawMessage
	IntakeHealthSignal        *string
	IdempotencyKey            string
	ActorID                   *string
}

type VaccinationCanceler interface {
	CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error)
}
