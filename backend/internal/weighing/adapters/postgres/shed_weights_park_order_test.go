package postgres

import (
	"regexp"
	"testing"
)

// The park vocabulary drives every Weights chart's cluster order. A configured
// display_order must not put CPT before CBE while Feed uses code order.
func TestShedWeightsParkCodePrecedesConfiguredDisplayOrder(t *testing.T) {
	src := readSource(t, "shed_weights.go")
	query := regexp.MustCompile("(?s)parkRows, err := r.pool.Query\\(ctx, `([^`]+)`").FindStringSubmatch(src)
	if len(query) != 2 {
		t.Fatal("could not locate the production park-vocabulary query")
	}
	if !regexp.MustCompile(`(?s)ORDER BY\s+COALESCE\(NULLIF\(location_code, ''\), name, ''\),\s*display_order,\s*name,\s*location_id`).MatchString(query[1]) {
		t.Fatal("park code must precede display_order: CBE display_order=2 and CPT display_order=1 must still render CBE first")
	}
}
