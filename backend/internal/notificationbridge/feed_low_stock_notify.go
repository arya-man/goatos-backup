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

// Daily low-stock alert (maintainer decision 2026-08-24).
//
// A feed running out is not a task anybody was assigned, so nothing in the kernel was ever going to
// raise it: the Stock tab turns a card red and waits to be looked at. This bridge is the message,
// sent every day a farm's feed is inside LowStockNotifyDays of running out, to the three desks that
// can act on it -- the CEO/CXO, the Feed Director who runs the chain, and the Procurement Director
// who buys the load.
//
// ONE MESSAGE PER (farm, feed). The maintainer chose per-item over a digest so each alert deep-links
// to the feed it is about and can be actioned or dismissed on its own.
//
// ONCE PER DAY, without a private scheduler. The stage runs on the shared operational cadence and
// the idempotency key carries the BUSINESS DATE, so every tick after the first writes nothing --
// the queue's own (event, recipient device) uniqueness does the deduplication. That is also what
// makes redelivery safe.

// NotificationTypeFeedLowStock is the notification_requests.notification_type for the daily alert.
const NotificationTypeFeedLowStock = "feed_low_stock"

const (
	positionProcurementDirector  = "procurement_director"
	roleLabelFeedDirector        = "feed_director"
	roleLabelProcurementDirector = "procurement_director"
)

// LowStockReader is the slice of the feed analytics repository this bridge needs.
type LowStockReader interface {
	LowStockFeeds(ctx context.Context, tenantID string, withinDays int) ([]feeddomain.LowStockFeed, error)
}

// FeedLowStockNotifier queues the daily low-stock alerts.
type FeedLowStockNotifier struct {
	stock      LowStockReader
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
	// locations turns the purchase ledger's farm CODE into the park's name. The ledger keys by
	// "CBE"/"CPT" because the legacy sheet does; a director reading a phone at 09:00 should see
	// "Coimbatore". Optional: without it the alert falls back to the code rather than going blank.
	locations *LocationNameResolver
}

// WithLocationNames attaches park-name enrichment. Chainable at construction time.
func (n *FeedLowStockNotifier) WithLocationNames(resolver *LocationNameResolver) *FeedLowStockNotifier {
	n.locations = resolver
	return n
}

func NewFeedLowStockNotifier(
	stock LowStockReader,
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *FeedLowStockNotifier {
	return &FeedLowStockNotifier{stock: stock, recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

// WithClock pins the clock, for tests.
func (n *FeedLowStockNotifier) WithClock(now func() time.Time) *FeedLowStockNotifier {
	n.now = now
	return n
}

// NotifyLowStock queues one alert per (farm, feed) below the horizon.
func (n *FeedLowStockNotifier) NotifyLowStock(ctx context.Context, tenantID string) error {
	if n == nil || n.stock == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("feed low stock notification: tenant id is required")
	}
	low, err := n.stock.LowStockFeeds(ctx, tenantID, feeddomain.LowStockNotifyDays)
	if err != nil {
		return fmt.Errorf("feed low stock notification: read: %w", err)
	}
	if len(low) == 0 {
		return nil
	}

	// The three desks are resolved ONCE for the whole run, not per feed: the recipient set is the
	// same for every alert, and resolving it inside the loop would be one roster read per low feed.
	recipients, err := n.leadership(ctx, tenantID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		// Loud, and no fallback: an alert nobody receives must not look sent.
		if n.logger != nil {
			n.logger.WarnContext(ctx, "feed_low_stock_notification_no_recipients",
				"tenant_id", tenantID, "low_feeds", len(low))
		}
		return nil
	}

	businessDate := biztime.BusinessDate(n.now())
	visibleDate := biztime.FarmDateFromBusinessDate(businessDate)
	// ONE lookup for the whole run, not one per feed: the same two parks repeat down the list.
	parkNames := map[string]string{}
	if n.locations != nil {
		ids := make([]string, 0, len(low))
		for _, feed := range low {
			if feed.ParkID != "" {
				ids = append(ids, feed.ParkID)
			}
		}
		parkNames = n.locations.ResolveNames(ctx, tenantID, ids...)
	}
	for _, feed := range low {
		// The park's name when it resolves, the ledger's farm code when it does not -- never blank,
		// because "low at" with nothing after it tells a director nothing.
		parkName := feed.FarmLabel
		if resolved := strings.TrimSpace(parkNames[feed.ParkID]); resolved != "" {
			parkName = resolved
		}
		// The key carries the business date, so the first tick of the day writes and every later
		// tick writes nothing -- "once per day" with no scheduler and no state of its own.
		eventKey := fmt.Sprintf("feed.low_stock:%s:%s:%s", businessDate, feed.FarmLabel, feed.FeedItemKey)
		context := map[string]string{
			"type":            "feed_low_stock",
			"message_key":     "feed.low_stock.leadership",
			"screen":          "feed_stock",
			"href":            "/feed/analytics?tab=items",
			"park_id":         feed.ParkID,
			"farm_label":      feed.FarmLabel,
			"park_name":       parkName,
			"feed_item_key":   feed.FeedItemKey,
			"feed_item_label": feed.FeedItemLabel,
			"days_left":       fmt.Sprintf("%d", feed.DaysLeft),
			"balance_kg":      feed.BalanceKg,
			"avg_daily_kg":    feed.AvgDailyKg,
			// Structured fields stay ISO: the client parses these and renders its own string.
			"business_date": businessDate,
			"priority":      priorityHigh,
			"group_key":     "feed_low_stock:" + tenantID,
			"collapse_key":  "feed_low_stock:" + tenantID + ":" + feed.FarmLabel + ":" + feed.FeedItemKey,
		}
		// One write per LOW feed by design: the maintainer chose per-item alerts over a digest
		// (2026-08-24) so each deep-links to its own feed. Bounded by the purchase catalog x farms
		// (~19 rows on live data), never herd-sized, and only feeds actually running out get here.
		//
		// scale-guard:ignore: bounded per-item alert loop, see above.
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "feed_stock",
			TargetID:         feed.FarmLabel + ":" + feed.FeedItemKey,
			NotificationType: NotificationTypeFeedLowStock,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			// Every fact the reader needs to act: which farm, which feed, how long is left and how
			// much is in the store. An abstract "3 feeds are low" is the exact defect the
			// notification-specificity rule bans.
			Title: fmt.Sprintf("%s low at %s", feed.FeedItemLabel, parkName),
			Body: fmt.Sprintf("%s at %s has %s left — about %s days at %s per day. Checked %s.",
				feed.FeedItemLabel, parkName, kgPhrase(feed.BalanceKg), daysPhrase(feed.DaysLeft),
				kgPhrase(feed.AvgDailyKg), visibleDate),
			TraceID:    eventKey,
			EventKey:   eventKey,
			Context:    context,
			Recipients: recipients,
		}); err != nil {
			return fmt.Errorf("feed low stock notification: queue %s/%s: %w", feed.FarmLabel, feed.FeedItemKey, err)
		}
	}
	return nil
}

// leadership resolves the three tenant seats that can act on a low feed. A seat with no reachable
// device is skipped and logged; the alert still goes to the others rather than failing whole.
func (n *FeedLowStockNotifier) leadership(ctx context.Context, tenantID string) ([]calendarports.NotificationRecipient, error) {
	seats := []struct{ position, roleLabel string }{
		{positionCEOInternal, roleLabelCEO},
		{positionFeedDirector, roleLabelFeedDirector},
		{positionProcurementDirector, roleLabelProcurementDirector},
	}
	var out []calendarports.NotificationRecipient
	for _, seat := range seats {
		devices, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, seat.position)
		if err != nil {
			return nil, fmt.Errorf("feed low stock notification: resolve %s: %w", seat.position, err)
		}
		if len(devices) == 0 && n.logger != nil {
			n.logger.WarnContext(ctx, "feed_low_stock_notification_no_devices_for_seat",
				"tenant_id", tenantID, "position", seat.position)
			continue
		}
		out = append(out, toQueueRecipients(devices, seat.roleLabel)...)
	}
	return out, nil
}

// kgPhrase and daysPhrase keep the body in farm words: kilos and days, never a bare decimal.
func kgPhrase(raw string) string {
	return strings.TrimSuffix(strings.TrimSuffix(raw, "0"), ".") + " kg"
}

func daysPhrase(days int64) string {
	return fmt.Sprintf("%d", days)
}
