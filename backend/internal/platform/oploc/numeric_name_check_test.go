package oploc

import "testing"

// TestSplitShedPartitionNameDoesNotInventPartitionsFromTrailingNumbers pins the rule the
// maintainer stated repeatedly: an UNDIVIDED shed whose NAME ends in a number is a whole name.
// "Yashoda 2" is a shed. "Mandela 1" is a shed. The trailing number is not a partition, and a
// NAME alone cannot tell you which it is -- "Castro 2" (partition 2 of Castro) and "Mandela 1"
// (an ordinary shed) are indistinguishable as strings. Only the shed_partitions catalog knows.
func TestSplitShedPartitionNameDoesNotInventPartitionsFromTrailingNumbers(t *testing.T) {
	for _, name := range []string{"Yashoda 2", "Mandela 1", "Ho Chi Minh 1", "Godel 1"} {
		shed, partition := SplitShedPartitionName(name)
		if partition != "" {
			t.Errorf("SplitShedPartitionName(%q) invented partition %q -- a trailing number is part of the NAME; only the catalog knows a real partition", name, partition)
		}
		if shed != name {
			t.Errorf("SplitShedPartitionName(%q) split the name to %q -- an undivided shed name must survive whole", name, shed)
		}
		if got := (OperationalLocation{ShedName: shed, PartitionLabel: partition}).Display(); got != name {
			t.Errorf("Display for %q = %q, want %q", name, got, name)
		}
	}
}

func TestSplitShedPartitionNameStillParsesLegacyHyphenWordedConvention(t *testing.T) {
	shed, partition := SplitShedPartitionName("Godel 1 - Part 3")
	if shed != "Godel 1" || partition != "Part 3" {
		t.Fatalf("got shed=%q partition=%q, want \"Godel 1\"/\"Part 3\"", shed, partition)
	}
}
