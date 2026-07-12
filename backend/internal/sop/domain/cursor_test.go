package domain

import (
	"testing"
	"time"
)

func TestSOPCursorRoundTripAndRejectsMalformed(t *testing.T) {
	want := SOPCursor{UpdatedAt: time.Date(2026, 7, 12, 0, 0, 0, 123, time.UTC), SOPID: "10000000-0000-4000-8000-000000000001"}
	encoded, err := EncodeSOPCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSOPCursor(encoded)
	if err != nil || got.SOPID != want.SOPID || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("round trip: cursor=%+v err=%v", got, err)
	}
	for _, value := range []string{"", "not-a-cursor"} {
		if _, err := DecodeSOPCursor(value); err == nil {
			t.Fatalf("DecodeSOPCursor(%q) succeeded", value)
		}
	}
}
