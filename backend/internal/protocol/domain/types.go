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
	ScopeType         string
	ScopeID           string
	Version           int32
	Status            string
	EffectiveFrom     *time.Time
	EffectiveTo       *time.Time
	RuleDsl           []byte
	ProofPolicy       []byte
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

// NewTrigger is the input to create a protocol trigger.
type NewTrigger struct {
	TenantID          string
	ProtocolVersionID string
	TriggerType       string
	TriggerConfig     []byte
	IsActive          bool
}
