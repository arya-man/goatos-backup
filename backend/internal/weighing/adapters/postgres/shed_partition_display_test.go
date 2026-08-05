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
		{"Castro 2", "Castro", "2"},
		{"Gandhi 3", "Gandhi", "3"},
		{"Godel 1 - Part 3", "Godel 1", "Part 3"},
		{"Mandela 1 - Part 10", "Mandela 1", "Part 10"},
		// Non-partitioned sheds must pass through unchanged with no partition label.
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
	shed := domain.CampaignShed{LocationID: "loc-2", DisplayName: "Castro 2"}
	applyShedPartitionDisplay(&shed)
	if shed.ParentShedName != "Castro" {
		t.Fatalf("ParentShedName = %q, want %q", shed.ParentShedName, "Castro")
	}
	if shed.PartitionLabel != "2" {
		t.Fatalf("PartitionLabel = %q, want %q", shed.PartitionLabel, "2")
	}
	if shed.OperationalLocationDisplay != "Castro 2" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Castro 2")
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
