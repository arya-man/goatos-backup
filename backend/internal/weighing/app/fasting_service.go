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
	return s.fasting.ListFastingShedCardsForOperator(ctx, actor.TenantID, actor.UserID, s.clock(), cursor, limit)
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
	result, err := s.fasting.SubmitFastingShed(ctx, cmd)
	if err != nil {
		return domain.FastingShedCard{}, err
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
