package domain

import "time"

const (
	GrainTenantLifecycle  = "tenant_lifecycle"
	GrainCustodianLife    = "custodian_lifecycle"
	GrainCustodianIdent   = "custodian_identity"
	GrainParkLifecycle    = "park_lifecycle"
	GrainShedLifecycle    = "shed_lifecycle"
	GrainBreedSexLife     = "breed_sex_lifecycle"
	GrainHealthStatus     = "health_status"
	GrainGrowthCohort     = "growth_cohort"
	GrainManagementStage  = "management_stage"
	GrainReproductiveStat = "reproductive_status"
)

var AllIdentityCounterGrains = []string{
	GrainTenantLifecycle,
	GrainCustodianLife,
	GrainCustodianIdent,
	GrainParkLifecycle,
	GrainShedLifecycle,
	GrainBreedSexLife,
	GrainHealthStatus,
	GrainGrowthCohort,
	GrainManagementStage,
	GrainReproductiveStat,
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

type CountDimensions struct {
	TenantID           string  `json:"tenant_id"`
	CustodianPartyID   *string `json:"custodian_party_id"`
	FarmID             *string `json:"farm_id"`
	FarmName           *string `json:"farm_name,omitempty"`
	ParkID             *string `json:"park_id"`
	ParkName           *string `json:"park_name,omitempty"`
	ShedID             *string `json:"shed_id"`
	ShedName           *string `json:"shed_name,omitempty"`
	CohortID           *string `json:"cohort_id"`
	CohortName         *string `json:"cohort_name,omitempty"`
	LifecycleStatus    *string `json:"lifecycle_status"`
	ReproductiveStatus *string `json:"reproductive_status"`
	GrowthCohortTag    *string `json:"growth_cohort_tag"`
	ManagementStage    *string `json:"management_stage"`
	HealthStatus       *string `json:"health_status"`
	IdentityState      *string `json:"identity_state"`
	BreedID            *string `json:"breed_id"`
	BreedName          *string `json:"breed_name,omitempty"`
	Sex                *string `json:"sex"`
}

type IdentityCount struct {
	CounterGrain      string          `json:"counter_grain"`
	Dimensions        CountDimensions `json:"dimensions"`
	CountValue        int64           `json:"count_value"`
	AsOfRecordedAt    *time.Time      `json:"as_of_recorded_at"`
	SourceImportRunID *string         `json:"source_import_run_id"`
	IsRebuilding      bool            `json:"is_rebuilding"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Freshness struct {
	AsOfRecordedAt    *time.Time `json:"as_of_recorded_at"`
	IsRebuilding      bool       `json:"is_rebuilding"`
	SourceImportRunID *string    `json:"source_import_run_id"`
	Warning           *string    `json:"warning"`
}

type IdentityCountsResult struct {
	Grain      string          `json:"grain"`
	Items      []IdentityCount `json:"items"`
	NextCursor *string         `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
	Freshness  Freshness       `json:"freshness"`
	TraceID    string          `json:"trace_id"`
}

type GrainRebuildResult struct {
	Grain        string `json:"grain"`
	DeletedRows  int64  `json:"deleted_rows"`
	InsertedRows int64  `json:"inserted_rows"`
}

type IdentityCounterRebuildResult struct {
	TenantID          string               `json:"tenant_id"`
	SourceImportRunID *string              `json:"source_import_run_id"`
	AsOfRecordedAt    *time.Time           `json:"as_of_recorded_at"`
	Grains            []GrainRebuildResult `json:"grains"`
}

type IncrementalCounterUpdateResult struct {
	TenantID            string  `json:"tenant_id"`
	ScannedEventCount   int     `json:"scanned_event_count"`
	AppliedEventCount   int     `json:"applied_event_count"`
	NoopEventCount      int     `json:"noop_event_count"`
	SkippedEventCount   int     `json:"skipped_event_count"`
	RebuildRequired     bool    `json:"rebuild_required"`
	RebuildReason       *string `json:"rebuild_reason"`
	PrunedProcessedRows int64   `json:"pruned_processed_rows"`
}
