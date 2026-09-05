package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// CreateRoundInput is the planner's round create as built by the HTTP handler. It is
// CreateTaskInput with the single pen replaced by a pen LIST — the planner already ticked
// several pens in the wizard, and this is the create that finally carries all of them
// (maintainer decision 2026-09-05).
type CreateRoundInput struct {
	Category string
	ParkID   string
	// Pens are the pens this round covers. One entry is the ordinary single-pen plan; the
	// round shape is the same either way, so there is no second code path to keep in step.
	Pens                []RoundPenInput
	PlannedBusinessDate string
	AssigneeUserIDs     []string
	// FeedRemovalRequired (deworming only) asks for the evening-before feed & water removal:
	// ONE round-grain card, one feed video + one water video PER PEN.
	FeedRemovalRequired bool
	// RemovalOperatorUserIDs are the operators for that card (>=1 when the toggle is on).
	RemovalOperatorUserIDs []string
	IdempotencyKey         string
	ActorID                string
	ActorType              string
	TraceID                string
}

// RoundPenInput is one pen named by the client: identity only. The display label is
// resolved server-side from the pen catalog, so a client cannot label a pen with a name
// the farm does not use.
type RoundPenInput struct {
	ShedID         string
	PartitionLabel string
}

// CreateRound plans a round covering one or more pens. Authority is identical to
// CreateTask — PCCarePlan (CEO) for any planner category, PCCarePlanTrimming (Breeding
// Director) for hoof/hair trimming — because planning four pens at once is the same act of
// planning, not a new one.
func (s *Service) CreateRound(ctx context.Context, actor domain.Actor, in CreateRoundInput) (ports.RoundRow, error) {
	if !canPlanAny(actor) {
		return ports.RoundRow{}, ports.ErrForbidden
	}
	in.ParkID = strings.TrimSpace(in.ParkID)
	if !uuidutil.IsUUIDString(in.ParkID) {
		return ports.RoundRow{}, ports.ErrInvalidArgument
	}
	in.Category = strings.TrimSpace(in.Category)
	if !domain.IsValidCategory(in.Category) {
		return ports.RoundRow{}, domain.ErrInvalidCategory
	}
	if domain.IsKernelOwnedCategory(in.Category) {
		return ports.RoundRow{}, domain.ErrKernelOwnedCategory
	}
	// A category that is real and human-plannable but not ROUND-plannable is refused with its
	// own error rather than the kernel-owned one, so the message names the actual reason.
	if !domain.IsRoundPlannableCategory(in.Category) {
		return ports.RoundRow{}, domain.ErrRoundCategoryNotPlannable
	}
	// The category gate comes AFTER the category is known to be real, so a trimming planner
	// sending deworming is told "not yours" rather than "no such category".
	if !canPlanCategory(actor, in.Category) {
		return ports.RoundRow{}, ports.ErrForbidden
	}
	if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, in.ParkID, planCapabilitiesForCategory(in.Category)...); err != nil {
		return ports.RoundRow{}, err
	}

	pens := make([]domain.RoundPen, 0, len(in.Pens))
	for _, pen := range in.Pens {
		shedID := strings.TrimSpace(pen.ShedID)
		if !uuidutil.IsUUIDString(shedID) {
			return ports.RoundRow{}, ports.ErrInvalidArgument
		}
		pens = append(pens, domain.RoundPen{ShedID: shedID, PartitionLabel: strings.TrimSpace(pen.PartitionLabel)})
	}
	if err := domain.ValidateRoundPens(pens); err != nil {
		return ports.RoundRow{}, err
	}

	in.PlannedBusinessDate = strings.TrimSpace(in.PlannedBusinessDate)
	if !isBusinessDate(in.PlannedBusinessDate) {
		return ports.RoundRow{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.RoundRow{}, ports.ErrIdempotencyRequired
	}
	assignees, err := dedupUUIDList(in.AssigneeUserIDs)
	if err != nil {
		return ports.RoundRow{}, err
	}
	if len(assignees) == 0 {
		return ports.RoundRow{}, domain.ErrAssigneesRequired
	}
	planned, err := time.ParseInLocation("2006-01-02", in.PlannedBusinessDate, biztime.DefaultLocation())
	if err != nil {
		return ports.RoundRow{}, ports.ErrInvalidArgument
	}

	// Feed & water removal precondition. Honored on deworming ONLY and validate-or-reject on
	// every other category — a dropped toggle would read to the planner as accepted while
	// creating no removal card at all.
	var removalOperators []string
	if in.FeedRemovalRequired || len(in.RemovalOperatorUserIDs) > 0 {
		if in.Category != domain.CategoryDeworming || !in.FeedRemovalRequired {
			return ports.RoundRow{}, domain.ErrFeedRemovalNotApplicable
		}
		removalOperators, err = dedupUUIDList(in.RemovalOperatorUserIDs)
		if err != nil {
			return ports.RoundRow{}, err
		}
		if len(removalOperators) == 0 {
			return ports.RoundRow{}, domain.ErrRemovalOperatorsRequired
		}
		// 20:00 IST planning cutoff: the removal happens the EVENING BEFORE, so the earliest
		// deworming date is tomorrow before 20:00 IST and the day after tomorrow from 20:00
		// on. Business-DAY comparison on the service's injectable clock — never now±N hours.
		// The cutoff is a property of the ROUND, not of a pen: one evening, one crew.
		if planned.Before(domain.EarliestFeedRemovalDewormingDate(s.now())) {
			return ports.RoundRow{}, domain.ErrFastingWindowClosed
		}
	}

	if s.rounds == nil {
		return ports.RoundRow{}, ports.ErrStoreUnavailable
	}
	return s.rounds.CreateRound(ctx, ports.CreateRoundParams{
		TenantID:               actor.TenantID,
		Category:               in.Category,
		ParkID:                 in.ParkID,
		Pens:                   pens,
		PlannedBusinessDate:    planned,
		AssigneeUserIDs:        assignees,
		FeedRemovalRequired:    in.FeedRemovalRequired,
		RemovalOperatorUserIDs: removalOperators,
		IdempotencyKey:         strings.TrimSpace(in.IdempotencyKey),
		CreatedBy:              actor.UserID,
		ActorID:                in.ActorID,
		ActorType:              in.ActorType,
		TraceID:                in.TraceID,
	})
}

// GetRound reads one round with its pen buckets, clamped to the caller's authorized parks.
// The read is offered to planners and monitors alike: a round is a planning artefact the
// monitor list also shows.
func (s *Service) GetRound(ctx context.Context, actor domain.Actor, roundID string) (ports.RoundRow, error) {
	roundID = strings.TrimSpace(roundID)
	if !uuidutil.IsUUIDString(roundID) {
		return ports.RoundRow{}, ports.ErrInvalidArgument
	}
	if s.rounds == nil {
		return ports.RoundRow{}, ports.ErrStoreUnavailable
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planOrMonitorParkCapabilities...)
	return s.rounds.GetRound(ctx, actor.TenantID, roundID, authorizedParkSlice(parks), tenantWide)
}

// CloseRound ends a whole round: every pen that is not already completed or closed, plus the
// round's feed & water removal card. Refused while ANY pen of the round holds evidence
// awaiting a verdict — the gate is unconditional, and the answer to a round that will not
// close is to resolve the verification, never to route around it.
func (s *Service) CloseRound(ctx context.Context, actor domain.Actor, roundID, reason, traceID string) error {
	if !canPlanAny(actor) {
		return ports.ErrForbidden
	}
	roundID = strings.TrimSpace(roundID)
	if !uuidutil.IsUUIDString(roundID) {
		return ports.ErrInvalidArgument
	}
	if strings.TrimSpace(reason) == "" {
		return domain.ErrCloseReasonRequired
	}
	if s.rounds == nil {
		return ports.ErrStoreUnavailable
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planCapabilities...)
	round, err := s.rounds.GetRound(ctx, actor.TenantID, roundID, authorizedParkSlice(parks), tenantWide)
	if err != nil {
		return err
	}
	if !canPlanCategory(actor, round.Category) {
		return ports.ErrForbidden
	}
	if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, round.ParkID, planCapabilitiesForCategory(round.Category)...); err != nil {
		return err
	}
	return s.rounds.CloseRound(ctx, ports.CloseRoundParams{
		TenantID: actor.TenantID,
		RoundID:  round.RoundID,
		Reason:   strings.TrimSpace(reason),
		ClosedBy: actor.UserID,
		ActorID:  actor.UserID,
		TraceID:  traceID,
	})
}

// RegisterRemovalPenProofInput attaches ONE pen's feed or water video to a round-grain feed
// & water removal card. The pen is named by the WORK TASK it gates, so a client cannot aim
// a pen's evidence at a pen outside the gated round.
type RegisterRemovalPenProofInput struct {
	RemovalTaskID  string
	GatedTaskID    string
	SlotKey        string
	ProofRef       string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// RegisterRemovalPenProof stores one pen's removal video. Authority is the same as any other
// capture: assignee membership on the removal card plus a live-camera proof that really is a
// video — a removal proved by a still photograph is not proof that anything was carried out.
func (s *Service) RegisterRemovalPenProof(ctx context.Context, actor domain.Actor, in RegisterRemovalPenProofInput) error {
	in.RemovalTaskID = strings.TrimSpace(in.RemovalTaskID)
	if err := s.requireAssignee(ctx, actor, in.RemovalTaskID); err != nil {
		return err
	}
	in.GatedTaskID = strings.TrimSpace(in.GatedTaskID)
	if !uuidutil.IsUUIDString(in.GatedTaskID) {
		return ports.ErrInvalidArgument
	}
	slot := strings.TrimSpace(in.SlotKey)
	if slot != domain.SlotFeedVideo && slot != domain.SlotWaterVideo {
		return domain.ErrInvalidSlotForCategory
	}
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	if in.ProofRef == "" {
		return ports.ErrProofRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.ErrIdempotencyRequired
	}
	if s.proofs != nil {
		if err := s.proofs.ValidateLiveCameraProofKind(ctx, actor.TenantID, []string{in.ProofRef}, "video"); err != nil {
			return err
		}
	}
	if s.rounds == nil {
		return ports.ErrStoreUnavailable
	}
	return s.rounds.RegisterRemovalPenProof(ctx, ports.RegisterRemovalPenProofParams{
		TenantID:       actor.TenantID,
		RemovalTaskID:  in.RemovalTaskID,
		GatedTaskID:    in.GatedTaskID,
		SlotKey:        slot,
		ProofRef:       in.ProofRef,
		CapturedBy:     actor.UserID,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		ActorID:        in.ActorID,
		ActorType:      in.ActorType,
		TraceID:        in.TraceID,
	})
}

// RemovalPenProofs reads a removal card's per-pen evidence rows — the operator's slot list
// (which pens still owe a video) and the reader's evidence trail.
//
// Read-gated exactly like the captures poll and the roster: ANY principal who can read the
// task can read its pens. Gating this on ASSIGNEE membership was the first cut and it was
// wrong twice over — it contradicted the route's own permission list (which admits plan,
// monitor and oversee) and it hid a card's pens from the leadership who must be able to see
// what the evening covered. The WRITE below stays assignee-only; reading is not capturing.
func (s *Service) RemovalPenProofs(ctx context.Context, actor domain.Actor, removalTaskID string) ([]ports.RemovalPenProofRow, error) {
	removalTaskID = strings.TrimSpace(removalTaskID)
	if _, err := s.GetTask(ctx, actor, removalTaskID); err != nil {
		return nil, err
	}
	if s.rounds == nil {
		return nil, ports.ErrStoreUnavailable
	}
	return s.rounds.ListRemovalPenProofs(ctx, actor.TenantID, removalTaskID)
}
