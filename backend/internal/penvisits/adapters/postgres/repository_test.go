package postgres

import "testing"

func TestDecodeCursorRejectsInvalidTaskID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "valid", raw: "2026-09-07|11111111-1111-4111-8111-111111111111", ok: true},
		{name: "invalid date", raw: "bad-date|11111111-1111-4111-8111-111111111111", ok: false},
		{name: "missing id", raw: "2026-09-07|", ok: false},
		{name: "invalid id", raw: "2026-09-07|not-a-uuid", ok: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, ok := decodeCursor(tt.raw)
			if ok != tt.ok {
				t.Fatalf("decodeCursor(%q) ok = %v, want %v", tt.raw, ok, tt.ok)
			}
		})
	}
}
