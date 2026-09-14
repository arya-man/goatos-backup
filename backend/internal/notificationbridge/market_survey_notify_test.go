package notificationbridge

import (
	"context"
	"strings"
	"testing"
	"time"

	marketapp "github.com/vgoats/goatos/backend/internal/market/app"
	marketdomain "github.com/vgoats/goatos/backend/internal/market/domain"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// marketTestRecipients gives every reporter their own device; the other resolver methods are
// never called by this bridge.
type marketTestRecipients struct{}

func (marketTestRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (marketTestRecipients) ResolvePositionRecipients(context.Context, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}

func (marketTestRecipients) ResolveMemberRecipients(_ context.Context, _ string, memberID string) ([]workforcedomain.NotificationRecipient, error) {
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: memberID, DeviceID: "device-" + memberID, FCMToken: "token-" + memberID}}, nil
}

type stubMarketDay struct{ view marketapp.DayView }

func (s stubMarketDay) GetDay(context.Context, string, string) (marketapp.DayView, error) {
	return s.view, nil
}

type stubReporters struct{ ids []string }

func (s stubReporters) ReporterUserIDs(context.Context, string) ([]string, error) { return s.ids, nil }

func marketView(pending, done int) marketapp.DayView {
	v := marketapp.DayView{Pending: pending, Done: done}
	names := []string{"Chennai", "Salem", "Madurai", "Erode"}
	for i := 0; i < pending+done; i++ {
		status := marketdomain.CardPending
		if i >= pending {
			status = marketdomain.CardDone
		}
		v.Cards = append(v.Cards, marketdomain.DayCard{City: marketdomain.City{ID: names[i], Name: names[i]}, Status: status})
	}
	return v
}

// Morning market reminder (maintainer decision 2026-09-14): sent to the named reporters at or
// after 08:00 IST, naming the cities still to call, and not at all once every card is done.
func TestMarketSurveyNotifierNamesPendingCitiesAfterTheCutoff(t *testing.T) {
	queue := &recordingQueue{}
	n := NewMarketSurveyNotifier(stubMarketDay{marketView(2, 1)}, stubReporters{[]string{"u1"}}, marketTestRecipients{}, queue, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-14 08:05") })
	if err := n.NotifyDue(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 1 {
		t.Fatalf("queued = %d, want 1", len(queue.queued))
	}
	got := queue.queued[0]
	if got.NotificationType != NotificationTypeMarketSurveyDue || got.EventKey != "market.survey_due:2026-09-14" {
		t.Fatalf("type/key = %s / %s", got.NotificationType, got.EventKey)
	}
	if !strings.Contains(got.Title, "2 of 3") || !strings.Contains(got.Body, "Chennai and Salem") || strings.Contains(got.Body, "Madurai") {
		t.Fatalf("copy names the wrong cities: %q / %q", got.Title, got.Body)
	}
	if got.Context["href"] != "/vendors/market" || len(got.Recipients) != 1 {
		t.Fatalf("context/recipients = %v / %d", got.Context, len(got.Recipients))
	}
}

func TestMarketSurveyNotifierIsSilentBeforeTheCutoffAndWhenEveryCityIsDone(t *testing.T) {
	queue := &recordingQueue{}
	early := NewMarketSurveyNotifier(stubMarketDay{marketView(2, 0)}, stubReporters{[]string{"u1"}}, marketTestRecipients{}, queue, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-14 07:59") })
	if err := early.NotifyDue(context.Background(), "tenant-1"); err != nil || len(queue.queued) != 0 {
		t.Fatalf("before the cutoff: queued %d, err %v", len(queue.queued), err)
	}
	done := NewMarketSurveyNotifier(stubMarketDay{marketView(0, 3)}, stubReporters{[]string{"u1"}}, marketTestRecipients{}, queue, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-14 09:00") })
	if err := done.NotifyDue(context.Background(), "tenant-1"); err != nil || len(queue.queued) != 0 {
		t.Fatalf("every city done: queued %d, err %v", len(queue.queued), err)
	}
	// No cities configured at all: nothing to phone, nothing to say.
	none := NewMarketSurveyNotifier(stubMarketDay{marketView(0, 0)}, stubReporters{[]string{"u1"}}, marketTestRecipients{}, queue, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-14 09:00") })
	if err := none.NotifyDue(context.Background(), "tenant-1"); err != nil || len(queue.queued) != 0 {
		t.Fatalf("no cities: queued %d, err %v", len(queue.queued), err)
	}
}
