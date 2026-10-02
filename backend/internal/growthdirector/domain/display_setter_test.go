package domain

import "testing"

func TestDisplaySetterHidesSeedActorsAndKeepsPeople(t *testing.T) {
	for raw, want := range map[string]string{
		"maintainer": "",
		"Maintainer": "",
		"migration:000363_growth_sale_price_assumptions": "",
		"system":        "",
		"seed-stg":      "",
		"":              "",
		"  Ravi Kumar ": "Ravi Kumar",
		"Manohar":       "Manohar",
	} {
		if got := DisplaySetter(raw); got != want {
			t.Errorf("DisplaySetter(%q) = %q, want %q", raw, got, want)
		}
	}
}
