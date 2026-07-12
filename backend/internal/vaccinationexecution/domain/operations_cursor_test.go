package domain

import "testing"

func TestOperationsCursorRoundTrip(t *testing.T) {
	want := OperationsCursor{
		ParkID:   "30000000-0000-4000-8000-000000000001",
		ParkName: "CBE Park",
		ShedID:   "40000000-0000-4000-8000-000000000001",
		ShedName: "K1 Shed",
		Stage:    "K1",
	}
	encoded, err := EncodeOperationsCursor(want)
	if err != nil {
		t.Fatalf("EncodeOperationsCursor: %v", err)
	}
	got, err := DecodeOperationsCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeOperationsCursor: %v", err)
	}
	if got != want {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

func TestOperationsCursorRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "not-base64", "e30"} {
		if _, err := DecodeOperationsCursor(value); err == nil {
			t.Fatalf("DecodeOperationsCursor(%q) succeeded", value)
		}
	}
}
