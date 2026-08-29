package ports

import (
	"context"
	"errors"
	"time"
)

// Feed WASTAGE verification gate ports (maintainer decision, 2026-08-18). The module's FOURTH write
// boundary, entirely separate from the distribution, packing and transport stores. It owns exactly
// one NEW table (feed_wastage_completions) behind WastageCompletionStore, gated on a
// verifier-approved video before a pen-day's wastage task is completed — and it is the ONLY feed
// store that also carries a VERIFIER-RECORDED measurement (the leftover weight she reads off the
// clip), because in this flow the operator submits no number at all: the video is the whole submit.

var (
	// ErrWastageProofRequired is returned when a wastage completion omits the MANDATORY wastage
	// VIDEO. There is nothing for a verifier to measure without it.
	ErrWastageProofRequired = errors.New("feeddirection: a wastage video proof is required")
	// ErrWastageStoreUnavailable is returned when a wastage completion is attempted but no
	// WastageCompletionStore is wired — a deployment/wiring error, surfaced as a 500.
	ErrWastageStoreUnavailable = errors.New("feeddirection: wastage completion store is not configured")
	// ErrWastageAlreadyRecorded is returned when a pen-day ALREADY holds a DIFFERENT wastage video —
	// awaiting verification or already verified — and a second, different one arrives. Same rule as
	// packing's ErrPackingAlreadyRecorded: a genuine retry replays on its idempotency key or matches
	// the stored proof_ref; only a DIFFERENT video conflicts, and answering it with success would be
	// silent loss of work the operator physically did.
	ErrWastageAlreadyRecorded = errors.New("feeddirection: this pen already has a different wastage video recorded for this day")
	// ErrWastageCompletionNotFound is returned by the measurement write when the completion the
	// verifier is recording against does not exist for her tenant.
	ErrWastageCompletionNotFound = errors.New("feeddirection: wastage completion not found")
	// ErrWastageValueOutOfRange is returned when a recorded wastage weight is negative, not finite,
	// or beyond the typo ceiling. ZERO IS VALID — an empty trough is a real measurement.
	ErrWastageValueOutOfRange = errors.New("feeddirection: wastage weight is out of range")
	// ErrWastageMeasurementRequired is returned when a verifier approval arrives before the
	// leftover kg has been recorded. Approval is irreversible, so the producer fails closed until
	// the measurement exists.
	ErrWastageMeasurementRequired = errors.New("feeddirection: wastage measurement is required before approval")
	// ErrWastageNotExperimentPen is returned when a completion names a pen the day's experiment
	// sheet does not cover — wastage is an experiment-pen task only, so a row here would be work no
	// worklist line ever matches.
	ErrWastageNotExperimentPen = errors.New("feeddirection: this pen is not on the experiment sheet for that day")
)

// CompleteWastageParams is the persisted gated-completion write, at the PEN-DAY grain
// (tenant, park, shed, partition, target_date). Workflow is stamped 'experiment' by the store — the
// caller cannot choose it, because there is nothing to choose.
type CompleteWastageParams struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen this completion covers ("2", "Part 3"); empty for an undivided
	// shed. Part of the completion's IDENTITY.
	PartitionLabel string
	TargetDate     time.Time
	// WastageProofRef is the ONE MANDATORY wastage VIDEO proof_id. It travels into the queued
	// verification item.
	WastageProofRef string
	// CompletedBy is the operator principal uuid when the caller carries one, else "".
	CompletedBy string
	// IdempotencyKey is the client-supplied request key, reserved in the same transaction as the write.
	IdempotencyKey string
	// ActorID/ActorType/TraceID feed the audit row written in the same transaction.
	ActorID   string
	ActorType string
	TraceID   string
}

// CompleteWastageResult reports the outcome of a wastage completion write. Same contract as
// CompletePackingResult: NewlyPending is true ONLY on a real pending transition (fresh submit or
// rework re-submit), so the verification enqueue fires exactly once per transition.
type CompleteWastageResult struct {
	CompletionID string
	Status       string
	RowVersion   int32
	NewlyPending bool
	// ShedName and PartitionLabel are carried for verification enqueue label composition.
	ShedName, PartitionLabel string
}

// WastageCompletionStatus is one pen-day's wastage completion row with its RAW status
// ('pending_verification' | 'rework' | 'completed'), for the serve-path status overlay + filter.
type WastageCompletionStatus struct {
	ShedID         string
	PartitionLabel string
	Status         string
	// ReworkReason is the stored sentence explaining a 'rework' row, empty in every other state.
	ReworkReason string
	// WastageKg is the verifier's recorded leftover weight, empty until she records one. Carried on
	// the overlay so the operator's completed card can show the measured value.
	WastageKg string
}

// ApplyWastageParams flips a wastage completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed'. Issued by the verification.verdict.approved consumer.
type ApplyWastageParams struct {
	TenantID     string
	CompletionID string
	VerifiedBy   string
	TraceID      string
}

// BounceWastageParams flips a wastage completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. Issued by the verification.verdict.rework consumer.
type BounceWastageParams struct {
	TenantID     string
	CompletionID string
	Reason       string
	TraceID      string
}

// RecordWastageMeasurementParams is the VERIFIER'S measurement: the leftover weight she read off
// the video, in kg. A later entry REPLACES the value — she may re-read a clip and fix her own
// number — and recorded_by/at move with it. There is no operator original to preserve: the operator
// never types a number in this flow.
type RecordWastageMeasurementParams struct {
	TenantID     string
	CompletionID string
	// WastageKg is the measured leftover weight. ZERO IS VALID (an empty trough); negative and
	// typo-scale values are refused with ErrWastageValueOutOfRange.
	WastageKg float64
	// RecordedBy is the verifier's user id, stamped on the row.
	RecordedBy     string
	IdempotencyKey string
	TraceID        string
}

// RecordWastageMeasurementResult is the readback of a recorded measurement, persisted as the
// idempotency result snapshot so an exact replay returns this same value without rewriting.
type RecordWastageMeasurementResult struct {
	CompletionID string  `json:"completion_id"`
	WastageKg    float64 `json:"wastage_kg"`
	// PreviousWastageKg is what the row held immediately before this entry, nil on a first entry.
	PreviousWastageKg *float64  `json:"previous_wastage_kg,omitempty"`
	RecordedBy        string    `json:"recorded_by"`
	RecordedAt        time.Time `json:"recorded_at"`
	// SubjectLabel is the recomposed verifier-facing label for this pen-day, carrying the recorded
	// value. The caller pushes it back onto the verification item so the queue shows the number.
	SubjectLabel string `json:"subject_label"`
}

// WastageCompletionStore owns the feed_wastage_completions table.
//
// OPTIONAL service dependency on the same terms as PackingCompletionStore: a pure-generation unit
// test wires none; production wires it so the gated wastage flow works and the worklist overlay
// reports statuses.
type WastageCompletionStore interface {
	// CompleteWastage records the operator's mandatory video at 'pending_verification' (or moves a
	// 'rework' row back to it), idempotent on both the request key and the pen-day natural key. It
	// does NOT emit feed.wastage.completed — that fires only at verifier approval.
	CompleteWastage(ctx context.Context, p CompleteWastageParams) (CompleteWastageResult, error)

	// ListWastageCompletionStatuses returns EVERY pen with a feed_wastage_completions row for one
	// park-day, each with its RAW status and any recorded measurement — the wastage serve path's
	// status overlay + filter source. One bounded indexed read, bounded by the park's pen catalog.
	ListWastageCompletionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]WastageCompletionStatus, error)

	// ApplyVerifiedWastage flips 'pending_verification' -> 'completed', stamps verified_by/at, and
	// emits feed.wastage.completed in one transaction. Returns applied=true only when it actually
	// flipped a pending row; already-completed or stale rows return false with no side effects.
	ApplyVerifiedWastage(ctx context.Context, p ApplyWastageParams) (bool, error)

	// BounceWastageForRework flips 'pending_verification' -> 'rework', stores the reason.
	// Idempotent and stale-guarded: a re-delivered verdict on a non-pending row is a no-op.
	BounceWastageForRework(ctx context.Context, p BounceWastageParams) (bool, error)

	// WastageMeasurementRecorded reports whether one completion already carries a measured leftover
	// weight. One indexed primary-key read.
	//
	// It exists for the APPROVE GATE: feed wastage cannot be approved without a number, and an item
	// measured earlier -- by an installed APK still using the separate save button -- must still be
	// approvable. Distinct from ErrWastageMeasurementRequired, which fails closed in the consumer
	// AFTER the verdict is already recorded and leaves the item stuck mid-apply; this answers
	// BEFORE the verdict so the verifier is told to enter the number instead.
	WastageMeasurementRecorded(ctx context.Context, tenantID, completionID string) (bool, error)

	// RecordWastageMeasurement stores the verifier's measured leftover weight on the completion
	// row, replacing any prior entry, in one transaction with its idempotency reservation and audit
	// row. Refuses a completion that does not exist (ErrWastageCompletionNotFound) and a value out
	// of range (ErrWastageValueOutOfRange). It never changes status: the verdict owns the lifecycle.
	RecordWastageMeasurement(ctx context.Context, p RecordWastageMeasurementParams) (RecordWastageMeasurementResult, error)
}
