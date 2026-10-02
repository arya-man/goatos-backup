package domain

import "testing"

func TestDisplaySubjectLabelRendersLegacyISODateSegments(t *testing.T) {
	cases := map[string]string{
		"6th Colostrum · Kid G-005346 · 2026-09-16 · Ravi Kumbar": "6th Colostrum · Kid G-005346 · 16/09/2026 · Ravi Kumbar",
		"6th Colostrum · Kid G-005346 · 2026-09-16":               "6th Colostrum · Kid G-005346 · 16/09/2026",
		"2026-09-16 · Castro 1":                                   "16/09/2026 · Castro 1",
		"Feed · 16/09/2026":                                       "Feed · 16/09/2026",
		"Batch B-2026-09-16X · Castro 1":                          "Batch B-2026-09-16X · Castro 1",
		"":                                                        "",
	}
	for in, want := range cases {
		if got := DisplaySubjectLabel(in); got != want {
			t.Errorf("DisplaySubjectLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
