package app

import (
	"context"
	"encoding/json"
	"strconv"
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
// after_previous_completion. Returns scheduled=false when there is no such next rule (series complete,
// or the next dose is calendar/age-triggered and already covered by SM-1), or on an idempotent replay.
func (s *BoosterService) ScheduleNextDose(ctx context.Context, in ScheduleNextInput) (scheduled bool, err error) {
	rules, err := s.proto.ListRules(ctx, in.TenantID, in.ProtocolVersionID)
	if err != nil {
		return false, err
	}
	var next *protodomain.Rule
	var nextSequence int32
	for i := range rules {
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
	if next == nil || next.TriggerType != "after_previous_completion" {
		return false, nil // series complete, or the immediate next dose is SM-1 scheduled
	}

	gap := next.OffsetDays
	if next.MinGapDays > gap {
		gap = next.MinGapDays // enforce the minimum interval
	}
	due := in.AdministeredAt.AddDate(0, 0, int(gap))

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

	key := obligationKey(in.TenantID, in.ProtocolVersionID, next.RuleID, "goat", in.GoatID,
		due.UTC().Format(time.RFC3339), strconv.Itoa(int(next.Sequence)))
	obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID:          in.TenantID,
		ProtocolVersionID: in.ProtocolVersionID,
		RuleID:            next.RuleID,
		TargetType:        "goat",
		TargetID:          in.GoatID,
		ScopeType:         in.ScopeType,
		ScopeID:           in.ScopeID,
		DueAt:             due,
		Status:            status,
		IdempotencyKey:    key,
		Sequence:          next.Sequence,
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
