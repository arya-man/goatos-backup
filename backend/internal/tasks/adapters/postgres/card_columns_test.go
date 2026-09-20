package postgres

import (
	"strings"
	"testing"
)

// countSelectColumns counts top-level comma-separated expressions in a column list.
func countSelectColumns(cols string) int {
	depth, n := 0, 1
	for _, r := range cols {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		}
	}
	return n
}

// TestColostrumCardColumnsMatchTheCardScanner pins the two card column lists to the same width:
// scanCard reads both, and a column added to one list only fails every read of the other at
// runtime ("number of field descriptions must equal number of destinations") -- which is exactly
// what happened when the sale subject columns landed on 2026-09-19.
func TestColostrumCardColumnsMatchTheCardScanner(t *testing.T) {
	a := countSelectColumns(strings.TrimSpace(cardSelectColumns))
	b := countSelectColumns(strings.TrimSpace(colostrumCardColumns))
	if a != b {
		t.Fatalf("cardSelectColumns has %d columns, colostrumCardColumns has %d; scanCard reads both position for position", a, b)
	}
	// The destinations in scanCard. It moves ONLY when a column is added to BOTH lists and read --
	// the 2026-09-20 purchase subject columns took it from 26 to 33.
	const scanned = 33
	if a != scanned {
		t.Fatalf("cardSelectColumns has %d columns, scanCard reads %d", a, scanned)
	}
}
