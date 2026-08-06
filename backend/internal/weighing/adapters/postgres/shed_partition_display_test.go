package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Only the EXPLICIT worded convention is parseable from a name. A trailing number is ambiguous:
// "Castro 2" (a partition) and "Mandela 1" (an ordinary numbered shed) are identical as strings,
// and the canonical oploc fixture pins "Mandela 1" as NON-partitioned. Guessing from the number
// stamped false partition labels on ordinary sheds, so the numeric convention is resolved from
// the shed_partitions catalog in migration 000121, never from the name here.
func TestSplitShedPartitionNameOnlyParsesExplicitPartSuffix(t *testing.T) {
	cases := []struct {
		name          string
		wantParent    string
		wantPartition string
	}{
		{"Godel 1 - Part 3", "Godel 1", "Part 3"},
		{"Mandela 1 - Part 10", "Mandela 1", "Part 10"},
		// Non-partitioned sheds pass through unchanged with no partition label.
		{"Yashoda", "Yashoda", ""},
		{"Ho Chi Minh", "Ho Chi Minh", ""},
		// REGRESSION: a trailing number must NOT be inferred as a partition. "Mandela 1" is an
		// ordinary shed (canonical fixture, oploc/golden_fixture_test.go), and "Castro 2" cannot
		// be distinguished from it by name -- the catalog decides, not this parser.
		{"Mandela 1", "Mandela 1", ""},
		{"Castro 2", "Castro 2", ""},
		{"Gandhi 3", "Gandhi 3", ""},
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
	// Worded convention only -- that is the one a NAME can prove.
	shed := domain.CampaignShed{LocationID: "loc-2", DisplayName: "Godel 1 - Part 3"}
	applyShedPartitionDisplay(&shed)
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

// REGRESSION: a numeric-suffixed catalog name must be carried through UNSPLIT. "Castro 2" may be
// a partition of Castro OR an ordinary shed named "Castro 2" (as "Mandela 1" is in the canonical
// fixture); the name cannot say which, so weighing renders it verbatim and leaves the partition
// fact to the catalog-backed backfill. Splitting it here stamped a false partition_label on every
// ordinary numbered shed.
func TestApplyShedPartitionDisplayDoesNotInventPartitionFromTrailingNumber(t *testing.T) {
	shed := domain.CampaignShed{LocationID: "loc-9", DisplayName: "Mandela 1"}
	applyShedPartitionDisplay(&shed)
	if shed.PartitionLabel != "" {
		t.Fatalf("PartitionLabel = %q, want empty -- a trailing number is not proof of a partition", shed.PartitionLabel)
	}
	if shed.OperationalLocationDisplay != "Mandela 1" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", shed.OperationalLocationDisplay, "Mandela 1")
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
