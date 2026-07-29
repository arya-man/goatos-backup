package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const EventWeighingShedSubmissionCompleted = "weighing.shed_submission.completed"

type weighingShedSubmissionCompletedPayload struct {
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	ParkID         string `json:"park_id"`
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	CompletedAt    string `json:"completed_at"`
}

// WeighingSubmissionEventConsumer turns the durable shed-completion event into
// idempotent FCM requests for tenant Directors and CEOs.
type WeighingSubmissionEventConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
}

func NewWeighingSubmissionEventConsumer(
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *WeighingSubmissionEventConsumer {
	return &WeighingSubmissionEventConsumer{recipients: recipients, queue: queue, logger: logger}
}

var _ eventbus.Handler = (*WeighingSubmissionEventConsumer)(nil)

func (c *WeighingSubmissionEventConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventWeighingShedSubmissionCompleted, c)
}

func (c *WeighingSubmissionEventConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if event.Type != EventWeighingShedSubmissionCompleted || c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	var payload weighingShedSubmissionCompletedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing submission notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignShedID := strings.TrimSpace(payload.CampaignShedID)
	if tenantID == "" || campaignShedID == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	directorDevices, err := c.recipients.ResolvePositionRecipients(
		ctx, tenantID, "tenant", tenantID, positionPCDirector,
	)
	if err != nil {
		return fmt.Errorf("weighing submission notification: resolve director recipients: %w", err)
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(
		ctx, tenantID, "tenant", tenantID, positionCEOInternal,
	)
	if err != nil {
		return fmt.Errorf("weighing submission notification: resolve CEO recipients: %w", err)
	}
	recipients := dedupeQueueRecipients(append(
		toQueueRecipients(directorDevices, "director"),
		toQueueRecipients(ceoDevices, "ceo")...,
	))
	if len(recipients) == 0 && c.logger != nil {
		c.logger.WarnContext(ctx, "weighing_submission_notification_no_recipients",
			"tenant_id", tenantID,
			"campaign_id", payload.CampaignID,
			"campaign_shed_id", campaignShedID,
		)
	}

	body := "A weighing shed submission is complete."
	if shedLabel := strings.TrimSpace(payload.ShedLabel); shedLabel != "" {
		body = shedLabel + " weighing submission is complete."
	}
	eventKey := EventWeighingShedSubmissionCompleted + ":" + campaignShedID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignShedID,
		TargetType:       "weighing_campaign_shed",
		TargetID:         campaignShedID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Weighing shed submitted",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":             "weighing_shed_submitted",
			"screen":           "weighing_overview",
			"campaign_id":      payload.CampaignID,
			"campaign_shed_id": campaignShedID,
			"park_id":          payload.ParkID,
			"shed_id":          payload.ShedID,
			"completed_at":     payload.CompletedAt,
			"group_key":        "weighing:" + tenantID + ":shed_submission",
			"collapse_key":     "weighing:" + tenantID + ":shed_submission",
			"priority":         priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}
