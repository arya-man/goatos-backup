package domain

import (
	"reflect"
	"testing"
)

func TestHistoricalAnswerFallbackPreservesOtherTextAndOrder(t *testing.T) {
	answers := map[string]string{"transport": "other", "transport_other": "Farm tractor", "z_note": "Last note", "a_note": "First note"}
	want := []VendorAnswerRow{{QuestionID: "a_note", Label: "a_note", Value: "First note"}, {QuestionID: "transport", Label: "transport", Value: "Farm tractor"}, {QuestionID: "z_note", Label: "z_note", Value: "Last note"}}
	for name, render := range map[string]func(VendorForm, map[string]string) []VendorAnswerRow{"vendor": VendorAnswerRows, "feed": FeedPurchaseAnswerRows} {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				if got := render(VendorForm{}, answers); !reflect.DeepEqual(got, want) {
					t.Fatalf("lost or unstable historical answers: %+v", got)
				}
			}
		})
	}
}
