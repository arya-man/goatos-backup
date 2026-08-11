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
	// RationSummary is the FROZEN expected ration for this pen-SESSION -- "Maize 12.5 kg · Soya 4 kg"
	// -- carried onto the verifier's item so she can judge the video against what should have been
	// packed. Without it the queue item carried the shed, pen, session, operator and clip and nothing
	// about the feed, so a verifier could confirm a video EXISTED but not that the work was RIGHT
	// (STG 2026-08-09).
	//
	// Read from the ISSUED sheet at submit time and stored on the item, so re-authoring the feed
	// config afterwards cannot rewrite what the verifier is judging against. Blank when the sheet
	// cannot be read: a completion must never fail because its decoration could not be composed.
	RationSummary string
	// HeadCountSummary is the pen's projected head count for that session, the denominator the
	// ration was computed from. Blank when unknown.
	HeadCountSummary string
}

// CompletePackingInput is the app-level packing completion request the HTTP handler builds from the
// body plus the authenticated actor context.
type CompletePackingInput struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen the operator actually worked ("2", "Part 3"); empty for an
	// undivided shed. Carried end-to-end so ONE pen's proof closes ONE pen -- see migration 000137.
	PartitionLabel string
	// SessionNo is the feeding session the operator packed and filmed. Carried end-to-end so ONE
	// session's proof closes ONE session and the sibling bag is still owed.
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

	// Write path: the route already clamped the park to the caller's grant. See CompleteDistribution.
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
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
	// A packing completion names a REAL session. Session 0 is not "the whole day" -- that reading was
	// the 2026-08-10 pen-day grain and it is reverted; accepting 0 now would write a row no worklist
	// line matches, so the operator's bag would still show as owed.
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
		PartitionLabel:  in.PartitionLabel,
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
		ration, heads := s.packingExpectation(ctx, in)
		if enqErr := s.packingEnqueuer.EnqueueFeedPackingVerification(ctx, FeedPackingVerificationEnqueueRequest{
			TenantID:         in.TenantID,
			CompletionID:     result.CompletionID,
			ParkID:           in.ParkID,
			ShedID:           in.ShedID,
			ShedName:         result.ShedName,
			PartitionLabel:   result.PartitionLabel,
			SessionNo:        in.SessionNo,
			Workflow:         in.Workflow,
			TargetDate:       in.TargetDate,
			PackingProofRef:  in.PackingProofRef,
			OperatorID:       strings.TrimSpace(in.CompletedBy),
			RationSummary:    ration,
			HeadCountSummary: heads,
			CapturedAt:       s.now().UTC(),
			// Keyed to the completion + its row_version so a rework re-submit (row_version bumped) enqueues a
			// fresh item while a retry of the same submit collapses onto one queue item.
			IdempotencyKey: "feed-packing-verification:" + result.CompletionID + ":" + strconv.Itoa(int(result.RowVersion)),
		}); enqErr != nil {
			return ports.CompletePackingResult{}, enqErr
		}
	}
	return result, nil
}

// packingExpectation reads the FROZEN issued sheet and composes what this PEN-SESSION was expected
// to be packed with: the ration ("Maize 12.5 kg · Soya 4 kg") and the head count it was computed
// from. Both are display strings for the verifier's screen (dumb-renderer rule).
//
// ONE SESSION'S figures, not the day's. The verifier is judging one video of one bag, so the day
// total would be the wrong yardstick -- she would see twice the quantity the clip should show. The
// "Morning: ... | Evening: ..." breakdown that lived here between 2026-08-10 and 2026-08-11 existed
// only because one clip then covered both bags; with two clips again, each carries its own figure.
//
// FAIL-OPEN, deliberately. A completion is the operator's work reaching the server; it must never
// fail because a decoration could not be composed. An unreadable or never-issued sheet yields blank
// strings and the item simply carries no expectation -- exactly the state every item was in before
// this existed.
//
// It reads the same frozen rows the packing worklist serves, so the verifier sees byte-for-byte what
// the packer was shown, and reads them ONCE per completion (a submit, not a list) bounded by the
// park's pens x sessions x items -- never by herd size.
func (s *Service) packingExpectation(ctx context.Context, in CompletePackingInput) (ration string, heads string) {
	if s.issues == nil {
		return "", ""
	}
	feedDay := biztime.BusinessDate(in.TargetDate)
	scopeRows, _, served, err := s.loadServedRows(ctx, in.TenantID, in.ParkID, feedDay, in.Workflow)
	if err != nil || !served {
		return "", ""
	}
	// Match on the SAME identity the completion is keyed by -- shed + pen + session -- with the pen
	// normalized the way the natural key normalizes it, so "Part 3" and "part 3" are one pen.
	wantPartition := domain.PartitionMatchKey(in.PartitionLabel)
	for _, row := range domain.BuildPackingRows(scopeRows, domain.DistinctFeedItems(scopeRows)) {
		if row.ShedID != in.ShedID || row.SessionNo != in.SessionNo {
			continue
		}
		if domain.PartitionMatchKey(row.PartitionLabel) != wantPartition {
			continue
		}
		parts := make([]string, 0, len(row.Items))
		for _, item := range row.Items {
			// A blocked item has no resolved quantity. Naming it without one still tells the
			// verifier it was expected in the bag, which is more useful than dropping it silently.
			if item.QuantityKg == nil {
				parts = append(parts, item.FeedItem)
				continue
			}
			parts = append(parts, item.FeedItem+" "+*item.QuantityKg+" kg")
		}
		if len(parts) > 0 {
			ration = strings.Join(parts, " · ")
		}
		if row.HeadCount > 0 {
			heads = strconv.FormatInt(row.HeadCount, 10)
		}
		return ration, heads
	}
	return "", ""
}
