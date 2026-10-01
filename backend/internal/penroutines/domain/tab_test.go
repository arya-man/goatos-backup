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
		{Label: "Fumigation", ModuleKey: "vaccination", IconKey: "fumigation"},
		{Label: "Fumigation", ModuleKey: "pc_care", IconKey: "rocket"},
		{Label: "Fumigation", ModuleKey: "pc_care", IconKey: "fumigation", Filters: []string{"breed"}},
	}
	for i, tab := range bad {
		if err := ValidateTab(NormalizeTab(tab)); !errors.Is(err, ErrInvalidTab) {
			t.Errorf("case %d: err = %v, want ErrInvalidTab", i, err)
		}
	}
}

// TestPhoneTaskSOPParsesIntoOneRoutinePerPark pins the phone-task document: its module comes from
// the SOP code, its tab label is the SOP name, each park becomes one routine definition, and a
// document outside a module SOP page, with no park, or with an unknown key is refused.
func TestPhoneTaskSOPParsesIntoOneRoutinePerPark(t *testing.T) {
	doc := map[string]any{"phone_task": map[string]any{
		"tab":          map[string]any{"icon": "fumigation", "filters": []any{"pen", "status"}},
		"instruction":  "Spray every pen; film before and after.",
		"scope_kind":   "all_pens",
		"cadence_kind": "every_n_days", "interval_days": 7, "start_date": "2026-10-01",
		"review_kind": "verifier",
		"evidence":    map[string]any{"questions": []any{}, "photo": map[string]any{"min": 0, "max": 0}, "video": map[string]any{"min": 2, "max": 2}, "presence": "off"},
		"parks": []any{
			map[string]any{"park_id": "p-cbe", "assignee_user_id": "u-1", "pens": []any{}},
			map[string]any{"park_id": "p-cpt", "assignee_user_id": "u-2", "pens": []any{}},
		},
	}}
	parsed, err := ParsePhoneTask("pc_care.wash", "Pen wash", doc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if PhoneModuleForSOPCode("pc_care.wash") != "pc_care" || PhoneTabKeyForSOPCode("pc_care.wash") != "pc_care_wash" {
		t.Fatal("module / key mapping drifted")
	}
	def := parsed.DefinitionFor("Pen wash", parsed.Parks[1], "2026-10-01")
	if def.ParkID != "p-cpt" || def.AssigneeUserID != "u-2" || def.Name != "Pen wash" || def.IntervalDays != 7 || def.Evidence.Video.Min != 2 || def.Pens != nil {
		t.Fatalf("definition = %+v", def)
	}
	if _, err := ParsePhoneTask("general.wash", "Pen wash", doc); !errors.Is(err, ErrInvalidPhoneTask) {
		t.Fatalf("a non-module SOP err = %v", err)
	}
	noParks := map[string]any{"phone_task": map[string]any{"tab": map[string]any{"icon": "fumigation"}, "scope_kind": "all_pens", "cadence_kind": "daily", "review_kind": "none",
		"evidence": map[string]any{"questions": []any{}, "photo": map[string]any{"min": 0, "max": 0}, "video": map[string]any{"min": 1, "max": 1}, "presence": "off"}, "parks": []any{}}}
	if _, err := ParsePhoneTask("pc_care.wash", "Pen wash", noParks); !errors.Is(err, ErrInvalidPhoneTask) {
		t.Fatalf("no parks err = %v", err)
	}
	unknown := map[string]any{"phone_task": map[string]any{"tab": map[string]any{"icon": "fumigation"}, "colour": "red"}}
	if _, err := ParsePhoneTask("pc_care.wash", "Pen wash", unknown); !errors.Is(err, ErrInvalidPhoneTask) {
		t.Fatalf("unknown key err = %v", err)
	}
	if _, err := ParsePhoneTask("pc_care.wash", "A name far too long for one bar slot", doc); !errors.Is(err, ErrInvalidPhoneTask) {
		t.Fatalf("long name err = %v", err)
	}
}
