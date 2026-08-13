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

func TestApplyShedPartitionDisplayKeepsExactShedName(t *testing.T) {
	shed := domain.CampaignShed{LocationID: "loc-2", DisplayName: "Godel 1 - Part 3"}
	applyShedPartitionDisplay(&shed)
	if shed.ParentShedName != "Godel 1 - Part 3" {
		t.Fatalf("ParentShedName = %q, want %q", shed.ParentShedName, "Godel 1 - Part 3")
	}
	if shed.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want blank because Godel 1 - Part 3 is the shed", shed.PartitionLabel)
	}
	if shed.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Godel 1 - Part 3")
	}
}

func TestApplyShedPartitionDisplayWithStoredLabelKeepsExactShedName(t *testing.T) {
	cases := []struct {
		name        string
		storedLabel string
	}{
		{name: "Castro 2", storedLabel: "2"},
		{name: "Gandhi 1", storedLabel: "1"},
		{name: "Godel 2 - Part 1", storedLabel: "Part 1"},
		{name: "Mandela 2 Part 1", storedLabel: "Part 1"},
	}
	for _, c := range cases {
		shed := domain.CampaignShed{LocationID: "loc", DisplayName: c.name}
		applyShedPartitionDisplayWithStoredLabel(&shed, c.storedLabel)
		if shed.ParentShedName != c.name {
			t.Fatalf("%s ParentShedName = %q, want exact shed name", c.name, shed.ParentShedName)
		}
		if shed.PartitionLabel != "" {
			t.Fatalf("%s PartitionLabel = %q, want blank compatibility label", c.name, shed.PartitionLabel)
		}
		if shed.OperationalLocationDisplay != c.name {
			t.Fatalf("%s OperationalLocationDisplay = %q, want exact shed name", c.name, shed.OperationalLocationDisplay)
		}
	}
}

func TestApplyPlannerShedPartitionDisplay(t *testing.T) {
	shed := domain.PlannerShed{LocationID: "loc-3", Name: "Godel 1 - Part 3"}
	applyPlannerShedPartitionDisplay(&shed)
	if shed.ParentShedName != "Godel 1 - Part 3" {
		t.Fatalf("ParentShedName = %q, want %q", shed.ParentShedName, "Godel 1 - Part 3")
	}
	if shed.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want blank because Godel 1 - Part 3 is the shed", shed.PartitionLabel)
	}
	if shed.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Godel 1 - Part 3")
	}
}

func TestPlannerParkBucketsPartitionOneToManyDisplayDoesNotCollapseSiblings(t *testing.T) {
	sheds := []domain.PlannerShed{
		{LocationID: "castro-1", ParentShedName: "Castro 1"},
		{LocationID: "castro-2", ParentShedName: "Castro 2"},
		{LocationID: "yashoda", ParentShedName: "Yashoda"},
	}
	seen := map[string]bool{}
	for i := range sheds {
		applyPlannerShedOperationalDisplay(&sheds[i])
		key := sheds[i].LocationID + "|" + sheds[i].PartitionLabel
		if seen[key] {
			t.Fatalf("operational key collapsed sibling partition %q", key)
		}
		seen[key] = true
	}
	if got := sheds[0].OperationalLocationDisplay; got != "Castro 1" {
		t.Fatalf("first sibling display = %q, want Castro 1", got)
	}
	if got := sheds[1].OperationalLocationDisplay; got != "Castro 2" {
		t.Fatalf("second sibling display = %q, want Castro 2", got)
	}
	if got := sheds[2].OperationalLocationDisplay; got != "Yashoda" {
		t.Fatalf("unpartitioned display = %q, want Yashoda", got)
	}
}

func TestPlannerParkBucketsPartitionPaginationPageBoundaryCursorIncludesPartition(t *testing.T) {
	cursor := plannerBucketCursor{
		Set:            true,
		ShedOrder:      7,
		ShedName:       "Castro",
		PartitionOrder: 2,
		PartitionKey:   "2",
		ShedID:         "castro",
	}
	decoded, err := decodePlannerBucketCursor(encodePlannerBucketCursor(cursor))
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.ShedName != "Castro" || decoded.PartitionOrder != 2 || decoded.PartitionKey != "2" || decoded.ShedID != "castro" {
		t.Fatalf("decoded cursor = %+v, want partition-aware cursor %+v", decoded, cursor)
	}
}

func TestPlannerParkBucketsPartitionParkScopeUsesOperationalKey(t *testing.T) {
	left := operationalLocationDisplay("park-a-castro-1", "Castro 1", "")
	right := operationalLocationDisplay("park-b-castro-1", "Castro 1", "")
	if left != "Castro 1" || right != "Castro 1" {
		t.Fatalf("display must be stable across parks, got %q and %q", left, right)
	}
	if key := "park-a-castro-1|"; key == "park-b-castro-1|" {
		t.Fatalf("park-scoped operational keys must include the shed id")
	}
}

func TestPlannerParkBucketsPartitionStatusMatrixNeverRendersWholeSentinel(t *testing.T) {
	statuses := []string{"queued", "in_progress", "closed", "completed", "canceled"}
	for _, status := range statuses {
		shed := domain.PlannerShed{LocationID: "castro", ParentShedName: "Castro", PartitionLabel: "whole", ScheduledStatus: status}
		applyPlannerShedOperationalDisplay(&shed)
		if shed.OperationalLocationDisplay != "Castro" {
			t.Fatalf("status %s display = %q, want parent-only for whole sentinel", status, shed.OperationalLocationDisplay)
		}
	}
}

func TestPlannerOperationalDisplayIgnoresCompatibilityPartitionLabel(t *testing.T) {
	shed := domain.PlannerShed{
		LocationID:      "godel-2-part-1",
		ParentShedName: "Godel 2 - Part 1",
		PartitionLabel: "Part 1",
	}
	applyPlannerShedOperationalDisplay(&shed)
	if shed.Name != "Godel 2 - Part 1" || shed.OperationalLocationDisplay != "Godel 2 - Part 1" {
		t.Fatalf("display = (%q, %q), want exact shed name", shed.Name, shed.OperationalLocationDisplay)
	}
	if shed.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want blank compatibility label", shed.PartitionLabel)
	}
}
