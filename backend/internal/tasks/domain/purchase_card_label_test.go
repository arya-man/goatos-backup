package domain

import "testing"

// TestPurchaseCardsNameTheirLoad pins the subject line the phone's Work list shows. Without it the
// 2026-09-20 E2E had three cards all reading "Animal purchase" with nothing to tell the loads
// apart -- which is what the list is for.
func TestPurchaseCardsNameTheirLoad(t *testing.T) {
	// The composer lives in the postgres adapter beside the sale's; this asserts the SHAPE it
	// depends on: each purchase template is subject-keyed, so the card's subject_ref_id is the
	// load it names, and a 1:0..1 join on that key cannot fan a card out.
	for _, key := range []string{TemplateKeyAnimalPurchaseIntake, TemplateKeyFeedPurchaseIntake} {
		if !SubjectKeyedTemplate(key) {
			t.Fatalf("%q must be subject-keyed for its card to name a load", key)
		}
		if TemplateLabel(key) == "" {
			t.Fatalf("%q has no operator-facing label", key)
		}
	}
}
