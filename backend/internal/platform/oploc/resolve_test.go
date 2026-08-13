package oploc

import (
	"context"
	"errors"
	"testing"
)

type fakeRow struct {
	shed, partition string
	err             error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	*(dest[0].(*string)) = f.shed
	*(dest[1].(*string)) = f.partition
	return nil
}

// The resolver's contract is the OUTPUT STRING an operator reads, so these assert Display()
// rather than field presence -- a field-presence test passed while real output was wrong.
func TestResolveShedLocationUsesExactShedNameForDisplay(t *testing.T) {
	for _, tc := range []struct {
		name, shed, partition, want string
	}{
		{"compatibility partition does not change shed name", "Godel 1", "Part 3", "Godel 1"},
		{"exact shed name renders as itself", "Godel 1 Part 3", "Part 3", "Godel 1 Part 3"},
		{"unpartitioned renders bare, no trailing separator", "Yashoda", "", "Yashoda"},
		{"whole sentinel never reaches a screen", "Yashoda", "whole", "Yashoda"},
		{"numeric compatibility partition does not change shed name", "Castro", "2", "Castro"},
		{"exact numbered shed renders as itself", "Castro 2", "2", "Castro 2"},
		{"unresolvable shed degrades to empty, never a uuid", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := ResolveShedLocation(context.Background(), fakeRow{shed: tc.shed, partition: tc.partition})
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got := loc.Display(); got != tc.want {
				t.Fatalf("Display() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveShedLocationPropagatesScanError(t *testing.T) {
	sentinel := errors.New("boom")
	if _, err := ResolveShedLocation(context.Background(), fakeRow{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped %v", err, sentinel)
	}
}

// The canonical SQL must never fetch legacy partition labels for display. This asserts the query
// text itself because the defect it guards compiles cleanly and renders names such as
// 'Castro 2 2' to an operator.
func TestShedScopedLocationSQLSelectsExactShedOnly(t *testing.T) {
	if contains(ShedScopedLocationSQL, "sp.partition_label") {
		t.Fatal("canonical SQL must not select partition_label for live display")
	}
	if contains(ShedScopedLocationSQL, "normalized_label") {
		t.Fatal("canonical SQL must NEVER select normalized_label -- it is a matching key, not display copy")
	}
	if contains(ShedScopedLocationSQL, "shed_partitions") {
		t.Fatal("canonical SQL must not inspect shed_partitions for exact-shed display")
	}
	if contains(ShedScopedLocationSQL, "LIMIT 1") {
		t.Fatal("LIMIT 1 over legitimately-differing rows fabricates a partition")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
