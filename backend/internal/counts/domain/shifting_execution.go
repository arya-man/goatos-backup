package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Shifting EXECUTION (maintainer decision 2026-07-28): Park Head approval and operator completion
// are independent gates. The second gate atomically relocates and moves census; verification only
// reviews mandatory video evidence afterward and cannot roll the movement back.

const (
	// ShiftingEventStatusPending is not-yet-decided; it may already carry operator completion stamps.
	ShiftingEventStatusPending = "pending"
	// ShiftingEventStatusAuthorized is approved-but-not-executed: the movement MAY happen and the
	// animals are still at the source shed. This is the state the execution queue lists.
	ShiftingEventStatusAuthorized = "authorized"
	// ShiftingEventStatusPendingVerification means operator completion/proof exists while Park Head
	// approval is absent (or a legacy rollout row awaits compatibility apply). Verification is
	// orthogonal and does not own relocation.
	ShiftingEventStatusPendingVerification = "pending_verification"
	// ShiftingEventStatusApplied means Park Head approval + operator completion both exist and the
	// animals' canonical location was rewritten atomically.
	ShiftingEventStatusApplied = "applied"
	// ShiftingEventStatusRejected is a movement an approver refused.
	ShiftingEventStatusRejected = "rejected"
	// ShiftingEventStatusCanceled is an authorized movement retired without ever being executed.
	ShiftingEventStatusCanceled = "canceled"

	// MaxShiftingExecutionPageSize caps the pending-execution queue. This screen is read from a
	// phone standing in a park, and a phone viewport holds roughly 7-10 rows, so a page is one
	// screen of work plus prefetch headroom -- never the whole backlog for client-side filtering.
	MaxShiftingExecutionPageSize = 20

	// MaxShiftingExecutionAnimalPreview bounds the per-row animal preview.
	//
	// A shifting may name up to identityports.MaxRelocateGoatsPerCommand (500) animals. Embedding
	// all of them in a 20-row page would be a 10,000-row read to draw one screen -- the mobile
	// over-fetch anti-pattern wearing a list row as a disguise. The row therefore carries the full
	// COUNT (which is what an operator needs to decide whether they can do this movement now) plus
	// the first few animals for recognition; the complete roster belongs to the drill-down.
	MaxShiftingExecutionAnimalPreview = 5

	// MaxShiftingCancelReasonLength bounds the free-text cancellation reason.
	MaxShiftingCancelReasonLength = 2000

	// Verification coordinates for a shifting move. The generic verification module stores only these
	// (module, ref_type, ref_id=shifting_event_id) as a back-pointer; the shifting consumer filters
	// verdict events on this module+ref_type so it ignores vaccination/feed/etc. verdicts.
	VerificationVerticalShifting = "counts"
	VerificationModuleShifting   = "counts"
	VerificationCategoryShifting = "shifting_move"
	VerificationRefTypeShifting  = "shifting_event"
)

// ValidShiftingEventStatus reports whether s is a known shifting event status.
func ValidShiftingEventStatus(s string) bool {
	switch s {
	case ShiftingEventStatusPending, ShiftingEventStatusAuthorized, ShiftingEventStatusPendingVerification,
		ShiftingEventStatusApplied, ShiftingEventStatusRejected, ShiftingEventStatusCanceled, "unresolved":
		return true
	}
	return false
}

// ShiftingCompletionCommand is one "the animals actually moved" confirmation.
//
// It carries its own idempotency key and request fingerprint rather than reusing the submit-time or
// decision-time pair: completion is a third write, by a different actor, at a different time, and
// it is the one that relocates animals -- so a phone retrying it on a flaky link must collapse onto
// the original relocation instead of moving the herd twice.
type ShiftingCompletionCommand struct {
	TenantID        string
	ShiftingEventID string

	// CompletedByUserID is ANY operator holding CountsWrite (maintainer decision, 2026-07-19), not
	// only the operator who raised the movement. The person who happens to be standing in the park
	// when the animals walk is not reliably the person who typed the request.
	CompletedByUserID string
	CompletedAt       time.Time
	TraceID           string

	// ProofRef is the MANDATORY video the operator records to prove the animals physically moved
	// (maintainer decision, 2026-07-26). It is a proof_artifact id; the completion is rejected when
	// it is blank. The video travels into the queued verification item's MediaRefs. Verification is
	// evidence review only; Park Head approval + operator completion own the move.
	ProofRef              string
	FeedPackingProofRef   string
	FeedGivenProofRef     string
	FeedConfigFingerprint string

	// DestinationTag is the OPTIONAL destination management_stage (operational cohort) the moved
	// animals adopt. It is only needed when the destination shed is EMPTY (no existing animals to
	// derive the cohort from); for an occupied shed the tag is derived server-side and a supplied
	// value must agree with it. Empty string means "not supplied" — existing callers omit it and the
	// server derives the tag from the occupied destination shed.
	DestinationTag string

	IdempotencyKey     string
	RequestFingerprint string
}

// ShiftingVerifiedApplyCommand records an approved evidence verdict. The historic name is retained
// across ports/wiring for compatibility; new rows are already applied by the second business gate.
type ShiftingVerifiedApplyCommand struct {
	TenantID         string
	ShiftingEventID  string
	VerifiedByUserID string
	VerifiedAt       time.Time
	TraceID          string
	DestinationTag   string
}

// ShiftingReworkCommand marks evidence rejected for re-shoot. It preserves movement/count state.
type ShiftingReworkCommand struct {
	TenantID        string
	ShiftingEventID string
	VerifiedBy      string
	Reason          string
}

// ShiftingCancellationCommand retires an authorized movement that will never be executed.
type ShiftingCancellationCommand struct {
	TenantID        string
	ShiftingEventID string

	CanceledByUserID string
	CanceledAt       time.Time

	// Reason is REQUIRED. An abandoned movement that vanishes from the queue with no explanation is
	// indistinguishable from one that was executed, which is exactly the ambiguity this endpoint
	// exists to remove.
	Reason string

	IdempotencyKey     string
	RequestFingerprint string
}

// ShiftingExecutionResult is the outcome of a completion or a cancellation.
type ShiftingExecutionResult struct {
	ShiftingEventID string
	EventStatus     string

	DestinationParkID string
	DestinationShedID string

	// SourceParkID / SourceShedID record where the animals stood before the applied move, so the
	// completion is a full audit trail (from -> to) rather than destination-only. By the P0-1
	// same-park rule SourceParkID equals DestinationParkID; SourceShedID is the meaningful delta.
	// Left empty on cancellation (nothing moved) and on an idempotent replay echo.
	SourceParkID string
	SourceShedID string

	// MovedGoatIDs is the animal set the completion relocated. Empty for a cancellation, which
	// moves nobody.
	MovedGoatIDs []string

	// RaiseComment is the note the OPERATOR WHO RAISED this movement wrote about why the animals
	// are moving. Carried on the completion result so the evidence-review enqueue can hand it to
	// the verifier, who otherwise sees only the video and a system-composed label.
	RaiseComment *string

	AppliedAt *time.Time
	AppliedBy *string

	CanceledAt   *time.Time
	CanceledBy   *string
	CancelReason *string
}

// ShiftingExecutionQuery is the keyset-paginated "authorized, waiting to be walked" read.
type ShiftingExecutionQuery struct {
	TenantID string

	// RaisedFrom/RaisedBefore are the inclusive/exclusive UTC bounds for one Asia/Kolkata business
	// date selected on the Actions calendar. Nil bounds keep compatibility for older clients.
	RaisedFrom   *time.Time
	RaisedBefore *time.Time
	// Status is one of all|pending|authorized|rework|completed. Empty is normalized to all by the
	// service. These buckets are disjoint at the event grain.
	Status string
	// Source filters remain an adapter-level compatibility seam for older internal callers. The
	// mobile Actions contract no longer exposes them.
	SourceParkID string
	SourceShedID string

	PageSize int
	Cursor   *ShiftingExecutionCursor
}

// ShiftingExecutionCursor is the keyset position: (authorized_at, shifting_event_id) descending.
type ShiftingExecutionCursor struct {
	AuthorizedAt    time.Time
	ShiftingEventID string
}

// ShiftingExecutionPage is one page of the pending-execution queue.
type ShiftingExecutionPage struct {
	Items         []ShiftingExecutionRow
	NextCursor    string
	StatusCounts  ShiftingActionStatusCounts
	PreviousDates []ShiftingPreviousDate
}

type ShiftingActionStatusCounts struct {
	All        int `json:"all"`
	Pending    int `json:"pending"`
	Authorized int `json:"authorized"`
	Rework     int `json:"rework"`
	Completed  int `json:"completed"`
}
type ShiftingPreviousDate struct {
	Date        string `json:"date"`
	ActionCount int    `json:"action_count"`
}

// ShiftingExecutionRow is one authorized movement an operator can go and execute.
type ShiftingExecutionRow struct {
	ShiftingEventID   string
	EventStatus       string
	VerificationState string
	// PrimaryActionKey is backend-owned row behavior. Completed history is visible but not
	// executable; open/rework rows use "execute".
	PrimaryActionKey string

	Priority string
	Category string

	SourceParkID   *string
	SourceParkName *string
	SourceShedID   *string
	SourceShedName *string

	DestinationParkID   string
	DestinationParkName string
	DestinationShedID   string
	DestinationShedName string

	// AuthorizedBy/AuthorizedAt answer "who said I may do this, and when" -- the operator's basis
	// for acting. AuthorizedAt is a UTC instant; it is rendered in Asia/Kolkata at the HTTP edge,
	// because a Goat OS business day is an India business day and a movement authorized at
	// 02:00 IST must not read as the previous date.
	AuthorizedByUserID *string
	AuthorizedAt       *time.Time

	RaisedByUserID string
	RaisedAt       time.Time
	EffectiveAt    time.Time

	// AnimalCount is the FULL size of the movement; Animals is a bounded preview of at most
	// MaxShiftingExecutionAnimalPreview of them. See MaxShiftingExecutionAnimalPreview.
	AnimalCount     int
	Animals         []ShiftingExecutionAnimal
	FeedRequirement *ShiftingFeedRequirement
}

// ShiftingFeedRequirement is the exact destination ration shown for a high-priority movement.
// A blocked requirement has no fingerprint and cannot be submitted.
type ShiftingFeedRequirement struct {
	Status                string                        `json:"status"`
	BlockedReason         string                        `json:"blocked_reason,omitempty"`
	Fingerprint           string                        `json:"fingerprint,omitempty"`
	TargetManagementStage string                        `json:"target_management_stage,omitempty"`
	AnimalCount           int                           `json:"animal_count"`
	Items                 []ShiftingFeedRequirementItem `json:"items"`
}

type ShiftingFeedRequirementItem struct {
	FeedItemLabel string `json:"feed_item_label"`
	QuantityGrams string `json:"quantity_grams"`
}

// ShiftingExecutionAnimal identifies one animal in a movement well enough for an operator to find
// it in a shed.
//
// The json tags are LOAD-BEARING, not decoration: the adapter builds this preview with jsonb_agg in
// SQL and decodes the result into this struct, so the tags are what bind the SQL object keys to
// these fields. Without them Go's case-insensitive field matching cannot map "display_id" onto
// DisplayID, and every animal in the queue renders blank.
type ShiftingExecutionAnimal struct {
	GoatID    string `json:"goat_id"`
	DisplayID string `json:"display_id"`
	// Tag is the animal's active primary ear-tag/RFID identifier, when it has one. It is the label
	// physically attached to the animal, so it is what an operator actually reads in the field.
	Tag *string `json:"tag"`
}

const (
	// MaxShiftingExecutionCursorLength bounds the encoded cursor a client may send back.
	MaxShiftingExecutionCursorLength        = 512
	maxShiftingExecutionCursorPayloadLength = 384
	shiftingExecutionCursorKind             = "counts_shifting_pending_execution"
)

type shiftingExecutionCursorPayload struct {
	Kind         string `json:"k"`
	AuthorizedAt string `json:"a"`
	ID           string `json:"i"`
}

// EncodeShiftingExecutionCursor encodes a keyset position for the next page.
func EncodeShiftingExecutionCursor(c ShiftingExecutionCursor) (string, error) {
	raw, err := json.Marshal(shiftingExecutionCursorPayload{
		Kind:         shiftingExecutionCursorKind,
		AuthorizedAt: c.AuthorizedAt.UTC().Format(time.RFC3339Nano),
		ID:           c.ShiftingEventID,
	})
	if err != nil {
		return "", fmt.Errorf("encode shifting execution cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeShiftingExecutionCursor parses a client-supplied cursor. It is strict on every axis (size,
// encoding, unknown fields, trailing data, kind) so a cursor minted for another list cannot be
// replayed here to walk a different keyset.
func DecodeShiftingExecutionCursor(value string) (*ShiftingExecutionCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > MaxShiftingExecutionCursorLength {
		return nil, fmt.Errorf("decode shifting execution cursor: too large")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode shifting execution cursor: %w", err)
	}
	if len(raw) > maxShiftingExecutionCursorPayloadLength {
		return nil, fmt.Errorf("decode shifting execution cursor payload: too large")
	}
	var payload shiftingExecutionCursorPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode shifting execution cursor payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode shifting execution cursor payload: trailing data")
	}
	if payload.Kind != shiftingExecutionCursorKind {
		return nil, fmt.Errorf("decode shifting execution cursor: kind mismatch")
	}
	authorizedAt, err := time.Parse(time.RFC3339Nano, payload.AuthorizedAt)
	if err != nil {
		return nil, fmt.Errorf("decode shifting execution cursor: %w", err)
	}
	if payload.ID == "" {
		return nil, fmt.Errorf("decode shifting execution cursor: missing id")
	}
	return &ShiftingExecutionCursor{AuthorizedAt: authorizedAt, ShiftingEventID: payload.ID}, nil
}
