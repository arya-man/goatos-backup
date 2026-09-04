package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The feed & water removal category contract (maintainer decision 2026-09-03): a
// system-created, verifier-reviewed, task-proof category with exactly two backend-owned
// video slots, never plannable by a human.
func TestFeedWaterRemovalCategoryContract(t *testing.T) {
	if !IsValidCategory(CategoryFeedWaterRemoval) {
		t.Fatal("feed_water_removal must be a valid category")
	}
	if !IsKernelOwnedCategory(CategoryFeedWaterRemoval) {
		t.Fatal("feed_water_removal must be system-owned — the deworming create births it, never the planner")
	}
	for _, c := range PlannerCategories {
		if c == CategoryFeedWaterRemoval {
			t.Fatal("feed_water_removal must NOT be planner-creatable")
		}
	}
	reviewed := false
	for _, c := range VerifierReviewedCategories {
		if c == CategoryFeedWaterRemoval {
			reviewed = true
		}
	}
	if !reviewed {
		t.Fatal("feed_water_removal must be verifier-reviewed (VerifierReviewedCategories)")
	}
	if IsDirectorApprovedCategory(CategoryFeedWaterRemoval) {
		t.Fatal("feed_water_removal is verifier-reviewed, not director-approved")
	}
	if got := CaptureModeForCategory(CategoryFeedWaterRemoval); got != CaptureModeTaskProof {
		t.Fatalf("capture mode = %q, want task_proof", got)
	}
	if got := CategoryLabel(CategoryFeedWaterRemoval); got != "Feed & water removal" {
		t.Fatalf("label = %q, want farm-facing 'Feed & water removal'", got)
	}
	if got := VerificationCategoryFor(CategoryFeedWaterRemoval); got != "pc_feed_water_removal" {
		t.Fatalf("verification category = %q, want pc_feed_water_removal", got)
	}
	if VerificationCategoryFeedWaterRemoval != "pc_feed_water_removal" {
		t.Fatalf("VerificationCategoryFeedWaterRemoval = %q, want pc_feed_water_removal", VerificationCategoryFeedWaterRemoval)
	}

	slots := SlotsForCategory(CategoryFeedWaterRemoval)
	if len(slots) != 2 {
		t.Fatalf("slots = %d, want exactly 2 (feed + water)", len(slots))
	}
	if slots[0].FieldKey != SlotFeedVideo || slots[0].Label != "Feed removal video" {
		t.Fatalf("slot[0] = %+v, want feed_video / 'Feed removal video'", slots[0])
	}
	if slots[1].FieldKey != SlotWaterVideo || slots[1].Label != "Water removal video" {
		t.Fatalf("slot[1] = %+v, want water_video / 'Water removal video'", slots[1])
	}
	if !IsValidSlotForCategory(CategoryFeedWaterRemoval, SlotFeedVideo) ||
		!IsValidSlotForCategory(CategoryFeedWaterRemoval, SlotWaterVideo) {
		t.Fatal("both removal slots must validate for the category")
	}
	if IsValidSlotForCategory(CategoryFeedWaterRemoval, SlotStockFridgePhoto) {
		t.Fatal("a fridge slot must not validate for feed_water_removal")
	}
}

// The 20:00 IST planning cutoff, on a pinned clock (business-DAY grain in Asia/Kolkata; the
// counts/domain.ShiftingActionsDueFrom shape — the caller's clock, never time.Now()).
func TestEarliestFeedRemovalDewormingDateCrossesTheEveningCutoff(t *testing.T) {
	ist := biztime.DefaultLocation()
	day := func(d int) time.Time { return time.Date(2026, time.September, d, 0, 0, 0, 0, ist) }

	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before 20:00 IST -> tomorrow", time.Date(2026, time.September, 10, 19, 59, 59, 0, ist), day(11)},
		{"at 20:00 IST -> day after tomorrow", time.Date(2026, time.September, 10, 20, 0, 0, 0, ist), day(12)},
		{"after 20:00 IST -> day after tomorrow", time.Date(2026, time.September, 10, 22, 15, 0, 0, ist), day(12)},
		{"morning -> tomorrow", time.Date(2026, time.September, 10, 6, 0, 0, 0, ist), day(11)},
	}
	for _, tc := range cases {
		if got := EarliestFeedRemovalDewormingDate(tc.now); !got.Equal(tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Adversarial timezone case: 15:00 UTC is only mid-afternoon in London but 20:30 IST — the
// rule reads the IST wall clock, so the window is already closed for tomorrow.
func TestEarliestFeedRemovalDewormingDateUsesISTWallClockNotUTC(t *testing.T) {
	nowUTC := time.Date(2026, time.September, 10, 15, 0, 0, 0, time.UTC) // 20:30 IST
	want := time.Date(2026, time.September, 12, 0, 0, 0, 0, biztime.DefaultLocation())
	if got := EarliestFeedRemovalDewormingDate(nowUTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v (IST wall clock decides, not UTC)", got, want)
	}
}
