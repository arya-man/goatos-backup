package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// TestPaymentRefusalNamesTheFieldInFarmWords pins the receipt refusals the payment form now shows
// verbatim (2026-09-25): "received_on cannot be in the future." leaked the contract key onto the
// sales desk's screen because salesFieldLabel had no entry for a receipt's fields.
func TestPaymentRefusalNamesTheFieldInFarmWords(t *testing.T) {
	for field, want := range map[string]string{
		"received_on":   "Received on cannot be in the future.",
		"amount_rupees": "Amount must be more than zero.",
		"note":          "Note too long.",
	} {
		reason := map[string]string{"received_on": "cannot be in the future", "amount_rupees": "must be more than zero", "note": "too long"}[field]
		got := SalesHTTPError(domain.ErrDealValidation{Field: field, Reason: reason})
		if got.Message != want {
			t.Fatalf("%s: message = %q, want %q", field, got.Message, want)
		}
		if strings.Contains(got.Message, "_") {
			t.Fatalf("%s: message leaks a raw key: %q", field, got.Message)
		}
	}
}
