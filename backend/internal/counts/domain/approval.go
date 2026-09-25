package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
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
	ApprovalResultTypeBirthEvent    = "birth_event"
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

	// Capture is the SOP capture card's snapshot taken at raise (proofs under their titles with
	// the register's kind, answers in farm words, the older-app missing note). Empty when the
	// card asked nothing or the row predates the feature. CaptureReviewStatus / Reason carry the
	// verifier's verdict on the report's own proof (birth: the birth_capture item).
	Capture             authored.Evidence
	CaptureReviewStatus *string
	CaptureReviewReason *string
}

// ApprovalCapture is what a judged capture-card submission stores beside the request: the
// pinned version, the slot map and answers as accepted, and the composed snapshot.
type ApprovalCapture struct {
	SOPVersionID string
	Proofs       authored.ProofRefs
	Answers      authored.Answers
	Evidence     authored.Evidence
}

// Capture review statuses (counts_approval_requests.capture_review_status).
const (
	CaptureReviewPending  = "pending"
	CaptureReviewApproved = "approved"
	CaptureReviewRework   = "rework"
)

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

	// Capture is the judged capture-card submission; nil when the card asked nothing.
	Capture *ApprovalCapture
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
	// BirthCounts is set for a birth approval. The canonical children already exist; approval only
	// changes the shared litter from count-pending to count-approved atomically with the request.
	BirthCounts *BirthCountsApprovalEffect
	// ExitGoat is set for a death: a *ports.ExitGoatCommand from the identity module.
	ExitGoat any
	// Shifting is set for a shifting approval.
	Shifting *ShiftingApprovalEffect
}

type BirthCountsApprovalEffect struct {
	BirthEventID string
}

// BirthChildResult is one canonical child created during a litter submission.
type BirthChildResult struct {
	GoatID              string `json:"goat_id"`
	TemporaryIdentifier string `json:"temporary_identifier"`
	ChildOrdinal        int    `json:"child_ordinal"`
}

// BirthSubmissionResult keeps the canonical children and the independent web approval together.
type BirthSubmissionResult struct {
	Approval ApprovalRequest
	Children []BirthChildResult
	Replayed bool
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

	// P1: CallerParkIDs filters the page to requests in the caller's park scope: every park the
	// caller holds a park grant in. Empty means no scope (a tenant-scoped caller).
	CallerParkIDs []string

	// FilterParkID is the CLIENT's optional farm filter (2026-09-25), applied in SQL on top of
	// CallerParkIDs -- never instead of it, so a park outside the caller's scope reads empty rather
	// than widening. A death matches through its subject animal's park.
	FilterParkID string
	// FilterKey is ApprovalListFilter.Key() for the page; it is stamped on the next cursor so a
	// cursor minted under one filter cannot walk the keyset of another.
	FilterKey string
	// RaisedFrom / RaisedBefore are the CLIENT's optional calendar filter (2026-09-25): the start
	// of the first India business day and the start of the day AFTER the last, so the range is
	// half-open and a request raised at 02:00 IST sits on its own day. Nil means no bound.
	RaisedFrom   *time.Time
	RaisedBefore *time.Time

	PageSize int
	Cursor   *ApprovalRequestCursor
}

// ApprovalListFilter is the client's optional narrowing of the approvals list (2026-09-25): the
// web filtered type and farm client-side over one 20-row page, so a farm's requests past that
// page were invisible. Both are applied server-side, validated, and never widen what the caller
// may decide.
type ApprovalListFilter struct {
	RequestType string
	ParkID      string
	// RaisedFrom / RaisedTo are the calendar filter as the client sent it: YYYY-MM-DD India
	// business dates, both inclusive (maintainer request 2026-09-25).
	RaisedFrom string
	RaisedTo   string
}

// Key is the stable identity of the filter, bound into the keyset cursor. Blank for no filter,
// so a cursor minted before filters existed still pages the unfiltered list.
func (f ApprovalListFilter) Key() string {
	t, p := strings.TrimSpace(f.RequestType), strings.ToLower(strings.TrimSpace(f.ParkID))
	from, to := strings.TrimSpace(f.RaisedFrom), strings.TrimSpace(f.RaisedTo)
	if t == "" && p == "" && from == "" && to == "" {
		return ""
	}
	key := "t=" + t + ";p=" + p
	// The dates join the key only when set, so a type/farm cursor minted before the calendar
	// filter existed keeps paging.
	if from != "" || to != "" {
		key += ";f=" + from + ";to=" + to
	}
	return key
}

// ApprovalRequestCursor is the keyset position: (raised_at, approval_request_id) descending.
type ApprovalRequestCursor struct {
	RaisedAt          time.Time
	ApprovalRequestID string
	// Filter is the ApprovalListFilter.Key() the cursor was minted under ("" = unfiltered).
	Filter string
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

	// Capture is the report's capture snapshot for the approver (see ApprovalRequest.Capture).
	Capture             authored.Evidence
	CaptureReviewStatus *string
	CaptureReviewReason *string
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
	Filter   string `json:"f,omitempty"`
}

// EncodeApprovalRequestCursor encodes a keyset position for the next page.
func EncodeApprovalRequestCursor(c ApprovalRequestCursor) (string, error) {
	raw, err := json.Marshal(approvalCursorPayload{
		Kind:     approvalCursorKind,
		RaisedAt: c.RaisedAt.UTC().Format(time.RFC3339Nano),
		ID:       c.ApprovalRequestID,
		Filter:   c.Filter,
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
	return &ApprovalRequestCursor{RaisedAt: raisedAt, ApprovalRequestID: payload.ID, Filter: payload.Filter}, nil
}
