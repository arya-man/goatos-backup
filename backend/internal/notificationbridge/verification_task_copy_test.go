package notificationbridge

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Task-level lifecycle copy (maintainer decision 2026-09-12).
//
// The phone showed, for ONE pen move, a pending push that named the work ("Pen move · Gandhi 2 ·
// from Ho Chi Minh 1 · 1 animals recorded; counts video verification is pending.") and an
// approved push that did not ("Counts proof verified · counts proof for Sumathi 1 (Coimbatore)
// is verified."). Two code paths composed copy for the same item: pending led with the producer's
// subject, approved rebuilt a sentence from module + shed and threw the subject away. These tests
// pin the one shape every lifecycle push now shares: TASK noun in the title, the item's OWN
// subject and park in the body.

const (
	taskCopyTenant   = "bb000000-0000-4000-8000-0000000000a1"
	taskCopyPark     = "bb000000-0000-4000-8000-0000000000a2"
	taskCopyShed     = "bb000000-0000-4000-8000-0000000000a3"
	taskCopyItem     = "bb000000-0000-4000-8000-0000000000a4"
	taskCopyOperator = "bb000000-0000-4000-8000-0000000000a5"
)

type taskCopyRecipients struct{}

func oneDevice(id string) []workforcedomain.NotificationRecipient {
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: id, DeviceID: "device-" + id, FCMToken: "token-" + id}}
}

func (taskCopyRecipients) ResolveModuleDutyRecipients(_ context.Context, _, _, _, module, duty string) ([]workforcedomain.NotificationRecipient, error) {
	return oneDevice(module + "-" + duty), nil
}

func (taskCopyRecipients) ResolveMemberRecipients(_ context.Context, _, member string) ([]workforcedomain.NotificationRecipient, error) {
	return oneDevice(member), nil
}

func (taskCopyRecipients) ResolvePositionRecipients(_ context.Context, _, _, _, position string) ([]workforcedomain.NotificationRecipient, error) {
	return oneDevice(position), nil
}

type taskCopyQueue struct {
	queued []calendarports.QueueRoleNotifications
}

func (q *taskCopyQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	q.queued = append(q.queued, in)
	return len(in.Recipients), nil
}

func taskCopyPayload(t *testing.T, module, category, subject, reason string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"tenant_id":     taskCopyTenant,
		"item_id":       taskCopyItem,
		"vertical":      module,
		"module":        module,
		"category":      category,
		"subject_label": subject,
		"shed_id":       taskCopyShed,
		"park_id":       taskCopyPark,
		"operator_id":   taskCopyOperator,
		"reason":        reason,
		"source":        map[string]any{"module": module, "ref_type": "shed", "ref_id": taskCopyItem},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func taskCopyConsumer(queue *taskCopyQueue) *VerificationEventConsumer {
	return NewVerificationEventConsumer(taskCopyRecipients{}, queue, slog.Default()).
		withLocationSource(fakeLocationNames{taskCopyPark: "Coimbatore", taskCopyShed: "Sumathi 1"})
}

func queuedByType(t *testing.T, queue *taskCopyQueue, notificationType, messageKey string) calendarports.QueueRoleNotifications {
	t.Helper()
	for _, q := range queue.queued {
		if q.NotificationType == notificationType && (messageKey == "" || q.Context["message_key"] == messageKey) {
			return q
		}
	}
	t.Fatalf("no %s/%s push queued; got %d pushes", notificationType, messageKey, len(queue.queued))
	return calendarports.QueueRoleNotifications{}
}

// TestLifecyclePushesLeadWithTheItemsOwnSubject is the RED test for the screenshot: the approved
// and closed pushes must carry exactly what the pending one carried.
func TestLifecyclePushesLeadWithTheItemsOwnSubject(t *testing.T) {
	const subject = "Pen move · Sumathi 1 · from Ho Chi Minh 1 · 1 animal"
	const head = "Pen move · Sumathi 1 · from Ho Chi Minh 1 · 1 animal (Coimbatore)"

	cases := []struct {
		name        string
		event       string
		reason      string
		notifType   string
		messageKey  string
		wantTitle   string
		wantBody    string
		wantContext map[string]string
	}{
		{
			name: "pending/verifier", event: EventVerificationItemPending,
			notifType: NotificationTypeVerificationPending, messageKey: "counts.proof.pending.verifier",
			wantTitle: "Pen move video to verify",
			wantBody:  head + " — video is waiting for your verification.",
		},
		{
			name: "pending/leadership", event: EventVerificationItemPending,
			notifType: NotificationTypeVerificationPending, messageKey: "counts.proof.pending.leadership",
			wantTitle: "Pen move video pending",
			wantBody:  head + " — video verification is pending.",
		},
		{
			name: "approved", event: EventVerificationVerdictApproved,
			notifType: "verification_approved",
			wantTitle: "Pen move verified",
			wantBody:  head + " — video verified.",
		},
		{
			name: "rework/no reason", event: EventVerificationVerdictRework,
			notifType: NotificationTypeRework,
			wantTitle: "Pen move video sent back",
			wantBody:  head + " — video sent back by the verifier. Please record it again.",
		},
		{
			name: "rework/with reason", event: EventVerificationVerdictRework, reason: "animals not visible",
			notifType: NotificationTypeRework,
			wantTitle: "Pen move video sent back",
			wantBody:  head + " — video sent back: animals not visible. Please record it again.",
		},
		{
			name: "closed", event: EventVerificationItemClosed,
			notifType: "verification_closed",
			wantTitle: "Pen move closed",
			wantBody:  head + " — verified and closed.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queue := &taskCopyQueue{}
			if err := taskCopyConsumer(queue).HandleEvent(context.Background(), eventbus.Event{
				Type:    tc.event,
				Payload: taskCopyPayload(t, "counts", "shifting_move", subject, tc.reason),
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			got := queuedByType(t, queue, tc.notifType, tc.messageKey)
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Body != tc.wantBody {
				t.Errorf("body = %q, want %q", got.Body, tc.wantBody)
			}
			// The module word never reaches the reader; the routing keys keep it.
			if lower := strings.ToLower(got.Title + " " + got.Body); strings.Contains(lower, "counts") {
				t.Errorf("module word leaked into copy: %q / %q", got.Title, got.Body)
			}
			if got.Context["message_key"] == "" || !strings.HasPrefix(got.Context["message_key"], "counts.") {
				t.Errorf("message_key = %q, want the counts.* localization key kept", got.Context["message_key"])
			}
		})
	}
}

// TestLifecycleTitleIsTheTaskNotTheModule pins the task-level rule on the two modules whose
// module word is furthest from the farm's word for the work.
func TestLifecycleTitleIsTheTaskNotTheModule(t *testing.T) {
	cases := []struct {
		module, category, subject string
		wantTitle, wantBody       string
	}{
		{"pc_care", "pc_hoof_trimming", "Hoof Trimming · Mandela 1 - Part 6",
			"Hoof trimming verified", "Hoof Trimming · Mandela 1 - Part 6 (Coimbatore) — video verified."},
		{"health", "health_kids", "Day 3 · Pneumonia · G-1042 · Castro 2",
			"Treatment verified", "Day 3 · Pneumonia · G-1042 · Castro 2 (Coimbatore) — video verified."},
		{"feed", "feed_packing", "Session 2 · Castro 1",
			"Feed packing verified", "Session 2 · Castro 1 (Coimbatore) — video verified."},
		{"weighing", "weighing_proof", "Whole pen · Godel 2 - Part 1",
			"Weighing verified", "Whole pen · Godel 2 - Part 1 (Coimbatore) — video verified."},
	}
	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			queue := &taskCopyQueue{}
			if err := taskCopyConsumer(queue).HandleEvent(context.Background(), eventbus.Event{
				Type:    EventVerificationVerdictApproved,
				Payload: taskCopyPayload(t, tc.module, tc.category, tc.subject, ""),
			}); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			got := queuedByType(t, queue, "verification_approved", "")
			if got.Title != tc.wantTitle || got.Body != tc.wantBody {
				t.Errorf("got %q / %q\nwant %q / %q", got.Title, got.Body, tc.wantTitle, tc.wantBody)
			}
		})
	}
}

// TestSubjectlessItemNamesTaskAndPen covers the producer that deliberately sends NO subject
// (feed transport: its card already renders the shed). The push must still name the work and the
// place, never "A pen" or a bare module word.
func TestSubjectlessItemNamesTaskAndPen(t *testing.T) {
	queue := &taskCopyQueue{}
	if err := taskCopyConsumer(queue).HandleEvent(context.Background(), eventbus.Event{
		Type:    EventVerificationItemPending,
		Payload: taskCopyPayload(t, "feed", "feed_transport", "", ""),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	got := queuedByType(t, queue, NotificationTypeVerificationPending, "feed.proof.pending.leadership")
	if got.Title != "Feed transport video pending" {
		t.Errorf("title = %q", got.Title)
	}
	if want := "Feed transport · Sumathi 1 (Coimbatore) — video verification is pending."; got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
}

// TestSubjectlessVaccinationApprovalStillNamesTheDose keeps the C19c dose lookup alive for the
// one case that still needs it: a vaccination item with no subject names its vaccine from the
// sop task it discharged rather than degrading to "Vaccination · <pen>".
func TestSubjectlessVaccinationApprovalStillNamesTheDose(t *testing.T) {
	labels := &fakeVaccineLabels{byTaskID: map[string][]string{doseTask: {"ET", "TT"}}}
	queue := &taskCopyQueue{}
	consumer := taskCopyConsumer(queue).withVaccineLabelSource(labels)
	raw, err := json.Marshal(map[string]any{
		"tenant_id": taskCopyTenant, "item_id": taskCopyItem, "module": "vaccination",
		"category": "vaccination_proof", "shed_id": taskCopyShed, "park_id": taskCopyPark,
		"source": map[string]any{"module": "vaccination", "ref_type": "vaccination_goat", "task_id": doseTask},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.HandleEvent(context.Background(), eventbus.Event{Type: EventVerificationVerdictApproved, Payload: raw}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	got := queuedByType(t, queue, "verification_approved", "")
	if want := "ET+TT · Sumathi 1 (Coimbatore) — video verified."; got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
	if got.Title != "Vaccination verified" {
		t.Errorf("title = %q", got.Title)
	}
}

// TestEveryVerificationCategoryHasATaskNoun keeps the noun table complete: a category shipped
// without a farm word would fall to the module fallback, which is the module-level wording this
// change retires.
func TestEveryVerificationCategoryHasATaskNoun(t *testing.T) {
	for _, def := range verificationcatalog.All() {
		if _, ok := verificationTaskNouns[def.Category]; !ok {
			t.Errorf("verification category %q (%s / %s) has no task noun in verificationTaskNouns", def.Category, def.Module, def.PageLabel)
		}
	}
	for category, noun := range verificationTaskNouns {
		if noun == "" || strings.Contains(strings.ToLower(noun), "_") {
			t.Errorf("task noun for %q is not farm copy: %q", category, noun)
		}
	}
}
