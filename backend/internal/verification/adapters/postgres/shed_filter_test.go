package postgres

import "testing"

// The filter OPTIONS this repository emits are oploc.Key() values
// ("<uuid>#<normalized partition>"). The queue query casts the shed filter to
// ::uuid. Feeding the composite key straight into that cast made Postgres reject
// the whole request with `invalid input syntax for type uuid`, which the client
// surfaced as stale cached rows and no error at all.
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
			name: "worded partition normalizes to its bare form",
			raw:  shed + "#Part 3", wantShed: shed, wantPartition: "3",
		},
		{
			name: "numeric partition passes through",
			raw:  shed + "#2", wantShed: shed, wantPartition: "2",
		},
		{
			// oploc.Key() emits the "whole" sentinel for an unpartitioned shed; it must
			// survive the round trip or the option would match nothing.
			name: "whole sentinel round-trips",
			raw:  shed + "#whole", wantShed: shed, wantPartition: "whole",
		},
		{
			name: "case and padding are normalized, matching oploc.NormalizePartition",
			raw:  shed + "#  PART  3 ", wantShed: shed, wantPartition: "3",
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
