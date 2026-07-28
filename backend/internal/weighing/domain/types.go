package domain

import "time"

const (
	StatusDraft      = "draft"
	StatusPublished  = "published"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"

	CategoryIndividualAnimal   = "individual_animal"
	CategoryPerShedPartition   = "per_shed_partition"
	AvailabilityExpectedShed   = "expected_shed"
	AvailabilityMovedOtherShed = "moved_other_shed"
)

type Actor struct {
	TenantID string
	UserID   string
	Roles    []string
}

type Campaign struct {
	CampaignID        string         `json:"campaign_id"`
	TenantID          string         `json:"tenant_id"`
	ParkID            string         `json:"park_id"`
	PeriodStartDate   string         `json:"period_start_date"`
	PeriodEndDate     string         `json:"period_end_date"`
	StartBusinessDate string         `json:"start_business_date"`
	Status            string         `json:"status"`
	PlannedCapPerDay  int            `json:"planned_cap_per_day"`
	OperatorUserID    string         `json:"operator_user_id"`
	CreatedBy         string         `json:"created_by"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	RowVersion        int            `json:"row_version"`
	Sheds             []CampaignShed `json:"sheds,omitempty"`
	Progress          Progress       `json:"progress"`
}

type CampaignPage struct {
	Items      []Campaign `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type PlannerCatalog struct {
	Parks     []PlannerPark     `json:"parks"`
	Operators []PlannerOperator `json:"operators"`
}

type PlannerPark struct {
	ParkID           string           `json:"park_id"`
	Name             string           `json:"name"`
	KidCount         int              `json:"kid_count"`
	Sheds            []PlannerShed    `json:"sheds"`
	ExistingCampaign *CampaignSummary `json:"existing_campaign,omitempty"`
}

type PlannerShed struct {
	LocationID string `json:"location_id"`
	Name       string `json:"name"`
	KidCount   int    `json:"kid_count"`
}

type CampaignSummary struct {
	CampaignID        string `json:"campaign_id"`
	Status            string `json:"status"`
	PeriodStartDate   string `json:"period_start_date"`
	PeriodEndDate     string `json:"period_end_date"`
	StartBusinessDate string `json:"start_business_date"`
	OperatorUserID    string `json:"operator_user_id"`
	ShedCount         int    `json:"shed_count"`
}

type PlannerOperator struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	DisplayCode string `json:"display_code"`
}

type CampaignShed struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	CampaignID          string `json:"campaign_id"`
	LocationID          string `json:"location_id"`
	LocationType        string `json:"location_type"`
	DisplayName         string `json:"display_name"`
	ExpectedAnimalCount int    `json:"expected_animal_count"`
	WeighingCategory    string `json:"weighing_category"`
	Status              string `json:"status"`
}

type ExpectedAnimal struct {
	CampaignID             string `json:"campaign_id"`
	CampaignShedID         string `json:"campaign_shed_id"`
	AnimalID               string `json:"animal_id"`
	DisplayAnimalID        string `json:"display_animal_id"`
	PrimaryIdentifier      string `json:"primary_identifier,omitempty"`
	SecondaryIdentifier    string `json:"secondary_identifier,omitempty"`
	ExpectedLocationID     string `json:"expected_location_id"`
	ExpectedLocationLabel  string `json:"expected_location_label"`
	Status                 string `json:"status"`
	AvailabilityStatus     string `json:"availability_status"`
	CurrentLocationID      string `json:"current_location_id,omitempty"`
	CurrentLocationLabel   string `json:"current_location_label,omitempty"`
	CurrentLifecycleStatus string `json:"current_lifecycle_status,omitempty"`
	Seq                    int64  `json:"seq"`
}

type RosterPage struct {
	Items      []ExpectedAnimal `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type Observation struct {
	ObservationID       string    `json:"observation_id"`
	CampaignID          string    `json:"campaign_id"`
	CampaignShedID      string    `json:"campaign_shed_id,omitempty"`
	AnimalID            string    `json:"animal_id,omitempty"`
	WeightKg            float64   `json:"weight_kg"`
	ProofArtifactID     string    `json:"proof_artifact_id"`
	ExpectedLocationID  string    `json:"expected_location_id,omitempty"`
	ActualLocationID    string    `json:"actual_location_id,omitempty"`
	ActualLocationLabel string    `json:"actual_location_label,omitempty"`
	AcceptedAt          time.Time `json:"accepted_at"`
}

type Progress struct {
	IndividualExpectedCount  int `json:"individual_expected_count"`
	IndividualCompletedCount int `json:"individual_completed_count"`
	PerScopeExpectedCount    int `json:"per_scope_expected_count"`
	PerScopeCompletedCount   int `json:"per_scope_completed_count"`
	WrongShedCount           int `json:"wrong_shed_count"`
	MissingCount             int `json:"missing_count"`
	RemainingCount           int `json:"remaining_count"`
}

type CreateCampaign struct {
	TenantID          string
	ParkID            string
	PeriodStartDate   string
	PeriodEndDate     string
	StartBusinessDate string
	PlannedCapPerDay  int
	OperatorUserID    string
	IdempotencyKey    string
	Sheds             []CreateCampaignShed
	CreatedBy         string
}

type UpdateCampaign = CreateCampaign

type CreateCampaignShed struct {
	LocationID       string `json:"location_id"`
	LocationType     string `json:"location_type"`
	DisplayName      string `json:"display_name"`
	WeighingCategory string `json:"weighing_category"`
}

type RecordAnimalObservation struct {
	TenantID         string
	CampaignID       string
	AnimalID         string
	WeightKg         float64
	ProofArtifactID  string
	ActualLocationID string
	IdempotencyKey   string
	RecordedBy       string
}

type RecordShedObservation struct {
	TenantID        string
	CampaignID      string
	CampaignShedID  string
	WeightKg        float64
	ProofArtifactID string
	IdempotencyKey  string
	RecordedBy      string
}
