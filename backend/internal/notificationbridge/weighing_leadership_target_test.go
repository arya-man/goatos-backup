package notificationbridge_test

import (
	"context"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
)

// Regression test for the leadership deep-link defect: every weighing push carried the bare
// "/weighing" module landing, which is the OPERATOR's own work list. A Growth Director or CEO who
// tapped a push about a task they oversee was dropped into an empty My Work with no route to the
// task the push named. The leadership-audience pushes must name the affected task or bucket.
//
// The operator-audience pushes in the same handlers are asserted to KEEP the module landing:
// their recipient has to go and capture something, and the read-only record is not where that
// happens. That contrast is the whole point of the fix and is what makes it a routing rule rather
// than a blanket rewrite.

// leadershipQueued returns the queued push whose recipients are the leadership seats (director +
// CEO tokens), identified by token so the assertion does not depend on queue ordering.
func leadershipQueued(t *testing.T, queued []calendarports.QueueRoleNotifications) calendarports.QueueRoleNotifications {
	t.Helper()
	for _, notif := range queued {
		for _, recipient := range notif.Recipients {
			if recipient.FCMToken == tokenDirector || recipient.FCMToken == tokenCEO {
				return notif
			}
		}
	}
	t.Fatalf("no leadership push was queued")
	return calendarports.QueueRoleNotifications{}
}

func TestWeighingPublishLeadershipPushOpensTheTaskNotMyWork(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(),
		lifecycleEvent(t, notificationbridge.EventWeighingCampaignPublished, "evt-publish", publishPayload())); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	leadership := leadershipQueued(t, queue.queued)
	want := "/weighing/task?campaignId=" + lifecycleCampaign
	if leadership.Context["target"] != want {
		t.Fatalf("leadership publish target=%q, want %q", leadership.Context["target"], want)
	}

	// The same event's operator pushes stay on the capture landing.
	for _, notif := range queue.queued {
		for _, recipient := range notif.Recipients {
			if recipient.FCMToken != tokenOpA && recipient.FCMToken != tokenOpB {
				continue
			}
			if notif.Context["target"] != "/weighing" {
				t.Fatalf("operator publish target=%q, want /weighing (the capture entry point)", notif.Context["target"])
			}
		}
	}
}

// TestWeighingLeadershipTargetsAreAppResolvable guards the other half of the fix: a deep link the
// Android build cannot parse falls through to the recipient's own landing, which would silently
// reinstate the defect. Only the shapes AppNavHost hosts as push destinations may be emitted.
func TestWeighingLeadershipTargetsAreAppResolvable(t *testing.T) {
	_, queue, consumer := newLifecycleFixture()

	if err := consumer.HandleEvent(context.Background(),
		lifecycleEvent(t, notificationbridge.EventWeighingCampaignPublished, "evt-publish", publishPayload())); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	// Routes.WEIGHING / WEIGHING_TASK / WEIGHING_SHED / WEIGHING_VIDEOS, the weighing entries of
	// AppNavHost.pushTargetDestinations.
	hosted := map[string]bool{
		"/weighing":        true,
		"/weighing/task":   true,
		"/weighing/shed":   true,
		"/weighing/videos": true,
	}
	for _, notif := range queue.queued {
		target := notif.Context["target"]
		if !hosted[strings.TrimRight(strings.SplitN(target, "?", 2)[0], "/")] {
			t.Fatalf("target %q is not an Android push destination; the tap would land on the recipient's own screen", target)
		}
	}
}
