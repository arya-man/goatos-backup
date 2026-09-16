package domain

import (
	"fmt"
	"strings"
	"testing"
)

func pen(shed, part string, head int64, kg float64) PenFeedDay {
	return PenFeedDay{ParkID: "p1", ParkLabel: "CPT", ShedID: shed, ShedName: "Mandela 1", PartitionLabel: part, HeadCount: head, QuantityKg: kg}
}

func TestPenFeedChangeExplainedByShiftingIsSilent(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 14, 28)},
		Movements:    []PenMovement{{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 4, Status: "applied"}},
	})
	if len(out) != 0 {
		t.Fatalf("a head-count move fully explained by the shifting register must raise nothing, got %+v", out)
	}
}

func TestPenFeedChangeWithNoMovementIsCritical(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 14, 28)},
	})
	if len(out) != 1 {
		t.Fatalf("want one alert, got %+v", out)
	}
	a := out[0]
	if a.Severity != SeverityCritical || a.RuleKey != RulePenFeedQuantityChange {
		t.Fatalf("unexplained move must be critical on the pen rule: %+v", a)
	}
	if a.OperationalLocationDisplay != "Mandela 1 - Part 1" {
		t.Fatalf("pen must render through oploc.Display, got %q", a.OperationalLocationDisplay)
	}
	if !strings.Contains(a.Title, "10 → 14") || !strings.Contains(a.Detail, "no shifting recorded") {
		t.Fatalf("copy must name the move and the missing shifting: %q / %q", a.Title, a.Detail)
	}
}

func TestPenFeedChangePartiallyExplainedIsWarning(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "", 16, 32)},
		Movements:    []PenMovement{{DestinationShedID: "s1", HeadCount: 4, Status: "pending"}},
	})
	if len(out) != 1 || out[0].Severity != SeverityWarning {
		t.Fatalf("partially explained move must be a warning, got %+v", out)
	}
	if !strings.Contains(out[0].Detail, "account for +4") || !strings.Contains(out[0].Detail, "awaiting approval") {
		t.Fatalf("detail must name what the register accounts for and that it is pending: %q", out[0].Detail)
	}
}

func TestPenFeedSameHeadCountKgChangeIsWarning(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "", 10, 23.5)},
	})
	if len(out) != 1 || out[0].Severity != SeverityWarning || !strings.HasSuffix(out[0].Key, ":kg") {
		t.Fatalf("kg change with the same animals must be one warning keyed :kg, got %+v", out)
	}
}

func TestPenFeedChangeBelowThresholdIsIgnored(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate:  "2026-09-16",
		Yesterday:     []PenFeedDay{pen("s1", "", 10, 20)},
		Today:         []PenFeedDay{pen("s1", "", 12, 24)},
		MinHeadChange: 3,
	})
	if len(out) != 0 {
		t.Fatalf("a move under the configured threshold must not fire, got %+v", out)
	}
}

func TestPenFeedChangeNeedsBothSheets(t *testing.T) {
	if out := DetectPenFeedChanges(PenFeedChangeInput{BusinessDate: "2026-09-16", Today: []PenFeedDay{pen("s1", "", 10, 20)}}); out != nil {
		t.Fatalf("no yesterday sheet means no baseline and no alerts, got %+v", out)
	}
}

func TestPenFeedNewlyBlockedCellsAreCritical(t *testing.T) {
	today := pen("s1", "", 10, 12)
	today.BlockedCells = 2
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{today},
	})
	// kg dropped with the same head count (warning) AND cells newly blocked (critical);
	// critical sorts first.
	if len(out) != 2 || out[0].Severity != SeverityCritical || !strings.HasSuffix(out[0].Key, ":blocked") {
		t.Fatalf("newly blocked cells must raise a critical row first, got %+v", out)
	}
}

func TestPenFeedDroppedPenCountsAsLeaving(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{},
		Movements:    []PenMovement{{SourceShedID: "s1", DestinationShedID: "s2", HeadCount: 10, Status: "applied"}},
	})
	if len(out) != 0 {
		t.Fatalf("a pen emptied by a recorded shifting is explained, got %+v", out)
	}
}

// TestPenFeedChangesOneToManyMovementsNetPerPen is the cardinality adversary: several movements
// touch ONE pen between the two sheets (two arrivals, one departure) and the detector must net
// them into one explained delta -- one row per pen, never one per movement.
func TestPenFeedChangesOneToManyMovementsNetPerPen(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 13, 26)},
		Movements: []PenMovement{
			{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 2, Status: "applied"},
			{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 3, Status: "applied"},
			{SourceShedID: "s1", SourcePartitionLabel: "Part 1", DestinationShedID: "s9", HeadCount: 2, Status: "applied"},
		},
	})
	if len(out) != 0 {
		t.Fatalf("+2 +3 -2 = +3 explains a 10 -> 13 move; got %+v", out)
	}
	// Same movements, but the sheet moved by +5: exactly ONE warning for the pen, naming +3.
	out = DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 15, 30)},
		Movements: []PenMovement{
			{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 2, Status: "applied"},
			{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 3, Status: "applied"},
			{SourceShedID: "s1", SourcePartitionLabel: "Part 1", DestinationShedID: "s9", HeadCount: 2, Status: "applied"},
		},
	})
	if len(out) != 1 || !strings.Contains(out[0].Detail, "account for +3") {
		t.Fatalf("three movements must fold into one row naming the net +3, got %+v", out)
	}
}

// TestPenFeedChangesWholeScopeNoPageBoundary is the pagination adversary: the detector is fed
// the WHOLE park-day (the frozen sheet is read in full, never a page), so 300 pens with an
// unexplained move yield 300 rows -- one per pen -- and the count never depends on any page size.
func TestPenFeedChangesWholeScopeNoPageBoundary(t *testing.T) {
	yesterday := make([]PenFeedDay, 0, 300)
	today := make([]PenFeedDay, 0, 300)
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("s%03d", i)
		yesterday = append(yesterday, pen(id, "", 10, 20))
		today = append(today, pen(id, "", 11, 22))
	}
	out := DetectPenFeedChanges(PenFeedChangeInput{BusinessDate: "2026-09-16", Yesterday: yesterday, Today: today})
	if len(out) != 300 {
		t.Fatalf("one row per pen over the whole scope, got %d", len(out))
	}
	seen := map[string]bool{}
	for _, a := range out {
		if seen[a.Key] {
			t.Fatalf("duplicate row key %s", a.Key)
		}
		seen[a.Key] = true
	}
}

// TestPenMovementStatusMatrix walks every shifting_events status the register can hold: the
// live ones (pending, authorized, applied) all count toward the explained delta, pending and
// authorized are flagged as still awaiting approval, and the SQL reader already excludes
// rejected/canceled -- so a caller that (wrongly) passed one through would over-explain, which
// the detector cannot tell apart; that exclusion is the reader's contract, pinned here by name.
func TestPenMovementStatusMatrix(t *testing.T) {
	for _, tc := range []struct {
		status  string
		pending bool
	}{{"pending", true}, {"authorized", true}, {"applied", false}, {"pending_verification", false}} {
		out := DetectPenFeedChanges(PenFeedChangeInput{
			BusinessDate: "2026-09-16",
			Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
			Today:        []PenFeedDay{pen("s1", "", 16, 32)},
			Movements:    []PenMovement{{DestinationShedID: "s1", HeadCount: 4, Status: tc.status}},
		})
		if len(out) != 1 || out[0].Severity != SeverityWarning {
			t.Fatalf("%s: a partially explained move is one warning, got %+v", tc.status, out)
		}
		if got := strings.Contains(out[0].Detail, "awaiting approval"); got != tc.pending {
			t.Fatalf("%s: awaiting-approval qualifier = %v want %v (%q)", tc.status, got, tc.pending, out[0].Detail)
		}
	}
}
