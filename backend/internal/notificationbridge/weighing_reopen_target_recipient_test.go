package notificationbridge_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// The reopen push keeps the "/weighing" module landing while every OTHER weighing leadership
// push deep-links a task or a bucket, and that asymmetry is deliberate. It was re-verified
// against the CURRENT Android navigation after /weighing's behaviour changed underneath it
// (apps/goatos-android/.../ui/AppNavHost.kt, composable(Routes.WEIGHING)), and it still holds:
//
//   - the ASSIGNEE is the only recipient the reopen asks to DO something -- go re-capture the
//     animals. They hold weighing.execute, so /weighing renders their own work list, and the
//     reopened bucket is live again and on it. The bucket RECORD (/weighing/shed) would be the
//     wrong destination for them: it is read-only and carries no scan entry point, so the one
//     person who must act would land one screen away from acting with no way through.
//   - a recipient who does NOT execute (the CEO holds weighing.plan + weighing.monitor and
//     deliberately not weighing.execute) is redirected by that same composable to
//     /weighing/operators, the oversight surface that carries closed and reopened history plus
//     the oversight actions. That redirect landed on this branch and it is what repaired the
//     only recipient this target used to be wrong for.
//
// One queued notification carries ONE target for all three recipients, so the choice is which
// recipient to serve, not what each of them gets. Serving the executor is the right trade: the
// non-executor's landing is already routed to oversight, while a bucket-record target would
// strand the executor.
//
// This test pins the target TOGETHER WITH the recipient set, because the decision only holds
// while the assignee is on it. If the reopen push ever becomes leadership-only, the module
// landing stops being the right answer and this test must fail rather than quietly pass.
func TestWeighingReopenTargetServesTheAssigneeItAsksToReCapture(t *testing.T) {
	recipients := &targetTestRecipients{}
	queue := &targetTestQueue{}
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(recipients, queue, slog.Default())

	if err := consumer.HandleEvent(context.Background(), eventbus.Event{
		Type:    notificationbridge.EventWeighingShedReopened,
		Payload: reopenedPayloadWithTarget(),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("reopen queued %d notifications, want exactly 1 (one target for all recipients)", len(queue.queued))
	}
	notif := queue.queued[0]

	if notif.Context["target"] != "/weighing" {
		t.Fatalf("reopen target=%q, want /weighing -- the assignee's capture surface", notif.Context["target"])
	}
	// The assignee must be ON this notification. Without them the module landing is serving
	// nobody and the target should become the reopened bucket.
	var assignee bool
	for _, recipient := range notif.Recipients {
		if recipient.FCMToken == "token-op" {
			assignee = true
		}
	}
	if !assignee {
		t.Fatalf("reopen no longer notifies the bucket's assignee; recipients=%+v. The module-landing target only serves the executor, so a leadership-only reopen push must deep-link the bucket instead", notif.Recipients)
	}
	// `screen` is the resolver's second-chance match when a build cannot host the target, and it
	// must agree with the target rather than naming a different destination.
	if notif.Context["screen"] != "weighing" {
		t.Fatalf("reopen screen=%q, want weighing to agree with the target", notif.Context["screen"])
	}
}
