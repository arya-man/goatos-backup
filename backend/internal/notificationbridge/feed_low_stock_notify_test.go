package notificationbridge

import (
	"context"
	"strings"
	"testing"
	"time"

	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type lowStockReaderFake struct {
	feeds     []feeddomain.LowStockFeed
	askedDays int
}

func (f *lowStockReaderFake) LowStockFeeds(_ context.Context, _ string, withinDays int) ([]feeddomain.LowStockFeed, error) {
	f.askedDays = withinDays
	return f.feeds, nil
}

func newLowStockFixture() (*lowStockReaderFake, *missedRecipientsFake, *missedQueueFake, *FeedLowStockNotifier) {
	reader := &lowStockReaderFake{feeds: []feeddomain.LowStockFeed{{
		ParkID: missedPark, FarmLabel: "Coimbatore",
		FeedItemLabel: "Mesha Adult Concentrate Sheep", FeedItemKey: "mesha_adult_concentrate_sheep",
		BalanceKg: "251.0", AvgDailyKg: "181.9", DaysLeft: 1,
	}}}
	recipients := &missedRecipientsFake{byPosition: map[string][]workforcedomain.NotificationRecipient{
		"tenant|" + missedTenant + "|ceo_internal":         {{WorkforceMemberID: "m-ceo", DeviceID: "d-ceo", FCMToken: "fcm-ceo"}},
		"tenant|" + missedTenant + "|feed_director":        {{WorkforceMemberID: "m-fd", DeviceID: "d-fd", FCMToken: "fcm-feed"}},
		"tenant|" + missedTenant + "|procurement_director": {{WorkforceMemberID: "m-pd", DeviceID: "d-pd", FCMToken: "fcm-proc"}},
		// Another director is reachable in the fake and must NOT be picked: this alert goes to the
		// three desks that can act on a feed running out, not to every leadership seat.
		"tenant|" + missedTenant + "|pc_director": {{WorkforceMemberID: "m-pc", DeviceID: "d-pc", FCMToken: "fcm-pc"}},
	}}
	queue := &missedQueueFake{}
	notifier := NewFeedLowStockNotifier(reader, recipients, queue, nil).
		WithClock(func() time.Time { return time.Date(2026, 8, 24, 4, 30, 0, 0, time.UTC) }) // 10:00 IST
	return reader, recipients, queue, notifier
}

// The alert must be ACTIONABLE: it names the farm, the feed, how many days are left and how much is
// in the store, and it dates itself the way the farm writes dates. An abstract "3 feeds are low" is
// the exact defect the notification-specificity rule bans.
func TestLowStockAlertNamesTheFarmFeedAndDaysAndDatesItTheFarmWay(t *testing.T) {
	reader, _, queue, notifier := newLowStockFixture()
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock: %v", err)
	}
	if reader.askedDays != feeddomain.LowStockNotifyDays {
		t.Errorf("asked for %d days, want the alert horizon %d", reader.askedDays, feeddomain.LowStockNotifyDays)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d messages, want one per low feed", len(queue.queued))
	}
	msg := queue.queued[0]
	for _, want := range []string{"Mesha Adult Concentrate Sheep", "Coimbatore", "251 kg", "1 days", "24/08/2026"} {
		if !strings.Contains(msg.Title+" "+msg.Body, want) {
			t.Errorf("alert must name %q: title=%q body=%q", want, msg.Title, msg.Body)
		}
	}
	// The visible copy carries no ISO date, and the structured context carries nothing else.
	if strings.Contains(msg.Body, "2026-08-24") {
		t.Errorf("visible copy must not carry the ISO date: %q", msg.Body)
	}
	if msg.Context["business_date"] != "2026-08-24" {
		t.Errorf("structured business_date = %q, want ISO for clients to parse", msg.Context["business_date"])
	}
	if msg.TargetID != "" {
		t.Errorf("target_id = %q, want empty: notification_requests.target_id is uuid-typed and farm/feed identity lives in context", msg.TargetID)
	}
	// The copy firewall: no internal vocabulary reaches a director's phone.
	for _, banned := range []string{"kg_key", "feed_item_key", "tenant", "payload", "API", "null"} {
		if strings.Contains(strings.ToLower(msg.Title+" "+msg.Body), strings.ToLower(banned)) {
			t.Errorf("visible copy leaked %q: title=%q body=%q", banned, msg.Title, msg.Body)
		}
	}
}

// The three desks that can act on a feed running out, and only those.
func TestLowStockAlertReachesCEOFeedAndProcurementOnly(t *testing.T) {
	_, _, queue, notifier := newLowStockFixture()
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock: %v", err)
	}
	got := map[string]bool{}
	for _, r := range queue.queued[0].Recipients {
		got[r.RoleLabel] = true
	}
	for _, want := range []string{roleLabelCEO, roleLabelFeedDirector, roleLabelProcurementDirector} {
		if !got[want] {
			t.Errorf("alert must reach %s; got %v", want, got)
		}
	}
	if got["pc_director"] {
		t.Errorf("the PC director does not buy feed and must not be on this alert: %v", got)
	}
	if len(queue.queued[0].Recipients) != 3 {
		t.Errorf("recipients = %d, want exactly the three seats", len(queue.queued[0].Recipients))
	}
}

// "Once per day" with no scheduler: the idempotency key carries the BUSINESS DATE, so every tick
// after the first writes the same key and the queue's own uniqueness drops it. A key that moved
// with the clock would send an alert every five minutes, all day.
func TestLowStockAlertKeyIsStableWithinTheBusinessDayAndTurnsOverAtMidnight(t *testing.T) {
	_, _, queue, notifier := newLowStockFixture()
	for _, at := range []time.Time{
		time.Date(2026, 8, 24, 4, 30, 0, 0, time.UTC),  // 10:00 IST
		time.Date(2026, 8, 24, 12, 45, 0, 0, time.UTC), // 18:15 IST, same business day
	} {
		notifier.WithClock(func() time.Time { return at })
		if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
			t.Fatalf("NotifyLowStock at %s: %v", at, err)
		}
	}
	if queue.queued[0].EventKey != queue.queued[1].EventKey {
		t.Errorf("two ticks of one business day produced different keys: %q vs %q",
			queue.queued[0].EventKey, queue.queued[1].EventKey)
	}
	notifier.WithClock(func() time.Time { return time.Date(2026, 8, 25, 4, 30, 0, 0, time.UTC) })
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock next day: %v", err)
	}
	if queue.queued[2].EventKey == queue.queued[0].EventKey {
		t.Errorf("the next business day must get its own key, got %q", queue.queued[2].EventKey)
	}
	if !strings.Contains(queue.queued[0].EventKey, biztime.BusinessDate(time.Date(2026, 8, 24, 4, 30, 0, 0, time.UTC))) {
		t.Errorf("key must carry the business date: %q", queue.queued[0].EventKey)
	}
}

// A feed per message: two low feeds are two alerts, each deep-linking to its own feed.
func TestLowStockAlertIsOnePerFarmAndFeed(t *testing.T) {
	reader, _, queue, notifier := newLowStockFixture()
	reader.feeds = append(reader.feeds, feeddomain.LowStockFeed{
		ParkID: missedPark, FarmLabel: "Channapatna", FeedItemLabel: "Dry Masoor Bhusa",
		FeedItemKey: "dry_masoor_bhusa", BalanceKg: "3518.0", AvgDailyKg: "447.1", DaysLeft: 6,
	})
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock: %v", err)
	}
	if len(queue.queued) != 2 {
		t.Fatalf("queued %d, want one per low feed", len(queue.queued))
	}
	if queue.queued[0].EventKey == queue.queued[1].EventKey {
		t.Errorf("two feeds shared one key, so the second would be dropped: %q", queue.queued[0].EventKey)
	}
	if queue.queued[1].Context["feed_item_key"] != "dry_masoor_bhusa" {
		t.Errorf("second alert must carry its own feed: %v", queue.queued[1].Context)
	}
}

// Nothing low, nothing sent: a daily alert that fires on an empty list trains people to ignore it.
func TestLowStockAlertSendsNothingWhenNoFeedIsLow(t *testing.T) {
	reader, _, queue, notifier := newLowStockFixture()
	reader.feeds = nil
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Errorf("queued %d messages with nothing low", len(queue.queued))
	}
}

// The purchase ledger keys by the legacy sheet's farm CODE ("CBE"), which is not what a director
// should read on a phone. With no resolver wired the code is still better than a blank, so the
// fallback is asserted here and the preference itself is asserted where the resolver exists.
func TestLowStockAlertFallsBackToTheFarmCodeWhenNoParkNameResolves(t *testing.T) {
	reader, _, queue, notifier := newLowStockFixture()
	reader.feeds[0].FarmLabel = "CBE"
	if err := notifier.NotifyLowStock(context.Background(), missedTenant); err != nil {
		t.Fatalf("NotifyLowStock: %v", err)
	}
	if !strings.Contains(queue.queued[0].Title, "CBE") {
		t.Errorf("with no park name the alert must still say where: %q", queue.queued[0].Title)
	}
	if queue.queued[0].Context["park_name"] != "CBE" {
		t.Errorf("park_name context = %q, want the farm code fallback", queue.queued[0].Context["park_name"])
	}
}
