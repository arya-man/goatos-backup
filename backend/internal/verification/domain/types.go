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
	ItemID        string
	TenantID      string
	Vertical      string
	Module        string
	Category      string
	Source        SourceRef
	MediaRefs     []string // proof_artifact IDs; signed URLs resolved at read time.
	Status        string
	VerdictReason *string
	OperatorID    *string
	OperatorName  *string // backend-owned display label for OperatorID
	ShedID        *string
	ShedLabel     *string // backend-owned display label for ShedID
	ParkID        *string
	ParkLabel     *string // backend-owned display label for ParkID
	CapturedAt    time.Time
	VerifiedBy    *string
	VerifiedAt    *time.Time
	RowVersion    int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateItem is the input a producer supplies to enqueue one verification item.
type CreateItem struct {
	TenantID       string
	Vertical       string
	Module         string
	Category       string
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
	TenantID   string
	ItemID     string
	Decision   string
	Reason     string
	VerifierID string
	RowVersion int
}

// MediaItem is one resolved, streamable media reference for display.
type MediaItem struct {
	ProofID     string `json:"proof_id"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}

// QueueRow is one queue listing row: the item plus its resolved media.
type QueueRow struct {
	Item  Item
	Media []MediaItem
}
