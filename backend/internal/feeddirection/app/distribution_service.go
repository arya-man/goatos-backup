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

// Feed DISTRIBUTION verification gate -- the app half (maintainer decision, 2026-07-26). A
// feed-direction shed-session now passes through the generic Verification module before it is
// completed: the operator submits three mandatory proofs (feed weight photo + feed-distribution video +
// water-distribution video), which writes a 'pending_verification' feed_distribution_completions row and enqueues ONE verification
// item; the session is 'completed' only when a verifier approves. This is entirely separate from the
// untouched feed PACKING path (CompleteSession / feed_direction_session_completions). See
// docs/decisions/feed-distribution-verification.md.

// ErrDistributionEnqueuerNotWired is returned when a distribution completion cannot enqueue its
// verification item because the enqueue seam was never wired -- a composition bug, surfaced loudly
// rather than silently stranding a pending_verification session.
var ErrDistributionEnqueuerNotWired = errors.New("feeddirection: distribution verification enqueuer is not wired")

// FeedDistributionVerificationEnqueuer enqueues the mandatory-proof verification item for a submitted
// feed distribution (mirrors counts' ShiftingVerificationEnqueuer). The composition layer adapts the
// verification module's CreateItem to this narrow port so feeddirection never touches verification's
// tables directly.
type FeedDistributionVerificationEnqueuer interface {
	EnqueueFeedDistributionVerification(ctx context.Context, in FeedDistributionVerificationEnqueueRequest) error
}

// FeedDistributionVerificationEnqueueRequest is one distribution completion (all three proofs) handed to the
// verifier queue.
type FeedDistributionVerificationEnqueueRequest struct {
	TenantID             string
	CompletionID         string
	ParkID               string
	ShedID               string
	PartitionLabel       string
	SessionNo            int32
	Workflow             string
	TargetDate           time.Time
	FeedWeightProofRef   string
	DistributionProofRef string
	WaterProofRef        string
	OperatorID           string
	CapturedAt           time.Time
	IdempotencyKey       string
}

// CompleteDistributionInput is the app-level distribution completion request the HTTP handler builds
// from the body plus the authenticated actor context.
type CompleteDistributionInput struct {
	TenantID             string
	ParkID               string
	ShedID               string
	PartitionLabel       string
	SessionNo            int32
	TargetDate           time.Time
	Workflow             string
	FeedWeightProofRef   string
	DistributionProofRef string
	WaterProofRef        string
	CompletedBy          string
	IdempotencyKey       string
	ActorID              string
	ActorType            string
	TraceID              string
}

// CompleteDistribution records a shed-session's three mandatory proofs at 'pending_verification' and
// enqueues one verifier-queue item. It validates the request on the same terms as CompleteSession,
// then requires all three proofs, validates them through the proof validator when wired, fails closed when
// the store or enqueue seam is missing, and enqueues only when the row actually enters
// pending_verification on this call. It relocates/completes NOTHING -- the session is completed only
// when a verifier approves (the consumer's ApplyVerifiedDistribution).
func (s *Service) CompleteDistribution(ctx context.Context, in CompleteDistributionInput) (ports.CompleteDistributionResult, error) {
	if s.distributions == nil {
		return ports.CompleteDistributionResult{}, ports.ErrDistributionStoreUnavailable
	}
	if s.distributionEnqueuer == nil {
		// Fail closed: without the verifier-queue seam a completion would flip a session to
		// pending_verification with nothing for a verifier to act on.
		return ports.CompleteDistributionResult{}, ErrDistributionEnqueuerNotWired
	}

	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return ports.CompleteDistributionResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.PartitionLabel = strings.TrimSpace(in.PartitionLabel)
	in.Workflow = strings.TrimSpace(in.Workflow)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.FeedWeightProofRef = strings.TrimSpace(in.FeedWeightProofRef)
	in.DistributionProofRef = strings.TrimSpace(in.DistributionProofRef)
	in.WaterProofRef = strings.TrimSpace(in.WaterProofRef)

	if in.TenantID == "" || in.ParkID == "" {
		return ports.CompleteDistributionResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return ports.CompleteDistributionResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return ports.CompleteDistributionResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.SessionNo < 1 {
		return ports.CompleteDistributionResult{}, ports.ErrInvalidSession
	}
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return ports.CompleteDistributionResult{}, ports.ErrWorkflowRequired
	default:
		return ports.CompleteDistributionResult{}, ports.ErrInvalidWorkflow
	}
	if in.IdempotencyKey == "" {
		return ports.CompleteDistributionResult{}, ports.ErrIdempotencyRequired
	}

	// ALL THREE proofs are mandatory -- reject before any state changes (there is nothing for a
	// verifier to approve without them). Checked in CAPTURE ORDER so the error names the earliest
	// missing step: an operator who shot nothing is told to weigh the feed, not to film the water.
	if in.FeedWeightProofRef == "" {
		return ports.CompleteDistributionResult{}, ports.ErrFeedWeightProofRequired
	}
	if in.DistributionProofRef == "" {
		return ports.CompleteDistributionResult{}, ports.ErrDistributionProofRequired
	}
	if in.WaterProofRef == "" {
		return ports.CompleteDistributionResult{}, ports.ErrWaterProofRequired
	}

	// When a validator is wired, each proof id must resolve to a real, completed, tenant-owned upload
	// AND be the media kind its step requires: the weight is a PHOTO, the other two are VIDEOS.
	if s.proofs != nil {
		if err := s.proofs.ValidateFeedProofMedia(ctx, in.TenantID, []ports.ExpectedProofMedia{
			{ProofID: in.FeedWeightProofRef, Kind: ports.MediaKindPhoto, RequireLiveCamera: true, OnAbsent: ports.ErrFeedWeightProofRequired},
			{ProofID: in.DistributionProofRef, Kind: ports.MediaKindVideo, OnAbsent: ports.ErrDistributionProofRequired},
			{ProofID: in.WaterProofRef, Kind: ports.MediaKindVideo, OnAbsent: ports.ErrWaterProofRequired},
		}); err != nil {
			return ports.CompleteDistributionResult{}, err
		}
	}

	result, err := s.distributions.CompleteDistribution(ctx, ports.CompleteDistributionParams{
		TenantID:             in.TenantID,
		ParkID:               in.ParkID,
		ShedID:               in.ShedID,
		PartitionLabel:       in.PartitionLabel,
		SessionNo:            in.SessionNo,
		TargetDate:           in.TargetDate,
		Workflow:             in.Workflow,
		FeedWeightProofRef:   in.FeedWeightProofRef,
		DistributionProofRef: in.DistributionProofRef,
		WaterProofRef:        in.WaterProofRef,
		CompletedBy:          strings.TrimSpace(in.CompletedBy),
		IdempotencyKey:       in.IdempotencyKey,
		ActorID:              in.ActorID,
		ActorType:            in.ActorType,
		TraceID:              in.TraceID,
	})
	if err != nil {
		return ports.CompleteDistributionResult{}, err
	}

	// Enqueue whenever the resulting row is awaiting verification. Use the canonical proof refs returned
	// from the store, not the current request body, so already-pending repair retries cannot queue media
	// different from the feed_distribution_completions row. The enqueue is idempotent on
	// (completion_id + row_version), so exact retries and already-pending retries heal a prior enqueue
	// failure without duplicating a verifier item. Completed rows are left alone.
	if result.Status == domain.DistributionStatusPendingVerification {
		if enqErr := s.distributionEnqueuer.EnqueueFeedDistributionVerification(ctx, FeedDistributionVerificationEnqueueRequest{
			TenantID:             in.TenantID,
			CompletionID:         result.CompletionID,
			ParkID:               in.ParkID,
			ShedID:               result.ShedID,
			PartitionLabel:       result.PartitionLabel,
			SessionNo:            in.SessionNo,
			Workflow:             in.Workflow,
			TargetDate:           in.TargetDate,
			FeedWeightProofRef:   result.FeedWeightProofRef,
			DistributionProofRef: result.DistributionProofRef,
			WaterProofRef:        result.WaterProofRef,
			OperatorID:           strings.TrimSpace(in.CompletedBy),
			CapturedAt:           s.now().UTC(),
			// Keyed to the completion + its row_version so a rework re-submit (row_version bumped) enqueues a
			// fresh item while a retry of the same submit collapses onto one queue item.
			IdempotencyKey: "feed-distribution-verification:" + result.CompletionID + ":" + strconv.Itoa(int(result.RowVersion)),
		}); enqErr != nil {
			return ports.CompleteDistributionResult{}, enqErr
		}
	}
	return result, nil
}
