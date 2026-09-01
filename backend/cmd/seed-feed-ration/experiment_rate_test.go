package main

import "testing"

// The workbook loader and migration 000238 must derive IDENTICAL rates from identical inputs: a
// database stood up by migrating an existing one and a database stood up by seeding the workbook
// have to agree about what the farm feeds, or the two ways of building an environment quietly
// disagree. The denominator itself is the caller's (live population where the herd register can
// answer, the workbook's own count where it cannot); this is the arithmetic they share.
//
// TRUNCATION IS THE PROPERTY UNDER TEST, not a formatting detail. The generator rounds a pen's
// session quantity UP to a packable 0.1 kg, so a rate a hair ABOVE exact lifts an unchanged pen's
// sheet by a whole notch (8 kg / 31 animals: 258.065 -> 4.1 kg per session where the pen has always
// been fed 4.0).
func TestWorkbookGramsPerHeadTruncatesToTheStoredScale(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kg    float64
		count float64
		want  string
	}{
		{name: "repeating decimal truncates rather than rounding up", kg: 8, count: 31, want: "258.064"},
		{name: "second repeating case", kg: 16, count: 63, want: "253.968"},
		{name: "exact division keeps its exact rate", kg: 18, count: 18, want: "1000.000"},
		{name: "an authored zero is a real instruction and stays zero", kg: 0, count: 31, want: "0.000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workbookGramsPerHead(tc.kg, tc.count)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("workbookGramsPerHead(%v, %v) = %q, want %q", tc.kg, tc.count, got, tc.want)
			}
		})
	}
}

// A pen the workbook feeds but does not count has NO denominator, so it has no rate. Failing loudly
// is the point: skipping the cell would drop the pen off the experiment workflow (membership IS the
// flag) and a 0 g rate would feed it nothing on a sheet that looks complete.
func TestWorkbookGramsPerHeadRefusesACountlessPen(t *testing.T) {
	for _, count := range []float64{0, -3} {
		if _, err := workbookGramsPerHead(12, count); err == nil {
			t.Fatalf("head count %v produced a rate; it must fail closed", count)
		}
	}
}
