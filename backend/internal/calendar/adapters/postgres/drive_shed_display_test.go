package postgres

import "testing"

// TestComposeDriveShedDisplay pins the DEFECT 1 composition rule: the calendar drive-shed
// summary must render "Shed - Partition" when SQL resolved a single shared partition across every
// animal counted in that shed, and must fall back to the bare shed name otherwise -- covering the
// "shed spans more than one partition, do not invent one" case explicitly.
func TestComposeDriveShedDisplay(t *testing.T) {
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
			name:           "multi-partition shed stays bare (nil partition, never invented)",
			shedName:       "Godel 1",
			partitionLabel: nil,
			want:           "Godel 1",
		},
		{
			name:           "numeric partition convention composes with dash",
			shedName:       "Castro",
			partitionLabel: partition("2"),
			want:           "Castro - 2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composeDriveShedDisplay(tc.shedName, tc.partitionLabel)
			if got != tc.want {
				t.Fatalf("composeDriveShedDisplay(%q, %v) = %q, want %q", tc.shedName, tc.partitionLabel, got, tc.want)
			}
		})
	}
}
