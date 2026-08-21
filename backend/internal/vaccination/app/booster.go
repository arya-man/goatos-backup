package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// BoosterRuleReader is the slice of the protocol repo SM-7 needs.
type BoosterRuleReader interface {
	ListRules(ctx context.Context, tenantID, versionID string) ([]protodomain.Rule, error)
}

type BoosterVersionReader interface {
	GetVersion(ctx context.Context, tenantID, versionID string) (protodomain.Version, error)
}

type BoosterGoatReader interface {
	GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (domain.EligibleGoat, bool, error)
}

type BoosterCrossVaccineGapReader interface {
	LastRecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string]domain.RecentVaccineAdministration, error)
}

type BoosterCrossVaccineHistoryReader interface {
	RecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string][]domain.RecentVaccineAdministration, error)
}

// BoosterService implements SM-7: when a dose is administered, schedule the next dose in the series
// when that next rule is triggered after_previous_completion. The next dose is due administeredAt +
// max(offset_days, min_gap_days) so the minimum interval between doses is always respected.
// Idempotent — the deterministic obligation key makes a replay a no-op.
type BoosterService struct {
	proto           BoosterRuleReader
	versions        BoosterVersionReader
	goats           BoosterGoatReader
	crossVaccineGap BoosterCrossVaccineGapReader
	crossHistory    BoosterCrossVaccineHistoryReader
	obl             ObligationWriter
}

// NewBoosterService wires the protocol rule reader and the obligation writer.
func NewBoosterService(proto BoosterRuleReader, obl ObligationWriter) *BoosterService {
	s := &BoosterService{proto: proto, obl: obl}
	if versions, ok := proto.(BoosterVersionReader); ok {
		s.versions = versions
	}
	return s
}

// WithGoatReader lets SM-7 apply the same current goat eligibility/defer-state rules as SM-1.
func (s *BoosterService) WithGoatReader(goats BoosterGoatReader) *BoosterService {
	s.goats = goats
	return s
}

// WithCrossVaccineGapReader applies cross-vaccine gap floors when scheduling the next dose.
func (s *BoosterService) WithCrossVaccineGapReader(reader BoosterCrossVaccineGapReader) *BoosterService {
	s.crossVaccineGap = reader
	if history, ok := reader.(BoosterCrossVaccineHistoryReader); ok {
		s.crossHistory = history
	}
	return s
}

// ScheduleNextInput identifies the just-administered dose and where the next obligation belongs.
type ScheduleNextInput struct {
	TenantID          string
	ProtocolVersionID string
	GoatID            string
	ScopeType         string
	ScopeID           string
	PrevSequence      int32     // sequence of the dose just administered
	AdministeredAt    time.Time // basis for the next due date

	// CompletedObligationID is the obligation that was just administered -- the CAUSE of
	// the successor this call mints. It becomes the successor's repeat-cycle anchor, which
	// is what lets one completed dose mint exactly one open successor however many times
	// the completion is replayed, and however far the successor's due date later moves.
	// Empty means the caller could not identify the source: no metadata is written and the
	// previous due-date behaviour applies unchanged.
	CompletedObligationID string
}

// ScheduleNextDose schedules the next higher-sequence dose when that rule is triggered
// after_previous_completion. When the completed rule is a repeatable adult revaccination row and the
// series has no higher sequence left, it schedules the same rule's next repeat cycle from the accepted
// administered_at. Returns scheduled=false when no next/recurring dose is due, or on an idempotent
// replay.
func (s *BoosterService) ScheduleNextDose(ctx context.Context, in ScheduleNextInput) (scheduled bool, err error) {
	rules, err := s.proto.ListRules(ctx, in.TenantID, in.ProtocolVersionID)
	if err != nil {
		return false, err
	}
	var current *protodomain.Rule
	var next *protodomain.Rule
	var nextSequence int32
	for i := range rules {
		if rules[i].Sequence == in.PrevSequence {
			current = &rules[i]
		}
		if rules[i].Sequence <= in.PrevSequence {
			continue
		}
		if next == nil ||
			rules[i].Sequence < nextSequence ||
			(rules[i].Sequence == nextSequence && (rules[i].SortOrder < next.SortOrder ||
				(rules[i].SortOrder == next.SortOrder && rules[i].RuleID < next.RuleID))) {
			next = &rules[i]
			nextSequence = rules[i].Sequence
		}
	}

	// A repeat/revac row owns its OWN recurrence: once its vaccine's dose series has no
	// higher-sequence row left, the completed dose starts the next cycle at the rule's
	// authored interval. "The series" is per VACCINE, not per protocol version — the
	// published CPT matrix is one version carrying every vaccine, with rule sequences
	// drawn from a single global counter, so a revac row is followed by the NEXT
	// VACCINE's primary rows. Testing `next == nil` version-wide therefore silenced the
	// recurrence of every vaccine except the last one in the matrix. Resolved before the
	// generic next-dose branch so cross-vaccine wave chaining (e.g. PPR -> Goat Pox
	// second wave) is untouched for non-repeat rows.
	repeatOnly, err := isTerminalRepeatRuleForItsVaccine(current, rules, in.PrevSequence)
	if err != nil {
		return false, err
	}

	candidate := next
	var due time.Time
	if repeatOnly {
		var ok bool
		due, ok = repeatDueAfterCompletion(*current, in.AdministeredAt)
		if !ok {
			return false, nil
		}
		candidate = current
	} else if next != nil && next.TriggerType == "after_previous_completion" {
		gap := next.OffsetDays
		if next.MinGapDays > gap {
			gap = next.MinGapDays // enforce the minimum interval
		}
		if gap <= 0 {
			return false, nil
		}
		due = businessDayStart(in.AdministeredAt).AddDate(0, 0, int(gap))
	} else if next == nil && current != nil {
		var ok bool
		due, ok = repeatDueAfterCompletion(*current, in.AdministeredAt)
		if !ok {
			return false, nil
		}
		candidate = current
	} else {
		return false, nil // immediate next dose is SM-1 scheduled
	}

	status := "scheduled"
	deferReason := ""
	if s.goats != nil && s.versions != nil {
		version, err := s.versions.GetVersion(ctx, in.TenantID, in.ProtocolVersionID)
		if err != nil {
			return false, err
		}
		dsl, err := parseGenerationDSL(version.RuleDsl)
		if err != nil {
			return false, err
		}
		goat, found, err := s.goats.GetGoatForGeneration(ctx, in.TenantID, in.GoatID)
		if err != nil {
			return false, err
		}
		policies := versionPoliciesFromDSL(dsl)
		ruleEligibility, ruleVaccine, err := ruleGenerationContext(*candidate, dsl.Eligibility, vaccineProfileFromDSL(dsl))
		if err != nil {
			return false, err
		}
		if s.crossHistory != nil {
			admins, err := s.crossHistory.RecentVaccineAdministrationsForGoats(ctx, in.TenantID, []string{in.GoatID}, in.AdministeredAt)
			if err != nil {
				return false, err
			}
			due = applyCrossVaccineGapFloorFromHistory(due, admins[in.GoatID], ruleVaccine, policies.Compatibility)
		} else if s.crossVaccineGap != nil {
			admins, err := s.crossVaccineGap.LastRecentVaccineAdministrationsForGoats(ctx, in.TenantID, []string{in.GoatID}, in.AdministeredAt)
			if err != nil {
				return false, err
			}
			if admin, ok := admins[in.GoatID]; ok {
				due = applyCrossVaccineGapFloor(due, &admin, ruleVaccine, policies.Compatibility)
			}
		}
		if !found || !inCare(goat.LifecycleStatus) || !goatMatchesEligibility(goat, ruleEligibility, policies.Pregnancy, due) {
			return false, nil
		}
		deferStates := ruleEligibility.DeferStates
		if len(deferStates) == 0 {
			deferStates = dsl.Eligibility.DeferStates
		}
		deferReason = deferredReason(goat, deferStates)
		if deferReason == "" {
			deferReason = policyDeferReason(goat, policies, due)
		}
		if deferReason != "" {
			status = "deferred"
		}
	}

	key := obligationKey(in.TenantID, in.ProtocolVersionID, candidate.RuleID, "goat", in.GoatID,
		due.UTC().Format(time.RFC3339), strconv.Itoa(int(candidate.Sequence)))
	// Anchored to the administration that caused it, for repeat rules only. A one-off dose
	// keeps its due-date identity, because its due date does not move on its own.
	//
	// The reference is the administration itself, not the completed obligation's id, because
	// generation recomputes this same cycle from history and names its cause that way. Two
	// vocabularies for one cause means two open rows, each invisible to the other. The
	// obligation id is still recorded as the anchor, for the audit trail and for the stricter
	// per-cause index.
	// The cause is named by the vaccine that was GIVEN, not by the rule about to be
	// scheduled. For a repeat those are the same rule, but a next-in-chain dose can belong to
	// a different rule, and generation names this cause from the administration itself. Take
	// the administered rule's vaccine so the two writers cannot disagree.
	var repeatCycle *obldomain.RepeatCycleSource
	if anchor := strings.TrimSpace(in.CompletedObligationID); anchor != "" && isRepeatRule(candidate) {
		// Read inside the gate. Evaluated unconditionally, a rule whose vaccine block does not
		// parse failed the completion event outright -- including for doses that write no
		// metadata at all and used to schedule perfectly well.
		administeredVaccineCode, err := boosterRuleVaccineCode(*current)
		if err != nil {
			return false, err
		}
		administered := in.AdministeredAt
		nextDue := due
		if ref := obldomain.RepeatCycleRef(administeredVaccineCode, administered, in.PrevSequence); ref != "" {
			repeatCycle = &obldomain.RepeatCycleSource{
				Source:             obldomain.RepeatCycleSourceTrustedHistory,
				SourceRef:          ref,
				AnchorObligationID: &anchor,
				AnchorAt:           &administered,
				DueAt:              &nextDue,
			}
		}
	}
	obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID:          in.TenantID,
		ProtocolVersionID: in.ProtocolVersionID,
		RuleID:            candidate.RuleID,
		TargetType:        "goat",
		TargetID:          in.GoatID,
		RepeatCycle:       repeatCycle,
		ScopeType:         in.ScopeType,
		ScopeID:           in.ScopeID,
		DueAt:             due,
		Status:            status,
		IdempotencyKey:    key,
		Sequence:          candidate.Sequence,
	})
	if err != nil {
		return false, err
	}
	if applied && deferReason != "" {
		payload, _ := json.Marshal(map[string]string{"reason": "defer_state", "defer_status": deferReason})
		if _, _, err := s.obl.RecordStatusEvent(ctx, obldomain.NewStatusEvent{
			TenantID:       in.TenantID,
			ObligationID:   obID,
			EventType:      "deferred",
			OccurredAt:     in.AdministeredAt,
			Payload:        payload,
			IdempotencyKey: obID + ":deferred:" + in.AdministeredAt.UTC().Format(time.RFC3339Nano),
			Scope:          "obligation.status_event",
			RequestHash:    "defer:" + deferReason,
		}); err != nil {
			return false, err
		}
	}
	return applied, nil
}

// boosterRuleVaccineCode reads a rule's OWN vaccine identity from the per-rule
// eligibility_json that protocol publish materializes for every matrix row
// (matrixRuleEligibilityJSON). Returns "" for a legacy/plain rule that carries no
// per-row vaccine metadata.
func boosterRuleVaccineCode(rule protodomain.Rule) (string, error) {
	_, vaccine, err := ruleGenerationContext(rule, genEligibility{}, vaccineProfile{})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(vaccine.Code), nil
}

// sameBoosterVaccineSeries reports whether a candidate rule belongs to the completed
// rule's own vaccine series. When either side has no per-row vaccine identity (a legacy
// single-vaccine protocol version), the whole version is one series, preserving the
// pre-matrix behaviour.
func sameBoosterVaccineSeries(current, candidate string) bool {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if current == "" || candidate == "" {
		return true
	}
	return strings.EqualFold(current, candidate)
}

// isTerminalRepeatRuleForItsVaccine reports whether the just-completed rule is a
// repeat/revac row (repeat = every_n_days | yearly) that has no higher-sequence row of
// its OWN vaccine left — i.e. it is the end of that vaccine's dose series and therefore
// owns the next cycle itself.
func isTerminalRepeatRuleForItsVaccine(current *protodomain.Rule, rules []protodomain.Rule, prevSequence int32) (bool, error) {
	if current == nil {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(current.Repeat)) {
	case "every_n_days", "yearly":
	default:
		return false, nil
	}
	currentVaccine, err := boosterRuleVaccineCode(*current)
	if err != nil {
		return false, err
	}
	if currentVaccine == "" {
		// No per-row vaccine identity: fall back to the version-wide `next == nil` test.
		return false, nil
	}
	for i := range rules {
		if rules[i].Sequence <= prevSequence {
			continue
		}
		candidateVaccine, err := boosterRuleVaccineCode(rules[i])
		if err != nil {
			return false, err
		}
		if sameBoosterVaccineSeries(currentVaccine, candidateVaccine) {
			return false, nil // this vaccine's series continues
		}
	}
	return true, nil
}

func repeatDueAfterCompletion(rule protodomain.Rule, administeredAt time.Time) (time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(rule.Repeat)) {
	case "every_n_days":
		gap := rule.MinGapDays
		if rule.OffsetDays > gap {
			gap = rule.OffsetDays
		}
		if gap <= 0 {
			return time.Time{}, false
		}
		return businessDayStart(administeredAt).AddDate(0, 0, int(gap)), true
	case "yearly":
		return businessDayStart(administeredAt).AddDate(1, 0, 0), true
	default:
		return time.Time{}, false
	}
}

// isRepeatRule reports whether a rule's obligations are repeat cycles -- the only ones that
// carry repeat-cycle metadata.
func isRepeatRule(rule *protodomain.Rule) bool {
	if rule == nil {
		return false
	}
	repeat := strings.TrimSpace(rule.Repeat)
	if repeat != "" && !strings.EqualFold(repeat, "none") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(rule.TriggerType), "after_previous_completion")
}
