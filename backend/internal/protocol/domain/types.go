// Package domain holds the protocol (ruleset) domain types.
package domain

import "time"

// Definition is a protocol definition (a category of ruleset, e.g. vaccination).
type Definition struct {
	ProtocolID string
	Code       string
	Name       string
	Category   string
	Status     string
	RowVersion int32
}

// NewDefinition is the input to create a Definition.
type NewDefinition struct {
	TenantID  string
	Code      string
	Name      string
	Category  string
	Status    string
	CreatedBy *string
}

// NewVersion is the input to create a protocol version. ScopeID is nil for tenant scope and a
// park location id for a park-scoped calendar. RuleDsl/ProofPolicy are JSON payloads.
type NewVersion struct {
	TenantID      string
	ProtocolID    string
	ScopeType     string
	ScopeID       *string
	Version       int32
	VersionLabel  string
	Status        string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	RuleDsl       []byte
	ProofPolicy   []byte
	SopVersionID  *string
	DraftedBy     *string
}

// Version is a stored protocol version.
type Version struct {
	ProtocolVersionID string
	ProtocolID        string
	Category          string
	ScopeType         string
	ScopeID           string
	Version           int32
	Status            string
	EffectiveFrom     *time.Time
	EffectiveTo       *time.Time
	RuleDsl           []byte
	ProofPolicy       []byte
	SopVersionID      string
	RowVersion        int32
}

// NewRule is the input to create one dose/phase rule under a version.
type NewRule struct {
	TenantID            string
	ProtocolVersionID   string
	DoseCode            string
	Sequence            int32
	TriggerType         string
	OffsetDays          int32
	DueWindowDays       int32
	MinGapDays          int32
	Repeat              string
	RepeatUntilAfterAge string
	CatchUp             string
	EligibilityJSON     []byte
	SopVersionID        *string
	ProofPolicy         []byte
	WithdrawalDays      *int32
	SortOrder           int32
}

// Rule is a stored dose/phase rule.
type Rule struct {
	RuleID              string
	DoseCode            string
	Sequence            int32
	TriggerType         string
	OffsetDays          int32
	DueWindowDays       int32
	MinGapDays          int32
	Repeat              string
	RepeatUntilAfterAge string
	CatchUp             string
	SortOrder           int32
}

// ConfigListItem is one row of the Config authority list (B3): a protocol version (any status)
// joined to its definition, with the rule-row count, source-review state lifted out of rule_dsl,
// linked SOP, effective window, and publisher/updated metadata. Read-only projection for /config.
type ConfigListItem struct {
	ProtocolID        string
	Code              string
	Name              string
	Category          string
	ProtocolVersionID string
	Version           int32
	VersionLabel      string
	ScopeType         string
	ScopeID           string
	Status            string // draft | published | retired
	EffectiveFrom     *time.Time
	EffectiveTo       *time.Time
	SopVersionID      string
	PublishedBy       string
	PublishedAt       *time.Time
	UpdatedAt         *time.Time
	SourceSystem      string // from rule_dsl.source — drives the publishable gate display
	SourceRef         string
	ReviewStatus      string
	ApprovedBy        string
	ApprovedAt        string
	RuleCount         int32
}

// AnimalStage is one active row of animal_stage_lookup — the tenant's stage reference data
// (e.g. K1 ≈ milk training, K2 ≈ milk drinking). The Config authoring stage picker is driven by
// these rows, never by frontend literals (PHC vaccination TRD: stage bands live in the lookup).
// MinAgeDays/MaxAgeDays are nil when the band is open-ended on that side.
type AnimalStage struct {
	AnimalStageID string
	StageCode     string
	Name          string
	MinAgeDays    *int32
	MaxAgeDays    *int32
	SortOrder     int32
}

// NewTrigger is the input to create a protocol trigger.
type NewTrigger struct {
	TenantID          string
	ProtocolVersionID string
	TriggerType       string
	TriggerConfig     []byte
	IsActive          bool
}
