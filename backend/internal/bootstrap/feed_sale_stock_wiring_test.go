package bootstrap

import (
	"os"
	"strings"
	"testing"
)

// TestAPIWiresTheFeedStoreIntoSales keeps the short-feed-sale confirmation actually connected.
//
// The confirmation is OPTIONAL by construction: a SalesService built without a feed store records
// feed sales with no warning, which is what the unit tests' fakes rely on. That makes forgetting
// the wiring silent -- the sale records, the stock still goes down, and nobody is ever asked about
// a sale taking more than the store holds. api.go is a composition root with no injectable seam,
// so the assertion is on its source: the same thing a reviewer would check, made mechanical.
func TestAPIWiresTheFeedStoreIntoSales(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatalf("read api.go: %v", err)
	}
	wired := false
	for _, line := range strings.Split(string(src), "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") {
			continue
		}
		if strings.Contains(code, "WithFeedStock(") {
			wired = true
		}
	}
	if !wired {
		t.Fatal("api.go never calls WithFeedStock, so a sale taking more feed than the store holds " +
			"would record with no confirmation and nobody would notice")
	}
}

// TestAPIWiresTheTaggedAnimalsReaderIntoSales keeps the failed-sale refusal connected (maintainer
// decision 2026-09-25). A sales repository built without the reader marks a deal failed with its
// tagged animals already gone from the herd, silently -- the same optional-by-construction trap.
func TestAPIWiresTheTaggedAnimalsReaderIntoSales(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatalf("read api.go: %v", err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		code := strings.TrimSpace(line)
		if !strings.HasPrefix(code, "//") && strings.Contains(code, "WithTaggedAnimals(salesidentitybridge.New(") {
			return
		}
	}
	t.Fatal("api.go never wires WithTaggedAnimals, so a sale could be marked failed while animals are tagged to it")
}
