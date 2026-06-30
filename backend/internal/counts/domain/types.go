// Package domain holds Counts/Shifting projection domain types.
package domain

import "time"

const (
	EventBaseCountAnchorRecorded    = "counts.base_count_anchor.recorded"
	EventShiftingEventRecorded      = "counts.shifting_event.recorded"
	EventProjectionExceptionOpened  = "counts.projection_exception.opened"
	EventProjectionExceptionUpdated = "counts.projection_exception.updated"
	EventProjectionExceptionClosed  = "counts.projection_exception.closed"
	SourceContractVersionV1         = "counts-shifting-v1"
)

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

type ProjectionRecomputeRequest struct {
	TenantID              string
	ParkID                string
	Horizon               string
	TargetDate            time.Time
	AsOf                  time.Time
	SourceContractVersion string
	GeneratedBy           string
	TraceID               *string
}

type ProjectionRecomputeResult struct {
	RunID            string
	SnapshotID       string
	Horizon          string
	TargetDate       time.Time
	AsOf             time.Time
	ProjectionStatus string
	RowCount         int
	ExceptionCount   int
}

type CountMismatchScanRequest struct {
	TenantID        string
	ParkID          *string
	ShedID          *string
	CountedAfter    *time.Time
	CountedBefore   time.Time
	CursorCountedAt *time.Time
	CursorAnchorID  *string
	Limit           int32
}

type CountMismatchScanResult struct {
	RunID                    string
	TenantID                 string
	Status                   string
	StartedAt                time.Time
	CompletedAt              *time.Time
	ScannedAnchorCount       int32
	ExceptionWriteCount      int32
	InvestigatingAnchorCount int32
	LastError                *string
	NextCursor               *CountMismatchScanCursor
}

type CountMismatchScanCursor struct {
	CountedAt         time.Time
	BaseCountAnchorID string
}

type ProjectionInputs struct {
	Anchors   []ProjectionBaseAnchor
	Movements []ProjectionMovementImpact
}

type ProjectionBaseAnchor struct {
	BaseCountAnchorID  string
	ParkID             string
	ShedID             string
	BreedID            *string
	BreedKey           string
	BreedLabel         string
	SourceSystem       string
	SourceBreedKey     string
	CountedAt          time.Time
	HeadCount          int32
	SourceHash         string
	AliasBlockerReason *string
}

type ProjectionMovementImpact struct {
	ShiftingEventID              string
	LogicalShiftingEventKey      string
	SourceSystem                 string
	SourceParkID                 *string
	SourceShedID                 *string
	DestinationParkID            string
	DestinationShedID            string
	EffectiveAt                  time.Time
	GrainKey                     string
	BreedID                      *string
	BreedKey                     string
	BreedLabel                   string
	SourceBreedKey               string
	StageTag                     *string
	SourceStageTag               *string
	AgeClass                     *string
	Sex                          *string
	HeadCount                    int32
	PregnantCount                int32
	LactatingCount               int32
	WarmupCount                  int32
	RationContextResolutionState string
	RationContextRef             *string
	BlockerReason                *string
	AliasBlockerReason           *string
}

type ProjectionRow struct {
	ProjectionRowID              string
	ParkID                       string
	ShedID                       string
	TargetDate                   time.Time
	GrainKey                     string
	BaseCountAnchorID            string
	IncludedShiftingEventIDsHash string
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

// ProjectionShedBreedTotal summarizes the returned projection page at the
// aggregate shed + breed grain Feed Direction needs for generation review.
type ProjectionShedBreedTotal struct {
	ParkID                       string
	ShedID                       string
	BreedKey                     string
	BreedLabel                   string
	HeadCount                    int32
	PregnantCount                int32
	LactatingCount               int32
	WarmupCount                  int32
	RationContextResolutionState string
}

type ProjectionException struct {
	ProjectionExceptionID string
	ProjectionSnapshotID  *string
	ExceptionType         string
	SourceKey             string
	GrainKey              string
	ParkID                *string
	ShedID                *string
	BreedKey              *string
	StageTag              *string
	Severity              string
	Status                string
	OwnerRef              *string
	WorkType              string
	WorkState             string
	DueAt                 time.Time
	NextAction            string
	EvidenceLink          string
	BlockerReason         string
	EvidenceJSON          []byte
	ResolutionID          *string
	ResolvedByRef         *string
	ResolutionReason      *string
	ResolutionRef         *string
	ResolvedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ProjectionExceptionCursor struct {
	UpdatedAt             time.Time
	ProjectionExceptionID string
}

type ProjectionExceptionQuery struct {
	TenantID      string
	Status        string
	ParkID        *string
	ShedID        *string
	ExceptionType *string
	Severity      *string
	OwnerRef      *string
	WorkState     *string
	Cursor        *ProjectionExceptionCursor
	Limit         int32
}

type ProjectionExceptionList struct {
	Items      []ProjectionException
	NextCursor *string
}

type ProjectionExceptionResolutionRequest struct {
	TenantID              string
	ProjectionExceptionID string
	Action                string
	ResolvedByRef         string
	ResolutionReason      string
	ResolutionRef         *string
	IdempotencyKey        string
	RequestFingerprint    string
}

type ProjectionExceptionResolution struct {
	ProjectionExceptionResolutionID string
	ProjectionExceptionID           string
	Action                          string
	Status                          string
	WorkState                       string
	ResolvedByRef                   string
	ResolutionReason                string
	ResolutionRef                   *string
	ResolvedAt                      time.Time
	Replayed                        bool
}

type CountProjectionRequest struct {
	TenantID                     string
	ParkID                       string
	AsOf                         time.Time
	TargetDate                   time.Time
	ShedID                       *string
	BreedKey                     *string
	RationContextResolutionState *string
	Cursor                       *string
	Limit                        int32
}

type CountProjection struct {
	TenantID              string
	Horizon               string
	ParkID                string
	TargetDate            time.Time
	SnapshotID            string
	ProjectionStatus      string
	SourceContractVersion string
	SourceHash            string
	BaseAnchorIDsHash     string
	ShiftingEventIDsHash  string
	ExceptionCount        int64
	TotalRowCount         int64
	Rows                  []ProjectionRow
	ShedBreedTotals       []ProjectionShedBreedTotal
	Exceptions            []ProjectionException
	Blockers              []ProjectionBlocker
	NextCursor            *string
}

type ProjectionBlocker struct {
	ExceptionType string
	SourceKey     string
	GrainKey      string
	Severity      string
	BlockerReason string
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
