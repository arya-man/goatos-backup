package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Counts lifecycle approval workflow.
//
// Maintainer decision (2026-07-19): a field operator RECORDS a count-moving event; they do not
// self-authorize it. Birth and death are therefore PENDING UNTIL APPROVED -- submitting one must
// not touch `goats` and must not emit goat.created / goat.exited, so a kid's vaccination
// obligations are generated only on approval and a death's obligations are cancelled only on
// approval. Shifting already landed pending (shifting_events.authorization_state = 'pending'), so
// its approval request links to that row instead of duplicating the movement.

const (
	ApprovalRequestTypeBirth    = "birth"
	ApprovalRequestTypeDeath    = "death"
	ApprovalRequestTypeShifting = "shifting"

	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved"
	ApprovalStatusRejected = "rejected"

	ApprovalResultTypeGoat          = "goat"
	ApprovalResultTypeShiftingEvent = "shifting_event"

	// MaxApprovalPageSize caps the approvals list. The screen is mobile-first and a phone viewport
	// holds roughly 7-10 rows, so a page is one screen of work plus prefetch headroom -- never a
	// "fetch them all and filter client-side" read.
	MaxApprovalPageSize = 20

	// MaxApprovalDecisionReasonLength bounds a free-text rejection reason.
	MaxApprovalDecisionReasonLength = 2000
)

// ValidApprovalRequestType reports whether t is one of the three approvable request types.
func ValidApprovalRequestType(t string) bool {
	switch t {
	case ApprovalRequestTypeBirth, ApprovalRequestTypeDeath, ApprovalRequestTypeShifting:
		return true
	}
	return false
}

// ValidApprovalStatus reports whether s is a known approval status.
func ValidApprovalStatus(s string) bool {
	switch s {
	case ApprovalStatusPending, ApprovalStatusApproved, ApprovalStatusRejected:
		return true
	}
	return false
}

// ApprovalRequest is one submitted, not-yet-or-already-decided lifecycle request.
type ApprovalRequest struct {
	ApprovalRequestID string
	TenantID          string
	RequestType       string

	// Payload is the operator's validated submit body, held verbatim for birth/death and replayed
	// through the same guarded identity command on approval. For shifting it is a small descriptor;
	// the movement itself lives in shifting_events.
	Payload json.RawMessage

	ShiftingEventID *string
	SubjectGoatID   *string

	Status         string
	RaisedByUserID string
	RaisedAt       time.Time

	DecidedByUserID *string
	DecidedAt       *time.Time
	DecisionReason  *string

	AppliedResultType *string
	AppliedResultID   *string

	IdempotencyKey     string
	RequestFingerprint string

	RowVersion int
}

// ApprovalRequestSubmission is the create input for a pending request.
type ApprovalRequestSubmission struct {
	TenantID    string
	RequestType string
	Payload     json.RawMessage

	ShiftingEventID *string
	SubjectGoatID   *string

	RaisedByUserID string
	RaisedAt       time.Time

	IdempotencyKey     string
	RequestFingerprint string
}

// ApprovalDecision is the approve/reject input.
//
// Approve and reject are themselves mutating writes, so they carry their own idempotency key and
// request fingerprint rather than reusing the submit-time pair.
type ApprovalDecision struct {
	TenantID          string
	ApprovalRequestID string

	// Status is ApprovalStatusApproved or ApprovalStatusRejected.
	Status string

	DecidedByUserID string
	DecidedAt       time.Time
	Reason          string

	IdempotencyKey     string
	RequestFingerprint string

	// Effect carries the prepared, already-validated side effect to apply INSIDE the decision's
	// transaction. It is nil for a reject, which applies nothing.
	Effect *ApprovalEffect
}

// ApprovalEffect is the prepared side effect an approval applies. Exactly one field is set,
// matching the request's type. The commands are built by the owning module's app service at
// decision time (identity's Prepare* seam), so this package never re-implements their validation.
type ApprovalEffect struct {
	// CreateGoat is set for a birth: a *ports.CreateAdminGoatCommand from the identity module,
	// carried as `any` so the counts domain does not depend on identity's port types.
	CreateGoat any
	// ExitGoat is set for a death: a *ports.ExitGoatCommand from the identity module.
	ExitGoat any
	// Shifting is set for a shifting approval.
	Shifting *ShiftingApprovalEffect
}

// ShiftingApprovalEffect authorizes a pending shifting event and moves the named animals.
type ShiftingApprovalEffect struct {
	ShiftingEventID   string
	DestinationParkID string
	DestinationShedID string

	// GoatIDs is the EXPLICIT set of animals the movement covers, captured at submit time.
	//
	// It is NON-EMPTY (maintainer decision, 2026-07-19: a shifting event must name the animals it
	// moves). shifting_events remains an aggregate, count-based model -- it records "12 head of
	// Boer moved from shed A to shed B" by breed grain -- so this set is the ONLY per-animal
	// linkage the movement has, and approval relocates exactly these animals. This module will
	// still never GUESS which animals a head count referred to, because picking them would
	// silently relocate real animals (and their shed-scoped vaccination obligations) that nobody
	// selected; instead the set is demanded at submit, where the operator can still supply it.
	//
	// Enforced at submit (normalizeShiftingEventRequest -> missing_goat_ids) and again when the
	// stored payload is decoded for approval (decodeShiftingApprovalPayload ->
	// ErrApprovalInvalidStoredPayload).
	GoatIDs []string
}

// ApprovalRequestQuery is the keyset-paginated pending-list read.
type ApprovalRequestQuery struct {
	TenantID string
	Status   string

	// RequestTypes restricts the page to the types the CALLER MAY DECIDE. It is derived from the
	// caller's permissions, never from a client-supplied filter, so a park_head cannot page
	// through births.
	RequestTypes []string

	// P1: CallerParkID filters the page to requests in the caller's park scope.
	// Empty string means no scope restriction (e.g. CEO/internal).
	CallerParkID string

	PageSize int
	Cursor   *ApprovalRequestCursor
}

// ApprovalRequestCursor is the keyset position: (raised_at, approval_request_id) descending.
type ApprovalRequestCursor struct {
	RaisedAt          time.Time
	ApprovalRequestID string
}

// ApprovalRequestPage is one page of the approvals list.
type ApprovalRequestPage struct {
	Items      []ApprovalRequestSummary
	NextCursor string
}

// ApprovalRequestSummary is the list row: who raised what, and when.
type ApprovalRequestSummary struct {
	ApprovalRequestID string
	RequestType       string
	Status            string
	RaisedByUserID    string
	RaisedAt          time.Time
	ShiftingEventID   *string
	SubjectGoatID     *string
	Summary           json.RawMessage

	DecidedByUserID *string
	DecidedAt       *time.Time
	DecisionReason  *string
}

const (
	// MaxApprovalCursorLength bounds the encoded cursor a client may send back.
	MaxApprovalCursorLength        = 512
	maxApprovalCursorPayloadLength = 384
	approvalCursorKind             = "counts_approval_request"
)

type approvalCursorPayload struct {
	Kind     string `json:"k"`
	RaisedAt string `json:"r"`
	ID       string `json:"i"`
}

// EncodeApprovalRequestCursor encodes a keyset position for the next page.
func EncodeApprovalRequestCursor(c ApprovalRequestCursor) (string, error) {
	raw, err := json.Marshal(approvalCursorPayload{
		Kind:     approvalCursorKind,
		RaisedAt: c.RaisedAt.UTC().Format(time.RFC3339Nano),
		ID:       c.ApprovalRequestID,
	})
	if err != nil {
		return "", fmt.Errorf("encode approval request cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeApprovalRequestCursor parses a client-supplied cursor. It is strict on every axis (size,
// encoding, unknown fields, trailing data, kind) so a cursor from another list cannot be replayed
// here to walk a different keyset.
func DecodeApprovalRequestCursor(value string) (*ApprovalRequestCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > MaxApprovalCursorLength {
		return nil, fmt.Errorf("decode approval request cursor: too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode approval request cursor: %w", err)
	}
	if len(raw) > maxApprovalCursorPayloadLength {
		return nil, fmt.Errorf("decode approval request cursor payload: too large")
	}
	var payload approvalCursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode approval request cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode approval request cursor payload: trailing data")
	}
	if payload.Kind != approvalCursorKind {
		return nil, fmt.Errorf("decode approval request cursor: kind mismatch")
	}
	raisedAt, err := time.Parse(time.RFC3339Nano, payload.RaisedAt)
	if err != nil {
		return nil, fmt.Errorf("decode approval request cursor: %w", err)
	}
	if payload.ID == "" {
		return nil, fmt.Errorf("decode approval request cursor: missing id")
	}
	return &ApprovalRequestCursor{RaisedAt: raisedAt, ApprovalRequestID: payload.ID}, nil
}
