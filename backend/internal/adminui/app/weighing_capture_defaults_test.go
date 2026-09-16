package app

import (
	"encoding/json"
	"strings"
	"testing"

	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): the Weighing SOP page serves the seeded slot copy
// so the web editor renders a pre-slot document as the backend reads it. adminui does not import
// the weighing domain in production code, so the two copies are pinned equal here.
func TestWeighingSOPCaptureDefaultsMatchTheEmbeddedSeed(t *testing.T) {
	served := weighingSOPEditorCopy()["wsop.capture.defaults"]
	var a, b any
	if err := json.Unmarshal([]byte(served), &a); err != nil {
		t.Fatalf("served defaults are not JSON: %v", err)
	}
	if err := json.Unmarshal(weighingdomain.SeededCaptureSlotsJSON(), &b); err != nil {
		t.Fatalf("embedded seed: %v", err)
	}
	ca, _ := json.Marshal(a)
	cb, _ := json.Marshal(b)
	if string(ca) != string(cb) {
		t.Fatalf("served capture defaults drifted from the embedded seed:\n%s\n%s", ca, cb)
	}
	// The two sections are titled apart on the page, never one shared list.
	copy := weighingSOPEditorCopy()
	if copy["wsop.capture.individual.title"] != "Per animal" || copy["wsop.capture.lump_sum.title"] != "Whole pen" {
		t.Fatalf("section titles = %q / %q", copy["wsop.capture.individual.title"], copy["wsop.capture.lump_sum.title"])
	}
	for _, key := range []string{"wsop.capture.individual.proofs", "wsop.capture.individual.questions", "wsop.capture.lump_sum.proofs", "wsop.capture.lump_sum.questions", "wsop.capture.lump_sum.slot_min", "wsop.capture.lump_sum.slot_max"} {
		if strings.TrimSpace(copy[key]) == "" {
			t.Fatalf("copy %q missing", key)
		}
	}
}
