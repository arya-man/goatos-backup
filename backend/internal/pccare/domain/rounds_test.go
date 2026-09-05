package domain

import (
	"errors"
	"testing"
)

// A pen is the SHED plus the NORMALIZED partition, so the same pen written three ways is
// one pen. Without this the create would happily plan "Part 3" and " part 3 " as two
// buckets, and the second would then be refused by the database's partition_key unique
// index AFTER the first had already been written.
func TestPenKeyNormalizesThePartitionTheWayTheDatabaseDoes(t *testing.T) {
	base := RoundPen{ShedID: "shed-1", PartitionLabel: "Part 3"}
	for _, variant := range []string{"Part 3", "part 3", "  Part 3  ", "PART 3"} {
		if got := (RoundPen{ShedID: "shed-1", PartitionLabel: variant}).PenKey(); got != base.PenKey() {
			t.Fatalf("PenKey(%q) = %q, want %q", variant, got, base.PenKey())
		}
	}
	whole := RoundPen{ShedID: "shed-1"}
	if whole.PenKey() == base.PenKey() {
		t.Fatal("an undivided shed must not key equal to one of its pens")
	}
}

func TestValidateRoundPensRefusesEmptyOversizeAndDuplicate(t *testing.T) {
	if err := ValidateRoundPens(nil); !errors.Is(err, ErrNoPens) {
		t.Fatalf("no pens err = %v, want ErrNoPens", err)
	}

	tooMany := make([]RoundPen, MaxPensPerRound+1)
	for i := range tooMany {
		tooMany[i] = RoundPen{ShedID: "shed", PartitionLabel: string(rune('a'+i%26)) + string(rune('a'+i/26))}
	}
	if err := ValidateRoundPens(tooMany); !errors.Is(err, ErrTooManyPens) {
		t.Fatalf("oversize err = %v, want ErrTooManyPens", err)
	}

	// The duplicate is spelled differently, which is the case a naive == would miss.
	dup := []RoundPen{
		{ShedID: "shed-1", PartitionLabel: "Part 3"},
		{ShedID: "shed-2", PartitionLabel: "Part 1"},
		{ShedID: "shed-1", PartitionLabel: "part 3"},
	}
	if err := ValidateRoundPens(dup); !errors.Is(err, ErrDuplicatePen) {
		t.Fatalf("duplicate err = %v, want ErrDuplicatePen", err)
	}

	ok := []RoundPen{{ShedID: "shed-1", PartitionLabel: "Part 3"}, {ShedID: "shed-1", PartitionLabel: "Part 4"}, {ShedID: "shed-2"}}
	if err := ValidateRoundPens(ok); err != nil {
		t.Fatalf("valid pen list err = %v, want nil", err)
	}
	if err := ValidateRoundPens(make([]RoundPen, 0, 4)); !errors.Is(err, ErrNoPens) {
		t.Fatalf("empty-but-allocated err = %v, want ErrNoPens", err)
	}
}

// The round-plannable set must BE the planner set. If a future category is added to
// PlannerCategories and this drifts, the wizard would offer a category the round create
// refuses — so the rule is derived, and this test is what proves the derivation.
func TestRoundPlannableCategoriesAreExactlyThePlannerCategories(t *testing.T) {
	for _, c := range PlannerCategories {
		if !IsRoundPlannableCategory(c) {
			t.Fatalf("planner category %q is not round-plannable", c)
		}
	}
	for _, c := range []string{CategoryInventoryVaccine, CategoryFeedWaterRemoval, "", "nonsense"} {
		if IsRoundPlannableCategory(c) {
			t.Fatalf("category %q must not be round-plannable", c)
		}
	}
}

// The card shows ONE status for pens that each carry their own. Completed only when every
// pen is; rework outranks pending because a bounced pen is work somebody must redo, while
// a pending pen is work already done.
func TestRoundStatusRollup(t *testing.T) {
	for _, tc := range []struct {
		name string
		pens []string
		want string
	}{
		{"no pens", nil, StatusOpen},
		{"all completed", []string{StatusCompleted, StatusCompleted}, StatusCompleted},
		{"one still open", []string{StatusCompleted, StatusOpen}, StatusOpen},
		{"rework outranks pending", []string{StatusPendingVerification, StatusRework}, StatusRework},
		{"rework outranks completed", []string{StatusCompleted, StatusRework}, StatusRework},
		// The trap: three pens submitted and one pen never worked must NOT read "in review".
		// That would tell the operator their evening is finished while a pen is still full.
		{"one unworked pen outranks submitted ones", []string{StatusPendingVerification, StatusPendingVerification, StatusOpen}, StatusOpen},
		{"every pen submitted", []string{StatusPendingVerification, StatusPendingVerification}, StatusPendingVerification},
		{"submitted beside completed", []string{StatusCompleted, StatusPendingVerification}, StatusPendingVerification},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoundStatusRollup(tc.pens); got != tc.want {
				t.Fatalf("RoundStatusRollup(%v) = %q, want %q", tc.pens, got, tc.want)
			}
		})
	}
}
