package domain

import "testing"

func TestNormalizeDriveShedKeepsExactShedNames(t *testing.T) {
	tests := []struct {
		raw       string
		physical  string
		partition string
	}{
		{raw: "Gandhi 1", physical: "Gandhi 1", partition: "whole"},
		{raw: "Castro 2", physical: "Castro 2", partition: "whole"},
		{raw: "Mandela 2 Part 1", physical: "Mandela 2 Part 1", partition: "whole"},
		{raw: "Godel 1 - Part 3", physical: "Godel 1 - Part 3", partition: "whole"},
		{raw: "Old Yashoda", physical: "Old Yashoda", partition: "whole"},
	}
	for _, tt := range tests {
		physical, partition := NormalizeDriveShed(tt.raw)
		if physical != tt.physical || partition != tt.partition {
			t.Fatalf("NormalizeDriveShed(%q) = %q/%q, want %q/%q", tt.raw, physical, partition, tt.physical, tt.partition)
		}
	}
}
