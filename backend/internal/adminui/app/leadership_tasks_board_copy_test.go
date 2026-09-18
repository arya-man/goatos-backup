package app

import "testing"

// The Tasks board rendered seven of its own sentences from the screen's 3-arg
// `copy(contract, key, fallback)` defaults because pageSpecificCopy("leadership-tasks") never
// carried them (pending-work P4, 2026-09-18). Copy is backend-owned: the contract must serve
// each key, with the text the screen was already showing, so the live board reads the same
// whether or not the frontend keeps its fallback.
func TestLeadershipTasksBoardCopyIsServedByTheContract(t *testing.T) {
	page := pageCopy("leadership-tasks")
	want := map[string]string{
		"board.aria":         "Tasks by status",
		"board.column_empty": "Nothing in this status on this page.",
		"board.drag_moving":  "Moving",
	}
	for key, text := range want {
		got, ok := page[key]
		if !ok {
			t.Errorf("leadership-tasks contract is missing board copy key %q", key)
			continue
		}
		if got != text {
			t.Errorf("%s = %q, want %q (the board's fallback text; reword both sides together)", key, got, text)
		}
	}
}
