package notificationbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// Daily overdue-load alert (maintainer decision 2026-09-01).
//
// A load bought more than LoadAgeAlertDays ago whose animals are still on farm is capital standing
// in a shed. Nothing in the kernel was ever going to raise it: no task is overdue, no proof is
// missing, no protocol was broken -- the load is simply not selling, and the only place that shows
// is a chart somebody has to open. This bridge is the message.
//
// TO THE CXO, and only there (the maintainer's word). The Procurement Director buys loads and the
// Feed Director feeds them, but neither decides to hold or move stock; that call sits with the
// CEO/CXO desk, so a copy to the others would be noise they cannot act on. Widening the audience
// is a maintainer decision, exactly as it was for the feed low-stock alert's three seats.
//
// ONE MESSAGE PER LOAD, so each names its own vendor, park, age and head count and can be actioned
// on its own. The alternative -- a digest saying "3 loads are overdue" -- is precisely the
// abstract-notification defect the specificity rule bans.
//
// ONCE PER DAY, without a private scheduler. The stage rides the shared operational cadence and
// the idempotency key carries the BUSINESS DATE, so the first tick of the day writes and every
// later tick writes nothing. That is what makes "once a day" hold across a worker restart, a
// redeploy at noon, or both HA instances ticking together -- no state of its own, and redelivery
// is safe. Same mechanism as FeedLowStockNotifier; it is the house pattern for a daily alert.

// NotificationTypeLoadOverdue is the notification_requests.notification_type for the daily alert.
const NotificationTypeLoadOverdue = "procurement_load_overdue"

// OverdueLoadReader is the slice of the load-wise repository this bridge needs.
//
// It asks for the FINISHED read model rather than raw rows on purpose: the age clock, the
// remaining count and the landed cost are all derived in procurement/domain, and re-deriving any
// of them here would be a second implementation of a business number that the Purchase & barn
// screen also shows. The alert and the chart must never disagree about whether a load is overdue.
type OverdueLoadReader interface {
	LoadwiseSales(ctx context.Context, tenantID string, maxLoads int) (procurementdomain.LoadwiseSales, error)
}

// loadAgeScanLimit bounds the read. The alert is about the farm's open loads, and the load-wise
// read is newest-first, so this is the same window the screen serves rather than a herd-sized
// scan.
const loadAgeScanLimit = 200

// LoadAgeNotifier queues the daily overdue-load alerts.
type LoadAgeNotifier struct {
	loads      OverdueLoadReader
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

func NewLoadAgeNotifier(
	loads OverdueLoadReader,
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *LoadAgeNotifier {
	return &LoadAgeNotifier{loads: loads, recipients: recipients, queue: queue, logger: logger, now: time.Now}
}

// WithClock pins the clock, for tests.
func (n *LoadAgeNotifier) WithClock(now func() time.Time) *LoadAgeNotifier {
	n.now = now
	return n
}

// NotifyOverdueLoads queues one alert per load past the threshold that still holds animals.
func (n *LoadAgeNotifier) NotifyOverdueLoads(ctx context.Context, tenantID string) error {
	if n == nil || n.loads == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("load overdue notification: tenant id is required")
	}

	read, err := n.loads.LoadwiseSales(ctx, tenantID, loadAgeScanLimit)
	if err != nil {
		return fmt.Errorf("load overdue notification: read: %w", err)
	}
	overdue := procurementdomain.OverdueLoads(read.Loads)
	if len(overdue) == 0 {
		return nil
	}

	// Resolved ONCE for the whole run: the recipient set is the same for every load, and resolving
	// inside the loop would be one roster read per overdue load.
	devices, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, positionCEOInternal)
	if err != nil {
		return fmt.Errorf("load overdue notification: resolve %s: %w", positionCEOInternal, err)
	}
	recipients := toQueueRecipients(devices, roleLabelCEO)
	if len(recipients) == 0 {
		// Loud, and no fallback: an alert nobody receives must not look sent.
		if n.logger != nil {
			n.logger.WarnContext(ctx, "load_overdue_notification_no_recipients",
				"tenant_id", tenantID, "overdue_loads", len(overdue))
		}
		return nil
	}

	businessDate := biztime.BusinessDate(n.now())
	for _, load := range overdue {
		// The business date in the key IS the once-a-day property.
		eventKey := fmt.Sprintf("procurement.load_overdue:%s:%s", businessDate, load.LoadID)
		name := loadDisplayName(load)
		context := map[string]string{
			"type":        "procurement_load_overdue",
			"message_key": "procurement.load_overdue.leadership",
			"screen":      "sales_loads",
			"href":        "/sales/loads",
			"load_id":     load.LoadID,
			"load_ref":    load.LoadRef,
			"vendor_name": load.VendorName,
			"farm":        load.Farm,
			// Structured fields stay ISO/numeric: the client parses these and renders its own
			// string from them.
			"purchase_date":       load.PurchaseDate,
			"days_since_purchase": fmt.Sprintf("%d", load.DaysSincePurchase),
			"remaining":           fmt.Sprintf("%d", load.Remaining),
			"threshold_days":      fmt.Sprintf("%d", procurementdomain.LoadAgeAlertDays),
			"business_date":       businessDate,
			"priority":            priorityHigh,
			"group_key":           "procurement_load_overdue:" + tenantID,
			"collapse_key":        "procurement_load_overdue:" + tenantID + ":" + load.LoadID,
		}
		// One write per OVERDUE load by design (see the header). Bounded by loadAgeScanLimit and
		// in practice by how many loads the farm has open at once -- never herd-sized.
		//
		// scale-guard:ignore: bounded per-load alert loop, see above.
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  eventKey,
			TargetType:       "procurement_load",
			NotificationType: NotificationTypeLoadOverdue,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			// Every fact the CXO needs to act on it without opening anything: which load, from
			// whom, where, how old, and how many animals are still standing there.
			Title:      fmt.Sprintf("%s is %d days old", name, load.DaysSincePurchase),
			Body:       n.body(load, name),
			TraceID:    eventKey,
			EventKey:   eventKey,
			Context:    context,
			Recipients: recipients,
		}); err != nil {
			return fmt.Errorf("load overdue notification: queue %s: %w", load.LoadID, err)
		}
	}
	return nil
}

// body writes the farm sentence. Park and purchase date are included only when known -- a dangling
// "at " or "bought on " reads as a broken message, and the alert is still worth sending without
// them.
func (n *LoadAgeNotifier) body(load procurementdomain.OverdueLoad, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s still has %d animal", name, load.Remaining)
	if load.Remaining != 1 {
		b.WriteString("s")
	}
	b.WriteString(" on farm")
	if farm := strings.TrimSpace(load.Farm); farm != "" {
		fmt.Fprintf(&b, " at %s", farm)
	}
	fmt.Fprintf(&b, ", %d days after it was bought", load.DaysSincePurchase)
	if date := strings.TrimSpace(load.PurchaseDate); date != "" {
		fmt.Fprintf(&b, " on %s", biztime.FarmDateFromBusinessDate(date))
	}
	fmt.Fprintf(&b, ". Anything over %d days needs a decision.", procurementdomain.LoadAgeAlertDays)
	return b.String()
}

// loadDisplayName names the load the way the farm does: its own load number when it has one, the
// vendor's name when it does not, and a plain fallback rather than a uuid -- an id on a farm
// screen (or in a push) is banned copy.
func loadDisplayName(load procurementdomain.OverdueLoad) string {
	if ref := strings.TrimSpace(load.LoadRef); ref != "" {
		if vendor := strings.TrimSpace(load.VendorName); vendor != "" {
			return fmt.Sprintf("Load %s · %s", ref, vendor)
		}
		return "Load " + ref
	}
	if vendor := strings.TrimSpace(load.VendorName); vendor != "" {
		return vendor + "'s load"
	}
	return "A purchased load"
}
