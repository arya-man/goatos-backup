package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestSplitShedPartitionNameBothConventions(t *testing.T) {
	cases := []struct {
		name          string
		wantParent    string
		wantPartition string
	}{
		// ONLY the explicit worded convention is parseable from a NAME.
		{"Godel 1 - Part 3", "Godel 1", "Part 3"},
		{"Mandela 1 - Part 10", "Mandela 1", "Part 10"},
		// The numeric convention is NOT parseable and must pass through WHOLE. "Castro 2" (a
		// partition of Castro) and "Mandela 1" (an ordinary shed) are indistinguishable as
		// strings, so guessing renames every numbered shed on the farm -- Godel 1 became
		// "Godel - 1" and Mandela 1 became "Mandela - 1" under the old regex. The real
		// partition for a numeric name comes from the shed_partitions CATALOG
		// (shed_partition_resolve.go), which knows which partitions exist. A name cannot.
		{"Castro 2", "Castro 2", ""},
		{"Gandhi 3", "Gandhi 3", ""},
		{"Godel 1", "Godel 1", ""},
		{"Mandela 1", "Mandela 1", ""},
		// Names with no number at all are unchanged, as before.
		{"Yashoda", "Yashoda", ""},
		{"Ho Chi Minh", "Ho Chi Minh", ""},
	}
	for _, c := range cases {
		parent, partition := splitShedPartitionName(c.name)
		if parent != c.wantParent || partition != c.wantPartition {
			t.Errorf("splitShedPartitionName(%q) = (%q, %q), want (%q, %q)",
				c.name, parent, partition, c.wantParent, c.wantPartition)
		}
	}
}

func TestApplyShedPartitionDisplayNeverRendersWholeSentinel(t *testing.T) {
	shed := domain.CampaignShed{LocationID: "loc-1", DisplayName: "Yashoda"}
	applyShedPartitionDisplay(&shed)
	if shed.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want empty for a non-partitioned shed", shed.PartitionLabel)
	}
	if shed.OperationalLocationDisplay != "Yashoda" {
		t.Fatalf("OperationalLocationDisplay = %q, want bare shed name (never a 'whole' sentinel)", shed.OperationalLocationDisplay)
	}
}

func TestApplyShedPartitionDisplayCarriesParentAndPartition(t *testing.T) {
	// Worded convention: parseable from the name, so parent and partition split.
	shed := domain.CampaignShed{LocationID: "loc-2", DisplayName: "Godel 1 - Part 3"}
	applyShedPartitionDisplay(&shed)
	if shed.ParentShedName != "Godel 1" {
		t.Fatalf("ParentShedName = %q, want %q", shed.ParentShedName, "Godel 1")
	}
	if shed.PartitionLabel != "Part 3" {
		t.Fatalf("PartitionLabel = %q, want %q", shed.PartitionLabel, "Part 3")
	}
	// The composed display uses the " - " separator (2026-08-06).
	if shed.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Castro - 2")
	}
}

func TestApplyPlannerShedPartitionDisplay(t *testing.T) {
	shed := domain.PlannerShed{LocationID: "loc-3", Name: "Godel 1 - Part 3"}
	applyPlannerShedPartitionDisplay(&shed)
	if shed.ParentShedName != "Godel 1" {
		t.Fatalf("ParentShedName = %q, want %q", shed.ParentShedName, "Godel 1")
	}
	if shed.PartitionLabel != "Part 3" {
		t.Fatalf("PartitionLabel = %q, want %q", shed.PartitionLabel, "Part 3")
	}
	if shed.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Godel 1 - Part 3")
	}
}
