package main

import (
	"errors"
	"testing"

	"google.golang.org/api/iterator"
)

func TestIsDone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"iterator.Done is done", iterator.Done, true},
		{"nil is not done", nil, false},
		{"a different error is not done", errors.New("boom"), false},
		{"a wrapped iterator.Done is NOT recognized (isDone uses ==, not errors.Is)", fmtErrorf(iterator.Done), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDone(tc.err); got != tc.want {
				t.Fatalf("isDone(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// fmtErrorf wraps err the way %w would, without importing fmt into the test
// solely for one call - keeps the "wrapped Done is not caught" case explicit
// so a future accidental fmt.Errorf("...: %w", iterator.Done) wrap elsewhere
// in this package would visibly break its own for-loop instead of silently
// looping forever.
func fmtErrorf(err error) error {
	return &wrappedErr{err: err}
}

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }
