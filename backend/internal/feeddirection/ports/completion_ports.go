package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// This file adds the module's FIRST write boundary. Until feed-direction completion, the package
// doc's "OWNS NO TABLES / no write path / no idempotency ledger" held; it now owns exactly one table
// (feed_direction_session_completions) behind CompletionStore, and every rule that made the read
// path safe still applies to the read half of that store.

var (
	// ErrShedRequired is returned when a completion omits the shed. Completion is shed-session
	// grained, so a park-wide completion has no meaning and fails closed.
	ErrShedRequired = errors.New("feeddirection: shed_id is required")
	// ErrInvalidSession is returned when a completion omits or malforms the session number. A
	// completion is always for ONE concrete feeding session (>= 1); session 0 ("every session") is a
	// read filter, never a completion target.
	ErrInvalidSession = errors.New("feeddirection: session_no must be a positive feeding session")
	// ErrWorkflowRequired is returned when a completion omits the workflow. Unlike a read (where empty
	// means "both"), a completion records ONE concrete dispatch workflow.
	ErrWorkflowRequired = errors.New("feeddirection: workflow is required for a completion")
	// ErrIdempotencyRequired is returned when a completion omits the idempotency key. Every mutating
	// write path must carry one so replays are safe.
	ErrIdempotencyRequired = errors.New("feeddirection: idempotency key is required")
	// ErrIdempotencyConflict is returned when the same idempotency key is replayed with a different
	// request payload -- the request is rejected without mutating state.
	ErrIdempotencyConflict = errors.New("feeddirection: idempotency key reused with a different request")
	// ErrShedNotInPark is returned when the addressed shed is not an active shed of the addressed park.
	ErrShedNotInPark = errors.New("feeddirection: shed is not an active shed of the addressed park")
	// ErrInvalidPartition is returned when a partitioned shed is completed without a real catalog
	// partition, or an undivided shed is completed with a fabricated partition label.
	ErrInvalidPartition = errors.New("feeddirection: partition_label is required and must match the shed partition catalog")
	// ErrInvalidProof is returned when a supplied proof reference does not resolve to a real,
	// completed, tenant-owned upload.
	ErrInvalidProof = errors.New("feeddirection: proof reference is invalid")
	// ErrCompletionUnavailable is returned when a completion is attempted but no CompletionStore is
	// wired -- a deployment/wiring error, surfaced as a 500 rather than a client error.
	ErrCompletionUnavailable = errors.New("feeddirection: completion store is not configured")
)

// CompleteSessionParams is the persisted completion write, at the shed-session grain
// (tenant, park, shed, session_no, target_date, workflow).
type CompleteSessionParams struct {
	TenantID   string
	ParkID     string
	ShedID     string
	SessionNo  int32
	TargetDate time.Time
	Workflow   string
	// ProofRefs is the OPTIONAL video proof. May be empty (completed with no video).
	ProofRefs []domain.ProofRef
	// CompletedBy is the operator principal uuid when the caller carries one, else "".
	CompletedBy string
	// IdempotencyKey is the client-supplied request key, reserved in the same transaction as the write.
	IdempotencyKey string
	// ActorID/ActorType/TraceID feed the audit row written in the same transaction.
	ActorID   string
	ActorType string
	TraceID   string
}

// CompleteSessionResult reports the outcome of a completion write.
type CompleteSessionResult struct {
	CompletionID string
	// Applied is false on an exact idempotent replay or a natural-key no-op (the shed-session was
	// already completed) -- the original result is returned and no new side effects ran.
	Applied bool
	Status  string
}

// CompletedSession identifies one completed shed-session for the serving-read overlay.
type CompletedSession struct {
	ShedID    string
	SessionNo int32
	Workflow  string
}

// CompletionStore owns the feed_direction_session_completions table.
//
// It is an OPTIONAL service dependency: a pure-generation unit test wires none, so the serve path
// simply overlays no completions and the write path is unavailable. Production wires it so the
// generator's rows report completion and POST /feed-direction/complete works.
type CompletionStore interface {
	// CompleteSession records that ONE shed-session was carried out, as a canonical producer of the
	// feed.direction.completed domain event: the canonical row, the audit entry, the transactional
	// outbox message, and the idempotency reservation are ONE transaction. Idempotent on both the
	// request key and the shed-session natural key.
	CompleteSession(ctx context.Context, p CompleteSessionParams) (CompleteSessionResult, error)

	// ListCompletedSessions returns every completed (shed, session, workflow) for one park-day in one
	// bounded indexed read. It is a DEDICATED read on the same footing as ListSessionTemplates -- NOT
	// the config snapshot -- so the read-count invariant (snapshot loaded exactly once per request) is
	// preserved. Bounded by the park's shed catalog x sessions, never by herd size.
	ListCompletedSessions(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]CompletedSession, error)
}

// ProofValidator validates OPTIONAL video-proof references attached to a completion.
//
// It is a narrow seam over the proof module so feeddirection does not depend on proofapp directly. A
// nil validator (unit tests, or a deployment with proof capture disabled) skips validation; when
// wired, it asserts each referenced proof is a real, completed, tenant-owned upload before the
// completion is written.
type ProofValidator interface {
	ValidateFeedProofs(ctx context.Context, tenantID string, proofIDs []string) error
	ValidateLiveCameraVideo(ctx context.Context, tenantID, proofID, shedID string) error
	// ValidateFeedProofMedia asserts each reference is a real, completed, tenant-owned upload AND is
	// the MEDIA KIND its step requires. It exists because ValidateFeedProofs deliberately does not look
	// at proof_type: for feed distribution the kind is the rule (weight = PHOTO, distribution and
	// water = VIDEO), and a submission that satisfies presence while carrying the wrong kind produces a
	// verification item the verifier cannot judge -- a still frame where a clip was promised.
	ValidateFeedProofMedia(ctx context.Context, tenantID string, expected []ExpectedProofMedia) error
}

// MediaKind is the capture kind a proof step demands.
type MediaKind string

const (
	// MediaKindPhoto is a still image (proof_type 'photo', mime image/*).
	MediaKindPhoto MediaKind = "photo"
	// MediaKindVideo is a clip (proof_type 'video', mime video/*).
	MediaKindVideo MediaKind = "video"
)

// ExpectedProofMedia pairs one proof reference with the kind its step requires, plus the error to
// return when the kind does not match. Carrying the error with the expectation keeps "which capture
// was wrong" answerable at the call site instead of collapsing every mismatch into one message.
type ExpectedProofMedia struct {
	ProofID string
	Kind    MediaKind
	// RequireLiveCamera additionally demands metadata capture_source == "in_app_camera", the same
	// property ValidateLiveCameraVideo enforces for transport. A weight photo pulled from the gallery
	// is a photo of a scale from some other day; only a live capture ties the reading to this pen's
	// feed, so the step that carries the number asserts it.
	RequireLiveCamera bool
	OnAbsent          error
}
