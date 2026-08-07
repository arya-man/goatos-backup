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

// TestReminderCadenceShedPartitionArrays pins the DEFECT 1 fix for reminder notifications:
// shed_labels and shed_partition_labels arrays must be parallel (same order, same length),
// so index [i] of one corresponds to index [i] of the other. The final notification label
// is composed from both arrays using composeDriveShedDisplay.
func TestReminderCadenceShedPartitionArrays(t *testing.T) {
	cases := []struct {
		name                string
		shedLabels          []string
		shedPartitionLabels []string
		expectedLabels      []string // after composition
	}{
		{
			name:                "single partition shed composes partition into label",
			shedLabels:          []string{"Mandela 2"},
			shedPartitionLabels: []string{"Part 3"},
			expectedLabels:      []string{"Mandela 2 - Part 3"},
		},
		{
			name:                "multi-partition shed stays bare",
			shedLabels:          []string{"Godel 1"},
			shedPartitionLabels: []string{""}, // NULL/empty means multi-partition or non-partitioned
			expectedLabels:      []string{"Godel 1"},
		},
		{
			name:                "two sheds with one partition each",
			shedLabels:          []string{"Castro", "Yashoda"},
			shedPartitionLabels: []string{"2", ""}, // Castro has partition 2, Yashoda has none
			expectedLabels:      []string{"Castro - 2", "Yashoda"},
		},
		{
			name:                "numeric partition convention",
			shedLabels:          []string{"Ho Chi Minh"},
			shedPartitionLabels: []string{"1"},
			expectedLabels:      []string{"Ho Chi Minh - 1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.shedLabels) != len(tc.shedPartitionLabels) {
				t.Fatalf("test setup: shedLabels and shedPartitionLabels must have same length")
			}
			var got []string
			for i, shedLabel := range tc.shedLabels {
				// Simulate the enrichReminderCadenceFiresWithDetails logic:
				// compose each (shedLabel, partitionLabel) pair.
				var partitionLabel *string
				if i < len(tc.shedPartitionLabels) && tc.shedPartitionLabels[i] != "" {
					p := tc.shedPartitionLabels[i]
					partitionLabel = &p
				}
				finalLabel := composeDriveShedDisplay(shedLabel, partitionLabel)
				got = append(got, finalLabel)
			}

			if len(got) != len(tc.expectedLabels) {
				t.Fatalf("expected %d labels, got %d", len(tc.expectedLabels), len(got))
			}
			for i, label := range got {
				if label != tc.expectedLabels[i] {
					t.Errorf("label[%d]: got %q, want %q", i, label, tc.expectedLabels[i])
				}
			}
		})
	}
}
