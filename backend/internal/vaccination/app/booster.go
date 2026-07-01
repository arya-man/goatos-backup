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

// BoosterService implements SM-7: when a dose is administered, schedule the next dose in the series
// when that next rule is triggered after_previous_completion. The next dose is due administeredAt +
// max(offset_days, min_gap_days) so the minimum interval between doses is always respected.
// Idempotent — the deterministic obligation key makes a replay a no-op.
type BoosterService struct {
	proto    BoosterRuleReader
	versions BoosterVersionReader
	goats    BoosterGoatReader
	obl      ObligationWriter
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

// ScheduleNextInput identifies the just-administered dose and where the next obligation belongs.
type ScheduleNextInput struct {
	TenantID          string
	ProtocolVersionID string
	GoatID            string
	ScopeType         string
	ScopeID           string
	PrevSequence      int32     // sequence of the dose just administered
	AdministeredAt    time.Time // basis for the next due date
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

	candidate := next
	var due time.Time
	if next != nil && next.TriggerType == "after_previous_completion" {
		gap := next.OffsetDays
		if next.MinGapDays > gap {
			gap = next.MinGapDays // enforce the minimum interval
		}
		due = in.AdministeredAt.AddDate(0, 0, int(gap))
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
		if !found || !inCare(goat.LifecycleStatus) || !goatMatchesEligibility(goat, dsl.Eligibility, due) {
			return false, nil
		}
		deferReason = deferredReason(goat, dsl.Eligibility.DeferStates)
		if deferReason != "" {
			status = "deferred"
		}
	}

	key := obligationKey(in.TenantID, in.ProtocolVersionID, candidate.RuleID, "goat", in.GoatID,
		due.UTC().Format(time.RFC3339), strconv.Itoa(int(candidate.Sequence)))
	obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID:          in.TenantID,
		ProtocolVersionID: in.ProtocolVersionID,
		RuleID:            candidate.RuleID,
		TargetType:        "goat",
		TargetID:          in.GoatID,
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
		return administeredAt.AddDate(0, 0, int(gap)), true
	case "yearly":
		return administeredAt.AddDate(1, 0, 0), true
	default:
		return time.Time{}, false
	}
}
