package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
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
