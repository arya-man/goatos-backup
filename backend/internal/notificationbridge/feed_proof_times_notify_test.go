package notificationbridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type stubProofTimesReader struct {
	reports []feeddomain.FeedProofTimesReport
	err     error
	calls   int
	feedDay time.Time
}

func (s *stubProofTimesReader) FeedProofTimesByPark(_ context.Context, _ string, feedDay time.Time) ([]feeddomain.FeedProofTimesReport, error) {
	s.calls++
	s.feedDay = feedDay
	return s.reports, s.err
}

type recordingQueue struct {
	queued []calendarports.QueueRoleNotifications
}

func (q *recordingQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	q.queued = append(q.queued, in)
	return len(in.Recipients), nil
}

func istTime(t *testing.T, clock string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", clock, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse %q: %v", clock, err)
	}
	return parsed
}

func twoParkReports() []feeddomain.FeedProofTimesReport {
	return []feeddomain.FeedProofTimesReport{
		{ParkID: "park-cbe", ParkName: "Coimbatore", FeedDay: "2026-09-05", Rows: []feeddomain.PenSessionProofTimes{
			{ShedName: "Castro", PartitionLabel: "1", SessionNo: 1, SessionLabel: "Morning"},
		}},
		{ParkID: "park-cpt", ParkName: "Channapatna", FeedDay: "2026-09-05", Rows: []feeddomain.PenSessionProofTimes{
			{ShedName: "Godel 1", PartitionLabel: "Part 3", SessionNo: 1, SessionLabel: "Morning"},
		}},
	}
}

const testSlackChannel = "C0BV1GXCX8B"

// The cutoff is the feature: nothing may be posted before 17:30 IST, because the evening session is
// still being captured and a report sent at noon would call complete work missing.
func TestNothingIsPostedBeforeTheCutoff(t *testing.T) {
	reader := &stubProofTimesReader{reports: twoParkReports()}
	queue := &recordingQueue{}
	notifier := NewFeedProofTimesNotifier(reader, queue, testSlackChannel, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-05 17:29") })

	if err := notifier.NotifyFeedProofTimes(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Errorf("nothing may be queued before 17:30 IST, queued %d", len(queue.queued))
	}
	if reader.calls != 0 {
		t.Errorf("the report must not even be read before the cutoff, read %d times", reader.calls)
	}
}

// At the cutoff, ONE message per park (the maintainer's choice), each addressed to the Slack channel
// and carrying its own park's table.
func TestOneMessagePerParkIsPostedAtTheCutoff(t *testing.T) {
	reader := &stubProofTimesReader{reports: twoParkReports()}
	queue := &recordingQueue{}
	notifier := NewFeedProofTimesNotifier(reader, queue, testSlackChannel, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-05 17:30") })

	if err := notifier.NotifyFeedProofTimes(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 2 {
		t.Fatalf("want one message per park, got %d", len(queue.queued))
	}
	if got := reader.feedDay.Format("2006-01-02"); got != "2026-09-05" {
		t.Errorf("the report must be read for TODAY's feed day, got %s", got)
	}
	seen := map[string]bool{}
	for _, queued := range queue.queued {
		if queued.Channel != channelSlack {
			t.Errorf("channel = %q, want slack", queued.Channel)
		}
		if len(queued.Recipients) != 1 || queued.Recipients[0].FCMToken != testSlackChannel {
			t.Fatalf("the destination address must be the Slack channel id, got %+v", queued.Recipients)
		}
		// The event key carries the BUSINESS DATE, which is the once-a-day property: the queue
		// dedupes on it, so every later tick of the same day writes nothing.
		if !strings.Contains(queued.EventKey, "2026-09-05") {
			t.Errorf("event key must carry the business date, got %q", queued.EventKey)
		}
		seen[queued.Context["park_id"]] = true
	}
	if !seen["park-cbe"] || !seen["park-cpt"] {
		t.Errorf("both parks must be posted, got %v", seen)
	}
	if !strings.Contains(queue.queued[0].Title, "Coimbatore") {
		t.Errorf("each message must name its own park, got %q", queue.queued[0].Title)
	}
	if !strings.Contains(queue.queued[0].Body, "```diff") {
		t.Errorf("the body must carry the diff-fenced table:\n%s", queue.queued[0].Body)
	}
}

// The same day's key is stable across ticks. The queue's own (tenant, idempotency key) uniqueness is
// what actually suppresses the duplicate; this pins that the key we hand it does not move.
func TestTheEventKeyIsStableAcrossTicksOfTheSameDay(t *testing.T) {
	first := &recordingQueue{}
	second := &recordingQueue{}
	for _, tc := range []struct {
		clock string
		queue *recordingQueue
	}{{"2026-09-05 17:31", first}, {"2026-09-05 19:05", second}} {
		notifier := NewFeedProofTimesNotifier(&stubProofTimesReader{reports: twoParkReports()}, tc.queue, testSlackChannel, nil).
			WithClock(func() time.Time { return istTime(t, tc.clock) })
		if err := notifier.NotifyFeedProofTimes(context.Background(), "tenant-1"); err != nil {
			t.Fatalf("notify at %s: %v", tc.clock, err)
		}
	}
	if first.queued[0].EventKey != second.queued[0].EventKey {
		t.Errorf("the same day must reuse one key, got %q then %q",
			first.queued[0].EventKey, second.queued[0].EventKey)
	}
	if first.queued[0].Recipients[0].DeviceID != second.queued[0].Recipients[0].DeviceID {
		t.Errorf("the recipient key must be stable, or the queue cannot dedupe the second tick")
	}
}

// An unconfigured channel posts nothing at all rather than queueing rows no gateway can deliver.
func TestAnUnconfiguredChannelPostsNothing(t *testing.T) {
	queue := &recordingQueue{}
	notifier := NewFeedProofTimesNotifier(&stubProofTimesReader{reports: twoParkReports()}, queue, "  ", nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-05 18:00") })

	if err := notifier.NotifyFeedProofTimes(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(queue.queued) != 0 {
		t.Errorf("no channel means no rows, queued %d", len(queue.queued))
	}
}

// A read failure is reported, never swallowed into a silent no-post.
func TestAReadFailureIsReported(t *testing.T) {
	notifier := NewFeedProofTimesNotifier(
		&stubProofTimesReader{err: errors.New("boom")}, &recordingQueue{}, testSlackChannel, nil).
		WithClock(func() time.Time { return istTime(t, "2026-09-05 18:00") })

	if err := notifier.NotifyFeedProofTimes(context.Background(), "tenant-1"); err == nil {
		t.Fatal("a failed read must surface, not read as a quiet day")
	}
}
