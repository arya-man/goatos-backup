package app

import (
	"context"
	"strconv"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// BoosterRuleReader is the slice of the protocol repo SM-7 needs.
type BoosterRuleReader interface {
	ListRules(ctx context.Context, tenantID, versionID string) ([]protodomain.Rule, error)
}

// BoosterService implements SM-7: when a dose is administered, schedule the next dose in the series
// when that next rule is triggered after_previous_completion. The next dose is due administeredAt +
// max(offset_days, min_gap_days) so the minimum interval between doses is always respected.
// Idempotent — the deterministic obligation key makes a replay a no-op.
type BoosterService struct {
	proto BoosterRuleReader
	obl   ObligationWriter
}

// NewBoosterService wires the protocol rule reader and the obligation writer.
func NewBoosterService(proto BoosterRuleReader, obl ObligationWriter) *BoosterService {
	return &BoosterService{proto: proto, obl: obl}
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
	for i := range rules {
		if rules[i].Sequence <= in.PrevSequence {
			continue
		}
		if rules[i].TriggerType != "after_previous_completion" {
			continue
		}
		if next == nil || rules[i].Sequence < next.Sequence {
			next = &rules[i]
		}
	}
	if next == nil {
		return false, nil // no booster step (series complete, or next dose is SM-1 scheduled)
	}

	gap := next.OffsetDays
	if next.MinGapDays > gap {
		gap = next.MinGapDays // enforce the minimum interval
	}
	due := in.AdministeredAt.AddDate(0, 0, int(gap))

	key := obligationKey(in.TenantID, in.ProtocolVersionID, next.RuleID, "goat", in.GoatID,
		due.UTC().Format(time.RFC3339), strconv.Itoa(int(next.Sequence)))
	_, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID:          in.TenantID,
		ProtocolVersionID: in.ProtocolVersionID,
		RuleID:            next.RuleID,
		TargetType:        "goat",
		TargetID:          in.GoatID,
		ScopeType:         in.ScopeType,
		ScopeID:           in.ScopeID,
		DueAt:             due,
		Status:            "scheduled",
		IdempotencyKey:    key,
		Sequence:          next.Sequence,
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}
