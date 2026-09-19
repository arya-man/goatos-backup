package domain

import (
	"testing"
	"time"
)

// TestStepOwnedByHonoursTheAuthoredDesignation pins the owner gate (SALES SOP, 2026-09-19): a
// step owned by a designation refuses a caller who does not hold it, an unowned step is anyone's,
// the CEO floor is never narrowed, and an engine/CLI caller (nil roles) is never refused.
func TestStepOwnedByHonoursTheAuthoredDesignation(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		roles []string
		want  bool
	}{
		{"unowned step is anyone's", "", []string{"operator"}, true},
		{"holder of the designation", "park_head", []string{"operator", "park_head"}, true},
		{"another designation is refused", "park_head", []string{"operator"}, false},
		{"no roles at all is refused", "park_head", []string{}, false},
		{"ceo floor", "park_head", []string{"ceo_internal"}, true},
		{"engine caller", "park_head", nil, true},
		{"whitespace owner is unowned", "  ", []string{}, true},
	}
	for _, tc := range cases {
		if got := StepOwnedBy(tc.owner, tc.roles); got != tc.want {
			t.Fatalf("%s: StepOwnedBy(%q, %v) = %v, want %v", tc.name, tc.owner, tc.roles, got, tc.want)
		}
	}
}

// TestCompileTrackStampsTheOwner: the authored owner reaches the template verbatim (trimmed), so
// the repository stamps workflow_actions.owner_role from it.
func TestCompileTrackStampsTheOwner(t *testing.T) {
	reg, _ := SeededTaskTypes()
	track := FollowUpTrack{Key: "t", Module: ModuleSales, Steps: []FollowUpStep{
		{Key: "a", TaskType: "do_and_confirm", Title: "A", Owner: " park_head ", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}},
		{Key: "b", TaskType: "do_and_confirm", Title: "B", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}},
	}}
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Actions[0].Owner != "park_head" || tmpl.Actions[1].Owner != "" {
		t.Fatalf("owners = %q, %q", tmpl.Actions[0].Owner, tmpl.Actions[1].Owner)
	}
}
