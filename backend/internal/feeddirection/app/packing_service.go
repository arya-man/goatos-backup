package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Feed PACKING verification gate -- the app half (maintainer decision, 2026-07-26, SUPERSEDING the
// "packing stays instant, no verifier" rule). A feed PACKING shed-session now passes through the
// generic Verification module before it is completed: the operator submits ONE mandatory packing video,
// which writes a 'pending_verification' feed_packing_completions row and enqueues ONE verification item;
// the session is 'completed' only when a verifier approves. This is a SEPARATE record from the old
// instant packing path (CompleteSession / feed_direction_session_completions), which is left inert, and
// from the distribution gate. See docs/decisions/feed-distribution-verification.md.

// ErrPackingEnqueuerNotWired is returned when a packing completion cannot enqueue its verification item
// because the enqueue seam was never wired -- a composition bug, surfaced loudly rather than silently
// stranding a pending_verification session.
var ErrPackingEnqueuerNotWired = errors.New("feeddirection: packing verification enqueuer is not wired")

// FeedPackingVerificationEnqueuer enqueues the mandatory-proof verification item for a submitted feed
// packing session (mirrors FeedDistributionVerificationEnqueuer). The composition layer adapts the
// verification module's CreateItem to this narrow port so feeddirection never touches verification's
// tables directly.
type FeedPackingVerificationEnqueuer interface {
	EnqueueFeedPackingVerification(ctx context.Context, in FeedPackingVerificationEnqueueRequest) error
}

// FeedPackingVerificationEnqueueRequest is one packing completion (its video) handed to the verifier
// queue.
type FeedPackingVerificationEnqueueRequest struct {
	TenantID        string
	CompletionID    string
	ParkID          string
	ShedID          string
	ShedName        string
	PartitionLabel  string
	SessionNo       int32
	Workflow        string
	TargetDate      time.Time
	PackingProofRef string
	OperatorID      string
	CapturedAt      time.Time
	IdempotencyKey  string
}

// CompletePackingInput is the app-level packing completion request the HTTP handler builds from the
// body plus the authenticated actor context.
type CompletePackingInput struct {
	TenantID        string
	ParkID          string
	ShedID          string
	SessionNo       int32
	TargetDate      time.Time
	Workflow        string
	PackingProofRef string
	CompletedBy     string
	IdempotencyKey  string
	ActorID         string
	ActorType       string
	TraceID         string
}

// CompletePacking records a packing shed-session's ONE mandatory video at 'pending_verification' and
// enqueues one verifier-queue item. It validates the request on the same terms as CompleteDistribution,
// then requires the video, validates it through the proof validator when wired, fails closed when the
// store or enqueue seam is missing, and enqueues only when the row actually enters pending_verification
// on this call. It completes NOTHING -- the session is completed only when a verifier approves (the
// consumer's ApplyVerifiedPacking).
func (s *Service) CompletePacking(ctx context.Context, in CompletePackingInput) (ports.CompletePackingResult, error) {
	if s.packing == nil {
		return ports.CompletePackingResult{}, ports.ErrPackingStoreUnavailable
	}
	if s.packingEnqueuer == nil {
		// Fail closed: without the verifier-queue seam a completion would flip a session to
		// pending_verification with nothing for a verifier to act on.
		return ports.CompletePackingResult{}, ErrPackingEnqueuerNotWired
	}

	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID)
	if err != nil {
		return ports.CompletePackingResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.Workflow = strings.TrimSpace(in.Workflow)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.PackingProofRef = strings.TrimSpace(in.PackingProofRef)

	if in.TenantID == "" || in.ParkID == "" {
		return ports.CompletePackingResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return ports.CompletePackingResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return ports.CompletePackingResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.SessionNo < 1 {
		return ports.CompletePackingResult{}, ports.ErrInvalidSession
	}
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return ports.CompletePackingResult{}, ports.ErrWorkflowRequired
	default:
		return ports.CompletePackingResult{}, ports.ErrInvalidWorkflow
	}
	if in.IdempotencyKey == "" {
		return ports.CompletePackingResult{}, ports.ErrIdempotencyRequired
	}

	// The packing VIDEO is mandatory -- reject before any state changes (there is nothing for a verifier
	// to approve without it).
	if in.PackingProofRef == "" {
		return ports.CompletePackingResult{}, ports.ErrPackingProofRequired
	}

	// When a validator is wired, the proof id must resolve to a real, completed, tenant-owned upload
	// before the completion is written.
	if s.proofs != nil {
		if err := s.proofs.ValidateFeedProofs(ctx, in.TenantID, []string{in.PackingProofRef}); err != nil {
			return ports.CompletePackingResult{}, err
		}
	}

	result, err := s.packing.CompletePacking(ctx, ports.CompletePackingParams{
		TenantID:        in.TenantID,
		ParkID:          in.ParkID,
		ShedID:          in.ShedID,
		SessionNo:       in.SessionNo,
		TargetDate:      in.TargetDate,
		Workflow:        in.Workflow,
		PackingProofRef: in.PackingProofRef,
		CompletedBy:     strings.TrimSpace(in.CompletedBy),
		IdempotencyKey:  in.IdempotencyKey,
		ActorID:         in.ActorID,
		ActorType:       in.ActorType,
		TraceID:         in.TraceID,
	})
	if err != nil {
		return ports.CompletePackingResult{}, err
	}

	// Enqueue the verifier item ONLY on a fresh pending transition (a new submit or a rework re-submit).
	// An idempotent replay or an already-pending/already-completed no-op enqueues nothing. The enqueue is
	// idempotent on (completion_id + row_version), so a retry after a prior enqueue failure heals rather
	// than duplicates: the completion is not "done" for the operator until the item is queued.
	if result.NewlyPending {
		if enqErr := s.packingEnqueuer.EnqueueFeedPackingVerification(ctx, FeedPackingVerificationEnqueueRequest{
			TenantID:        in.TenantID,
			CompletionID:    result.CompletionID,
			ParkID:          in.ParkID,
			ShedID:          in.ShedID,
			ShedName:        result.ShedName,
			PartitionLabel:  result.PartitionLabel,
			SessionNo:       in.SessionNo,
			Workflow:        in.Workflow,
			TargetDate:      in.TargetDate,
			PackingProofRef: in.PackingProofRef,
			OperatorID:      strings.TrimSpace(in.CompletedBy),
			CapturedAt:      s.now().UTC(),
			// Keyed to the completion + its row_version so a rework re-submit (row_version bumped) enqueues a
			// fresh item while a retry of the same submit collapses onto one queue item.
			IdempotencyKey: "feed-packing-verification:" + result.CompletionID + ":" + strconv.Itoa(int(result.RowVersion)),
		}); enqErr != nil {
			return ports.CompletePackingResult{}, enqErr
		}
	}
	return result, nil
}
