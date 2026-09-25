package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Sale -> Feed Director (maintainer decision 2026-09-07).
//
// Animals tagged to a sale leave the register, and the pens they stood in need less feed from the
// next sheet. Nothing told the Feed Director: the sheet quietly shrank a head count and the packer
// found a smaller bag. Two pushes now go to the Feed Director, and ONLY the Feed Director (the
// maintainer's choice over park heads and the CEO):
//
//  1. THE NOTICE, the moment the sale is confirmed -- consumed from goat.sale_allocated, which the
//     confirm transaction emits once per batch with the pen-by-pen breakdown already attached.
//     "Sale confirmed: 12 animals sold from Castro 1 (8) and Mandela 1 - Part 2 (4) at Coimbatore.
//     Feed for these pens should reduce from 08/09/2026."
//  2. THE REMINDER, on that feed day -- riding the shared operational cadence, asking the director
//     to confirm the pens' feed did reduce. The maintainer wanted a reminder, not a report: no
//     kilograms, no head counts from the sheet, just "did it reduce for these pens?".
//
// WHICH DAY is the feed domain's rule (feeddomain.SaleFeedReductionDay): the first feed day whose
// sheet is issued or corrected after the sale, read off the park's own correction clock.
//
// ONCE EACH, without a scheduler: both keys carry the deal and the confirm instant, so a redelivered
// event, a retried tick, or the HA pair running both instances collapses onto one row per device.
const (
	// EventGoatSaleAllocated is the confirm-time event (identity/adapters/postgres/sale_allocation_event.go).
	EventGoatSaleAllocated = "goat.sale_allocated"
	// NotificationTypeFeedSaleReduce is the confirm-time notice.
	NotificationTypeFeedSaleReduce = "feed_sale_reduce"
	// NotificationTypeFeedSaleReduceReminder is the feed-day reminder.
	NotificationTypeFeedSaleReduceReminder = "feed_sale_reduce_reminder"
	// EventGoatSaleReleased is the failed-sale release event (identity/adapters/postgres/
	// sale_allocation_release.go): ONE per released deal, with the pens the animals went back to.
	EventGoatSaleReleased = "goat.sale_released"
	// NotificationTypeFeedSaleFailedReturn is the "sale failed, animals back" message (2026-09-25).
	NotificationTypeFeedSaleFailedReturn = "feed_sale_failed_return"

	// saleFeedReminderWindow bounds the reminder's read: confirms older than this have had their
	// feed day, and their key already exists. Four days covers a sale after the cutoff (feed day
	// D+2) plus a worker outage over a weekend.
	saleFeedReminderWindow = 4 * 24 * time.Hour
	// saleFeedReminderNotBeforeHour gates the reminder to a waking hour on the feed day: the shared
	// cadence would otherwise push it at 00:00 IST, the first tick of the business day.
	saleFeedReminderNotBeforeHour = 7
	// saleFeedPensNamedInPush caps how many pens a push names outright; the rest are counted, so a
	// hundred-animal sale across many pens stays a readable notification.
	saleFeedPensNamedInPush = 6
)

// FeedClockReader is the slice of the feed repository this bridge needs: a park's dispatch clocks.
type FeedClockReader interface {
	ListScheduleClocks(ctx context.Context, tenantID, parkID string, asOf time.Time) ([]feeddomain.WorkflowClock, error)
}

// goatSaleAllocatedPayload mirrors the producer's payload.
type goatSaleAllocatedPayload struct {
	TenantID    string                 `json:"tenant_id"`
	SalesDealID string                 `json:"sales_deal_id"`
	AllocatedAt string                 `json:"allocated_at"`
	Animals     int                    `json:"animals"`
	Pens        []goatSaleAllocatedPen `json:"pens"`
}

type goatSaleAllocatedPen struct {
	ParkID                     string `json:"park_id"`
	ParkName                   string `json:"park_name"`
	ParkCode                   string `json:"park_code"`
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Animals                    int    `json:"animals"`
}

// SaleFeedReduceNotifier is both the goat.sale_allocated consumer (the notice) and the cadence
// notifier (the reminder). One type, because the two pushes describe one batch and must name the
// same pens, the same counts and the same feed day.
type SaleFeedReduceNotifier struct {
	recipients RecipientResolver
	// audience answers WHO hears both pushes (feed.sale_reduce): the tenant's stored
	// designations, or the catalog default -- the Feed Director -- when none is stored.
	audience AudienceResolver
	queue    NotificationQueue
	batches  identityports.SaleAllocationBatchReader
	clocks   FeedClockReader
	logger   *slog.Logger
	now      func() time.Time
}

// NewSaleFeedReduceNotifier builds the notifier over the shared roster/notification seams.
func NewSaleFeedReduceNotifier(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *SaleFeedReduceNotifier {
	return &SaleFeedReduceNotifier{recipients: recipients, audience: defaultAudience(recipients), queue: queue, logger: logger, now: time.Now}
}

// WithAudience attaches the stored per-designation audience (production wiring).
func (n *SaleFeedReduceNotifier) WithAudience(audience AudienceResolver) *SaleFeedReduceNotifier {
	if audience != nil {
		n.audience = audience
	}
	return n
}

// WithBatches attaches the confirm reader the REMINDER needs (the notice reads its batch off the
// event and needs none). Chainable.
func (n *SaleFeedReduceNotifier) WithBatches(reader identityports.SaleAllocationBatchReader) *SaleFeedReduceNotifier {
	n.batches = reader
	return n
}

// WithFeedClocks attaches the park clock reader that decides the feed day. Without it the D+1
// fallback in feeddomain.SaleFeedReductionDay applies. Chainable.
func (n *SaleFeedReduceNotifier) WithFeedClocks(reader FeedClockReader) *SaleFeedReduceNotifier {
	n.clocks = reader
	return n
}

// WithClock pins the clock, for tests.
func (n *SaleFeedReduceNotifier) WithClock(now func() time.Time) *SaleFeedReduceNotifier {
	n.now = now
	return n
}

var _ eventbus.Handler = (*SaleFeedReduceNotifier)(nil)

// Register subscribes the notice and the failed-sale message on the bus.
func (n *SaleFeedReduceNotifier) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatSaleAllocated, n)
	bus.Subscribe(EventGoatSaleReleased, n)
}

// goatSaleReleasedPayload mirrors the release producer's payload.
type goatSaleReleasedPayload struct {
	TenantID    string                 `json:"tenant_id"`
	SalesDealID string                 `json:"sales_deal_id"`
	BuyerName   string                 `json:"buyer_name"`
	ReleasedAt  string                 `json:"released_at"`
	Animals     int                    `json:"animals"`
	Pens        []goatSaleAllocatedPen `json:"pens"`
}

// HandleEvent queues the confirm-time notice for one goat.sale_allocated event.
func (n *SaleFeedReduceNotifier) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if n == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	if event.Type == EventGoatSaleReleased {
		return n.handleReleased(ctx, event)
	}
	if event.Type != EventGoatSaleAllocated {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var payload goatSaleAllocatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("sale feed reduce notice: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	dealID := strings.TrimSpace(payload.SalesDealID)
	if tenantID == "" || dealID == "" || len(payload.Pens) == 0 {
		return nil
	}
	allocatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.AllocatedAt))
	if err != nil {
		return eventbus.PermanentError(fmt.Errorf("sale feed reduce notice: allocated_at %q: %w", payload.AllocatedAt, err))
	}
	batch := identityports.SaleAllocationBatch{SalesDealID: dealID, AllocatedAt: allocatedAt, Animals: payload.Animals}
	for _, pen := range payload.Pens {
		batch.Pens = append(batch.Pens, identityports.SaleAllocationPen{
			ParkID: pen.ParkID, ParkName: pen.ParkName, ShedID: pen.ShedID, ShedName: pen.ShedName,
			PartitionLabel: pen.PartitionLabel, OperationalLocationDisplay: pen.OperationalLocationDisplay,
			Animals: pen.Animals,
		})
	}
	return n.notify(ctx, tenantID, batch, false)
}

// RemindDue queues the feed-day reminder for every recent confirm whose feed day has arrived.
// Runs on the shared operational cadence; the key makes every tick after the first a no-op.
func (n *SaleFeedReduceNotifier) RemindDue(ctx context.Context, tenantID string) error {
	if n == nil || n.recipients == nil || n.queue == nil || n.batches == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("sale feed reduce reminder: tenant id is required")
	}
	now := n.now()
	batches, err := n.batches.ListRecentSaleAllocationBatches(ctx, tenantID, now.Add(-saleFeedReminderWindow))
	if err != nil {
		return fmt.Errorf("sale feed reduce reminder: read confirms: %w", err)
	}
	today := biztime.BusinessDate(now)
	localHour := now.In(biztime.DefaultLocation()).Hour()
	for _, batch := range batches {
		if len(batch.Pens) == 0 {
			continue
		}
		feedDay, err := n.feedDay(ctx, tenantID, batch)
		if err != nil {
			return err
		}
		// Not yet its feed day, or its feed day but before the waking-hour gate: wait.
		if feedDay > today || (feedDay == today && localHour < saleFeedReminderNotBeforeHour) {
			continue
		}
		if err := n.notify(ctx, tenantID, batch, true); err != nil {
			return err
		}
	}
	return nil
}

// feedDay resolves the feed day a batch reduces from, off the clock of the park its pens stand in.
// A batch is confirmed from one park's picker, so the first pen's park is the batch's park.
func (n *SaleFeedReduceNotifier) feedDay(ctx context.Context, tenantID string, batch identityports.SaleAllocationBatch) (string, error) {
	var clock *feeddomain.WorkflowClock
	if n.clocks != nil {
		if parkID := strings.TrimSpace(batch.Pens[0].ParkID); parkID != "" {
			clocks, err := n.clocks.ListScheduleClocks(ctx, tenantID, parkID, batch.AllocatedAt)
			if err != nil {
				return "", fmt.Errorf("sale feed reduce: read park clock: %w", err)
			}
			clock = feeddomain.SaleFeedReductionClock(clocks)
		}
	}
	feedDay, err := feeddomain.SaleFeedReductionDay(batch.AllocatedAt, clock)
	if err != nil {
		return "", fmt.Errorf("sale feed reduce: feed day: %w", err)
	}
	return feedDay, nil
}

// notify queues one push -- the notice or the reminder -- for one batch, to the Feed Director.
func (n *SaleFeedReduceNotifier) notify(ctx context.Context, tenantID string, batch identityports.SaleAllocationBatch, reminder bool) error {
	feedDay, err := n.feedDay(ctx, tenantID, batch)
	if err != nil {
		return err
	}
	recipients, err := n.audience.Recipients(ctx, tenantID, strings.TrimSpace(batch.Pens[0].ParkID), audiencedomain.AlertFeedSaleReduce)
	if err != nil {
		return fmt.Errorf("sale feed reduce: %w", err)
	}
	if len(recipients) == 0 {
		// Loud, and no fallback: a notice nobody receives must not look sent.
		if n.logger != nil {
			n.logger.WarnContext(ctx, "sale_feed_reduce_notification_no_recipients",
				slog.String("tenant_id", tenantID), slog.String("sales_deal_id", batch.SalesDealID),
				slog.Bool("reminder", reminder))
		}
		return nil
	}

	parkName := saleFeedParkName(batch.Pens)
	penList := saleFeedPenList(batch.Pens)
	visibleSaleDate := biztime.FarmDateFromBusinessDate(biztime.BusinessDate(batch.AllocatedAt))
	visibleFeedDay := biztime.FarmDateFromBusinessDate(feedDay)
	penCount := fmt.Sprintf("%d pens", len(batch.Pens))
	if len(batch.Pens) == 1 {
		penCount = "1 pen"
	}
	animals := fmt.Sprintf("%d animals", batch.Animals)
	if batch.Animals == 1 {
		animals = "1 animal"
	}
	batchKey := fmt.Sprintf("%s:%d", batch.SalesDealID, batch.AllocatedAt.UnixMicro())

	var title, body, eventKey, notificationType, messageKey string
	if reminder {
		notificationType = NotificationTypeFeedSaleReduceReminder
		messageKey = "feed.sale_reduce.reminder"
		eventKey = "feed.sale_reduce.reminder:" + batchKey
		// Every fact the reader needs to check the sheet: which park, which pens, how many left
		// each, when they were sold, and which feed day should show the smaller quantity.
		title = fmt.Sprintf("Did feed reduce for %s at %s?", penCount, parkName)
		body = fmt.Sprintf("%s were sold from %s at %s on %s. Confirm their feed has reduced from %s.",
			animals, penList, parkName, visibleSaleDate, visibleFeedDay)
	} else {
		notificationType = NotificationTypeFeedSaleReduce
		messageKey = "feed.sale_reduce.notice"
		eventKey = "feed.sale_reduce:" + batchKey
		title = fmt.Sprintf("Sale confirmed: reduce feed for %s at %s", penCount, parkName)
		body = fmt.Sprintf("%s sold from %s at %s on %s. Feed for these pens should reduce from %s.",
			animals, penList, parkName, visibleSaleDate, visibleFeedDay)
	}

	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  eventKey,
		TargetType:       "sales_deal",
		TargetID:         batch.SalesDealID,
		NotificationType: notificationType,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":          notificationType,
			"message_key":   messageKey,
			"screen":        "feed_stock",
			"href":          "/feed/analytics",
			"sales_deal_id": batch.SalesDealID,
			"park_id":       strings.TrimSpace(batch.Pens[0].ParkID),
			"park_name":     parkName,
			"pen_count":     fmt.Sprintf("%d", len(batch.Pens)),
			"animals":       fmt.Sprintf("%d", batch.Animals),
			// Structured fields stay ISO: the client parses these and renders its own string.
			"sale_date":    biztime.BusinessDate(batch.AllocatedAt),
			"feed_day":     feedDay,
			"priority":     priorityHigh,
			"group_key":    "feed_sale_reduce:" + tenantID,
			"collapse_key": "feed_sale_reduce:" + tenantID + ":" + batchKey,
		},
		Recipients: recipients,
	})
	if err != nil {
		return fmt.Errorf("sale feed reduce: queue %s: %w", eventKey, err)
	}
	return nil
}

// saleFeedParkName is the batch's park: the first resolved name, "the park" when none resolved,
// so the copy never reads "at ".
func saleFeedParkName(pens []identityports.SaleAllocationPen) string {
	for _, pen := range pens {
		if name := strings.TrimSpace(pen.ParkName); name != "" {
			return name
		}
	}
	return "the park"
}

// saleFeedPenList renders "Castro 1 (8), Mandela 1 - Part 2 (4) and 2 more pens".
func saleFeedPenList(pens []identityports.SaleAllocationPen) string {
	parts := make([]string, 0, len(pens))
	for i, pen := range pens {
		if i == saleFeedPensNamedInPush {
			break
		}
		where := strings.TrimSpace(pen.OperationalLocationDisplay)
		if where == "" {
			where = strings.TrimSpace(pen.ShedName)
		}
		if where == "" {
			where = "an unnamed pen"
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", where, pen.Animals))
	}
	rest := len(pens) - len(parts)
	switch {
	case rest == 1:
		return strings.Join(parts, ", ") + " and 1 more pen"
	case rest > 1:
		return fmt.Sprintf("%s and %d more pens", strings.Join(parts, ", "), rest)
	case len(parts) > 1:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return parts[0]
}

// A FAILED SALE GIVES ITS ANIMALS BACK (maintainer decision 2026-09-25). When a sale is marked
// failed, identity returns every animal tagged to it to alive in the pen it was sold from and
// emits goat.sale_released once, with the pens. The Feed Director -- who was told those pens would
// feed fewer mouths -- gets ONE message naming the buyer, each pen (park-qualified, "CBE Castro 1")
// with how many came back, and the feed day from which the pens feed as before. The feed day is
// the SAME park correction clock the sale notice uses (feeddomain.SaleFeedReductionDay), read at
// the release instant: the first sheet issued or corrected after the animals came back.
// Audience: its own catalog key, feed.sale_failed_return (default: the Feed Director).
// A release with no animals emits no event, so nothing is sent for it.
func (n *SaleFeedReduceNotifier) handleReleased(ctx context.Context, event eventbus.Event) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var payload goatSaleReleasedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("sale failed return: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	dealID := strings.TrimSpace(payload.SalesDealID)
	if tenantID == "" || dealID == "" || len(payload.Pens) == 0 || payload.Animals <= 0 {
		return nil
	}
	releasedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.ReleasedAt))
	if err != nil {
		return eventbus.PermanentError(fmt.Errorf("sale failed return: released_at %q: %w", payload.ReleasedAt, err))
	}
	batch := identityports.SaleAllocationBatch{SalesDealID: dealID, AllocatedAt: releasedAt, Animals: payload.Animals}
	for _, pen := range payload.Pens {
		batch.Pens = append(batch.Pens, identityports.SaleAllocationPen{
			ParkID: pen.ParkID, ParkName: pen.ParkName, ParkCode: pen.ParkCode, ShedID: pen.ShedID, ShedName: pen.ShedName,
			PartitionLabel: pen.PartitionLabel, OperationalLocationDisplay: pen.OperationalLocationDisplay,
			Animals: pen.Animals,
		})
	}
	feedDay, err := n.feedDay(ctx, tenantID, batch)
	if err != nil {
		return err
	}
	parkID := strings.TrimSpace(batch.Pens[0].ParkID)
	recipients, err := n.audience.Recipients(ctx, tenantID, parkID, audiencedomain.AlertFeedSaleFailedReturn)
	if err != nil {
		return fmt.Errorf("sale failed return: %w", err)
	}
	if len(recipients) == 0 {
		if n.logger != nil {
			n.logger.WarnContext(ctx, "sale_failed_return_notification_no_recipients",
				slog.String("tenant_id", tenantID), slog.String("sales_deal_id", dealID))
		}
		return nil
	}
	buyer := strings.TrimSpace(payload.BuyerName)
	if buyer == "" {
		buyer = "the buyer"
	}
	animals := fmt.Sprintf("%d animals", batch.Animals)
	if batch.Animals == 1 {
		animals = "1 animal"
	}
	parkQualified := make([]identityports.SaleAllocationPen, len(batch.Pens))
	for i, pen := range batch.Pens {
		parkQualified[i] = pen
		where := strings.TrimSpace(pen.OperationalLocationDisplay)
		if where == "" {
			where = strings.TrimSpace(pen.ShedName)
		}
		if code := strings.TrimSpace(pen.ParkCode); code != "" && where != "" {
			parkQualified[i].OperationalLocationDisplay = code + " " + where
		}
	}
	visibleFeedDay := biztime.FarmDateFromBusinessDate(feedDay)
	visibleFailedOn := biztime.FarmDateFromBusinessDate(biztime.BusinessDate(releasedAt))
	eventKey := "feed.sale_failed_return:" + dealID
	title := fmt.Sprintf("Sale to %s failed: %s back in their pens", buyer, animals)
	verb, pens := "are", "these pens"
	if batch.Animals == 1 {
		verb = "is"
	}
	if len(batch.Pens) == 1 {
		pens = "this pen"
	}
	body := fmt.Sprintf("The sale to %s failed on %s. %s %s back in %s. Feed %s as before from %s.",
		buyer, visibleFailedOn, animals, verb, saleFeedPenList(parkQualified), pens, visibleFeedDay)
	_, err = n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  eventKey,
		TargetType:       "sales_deal",
		TargetID:         dealID,
		NotificationType: NotificationTypeFeedSaleFailedReturn,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":          NotificationTypeFeedSaleFailedReturn,
			"message_key":   "feed.sale_failed_return",
			"screen":        "feed_stock",
			"href":          "/feed/analytics",
			"sales_deal_id": dealID,
			"park_id":       parkID,
			"park_name":     saleFeedParkName(batch.Pens),
			"buyer_name":    buyer,
			"pen_count":     fmt.Sprintf("%d", len(batch.Pens)),
			"animals":       fmt.Sprintf("%d", batch.Animals),
			// Structured fields stay ISO: the client parses these and renders its own string.
			"failed_on":    biztime.BusinessDate(releasedAt),
			"feed_day":     feedDay,
			"priority":     priorityHigh,
			"group_key":    "feed_sale_reduce:" + tenantID,
			"collapse_key": eventKey,
		},
		Recipients: recipients,
	})
	if err != nil {
		return fmt.Errorf("sale failed return: queue %s: %w", eventKey, err)
	}
	return nil
}
