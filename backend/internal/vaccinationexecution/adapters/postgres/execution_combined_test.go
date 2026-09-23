package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func TestExecutionCardSummaryUsesCanonicalVaccineLabelsAndAssignmentIdentity(t *testing.T) {
	summaries := map[string]*domain.ShedCardSummary{}
	assignmentID := "assignment-castro"
	addExecutionCardSummary(summaries, executionCardSummaryRecord{
		ShedID:             "shed-castro",
		AssignmentID:       &assignmentID,
		ObligationCount:    9,
		OpenCount:          9,
		VaccineLabels:      []string{"BT", "ET+TT", "PPR"},
		VaccineLabelCounts: []string{"\x1fBT", "\x1fBT", "\x1fBT", "\x1fET+TT", "\x1fET+TT", "\x1fET+TT", "\x1fPPR", "\x1fPPR", "\x1fPPR"},
	})

	cardID := domain.BuildAssignmentCardID("shed-castro", "", assignmentID, "", "", "")
	summary := summaries[cardID]
	if summary == nil {
		t.Fatalf("missing assignment card %q in %#v", cardID, summaries)
	}
	got := make(map[string]string, len(summary.VaccineGroups))
	for _, group := range summary.VaccineGroups {
		got[group.Label] = group.CountLabel
	}
	want := map[string]string{"BT": "3 doses", "ET+TT": "3 doses", "PPR": "3 doses"}
	for label, count := range want {
		if got[label] != count {
			t.Fatalf("group %s=%q want %q; groups=%#v", label, got[label], count, summary.VaccineGroups)
		}
	}
}

func TestExecutionCardSummaryNormalizesPartitionKeyLikeAndroid(t *testing.T) {
	summaries := map[string]*domain.ShedCardSummary{}
	assignmentID := "assignment-yashoda"
	partitionLabel := "Part 3"

	addExecutionCardSummary(summaries, executionCardSummaryRecord{
		ShedID:          "shed-yashoda",
		PartitionLabel:  &partitionLabel,
		AssignmentID:    &assignmentID,
		ObligationCount: 2,
		OpenCount:       2,
		VaccineLabels:   []string{"ET+TT"},
	})

	androidCardID := domain.BuildAssignmentCardID("shed-yashoda", "3", assignmentID, "", "", "")
	if summaries[androidCardID] == nil {
		t.Fatalf("missing Android-normalized card %q in %#v", androidCardID, summaries)
	}
	rawCardID := "shed:shed-yashoda|partition:Part 3|assignment:" + assignmentID
	if summaries[rawCardID] != nil {
		t.Fatalf("summary leaked raw partition key %q", rawCardID)
	}
}
