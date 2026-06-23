// Package domain holds the obligation (due-state) domain types.
package domain

import "time"

// NewObligation is the input to generate one obligation instance. IdempotencyKey is the
// deterministic key that makes generation a no-op on replay.
type NewObligation struct {
	TenantID             string
	ProtocolVersionID    string
	RuleID               string
	BatchID              *string
	TargetType           string
	TargetID             string
	ScopeType            string
	ScopeID              string
	DueAt                time.Time
	WindowStart          *time.Time
	WindowEnd            *time.Time
	Status               string
	IdempotencyKey       string
	GeneratedByTriggerID *string
	Sequence             int32
}

// ObligationRef is a minimal stored-obligation lookup result.
type ObligationRef struct {
	ObligationID string
	Status       string
	DueAt        time.Time
	RowVersion   int32
}

// DueObligation is a row from the due-window scan.
type DueObligation struct {
	ObligationID      string
	ProtocolVersionID string
	RuleID            string
	TargetType        string
	TargetID          string
	ScopeType         string
	ScopeID           string
	DueAt             time.Time
	Status            string
}

// NewBatch is the input to create a work-unit batch (a drive / feed session).
type NewBatch struct {
	TenantID              string
	ProtocolVersionID     string
	ScopeType             string
	ScopeID               string
	Session               string
	PlannedDate           *time.Time
	WindowStart           *time.Time
	WindowEnd             *time.Time
	Status                string
	EstimatedTargets      int32
	PlannedQuantity       string
	QuantityUnit          string
	PrimaryInventoryLotID *string
	SopTaskID             *string
	ConductedBy           *string
}

// UnbatchedDue is an unbatched scheduled/due obligation (SM-4 sweep input).
type UnbatchedDue struct {
	ObligationID string
	ScopeType    string
	ScopeID      string
}

// SweepResult summarises an SM-4 sweep (batches created, obligations attached).
type SweepResult struct {
	Batches     int
	Obligations int
}

// NewStatusEvent is the input to append an obligation status event. Scope/RequestHash drive the
// shared idempotency_keys reserve-before-insert guard (cross-partition dedup).
type NewStatusEvent struct {
	TenantID       string
	ObligationID   string
	EventType      string
	OccurredAt     time.Time
	ActorID        *string
	Payload        []byte
	IdempotencyKey string
	Scope          string
	RequestHash    string
}
