package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

func kgPtr(v string) *string { return &v }

// The verifier's entry boxes list ONLY what the frozen sheet directs this bag to contain
// (maintainer decision 2026-08-22): the sheet's grid mentions every feed item the pen's ration
// rows carry, zero-quantity cells included, and shipping those as boxes made the verifier type 0
// for every item the shed is never fed. Zero, blank, unparseable and blocked cells are all
// omitted; order of the surviving items is the sheet's own.
func TestPackingEntryFieldsListOnlyItemsDirectedForTheBag(t *testing.T) {
	reason := domain.BlockedReason{Code: "ration_unauthored", Detail: "no rate for F2"}
	got := packingEntryFields([]domain.ItemQuantity{
		{FeedItem: "Maize", Status: domain.QuantityResolved, QuantityKg: kgPtr("12.500")},
		{FeedItem: "Soya DOC", Status: domain.QuantityResolved, QuantityKg: kgPtr("0.000")},
		{FeedItem: "Lucerne", Status: domain.QuantityResolved, QuantityKg: kgPtr("4.000")},
		{FeedItem: "Mineral Mix", Status: domain.QuantityResolved, QuantityKg: nil},
		{FeedItem: "Silage", Status: domain.QuantityBlocked, QuantityKg: nil, BlockedReason: &reason},
		{FeedItem: "Ghost", Status: domain.QuantityResolved, QuantityKg: kgPtr("not-a-number")},
	})

	want := []PackingMeasurementField{
		{Key: domain.NormalizeConfigKey("Maize"), Label: "Maize"},
		{Key: domain.NormalizeConfigKey("Lucerne"), Label: "Lucerne"},
	}
	if len(got) != len(want) {
		t.Fatalf("fields = %+v, want only the positively-directed items %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v (sheet order preserved)", i, got[i], want[i])
		}
	}
}

// A bag whose every cell is zero or blocked yields NO boxes -- nil, the same shape as an
// unreadable sheet -- so the verification service's fields-less exemption applies and the item
// stays approvable as a plain judge-the-video review.
func TestPackingEntryFieldsAreNilWhenNothingIsDirected(t *testing.T) {
	if got := packingEntryFields([]domain.ItemQuantity{
		{FeedItem: "Maize", Status: domain.QuantityResolved, QuantityKg: kgPtr("0.000")},
	}); got != nil {
		t.Fatalf("fields = %+v, want nil for an all-zero bag", got)
	}
}
