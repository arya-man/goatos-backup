package domain

import (
	"testing"
	"time"
)

func TestRecordedCompletionCursorRoundTrip(t *testing.T) {
	cursor := RecordedCompletionCursor{
		AdministeredAt: time.Date(2026, 7, 12, 12, 30, 0, 0, time.UTC),
		CompletionID:   "00000000-0000-4000-8000-000000000001",
	}
	encoded, err := EncodeRecordedCompletionCursor(cursor)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeRecordedCompletionCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != cursor {
		t.Fatalf("round-trip mismatch: got %+v want %+v", decoded, cursor)
	}
}

func TestRecordedCompletionCursorRejectsInvalidPayload(t *testing.T) {
	if _, err := DecodeRecordedCompletionCursor("not-a-cursor"); err != ErrInvalidCursor {
		t.Fatalf("want ErrInvalidCursor, got %v", err)
	}
}
