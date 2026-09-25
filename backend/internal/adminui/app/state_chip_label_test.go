package app

import "testing"

// The /routines Today table's status column is keyed state_chip; the derived label read
// "State chip" on screen (2026-09-26). A column key names the widget, never the reader's word.
func TestStateChipColumnReadsStatus(t *testing.T) {
	if got := humanLabel("state_chip"); got != "Status" {
		t.Fatalf(`humanLabel("state_chip") = %q, want "Status"`, got)
	}
}
