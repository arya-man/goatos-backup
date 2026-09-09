package domain

import (
	"math"
	"strings"
	"testing"
)

// The verifier's confirm guard (2026-09-09) hangs off this one predicate, so its boundary is pinned
// exactly: strictly more than 500 g on EITHER side, and never on a number that cannot be read.
func TestPackingEntryVarianceIsSymmetricStrictAndDirectional(t *testing.T) {
	for _, tc := range []struct {
		name      string
		entered   float64
		planned   float64
		direction string
		exceeds   bool
	}{
		{"exact match is quiet", 2, 2, "", false},
		{"just inside, over", 2.49, 2, "", false},
		{"just inside, under", 1.51, 2, "", false},
		{"exactly the tolerance, over", 2.5, 2, "", false},
		{"exactly the tolerance, under", 1.5, 2, "", false},
		// The decimal arithmetic a verifier actually types: 2.7 - 2.2 is 0.5000000000000004 in
		// float64 and must still read as exactly the tolerance.
		{"exactly the tolerance in binary noise", 2.7, 2.2, "", false},
		{"a gram past it, over", 2.501, 2, PackingEntryAbovePlan, true},
		{"a gram past it, under", 1.499, 2, PackingEntryBelowPlan, true},
		{"far over", 12, 2, PackingEntryAbovePlan, true},
		{"far under", 0, 2, PackingEntryBelowPlan, true},
		{"zero plan, real reading", 1, 0, PackingEntryAbovePlan, true},
		{"unreadable entry", math.NaN(), 2, "", false},
		{"unreadable plan", 2, math.Inf(1), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			direction, exceeds := PackingEntryVariance(tc.entered, tc.planned)
			if direction != tc.direction || exceeds != tc.exceeds {
				t.Fatalf("PackingEntryVariance(%v, %v) = (%q, %v), want (%q, %v)", tc.entered, tc.planned, direction, exceeds, tc.direction, tc.exceeds)
			}
		})
	}
}

// The confirm tolerance is deliberately NOT the leadership variance tolerance; merging them changes
// what both screens mean. Pinned so a later "tidy-up" that points one at the other goes red.
func TestPackingEntryConfirmToleranceIsItsOwnConstant(t *testing.T) {
	if PackingEntryConfirmToleranceKg != 0.5 {
		t.Fatalf("confirm tolerance = %v, want 0.5 kg (500 g)", PackingEntryConfirmToleranceKg)
	}
	if PackingEntryConfirmToleranceKg == PackingVarianceToleranceKg {
		t.Fatal("the verifier's confirm tolerance must not be the leadership variance tolerance")
	}
}

// Blind entry survives the warning: the copy names a direction and the 500 g line, never a figure
// from the plan and never the gap.
func TestPackingEntryVarianceMessagesRevealDirectionOnly(t *testing.T) {
	above := PackingEntryVarianceMessage(PackingEntryAbovePlan)
	below := PackingEntryVarianceMessage(PackingEntryBelowPlan)
	if !strings.Contains(above, "above") || !strings.Contains(below, "below") {
		t.Fatalf("messages must name the direction: above=%q below=%q", above, below)
	}
	for _, msg := range []string{above, below, PackingEntryConfirmMessage} {
		if !strings.Contains(msg, "500 g") {
			t.Errorf("message must state the 500 g line: %q", msg)
		}
		if strings.Contains(msg, "%") || strings.Contains(msg, "kg") {
			t.Errorf("message must carry no placeholder or figure: %q", msg)
		}
	}
	if PackingEntryVarianceMessage("sideways") != "" {
		t.Fatal("an unknown direction renders nothing rather than inventing a sentence")
	}
}
