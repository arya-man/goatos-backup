package app

import "testing"

// Phase A E2E (2026-09-17): opening "Change SOP" on /weighing/sops crashed the editor with
// "Admin-web page contract weighing-sops missing copy key wsop.title". The merge that gave every
// module's SOP builder its own name (e41a95e93) dropped the weighing editor copy from the
// weighing-sops page contract, so the rules editor and its drawer summary (both read wsop.*) had
// no copy at all. Every wsop key the editor reads must be on the page it renders on.
func TestWeighingSOPsPageCarriesTheWeighingEditorCopy(t *testing.T) {
	page := pageSpecificCopy("weighing-sops")
	for key := range weighingSOPEditorCopy() {
		if _, ok := page[key]; !ok {
			t.Fatalf("weighing-sops page contract is missing editor copy key %q", key)
		}
	}
	if page["modal.builder.default_name"] != "Weighing session" {
		t.Fatalf("builder name = %q, want the module's own (the e41a95e93 fix must stay)", page["modal.builder.default_name"])
	}
}

// The locked tick above the per-animal slots said "One live-camera video per animal · fixed"
// while the slot list beneath it lets the author make that capture a photo -- the lock is "at
// least one compulsory capture", which is what the validator enforces (Phase A E2E screenshot).
func TestWeighingPerAnimalLockCopyDoesNotPromiseAVideo(t *testing.T) {
	got := weighingSOPEditorCopy()["wsop.capture.individual.video"]
	if got != "At least one compulsory capture per animal" {
		t.Fatalf("wsop.capture.individual.video = %q", got)
	}
}
