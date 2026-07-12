package domain

import "testing"

func TestScanRosterCursorRoundTrip(t *testing.T) {
	want := ScanRosterCursor{
		GoatID:       "10000000-0000-4000-8000-000000000001",
		ObligationID: "20000000-0000-4000-8000-000000000001",
	}
	raw, err := EncodeScanRosterCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeScanRosterCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor=%#v want %#v", got, want)
	}
	if _, err := DecodeScanRosterCursor("not-a-cursor"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}
