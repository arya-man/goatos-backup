package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// PEN RECONCILIATION (maintainer decision 2026-09-02). The herd register is TRUTH: when an
// individual weighing submit finds a scanned animal in a pen the register disagrees with, one
// card is raised in the Herd Operations Reconcile tab. The operator physically returns the
// animal to its REGISTERED pen, records a mandatory video, and submits; the video is reviewed
// by the tenant verifier. There is deliberately NO approver step (unlike shifting), and a
// completion NEVER rewrites the register — the card is closed by moving the animal.
//
// One open card per animal ("one piece one card"): while an animal already carries a card
// that is not completed, no weighing submit raises another.

const (
	// PenReconciliationStatusOpen is a raised card waiting for the operator to return the
	// animal and record the video.
	PenReconciliationStatusOpen = "open"
	// PenReconciliationStatusPendingVerification is operator-submitted evidence waiting for
	// the verifier's verdict.
	PenReconciliationStatusPendingVerification = "pending_verification"
	// PenReconciliationStatusCompleted is a verifier-approved card. Terminal.
	PenReconciliationStatusCompleted = "completed"
	// PenReconciliationStatusRework is verifier-rejected evidence: the operator must
	// re-shoot and submit again.
	PenReconciliationStatusRework = "rework"

	// MaxPenReconciliationPageSize caps the Reconcile queue page: this screen is read from a
	// phone standing in a park, and a phone viewport holds roughly 7-10 rows.
	MaxPenReconciliationPageSize = 20

	// Verification coordinates. The generic verification module stores only these
	// (module, ref_type, ref_id=card_id) as a back-pointer; the reconciliation consumer
	// filters verdict events on module+ref_type so it ignores shifting/birth/death verdicts,
	// which share the "counts" module.
	VerificationVerticalPenReconciliation = "counts"
	VerificationModulePenReconciliation   = "counts"
	VerificationCategoryPenReconciliation = "pen_reconciliation"
	VerificationRefTypePenReconciliation  = "pen_reconciliation_card"
)

// Reconcile tab status buckets. "all" is every card; the rest are disjoint at the card grain
// and map 1:1 onto the stored status.
const (
	PenReconciliationBucketAll       = "all"
	PenReconciliationBucketOpen      = "open"
	PenReconciliationBucketSubmitted = "pending_verification"
	PenReconciliationBucketRework    = "rework"
	PenReconciliationBucketCompleted = "completed"
)

// ValidPenReconciliationBucket reports whether s names a known Reconcile list bucket.
func ValidPenReconciliationBucket(s string) bool {
	switch s {
	case PenReconciliationBucketAll, PenReconciliationBucketOpen, PenReconciliationBucketSubmitted,
		PenReconciliationBucketRework, PenReconciliationBucketCompleted:
		return true
	}
	return false
}

// PenReconciliationRaiseCommand asks the repository to raise cards for one submitted
// individual weighing bucket: resolve every scanned tag to a live animal, compare the
// register's pen against the bucket's canonical pen, and insert one card per mismatched
// animal that does not already carry an open card. Idempotent by construction (the one-open-
// card-per-animal unique index), so a duplicate bus delivery inserts nothing.
type PenReconciliationRaiseCommand struct {
	TenantID       string
	CampaignID     string
	CampaignShedID string
	RaisedAt       time.Time
}

// PenReconciliationCompletionCommand is one "the animal is back in its pen" submission.
type PenReconciliationCompletionCommand struct {
	TenantID string
	CardID   string

	CompletedByUserID string
	CompletedAt       time.Time
	TraceID           string

	// ProofRef is the MANDATORY video proving the animal was returned to its registered pen.
	// A blank value is rejected; the video is what the verifier reviews.
	ProofRef string

	IdempotencyKey     string
	RequestFingerprint string
}

// PenReconciliationVerdictCommand applies a verifier decision to a submitted card.
type PenReconciliationVerdictCommand struct {
	TenantID   string
	CardID     string
	VerifiedBy string
	VerifiedAt time.Time
	// Reason is the verifier's rework reason; ignored on approve.
	Reason string
}

// PenReconciliationCard is one "this animal is in the wrong pen" action.
type PenReconciliationCard struct {
	CardID string
	Status string
	// PrimaryActionKey is backend-owned row behavior: open/rework rows use "execute";
	// submitted/completed history is visible but not actionable.
	PrimaryActionKey string

	GoatID            string
	GoatDisplayID     string
	ScannedIdentifier string

	FoundLocationID     string
	FoundPartitionLabel string
	// FoundDisplayName is the weighing bucket's operator-facing pen label, snapshotted at
	// raise: where the animal was actually scanned.
	FoundDisplayName string

	RegisteredShedID         string
	RegisteredShedName       string
	RegisteredPartitionLabel string

	ParkID   *string
	ParkName *string

	CampaignID     string
	CampaignShedID string

	RaisedAt time.Time

	ProofRef     *string
	CompletedBy  *string
	CompletedAt  *time.Time
	VerifiedBy   *string
	VerifiedAt   *time.Time
	ReworkReason *string
}

// PenReconciliationCompletionResult is the outcome of a completion (or its idempotent replay).
type PenReconciliationCompletionResult struct {
	CardID string
	Status string

	GoatID            string
	ScannedIdentifier string

	FoundDisplayName         string
	RegisteredShedID         string
	RegisteredShedName       string
	RegisteredPartitionLabel string
	ParkID                   *string

	ProofRef    string
	CompletedAt *time.Time

	// NeedsVerificationEnqueue is durable recovery state: true means the card has reached
	// pending_verification but the mandatory verifier item has not yet been confirmed enqueued.
	// Exact completion replays keep returning true until the producer clears it after a successful
	// idempotent enqueue.
	NeedsVerificationEnqueue bool
}

// PenReconciliationQuery is the keyset-paginated Reconcile list read.
type PenReconciliationQuery struct {
	TenantID string
	// Status is one of the PenReconciliationBucket* values; empty normalizes to all.
	Status   string
	PageSize int
	Cursor   *PenReconciliationCursor
}

// PenReconciliationCursor is the keyset position: (raised_at, card_id) descending.
type PenReconciliationCursor struct {
	RaisedAt time.Time
	CardID   string
}

// PenReconciliationPage is one page of the Reconcile queue.
type PenReconciliationPage struct {
	Items        []PenReconciliationCard
	NextCursor   string
	StatusCounts PenReconciliationStatusCounts
}

// PenReconciliationStatusCounts is the whole-filter bucket summary (never page-local).
type PenReconciliationStatusCounts struct {
	All       int `json:"all"`
	Open      int `json:"open"`
	Submitted int `json:"pending_verification"`
	Rework    int `json:"rework"`
	Completed int `json:"completed"`
}

const (
	// MaxPenReconciliationCursorLength bounds the encoded cursor a client may send back.
	MaxPenReconciliationCursorLength        = 512
	maxPenReconciliationCursorPayloadLength = 384
	penReconciliationCursorKind             = "counts_pen_reconciliation"
)

type penReconciliationCursorPayload struct {
	Kind     string `json:"k"`
	RaisedAt string `json:"r"`
	ID       string `json:"i"`
}

// EncodePenReconciliationCursor encodes a keyset position for the next page.
func EncodePenReconciliationCursor(c PenReconciliationCursor) (string, error) {
	raw, err := json.Marshal(penReconciliationCursorPayload{
		Kind:     penReconciliationCursorKind,
		RaisedAt: c.RaisedAt.UTC().Format(time.RFC3339Nano),
		ID:       c.CardID,
	})
	if err != nil {
		return "", fmt.Errorf("encode pen reconciliation cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodePenReconciliationCursor parses a client-supplied cursor. Strict on every axis (size,
// encoding, unknown fields, trailing data, kind) so a cursor minted for another list cannot
// be replayed here to walk a different keyset.
func DecodePenReconciliationCursor(value string) (*PenReconciliationCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > MaxPenReconciliationCursorLength {
		return nil, fmt.Errorf("decode pen reconciliation cursor: too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode pen reconciliation cursor: %w", err)
	}
	if len(raw) > maxPenReconciliationCursorPayloadLength {
		return nil, fmt.Errorf("decode pen reconciliation cursor payload: too large")
	}
	var payload penReconciliationCursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode pen reconciliation cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode pen reconciliation cursor payload: trailing data")
	}
	if payload.Kind != penReconciliationCursorKind {
		return nil, fmt.Errorf("decode pen reconciliation cursor: kind mismatch")
	}
	raisedAt, err := time.Parse(time.RFC3339Nano, payload.RaisedAt)
	if err != nil {
		return nil, fmt.Errorf("decode pen reconciliation cursor: %w", err)
	}
	if payload.ID == "" {
		return nil, fmt.Errorf("decode pen reconciliation cursor: missing id")
	}
	return &PenReconciliationCursor{RaisedAt: raisedAt, CardID: payload.ID}, nil
}
