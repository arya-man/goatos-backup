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
		"board.aria":              "Tasks by status",
		"board.on_this_page":      "on this page",
		"board.focus_status":      "See every task in this status",
		"board.total_unavailable": "The whole-list total for this status is not published.",
		"board.column_empty":      "Nothing in this status on this page.",
		"board.drag_hint":         "Drag a card onto a status it is allowed to move to, or open a task and use its status buttons.",
		"board.drag_moving":       "Moving",
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
