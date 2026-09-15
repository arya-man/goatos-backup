package app

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// WEIGHING SOP rule resolution (maintainer decision 2026-09-15, domain/sop.go).
//
// Two questions, asked at two moments:
//
//	publishedRules   -- what a task planned NOW is stamped with (create, planner catalog);
//	rulesForCampaign -- what an EXISTING task runs under (edit, capture, the removal card).
//
// Both fail open to the SEEDED rules when nothing is wired: the seeded document is the
// pre-SOP behaviour, so a test fake or a process without the source runs exactly what shipped
// before the SOP existed. A task pinned to a version the farm never published is a data
// error and fails closed (ports.ErrSOPVersionUnknown).

// WithSOPRules wires the rules source (the out-of-package Postgres adapter) and the pin
// reader (the postgres Repository).
func (s *Service) WithSOPRules(src ports.SOPRulesSource, pins ports.SOPPinReader) *Service {
	s.sopRules = src
	s.sopPins = pins
	return s
}

func (s *Service) publishedRules(ctx context.Context, tenantID string) (domain.Rules, error) {
	if s.sopRules == nil {
		return domain.SeededRules(), nil
	}
	return s.sopRules.PublishedRules(ctx, tenantID)
}

func (s *Service) rulesForVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 || s.sopRules == nil {
		return domain.SeededRules(), nil
	}
	return s.sopRules.RulesVersion(ctx, tenantID, version)
}

func (s *Service) rulesForCampaign(ctx context.Context, tenantID, campaignID string) (domain.Rules, error) {
	if s.sopPins == nil {
		return domain.SeededRules(), nil
	}
	version, err := s.sopPins.CampaignSOPVersion(ctx, tenantID, campaignID)
	if err != nil {
		return domain.Rules{}, err
	}
	return s.rulesForVersion(ctx, tenantID, version)
}

// applyPlanningRules is the SOP half of a create/edit: capture modes offered, the default
// cap, and whether THIS task carries the feed & water removal precondition. It normalizes the
// command in place (dropping a removal operator the SOP does not ask for, stamping the
// version) and returns whether the removal applies, so the caller runs the matching date rule.
//
// hasFasting is the task's current state on an EDIT (false on create): under `optional`, an
// edit that says nothing about the removal keeps what the task has.
func applyPlanningRules(rules domain.Rules, cmd *domain.CreateCampaign, hasFasting bool) (bool, error) {
	for _, shed := range cmd.Sheds {
		if !rules.ModeAllowed(shed.WeighingCategory) {
			return false, ports.ErrModeNotAllowed
		}
	}
	if cmd.PlannedCapPerDay <= 0 {
		cmd.PlannedCapPerDay = rules.Planning.DefaultCapPerDay
	}
	cmd.SOPVersion = rules.Version
	requested := cmd.FeedWaterRemovalRequested
	switch rules.FeedWaterRemoval.Mode {
	case domain.RemovalModeOff:
		if requested != nil && *requested {
			return false, ports.ErrRemovalNotOffered
		}
		// An older APK always sends the operator; under OFF it is simply not an assignment.
		cmd.FastingOperatorUserID = ""
		return false, nil
	case domain.RemovalModeOptional:
		if requested == nil {
			// "Not said": on an edit keep what the task has; on a create (or an edit of a
			// task without a round) the presence of an operator IS the ask.
			applies := hasFasting || strings.TrimSpace(cmd.FastingOperatorUserID) != ""
			if !applies {
				cmd.FastingOperatorUserID = ""
			}
			return applies, nil
		}
		if !*requested {
			cmd.FastingOperatorUserID = ""
		}
		return *requested, nil
	default:
		return true, nil
	}
}

// validateRemovalAssignment is the operator half of a task that carries the removal: the
// evening-shift person is mandatory and park-scoped like every bucket operator.
func validateRemovalAssignment(cmd domain.CreateCampaign) error {
	if strings.TrimSpace(cmd.FastingOperatorUserID) == "" {
		return ports.ErrFastingOperatorRequired
	}
	if !uuidutil.IsUUIDString(cmd.FastingOperatorUserID) {
		return ports.ErrInvalidArgument
	}
	return nil
}

// removalCardCopy decorates one operator card with its task's pinned SOP copy. Cards on one
// page may belong to different tasks pinned to different versions; the resolved rule sets are
// memoized per version so a page costs one source read per DISTINCT version, bounded by the
// page size.
func (s *Service) removalCardCopy(ctx context.Context, tenantID string, cards []domain.FastingShedCard) error {
	byVersion := map[int]domain.Rules{}
	for i := range cards {
		rules, ok := byVersion[cards[i].SOPVersion]
		if !ok {
			var err error
			rules, err = s.rulesForVersion(ctx, tenantID, cards[i].SOPVersion)
			if errors.Is(err, ports.ErrSOPVersionUnknown) {
				// A card whose task points at a version the farm never published still has
				// to be workable tonight: render the seeded copy and let the submit's own
				// validation report the pin.
				rules = domain.SeededRules()
				err = nil
			}
			if err != nil {
				return err
			}
			byVersion[cards[i].SOPVersion] = rules
		}
		cards[i].Instruction = rules.FeedWaterRemoval.Instruction
		cards[i].Proofs = []domain.RemovalProofSlot{rules.RemovalProof(domain.RemovalProofFeed), rules.RemovalProof(domain.RemovalProofWater)}
		cards[i].Questions = rules.FeedWaterRemoval.Questions
		if cards[i].Questions == nil {
			cards[i].Questions = []domain.SOPQuestion{}
		}
	}
	return nil
}

// decorateCampaignRules attaches each task's PINNED rule set (Campaign.SOP). A page may hold
// tasks pinned to different versions; the rule sets are memoized per version, so a page costs
// one source read per DISTINCT version, bounded by the page size. A task pinned to a version
// the farm never published is left without rules rather than failing the whole page -- the
// write paths still refuse it by name.
func (s *Service) decorateCampaignRules(ctx context.Context, tenantID string, items []domain.Campaign) error {
	byVersion := map[int]*domain.Rules{}
	for i := range items {
		rules, seen := byVersion[items[i].SOPVersion]
		if !seen {
			resolved, err := s.rulesForVersion(ctx, tenantID, items[i].SOPVersion)
			switch {
			case errors.Is(err, ports.ErrSOPVersionUnknown):
				rules = nil
			case err != nil:
				return err
			default:
				rules = &resolved
			}
			byVersion[items[i].SOPVersion] = rules
		}
		items[i].SOP = rules
	}
	return nil
}
