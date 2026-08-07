package main

import "testing"

// summarise must match the live writer's collapse rule exactly (sopbridge vaccinationShedSummary /
// vaccinationVaccineSummary). The cases below are the ones the first version got wrong: a
// partitioned shed must keep its partition verbatim, and a MIXED-vaccine submission must name every
// distinct vaccine rather than whichever row the database happened to return first.
func TestSummariseMatchesLiveWriterCollapseRule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     []string
		plural string
		want   string
	}{
		{"partition is preserved verbatim", []string{"Sumathi 1 - Part 3"}, "sheds", "Sumathi 1 - Part 3"},
		{"duplicates collapse to one", []string{"Sumathi 1 - Part 3", "Sumathi 1 - Part 3"}, "sheds", "Sumathi 1 - Part 3"},
		// The mixed-vaccine case: LIMIT 1 would have produced just one of these.
		{"two vaccines are both named, sorted", []string{"PPR", "ET+TT"}, "vaccines", "ET+TT + PPR"},
		{"order does not change the label", []string{"ET+TT", "PPR"}, "vaccines", "ET+TT + PPR"},
		{"past two collapses to a count", []string{"PPR", "ET+TT", "Blue Tongue", "Goat Pox"}, "vaccines", "4 vaccines"},
		{"three sheds collapse to a count", []string{"Godel 1", "Godel 2", "Mandela 2"}, "sheds", "3 sheds"},
		// An id-shaped fragment: the shed name did not resolve and only the suffix survived.
		{"bare partition suffix is dropped", []string{" - Part 3"}, "sheds", ""},
		{"blank input yields nothing", []string{"", "   "}, "sheds", ""},
		{"real value survives alongside a dropped fragment", []string{" - Part 3", "Yashoda"}, "sheds", "Yashoda"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarise(tc.in, tc.plural); got != tc.want {
				t.Fatalf("summarise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
