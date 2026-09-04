package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The create cutoff is a WALL-CLOCK boundary at 20:00 IST, business-day
// grained. Every anchor here is derived from one fixed IST date — never
// time.Now, never hour arithmetic against the running clock.
func TestEarliestPlannableWeighDateFlipsAtEightPMIST(t *testing.T) {
	ist := biztime.DefaultLocation()
	day := time.Date(2026, 9, 3, 0, 0, 0, 0, ist)

	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"morning offers tomorrow", day.Add(10 * time.Hour), "2026-09-04"},
		{"19:59 still offers tomorrow", day.Add(19*time.Hour + 59*time.Minute), "2026-09-04"},
		{"20:00 exactly flips to day after", day.Add(20 * time.Hour), "2026-09-05"},
		{"23:30 offers day after", day.Add(23*time.Hour + 30*time.Minute), "2026-09-05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EarliestPlannableWeighDate(tc.now); got != tc.want {
				t.Fatalf("EarliestPlannableWeighDate(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}

	// The boundary is IST wall clock, not the server's zone: 20:30 IST
	// expressed as 15:00 UTC must still flip to the day after.
	utcEvening := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	if got := EarliestPlannableWeighDate(utcEvening); got != "2026-09-05" {
		t.Fatalf("UTC-expressed 20:30 IST = %s, want 2026-09-05", got)
	}

	if WeighDateAllowsFastingCreate("2026-09-04", day.Add(20*time.Hour)) {
		t.Fatal("tomorrow must be refused at/after 20:00 IST")
	}
	if !WeighDateAllowsFastingCreate("2026-09-05", day.Add(20*time.Hour)) {
		t.Fatal("day after tomorrow must stay allowed at 20:00 IST")
	}
}

func TestFastingWindowClocksAreBusinessDayAnchored(t *testing.T) {
	ist := biztime.DefaultLocation()
	visible, err := FastingCardVisibleFrom("2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 3, 20, 0, 0, 0, ist); !visible.Equal(want) {
		t.Fatalf("visible from %s, want %s", visible, want)
	}
	deadline, err := FastingDeadline("2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 4, 0, 0, 0, 0, ist); !deadline.Equal(want) {
		t.Fatalf("deadline %s, want %s", deadline, want)
	}
	if got := RemovalBusinessDate("2026-09-04"); got != "2026-09-03" {
		t.Fatalf("removal date %s, want 2026-09-03", got)
	}
}

// The card/verifier label is backend-owned farm copy: never an id, never a
// dangling separator, and it counts sheds honestly.
func TestFastingSubjectLabelDegradesWithoutInventing(t *testing.T) {
	if got := FastingSubjectLabel("Coimbatore", 4); got != "Remove feed & water · Coimbatore · 4 sheds" {
		t.Fatalf("label = %q", got)
	}
	if got := FastingSubjectLabel("Coimbatore", 1); got != "Remove feed & water · Coimbatore · 1 shed" {
		t.Fatalf("label = %q", got)
	}
	if got := FastingSubjectLabel("", 0); got != "Remove feed & water" {
		t.Fatalf("blank park/sheds label = %q, must degrade to the bare work name", got)
	}
}
