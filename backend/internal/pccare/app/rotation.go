package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// PC CARE ROTATION (maintainer instruction 2026-10-02, docs/decisions/pc-care-rotation.md).
//
// A Preventive Care SOP card may ROTATE instead of repeating every N days: the work goes round
// the park one pen a day.
//
//   - The next pen is planned once EVERY pen of the latest task is SUBMITTED by the operator
//     (waiting for the verifier counts; the verifier's pace never holds the farm back).
//   - It is the next pen WITH ANIMALS IN IT, in the park's pen order -- the Care Coverage board's
//     own occupied-pen set and order, so the two screens agree on which pens exist.
//   - ONE PEN PER DAY: the day after the later of the submit and the last planned date.
//   - After the last pen it wraps to the first, waiting the card's gap first.
//   - Same operators as the last pen; anyone who can no longer be assigned drops off, and with
//     nobody left the rotation STOPS and the planner of the last task is alerted once. Nobody is
//     substituted.
//   - A planner starts a rotation by planning the first pen by hand and stops it by closing the
//     open pen: a closed pen is never submitted, so nothing follows it.

// RotationResult reports one tick.
type RotationResult struct {
	PensCreated int
	PensSkipped int
	Conflicts   int
}

// RunRotation performs one bounded rotation tick for a tenant.
func (s *Service) RunRotation(ctx context.Context, tenantID string, store ports.RotationStore, alerter RepeatAlerter, limit int) (RotationResult, error) {
	var result RotationResult
	if store == nil || s.rounds == nil {
		return result, ports.ErrStoreUnavailable
	}
	rules, err := s.publishedRules(ctx, tenantID)
	if err != nil {
		return result, err
	}
	cfg := []ports.RotationConfig{}
	for _, category := range domain.PlannerCategories {
		if gap, ok := rules.RotationGapDays(category); ok {
			cfg = append(cfg, ports.RotationConfig{Category: category, GapDays: gap, SOPVersion: rules.Version})
		}
	}
	if len(cfg) == 0 {
		return result, nil
	}
	today := biztime.BusinessDayStart(s.now())
	candidates, err := store.ListRotationCandidates(ctx, tenantID, cfg, today, today.AddDate(0, 0, RepeatLeadDays), limit)
	if err != nil {
		return result, err
	}
	for _, c := range candidates {
		ok, err := s.rotationSourceStillApplies(ctx, tenantID, rules, c)
		if err != nil {
			return result, err
		}
		if !ok {
			continue
		}
		// scale-guard:ignore: bounded by the per-tick candidate LIMIT and at most one row per (category, park); one small availability read and one single-pen round create each, never per animal.
		created, skipped, conflict, err := s.rotateOne(ctx, tenantID, rules, store, alerter, c)
		if err != nil {
			return result, err
		}
		result.PensCreated += created
		result.PensSkipped += skipped
		if conflict {
			result.Conflicts++
		}
	}
	return result, nil
}

func (s *Service) rotationSourceStillApplies(ctx context.Context, tenantID string, published domain.Rules, c ports.RotationCandidate) (bool, error) {
	if c.SourceSOPVersion == published.Version {
		return true, nil
	}
	sourceRules, err := s.rulesForVersion(ctx, tenantID, c.SourceSOPVersion)
	if errors.Is(err, ports.ErrSOPVersionUnknown) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok := sourceRules.RotationGapDays(c.Category)
	return ok, nil
}

func (s *Service) rotateOne(ctx context.Context, tenantID string, rules domain.Rules, store ports.RotationStore, alerter RepeatAlerter, c ports.RotationCandidate) (created, skipped int, conflict bool, err error) {
	next := time.Date(c.NextDate.Year(), c.NextDate.Month(), c.NextDate.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
	available, err := store.OperatorsAvailableInPark(ctx, tenantID, c.ParkID, c.AssigneeUserIDs)
	if err != nil {
		return 0, 0, false, err
	}
	stillAble := intersectIDs(c.AssigneeUserIDs, available)
	if len(stillAble) == 0 {
		inserted, err := store.RecordRepeatSkip(ctx, tenantID, c.SourceTaskID, RepeatSkipNoOperator, next, c.CreatedBy)
		if err != nil {
			return 0, 0, false, err
		}
		if inserted && alerter != nil {
			if err := alerter.NotifyRepeatSkipped(ctx, tenantID, RepeatSkip{
				PlannerUserID: c.CreatedBy, Category: c.Category, ParkID: c.ParkID, ParkName: c.ParkName,
				PenLabels: []string{oploc.OperationalLocation{ShedName: c.ShedName, PartitionLabel: c.PartitionLabel}.Display()},
				DueDate:   next, Reason: RepeatSkipNoOperator, Rotation: true,
			}); err != nil {
				return 0, 0, false, err
			}
		}
		return 0, 1, false, nil
	}
	pen := domain.RoundPen{ShedID: c.ShedID, PartitionLabel: c.PartitionLabel}
	_, err = s.rounds.CreateRound(ctx, ports.CreateRoundParams{
		TenantID:            tenantID,
		Category:            c.Category,
		ParkID:              c.ParkID,
		Pens:                []domain.RoundPen{pen},
		PlannedBusinessDate: next,
		AssigneeUserIDs:     stillAble,
		// The validator refuses a rotation on work the removal applies to, so a rotating pen
		// never carries one.
		SOPVersion:       rules.Version,
		SlotKeys:         allKeys(slotsAsAuthored(rules.CategorySlots(c.Category))),
		RequiredSlotKeys: rules.RequiredSlotKeys(c.Category),
		IdempotencyKey:   "pc-care-rotation:" + c.SourceTaskID,
		RepeatOf:         map[string]string{pen.PenKey(): c.SourceTaskID},
		// The planner of the last pen stays the owner of the next: they can end it, and they are
		// who is told when the rotation cannot go on.
		CreatedBy: c.CreatedBy,
		ActorID:   c.CreatedBy,
		ActorType: "system",
		TraceID:   "pc-care-rotation",
	})
	if errors.Is(err, domain.ErrTaskAlreadyPlanned) {
		// Someone planned this pen for that day by hand (or a racing tick won); the next tick
		// reads the latest task again and carries on from there.
		return 0, 0, true, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("pccare rotation: create round: %w", err)
	}
	return 1, 0, false, nil
}
