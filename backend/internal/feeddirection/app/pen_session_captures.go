package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// PenSessionCapturesInput addresses ONE pen-session. Same identity as a completion: shed alone
// answers for the wrong pen.
type PenSessionCapturesInput struct {
	TenantID       string
	ParkID         string
	ShedID         string
	PartitionLabel string
	SessionNo      int32
	TargetDate     time.Time
	Workflow       string
	// The caller's own park set, forwarded so the proof module can authorize the shed itself.
	AuthorizedParkIDs []string
}

// ListPenSessionCaptures reports which proof slots of a pen-session are already recorded, by any
// operator, and the SERVER proof id of each.
//
// Read-only and additive: it changes no completion state and gates nothing. It exists so three
// operators can split a pen-session's three proofs -- one shoots the weight photo, one the feed
// video, one the water video -- and so whoever submits can reference proofs they did not shoot
// (maintainer decision 2026-08-14). The completion route already accepts another operator's proof:
// ValidateFeedProofMedia checks tenant, upload state and media kind, never the uploader.
// PenSessionCapturesResult pairs the recorded proof slots with the pen-session's completion
// status IN ONE ANSWER. The mobile proof screen's read-only gate previously came from a stale
// list-row snapshot while this fetch answered only the slots — a submitted session opened
// editable for up to one poll interval (field bug 2026-08-15). Slots and status now travel
// together so the screen paints both from the same instant.
type PenSessionCapturesResult struct {
	Slots []ports.CapturedProofSlot
	// SessionStatus is the RAW feed_distribution_completions status for this exact pen-session
	// ("pending_verification", "completed", "rework"); empty when no completion row exists yet.
	SessionStatus string
}

func (s *Service) ListPenSessionCaptures(
	ctx context.Context, in PenSessionCapturesInput,
) (PenSessionCapturesResult, error) {
	if s.proofs == nil {
		// No validator wired (a pure-generation unit build). Nothing is discoverable, which degrades
		// to today's single-phone behaviour rather than failing the screen.
		return PenSessionCapturesResult{}, nil
	}
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return PenSessionCapturesResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.Workflow = strings.TrimSpace(in.Workflow)

	if in.TenantID == "" || in.ParkID == "" {
		return PenSessionCapturesResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return PenSessionCapturesResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return PenSessionCapturesResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.SessionNo < 1 {
		// Same rule as the completion: 0 is not "the whole day", it is a value no worklist line
		// matches (docs/decisions/feed-distribution-verification.md).
		return PenSessionCapturesResult{}, ports.ErrInvalidSession
	}
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return PenSessionCapturesResult{}, ports.ErrWorkflowRequired
	default:
		return PenSessionCapturesResult{}, ports.ErrInvalidWorkflow
	}

	slots, err := s.proofs.ListPenSessionCaptures(ctx, ports.PenSessionCaptureQuery{
		TenantID:          in.TenantID,
		ParkID:            in.ParkID,
		ShedID:            in.ShedID,
		PartitionLabel:    strings.TrimSpace(in.PartitionLabel),
		SessionNo:         in.SessionNo,
		TargetDate:        in.TargetDate,
		Workflow:          in.Workflow,
		AuthorizedParkIDs: in.AuthorizedParkIDs,
	})
	if err != nil {
		return PenSessionCapturesResult{}, err
	}

	// The completion status for THIS pen-session, from the same authoritative table the list
	// overlay reads. Fail-open on error: slots still answer (single-phone behaviour), and the
	// mobile gate treats an absent status as "no answer", never as "editable".
	status := ""
	if s.distributions != nil {
		statuses, serr := s.distributions.ListDistributionSessionStatuses(ctx, in.TenantID, in.ParkID, in.TargetDate)
		if serr == nil {
			wantPartition := domain.PartitionMatchKey(in.PartitionLabel)
			for _, st := range statuses {
				if st.ShedID == in.ShedID &&
					st.SessionNo == in.SessionNo &&
					st.Workflow == in.Workflow &&
					domain.PartitionMatchKey(st.PartitionLabel) == wantPartition {
					status = st.Status
					break
				}
			}
		}
	}
	return PenSessionCapturesResult{Slots: slots, SessionStatus: status}, nil
}
