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

func TestPlanUsesLatestLifecycleEventPerIdentifier(t *testing.T) {
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
		{
			name:   "shifting only is alive",
			events: []Event{{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"}},
			want:   "alive",
		},
		{
			name: "later sale beats earlier shifting",
			events: []Event{
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-03-01"},
			},
			want: "sold",
		},
		{
			name: "later shifting beats earlier sale",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-03-01"},
			},
			want: "alive",
		},
		{
			name: "later purchase beats earlier sale",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Purchase", Date: "2026-03-01"},
			},
			want: "alive",
		},
		{
			name: "later death beats earlier sale",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Death", Date: "2026-03-01"},
			},
			want: "dead",
		},
		{
			name: "same day terminal tie breaker is stable",
			events: []Event{
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
			},
			want: "sold",
		},
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

func TestPlanRFIDEvidenceWinsAndFlagsOldTagLifecycleConflict(t *testing.T) {
	events := []Event{
		{GoatID: "123456789012345", Farm: "CBE", Event: "Shifting", Date: "2026-02-03"},
		{GoatID: "765", Farm: "CPT", Event: "Sale", Date: "2025-10-16"},
	}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		LifecycleStatus: "sold",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
			{IdentifierType: "old_tag", NormalizedValue: "765", ScopeKey: "park:CPT"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.LifecycleUpdates["alive"] != 1 {
		t.Fatalf("lifecycle updates=%#v want alive update", summary.LifecycleUpdates)
	}
	if summary.IdentityReviewUpdates != 1 || summary.LifecycleConflicts != 1 {
		t.Fatalf("summary conflict counts wrong: %#v", summary)
	}
	if len(patches) != 1 {
		t.Fatalf("patches=%d want 1", len(patches))
	}
	if patches[0].AfterLifecycle != "alive" || patches[0].AfterIdentity != "needs_review" || patches[0].Conflict == nil {
		t.Fatalf("unexpected conflict patch: %#v", patches[0])
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
