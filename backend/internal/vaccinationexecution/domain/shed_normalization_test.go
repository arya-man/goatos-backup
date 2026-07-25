package domain

import "testing"

func TestNormalizeDriveShedParsesSourcePartitionLabels(t *testing.T) {
	tests := []struct {
		raw       string
		physical  string
		partition string
	}{
		{raw: "Gandhi 1", physical: "Gandhi", partition: "1"},
		{raw: "Gandhi - Part 1", physical: "Gandhi", partition: "Part 1"},
		{raw: "Godel 1 - Part 3", physical: "Godel 1", partition: "Part 3"},
		{raw: "Old Yashoda", physical: "Old Yashoda", partition: "whole"},
	}
	for _, tt := range tests {
		physical, partition := NormalizeDriveShed(tt.raw)
		if physical != tt.physical || partition != tt.partition {
			t.Fatalf("NormalizeDriveShed(%q) = %q/%q, want %q/%q", tt.raw, physical, partition, tt.physical, tt.partition)
		}
	}
}
