package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
)

// TestPartitionOverCapacityIsFlaggedNeverRefused pins the 2026-10-02 rule: capacity is a guide, so a
// partition holding more animals than it is only flagged (shown in red); at or under capacity, or
// with no capacity set, nothing is flagged.
func TestPartitionOverCapacityIsFlaggedNeverRefused(t *testing.T) {
	cases := []struct {
		name     string
		capacity any
		animals  int
		want     string
	}{
		{"over", float64(10), 32, "32 animals for a capacity of 10: 22 over."},
		{"at", float64(10), 10, ""},
		{"under", float64(10), 3, ""},
		{"no capacity set", nil, 50, ""},
		{"zero capacity with animals", float64(0), 1, "1 animal for a capacity of 0: 1 over."},
	}
	for _, tc := range cases {
		row := domain.Row{Fields: map[string]any{"capacity": tc.capacity}, Counts: map[string]int{"animals": tc.animals}}
		flagOverCapacity(&row)
		if row.Warnings["capacity"] != tc.want || row.Warnings["animals"] != tc.want {
			t.Errorf("%s: warnings = %v, want %q on capacity and animals", tc.name, row.Warnings, tc.want)
		}
		if tc.want == "" && row.Warnings != nil {
			t.Errorf("%s: a row within capacity must carry no warnings, got %v", tc.name, row.Warnings)
		}
	}
}
