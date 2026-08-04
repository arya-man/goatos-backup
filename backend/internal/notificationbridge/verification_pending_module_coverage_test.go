package notificationbridge

import "testing"

// Every module that enqueues a verification item must declare how its pending-proof
// notification is routed. This is the assertion that was missing when weighing proofs
// notified the vaccination verifier and the PC director, in vaccination wording, because
// an unclaimed module quietly inherited the vaccination profile.
//
// The fallback is gone, so an unclaimed module now notifies NOBODY -- which is the safer
// failure, but still a failure. This test is what turns "nobody was told" from a field
// report into a build error.
//
// When a module starts creating verification items, add it here AND to
// pendingModuleProfiles. The list is deliberately hand-maintained: deriving it by scanning
// for CreateItem callers would make the test pass by construction, which is exactly the
// vacuous shape a reviewer rejected on the permissions guard.
func TestEveryEnqueuingModuleHasAPendingNotificationProfile(t *testing.T) {
	// Source module strings as written by each module's verificationbridge enqueuer.
	enqueuingModules := map[string]string{
		"vaccination": "backend/internal/sopbridge/vaccination_submission.go",
		"weighing":    "backend/internal/weighing/adapters/verificationbridge/enqueue.go",
		"counts":      "backend/internal/tasks/adapters/verificationbridge/enqueue.go",
		"feed":        "backend/internal/feeddirection/adapters/verificationbridge/enqueue.go",
	}

	for module, producer := range enqueuingModules {
		if _, ok := pendingProfileFor(module); !ok {
			t.Errorf(
				"module %q enqueues verification items (%s) but has no entry in pendingModuleProfiles: "+
					"its pending-proof notification reaches nobody. Declare the duty module, the "+
					"leadership position, and the module's own wording and tap route.",
				module, producer,
			)
		}
	}
}

// A module must never inherit another module's recipients or wording.
func TestPendingProfileHasNoFallback(t *testing.T) {
	if _, ok := pendingProfileFor("some_module_that_does_not_exist"); ok {
		t.Fatal("an unknown module resolved to a profile; the vaccination fallback has returned")
	}
}
