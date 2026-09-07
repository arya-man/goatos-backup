package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The rule is parameterised by the CONFIGURED cutoff, so the same instant must
// answer differently under different farms' evenings. Every anchor is a fixed
// IST wall-clock instant — never time.Now, never hour arithmetic on the
// running clock.
func TestEarliestPlannableDateFollowsTheConfiguredCutoff(t *testing.T) {
	ist := biztime.DefaultLocation()
	day := time.Date(2026, 9, 3, 0, 0, 0, 0, ist)
	eight := MustCutoff(20, 0)
	nine := MustCutoff(21, 0)
	half := MustCutoff(19, 30)

	cases := []struct {
		name   string
		now    time.Time
		cutoff Cutoff
		want   string
	}{
		{"morning offers tomorrow", day.Add(10 * time.Hour), eight, "2026-09-04"},
		{"19:59 under a 20:00 cutoff still offers tomorrow", day.Add(19*time.Hour + 59*time.Minute), eight, "2026-09-04"},
		{"20:00 exactly under a 20:00 cutoff flips", day.Add(20 * time.Hour), eight, "2026-09-05"},
		{"20:14 under a 21:00 cutoff still offers tomorrow", day.Add(20*time.Hour + 14*time.Minute), nine, "2026-09-04"},
		{"21:00 under a 21:00 cutoff flips", day.Add(21 * time.Hour), nine, "2026-09-05"},
		{"19:30 under a 19:30 cutoff flips on the minute", day.Add(19*time.Hour + 30*time.Minute), half, "2026-09-05"},
		{"19:29 under a 19:30 cutoff does not", day.Add(19*time.Hour + 29*time.Minute), half, "2026-09-04"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EarliestPlannableBusinessDate(tc.now, tc.cutoff); got != tc.want {
				t.Fatalf("EarliestPlannableBusinessDate(%s, %s) = %s, want %s", tc.now, tc.cutoff, got, tc.want)
			}
		})
	}

	// The boundary is the IST wall clock, not the server's zone: 20:30 IST
	// expressed as 15:00 UTC must flip under a 20:00 cutoff.
	utcEvening := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	if got := EarliestPlannableBusinessDate(utcEvening, eight); got != "2026-09-05" {
		t.Fatalf("UTC-expressed 20:30 IST = %s, want 2026-09-05", got)
	}
	if DateAllowsPlanning("2026-09-04", day.Add(20*time.Hour), eight) {
		t.Fatal("tomorrow must be refused at the cutoff")
	}
	if !DateAllowsPlanning("2026-09-04", day.Add(20*time.Hour), nine) {
		t.Fatal("tomorrow must stay allowed while the configured cutoff is still ahead")
	}
}

func TestVisibleFromIsTheCutoffOnTheEveningBefore(t *testing.T) {
	ist := biztime.DefaultLocation()
	visible, err := VisibleFrom("2026-09-04", MustCutoff(19, 45))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 3, 19, 45, 0, 0, ist); !visible.Equal(want) {
		t.Fatalf("visible from %s, want %s", visible, want)
	}
	if _, err := VisibleFrom("not-a-date", MustCutoff(20, 0)); err == nil {
		t.Fatal("a malformed business date must error, not resolve")
	}
}

// The zero value is deliberately unusable: an unset cutoff must never read as
// midnight, and the stored text forms must round-trip minute for minute.
func TestCutoffParsingAndValidity(t *testing.T) {
	if (Cutoff{}).Valid() {
		t.Fatal("zero-value cutoff must be invalid")
	}
	for raw, want := range map[string]string{"20:00": "20:00", "19:30:00": "19:30", "00:00": "00:00", "23:59:59": "23:59"} {
		c, err := ParseCutoff(raw)
		if err != nil {
			t.Fatalf("ParseCutoff(%q): %v", raw, err)
		}
		if !c.Valid() || c.String() != want {
			t.Fatalf("ParseCutoff(%q) = %s valid=%v, want %s", raw, c, c.Valid(), want)
		}
	}
	for _, bad := range []string{"", "8pm", "24:00", "20:60", "-1:00", "2000"} {
		if _, err := ParseCutoff(bad); !errors.Is(err, ErrInvalidCutoff) {
			t.Fatalf("ParseCutoff(%q) err = %v, want ErrInvalidCutoff", bad, err)
		}
	}
	if got := MustCutoff(20, 0).SQLTime(); got != "20:00:00" {
		t.Fatalf("SQLTime = %q", got)
	}
	if _, err := NewCutoff(24, 0); !errors.Is(err, ErrInvalidCutoff) {
		t.Fatal("hour 24 must be refused")
	}
}
