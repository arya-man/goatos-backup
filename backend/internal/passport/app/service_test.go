package app

import (
	"context"
	"strings"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type fakeVacc struct {
	history []vaccdomain.CompletionHistoryItem
	last    vaccdomain.LastAccepted
	found   bool
}

func (f fakeVacc) GoatHistory(context.Context, string, string, int32) ([]vaccdomain.CompletionHistoryItem, error) {
	return f.history, nil
}
func (f fakeVacc) LastAccepted(context.Context, string, string) (vaccdomain.LastAccepted, bool, error) {
	return f.last, f.found, nil
}

type fakeObl struct{ open []obldomain.OpenObligation }

func (f fakeObl) ListOpenByGoat(context.Context, string, string, int32) ([]obldomain.OpenObligation, error) {
	return f.open, nil
}

type fakeLoc struct {
	loc   oploc.OperationalLocation
	found bool
}

func (f fakeLoc) GoatLocation(context.Context, string, string) (oploc.OperationalLocation, bool, error) {
	return f.loc, f.found, nil
}

func TestGetPassportComposesAndPicksNextDue(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	svc := NewService(
		fakeVacc{
			history: []vaccdomain.CompletionHistoryItem{
				{CompletionID: "c1", ObligationID: "o0", Status: "accepted", AdministeredAt: t0, Doses: 1, DoseCode: "et_tt_adult_w2", VaccineLabel: "ET+TT"},
			},
			last:  vaccdomain.LastAccepted{CompletionID: "c1", ObligationID: "o0", AdministeredAt: t0},
			found: true,
		},
		fakeObl{open: []obldomain.OpenObligation{
			{ObligationID: "o1", RuleID: "r1", ScopeType: "shed", ScopeID: "shed1", BatchID: "b1", DueAt: t0, ClinicalDueAt: t0, ScheduledFor: &t1, Status: "due", Sequence: 1, DoseCode: "et_tt_adult_w1", VaccineLabel: "ET+TT"}, // earliest → next due
			{ObligationID: "o2", DueAt: t1, Status: "scheduled", Sequence: 2},
		}},
		fakeLoc{found: true, loc: oploc.OperationalLocation{ParkID: "park1", ParkName: "CPT", ShedID: "shed1", ShedName: "Castro", PartitionLabel: "2"}},
	)

	p, err := svc.GetPassport(ctx, "tenant", "goat")
	if err != nil {
		t.Fatalf("get passport: %v", err)
	}
	if p.GoatID != "goat" || len(p.OpenObligations) != 2 || len(p.VaccinationHistory) != 1 {
		t.Fatalf("passport shape: %+v", p)
	}
	if p.OperationalLocationDisplay != "Castro - 2" || p.ParkName != "CPT" || p.ShedID != "shed1" || p.PartitionLabel != "2" {
		t.Fatalf("passport location: %+v", p)
	}
	if p.NextDue == nil || p.NextDue.ObligationID != "o1" {
		t.Fatalf("next due: want o1, got %+v", p.NextDue)
	}
	if p.NextDue.WorkflowRowID != "batch:b1:rule:r1:shed:shed1" {
		t.Fatalf("workflow row id: %q", p.NextDue.WorkflowRowID)
	}
	if p.NextDue.DisplayLabel != "ET+TT W1" || p.NextDue.ScheduledFor == nil || !p.NextDue.ScheduledFor.Equal(t1) {
		t.Fatalf("next due display fields: %+v", p.NextDue)
	}
	if p.OpenObligations[1].WorkflowRowID != "obligation:o2" {
		t.Fatalf("fallback workflow row id: %q", p.OpenObligations[1].WorkflowRowID)
	}
	if got := p.VaccinationHistory[0].DisplayLabel; got != "ET+TT W2" {
		t.Fatalf("history display label: %q", got)
	}
	if p.LastAccepted == nil || p.LastAccepted.CompletionID != "c1" {
		t.Fatalf("last accepted: %+v", p.LastAccepted)
	}
}

func TestGetPassportEmptyHasNoNextDueAndEmptyArrays(t *testing.T) {
	svc := NewService(fakeVacc{found: false}, fakeObl{}, nil)
	p, err := svc.GetPassport(context.Background(), "tenant", "goat")
	if err != nil {
		t.Fatalf("get passport: %v", err)
	}
	if p.NextDue != nil || p.LastAccepted != nil {
		t.Fatalf("expected no next-due / last-accepted, got %+v", p)
	}
	if p.OpenObligations == nil || p.VaccinationHistory == nil {
		t.Fatalf("empty sections must be non-nil arrays for a stable API shape")
	}
}

// VALUE-LEVEL TEST: Vaccination Passport rendered location strings (OL-7 audit).
// This test pins the exact output string for partitioned and unpartitioned sheds
// across the passport read path, ensuring no silent partition data loss.
func TestVaccinationPassportPartitionLabelRendersCorrectly(t *testing.T) {
	tests := []struct {
		name string
		loc  oploc.OperationalLocation
		want string
		// Invariants that MUST hold (string must not equal, and must not contain):
		mustNotEqual string
		mustNotContain string
	}{
		{
			name: "partitioned_numeric_label",
			loc:  oploc.OperationalLocation{ShedName: "Castro", PartitionLabel: "2"},
			want: "Castro - 2",
			mustNotEqual: "Castro 2", // no space
			mustNotContain: "whole",
		},
		{
			name: "partitioned_prefixed_label",
			loc:  oploc.OperationalLocation{ShedName: "Godel 1", PartitionLabel: "Part 3"},
			want: "Godel 1 - Part 3",
			mustNotEqual: "Godel 1 Part 3", // naive space join
			mustNotContain: "whole",
		},
		{
			name: "partitioned_large_number",
			loc:  oploc.OperationalLocation{ShedName: "Gandhi", PartitionLabel: "10"},
			want: "Gandhi - 10",
			mustNotEqual: "Gandhi 10", // naive space
			mustNotContain: "whole",
		},
		{
			name: "non_partitioned_bare_name",
			loc:  oploc.OperationalLocation{ShedName: "Yashoda", PartitionLabel: ""},
			want: "Yashoda",
			mustNotEqual: "Yashoda whole", // bare name never gets 'whole' suffix
			mustNotContain: "whole",
		},
		{
			name: "non_partitioned_whole_sentinel",
			loc:  oploc.OperationalLocation{ShedName: "Ho Chi Minh 1", PartitionLabel: "whole"},
			want: "Ho Chi Minh 1",
			mustNotEqual: "Ho Chi Minh 1 whole", // sentinel never rendered
			mustNotContain: "whole",
		},
		{
			name: "partitioned_digit_suffix_shed",
			loc:  oploc.OperationalLocation{ShedName: "Mandela 2", PartitionLabel: "Part 1"},
			want: "Mandela 2 - Part 1",
			mustNotEqual: "Mandela 2 Part 1", // the defect from OL-3: space join is ambiguous
			mustNotContain: "whole",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(fakeVacc{}, fakeObl{}, fakeLoc{found: true, loc: tt.loc})
			p, err := svc.GetPassport(context.Background(), "tenant", "goat")
			if err != nil {
				t.Fatalf("get passport: %v", err)
			}

			got := p.OperationalLocationDisplay
			if got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
			if got == tt.mustNotEqual {
				t.Errorf("output must not equal %q (this is a known defect pattern)", tt.mustNotEqual)
			}
			if tt.mustNotContain != "" && strings.Contains(got, tt.mustNotContain) {
				t.Errorf("output must not contain %q, but got %q", tt.mustNotContain, got)
			}
		})
	}
}

