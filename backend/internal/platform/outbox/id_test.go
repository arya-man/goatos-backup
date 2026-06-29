package outbox

import (
	"strings"
	"testing"
)

func TestDeterministicUUIDIsRFC4122(t *testing.T) {
	got := DeterministicUUID("vaccination.completed:tenant:obligation")
	if len(got) != 36 {
		t.Fatalf("uuid length = %d, want 36: %q", len(got), got)
	}
	if got[8] != '-' || got[13] != '-' || got[18] != '-' || got[23] != '-' {
		t.Fatalf("uuid separators malformed: %q", got)
	}
	if got[14] != '3' {
		t.Fatalf("uuid version nibble = %q, want 3: %q", got[14], got)
	}
	if !strings.ContainsRune("89ab", rune(got[19])) {
		t.Fatalf("uuid variant nibble = %q, want RFC4122 variant: %q", got[19], got)
	}
	if again := DeterministicUUID("vaccination.completed:tenant:obligation"); again != got {
		t.Fatalf("uuid not deterministic: %q then %q", got, again)
	}
}
