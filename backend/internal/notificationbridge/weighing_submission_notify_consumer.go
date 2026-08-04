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

const (
	EventWeighingShedSubmissionCompleted = "weighing.shed_submission.completed"
	EventWeighingShedReopened            = "weighing.shed.reopened"
)

type weighingShedSubmissionCompletedPayload struct {
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	ParkID         string `json:"park_id"`
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	CompletedAt    string `json:"completed_at"`
}

type weighingShedReopenedPayload struct {
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	ParkID         string `json:"park_id"`
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	OperatorID     string `json:"operator_id"`
	ReopenedBy     string `json:"reopened_by"`
	Reason         string `json:"reason"`
	ReopenedAt     string `json:"reopened_at"`
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
	bus.Subscribe(EventWeighingShedReopened, c)
}

func (c *WeighingSubmissionEventConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	if event.Type == EventWeighingShedReopened {
		return c.handleReopened(ctx, event)
	}
	if event.Type != EventWeighingShedSubmissionCompleted {
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
		ctx, tenantID, "tenant", tenantID, positionGrowthDirector,
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
		toQueueRecipients(directorDevices, "growth_director"),
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
			"type":   "weighing_shed_submitted",
			"screen": "weighing_overview",
			// Leadership-only push about ONE finished bucket: it opens that bucket's record,
			// never the recipient's own operator work list.
			"target":           weighingBucketTarget(payload.CampaignID, campaignShedID),
			"message_key":      "weighing.shed_submitted",
			"shed_label":       payload.ShedLabel,
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

func (c *WeighingSubmissionEventConsumer) handleReopened(ctx context.Context, event eventbus.Event) error {
	var payload weighingShedReopenedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing reopen notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignShedID := strings.TrimSpace(payload.CampaignShedID)
	if tenantID == "" || campaignShedID == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	operatorDevices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, payload.OperatorID)
	if err != nil {
		return fmt.Errorf("weighing reopen notification: resolve operator recipients: %w", err)
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionGrowthDirector)
	if err != nil {
		return fmt.Errorf("weighing reopen notification: resolve growth director recipients: %w", err)
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return fmt.Errorf("weighing reopen notification: resolve CEO recipients: %w", err)
	}
	recipients := dedupeQueueRecipients(append(append(
		toQueueRecipients(operatorDevices, "operator"),
		toQueueRecipients(directorDevices, "growth_director")...,
	), toQueueRecipients(ceoDevices, "ceo")...))
	body := "A weighing shed was reopened for more scans."
	if shedLabel := strings.TrimSpace(payload.ShedLabel); shedLabel != "" {
		body = shedLabel + " was reopened for more scans."
	}
	eventKey := EventWeighingShedReopened + ":" + campaignShedID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignShedID,
		TargetType:       "weighing_campaign_shed",
		TargetID:         campaignShedID,
		NotificationType: "rework",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Weighing shed reopened",
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":             "weighing_shed_reopened",
			"screen":           "weighing",
			"target":           "/weighing",
			"message_key":      "weighing.shed_reopened",
			"shed_label":       payload.ShedLabel,
			"campaign_id":      payload.CampaignID,
			"campaign_shed_id": campaignShedID,
			"park_id":          payload.ParkID,
			"shed_id":          payload.ShedID,
			"reopened_at":      payload.ReopenedAt,
			"group_key":        "weighing:" + tenantID + ":shed_reopen",
			"collapse_key":     "weighing:" + tenantID + ":shed_reopen",
			"priority":         priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}
