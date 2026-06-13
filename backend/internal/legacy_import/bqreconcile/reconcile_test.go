package bqreconcile

import (
	"strings"
	"testing"
)

func TestCanonicalIdentifierNormalizesExcelValues(t *testing.T) {
	tests := map[string]string{
		"1.23456789012345E14": "123456789012345",
		"1001.0":              "1001",
		" SA2328252 ":         "SA2328252",
	}
	for input, want := range tests {
		if got := CanonicalIdentifier(input); got != want {
			t.Fatalf("CanonicalIdentifier(%q)=%q want %q", input, got, want)
		}
	}
}

func TestPlanMatchesRFIDAndScopedOldTag(t *testing.T) {
	events := []Event{
		{GoatID: "123456789012345", Farm: "CBE", Event: "Shifting", Date: "2026-06-08", DstShed: "gandhi 2"},
		{GoatID: "765", Farm: "CPT", Event: "Sale", Date: "2026-06-01", DstShed: "Yashoda 1"},
	}
	goats := []LocalGoat{
		{
			GoatID:          "00000000-0000-4000-8000-000000000101",
			LifecycleStatus: "inactive",
			Identifiers: []LocalIdentifier{
				{IdentifierType: "rfid", NormalizedValue: "1.23456789012345E14", ScopeKey: "global:rfid"},
			},
		},
		{
			GoatID:          "00000000-0000-4000-8000-000000000102",
			LifecycleStatus: "alive",
			Identifiers: []LocalIdentifier{
				{IdentifierType: "old_tag", NormalizedValue: "765.0", ScopeKey: "park:CPT"},
			},
		},
	}
	locations := LocationLookup{
		ShedsByAlias: map[string]LocationTarget{
			"CBE:GANDHI 2":  {LocationID: "00000000-0000-4000-8000-000000000201", ParentLocationID: "00000000-0000-4000-8000-000000000301"},
			"CPT:YASHODA 1": {LocationID: "00000000-0000-4000-8000-000000000202", ParentLocationID: "00000000-0000-4000-8000-000000000302"},
		},
		ParksByCode: map[string]string{"CBE": "00000000-0000-4000-8000-000000000301", "CPT": "00000000-0000-4000-8000-000000000302"},
	}

	summary, patches := Plan(events, goats, locations)
	if summary.MatchedGoats != 2 {
		t.Fatalf("matched goats=%d want 2", summary.MatchedGoats)
	}
	if summary.LifecycleUpdates["alive"] != 1 || summary.LifecycleUpdates["sold"] != 1 {
		t.Fatalf("unexpected lifecycle updates: %#v", summary.LifecycleUpdates)
	}
	if summary.LocationShedUpdates != 2 {
		t.Fatalf("shed updates=%d want 2", summary.LocationShedUpdates)
	}
	if len(patches) != 2 {
		t.Fatalf("patches=%d want 2", len(patches))
	}
}

func TestPlanUsesSeparateLatestLocationExport(t *testing.T) {
	events := []Event{{GoatID: "123456789012345", Farm: "CBE", Event: "Shifting", Date: "2026-06-01"}}
	locationEvents := []Event{{GoatID: "123456789012345", Farm: "CBE", Event: "Shifting", Date: "2026-06-08", CurrentShed: "gandhi 2"}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		LifecycleStatus: "inactive",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}
	locations := LocationLookup{
		ShedsByAlias: map[string]LocationTarget{
			"CBE:GANDHI 2": {LocationID: "00000000-0000-4000-8000-000000000201", ParentLocationID: "00000000-0000-4000-8000-000000000301"},
		},
		ParksByCode: map[string]string{"CBE": "00000000-0000-4000-8000-000000000301"},
	}

	summary, _ := PlanWithLocations(events, locationEvents, goats, locations)
	if summary.LifecycleUpdates["alive"] != 1 {
		t.Fatalf("lifecycle updates=%#v want alive update", summary.LifecycleUpdates)
	}
	if summary.LocationShedUpdates != 1 {
		t.Fatalf("shed updates=%d want 1", summary.LocationShedUpdates)
	}
}

func TestPlanLifecyclePrecedence(t *testing.T) {
	goat := LocalGoat{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		LifecycleStatus: "alive",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}
	tests := []struct {
		name   string
		events []Event
		want   string
	}{
		{name: "shifting only is alive", events: []Event{{GoatID: "123456789012345", Event: "Shifting"}}, want: "alive"},
		{name: "sale beats shifting", events: []Event{{GoatID: "123456789012345", Event: "Shifting"}, {GoatID: "123456789012345", Event: "Sale"}}, want: "sold"},
		{name: "death beats sale", events: []Event{{GoatID: "123456789012345", Event: "Sale"}, {GoatID: "123456789012345", Event: "Death"}}, want: "dead"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := goat
			if tt.want == "alive" {
				local.LifecycleStatus = "inactive"
			}
			summary, _ := Plan(tt.events, []LocalGoat{local}, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
			if summary.LifecycleUpdates[tt.want] != 1 {
				t.Fatalf("lifecycle updates=%#v want one %s update", summary.LifecycleUpdates, tt.want)
			}
		})
	}
}

func TestPlanSkipsAmbiguousIdentifierMatches(t *testing.T) {
	events := []Event{{GoatID: "123456789012345", Event: "Shifting"}}
	goats := []LocalGoat{
		{GoatID: "00000000-0000-4000-8000-000000000101", LifecycleStatus: "inactive", Identifiers: []LocalIdentifier{{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"}}},
		{GoatID: "00000000-0000-4000-8000-000000000102", LifecycleStatus: "inactive", Identifiers: []LocalIdentifier{{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"}}},
	}
	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.AmbiguousBQKeys != 1 || summary.MatchedGoats != 0 || len(patches) != 0 {
		t.Fatalf("ambiguous match not skipped: summary=%#v patches=%#v", summary, patches)
	}
}

func TestReadEventsSupportsArrayAndJSONL(t *testing.T) {
	arrayEvents, err := ReadEvents(strings.NewReader(`[{"goat_id":"1","event":"Shifting"}]`))
	if err != nil || len(arrayEvents) != 1 {
		t.Fatalf("array events=%#v err=%v", arrayEvents, err)
	}
	jsonlEvents, err := ReadEvents(strings.NewReader(`{"goat_id":"1","event":"Shifting"}` + "\n" + `{"goat_id":"2","event":"Sale"}`))
	if err != nil || len(jsonlEvents) != 2 {
		t.Fatalf("jsonl events=%#v err=%v", jsonlEvents, err)
	}
}
