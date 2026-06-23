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
