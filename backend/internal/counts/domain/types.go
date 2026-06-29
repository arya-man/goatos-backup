// Package domain holds Counts/Shifting projection domain types.
package domain

import "time"

type ReadinessStatus string

const (
	ReadinessReady   ReadinessStatus = "ready"
	ReadinessBlocked ReadinessStatus = "blocked"
	ReadinessPending ReadinessStatus = "pending"
)

// BaseCountAnchor is the physical count anchor consumed by the projection worker.
type BaseCountAnchor struct {
	TenantID           string
	ParkID             string
	ShedID             string
	BreedID            *string
	BreedKey           string
	BreedLabel         string
	CountedAt          time.Time
	HeadCount          int32
	SourceSystem       string
	SourceRef          string
	SourceHash         string
	DiscrepancyState   string
	IdempotencyKey     string
	RequestFingerprint string
	RecordedBy         *string
}

// ShiftingEvent is the append-only movement header used by count projections.
type ShiftingEvent struct {
	TenantID                string
	LogicalShiftingEventKey string
	Priority                string
	Category                string
	SourceParkID            *string
	SourceShedID            *string
	DestinationParkID       string
	DestinationShedID       string
	RaisedAt                time.Time
	EffectiveAt             time.Time
	AuthorizedAt            *time.Time
	AuthorizedBy            *string
	AuthorizationState      string
	VerificationState       string
	EventStatus             string
	SourceSystem            string
	SourceRef               string
	ProofRef                *string
	PayloadHash             string
	IdempotencyKey          string
	RequestFingerprint      string
	Impacts                 []ShiftingEventImpact
}

// ShiftingEventImpact is the structured cohort/stage effect of one movement.
type ShiftingEventImpact struct {
	GrainKey                     string
	BreedID                      *string
	BreedKey                     string
	BreedLabel                   string
	StageTag                     *string
	AgeClass                     *string
	Sex                          *string
	HeadCount                    int32
	PregnantCount                int32
	LactatingCount               int32
	WarmupCount                  int32
	RiskFlagsJSON                []byte
	RationContextResolutionState string
	RationContextRef             *string
	BlockerReason                *string
}

// ProjectionSnapshot is an immutable count snapshot consumed by Feed Direction.
type ProjectionSnapshot struct {
	TenantID              string
	Horizon               string
	ParkID                string
	TargetDate            time.Time
	AsOf                  time.Time
	ProjectionStatus      string
	SourceContractVersion string
	SourceHash            string
	BaseAnchorIDsHash     string
	ShiftingEventIDsHash  string
	GeneratedBy           string
	TraceID               *string
	Rows                  []ProjectionRow
	Exceptions            []ProjectionException
}

type ProjectionRow struct {
	ParkID                       string
	ShedID                       string
	TargetDate                   time.Time
	GrainKey                     string
	BreedID                      *string
	BreedKey                     string
	BreedLabel                   string
	StageTag                     *string
	AgeClass                     *string
	Sex                          *string
	HeadCount                    int32
	PregnantCount                int32
	LactatingCount               int32
	WarmupCount                  int32
	RationContextResolutionState string
	RationContextRef             *string
	BlockerReason                *string
	SourceRowHash                string
}

type ProjectionException struct {
	ExceptionType string
	SourceKey     string
	GrainKey      string
	ParkID        *string
	ShedID        *string
	BreedKey      *string
	StageTag      *string
	Severity      string
	OwnerRef      *string
	BlockerReason string
	EvidenceJSON  []byte
}

type ReadinessSubgate struct {
	ID                string
	Status            ReadinessStatus
	Owner             string
	EvidenceRef       string
	BlockerReason     string
	ImplementationRef string
	LastCheckedAt     time.Time
}

type Readiness struct {
	TenantID                 string
	Status                   ReadinessStatus
	GenerationAllowed        bool
	Subgates                 []ReadinessSubgate
	OpenExceptionCount       int64
	LatestProjectionStatus   string
	LatestProjectionTarget   *time.Time
	LatestProjectionRowCount int64
}
