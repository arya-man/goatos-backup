// Package domain holds the module-agnostic Verification vertical types (verification-module-design.md).
// The verification module knows nothing about vaccination/feed/diagnosis/etc. internals; producers
// speak to it only through the generic Item shape + a back-pointer SourceRef.
package domain

import (
	"errors"
	"time"
)

// Status values for a verification_item.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Decision values accepted by RecordVerdict.
const (
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
)

var (
	ErrInvalid         = errors.New("verification: invalid input")
	ErrReasonRequired  = errors.New("verification: reason is required to reject")
	ErrUnknownCategory = errors.New("verification: category is not registered")
)

// FieldError and ErrorEnvelope mirror the sop/proof modules' per-module error envelope shape
// (each module owns its own copy rather than sharing a cross-module type).
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

// SourceRef is the generic back-pointer to the producing module's own record. Verification stores
// only this reference tuple — never producer-specific columns (verification-module-design.md §2.2).
type SourceRef struct {
	Module       string  `json:"module"`
	TaskID       *string `json:"task_id,omitempty"`
	SubmissionID *string `json:"submission_id,omitempty"`
	RefType      string  `json:"ref_type"`
	RefID        string  `json:"ref_id"`
}

// Item is one unit of media awaiting (or having received) independent verification.
type Item struct {
	ItemID         string
	TenantID       string
	Vertical       string
	Module         string
	Category       string
	SubjectLabel   *string
	Source         SourceRef
	MediaRefs      []string // proof_artifact IDs; signed URLs resolved at read time.
	Status         string
	VerdictReason  *string
	OperatorID     *string
	OperatorName   *string // backend-owned display label for OperatorID
	ShedID         *string
	ShedLabel      *string // backend-owned display label for ShedID
	ParkID         *string
	ParkLabel      *string // backend-owned display label for ParkID
	CapturedAt     time.Time
	VerifiedBy     *string
	VerifiedByName *string // backend-owned display label for VerifiedBy
	VerifiedAt     *time.Time
	ClosedBy       *string
	ClosedAt       *time.Time
	RowVersion     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateItem is the input a producer supplies to enqueue one verification item.
type CreateItem struct {
	TenantID       string
	Vertical       string
	Module         string
	Category       string
	SubjectLabel   *string
	Source         SourceRef
	MediaRefs      []string
	OperatorID     *string
	ShedID         *string
	ParkID         *string
	CapturedAt     time.Time
	IdempotencyKey string
}

// CreateItemResult reports whether CreateItem minted a new row (false = idempotent replay no-op,
// the existing item is still returned).
type CreateItemResult struct {
	Item    Item
	Created bool
}

// Verdict is the verifier's approve/reject decision on one item.
type Verdict struct {
	TenantID       string
	ItemID         string
	Decision       string
	Reason         string
	VerifierID     string
	RowVersion     int
	IdempotencyKey string
}

// CloseAction is the leadership authority transition applied only after verifier approval.
type CloseAction struct {
	TenantID       string
	ItemID         string
	ActorID        string
	RowVersion     int
	IdempotencyKey string
}

// CloseSubmissionAction closes one operator submission (the vaccination drive execution unit) only
// after every goat verification item in it has independent verifier approval.
type CloseSubmissionAction struct {
	TenantID       string
	SubmissionID   string
	ActorID        string
	IdempotencyKey string
}

// CloseVaccinationBatchAction closes one vaccination drive/batch after every obligation in that
// batch has an approved proof. The batch can contain one or more planned dates.
type CloseVaccinationBatchAction struct {
	TenantID       string
	BatchID        string
	ActorID        string
	IdempotencyKey string
}

// VaccinationBatchClosure is a leadership-ready drive close candidate.
type VaccinationBatchClosure struct {
	BatchID        string `json:"batch_id"`
	DriveKey       string `json:"drive_key,omitempty"`
	DriveLabel     string `json:"drive_label,omitempty"`
	BatchLabel     string `json:"batch_label,omitempty"`
	ParkID         string `json:"park_id,omitempty"`
	ParkLabel      string `json:"park_label,omitempty"`
	StartDate      string `json:"start_date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	TotalCount     int    `json:"total_count"`
	ApprovedCount  int    `json:"approved_count"`
	RejectedCount  int    `json:"rejected_count"`
	PendingCount   int    `json:"pending_count"`
	VideoCount     int    `json:"video_count"`
	ApprovedVideos int    `json:"approved_videos"`
	RejectedVideos int    `json:"rejected_videos"`
	PendingVideos  int    `json:"pending_videos"`
	ShedCount      int    `json:"shed_count"`
	Ready          bool   `json:"ready"`
}

type LocationFilterOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// QueuePageOption is one backend-defined page tab within the selected verifier drawer module.
// Categories are disjoint queue filters over verification-item grain; the UI never adds tabs.
type QueuePageOption struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

// QueueStatusOption is one backend-defined secondary tab. Status filters are disjoint at
// verification-item grain: pending (Due), approved, and rejected.
type QueueStatusOption struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

// QueueActionTypeOption is one backend-registered verification action type exposed to
// cross-module renderers such as admin-web. Category is the disjoint queue predicate; the
// remaining fields are presentation and grouping metadata owned by the registry.
type QueueActionTypeOption struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	ModuleKey   string `json:"module_key"`
	ModuleLabel string `json:"module_label"`
}

type QueueFilterOptions struct {
	ModuleKey            string                  `json:"module_key,omitempty"`
	ModuleLabel          string                  `json:"module_label,omitempty"`
	ActionTypes          []QueueActionTypeOption `json:"action_types"`
	Pages                []QueuePageOption       `json:"pages"`
	Statuses             []QueueStatusOption     `json:"statuses"`
	Parks                []LocationFilterOption  `json:"parks"`
	Sheds                []LocationFilterOption  `json:"sheds"`
	SelectedBusinessDate string                  `json:"selected_business_date,omitempty"`
	BusinessTimezone     string                  `json:"business_timezone"`
	MissedOnly           bool                    `json:"missed_only"`
	HasMissed            bool                    `json:"has_missed"`
}

// MediaItem is one resolved, streamable media reference for display.
type MediaItem struct {
	ProofID     string `json:"proof_id"`
	Label       string `json:"label,omitempty"`
	Answer      string `json:"answer,omitempty"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}

// QueueRow is one queue listing row: the item plus its resolved media.
type QueueRow struct {
	Item              Item
	Media             []MediaItem
	EvidenceAvailable bool
}
