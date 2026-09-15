package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Fasting task service surface (maintainer decision 2026-09-03, see
// domain/fasting.go for the product rule and the three clocks).

// ListMyFastingShedCards is the removal operator's list: ONE CARD PER SHED
// (maintainer correction #2, 2026-09-03) — the operator never opens an
// umbrella card to find the sheds inside. The 20:00 IST visibility window is
// applied inside the store's SQL with the service clock bound as a parameter;
// this method only decides WHO is asking.
func (s *Service) ListMyFastingShedCards(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.FastingShedCardPage, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.FastingShedCardPage{}, ports.ErrForbidden
	}
	if s.fasting == nil {
		return domain.FastingShedCardPage{}, ports.ErrNotFound
	}
	// The visibility window opens at the tenant's CONFIGURED cutoff; resolved
	// here and bound into the store's SQL so the read names no config table.
	cutoff, err := s.removalCutoff(ctx, actor.TenantID)
	if err != nil {
		return domain.FastingShedCardPage{}, err
	}
	page, err := s.fasting.ListFastingShedCardsForOperator(ctx, actor.TenantID, actor.UserID, s.clock(), cutoff, cursor, limit)
	if err != nil {
		return domain.FastingShedCardPage{}, err
	}
	// WEIGHING SOP: every card carries its task's pinned instruction, slot copy and questions.
	if err := s.removalCardCopy(ctx, actor.TenantID, page.Items); err != nil {
		return domain.FastingShedCardPage{}, err
	}
	return page, nil
}

// SubmitFastingShed records ONE shed's removal videos. The round's midnight
// gate is satisfied only when EVERY shed of the round is submitted — the store
// stamps the parent's submitted_at inside the same transaction as the LAST
// shed's submit. Verification is post-hoc and follows the EVIDENCE: one item
// per shed, carrying that shed's feed and water clips and naming the shed.
func (s *Service) SubmitFastingShed(ctx context.Context, actor domain.Actor, cmd domain.SubmitFastingShed) (domain.FastingShedCard, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.FastingShedCard{}, ports.ErrForbidden
	}
	if s.fasting == nil {
		return domain.FastingShedCard{}, ports.ErrNotFound
	}
	cmd.TenantID = actor.TenantID
	cmd.SubmittedBy = actor.UserID
	if !uuidutil.IsUUIDString(cmd.FastingTaskID) || !uuidutil.IsUUIDString(cmd.CampaignShedID) ||
		strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return domain.FastingShedCard{}, ports.ErrInvalidArgument
	}
	// BOTH videos, always. A missing ref is the named proof error so the
	// operator is told which work is owed, never a generic 400.
	if strings.TrimSpace(cmd.FeedProofRef) == "" || strings.TrimSpace(cmd.WaterProofRef) == "" {
		return domain.FastingShedCard{}, ports.ErrFastingProofRequired
	}
	if !uuidutil.IsUUIDString(cmd.FeedProofRef) || !uuidutil.IsUUIDString(cmd.WaterProofRef) {
		return domain.FastingShedCard{}, ports.ErrInvalidArgument
	}
	// WEIGHING SOP: the authored questions are answered with the clips and judged by the
	// rules the task was PLANNED on -- the same document the card rendered -- never by a
	// later publish. The task is read as the caller (assignment predicate inside the query),
	// so an unassigned caller learns nothing here that the submit would not tell them.
	task, err := s.fasting.FastingTaskByID(ctx, actor.TenantID, cmd.FastingTaskID, actor.UserID)
	if err != nil {
		return domain.FastingShedCard{}, err
	}
	rules, err := s.rulesForCampaign(ctx, actor.TenantID, task.CampaignID)
	if err != nil {
		return domain.FastingShedCard{}, err
	}
	if cmd.Answers == nil {
		cmd.Answers = domain.SOPAnswers{}
	}
	if err := rules.ValidateRemovalAnswers(cmd.Answers); err != nil {
		return domain.FastingShedCard{}, err
	}
	cmd.Answers = rules.NormalizeRemovalAnswers(cmd.Answers)
	result, err := s.fasting.SubmitFastingShed(ctx, cmd)
	if err != nil {
		return domain.FastingShedCard{}, err
	}
	if !result.Replayed || result.Card.Proofs == nil {
		result.Card.SOPVersion = rules.Version
		result.Card.Instruction = rules.FeedWaterRemoval.Instruction
		result.Card.Proofs = []domain.RemovalProofSlot{rules.RemovalProof(domain.RemovalProofFeed), rules.RemovalProof(domain.RemovalProofWater)}
		result.Card.Questions = rules.FeedWaterRemoval.Questions
		if result.Card.Questions == nil {
			result.Card.Questions = []domain.SOPQuestion{}
		}
	}
	// A replay may be the operator retrying after the submit committed but the
	// post-commit verification enqueue failed. Re-run the enqueue from the
	// replayed evidence; CreateItem is idempotent on this key, so an already
	// raised item no-ops while a missing item is repaired.
	if result.Evidence.FastingShedID != "" && result.Task.FastingTaskID != "" {
		if err := s.enqueueFastingShedVerification(ctx, result.Task, result.Evidence); err != nil {
			return domain.FastingShedCard{}, err
		}
	}
	return result.Card, nil
}

// enqueueFastingShedVerification raises ONE verification item for ONE shed's
// evidence — feed then water, in capture order — keyed on the shed row and its
// submitted row_version so an exact replay no-ops while a rework re-submit
// (which bumps the row) mints a fresh round for that shed alone.
func (s *Service) enqueueFastingShedVerification(ctx context.Context, task domain.FastingTask, shed domain.FastingShedProof) error {
	if s.enqueuer == nil {
		return nil
	}
	return s.enqueuer.EnqueueWeighingVerification(ctx, VerificationEnqueueRequest{
		TenantID:       task.TenantID,
		Category:       domain.VerificationRefTypeFasting,
		ObservationID:  shed.FastingShedID,
		CampaignID:     task.CampaignID,
		MediaRefs:      []string{shed.FeedProofRef, shed.WaterProofRef},
		OperatorID:     task.OperatorUserID,
		ShedID:         shed.ShedLocationID,
		ParkID:         task.ParkID,
		SubjectLabel:   domain.FastingShedSubjectLabel(shed.ShedLabel),
		CapturedAt:     derefSubmittedAt(task),
		IdempotencyKey: fmt.Sprintf("weighing:%s:%s:%d", domain.VerificationRefTypeFasting, shed.FastingShedID, shed.RowVersion),
	})
}

func derefSubmittedAt(task domain.FastingTask) time.Time {
	if task.SubmittedAt != nil {
		return *task.SubmittedAt
	}
	return time.Time{}
}
