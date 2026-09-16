package domain

import (
	"fmt"
	"strings"
	"testing"
)

func pen(shed, part string, head int64, kg float64) PenFeedDay {
	return PenFeedDay{ParkID: "p1", ParkLabel: "CPT", ShedID: shed, ShedName: "Mandela 1", PartitionLabel: part, HeadCount: head, QuantityKg: kg}
}

// Count and feed moved together: the sheet followed the animals. Silent whether or not a
// shifting was recorded -- the register is secondary (maintainer clarification 2026-09-16).
func TestPenFeedCountAndFeedMovingTogetherIsSilent(t *testing.T) {
	for _, moves := range [][]PenMovement{nil, {{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 4, Status: "applied"}}} {
		out := DetectPenFeedChanges(PenFeedChangeInput{
			BusinessDate: "2026-09-16",
			Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
			Today:        []PenFeedDay{pen("s1", "Part 1", 14, 28)},
			Movements:    moves,
		})
		if len(out) != 0 {
			t.Fatalf("count and feed moved together must be silent (movements=%v), got %+v", moves, out)
		}
	}
}

func TestPenFeedCountMovedButFeedUnchangedIsCritical(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 14, 20)},
	})
	if len(out) != 1 {
		t.Fatalf("want one alert, got %+v", out)
	}
	a := out[0]
	if a.Severity != SeverityCritical || a.RuleKey != RulePenFeedQuantityChange || !strings.HasSuffix(a.Key, ":head") {
		t.Fatalf("count moved with feed unchanged must be the critical :head row: %+v", a)
	}
	if a.OperationalLocationDisplay != "Mandela 1 - Part 1" || a.PartitionLabel != "Part 1" {
		t.Fatalf("pen must render through oploc.Display: %q / %q", a.OperationalLocationDisplay, a.PartitionLabel)
	}
	if !strings.Contains(a.Title, "10 → 14") || !strings.Contains(a.Title, "unchanged at 20.0 kg") || !strings.Contains(a.Detail, "No shifting is recorded") {
		t.Fatalf("copy must name the move, the frozen feed and the register note: %q / %q", a.Title, a.Detail)
	}
	// With a shifting on the register the row still fires (the trigger is the feed, not the
	// register) and the note names the recorded movement instead.
	out = DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 14, 20)},
		Movements:    []PenMovement{{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 4, Status: "applied"}},
	})
	if len(out) != 1 || out[0].Severity != SeverityCritical || !strings.Contains(out[0].Detail, "register shows +4") {
		t.Fatalf("a recorded shifting must not silence an unfed count move; got %+v", out)
	}
}

func TestPenFeedSameHeadCountKgChangeIsWarning(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "", 10, 23.5)},
	})
	if len(out) != 1 || out[0].Severity != SeverityWarning || !strings.HasSuffix(out[0].Key, ":kg") || out[0].PartitionLabel != "" {
		t.Fatalf("kg change with the same animals must be one warning keyed :kg with a blank partition, got %+v", out)
	}
}

func TestPenFeedChangeBelowThresholdIsIgnored(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate:  "2026-09-16",
		Yesterday:     []PenFeedDay{pen("s1", "", 10, 20)},
		Today:         []PenFeedDay{pen("s1", "", 12, 20)},
		MinHeadChange: 3,
	})
	if len(out) != 0 {
		t.Fatalf("a count move under the configured threshold must not fire, got %+v", out)
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

func TestPenFeedPenAppearingOrDroppingIsSilent(t *testing.T) {
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
		Today:        []PenFeedDay{pen("s2", "", 10, 20)},
	})
	if len(out) != 0 {
		t.Fatalf("a pen leaving or joining the sheet moved count and feed together (to/from zero), got %+v", out)
	}
}

// TestPenFeedChangesOneToManyMovementsNetPerPen is the cardinality adversary: several movements
// touch ONE pen between the two sheets and the note must net them into one figure on ONE row
// -- one row per pen, never one per movement, and the register never changes the verdict.
func TestPenFeedChangesOneToManyMovementsNetPerPen(t *testing.T) {
	moves := []PenMovement{
		{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 2, Status: "applied"},
		{DestinationShedID: "s1", DestinationPartitionLabel: "Part 1", HeadCount: 3, Status: "applied"},
		{SourceShedID: "s1", SourcePartitionLabel: "Part 1", DestinationShedID: "s9", HeadCount: 2, Status: "applied"},
	}
	out := DetectPenFeedChanges(PenFeedChangeInput{
		BusinessDate: "2026-09-16",
		Yesterday:    []PenFeedDay{pen("s1", "Part 1", 10, 20)},
		Today:        []PenFeedDay{pen("s1", "Part 1", 13, 20)},
		Movements:    moves,
	})
	if len(out) != 1 || !strings.Contains(out[0].Detail, "register shows +3") {
		t.Fatalf("three movements must fold into one row noting the net +3, got %+v", out)
	}
}

// TestPenFeedChangesWholeScopeNoPageBoundary is the pagination adversary: the detector is fed
// the WHOLE park-day (the frozen sheet is read in full, never a page), so 300 pens with an
// unfed count move yield 300 rows -- one per pen -- and the count never depends on a page size.
func TestPenFeedChangesWholeScopeNoPageBoundary(t *testing.T) {
	yesterday := make([]PenFeedDay, 0, 300)
	today := make([]PenFeedDay, 0, 300)
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("s%03d", i)
		yesterday = append(yesterday, pen(id, "", 10, 20))
		today = append(today, pen(id, "", 11, 20))
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

// TestPenMovementStatusMatrix walks every shifting_events status the reader can hand over: none
// of them changes the verdict (feed unchanged against a count move is critical regardless), and
// the note reports the register's net either way. rejected/canceled never reach the detector --
// the reader excludes them -- which is pinned here by name.
func TestPenMovementStatusMatrix(t *testing.T) {
	for _, status := range []string{"pending", "authorized", "applied", "pending_verification"} {
		out := DetectPenFeedChanges(PenFeedChangeInput{
			BusinessDate: "2026-09-16",
			Yesterday:    []PenFeedDay{pen("s1", "", 10, 20)},
			Today:        []PenFeedDay{pen("s1", "", 16, 20)},
			Movements:    []PenMovement{{DestinationShedID: "s1", HeadCount: 6, Status: status}},
		})
		if len(out) != 1 || out[0].Severity != SeverityCritical || !strings.Contains(out[0].Detail, "register shows +6") {
			t.Fatalf("%s: unfed count move is critical with the register note, got %+v", status, out)
		}
	}
}
