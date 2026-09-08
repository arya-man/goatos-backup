package notificationbridge

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

const saleFeedDeal = "77777777-7777-4777-8777-777777777771"

var saleFeedIST = time.FixedZone("IST", 5*3600+1800)

type saleBatchReaderFake struct {
	batches []identityports.SaleAllocationBatch
	since   []time.Time
}

func (f *saleBatchReaderFake) ListRecentSaleAllocationBatches(_ context.Context, _ string, since time.Time) ([]identityports.SaleAllocationBatch, error) {
	f.since = append(f.since, since)
	return f.batches, nil
}

type feedClockFake struct {
	clocks []feeddomain.WorkflowClock
	parks  []string
}

func (f *feedClockFake) ListScheduleClocks(_ context.Context, _, parkID string, _ time.Time) ([]feeddomain.WorkflowClock, error) {
	f.parks = append(f.parks, parkID)
	return f.clocks, nil
}

func saleFeedBatch(allocatedAt time.Time) identityports.SaleAllocationBatch {
	return identityports.SaleAllocationBatch{
		SalesDealID: saleFeedDeal, AllocatedAt: allocatedAt, Animals: 12,
		Pens: []identityports.SaleAllocationPen{
			{ParkID: missedPark, ParkName: "Coimbatore", ShedID: "s1", ShedName: "Castro", PartitionLabel: "1", OperationalLocationDisplay: "Castro 1", Animals: 8},
			{ParkID: missedPark, ParkName: "Coimbatore", ShedID: "s2", ShedName: "Mandela 1", PartitionLabel: "Part 2", OperationalLocationDisplay: "Mandela 1 - Part 2", Animals: 4},
		},
	}
}

func newSaleFeedFixture(now time.Time) (*saleBatchReaderFake, *feedClockFake, *missedRecipientsFake, *missedQueueFake, *SaleFeedReduceNotifier) {
	recipients := &missedRecipientsFake{byPosition: map[string][]workforcedomain.NotificationRecipient{
		"tenant|" + missedTenant + "|feed_director": {{WorkforceMemberID: "m-fd", DeviceID: "d-fd", FCMToken: "fcm-feed"}},
		// Reachable and must NOT be picked: the maintainer chose the Feed Director alone.
		"tenant|" + missedTenant + "|ceo_internal": {{WorkforceMemberID: "m-ceo", DeviceID: "d-ceo", FCMToken: "fcm-ceo"}},
		"tenant|" + missedTenant + "|pc_director":  {{WorkforceMemberID: "m-pc", DeviceID: "d-pc", FCMToken: "fcm-pc"}},
	}}
	queue := &missedQueueFake{}
	batches := &saleBatchReaderFake{}
	clocks := &feedClockFake{clocks: []feeddomain.WorkflowClock{
		{Workflow: feeddomain.WorkflowExperiment, DirectionTime: "09:00:00", CorrectionTime: "13:00:00"},
		{Workflow: feeddomain.WorkflowNormal, DirectionTime: "09:00:00", CorrectionTime: "14:00:00"},
	}}
	notifier := NewSaleFeedReduceNotifier(recipients, queue, nil).
		WithBatches(batches).WithFeedClocks(clocks).
		WithClock(func() time.Time { return now })
	return batches, clocks, recipients, queue, notifier
}

func saleAllocatedEvent(t *testing.T, batch identityports.SaleAllocationBatch) eventbus.Event {
	t.Helper()
	pens := make([]map[string]any, 0, len(batch.Pens))
	for _, pen := range batch.Pens {
		pens = append(pens, map[string]any{
			"park_id": pen.ParkID, "park_name": pen.ParkName, "shed_id": pen.ShedID, "shed_name": pen.ShedName,
			"partition_label": pen.PartitionLabel, "operational_location_display": pen.OperationalLocationDisplay,
			"animals": pen.Animals,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"tenant_id": missedTenant, "sales_deal_id": batch.SalesDealID,
		"allocated_at": batch.AllocatedAt.UTC().Format(time.RFC3339Nano),
		"animals":      batch.Animals, "pens": pens,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{ID: "evt-1", Type: EventGoatSaleAllocated, TenantID: missedTenant, Payload: payload}
}

// THE NOTICE: fired off the confirm event, it names the park, every pen with how many animals left
// it, the sale date, and the feed day the reduction lands on -- and goes to the Feed Director alone.
func TestSaleFeedNoticeNamesParkPensCountsAndTheFeedDay(t *testing.T) {
	sold := time.Date(2026, 9, 7, 11, 0, 0, 0, saleFeedIST) // before the 14:00 cutoff -> feed day 08/09
	_, clocks, recipients, queue, notifier := newSaleFeedFixture(sold)

	if err := notifier.HandleEvent(context.Background(), saleAllocatedEvent(t, saleFeedBatch(sold))); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d, want 1", len(queue.queued))
	}
	got := queue.queued[0]
	if got.NotificationType != NotificationTypeFeedSaleReduce {
		t.Fatalf("type = %q", got.NotificationType)
	}
	if got.Title != "Sale confirmed: reduce feed for 2 pens at Coimbatore" {
		t.Fatalf("title = %q", got.Title)
	}
	wantBody := "12 animals sold from Castro 1 (8) and Mandela 1 - Part 2 (4) at Coimbatore on 07/09/2026. Feed for these pens should reduce from 08/09/2026."
	if got.Body != wantBody {
		t.Fatalf("body = %q\nwant  %q", got.Body, wantBody)
	}
	if len(got.Recipients) != 1 || got.Recipients[0].DeviceID != "d-fd" || got.Recipients[0].RoleLabel != "feed_director" {
		t.Fatalf("recipients = %+v, want the feed director alone", got.Recipients)
	}
	for _, asked := range recipients.asked {
		if strings.Contains(asked, "ceo_internal") || strings.Contains(asked, "pc_director") {
			t.Fatalf("asked for %s; the notice is the feed director's alone", asked)
		}
	}
	if got.Context["feed_day"] != "2026-09-08" || got.Context["sale_date"] != "2026-09-07" || got.Context["park_id"] != missedPark {
		t.Fatalf("context = %+v", got.Context)
	}
	if got.EventKey != "feed.sale_reduce:"+saleFeedDeal+":"+itoa64(sold.UnixMicro()) {
		t.Fatalf("event key = %q", got.EventKey)
	}
	if len(clocks.parks) != 1 || clocks.parks[0] != missedPark {
		t.Fatalf("clock read for parks %v, want the batch's park once", clocks.parks)
	}
}

func TestSaleFeedConfiguredParkDeskResolvesAgainstTheSoldPensPark(t *testing.T) {
	sold := time.Date(2026, 9, 7, 11, 0, 0, 0, saleFeedIST)
	_, _, _, queue, notifier := newSaleFeedFixture(sold)
	audience := &feedAudienceSpy{}
	notifier.WithAudience(audience)
	if err := notifier.HandleEvent(context.Background(), saleAllocatedEvent(t, saleFeedBatch(sold))); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(audience.parks) != 1 || audience.parks[0] != missedPark {
		t.Fatalf("audience resolved parks %v, want the sale pens' park", audience.parks)
	}
	if len(queue.queued) != 1 || len(queue.queued[0].Recipients) != 1 || queue.queued[0].Recipients[0].RoleLabel != roleLabelParkHead {
		t.Fatalf("queued recipients = %+v, want the configured park desk", queue.queued)
	}
}

// The feed day follows the park's correction cutoff: a sale after 14:00 cannot reach tomorrow's
// frozen sheet, so the notice names the day after.
func TestSaleFeedNoticeAfterTheCutoffNamesTheDayAfterNext(t *testing.T) {
	sold := time.Date(2026, 9, 7, 15, 30, 0, 0, saleFeedIST)
	_, _, _, queue, notifier := newSaleFeedFixture(sold)
	if err := notifier.HandleEvent(context.Background(), saleAllocatedEvent(t, saleFeedBatch(sold))); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 1 || !strings.HasSuffix(queue.queued[0].Body, "should reduce from 09/09/2026.") {
		t.Fatalf("queued = %+v", queue.queued)
	}
	if queue.queued[0].Context["feed_day"] != "2026-09-09" {
		t.Fatalf("feed_day = %q", queue.queued[0].Context["feed_day"])
	}
}

// THE REMINDER: nothing before the feed day, nothing before 07:00 on it, then exactly one push
// whose key is stable across ticks -- "once" comes from the key, not from state.
func TestSaleFeedReminderFiresOnTheFeedDayMorningWithAStableKey(t *testing.T) {
	sold := time.Date(2026, 9, 7, 11, 0, 0, 0, saleFeedIST) // feed day 2026-09-08
	batches, _, _, queue, notifier := newSaleFeedFixture(sold)
	batches.batches = []identityports.SaleAllocationBatch{saleFeedBatch(sold)}

	ticks := []struct {
		at   time.Time
		want int
	}{
		{time.Date(2026, 9, 7, 23, 55, 0, 0, saleFeedIST), 0}, // still the sale day
		{time.Date(2026, 9, 8, 0, 5, 0, 0, saleFeedIST), 0},   // feed day, but 00:05
		{time.Date(2026, 9, 8, 7, 0, 0, 0, saleFeedIST), 1},   // feed day, 07:00: push
		{time.Date(2026, 9, 8, 7, 5, 0, 0, saleFeedIST), 2},   // next tick: same key again (queue dedups)
		{time.Date(2026, 9, 9, 9, 0, 0, 0, saleFeedIST), 3},   // a day later, still inside the window: same key
	}
	for _, tick := range ticks {
		now := tick.at
		notifier.WithClock(func() time.Time { return now })
		if err := notifier.RemindDue(context.Background(), missedTenant); err != nil {
			t.Fatalf("RemindDue at %s: %v", tick.at, err)
		}
		if len(queue.queued) != tick.want {
			t.Fatalf("at %s queued %d, want %d", tick.at.Format(time.RFC3339), len(queue.queued), tick.want)
		}
	}
	first := queue.queued[0]
	if first.NotificationType != NotificationTypeFeedSaleReduceReminder {
		t.Fatalf("type = %q", first.NotificationType)
	}
	if first.Title != "Did feed reduce for 2 pens at Coimbatore?" {
		t.Fatalf("title = %q", first.Title)
	}
	wantBody := "12 animals were sold from Castro 1 (8) and Mandela 1 - Part 2 (4) at Coimbatore on 07/09/2026. Confirm their feed has reduced from 08/09/2026."
	if first.Body != wantBody {
		t.Fatalf("body = %q\nwant  %q", first.Body, wantBody)
	}
	for _, later := range queue.queued[1:] {
		if later.EventKey != first.EventKey {
			t.Fatalf("reminder key drifted: %q vs %q", later.EventKey, first.EventKey)
		}
	}
	if first.EventKey == "feed.sale_reduce:"+saleFeedDeal+":"+itoa64(sold.UnixMicro()) {
		t.Fatal("the reminder must not reuse the notice's key, or the queue would swallow it")
	}
	// The read is a bounded trailing window, never the whole table.
	if len(batches.since) == 0 || batches.since[0].After(ticks[0].at) || ticks[0].at.Sub(batches.since[0]) > saleFeedReminderWindow {
		t.Fatalf("since = %v for now %v", batches.since, ticks[0].at)
	}
}

// Many pens stay readable: the push names a handful and counts the rest.
func TestSaleFeedPenListCapsTheNamedPens(t *testing.T) {
	pens := make([]identityports.SaleAllocationPen, 0, 9)
	for i := 1; i <= 9; i++ {
		pens = append(pens, identityports.SaleAllocationPen{OperationalLocationDisplay: "Pen " + itoa64(int64(i)), Animals: i})
	}
	got := saleFeedPenList(pens)
	if !strings.HasPrefix(got, "Pen 1 (1), Pen 2 (2)") || !strings.HasSuffix(got, "Pen 6 (6) and 3 more pens") {
		t.Fatalf("pen list = %q", got)
	}
	if got := saleFeedPenList(pens[:1]); got != "Pen 1 (1)" {
		t.Fatalf("single pen = %q", got)
	}
	if got := saleFeedPenList(pens[:7]); !strings.HasSuffix(got, "and 1 more pen") {
		t.Fatalf("seven pens = %q", got)
	}
}

// Nobody to address: log and queue nothing, never fail the event (a retry would change nothing).
func TestSaleFeedNoticeWithNoFeedDirectorQueuesNothing(t *testing.T) {
	sold := time.Date(2026, 9, 7, 11, 0, 0, 0, saleFeedIST)
	_, _, recipients, queue, notifier := newSaleFeedFixture(sold)
	delete(recipients.byPosition, "tenant|"+missedTenant+"|feed_director")
	if err := notifier.HandleEvent(context.Background(), saleAllocatedEvent(t, saleFeedBatch(sold))); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued %+v, want nothing", queue.queued)
	}
}

// Foreign or empty events are ignored; a malformed payload is permanent, not retried forever.
func TestSaleFeedNoticeIgnoresOtherEventsAndRejectsGarbage(t *testing.T) {
	sold := time.Date(2026, 9, 7, 11, 0, 0, 0, saleFeedIST)
	_, _, _, queue, notifier := newSaleFeedFixture(sold)
	if err := notifier.HandleEvent(context.Background(), eventbus.Event{Type: "goat.exited", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := notifier.HandleEvent(context.Background(), eventbus.Event{Type: EventGoatSaleAllocated, TenantID: missedTenant, Payload: []byte(`{"sales_deal_id":"x","pens":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queued %+v", queue.queued)
	}
	err := notifier.HandleEvent(context.Background(), eventbus.Event{Type: EventGoatSaleAllocated, Payload: []byte(`{`)})
	if err == nil || !eventbus.IsPermanentError(err) {
		t.Fatalf("garbage payload: err = %v, want permanent", err)
	}
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
