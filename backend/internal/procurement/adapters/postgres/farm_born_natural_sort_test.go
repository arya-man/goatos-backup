package postgres

import (
	"sort"
	"testing"
)

// TestNaturalLessOrdersPenNumbersByValue pins the pen option order: Yashoda 2 before Yashoda 10,
// which a byte-wise sort gets wrong, and worded partitions in their numeric order.
func TestNaturalLessOrdersPenNumbersByValue(t *testing.T) {
	in := []string{"Yashoda 10", "Yashoda 2", "Yashoda 1", "Godel 1 - Part 8", "Godel 1 - Part 10", "Gandhi 3", "Castro"}
	sort.Slice(in, func(i, j int) bool { return naturalLess(in[i], in[j]) })
	want := []string{"Castro", "Gandhi 3", "Godel 1 - Part 8", "Godel 1 - Part 10", "Yashoda 1", "Yashoda 2", "Yashoda 10"}
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("order = %v, want %v", in, want)
		}
	}
}
