package domain

import (
	"testing"
	"time"
)

func TestCalendarCursorRoundTrip(t *testing.T) {
	in := CalendarCursor{DueAt: time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC), EventID: "obligation:86000000-0000-4000-8000-000000001001"}
	encoded, err := EncodeCalendarCursor(in)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	out, err := DecodeCalendarCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if !out.DueAt.Equal(in.DueAt) || out.EventID != in.EventID {
		t.Fatalf("cursor round trip = %#v, want %#v", out, in)
	}
}

func TestValidateEventID(t *testing.T) {
	valid := []string{
		"obligation:86000000-0000-4000-8000-000000001001",
		"calendar:86000000-0000-4000-8000-000000001002",
		"batch:86000000-0000-4000-8000-000000001003:rule:86000000-0000-4000-8000-000000001004:shed:86000000-0000-4000-8000-000000001005",
		"history:2025-12-09:86000000-0000-4000-8000-000000001006:86000000-0000-4000-8000-000000001007:86000000-0000-4000-8000-000000001008",
	}
	for _, id := range valid {
		if err := ValidateEventID(id); err != nil {
			t.Fatalf("ValidateEventID(%q) unexpected err: %v", id, err)
		}
	}
	invalid := []string{"", "reminder:86000000-0000-4000-8000-000000001001", "batch:bad"}
	for _, id := range invalid {
		if err := ValidateEventID(id); err == nil {
			t.Fatalf("ValidateEventID(%q) succeeded, want error", id)
		}
	}
}
