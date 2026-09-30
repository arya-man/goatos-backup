package domain

import (
	"errors"
	"testing"
)

// TestTabKeyIsARouteSafeSlugOfTheLabel pins the key rule: lowercase letters, digits and
// underscores, starting with a letter, stable for the label it was made from.
func TestTabKeyIsARouteSafeSlugOfTheLabel(t *testing.T) {
	cases := map[string]string{
		"Fumigation":         "fumigation",
		"  Pen wash  2 ":     "pen_wash_2",
		"Water-trough check": "water_trough_check",
		"2nd round":          "tab_2nd_round",
		"फ्यूमिगेशन":         "tab",
		"A":                  "tab",
	}
	for label, want := range cases {
		if got := TabKeyBase(label); got != want {
			t.Errorf("TabKeyBase(%q) = %q, want %q", label, got, want)
		}
	}
}

// TestTabValidationRefusesAnythingThePhoneCannotRender: a module, icon or filter outside the
// closed vocabularies would reach the bar as a blank, so it is refused at authoring.
func TestTabValidationRefusesAnythingThePhoneCannotRender(t *testing.T) {
	good := NormalizeTab(Tab{Label: " Fumigation ", ModuleKey: "pc_care", IconKey: "fumigation", Filters: []string{"pen", "status", "pen"}})
	if err := ValidateTab(good); err != nil {
		t.Fatalf("good tab refused: %v", err)
	}
	if good.Label != "Fumigation" || len(good.Filters) != 2 || good.Filters[0] != TabFilterStatus || good.Filters[1] != TabFilterPen {
		t.Fatalf("normalize = %+v", good)
	}
	bad := []Tab{
		{Label: "", ModuleKey: "pc_care", IconKey: "fumigation"},
		{Label: "A label far too long for one bottom bar slot", ModuleKey: "pc_care", IconKey: "fumigation"},
		{Label: "Fumigation", ModuleKey: "sales", IconKey: "fumigation"},
		{Label: "Fumigation", ModuleKey: "pc_care", IconKey: "rocket"},
		{Label: "Fumigation", ModuleKey: "pc_care", IconKey: "fumigation", Filters: []string{"breed"}},
	}
	for i, tab := range bad {
		if err := ValidateTab(NormalizeTab(tab)); !errors.Is(err, ErrInvalidTab) {
			t.Errorf("case %d: err = %v, want ErrInvalidTab", i, err)
		}
	}
}
