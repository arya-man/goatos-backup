package postgres

import "testing"

// The panel lists sheds for a reader scanning for ONE pen, so the order has to be the one they
// would count in. Two orderings that look right and are not, both observed on the real register:
// plain text puts "Yashoda 10" between "Yashoda 1" and "Yashoda 2", and the SQL's own ordering
// (by the location row's name) puts an alias-spelled "Mandela 1" + "Part 7" ahead of the location
// literally named "Mandela 1 - Part 2" -- which is exactly how the panel read "Part 7, Part 2,
// Part 3, Part 4, Part 9".
func TestNaturalLessOrdersShedNamesTheWayAPersonCountsThem(t *testing.T) {
	sorted := []string{
		"Castro 1", "Castro 2", "Castro 3",
		"Godel 1 - Part 7", "Godel 1 - Part 8", "Godel 2 - Part 1",
		"Mandela 1 - Part 2", "Mandela 1 - Part 7", "Mandela 1 - Part 9", "Mandela 1 - Part 10",
		"Mandela 2 - Part 5",
		"Yashoda 1", "Yashoda 2", "Yashoda 9", "Yashoda 10",
	}
	for i := 0; i+1 < len(sorted); i++ {
		lo, hi := sorted[i], sorted[i+1]
		if !naturalLess(lo, hi) {
			t.Fatalf("%q must sort before %q", lo, hi)
		}
		if naturalLess(hi, lo) {
			t.Fatalf("%q must NOT sort before %q", hi, lo)
		}
	}
	// Equal names are neither less than the other, or a stable sort would still swap them.
	if naturalLess("Castro 1", "Castro 1") {
		t.Fatal("an identical name must not compare less than itself")
	}
	// A digit run with leading zeros is still that number, not a longer string.
	if !naturalLess("Part 007", "Part 8") {
		t.Fatal("leading zeros must not make 007 sort after 8")
	}
	// A prefix sorts before the longer name that contains it.
	if !naturalLess("Yashoda", "Yashoda 1") {
		t.Fatal("a bare shed name must sort before its numbered pens")
	}
	// Case is not identity here.
	if naturalLess("yashoda 2", "Yashoda 1") {
		t.Fatal("comparison must be case-insensitive on the text runs")
	}
}
