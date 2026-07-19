// Package domain holds the feed-direction module domain types (per-shed feed execution records).
package domain

import (
	"encoding/json"
	"time"
)

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

type CountsProjectionExceptionResolutionCommand struct {
	TenantID              string
	ProjectionExceptionID string
	Action                string
	ActorID               string
	ResolutionReason      string
	ResolutionRef         *string
	IdempotencyKey        string
}

type CountsProjectionExceptionQuery struct {
	TenantID      string
	Status        string
	ParkID        *string
	ShedID        *string
	ExceptionType *string
	Severity      *string
	OwnerRef      *string
	WorkState     *string
	Cursor        *string
	Limit         int32
}

type CountsProjectionExceptionList struct {
	Items      []CountsProjectionException `json:"items"`
	NextCursor *string                     `json:"next_cursor,omitempty"`
}

type CountsProjectionException struct {
	ProjectionExceptionID string          `json:"projection_exception_id"`
	ProjectionSnapshotID  *string         `json:"projection_snapshot_id,omitempty"`
	ExceptionType         string          `json:"exception_type"`
	SourceKey             string          `json:"source_key"`
	GrainKey              string          `json:"grain_key"`
	ParkID                *string         `json:"park_id,omitempty"`
	ShedID                *string         `json:"shed_id,omitempty"`
	BreedKey              *string         `json:"breed_key,omitempty"`
	StageTag              *string         `json:"stage_tag,omitempty"`
	Severity              string          `json:"severity"`
	Status                string          `json:"status"`
	OwnerRef              *string         `json:"owner_ref,omitempty"`
	WorkType              string          `json:"work_type"`
	WorkState             string          `json:"work_state"`
	DueAt                 time.Time       `json:"due_at"`
	NextAction            string          `json:"next_action"`
	EvidenceLink          string          `json:"evidence_link"`
	BlockerReason         string          `json:"blocker_reason"`
	EvidenceJSON          json.RawMessage `json:"evidence_json"`
	ResolutionID          *string         `json:"resolution_id,omitempty"`
	ResolvedByRef         *string         `json:"resolved_by_ref,omitempty"`
	ResolutionReason      *string         `json:"resolution_reason,omitempty"`
	ResolutionRef         *string         `json:"resolution_ref,omitempty"`
	ResolvedAt            *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type CountsProjectionExceptionResolution struct {
	ResolutionID          string    `json:"resolution_id"`
	ProjectionExceptionID string    `json:"projection_exception_id"`
	Action                string    `json:"action"`
	Status                string    `json:"status"`
	WorkState             string    `json:"work_state"`
	ResolvedByRef         string    `json:"resolved_by_ref"`
	ResolutionReason      string    `json:"resolution_reason"`
	ResolutionRef         *string   `json:"resolution_ref,omitempty"`
	ResolvedAt            time.Time `json:"resolved_at"`
	Replayed              bool      `json:"replayed"`
}

type GenerationPreviewQuery struct {
	TenantID   string
	ParkID     string
	TargetDate time.Time
	ShedID     *string
	BreedKey   *string
	Cursor     *string
	Limit      int32
}

// GenerationPreview is Feed Direction's bounded view of the immutable
// Counts/Shifting projection. It is a preview/read model: generation is allowed
// only when the projection page carries no blockers.
type GenerationPreview struct {
	TenantID              string                     `json:"tenant_id"`
	ParkID                string                     `json:"park_id"`
	TargetDate            time.Time                  `json:"target_date"`
	Status                ReadinessStatus            `json:"status"`
	GenerationAllowed     bool                       `json:"generation_allowed"`
	BlockerReason         string                     `json:"blocker_reason"`
	SnapshotID            string                     `json:"snapshot_id"`
	ProjectionStatus      string                     `json:"projection_status"`
	SourceContractVersion string                     `json:"source_contract_version"`
	SourceHash            string                     `json:"source_hash"`
	BaseAnchorIDsHash     string                     `json:"base_anchor_ids_hash"`
	ShiftingEventIDsHash  string                     `json:"shifting_event_ids_hash"`
	ExceptionCount        int64                      `json:"exception_count"`
	TotalRowCount         int64                      `json:"total_row_count"`
	Rows                  []GenerationPreviewRow     `json:"rows"`
	ShedBreedTotals       []GenerationPreviewTotal   `json:"shed_breed_totals"`
	Blockers              []GenerationPreviewBlocker `json:"blockers"`
	NextCursor            *string                    `json:"next_cursor,omitempty"`
}

type GenerationPreviewRow struct {
	ProjectionRowID              string    `json:"projection_row_id"`
	ParkID                       string    `json:"park_id"`
	ShedID                       string    `json:"shed_id"`
	TargetDate                   time.Time `json:"target_date"`
	GrainKey                     string    `json:"grain_key"`
	BaseCountAnchorID            string    `json:"base_count_anchor_id"`
	IncludedShiftingEventIDsHash string    `json:"included_shifting_event_ids_hash"`
	BreedID                      *string   `json:"breed_id,omitempty"`
	BreedKey                     string    `json:"breed_key"`
	BreedLabel                   string    `json:"breed_label"`
	StageTag                     *string   `json:"stage_tag,omitempty"`
	AgeClass                     *string   `json:"age_class,omitempty"`
	Sex                          *string   `json:"sex,omitempty"`
	HeadCount                    int32     `json:"head_count"`
	PregnantCount                int32     `json:"pregnant_count"`
	LactatingCount               int32     `json:"lactating_count"`
	WarmupCount                  int32     `json:"warmup_count"`
	RationContextResolutionState string    `json:"ration_context_resolution_state"`
	RationContextRef             *string   `json:"ration_context_ref,omitempty"`
	BlockerReason                *string   `json:"blocker_reason,omitempty"`
	SourceRowHash                string    `json:"source_row_hash"`
}

type GenerationPreviewTotal struct {
	ParkID                       string `json:"park_id"`
	ShedID                       string `json:"shed_id"`
	BreedKey                     string `json:"breed_key"`
	BreedLabel                   string `json:"breed_label"`
	HeadCount                    int32  `json:"head_count"`
	PregnantCount                int32  `json:"pregnant_count"`
	LactatingCount               int32  `json:"lactating_count"`
	WarmupCount                  int32  `json:"warmup_count"`
	RationContextResolutionState string `json:"ration_context_resolution_state"`
}

type GenerationPreviewBlocker struct {
	Source        string `json:"source"`
	Type          string `json:"type"`
	SourceKey     string `json:"source_key"`
	GrainKey      string `json:"grain_key"`
	Severity      string `json:"severity"`
	BlockerReason string `json:"blocker_reason"`
}

// ReadinessStatus is the status of a Feed Direction generation preview: whether
// the bounded Counts/Shifting projection page it was built from is clear for
// generation, blocked by projection blockers, or still pending.
type ReadinessStatus string

const (
	ReadinessReady   ReadinessStatus = "ready"
	ReadinessBlocked ReadinessStatus = "blocked"
	ReadinessPending ReadinessStatus = "pending"
)
