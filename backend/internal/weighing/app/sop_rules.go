package app

import (
	"context"
	"errors"
	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"strconv"
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

// withEffectiveCutoff fills the served rule set's cutoff with the evening the task actually
// runs against (the SOP's own, else the farm config), so the phone renders and never derives
// it. A farm with no evening at all leaves it blank; the create refuses that by name.
func (s *Service) withEffectiveCutoff(ctx context.Context, tenantID string, rules domain.Rules) domain.Rules {
	if rules.FeedWaterRemoval.Mode == domain.RemovalModeOff {
		return rules
	}
	cutoff, err := s.removalCutoff(ctx, tenantID, rules)
	if err != nil {
		return rules
	}
	rules.FeedWaterRemoval.CutoffTime = cutoff.String()
	return rules
}

// rulesForVersion reads one PINNED version's rules. A published version's document is
// immutable (an edit is a new version; a retired one still serves the tasks pinned to it),
// so the read is cached per (tenant, version) for the life of the process: the card list
// resolves every candidate version's evening and copy on every refresh, and without this
// each refresh was one SOP read per version ever pinned (PR #274 review round 3, finding 1).
// The published version itself is never cached here -- publishedRules reads it each time.
func (s *Service) rulesForVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 || s.sopRules == nil {
		return domain.SeededRules(), nil
	}
	key := tenantID + "|" + strconv.Itoa(version)
	s.versionRulesMu.Lock()
	cached, ok := s.versionRules[key]
	s.versionRulesMu.Unlock()
	if ok {
		return cached, nil
	}
	rules, err := s.sopRules.RulesVersion(ctx, tenantID, version)
	if err != nil {
		return domain.Rules{}, err
	}
	s.versionRulesMu.Lock()
	if s.versionRules == nil || len(s.versionRules) >= versionRulesCacheCap {
		// Bounded: a farm cannot publish its way into an unbounded map. Dropping the whole
		// map on overflow is a cold refresh, never a wrong answer.
		s.versionRules = map[string]domain.Rules{}
	}
	s.versionRules[key] = rules
	s.versionRulesMu.Unlock()
	return rules, nil
}

// versionRulesCacheCap bounds the per-process pinned-version cache (tenants x versions).
const versionRulesCacheCap = 256

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
// removalCardCutoffs resolves the card list's window per pinned version: the farm's
// configured evening as the default, and each pinned version's own evening where the
// document sets one. A version the farm never published falls back to the default, the
// same leniency removalCardCopy gives its copy, so tonight's card is still listed.
func (s *Service) removalCardCutoffs(ctx context.Context, tenantID, operatorUserID string) (ports.RemovalCutoffs, error) {
	farm, err := s.removalCutoff(ctx, tenantID, domain.SeededRules())
	if err != nil {
		return ports.RemovalCutoffs{}, err
	}
	out := ports.RemovalCutoffs{Default: farm, ByVersion: map[int]fwrdomain.Cutoff{}}
	versions, err := s.fasting.FastingCardSOPVersions(ctx, tenantID, operatorUserID)
	if err != nil {
		return ports.RemovalCutoffs{}, err
	}
	for _, v := range versions {
		rules, err := s.rulesForVersion(ctx, tenantID, v)
		if errors.Is(err, ports.ErrSOPVersionUnknown) {
			continue
		}
		if err != nil {
			return ports.RemovalCutoffs{}, err
		}
		if rules.FeedWaterRemoval.CutoffTime == "" {
			continue
		}
		cutoff, err := s.removalCutoff(ctx, tenantID, rules)
		if err != nil {
			return ports.RemovalCutoffs{}, err
		}
		out.ByVersion[v] = cutoff
	}
	return out, nil
}

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
		cards[i].Proofs = rules.RemovalProofs()
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
		if rules != nil {
			effective := s.withEffectiveCutoff(ctx, tenantID, *rules)
			rules = &effective
		}
		items[i].SOP = rules
	}
	return nil
}
