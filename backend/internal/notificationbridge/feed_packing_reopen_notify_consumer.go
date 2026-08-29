package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Feed packing reopen push (maintainer decision 2026-08-29, closing the STG 2026-08-28 confusion):
// when the afternoon correction takes back a packed bag because animals moved after it was filmed,
// the PACKER is told -- with the pen, the session, the feed day, and the OLD-vs-NEW quantities --
// instead of discovering a silently rewritten card the next time they open the worklist.
//
// DOWNWARD ONLY. A head-count correction reopening a bag is routine daily work, the same class as a
// day-start nudge, so there is deliberately no leadership leg: leadership already sees the reopened
// pens on the feed execution view, and pushing every routine correction upward would be noise that
// buries the escalations that matter.
//
// Every recipient resolves from the event's operator_id (the completion's completed_by, read by the
// producer inside the reopen transaction) via ResolveMemberRecipients. No name, phone, token, or
// member id is hardcoded. An event with no operator_id (a legacy row whose completed_by was blank)
// pushes nothing and logs a gap warning -- there is nobody to address.
const EventFeedPackingReopened = "feed.packing.reopened"

// feedPackingReopenedPayload mirrors the producer's payload
// (feeddirection/adapters/postgres.insertFeedPackingReopenedOutbox). The packed_* pair is absent
// when the reopened row predates the packed-against snapshot (migration 000222); the copy then
// degrades to new-values-only rather than pushing a fabricated zero.
type feedPackingReopenedPayload struct {
	TenantID                   string `json:"tenant_id"`
	CompletionID               string `json:"completion_id"`
	ParkID                     string `json:"park_id"`
	ParkLabel                  string `json:"park_label"`
	ShedID                     string `json:"shed_id"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	SessionNo                  int32  `json:"session_no"`
	SessionLabel               string `json:"session_label"`
	TargetDate                 string `json:"target_date"`
	Workflow                   string `json:"workflow"`
	OperatorID                 string `json:"operator_id"`
	Reason                     string `json:"reason"`
	PackedHeadCount            *int64 `json:"packed_head_count"`
	PackedTotalKg              string `json:"packed_total_kg"`
	NewHeadCount               int64  `json:"new_head_count"`
	NewTotalKg                 string `json:"new_total_kg"`
}

// FeedPackingReopenNotifyConsumer consumes feed.packing.reopened and queues the packer's push.
type FeedPackingReopenNotifyConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
}

// NewFeedPackingReopenNotifyConsumer builds the consumer over the shared roster/notification seams
// (the same RecipientResolver + NotificationQueue every consumer in this package uses).
func NewFeedPackingReopenNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *FeedPackingReopenNotifyConsumer {
	return &FeedPackingReopenNotifyConsumer{recipients: recipients, queue: queue, logger: logger}
}

var _ eventbus.Handler = (*FeedPackingReopenNotifyConsumer)(nil)

// Register subscribes the consumer on the bus.
func (c *FeedPackingReopenNotifyConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventFeedPackingReopened, c)
}

// HandleEvent queues ONE push per reopened completion, addressed to its packer. Idempotent under
// at-least-once redelivery: the queue write is keyed on the event id, and the producer emits one
// event per (completion, reopen row_version), so a re-delivered event collapses onto the same
// notification row.
func (c *FeedPackingReopenNotifyConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	if event.Type != EventFeedPackingReopened {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var payload feedPackingReopenedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("feed packing reopen notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	completionID := strings.TrimSpace(payload.CompletionID)
	if tenantID == "" || completionID == "" {
		return nil
	}
	operatorID := strings.TrimSpace(payload.OperatorID)
	if operatorID == "" {
		// A legacy completion with no recorded packer: nobody to address. The reopened card itself
		// still carries the reason, so the work is visible; log the gap rather than guessing at an
		// audience.
		if c.logger != nil {
			c.logger.WarnContext(ctx, "feed_packing_reopen_notification_no_operator",
				slog.String("tenant_id", tenantID), slog.String("completion_id", completionID))
		}
		return nil
	}
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
	if err != nil {
		return fmt.Errorf("feed packing reopen notification: resolve packer recipients: %w", err)
	}
	if len(devices) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "feed_packing_reopen_notification_no_recipients",
				slog.String("tenant_id", tenantID), slog.String("completion_id", completionID),
				slog.String("operator_id", operatorID))
		}
		return nil
	}

	where := strings.TrimSpace(payload.OperationalLocationDisplay)
	if where == "" {
		where = "your pen"
	}
	if parkLabel := strings.TrimSpace(payload.ParkLabel); parkLabel != "" {
		where = parkLabel + " · " + where
	}
	session := strings.TrimSpace(payload.SessionLabel)
	if session == "" && payload.SessionNo > 0 {
		session = fmt.Sprintf("Session %d", payload.SessionNo)
	}
	feedDay := strings.TrimSpace(payload.TargetDate)
	visibleDate := feedDay
	if feedDay != "" {
		visibleDate = biztime.FarmDateFromBusinessDate(feedDay)
	}

	// The body leads with the change in numbers -- the fact the packer must act on -- then the
	// re-shoot instruction and the feed day. The producer-composed reason already carries the
	// old-vs-new sentence; it is re-composed here from the structured fields so the push stays
	// correct even for a fallback-reason row.
	oldPart := ""
	if payload.PackedHeadCount != nil {
		oldPart = packedQuantityPhrase(payload.PackedTotalKg, *payload.PackedHeadCount)
	}
	newPart := packedQuantityPhrase(payload.NewTotalKg, payload.NewHeadCount)
	change := "the feed quantities changed"
	switch {
	case oldPart != "" && newPart != "":
		change = "you packed " + oldPart + "; it now needs " + newPart
	case newPart != "":
		change = "it now needs " + newPart
	}
	sessionPart := ""
	if session != "" {
		sessionPart = " (" + session + ")"
	}
	title := "Repack " + where
	body := "Animals moved in " + where + " after you packed" + sessionPart + ": " + change +
		". Pack the new amounts and record a new video for " + visibleDate + "."

	eventKey := EventFeedPackingReopened + ":" + completionID + ":" + event.ID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "feed-packing:" + completionID,
		TargetType:       "feed_packing_completion",
		TargetID:         completionID,
		NotificationType: NotificationTypeRework,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":            "feed_packing_reopened",
			"screen":          "feed_packing",
			"target":          "/feed",
			"message_key":     "feed.packing_reopened",
			"completion_id":   completionID,
			"park_id":         payload.ParkID,
			"shed_id":         payload.ShedID,
			"partition_label": payload.PartitionLabel,
			"session_no":      fmt.Sprintf("%d", payload.SessionNo),
			"target_date":     feedDay,
			"group_key":       "feed:" + tenantID + ":packing_reopened",
			"collapse_key":    "feed:" + tenantID + ":packing_reopened",
			"priority":        priorityHigh,
		},
		Recipients: toQueueRecipients(devices, roleLabelOperator),
	})
	return err
}

// packedQuantityPhrase renders "24 kg for 12 animals" (or the half it can honestly state) from a
// kg-string and a head count -- the push twin of the producer's card sentence. Empty when neither
// value is usable, so the copy degrades instead of pushing a zero the farm never directed.
func packedQuantityPhrase(totalKg string, headCount int64) string {
	kg := strings.TrimSpace(totalKg)
	if kg != "" {
		if strings.Contains(kg, ".") {
			kg = strings.TrimRight(kg, "0")
			kg = strings.TrimRight(kg, ".")
		}
		if kg == "" || kg == "0" || kg == "-0" {
			kg = ""
		}
	}
	animals := ""
	if headCount == 1 {
		animals = "1 animal"
	} else if headCount > 0 {
		animals = fmt.Sprintf("%d animals", headCount)
	}
	switch {
	case kg != "" && animals != "":
		return kg + " kg for " + animals
	case kg != "":
		return kg + " kg"
	case animals != "":
		return "feed for " + animals
	}
	return ""
}
