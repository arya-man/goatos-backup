package domain

import "testing"

// TestFeedDeliveryLabelsSayTransitAndDelivered pins the maintainer's 2026-09-05 wording on the ONE
// function both surfaces read.
//
// It exists because the first pass at that rename missed this function and changed only the
// admin-web page contract's option labels. The phone reads FeedDeliveryLabel directly, so its chips
// came back "All | In transit | Reached" -- half renamed, which is worse than not renaming at all:
// a chip saying "In transit" beside one saying "Reached" reads as two vocabularies for one
// lifecycle. Caught on the device, not by any test, because nothing asserted the words.
func TestFeedDeliveryLabelsSayTransitAndDelivered(t *testing.T) {
	for status, want := range map[string]string{
		FeedDeliveryPurchased: "In transit",
		FeedDeliveryReached:   "Delivered",
	} {
		if got := FeedDeliveryLabel(status); got != want {
			t.Errorf("FeedDeliveryLabel(%q) = %q, want %q", status, got, want)
		}
	}
	// The banned words, stated directly so a revert fails here rather than on a phone screen.
	for _, status := range FeedDeliveryStatuses {
		switch FeedDeliveryLabel(status) {
		case "On the road", "Reached":
			t.Errorf("FeedDeliveryLabel(%q) still uses the retired wording", status)
		}
	}
	// The STORED values are deliberately unchanged: only the shown word moved.
	if FeedDeliveryPurchased != "purchased" || FeedDeliveryReached != "reached" {
		t.Fatal("the stored delivery values changed; the rename is vocabulary only and must not touch storage")
	}
}
