package postgres

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A phone on Android 13+ starts with the OS notification permission DENIED, and a person can
// switch notifications off at any time afterwards. That phone still holds a valid FCM token, so
// FCM accepts the send and reports success while the OS drops it — the backend counts a delivery
// that never reached anyone. Recipient resolution must therefore skip a device that has reported
// itself muted, and must keep addressing one that has never reported (NULL, older app build).
func TestPushReachableDeviceExcludesMutedDevicesAndKeepsUnreported(t *testing.T) {
	if !strings.Contains(pushReachableDeviceSQL, "d.status = 'active'") {
		t.Errorf("reachable-device predicate no longer requires an active device:\n%s", pushReachableDeviceSQL)
	}
	if !strings.Contains(pushReachableDeviceSQL, "d.fcm_token IS NOT NULL") {
		t.Errorf("reachable-device predicate no longer requires a push token:\n%s", pushReachableDeviceSQL)
	}
	if !strings.Contains(pushReachableDeviceSQL, "d.notifications_enabled IS DISTINCT FROM false") {
		t.Errorf("reachable-device predicate does not exclude push-muted devices, so a dropped push is still counted as delivered:\n%s", pushReachableDeviceSQL)
	}
	// `= true` would silently stop notifying every device registered by an app build that predates
	// the notifications_enabled contract (those rows are NULL).
	if strings.Contains(pushReachableDeviceSQL, "d.notifications_enabled = true") {
		t.Errorf("predicate uses `= true`, which drops every device that has never reported the switch:\n%s", pushReachableDeviceSQL)
	}
}

// The mute condition went missing everywhere at once because "a device we can reach" was
// hand-copied into six separate queries. This keeps that from happening again: every recipient
// query must splice the one shared predicate rather than spelling the conditions out itself.
func TestRecipientQueriesUseTheSharedReachablePredicate(t *testing.T) {
	source, err := os.ReadFile("roster_repository.go")
	if err != nil {
		t.Fatalf("read roster_repository.go: %v", err)
	}
	body := string(source)

	const constDefinition = "const pushReachableDeviceSQL = `"
	idx := strings.Index(body, constDefinition)
	if idx < 0 {
		t.Fatalf("pushReachableDeviceSQL definition not found")
	}
	// Everything except the const's own declaration.
	rest := body[:idx] + body[idx+len(constDefinition)+len(pushReachableDeviceSQL):]

	handRolled := regexp.MustCompile(`(?m)^\s*AND\s+d\.fcm_token IS NOT NULL`)
	if got := handRolled.FindAllString(rest, -1); len(got) > 0 {
		t.Errorf("%d recipient quer(ies) still spell out the reachable-device conditions by hand instead of splicing pushReachableDeviceSQL; that is how the push-muted check went missing: %q", len(got), got)
	}
	// gofmt drops the spaces around `+` inside a raw-string splice, so accept either form.
	spliced := regexp.MustCompile("`\\s*\\+\\s*pushReachableDeviceSQL\\s*\\+\\s*`")
	if !spliced.MatchString(rest) {
		t.Error("no recipient query splices pushReachableDeviceSQL")
	}
}
