package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The 17:30 feed-proof-times Slack post (maintainer decision 2026-09-05).
//
// Every feed day each pen owes three captures per session -- feed weight, feed distribution, water --
// and today the only way to see whether they arrived is to open the verifier queue pen by pen. This
// bridge posts the whole day as one table per park, at 17:30 IST, into the farm's Slack channel.
//
// ONE MESSAGE PER PARK (the maintainer's choice). CBE and CPT are run by different people, the table
// is long enough with both sessions of every pen, and one park's sheet arriving late must not hold
// the other park's report.
//
// ONCE PER DAY, without a private scheduler, exactly as FeedLowStockNotifier does it: the stage rides
// the shared operational cadence and the BUSINESS DATE is in the idempotency key, so the first tick
// after the cutoff writes and every later tick that day writes nothing. That is what makes "once"
// hold across a worker restart, a redeploy at 18:00, or both HA instances ticking together -- no
// state of its own, and redelivery is safe.
//
// THE CUTOFF IS A GATE, NOT A SCHEDULE. The cadence ticks every 5 minutes; this notifier simply
// declines to write before 17:30 IST. So the post lands on the first tick at or after 17:30 (in
// practice 17:30-17:35), and a worker that was down at 17:30 posts as soon as it is back rather than
// skipping the day. A late post is worth having; a missing one is not.

const (
	// channelSlack is the notification_requests.channel this post is delivered on. It is already an
	// allowed channel value (baseline CHECK) and already has a gateway path.
	channelSlack = "slack"
	// roleLabelSlackChannel is descriptive only: it lands in notification_requests.context for
	// triage and never affects delivery, which is driven entirely by recipient_ref.
	roleLabelSlackChannel = "slack_channel"
)

// NotificationTypeFeedProofTimes is the notification_requests.notification_type for the daily post.
const NotificationTypeFeedProofTimes = "feed_proof_times_daily"

// FeedProofTimesCutoff is the IST time of day from which the report may be posted. It sits after the
// 15:00 evening serving slot with enough margin for the last uploads to land.
var FeedProofTimesCutoff = struct{ Hour, Minute int }{Hour: 17, Minute: 30}

// FeedProofTimesReader is the slice of the feed-direction repository this bridge needs.
//
// It asks for the FINISHED per-park reports rather than raw completion rows, for the same reason the
// overdue-load bridge does: which pens were expected, which capture is missing and how a pen is
// named are all resolved once, in the read model, so the Slack table and any future screen of the
// same facts cannot disagree.
type FeedProofTimesReader interface {
	FeedProofTimesByPark(ctx context.Context, tenantID string, feedDay time.Time) ([]feeddomain.FeedProofTimesReport, error)
}

// FeedProofTimesNotifier queues the daily Slack post, one per park.
type FeedProofTimesNotifier struct {
	reports FeedProofTimesReader
	queue   NotificationQueue
	logger  *slog.Logger
	now     func() time.Time
	// slackChannelID is the destination channel. Empty disables the notifier entirely -- an
	// unconfigured environment must post nothing rather than queue rows no gateway can deliver.
	slackChannelID string
}

func NewFeedProofTimesNotifier(
	reports FeedProofTimesReader,
	queue NotificationQueue,
	slackChannelID string,
	logger *slog.Logger,
) *FeedProofTimesNotifier {
	return &FeedProofTimesNotifier{
		reports:        reports,
		queue:          queue,
		slackChannelID: strings.TrimSpace(slackChannelID),
		logger:         logger,
		now:            time.Now,
	}
}

// WithClock pins the clock, for tests.
func (n *FeedProofTimesNotifier) WithClock(now func() time.Time) *FeedProofTimesNotifier {
	n.now = now
	return n
}

// NotifyFeedProofTimes posts today's feed-proof-times table, one message per park, once the cutoff
// has passed. Before the cutoff it is a no-op.
func (n *FeedProofTimesNotifier) NotifyFeedProofTimes(ctx context.Context, tenantID string) error {
	if n == nil || n.reports == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("feed proof times notification: tenant id is required")
	}
	if n.slackChannelID == "" {
		// Silent by construction: a farm that has not wired the channel gets no rows, not failures.
		return nil
	}

	now := n.now()
	if !FeedProofTimesCutoffPassed(now) {
		return nil
	}

	businessDate := biztime.BusinessDate(now)
	feedDay := biztime.BusinessDayStart(now)
	reports, err := n.reports.FeedProofTimesByPark(ctx, tenantID, feedDay)
	if err != nil {
		return fmt.Errorf("feed proof times notification: read: %w", err)
	}
	if len(reports) == 0 {
		// No park issued a sheet today. Nothing was planned, so there is nothing to report on --
		// posting "no pens" for a farm that never fed would be noise, not information.
		if n.logger != nil {
			n.logger.InfoContext(ctx, "feed_proof_times_no_live_sheet",
				"tenant_id", tenantID, "business_date", businessDate)
		}
		return nil
	}

	location := biztime.DefaultLocation()
	for _, report := range reports {
		// The business date in the key IS the once-a-day property; the park id keeps the two parks'
		// posts independent of each other.
		eventKey := fmt.Sprintf("feed.proof_times:%s:%s", businessDate, report.ParkID)
		context := map[string]string{
			"type":          "feed_proof_times_daily",
			"message_key":   "feed.proof_times.daily",
			"screen":        "feed_direction",
			"href":          "/feed/direction",
			"park_id":       report.ParkID,
			"park_name":     report.ParkName,
			"business_date": businessDate,
			// Structured, so a reader of the row can count without re-parsing the table text. Both
			// grains are carried because the table counts PENS while the underlying facts are
			// pen-SESSIONS, and a reader that assumed one from the other would be wrong.
			"pens":                 fmt.Sprintf("%d", len(report.Pens())),
			"pens_missing":         fmt.Sprintf("%d", report.MissingPenCount()),
			"pen_sessions":         fmt.Sprintf("%d", len(report.Rows)),
			"pen_sessions_missing": fmt.Sprintf("%d", report.MissingCount()),
			"priority":             priorityNormal,
			"group_key":            "feed_proof_times:" + tenantID,
			"collapse_key":         "feed_proof_times:" + tenantID + ":" + report.ParkID,
		}
		// One write per PARK by design (see the header). Two parks today; bounded by the tenant's
		// park catalog, which is physical infrastructure.
		//
		// scale-guard:ignore: bounded per-park post loop, see above.
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "feed_direction_park_day",
			TargetID:         report.ParkID,
			NotificationType: NotificationTypeFeedProofTimes,
			Channel:          channelSlack,
			Priority:         priorityNormal,
			Title:            feeddomain.FeedProofTimesTitle(report),
			Body:             feeddomain.FeedProofTimesSlackBody(report, location),
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context:          context,
			Recipients:       []calendarports.NotificationRecipient{n.slackRecipient()},
		}); err != nil {
			return fmt.Errorf("feed proof times notification: queue %s: %w", report.ParkID, err)
		}
	}
	return nil
}

// slackRecipient addresses the channel the way every other channel addresses its destination:
// recipient_ref carries the delivery address, which for Slack is the channel id (for FCM it is the
// device token). DeviceID is what the queue's idempotency key is built from, so it must be stable
// and distinct per destination -- it is the channel id under a scheme prefix, never a real device.
func (n *FeedProofTimesNotifier) slackRecipient() calendarports.NotificationRecipient {
	return calendarports.NotificationRecipient{
		DeviceID:  "slack:" + n.slackChannelID,
		FCMToken:  n.slackChannelID,
		RoleLabel: roleLabelSlackChannel,
	}
}

// FeedProofTimesCutoffPassed reports whether now is at or after the cutoff on its own IST business
// day. Exported so the stage and its tests read the same rule.
func FeedProofTimesCutoffPassed(now time.Time) bool {
	local := now.In(biztime.DefaultLocation())
	cutoff := time.Date(local.Year(), local.Month(), local.Day(),
		FeedProofTimesCutoff.Hour, FeedProofTimesCutoff.Minute, 0, 0, biztime.DefaultLocation())
	return !local.Before(cutoff)
}
