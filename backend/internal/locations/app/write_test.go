package app

import "testing"

func TestShedsDBSourceEvidenceIsAllowedForLocationProfiles(t *testing.T) {
	if !allowedAliasSourceContexts["sheds_db"] {
		t.Fatal("sheds_db source_context must be allowed for reviewed Sheds DB aliases")
	}

	sourceRef := "Sheds DB.xlsx#DB!A:H"
	notes := "Reviewed Sheds DB capacity evidence."
	body := &createCapacityBody{
		CapacityKind:  "goat_occupancy",
		CapacityValue: 50,
		EffectiveFrom: "2026-06-30",
		Source:        "sheds_db",
		SourceRef:     &sourceRef,
		Notes:         &notes,
	}
	if err := validateCapacityBody(body); err != nil {
		t.Fatalf("validateCapacityBody rejected sheds_db source evidence: %v", err)
	}
}
