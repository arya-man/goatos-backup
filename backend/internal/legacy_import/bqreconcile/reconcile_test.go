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
	tests := []struct {
		name             string
		startLifecycle   string
		startIdentity    string
		events           []Event
		wantLifecycle    string
		wantIdentity     string
		wantConflict     bool
		wantConflictCode string
	}{
		{
			name:           "shifting only is alive",
			startLifecycle: "inactive",
			startIdentity:  "clean",
			events:         []Event{{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"}},
			wantLifecycle:  "alive",
			wantIdentity:   "clean",
		},
		{
			name:           "abortion only is alive",
			startLifecycle: "inactive",
			startIdentity:  "clean",
			events:         []Event{{GoatID: "123456789012345", Event: "Abortion", Date: "2026-02-01"}},
			wantLifecycle:  "alive",
			wantIdentity:   "clean",
		},
		{
			name:           "later sale beats earlier shifting",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-03-01"},
			},
			wantLifecycle: "sold",
			wantIdentity:  "clean",
		},
		{
			name:           "sale followed only by shifting stays sold and needs review",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-03-01"},
			},
			wantLifecycle:    "sold",
			wantIdentity:     "needs_review",
			wantConflict:     true,
			wantConflictCode: "sale_then_later_nonpurchase_activity",
		},
		{
			name:           "sale followed only by abortion stays sold and needs review",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Abortion", Date: "2026-03-01"},
			},
			wantLifecycle:    "sold",
			wantIdentity:     "needs_review",
			wantConflict:     true,
			wantConflictCode: "sale_then_later_nonpurchase_activity",
		},
		{
			name:           "later purchase reopens a sold goat",
			startLifecycle: "sold",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Purchase", Date: "2026-03-01"},
			},
			wantLifecycle: "alive",
			wantIdentity:  "clean",
		},
		{
			name:           "later death beats earlier sale",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Death", Date: "2026-03-01"},
			},
			wantLifecycle: "dead",
			wantIdentity:  "clean",
		},
		{
			name:           "death followed by shifting stays dead and needs review",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Death", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-03-01"},
			},
			wantLifecycle:    "dead",
			wantIdentity:     "needs_review",
			wantConflict:     true,
			wantConflictCode: "death_then_later_activity",
		},
		{
			name:           "birth after death stays dead and needs review",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Death", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Birth", Date: "2026-03-01"},
			},
			wantLifecycle:    "dead",
			wantIdentity:     "needs_review",
			wantConflict:     true,
			wantConflictCode: "death_then_later_activity",
		},
		{
			name:           "abortion after death stays dead and needs review",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Death", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Abortion", Date: "2026-03-01"},
			},
			wantLifecycle:    "dead",
			wantIdentity:     "needs_review",
			wantConflict:     true,
			wantConflictCode: "death_then_later_activity",
		},
		{
			name:           "same day terminal tie breaker is stable",
			startLifecycle: "alive",
			startIdentity:  "clean",
			events: []Event{
				{GoatID: "123456789012345", Event: "Shifting", Date: "2026-02-01"},
				{GoatID: "123456789012345", Event: "Sale", Date: "2026-02-01"},
			},
			wantLifecycle: "sold",
			wantIdentity:  "clean",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := LocalGoat{
				GoatID:          "00000000-0000-4000-8000-000000000101",
				LifecycleStatus: tt.startLifecycle,
				IdentityState:   tt.startIdentity,
				Identifiers: []LocalIdentifier{
					{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
				},
			}
			summary, _ := Plan(tt.events, []LocalGoat{local}, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
			if summary.LifecycleUpdates[tt.wantLifecycle] != 1 {
				t.Fatalf("lifecycle updates=%#v want one %s update", summary.LifecycleUpdates, tt.wantLifecycle)
			}
			if got := summary.LifecycleConflicts; got != boolToInt(tt.wantConflict) {
				t.Fatalf("lifecycle conflicts=%d want %d", got, boolToInt(tt.wantConflict))
			}
			if got := summary.IdentityReviewUpdates; got != boolToInt(tt.wantIdentity == "needs_review") {
				t.Fatalf("identity review updates=%d want %d", got, boolToInt(tt.wantIdentity == "needs_review"))
			}
			_, patches := Plan(tt.events, []LocalGoat{local}, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
			if len(patches) != 1 {
				t.Fatalf("patches=%d want 1", len(patches))
			}
			if patches[0].AfterLifecycle != tt.wantLifecycle || patches[0].AfterIdentity != tt.wantIdentity {
				t.Fatalf("patch=%#v want lifecycle=%s identity=%s", patches[0], tt.wantLifecycle, tt.wantIdentity)
			}
			if tt.wantConflict {
				if patches[0].Conflict == nil {
					t.Fatalf("expected conflict patch: %#v", patches[0])
				}
				if patches[0].Conflict.Reason != tt.wantConflictCode {
					t.Fatalf("conflict reason=%q want %q", patches[0].Conflict.Reason, tt.wantConflictCode)
				}
			} else if patches[0].Conflict != nil {
				t.Fatalf("unexpected conflict patch: %#v", patches[0])
			}
		})
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
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
