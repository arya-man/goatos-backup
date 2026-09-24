package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func TestOperatorDaySummaryUnionsAnimalsWithPendingBeforeDone(t *testing.T) {
	summaries := map[string]*domain.ShedCardSummary{
		"a": {ShedID: "shed", RosterMemberships: []domain.ExecutionRosterMembership{
			{PlannedDate: "2026-06-23", IncludeWhenOverdue: true, Status: domain.WorkStateVerificationPending, Animals: []domain.MembershipAnimal{{ID: "same-goat", Done: true, Accepted: true}}},
		}},
		"b": {ShedID: "shed", RosterMemberships: []domain.ExecutionRosterMembership{
			{PlannedDate: "2026-06-24", Status: domain.WorkStateDue, Animals: []domain.MembershipAnimal{{ID: "same-goat", Open: true}}},
			{PlannedDate: "2026-06-25", Status: domain.WorkStateDue, Animals: []domain.MembershipAnimal{{ID: "future-goat", Open: true}}},
			{PlannedDate: "2026-06-22", IncludeWhenOverdue: false, Status: domain.WorkStateCompleted, Animals: []domain.MembershipAnimal{{ID: "closed-goat", Done: true}}},
		}},
	}
	addOperatorDaySummaries(summaries, time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))
	if len(summaries["b"].OperatorDaySummaries) != 0 {
		t.Fatal("day summaries must be emitted once per pen")
	}
	byDate := map[string]domain.OperatorDaySummary{}
	for _, day := range summaries["a"].OperatorDaySummaries {
		byDate[day.BusinessDate] = day
	}
	today := byDate["2026-06-24"]
	if today.TargetCount != 1 || today.OpenCount != 1 || today.DoneCount != 0 || today.AcceptedCount != 0 || today.Status != domain.WorkStateDue {
		t.Fatalf("pending must win for same animal: %#v", today)
	}
	previous := byDate["2026-06-23"]
	if previous.TargetCount != 1 || previous.DoneCount != 1 || previous.OpenCount != 0 {
		t.Fatalf("other selected dates must exclude overdue: %#v", previous)
	}
	payload, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"same-goat", "future-goat", "closed-goat", "animals"} {
		if strings.Contains(string(payload), id) {
			t.Fatalf("internal animal identity leaked: %s", payload)
		}
	}
}
