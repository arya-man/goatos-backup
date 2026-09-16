package app

import (
	"testing"

	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// SHIFTING SOP (2026-09-16): the shifting code runs the card-with-slots `shifting` section, not a
// follow_up track. The seeded track stays on existing versions as dormant history; a new version
// without one must still save.
func TestShiftingCodeNoLongerRequiresFollowUpTrack(t *testing.T) {
	if FollowUpRequired(tasksdomain.SOPCodeShifting) {
		t.Fatal("shifting still demands a follow_up track; its operator work is the shifting section")
	}
	for _, code := range []string{tasksdomain.SOPCodeBirth, tasksdomain.SOPCodeDeath, tasksdomain.SOPCodeReconcile} {
		if !FollowUpRequired(code) {
			t.Fatalf("%s must still require its follow_up tracks", code)
		}
	}
}
