package postgres

import "testing"

// TestComposeShedCompletionDisplay pins the DEFECT 2 composition rule: the vaccination submit
// header must render "Shed - Partition" only when SQL resolved a single shared real partition
// across every goat scoped into the shed completion summary, and must fall back to the bare shed
// name (including the 'Multiple sheds' sentinel, unaffected because it never receives a
// partition) whenever that membership spans more than one partition.
func TestComposeShedCompletionDisplay(t *testing.T) {
	partition := func(s string) *string { return &s }

	cases := []struct {
		name           string
		shedName       string
		partitionLabel *string
		want           string
	}{
		{
			name:           "no partition resolved renders bare shed name",
			shedName:       "Yashoda",
			partitionLabel: nil,
			want:           "Yashoda",
		},
		{
			name:           "single shared partition composes shed and partition",
			shedName:       "Mandela 2",
			partitionLabel: partition("Part 3"),
			want:           "Mandela 2 - Part 3",
		},
		{
			name:           "multi-partition membership stays bare (nil partition, never invented)",
			shedName:       "Godel 1",
			partitionLabel: nil,
			want:           "Godel 1",
		},
		{
			name:           "'Multiple sheds' sentinel never carries a partition",
			shedName:       "Multiple sheds",
			partitionLabel: nil,
			want:           "Multiple sheds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composeShedCompletionDisplay(tc.shedName, tc.partitionLabel)
			if got != tc.want {
				t.Fatalf("composeShedCompletionDisplay(%q, %v) = %q, want %q", tc.shedName, tc.partitionLabel, got, tc.want)
			}
		})
	}
}
