package postgres

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// AN UNSUPPORTED SEX IS A BAD REQUEST, NOT A BROKEN SERVER.
//
// The defect this pins (review finding, 2026-08-26): normalizeSexFilter returned a bare
// fmt.Errorf, which matched no arm of the weighing HTTP error mapper and fell through to the
// internal-error path -- `?sex=foo` answered 500 while the handler's own comment claimed unknown
// values were rejected as invalid input. A caller who mistypes a filter has made a bad REQUEST;
// telling them the server broke sends them looking in the wrong place entirely.
//
// A pure unit test on purpose: this is the one rule in this file that needs no database, and it
// must run in every environment, including the ones where the Postgres suite is opted out.
func TestNormalizeSexFilterRejectsUnknownValuesAsInvalidArgument(t *testing.T) {
	for _, bad := range []string{"foo", "m", "unknown", "all", "0", "both"} {
		got, err := normalizeSexFilter(bad)
		if err == nil {
			t.Fatalf("normalizeSexFilter(%q) must fail, got %q", bad, got)
		}
		// errors.Is, not a string match: the HTTP mapper switches on this sentinel, so a message
		// that merely READS like a validation error would still answer 500.
		if !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("normalizeSexFilter(%q) must wrap ports.ErrInvalidArgument so the API answers 400, got %v", bad, err)
		}
	}

	// And the three the page really sends still resolve. An EMPTY sex is "no filter", never an
	// error: it is what every page load carries until a reader picks a side.
	for _, in := range []struct{ raw, want string }{{"", ""}, {"male", "male"}, {"female", "female"}, {" MALE ", "male"}} {
		got, err := normalizeSexFilter(in.raw)
		if err != nil {
			t.Fatalf("normalizeSexFilter(%q): %v", in.raw, err)
		}
		if got != in.want {
			t.Fatalf("normalizeSexFilter(%q) = %q, want %q", in.raw, got, in.want)
		}
	}
}
