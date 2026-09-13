package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Animal purchase decision push (maintainer decision 2026-09-13).
//
// The buying desk films a candidate animal on the phone; the CEO/CXO accepts or rejects it on
// admin-web. The person who recorded the animal hears the answer AT ONCE: which load, which
// animal, accepted or rejected, by whom, and any note. The phone re-reads the load on open and
// on resume, so this push is the nudge and the read is the truth -- but a director standing at
// the vendor's farm waiting for a yes must not have to keep opening the screen.
//
// Addressed to the PERSON the event names (recorded_by, a user id the device registry resolves),
// never to a position. Rides the durable event spine (procurement.animal_purchase.decided,
// emitted inside the decision transaction), so a committed decision always announces itself and
// a rolled-back one never does.
const (
	EventAnimalPurchaseDecided = "procurement.animal_purchase.decided"

	NotificationTypeAnimalPurchaseDecided = "animal_purchase_decided"

	animalPurchaseScreen = "animal_purchases"
)

type animalPurchaseDecidedPayload struct {
	CandidateID   string `json:"candidate_id"`
	LoadID        string `json:"load_id"`
	LoadRef       string `json:"load_ref"`
	SeqNo         int    `json:"seq_no"`
	Species       string `json:"species"`
	Sex           string `json:"sex"`
	Breed         string `json:"breed"`
	FarmLabel     string `json:"farm_label"`
	VendorName    string `json:"vendor_name"`
	Decision      string `json:"decision"`
	DecidedByName string `json:"decided_by_name"`
	DecisionNote  string `json:"decision_note"`
	RecordedBy    string `json:"recorded_by"`
	OccurredAt    string `json:"occurred_at"`
	Pending       int    `json:"load_pending"`
	Accepted      int    `json:"load_accepted"`
	Rejected      int    `json:"load_rejected"`
}

// AnimalPurchaseNotifyConsumer turns the decision event into a push to the recorder.
type AnimalPurchaseNotifyConsumer struct {
	recipients RecipientResolver
	audience   AudienceResolver
	queue      NotificationQueue
	logger     *slog.Logger
	now        func() time.Time
}

func NewAnimalPurchaseNotifyConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *AnimalPurchaseNotifyConsumer {
	return &AnimalPurchaseNotifyConsumer{recipients: recipients, audience: defaultAudience(recipients), queue: queue, logger: logger, now: time.Now}
}

// WithAudience attaches the stored per-designation audience (production wiring).
func (c *AnimalPurchaseNotifyConsumer) WithAudience(audience AudienceResolver) *AnimalPurchaseNotifyConsumer {
	if audience != nil {
		c.audience = audience
	}
	return c
}

var _ eventbus.Handler = (*AnimalPurchaseNotifyConsumer)(nil)

func (c *AnimalPurchaseNotifyConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventAnimalPurchaseDecided, c)
}

func (c *AnimalPurchaseNotifyConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil || event.Type != EventAnimalPurchaseDecided {
		return nil
	}
	var p animalPurchaseDecidedPayload
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		return eventbus.PermanentError(fmt.Errorf("animal purchase notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(event.TenantID)
	if tenantID == "" || strings.TrimSpace(p.CandidateID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var addressed []calendarports.NotificationRecipient
	if recorder := strings.TrimSpace(p.RecordedBy); recorder != "" {
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, recorder)
		if err != nil {
			return fmt.Errorf("animal purchase notification: resolve recorder: %w", err)
		}
		addressed = dedupeQueueRecipients(toQueueRecipients(devices, "recorder"))
	}
	recipients, err := c.audience.Addressed(ctx, tenantID, "", audiencedomain.AlertAnimalPurchaseDecided,
		[]string{audiencedomain.DesignationProcurementDirector}, addressed)
	if err != nil {
		return fmt.Errorf("animal purchase notification: %w", err)
	}
	if len(recipients) == 0 {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "animal_purchase_decided_notification_no_recipients",
				"tenant_id", tenantID, "candidate_id", p.CandidateID, "recorded_by", p.RecordedBy)
		}
		return nil
	}

	animal := fmt.Sprintf("Animal %d", p.SeqNo)
	if p.Breed != "" {
		animal += " (" + p.Breed + ")"
	}
	load := "Load " + strings.TrimSpace(p.LoadRef)
	if v := strings.TrimSpace(p.VendorName); v != "" {
		load += " from " + v
	}
	when := farmDateOrToday(p.OccurredAt, c.now())
	by := nameOrFallback(p.DecidedByName, "the CEO")
	var title, body string
	if p.Decision == "accepted" {
		title = fmt.Sprintf("Accepted · %s · %s", animal, load)
		body = fmt.Sprintf("%s in %s was accepted by %s on %s.", animal, load, by, when)
	} else {
		title = fmt.Sprintf("Rejected · %s · %s", animal, load)
		body = fmt.Sprintf("%s in %s was rejected by %s on %s.", animal, load, by, when)
	}
	if note := strings.TrimSpace(p.DecisionNote); note != "" {
		body += " Note: " + note
	}
	if p.Pending > 0 {
		body += fmt.Sprintf(" %d still awaiting decision in this load.", p.Pending)
	}
	eventKey := EventAnimalPurchaseDecided + ":" + p.CandidateID + ":" + event.ID
	target := "/vendors/animal-purchases/loads/" + p.LoadID
	if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "animal_purchase_candidate:" + p.CandidateID,
		TargetType:       "animal_purchase_candidate",
		TargetID:         p.CandidateID,
		NotificationType: NotificationTypeAnimalPurchaseDecided,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":          "animal_purchase_decided",
			"message_key":   "animal_purchase.decided",
			"screen":        animalPurchaseScreen,
			"href":          target,
			"target":        target,
			"load_id":       p.LoadID,
			"load_ref":      p.LoadRef,
			"candidate_id":  p.CandidateID,
			"decision":      p.Decision,
			"farm":          p.FarmLabel,
			"business_date": when,
			"priority":      priorityHigh,
			"group_key":     "animal_purchases",
		},
		Recipients: recipients,
	}); err != nil {
		return fmt.Errorf("animal purchase notification: queue decided: %w", err)
	}
	return nil
}
