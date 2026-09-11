package ports

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// A cursor id that is not a uuid is refused as an invalid cursor before any SQL cast can turn
// it into a 500 (live E2E 2026-09-11: `weighing|weighing_work_item|not-a-uuid` and a
// four-part cursor both produced internal_error with SQLSTATE 22P02 in the log).
func TestCheckUUIDSourceID(t *testing.T) {
	if err := CheckUUIDSourceID(""); err != nil {
		t.Fatalf("empty is the start of the keyset: %v", err)
	}
	if err := CheckUUIDSourceID("2334cda1-8d8a-4bdd-86a9-32eaf70f9d9a"); err != nil {
		t.Fatalf("a uuid passes: %v", err)
	}
	for _, bad := range []string{"not-a-uuid", "2334cda1-8d8a-4bdd-86a9-32eaf70f9d9a|extra", "' OR 1=1 --"} {
		if err := CheckUUIDSourceID(bad); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Fatalf("%q must be ErrInvalidCursor, got %v", bad, err)
		}
	}
}
