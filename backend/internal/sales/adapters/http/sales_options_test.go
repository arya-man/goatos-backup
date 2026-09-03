package http

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// TestSalesOptionsCarryEveryVocabularyTheWebDrawerOffers pins GET /sales/options (maintainer
// instruction 2026-09-04): both farms, all three products each with its breeds, all four statuses
// with a chip tone, the default status and the 60-day sale-date horizon.
func TestSalesOptionsCarryEveryVocabularyTheWebDrawerOffers(t *testing.T) {
	got := buildSalesOptionsPayload()
	if len(got.Farms) != 2 || got.Farms[0] != domain.FarmCBE || got.Farms[1] != domain.FarmCPT {
		t.Fatalf("farms = %v", got.Farms)
	}
	if len(got.ProductTypes) != 3 {
		t.Fatalf("product types = %v", got.ProductTypes)
	}
	for _, p := range got.ProductTypes {
		if len(got.Breeds[p]) == 0 {
			t.Fatalf("product %q offers no breed", p)
		}
	}
	if len(got.Breeds[domain.ProductGoat]) != 5 || len(got.Breeds[domain.ProductSheep]) != 3 {
		t.Fatalf("breeds = %v", got.Breeds)
	}
	if len(got.Statuses) != 4 {
		t.Fatalf("statuses = %v", got.Statuses)
	}
	for _, s := range got.Statuses {
		if s.Tone == "" || s.Label != s.Key {
			t.Fatalf("status %+v carries no tone or a label that is not its key", s)
		}
	}
	if got.DefaultStatus != domain.StatusDealClosed || got.MaxSaleDateDaysAhead != 60 {
		t.Fatalf("default/horizon = %q/%d", got.DefaultStatus, got.MaxSaleDateDaysAhead)
	}
}
