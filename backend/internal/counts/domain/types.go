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
	RecentEvidence    []ReadinessEvidence
}

type ReadinessEvidence struct {
	Status            ReadinessStatus
	EvidenceRef       string
	BlockerReason     string
	ImplementationRef string
	RecordedAt        time.Time
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

// HerdRegisterSummaryCounts is the canonical scoped herd KPI row.
type HerdRegisterSummaryCounts struct {
	ParkID            *string   `json:"parkId"`
	FarmID            *string   `json:"farmId"`
	CurrentLocationID *string   `json:"currentLocationId"`
	Breed             *string   `json:"breed"`
	Sex               string    `json:"sex"`
	LifecycleStatus   string    `json:"lifecycleStatus"`
	ActiveCount       int64     `json:"activeCount"`
	AdultCount        int64     `json:"adultCount"`
	KidCount          int64     `json:"kidCount"`
	UntaggedKidCount  int64     `json:"untaggedKidCount"`
	ProjectedAt       time.Time `json:"projectedAt"`
}

// HerdRegisterSummary is the exact scoped summary from canonical goats.
type HerdRegisterSummary struct {
	Items []HerdRegisterSummaryCounts
}

// HerdRegisterSummaryQuery is the query for herd register summary.
type HerdRegisterSummaryQuery struct {
	TenantID        string
	LifecycleStatus *string
	ParkID          *string
	Breed           *string
	Sex             *string
}

// CountsBreakdownRow is one census grain: farm x stage x breed x sex x shed.
//
// ManagementStage is raw goats.management_stage. That column has no CHECK constraint and is
// written verbatim from the source sheet, so near-duplicate labels ("ICU-Kid" vs "ICU-Kids")
// are reported as distinct rows on purpose — collapsing them here would hide a real data
// quality problem that count_dimension_aliases exists to fix at the source.
type CountsBreakdownRow struct {
	ParkID          *string `json:"park_id"`
	ParkLabel       string  `json:"park_label"`
	ShedID          *string `json:"shed_id"`
	ShedLabel       string  `json:"shed_label"`
	ManagementStage string  `json:"management_stage"`
	Breed           string  `json:"breed"`
	Sex             string  `json:"sex"`
	Count           int64   `json:"count"`
}

// CountsBreakdownSeriesPoint is one chart bar or one filter facet value.
type CountsBreakdownSeriesPoint struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// CountsBreakdownCharts holds the four distribution series. Each is rolled up over the FULL
// filtered grain set, never over the returned page.
type CountsBreakdownCharts struct {
	Breed []CountsBreakdownSeriesPoint `json:"breed"`
	Stage []CountsBreakdownSeriesPoint `json:"stage"`
	Sex   []CountsBreakdownSeriesPoint `json:"sex"`
	Shed  []CountsBreakdownSeriesPoint `json:"shed"`
}

// CountsBreakdownShedFacet is one shed filter option: a CountsBreakdownSeriesPoint plus the park
// that the counted animals sit in.
//
// The park identifier is NOT decoration. SHED NAMES ARE NOT UNIQUE ACROSS PARKS — in real seeded
// data 66 of 154 shed names exist in BOTH parks (there is a "Castro 1" under Coimbatore and a
// different "Castro 1" under Channapatna). A client that cascades Park -> Shed must therefore
// filter this list by ParkID; matching on Label would silently merge two physically distinct
// sheds into one option. Key stays the shed UUID so an entry is still unambiguous on its own,
// and ParkID is carried as its own field rather than smuggled into Label, so the client never
// has to parse a display string to recover an identifier.
type CountsBreakdownShedFacet struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
	// ParkID is the park these animals are in, empty only for animals with no park assigned.
	ParkID string `json:"park_id"`
}

// CountsBreakdownFacets reports the values actually present in the unfiltered tenant herd so a
// filter dropdown can never offer an option that matches zero rows. This matters for stage:
// animal_stage_lookup is joined to goats through shed_profiles, NOT through
// goats.management_stage, so the stage lookup and the stage column can legitimately disagree.
type CountsBreakdownFacets struct {
	Stages []CountsBreakdownSeriesPoint `json:"stages"`
	Breeds []CountsBreakdownSeriesPoint `json:"breeds"`
	// Parks keyed by park_id, labelled with the park code — the Farm filter's vocabulary.
	Parks []CountsBreakdownSeriesPoint `json:"parks"`
	// Sheds keyed by shed_id and carrying park_id — the Shed filter's vocabulary, cascaded from
	// the selected park. Unlike Charts.Shed (display-capped to the top 12 bars) this list is
	// COMPLETE: a filter dropdown that silently truncated would present a partial vocabulary as
	// the whole one and hide sheds the operator can legitimately select.
	Sheds []CountsBreakdownShedFacet `json:"sheds"`
}

// CountsBreakdown is the whole census breakdown payload: one page of grain rows plus
// page-independent totals, chart series, and filter facets.
type CountsBreakdown struct {
	Items      []CountsBreakdownRow `json:"items"`
	TotalRows  int64                `json:"total_rows"`
	TotalCount int64                `json:"total_count"`
	// TotalKids and TotalAdults always partition TotalCount exactly — an animal with an unknown
	// age band counts as an adult rather than falling out of both buckets.
	TotalKids   int64                 `json:"total_kids"`
	TotalAdults int64                 `json:"total_adults"`
	Charts      CountsBreakdownCharts `json:"charts"`
	Facets      CountsBreakdownFacets `json:"facets"`
	ProjectedAt time.Time             `json:"projected_at"`
}

// CountsBreakdownQuery filters and pages the census breakdown.
type CountsBreakdownQuery struct {
	TenantID        string
	LifecycleStatus *string
	ParkID          *string
	ShedID          *string
	ManagementStage *string
	Breed           *string
	Sex             *string
	Limit           int32
	Offset          int32
}

// ---------------------------------------------------------------------------
// Operator-facing shifting support types
// ---------------------------------------------------------------------------

// ShiftingDestinationCatalog is the bounded park -> shed option tree a field operator picks a
// shifting destination from.
//
// It is a CASCADE, not a flat shed list, and that is load-bearing rather than cosmetic: shed names
// REPEAT across parks (there is a "Castro 1" under both Coimbatore and Channapatna), so a flat list
// of shed names is genuinely ambiguous -- two different sheds render as the same string. Grouping
// under the park disambiguates them for a human, and every option still carries its own ShedID so
// the write is addressed by id and never by the displayed name.
//
// This is a bounded CONFIG CATALOG (2 parks, ~154 sheds for the current tenant), not a feed: it is
// fetched whole, cached on-device, and never paginated. See the guard annotations on the query.
type ShiftingDestinationCatalog struct {
	Parks []ShiftingDestinationPark
}

// ShiftingDestinationPark is one park and the sheds that belong to it.
//
// What the operator UI calls a "farm" IS this park: the location hierarchy has exactly two live
// levels (park -> shed) and no farm level, so there is deliberately no third tier here.
type ShiftingDestinationPark struct {
	ParkID string
	Name   string
	Sheds  []ShiftingDestinationShed
}

// ShiftingDestinationShed is one selectable destination shed.
type ShiftingDestinationShed struct {
	ShedID string
	Name   string
}

// GoatShiftingFact is the narrow set of canonical goat attributes needed to DERIVE a shifting
// impact when an operator moves a single animal and therefore has no reason to describe a cohort.
//
// It is deliberately not a passport read: it carries only the fields that map onto
// ShiftingEventImpact, so the single-animal path stays a cheap indexed lookup.
type GoatShiftingFact struct {
	GoatID string
	// BreedID is the canonical breed FK when the animal has one; nil when the goat carries only a
	// free-text breed or none at all.
	BreedID *string
	// BreedKey is the normalized grouping key, produced by the SAME normalization the counts alias
	// resolver uses so a derived impact groups with an imported one instead of forking the grain.
	BreedKey string
	// BreedLabel is the human label: the canonical breed name when resolvable, else the goat's
	// free-text breed, else the species. See the fallback note on GoatShiftingFacts.
	BreedLabel string
	StageTag   *string
	AgeClass   *string
	Sex        *string
	// ParkID / ShedID are the animal's CURRENT placement, used to backfill a shifting event's
	// source when the operator did not send one. The simplified Shifting screen deliberately stops
	// asking the operator to retype a location the server already knows, so without this the stored
	// event carried a blank source and the movement lost its "from" half.
	//
	// Nil when the animal has no placement recorded; the caller must treat that as "cannot derive"
	// and leave the source absent rather than storing an empty string.
	ParkID *string
	ShedID *string
}
