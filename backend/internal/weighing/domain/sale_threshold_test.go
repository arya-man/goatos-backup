package domain

import "testing"

// The sale-ready line is the caller's (the tenant's sale_ready_threshold_kg assumption, maintainer
// decision 2026-09-19): 0 means the 35 kg default, a supplied line replaces ONLY the upper line,
// and the margin still lowers both lines together so the 30 kg and sale-ready counts read the
// same population at the same tolerance.
func TestSaleThresholdsKgTakeTheCallersSaleLine(t *testing.T) {
	lower, upper := SaleThresholdsKg(0, 0, 0)
	if lower != 30 || upper != 35 {
		t.Fatalf("defaults = %v/%v, want 30/35", lower, upper)
	}
	lower, upper = SaleThresholdsKg(0, 0, 32)
	if lower != 30 || upper != 32 {
		t.Fatalf("supplied line = %v/%v, want 30/32", lower, upper)
	}
	lower, upper = SaleThresholdsKg(500, 0, 40)
	if lower != 29.5 || upper != 39.5 {
		t.Fatalf("margin on a supplied line = %v/%v, want 29.5/39.5", lower, upper)
	}
	lower, upper = SaleThresholdsKg(0, 28, 33)
	if lower != 28 || upper != 33 {
		t.Fatalf("supplied both lines = %v/%v, want 28/33", lower, upper)
	}
	for _, kg := range []float64{0, 10, 34.5, 80} {
		if !ValidSaleThresholdUpperKg(kg) {
			t.Fatalf("%v kg must be a valid sale line", kg)
		}
	}
	for _, kg := range []float64{-1, 3, 9.99, 80.01, 500} {
		if ValidSaleThresholdUpperKg(kg) {
			t.Fatalf("%v kg must be refused, never clamped", kg)
		}
	}
}
