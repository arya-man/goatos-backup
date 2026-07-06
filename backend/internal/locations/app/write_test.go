package app

import "testing"

func TestNormalizeLocationTimezoneDefaultsToIST(t *testing.T) {
	got, err := normalizeLocationTimezone("")
	if err != nil {
		t.Fatalf("normalizeLocationTimezone empty: %v", err)
	}
	if got != "Asia/Kolkata" {
		t.Fatalf("timezone=%s, want Asia/Kolkata", got)
	}
}

func TestNormalizeLocationTimezoneRejectsNonIST(t *testing.T) {
	if _, err := normalizeLocationTimezone("UTC"); err == nil {
		t.Fatal("normalizeLocationTimezone should reject non-IST values")
	}
}

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
