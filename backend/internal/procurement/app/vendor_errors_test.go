package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

func TestVendorValidationErrorLabelsAverageAnimalWeight(t *testing.T) {
	err := VendorHTTPError(domain.ErrVendorValidation{
		Field:  "average_animal_weight_kg",
		Reason: "must be more than zero",
	})

	if err.Message != "Average animal weight must be more than zero." {
		t.Fatalf("unexpected message: %q", err.Message)
	}
}

// The duplicate refusal is rendered verbatim in a phone banner, so it must stay short and must
// name the action. The 2026-09-18 wording ran to two sentences and 27 words; the operator's phone
// hid it behind "Try again", and they tried four times.
func TestVendorDuplicateRefusalIsShortAndNamesTheAction(t *testing.T) {
	err := VendorHTTPError(ports.ErrVendorDuplicate)

	if err.Code != "vendor_duplicate" || err.HTTPStatus != http.StatusConflict {
		t.Fatalf("unexpected error shape: %+v", err)
	}
	if words := len(strings.Fields(err.Message)); words > 16 {
		t.Fatalf("duplicate refusal is %d words, must fit a phone banner (<= 16): %q", words, err.Message)
	}
	if !strings.Contains(err.Message, "already exists") || !strings.Contains(err.Message, "Edit it") {
		t.Fatalf("duplicate refusal must say the vendor exists and that editing is the fix: %q", err.Message)
	}
}
