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
func (s *Service) ListPenSessionCaptures(
	ctx context.Context, in PenSessionCapturesInput,
) ([]ports.CapturedProofSlot, error) {
	if s.proofs == nil {
		// No validator wired (a pure-generation unit build). Nothing is discoverable, which degrades
		// to today's single-phone behaviour rather than failing the screen.
		return nil, nil
	}
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return nil, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.Workflow = strings.TrimSpace(in.Workflow)

	if in.TenantID == "" || in.ParkID == "" {
		return nil, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return nil, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return nil, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.SessionNo < 1 {
		// Same rule as the completion: 0 is not "the whole day", it is a value no worklist line
		// matches (docs/decisions/feed-distribution-verification.md).
		return nil, ports.ErrInvalidSession
	}
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return nil, ports.ErrWorkflowRequired
	default:
		return nil, ports.ErrInvalidWorkflow
	}

	return s.proofs.ListPenSessionCaptures(ctx, ports.PenSessionCaptureQuery{
		TenantID:          in.TenantID,
		ParkID:            in.ParkID,
		ShedID:            in.ShedID,
		PartitionLabel:    strings.TrimSpace(in.PartitionLabel),
		SessionNo:         in.SessionNo,
		TargetDate:        in.TargetDate,
		Workflow:          in.Workflow,
		AuthorizedParkIDs: in.AuthorizedParkIDs,
	})
}
