package postgres

import (
	"reflect"
	"testing"
)

// Round cards name each pen with the canonical composer (found on the phone 2026-09-30, where a
// hand-rolled ' - ' join read "Castro - 1 · Castro - 2" beside task screens saying "Castro 1").
func TestRoundCardPenLabelsUseTheCanonicalComposer(t *testing.T) {
	got := roundCardPenLabels(
		[]string{"Castro", "Castro", "Godel 1", "Yashoda 2", "", "Mandela 1"},
		[]string{"1", "2", "Part 3", "", "4", "whole"},
	)
	want := []string{"Castro 1", "Castro 2", "Godel 1 - Part 3", "Yashoda 2", "Mandela 1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pen labels = %q, want %q", got, want)
	}
}
