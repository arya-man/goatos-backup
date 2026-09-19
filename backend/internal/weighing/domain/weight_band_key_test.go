package domain

import "testing"

// The Weight-wise bands follow the caller's edges (the tenant's weight_band_edges_kg assumption,
// maintainer decision 2026-09-19). The DEFAULT edges must reproduce the exact keys the tab has
// always carried -- so a deployment with the seeded rows changes nothing on the wire -- and any
// other edges produce keys and farm labels from the same arithmetic.
func TestWeightBandKeysAndLabelsFollowTheEdges(t *testing.T) {
	wantKeys := []string{"under_15", "15_20", "20_25", "25_30", "30_35", "35_plus"}
	wantLabels := []string{"Under 15 kg", "15 – 20 kg", "20 – 25 kg", "25 – 30 kg", "30 – 35 kg", "35 kg and over"}
	for i := range wantKeys {
		if got := WeightBandKey(nil, i); got != wantKeys[i] {
			t.Fatalf("default key %d = %q want %q", i, got, wantKeys[i])
		}
		if got := WeightBandLabel(nil, i); got != wantLabels[i] {
			t.Fatalf("default label %d = %q want %q", i, got, wantLabels[i])
		}
	}
	edges := []float64{10, 17.5, 40}
	if k := WeightBandKey(edges, 1); k != "10_17p5" {
		t.Fatalf("decimal edge key = %q", k)
	}
	if l := WeightBandLabel(edges, 3); l != "40 kg and over" {
		t.Fatalf("top label = %q", l)
	}
	for _, bad := range [][]float64{{15}, {20, 15}, {0.5, 20}, {1, 2, 3, 4, 5, 6, 7, 8, 9}} {
		if ValidWeightBandEdgesKg(bad) {
			t.Fatalf("edges %v must be refused", bad)
		}
	}
	if !ValidWeightBandEdgesKg(nil) || !ValidWeightBandEdgesKg(edges) {
		t.Fatalf("nil and a rising list must be valid")
	}
}
