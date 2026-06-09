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
	ParkID             *string `json:"park_id"`
	ShedID             *string `json:"shed_id"`
	CohortID           *string `json:"cohort_id"`
	LifecycleStatus    *string `json:"lifecycle_status"`
	ReproductiveStatus *string `json:"reproductive_status"`
	GrowthCohortTag    *string `json:"growth_cohort_tag"`
	ManagementStage    *string `json:"management_stage"`
	HealthStatus       *string `json:"health_status"`
	IdentityState      *string `json:"identity_state"`
	BreedID            *string `json:"breed_id"`
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
