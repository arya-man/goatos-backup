// Package domain holds the feed-direction module domain types (per-shed feed execution records).
package domain

import "time"

// NewDirection is the input to record one shed feed-direction execution against an obligation.
// QuantityFed/QuantityUnit are optional ("" = unset). BatchID/lot/submission/ration are optional.
type NewDirection struct {
	TenantID                string
	ObligationID            string
	BatchID                 *string
	ShedID                  string
	RationProtocolVersionID *string
	SopSubmissionItemID     *string
	FeedInventoryLotID      *string
	QuantityFed             string // decimal string; "" = null
	QuantityUnit            string
	HeadCount               *int32
	FedAt                   time.Time
	Status                  string // defaults to "recorded"
	RecordedBy              *string
	IdempotencyKey          string
}

// AcceptedDirection is the verification context returned when a recorded direction is accepted:
// enough to complete the obligation (SM-5) and later consume the reserved feed stock. BatchID/LotID
// are "" when the direction was not part of a drive (no stock to consume).
type AcceptedDirection struct {
	ObligationID       string
	ShedID             string
	BatchID            string
	FeedInventoryLotID string
	QuantityFed        string
	QuantityUnit       string
	FedAt              time.Time
}

// DirectionHistoryItem is one row of a shed's feed history.
type DirectionHistoryItem struct {
	CompletionID string
	ObligationID string
	BatchID      string
	FedAt        time.Time
	Status       string
	QuantityFed  string
	QuantityUnit string
	HeadCount    int32
}

// RecordedDirection is one direction awaiting review (Verification queue).
type RecordedDirection struct {
	CompletionID string
	ObligationID string
	ShedID       string
	BatchID      string
	FedAt        time.Time
	QuantityFed  string
	QuantityUnit string
	HeadCount    int32
}

// ReadinessStatus is the public gate state for Feed Direction build/readiness.
type ReadinessStatus string

const (
	ReadinessReady   ReadinessStatus = "ready"
	ReadinessBlocked ReadinessStatus = "blocked"
	ReadinessPending ReadinessStatus = "pending"
)

// ReadinessGate is one Feed Direction closure gate (G1-G17).
type ReadinessGate struct {
	ID             string          `json:"id"`
	Sequence       int             `json:"sequence"`
	Name           string          `json:"name"`
	Status         ReadinessStatus `json:"status"`
	Owner          string          `json:"owner"`
	EvidenceRef    string          `json:"evidence_ref"`
	BlockerReason  string          `json:"blocker_reason,omitempty"`
	LastCheckedAt  time.Time       `json:"last_checked_at"`
	AllowsBuild    bool            `json:"allows_build"`
	AllowsGenerate bool            `json:"allows_generate"`
}

// CountsShiftingSubgate is the G2 subgate roll-up (CSG1-CSG10).
type CountsShiftingSubgate struct {
	ID             string          `json:"id"`
	Sequence       int             `json:"sequence"`
	Name           string          `json:"name"`
	Status         ReadinessStatus `json:"status"`
	Owner          string          `json:"owner"`
	EvidenceRef    string          `json:"evidence_ref"`
	BlockerReason  string          `json:"blocker_reason"`
	LastCheckedAt  time.Time       `json:"last_checked_at"`
	AllowsGenerate bool            `json:"allows_generate"`
}

// SafetyInvariant is a feed-safety rule that must stay fail-closed until its
// backing projection/config/rework path is implemented and source-reviewed.
type SafetyInvariant struct {
	Key            string          `json:"key"`
	Status         ReadinessStatus `json:"status"`
	Owner          string          `json:"owner"`
	EvidenceRef    string          `json:"evidence_ref"`
	BlockerReason  string          `json:"blocker_reason"`
	LastCheckedAt  time.Time       `json:"last_checked_at"`
	AllowsGenerate bool            `json:"allows_generate"`
}

// Readiness is the Feed Direction gate contract used by UI and later generation
// commands to avoid treating docs or legacy schedules as runnable GoatOS state.
type Readiness struct {
	TenantID                 string                  `json:"tenant_id"`
	Status                   ReadinessStatus         `json:"status"`
	CurrentGate              string                  `json:"current_gate"`
	GenerationAllowed        bool                    `json:"generation_allowed"`
	NextAction               string                  `json:"next_action"`
	SourcePriority           string                  `json:"source_priority"`
	Gates                    []ReadinessGate         `json:"gates"`
	CountsShiftingSubgates   []CountsShiftingSubgate `json:"counts_shifting_subgates"`
	SafetyInvariants         []SafetyInvariant       `json:"safety_invariants"`
	GeneratedDirectionsState string                  `json:"generated_directions_state"`
}
