package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	marketapp "github.com/vgoats/goatos/backend/internal/market/app"
	marketdomain "github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Morning market-survey push (maintainer decision 2026-09-14).
//
// The procurement director phones a handful of markets every morning for goat and sheep prices.
// This bridge is the reminder: on the first tick at or after the tenant's configured call time
// (Sales Config, default 08:00 IST -- the same instant the cards appear on the phone) it tells
// each market reporter which cities are still to be called today, and it says nothing once every
// city's card is done.
//
// ONCE PER DAY, without a private scheduler: the stage rides the shared operational cadence and the
// idempotency key carries the BUSINESS DATE, so the first tick after the cutoff writes and every
// later tick that day writes nothing (the feed low-stock shape). A reporter who finishes before
// 08:00 gets no push at all, because the notifier checks the day's cards first.
//
// PER PERSON: the audience is whoever holds an active market_reporter grant -- the same grant that
// opens the entry form -- resolved to their devices. No designation, no job.

// NotificationTypeMarketSurveyDue is the notification_requests.notification_type for the push.
const NotificationTypeMarketSurveyDue = "market_survey_due"

// MarketDayReader is the slice of the market service this bridge needs. GetDay applies the
// configured call time itself: before it the view is closed (Open=false), so the notifier
// needs no clock rule of its own -- the time the cards appear IS the time the push goes.
type MarketDayReader interface {
	GetDay(ctx context.Context, tenantID, businessDate string) (marketapp.DayView, error)
}

// MarketReporterReader lists the users holding the market_reporter grant.
type MarketReporterReader interface {
	ReporterUserIDs(ctx context.Context, tenantID string) ([]string, error)
}

// MarketSurveyNotifier queues the morning reminder.
type MarketSurveyNotifier struct {
	day        MarketDayReader
	reporters  MarketReporterReader
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

// NewMarketSurveyNotifier wires the bridge.
func NewMarketSurveyNotifier(day MarketDayReader, reporters MarketReporterReader, recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *MarketSurveyNotifier {
	return &MarketSurveyNotifier{day: day, reporters: reporters, recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

// WithClock pins the clock, for tests.
func (n *MarketSurveyNotifier) WithClock(now func() time.Time) *MarketSurveyNotifier {
	n.now = now
	return n
}

// NotifyDue queues today's reminder when the cutoff has passed and at least one city is pending.
func (n *MarketSurveyNotifier) NotifyDue(ctx context.Context, tenantID string) error {
	if n == nil || n.day == nil || n.reporters == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("market survey notification: tenant id is required")
	}
	businessDate := biztime.BusinessDate(n.now())
	view, err := n.day.GetDay(ctx, tenantID, businessDate)
	if err != nil {
		return fmt.Errorf("market survey notification: read day: %w", err)
	}
	if !view.Open {
		return nil
	}
	pending := make([]string, 0, len(view.Cards))
	for _, c := range view.Cards {
		if c.Status != marketdomain.CardDone {
			pending = append(pending, c.City.Name)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	userIDs, err := n.reporters.ReporterUserIDs(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("market survey notification: reporters: %w", err)
	}
	var recipients []calendarports.NotificationRecipient
	// Bounded by the handful of named reporters, never herd-sized.
	// scale-guard:ignore: bounded per-reporter device resolution, see above.
	for _, uid := range userIDs {
		devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, uid)
		if err != nil {
			return fmt.Errorf("market survey notification: resolve reporter: %w", err)
		}
		recipients = append(recipients, toQueueRecipients(devices, "market_reporter")...)
	}
	recipients = dedupeQueueRecipients(recipients)
	if len(recipients) == 0 {
		// Loud, and no fallback: a reminder nobody receives must not look sent.
		if n.logger != nil {
			n.logger.WarnContext(ctx, "market_survey_notification_no_recipients",
				"tenant_id", tenantID, "business_date", businessDate, "pending_cities", len(pending))
		}
		return nil
	}
	eventKey := "market.survey_due:" + businessDate
	title := fmt.Sprintf("Market calls due today · %d of %d cities pending", len(pending), len(view.Cards))
	body := fmt.Sprintf("Phone %s for today's goat and sheep prices (%s) and enter them in Procurement › Market.",
		joinCities(pending), biztime.FarmDateFromBusinessDate(businessDate))
	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  eventKey,
		TargetType:       "market_survey_day",
		TargetID:         "",
		NotificationType: NotificationTypeMarketSurveyDue,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":           "market_survey_due",
			"message_key":    "market.survey_due",
			"screen":         "market_survey",
			"href":           "/sales/market",
			"business_date":  businessDate,
			"pending_cities": fmt.Sprintf("%d", len(pending)),
			"total_cities":   fmt.Sprintf("%d", len(view.Cards)),
			"priority":       priorityHigh,
			"group_key":      "market_survey_due:" + tenantID,
			"collapse_key":   "market_survey_due:" + tenantID + ":" + businessDate,
		},
		Recipients: recipients,
	})
	if err != nil {
		return fmt.Errorf("market survey notification: queue: %w", err)
	}
	return nil
}

// joinCities renders "Chennai, Salem and Bangalore" -- the reader is owed the names, not a count.
func joinCities(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}
