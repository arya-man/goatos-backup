package bqreconcile

import (
	"encoding/json"
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

func TestConflictIdentifierPartsUsesPrimaryCompositeKey(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		wantType  string
		wantValue string
	}{
		{
			name:      "old tag first does not pick rfid value",
			key:       "old_tag:park:cpt:356|rfid:global:901007000504678",
			wantType:  "old_tag",
			wantValue: "356",
		},
		{
			name:      "rfid first stays rfid",
			key:       "rfid:global:901007000504678|old_tag:park:cpt:356",
			wantType:  "rfid",
			wantValue: "901007000504678",
		},
		{
			name:      "fallback external key",
			key:       "source:legacy:ABC123",
			wantType:  "external_system_id",
			wantValue: "ABC123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotValue := conflictIdentifierParts(tt.key)
			if gotType != tt.wantType || gotValue != tt.wantValue {
				t.Fatalf("conflictIdentifierParts(%q)=(%q,%q) want (%q,%q)", tt.key, gotType, gotValue, tt.wantType, tt.wantValue)
			}
		})
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

func TestPlanFillsMissingSexFromBQ(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Male",
		Breed:  "Sojat",
	}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sojat",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.SexUpdates != 1 || summary.AttributeConflicts != 0 {
		t.Fatalf("summary=%#v want one sex update and no attribute conflict", summary)
	}
	if len(patches) != 1 || patches[0].AfterSex != "male" || patches[0].AfterIdentity != "clean" {
		t.Fatalf("patches=%#v want sex fill to male without review", patches)
	}
}

func TestPlanFlagsGenderMismatchFromBQ(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Male",
		Breed:  "Sojat",
	}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sojat",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.GenderConflicts != 1 || summary.AttributeConflicts != 1 || summary.GenderCorrections != 0 || summary.SexUpdates != 0 || summary.IdentityReviewUpdates != 1 {
		t.Fatalf("summary=%#v want gender conflict and review update", summary)
	}
	if len(patches) != 1 || patches[0].AfterSex != "female" || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want sex unchanged and identity review", patches)
	}
	if got := patches[0].AttrConflicts[0].Reason; got != "gender_mismatch" {
		t.Fatalf("conflict reason=%q want gender_mismatch", got)
	}
}

func TestPlanTreatsAnantapurSheepAsCosmeticBreedDrift(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Female",
		Breed:  "Anantapur",
	}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Anantapur Sheep",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.BreedCosmeticDrifts != 1 || summary.BreedCorrections != 1 || summary.AttributeCorrections != 1 || summary.BreedConflicts != 0 {
		t.Fatalf("summary=%#v want cosmetic breed label normalization", summary)
	}
	if len(patches) != 1 || patches[0].AfterBreed != "Anantapur" || patches[0].AfterIdentity != "clean" {
		t.Fatalf("patches=%#v want BQ breed label normalized without review", patches)
	}
	if got := patches[0].AttributeChanges[0].Reason; got != "cosmetic_breed_label_normalized_from_bq" {
		t.Fatalf("correction reason=%q want cosmetic_breed_label_normalized_from_bq", got)
	}
}

func TestPlanFlagsRealBreedMismatchFromBQ(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Female",
		Breed:  "Beetal",
	}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sirohi",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.BreedConflicts != 1 || summary.AttributeConflicts != 1 || summary.BreedCorrections != 0 || summary.AttributeCorrections != 0 || summary.IdentityReviewUpdates != 1 {
		t.Fatalf("summary=%#v want breed conflict and review update", summary)
	}
	if len(patches) != 1 || patches[0].AfterBreed != "Sirohi" || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want breed unchanged and identity review", patches)
	}
	if got := patches[0].AttrConflicts[0].Reason; got != "breed_mismatch" {
		t.Fatalf("conflict reason=%q want breed_mismatch", got)
	}
}

func TestPlanFlagsBQGenderSelfConflict(t *testing.T) {
	events := []Event{
		{
			GoatID: "123456789012345",
			Event:  "Shifting",
			Date:   "2026-02-03",
			Gender: "Male",
			Breed:  "Sojat",
		},
		{
			GoatID: "123456789012345",
			Event:  "Shifting",
			Date:   "2026-02-04",
			Gender: "Female",
			Breed:  "Sojat",
		},
	}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sojat",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.GenderConflicts != 1 || summary.AttributeConflicts != 1 || summary.SexUpdates != 0 || summary.IdentityReviewUpdates != 1 {
		t.Fatalf("summary=%#v want BQ gender self-conflict", summary)
	}
	if len(patches) != 1 || patches[0].AfterSex != "female" || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want sex unchanged and review", patches)
	}
	if got := patches[0].AttrConflicts[0].Reason; got != "bq_gender_self_conflict" {
		t.Fatalf("conflict reason=%q want bq_gender_self_conflict", got)
	}
}

func TestPlanFlagsBQGenderConflictAcrossMatchedIdentifiers(t *testing.T) {
	events := []Event{
		{
			GoatID: "123456789012345",
			Event:  "Shifting",
			Date:   "2026-02-03",
			Gender: "Male",
			Breed:  "Sojat",
		},
		{
			GoatID: "765",
			Farm:   "CPT",
			Event:  "Shifting",
			Date:   "2026-02-04",
			Gender: "Female",
			Breed:  "Sojat",
		},
	}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sojat",
		Sex:             "male",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
			{IdentifierType: "old_tag", NormalizedValue: "765", ScopeKey: "park:cpt"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.GenderConflicts != 1 || summary.AttributeConflicts != 1 || summary.SexUpdates != 0 || summary.IdentityReviewUpdates != 1 {
		t.Fatalf("summary=%#v want cross-identifier BQ gender conflict", summary)
	}
	if len(patches) != 1 || patches[0].AfterSex != "male" || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want sex unchanged and review", patches)
	}
	if got := patches[0].AttrConflicts[0].Reason; got != "bq_gender_self_conflict" {
		t.Fatalf("conflict reason=%q want bq_gender_self_conflict", got)
	}
	if got := patches[0].AttrConflicts[0].IdentifierKind; got != "matched_identifiers" {
		t.Fatalf("identifier kind=%q want matched_identifiers", got)
	}
}

func TestPlanFlagsBQBreedConflictAcrossMatchedIdentifiers(t *testing.T) {
	events := []Event{
		{
			GoatID: "123456789012345",
			Event:  "Shifting",
			Date:   "2026-02-03",
			Gender: "Female",
			Breed:  "Sirohi",
		},
		{
			GoatID: "765",
			Farm:   "CPT",
			Event:  "Shifting",
			Date:   "2026-02-04",
			Gender: "Female",
			Breed:  "Beetal",
		},
	}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sirohi",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "clean",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
			{IdentifierType: "old_tag", NormalizedValue: "765", ScopeKey: "park:cpt"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.BreedConflicts != 1 || summary.AttributeConflicts != 1 || summary.BreedCorrections != 0 || summary.IdentityReviewUpdates != 1 {
		t.Fatalf("summary=%#v want cross-identifier BQ breed conflict", summary)
	}
	if len(patches) != 1 || patches[0].AfterBreed != "Sirohi" || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want breed unchanged and review", patches)
	}
	if got := patches[0].AttrConflicts[0].Reason; got != "bq_breed_self_conflict" {
		t.Fatalf("conflict reason=%q want bq_breed_self_conflict", got)
	}
}

func TestPlanCreatesMissingAttributeConflictForAlreadyReviewGoat(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Male",
		Breed:  "Sojat",
	}}
	goats := []LocalGoat{{
		GoatID:          "00000000-0000-4000-8000-000000000101",
		Breed:           "Sojat",
		Sex:             "female",
		LifecycleStatus: "alive",
		IdentityState:   "needs_review",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.AttributeConflicts != 1 || summary.PatchesPlanned != 1 || summary.IdentityReviewUpdates != 0 {
		t.Fatalf("summary=%#v want missing attribute conflict patch without identity update", summary)
	}
	if len(patches) != 1 || patches[0].AfterIdentity != "needs_review" || len(patches[0].AttrConflicts) != 1 {
		t.Fatalf("patches=%#v want one conflict-only patch", patches)
	}
}

func TestPlanSkipsExistingAttributeConflictForAlreadyReviewGoat(t *testing.T) {
	events := []Event{{
		GoatID: "123456789012345",
		Event:  "Shifting",
		Date:   "2026-02-03",
		Gender: "Male",
		Breed:  "Sojat",
	}}
	goats := []LocalGoat{{
		GoatID:                         "00000000-0000-4000-8000-000000000101",
		Breed:                          "Sojat",
		Sex:                            "female",
		LifecycleStatus:                "alive",
		IdentityState:                  "needs_review",
		HasOpenBQAttributeConflict:     true,
		OpenBQAttributeIdentifierType:  "rfid",
		OpenBQAttributeIdentifierValue: "123456789012345",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "rfid", NormalizedValue: "123456789012345", ScopeKey: "global:rfid"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.AttributeConflicts != 1 || summary.PatchesPlanned != 0 || summary.IdentityReviewUpdates != 0 {
		t.Fatalf("summary=%#v want current attribute conflict counted without patch", summary)
	}
	if len(patches) != 0 {
		t.Fatalf("patches=%#v want no patch when conflict row already exists", patches)
	}
}

func TestPlanRefreshesStaleOldTagAttributeConflictMetadata(t *testing.T) {
	events := []Event{{
		GoatID: "765",
		Event:  "Shifting",
		Farm:   "CPT",
		Date:   "2026-02-03",
		Gender: "Male",
		Breed:  "Sojat",
	}}
	goats := []LocalGoat{{
		GoatID:                         "00000000-0000-4000-8000-000000000101",
		Breed:                          "Sojat",
		Sex:                            "female",
		LifecycleStatus:                "alive",
		IdentityState:                  "needs_review",
		HasOpenBQAttributeConflict:     true,
		OpenBQAttributeIdentifierType:  "rfid",
		OpenBQAttributeIdentifierValue: "",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "old_tag", NormalizedValue: "765", ScopeKey: "park:cpt"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.AttributeConflicts != 1 || summary.PatchesPlanned != 1 || summary.IdentityReviewUpdates != 0 {
		t.Fatalf("summary=%#v want stale existing attribute conflict metadata patch", summary)
	}
	if len(patches) != 1 || !patches[0].RefreshAttributeConflict || patches[0].AfterIdentity != "needs_review" {
		t.Fatalf("patches=%#v want attribute conflict metadata refresh only", patches)
	}
	gotType, gotValue := plannedAttributeConflictIdentifier(patches[0].AttrConflicts, patches[0].MatchedKeys)
	if gotType != "old_tag" || gotValue != "765" {
		t.Fatalf("planned identifier=%s/%s want old_tag/765", gotType, gotValue)
	}
}

func TestPlanRefreshesStaleOldTagLifecycleConflictMetadata(t *testing.T) {
	events := []Event{
		{GoatID: "765", Event: "Sale", Farm: "CPT", Date: "2026-02-03"},
		{GoatID: "765", Event: "Shifting", Farm: "CPT", Date: "2026-02-04"},
	}
	goats := []LocalGoat{{
		GoatID:                         "00000000-0000-4000-8000-000000000101",
		Breed:                          "Sojat",
		Sex:                            "male",
		LifecycleStatus:                "sold",
		IdentityState:                  "needs_review",
		HasOpenBQLifecycleConflict:     true,
		OpenBQLifecycleIdentifierType:  "rfid",
		OpenBQLifecycleIdentifierValue: "",
		Identifiers: []LocalIdentifier{
			{IdentifierType: "old_tag", NormalizedValue: "765", ScopeKey: "park:cpt"},
		},
	}}

	summary, patches := Plan(events, goats, LocationLookup{ShedsByAlias: map[string]LocationTarget{}, ParksByCode: map[string]string{}})
	if summary.LifecycleConflicts != 1 || summary.PatchesPlanned != 1 || summary.IdentityReviewUpdates != 0 {
		t.Fatalf("summary=%#v want stale existing lifecycle conflict metadata patch", summary)
	}
	if len(patches) != 1 || !patches[0].RefreshLifecycleConflict || patches[0].AfterLifecycle != "sold" {
		t.Fatalf("patches=%#v want lifecycle conflict metadata refresh only", patches)
	}
	gotType, gotValue := plannedLifecycleConflictIdentifier(patches[0].Conflict, patches[0].MatchedKeys)
	if gotType != "old_tag" || gotValue != "765" {
		t.Fatalf("planned identifier=%s/%s want old_tag/765", gotType, gotValue)
	}
}

func TestPlanImportBlankGenderFillFromBQ(t *testing.T) {
	payload := []byte(`{"rfid":"123456789012345","sex":"","processing_reasons":["blank_gender"]}`)
	patch, ok, err := planImportBlankGenderFill("00000000-0000-4000-8000-000000000201", "needs_review", payload, map[string]attributeValue{
		"123456789012345": {Value: "male", Date: "2026-06-13"},
	})
	if err != nil {
		t.Fatalf("planImportBlankGenderFill err=%v", err)
	}
	if !ok {
		t.Fatal("blank gender row was not planned")
	}
	if patch.AfterState != "pending" || patch.BQSex != "male" || patch.BQEventDate != "2026-06-13" {
		t.Fatalf("patch=%#v want pending male fill", patch)
	}
	var after map[string]any
	if err := json.Unmarshal(patch.AfterPayload, &after); err != nil {
		t.Fatalf("decode after payload: %v", err)
	}
	if got := after["sex"]; got != "male" {
		t.Fatalf("after sex=%#v want male", got)
	}
	if reasons := jsonStringSlice(after["processing_reasons"]); len(reasons) != 0 {
		t.Fatalf("after reasons=%#v want empty", reasons)
	}
}

func TestPlanImportBlankGenderFillRequiresDeterministicRFIDGender(t *testing.T) {
	payload := []byte(`{"rfid":"123456789012345","sex":"","processing_reasons":["blank_gender"]}`)
	sexByRFID := deterministicSexByRFID([]Event{
		{GoatID: "123456789012345", Event: "Shifting", Date: "2026-06-12", Gender: "Male"},
		{GoatID: "123456789012345", Event: "Shifting", Date: "2026-06-13", Gender: "Female"},
	})
	if _, ok := sexByRFID["123456789012345"]; ok {
		t.Fatalf("conflicting BQ genders must not produce a deterministic fill: %#v", sexByRFID)
	}
	_, ok, err := planImportBlankGenderFill("00000000-0000-4000-8000-000000000201", "needs_review", payload, sexByRFID)
	if err != nil {
		t.Fatalf("planImportBlankGenderFill err=%v", err)
	}
	if ok {
		t.Fatal("blank gender row with contradictory BQ RFID gender must not be filled/requeued")
	}
}

func TestPlanImportBlankGenderFillSkipsRowsWithOtherReasons(t *testing.T) {
	payload := []byte(`{"rfid":"123456789012345","sex":"","processing_reasons":["blank_gender","duplicate_old_tag_same_scope"]}`)
	_, ok, err := planImportBlankGenderFill("00000000-0000-4000-8000-000000000201", "needs_review", payload, map[string]attributeValue{
		"123456789012345": {Value: "male", Date: "2026-06-13"},
	})
	if err != nil {
		t.Fatalf("planImportBlankGenderFill err=%v", err)
	}
	if ok {
		t.Fatal("row with multiple review reasons must not be filled/requeued")
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
	arrayEvents, err := ReadEvents(strings.NewReader(`[{"goat_id":"1","event":"Shifting","date":"2026-06-01"}]`))
	if err != nil || len(arrayEvents) != 1 {
		t.Fatalf("array events=%#v err=%v", arrayEvents, err)
	}
	jsonlEvents, err := ReadEvents(strings.NewReader(`{"goat_id":"1","event":"Shifting","date":"2026-06-01"}` + "\n" + `{"goat_id":"2","event":"Sale","date":"2026-06-02"}`))
	if err != nil || len(jsonlEvents) != 2 {
		t.Fatalf("jsonl events=%#v err=%v", jsonlEvents, err)
	}
}

func TestReadEventsRejectsNonISODate(t *testing.T) {
	_, err := ReadEvents(strings.NewReader(`[{"goat_id":"1","event":"Shifting","date":"06/01/2026"}]`))
	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("expected YYYY-MM-DD validation error, got %v", err)
	}
}
