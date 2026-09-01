package notificationbridge

import (
	"context"
	"strings"
	"testing"
	"time"

	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type overdueLoadReaderFake struct {
	overdue   []procurementdomain.OverdueLoad
	askedAsOf string
	callCount int
}

func (f *overdueLoadReaderFake) OverdueLoadCandidates(_ context.Context, _ string, asOf string) ([]procurementdomain.OverdueLoad, error) {
	f.askedAsOf = asOf
	f.callCount++
	return f.overdue, nil
}

// The fixture holds four loads, and only ONE of them should alert:
//
//	131  71 days, 63 on farm   -- under the threshold
//	129  92 days, 77 on farm   -- OVER and still holding: the alert
//	113 292 days,  0 on farm   -- over but sold out, so it is history, not an alert
//	100  90 days,  5 on farm   -- exactly AT the threshold, and "exceeds 90" means 91
func newOverdueFixture() (*overdueLoadReaderFake, *missedRecipientsFake, *missedQueueFake, *LoadAgeNotifier) {
	reader := &overdueLoadReaderFake{overdue: []procurementdomain.OverdueLoad{{
		LoadID: "l-129", LoadRef: "129", VendorName: "Krishnamorrthy", Farm: "CPT",
		PurchaseDate: "2026-06-01", DaysSincePurchase: 92, Remaining: 77,
	}}}
	recipients := &missedRecipientsFake{byPosition: map[string][]workforcedomain.NotificationRecipient{
		"tenant|" + missedTenant + "|ceo_internal": {{WorkforceMemberID: "m-ceo", DeviceID: "d-ceo", FCMToken: "fcm-ceo"}},
		// Other leadership seats are reachable in the fake and must NOT be picked: the maintainer
		// sent this one to the CXO alone.
		"tenant|" + missedTenant + "|procurement_director": {{WorkforceMemberID: "m-pd", DeviceID: "d-pd", FCMToken: "fcm-proc"}},
		"tenant|" + missedTenant + "|feed_director":        {{WorkforceMemberID: "m-fd", DeviceID: "d-fd", FCMToken: "fcm-feed"}},
		"tenant|" + missedTenant + "|pc_director":          {{WorkforceMemberID: "m-pc", DeviceID: "d-pc", FCMToken: "fcm-pc"}},
	}}
	queue := &missedQueueFake{}
	notifier := NewLoadAgeNotifier(reader, recipients, queue, nil).
		WithClock(func() time.Time { return time.Date(2026, 9, 1, 4, 30, 0, 0, time.UTC) }) // 10:00 IST
	return reader, recipients, queue, notifier
}

// TestOverdueLoadAlertFiresOnlyForOpenLoadsPastTheThreshold pins BOTH halves of the selection.
//
// The sold-out row is the one that matters: a load bought 292 days ago that has no animals left is
// history, and alerting on it every morning forever would train the CXO to ignore the alert --
// which costs more than the alert gains. The at-exactly-90 row pins "exceeds 90 days" as > 90.
func TestOverdueLoadAlertQueuesEveryCandidateTheRepositoryReturns(t *testing.T) {
	_, _, queue, notifier := newOverdueFixture()
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyOverdueLoads: %v", err)
	}
	if len(queue.queued) != 1 {
		var got []string
		for _, m := range queue.queued {
			got = append(got, m.Title)
		}
		t.Fatalf("queued %d messages, want exactly the one open overdue load; got %v", len(queue.queued), got)
	}
	if !strings.Contains(queue.queued[0].Title, "129") {
		t.Fatalf("alerted on the wrong load: %q", queue.queued[0].Title)
	}
}

// TestOverdueLoadAlertNamesTheLoadVendorFarmAgeAndHeadCount: the message must be actionable
// without opening anything. An abstract "1 load is overdue" is the defect the
// notification-specificity rule bans, and a uuid in a push is banned copy.
func TestOverdueLoadAlertNamesTheLoadVendorFarmAgeAndHeadCount(t *testing.T) {
	_, _, queue, notifier := newOverdueFixture()
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyOverdueLoads: %v", err)
	}
	msg := queue.queued[0]
	text := msg.Title + " " + msg.Body
	for _, want := range []string{
		"Load 129",       // which load, the farm's own number
		"Krishnamorrthy", // from whom
		"CPT",            // where
		"92 days",        // how old
		"77 animals",     // what is still standing there
		"01/06/2026",     // bought on, in farm date format
		"90 days",        // the rule it broke
	} {
		if !strings.Contains(text, want) {
			t.Errorf("alert does not say %q: %q", want, text)
		}
	}
	if strings.Contains(text, "l-129") {
		t.Errorf("the load's internal id leaked into farm copy: %q", text)
	}
}

// TestOverdueLoadAlertGoesToTheCXOAlone is the audience lock. The Procurement Director buys loads
// and the Feed Director feeds them, but the decision to hold or move stock sits with the CEO/CXO
// desk -- the maintainer's word. All three other seats are reachable in the fixture, so any change
// that widens the audience turns this red.
func TestOverdueLoadAlertGoesToTheCXOAlone(t *testing.T) {
	_, _, queue, notifier := newOverdueFixture()
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyOverdueLoads: %v", err)
	}
	recipients := queue.queued[0].Recipients
	if len(recipients) != 1 {
		t.Fatalf("queued %d recipients, want the CXO alone", len(recipients))
	}
	if recipients[0].FCMToken != "fcm-ceo" {
		t.Fatalf("recipient = %q, want the CEO/CXO device", recipients[0].FCMToken)
	}
}

// TestOverdueLoadAlertIsOncePerDayByIdempotencyKey: "once a day" is a property of the KEY, not of a
// scheduler. Two ticks on the same business date must produce the same event key, so the queue's
// own uniqueness drops the second -- which is what makes the property survive a worker restart, a
// mid-day redeploy, or both HA instances ticking together.
func TestOverdueLoadAlertIsOncePerDayByIdempotencyKey(t *testing.T) {
	_, _, queue, notifier := newOverdueFixture()
	for i := 0; i < 3; i++ {
		if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(queue.queued) != 3 {
		t.Fatalf("the fake queue recorded %d writes; this test is about the KEY, not the fake", len(queue.queued))
	}
	first := queue.queued[0].EventKey
	for i, msg := range queue.queued {
		if msg.EventKey != first {
			t.Fatalf("tick %d produced event key %q, want the same %q — a differing key defeats the queue's dedupe and re-pushes the alert",
				i, msg.EventKey, first)
		}
	}
	if !strings.Contains(first, "2026-09-01") {
		t.Fatalf("event key %q does not carry the business date; without it the key never rolls over to tomorrow", first)
	}

	// And tomorrow it MUST fire again — a key that never changes would alert once and go silent.
	notifier.WithClock(func() time.Time { return time.Date(2026, 9, 2, 4, 30, 0, 0, time.UTC) })
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if next := queue.queued[len(queue.queued)-1].EventKey; next == first {
		t.Fatalf("the next day reused key %q; the alert would never fire again", next)
	}
}

// TestOverdueLoadAlertReadsUnpagedCandidatesOnce: the alert is about old loads, so it must not
// read the newest-first UI page and then filter inside that clipped window. One read per run, not
// one per load, and the business date is passed through so the repository and idempotency key use
// the same day.
func TestOverdueLoadAlertReadsUnpagedCandidatesOnce(t *testing.T) {
	reader, _, _, notifier := newOverdueFixture()
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyOverdueLoads: %v", err)
	}
	if reader.callCount != 1 {
		t.Fatalf("read the load model %d times, want once for the whole run", reader.callCount)
	}
	if reader.askedAsOf != "2026-09-01" {
		t.Fatalf("asked as-of date %q, want the business date 2026-09-01", reader.askedAsOf)
	}
}

// TestNoOverdueLoadsQueuesNothing: a quiet day must be silent, not an empty push.
func TestNoOverdueLoadsQueuesNothing(t *testing.T) {
	reader, _, queue, notifier := newOverdueFixture()
	reader.overdue = nil
	if err := notifier.NotifyOverdueLoads(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyOverdueLoads: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued %d messages on a day with nothing overdue", len(queue.queued))
	}
}
