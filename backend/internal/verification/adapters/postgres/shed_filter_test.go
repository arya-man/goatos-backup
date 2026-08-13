package postgres

import "testing"

// The filter OPTIONS this repository historically emitted were oploc.Key() values
// ("<uuid>#<normalized partition>"). Live verification is exact-shed grain now:
// stale suffixes are stripped so they cannot hide exact-shed rows.
func TestSplitShedFilter(t *testing.T) {
	const shed = "6f7e1d2c-0000-4000-8000-000000000001"
	tests := []struct {
		name          string
		raw           string
		wantShed      string
		wantPartition string
	}{
		{
			name: "empty filter selects everything",
			raw:  "", wantShed: "", wantPartition: "",
		},
		{
			// Backward compatibility: an older client that still sends a plain shed
			// uuid must keep working and match EVERY partition of that shed.
			name: "bare uuid keeps working and does not constrain partition",
			raw:  shed, wantShed: shed, wantPartition: "",
		},
		{
			name: "worded partition is ignored",
			raw:  shed + "#Part 3", wantShed: shed, wantPartition: "",
		},
		{
			name: "numeric partition is ignored",
			raw:  shed + "#2", wantShed: shed, wantPartition: "",
		},
		{
			name: "whole sentinel is ignored",
			raw:  shed + "#whole", wantShed: shed, wantPartition: "",
		},
		{
			name: "case and padding are ignored with the suffix",
			raw:  shed + "#  PART  3 ", wantShed: shed, wantPartition: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShed, gotPartition := splitShedFilter(tt.raw)
			if gotShed != tt.wantShed {
				t.Fatalf("shed = %q, want %q", gotShed, tt.wantShed)
			}
			if gotPartition != tt.wantPartition {
				t.Fatalf("partition = %q, want %q", gotPartition, tt.wantPartition)
			}
		})
	}
}

// REGRESSION: the shed half must always be castable to uuid. This is the exact
// property whose absence broke the leadership Videos shed filter.
func TestSplitShedFilterNeverReturnsCompositeAsShedID(t *testing.T) {
	const shed = "6f7e1d2c-0000-4000-8000-000000000001"
	for _, raw := range []string{shed + "#Part 3", shed + "#2", shed + "#whole"} {
		gotShed, _ := splitShedFilter(raw)
		if gotShed != shed {
			t.Fatalf("splitShedFilter(%q) shed = %q, want a bare uuid %q", raw, gotShed, shed)
		}
	}
}
