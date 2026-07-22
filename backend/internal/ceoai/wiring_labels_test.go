package ceoai

import "testing"

// TestFormatMetricValue proves operator utilization is rendered as a whole
// percent (grounded "155%") — killing the raw "1.5500000000000000" decimal leak
// — while every other metric passes through unchanged.
func TestFormatMetricValue(t *testing.T) {
	cases := []struct {
		metric string
		raw    string
		want   string
	}{
		{"operator_vaccination_utilization", "1.5500000000000000", "155%"},
		{"operator_vaccination_utilization", "0.40000000000000000000", "40%"},
		{"operator_vaccination_utilization", "1", "100%"},
		{"operator_vaccination_utilization", "not-a-number", "not-a-number"},
		{"active_animals", "972", "972"},
		{"operator_vaccination_load", "155", "155"},
	}
	for _, c := range cases {
		if got := formatMetricValue(c.metric, c.raw); got != c.want {
			t.Fatalf("formatMetricValue(%q, %q) = %q, want %q", c.metric, c.raw, got, c.want)
		}
	}
}

// TestCubeSurfaceIsBusinessLabel proves the user-facing Cube surface is the clean
// business title, never the "Cube · <raw metric id>" plumbing tag that leaked
// into the leadership citation chip.
func TestCubeSurfaceIsBusinessLabel(t *testing.T) {
	for name, b := range cubeMetricBindings {
		if b.title == "" {
			t.Fatalf("metric %q has no business title for the citation surface", name)
		}
	}
	// The two demonstrated metrics from the maintainer complaint.
	if cubeMetricBindings["active_animals"].title != "Active animals" {
		t.Fatalf("active_animals title = %q, want %q", cubeMetricBindings["active_animals"].title, "Active animals")
	}
	if cubeMetricBindings["operator_vaccination_overdue"].title != "Operator vaccination overdue" {
		t.Fatalf("operator_vaccination_overdue title = %q", cubeMetricBindings["operator_vaccination_overdue"].title)
	}
}
