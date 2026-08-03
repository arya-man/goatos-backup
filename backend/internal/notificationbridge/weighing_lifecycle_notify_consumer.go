package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Weighing lifecycle push notifications: UP and DOWN for every state change that
// was previously silent.
//
// The pre-existing WeighingSubmissionEventConsumer covered only two states
// (submitted -> upward, reopened -> both ways). Publish, verdict, and close fired
// no push at all, so an operator was never told a bucket had been assigned to
// them, a rejected proof never reached the person who has to re-shoot it, and
// leadership never learned that work had been ended. This consumer closes those:
//
//	weighing.campaign_published    -> DOWNWARD to each assigned bucket operator
//	                                  (ONLY their own buckets) + UPWARD to
//	                                  growth_director + ceo_internal
//	weighing.observation.verified  -> UPWARD to growth_director + ceo_internal;
//	                                  the operator is pushed ONLY when the verdict
//	                                  left them an action (it does not)
//	weighing.observation.rework    -> DOWNWARD to the assigned operator + UPWARD to
//	                                  the owning director
//	weighing.shed.closed           -> UPWARD to leadership; DOWNWARD to the assigned
//	                                  operator when their bucket was closed with
//	                                  work that was never accepted
//	weighing.campaign.closed       -> UPWARD to leadership; DOWNWARD to each
//	                                  operator whose bucket was closed not accepted
//
// Every recipient resolves from role-grant / assigned-operator truth:
// ResolvePositionRecipients for leadership positions, ResolveMemberRecipients for
// the operator id carried on the event (which the weighing writer read from
// weighing_campaign_sheds.operator_user_id). No name, phone, token, or member id
// is hardcoded anywhere in this file.
const (
	EventWeighingCampaignPublished   = "weighing.campaign_published"
	EventWeighingObservationVerified = "weighing.observation.verified"
	EventWeighingObservationRework   = "weighing.observation.rework"
	EventWeighingShedClosed          = "weighing.shed.closed"
	// Abandon is a separate event so "ended without verification" is never
	// mistaken for "verified and closed". It notifies the same audience as a
	// close -- people still need to know the bucket ended -- but says so plainly.
	EventWeighingShedAbandoned  = "weighing.shed.abandoned"
	EventWeighingCampaignClosed = "weighing.campaign.closed"

	// WEIGHING PHASE 2 kernel cadences. Same consumer, not a parallel one:
	//
	//	day_start      -> DOWNWARD to the assigned operator ONLY, and only their
	//	                  own buckets. No leadership noise for routine daily work.
	//	rolled_forward -> DOWNWARD to the assigned operator (the work is still
	//	                  executable today) + UPWARD to leadership (the plan slipped).
	//	delayed        -> UPWARD escalation to growth_director + ceo_internal only.
	//	                  The operator already has the work in their day-start list;
	//	                  a delay is a leadership signal, not a second nudge.
	EventWeighingWorkItemDayStart      = "weighing.work_item.day_start"
	EventWeighingWorkItemRolledForward = "weighing.work_item.rolled_forward"
	EventWeighingWorkItemDelayed       = "weighing.work_item.delayed"

	// scopeTenant is the workforce position scope for tenant-wide leadership seats,
	// matching the existing weighing/verification consumers.
	scopeTenant = "tenant"

	roleLabelOperator       = "operator"
	roleLabelGrowthDirector = "growth_director"
	roleLabelCEO            = "ceo"
)

type weighingPublishedBucketPayload struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	ShedID              string `json:"shed_id"`
	ShedLabel           string `json:"shed_label"`
	OperatorID          string `json:"operator_id"`
	WeighingCategory    string `json:"weighing_category"`
	ExpectedAnimalCount int    `json:"expected_animal_count"`
}

type weighingCampaignPublishedPayload struct {
	TenantID          string                           `json:"tenant_id"`
	CampaignID        string                           `json:"campaign_id"`
	ParkID            string                           `json:"park_id"`
	PublishedBy       string                           `json:"published_by"`
	PeriodStartDate   string                           `json:"period_start_date"`
	PeriodEndDate     string                           `json:"period_end_date"`
	StartBusinessDate string                           `json:"start_business_date"`
	Buckets           []weighingPublishedBucketPayload `json:"buckets"`
}

type weighingObservationVerdictPayload struct {
	TenantID           string `json:"tenant_id"`
	CampaignID         string `json:"campaign_id"`
	CampaignShedID     string `json:"campaign_shed_id"`
	ObservationID      string `json:"observation_id"`
	RefType            string `json:"ref_type"`
	ParkID             string `json:"park_id"`
	ShedID             string `json:"shed_id"`
	ShedLabel          string `json:"shed_label"`
	OperatorID         string `json:"operator_id"`
	VerifiedBy         string `json:"verified_by"`
	Reason             string `json:"reason"`
	VerificationStatus string `json:"verification_status"`
	OperatorActionable bool   `json:"operator_actionable"`
	DecidedAt          string `json:"decided_at"`
}

type weighingShedClosedPayload struct {
	TenantID         string   `json:"tenant_id"`
	CampaignID       string   `json:"campaign_id"`
	CampaignShedID   string   `json:"campaign_shed_id"`
	ParkID           string   `json:"park_id"`
	ShedID           string   `json:"shed_id"`
	ShedLabel        string   `json:"shed_label"`
	OperatorID       string   `json:"operator_id"`
	ClosedBy         string   `json:"closed_by"`
	Reason           string   `json:"reason"`
	NotAcceptedCount int      `json:"not_accepted_count"`
	NotAccepted      []string `json:"not_accepted"`
	ClosedAt         string   `json:"closed_at"`
}

type weighingClosedBucketPayload struct {
	CampaignShedID string `json:"campaign_shed_id"`
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	OperatorID     string `json:"operator_id"`
	PreviousStatus string `json:"previous_status"`
}

type weighingCampaignClosedPayload struct {
	TenantID         string                        `json:"tenant_id"`
	CampaignID       string                        `json:"campaign_id"`
	ParkID           string                        `json:"park_id"`
	ClosedBy         string                        `json:"closed_by"`
	Reason           string                        `json:"reason"`
	NotAcceptedCount int                           `json:"not_accepted_count"`
	Buckets          []weighingClosedBucketPayload `json:"buckets"`
	ClosedAt         string                        `json:"closed_at"`
}

// WeighingLifecycleEventConsumer turns weighing publish/verdict/close events into
// idempotent FCM requests, routed up to leadership and down to the one operator
// who owns the affected bucket.
type WeighingLifecycleEventConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
}

func NewWeighingLifecycleEventConsumer(
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *WeighingLifecycleEventConsumer {
	return &WeighingLifecycleEventConsumer{recipients: recipients, queue: queue, logger: logger}
}

var _ eventbus.Handler = (*WeighingLifecycleEventConsumer)(nil)

func (c *WeighingLifecycleEventConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventWeighingCampaignPublished, c)
	bus.Subscribe(EventWeighingObservationVerified, c)
	bus.Subscribe(EventWeighingObservationRework, c)
	bus.Subscribe(EventWeighingShedClosed, c)
	bus.Subscribe(EventWeighingShedAbandoned, c)
	bus.Subscribe(EventWeighingCampaignClosed, c)
	bus.Subscribe(EventWeighingWorkItemDayStart, c)
	bus.Subscribe(EventWeighingWorkItemRolledForward, c)
	bus.Subscribe(EventWeighingWorkItemDelayed, c)
}

func (c *WeighingLifecycleEventConsumer) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch event.Type {
	case EventWeighingCampaignPublished:
		return c.handlePublished(ctx, event)
	case EventWeighingObservationVerified, EventWeighingObservationRework:
		return c.handleVerdict(ctx, event)
	case EventWeighingShedClosed, EventWeighingShedAbandoned:
		return c.handleShedClosed(ctx, event)
	case EventWeighingCampaignClosed:
		return c.handleCampaignClosed(ctx, event)
	case EventWeighingWorkItemDayStart, EventWeighingWorkItemRolledForward, EventWeighingWorkItemDelayed:
		return c.handleWorkItemCadence(ctx, event)
	default:
		return nil
	}
}

// handlePublished pushes each operator a note about ONLY their own buckets, and
// leadership one campaign-level note.
func (c *WeighingLifecycleEventConsumer) handlePublished(ctx context.Context, event eventbus.Event) error {
	var payload weighingCampaignPublishedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing publish notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignID := strings.TrimSpace(payload.CampaignID)
	if tenantID == "" || campaignID == "" {
		return nil
	}

	// One bucket has exactly ONE operator, so grouping by operator id yields the
	// per-operator bucket list. An operator must never be told about another
	// operator's bucket, which is why the fan-out is per operator rather than one
	// broadcast carrying every bucket.
	byOperator := map[string][]string{}
	for _, bucket := range payload.Buckets {
		operatorID := strings.TrimSpace(bucket.OperatorID)
		label := strings.TrimSpace(bucket.ShedLabel)
		if operatorID == "" || label == "" {
			continue
		}
		byOperator[operatorID] = append(byOperator[operatorID], label)
	}
	for _, operatorID := range sortedKeys(byOperator) {
		labels := byOperator[operatorID]
		sort.Strings(labels)
		// scale-guard:ignore: bounded fan-out over the DISTINCT operators of one campaign's buckets (one park's sheds, tens at most); each operator needs a private message listing only their own buckets, so a single batched read cannot replace it
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
		if err != nil {
			return fmt.Errorf("weighing publish notification: resolve operator %s recipients: %w", operatorID, err)
		}
		eventKey := EventWeighingCampaignPublished + ":" + campaignID + ":" + operatorID
		body := "Weighing work is assigned to you: " + strings.Join(labels, ", ") + "."
		// scale-guard:ignore: bounded per-operator write over the DISTINCT operators of one campaign's buckets (one park's sheds). Each message carries a DIFFERENT body and a DIFFERENT recipient set, because an operator must be told only about their own buckets, so a single batched write would either leak other operators' buckets or lose the per-operator idempotency key.
		if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  "weighing:" + campaignID,
			TargetType:       "weighing_campaign",
			TargetID:         campaignID,
			NotificationType: "advance_notice",
			Channel:          channelPushFCM,
			Priority:         priorityNormal,
			Title:            "New weighing work", // notification-copy:ignore: body variable names the operator's own shed labels
			Body:             body,
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context: map[string]string{
				"type":         "weighing_campaign_published",
				"screen":       "weighing",
				"target":       "/weighing",
				"message_key":  "weighing.assigned_operator",
				"shed_labels":  strings.Join(labels, ", "),
				"campaign_id":  campaignID,
				"park_id":      payload.ParkID,
				"start_date":   payload.StartBusinessDate,
				"group_key":    "weighing:" + tenantID + ":campaign_published",
				"collapse_key": "weighing:" + tenantID + ":campaign_published",
				"priority":     priorityNormal,
			},
			Recipients: toQueueRecipients(devices, roleLabelOperator),
		}); err != nil {
			return err
		}
	}

	leadership, err := c.leadershipRecipients(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("weighing publish notification: %w", err)
	}
	c.warnIfNoRecipients(ctx, leadership, "weighing_publish_notification_no_leadership_recipients", tenantID, campaignID)
	eventKey := EventWeighingCampaignPublished + ":" + campaignID
	// Leadership needs to know WHICH sheds and WHEN, not just how many: a bare count
	// ("a weighing plan with 4 sheds is now live") tells a director nothing they can act on.
	// The shed list is capped so the push stays readable; the full set is on the overview.
	allShedLabels := make([]string, 0, len(payload.Buckets))
	for _, bucket := range payload.Buckets {
		if label := strings.TrimSpace(bucket.ShedLabel); label != "" {
			allShedLabels = append(allShedLabels, label)
		}
	}
	sort.Strings(allShedLabels)
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignID,
		TargetType:       "weighing_campaign",
		TargetID:         campaignID,
		NotificationType: "advance_notice",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Weighing plan published", // notification-copy:ignore: weighingPlanPublishedBody names the sheds and the start date
		Body:             weighingPlanPublishedBody(allShedLabels, payload.StartBusinessDate),
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":   "weighing_campaign_published",
			"screen": "weighing_overview",
			// The task itself, not the module landing: this push exists to tell leadership a
			// PARTICULAR plan is live, and "/weighing" answered that with the recipient's own
			// (empty) operator work list.
			"target":       weighingTaskTarget(campaignID),
			"message_key":  "weighing.plan_published",
			"shed_count":   strconv.Itoa(len(payload.Buckets)),
			"campaign_id":  campaignID,
			"park_id":      payload.ParkID,
			"start_date":   payload.StartBusinessDate,
			"group_key":    "weighing:" + tenantID + ":campaign_published",
			"collapse_key": "weighing:" + tenantID + ":campaign_published",
			"priority":     priorityNormal,
		},
		Recipients: leadership,
	})
	return err
}

// handleVerdict routes an approved/rework verdict. Approved goes UPWARD only,
// because an approval leaves the operator nothing to do; the operator is included
// solely when the event says an action is needed. Rework goes DOWNWARD to the
// operator who must re-shoot, plus UPWARD to the owning director.
func (c *WeighingLifecycleEventConsumer) handleVerdict(ctx context.Context, event eventbus.Event) error {
	var payload weighingObservationVerdictPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing verdict notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	observationID := strings.TrimSpace(payload.ObservationID)
	if tenantID == "" || observationID == "" {
		return nil
	}
	rework := event.Type == EventWeighingObservationRework

	recipients := []calendarports.NotificationRecipient(nil)
	if rework || payload.OperatorActionable {
		operatorID := strings.TrimSpace(payload.OperatorID)
		if operatorID != "" {
			devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
			if err != nil {
				return fmt.Errorf("weighing verdict notification: resolve operator recipients: %w", err)
			}
			recipients = append(recipients, toQueueRecipients(devices, roleLabelOperator)...)
		}
	}
	if rework {
		// Rework escalates to the owning director only; a bounced proof is an
		// execution problem, not a CEO event.
		directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, positionGrowthDirector)
		if err != nil {
			return fmt.Errorf("weighing verdict notification: resolve growth director recipients: %w", err)
		}
		recipients = append(recipients, toQueueRecipients(directorDevices, roleLabelGrowthDirector)...)
	} else {
		leadership, err := c.leadershipRecipients(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("weighing verdict notification: %w", err)
		}
		recipients = append(recipients, leadership...)
	}
	recipients = dedupeQueueRecipients(recipients)

	shedLabel := strings.TrimSpace(payload.ShedLabel)
	if shedLabel == "" {
		shedLabel = "A weighing shed"
	}
	title := "Weighing proof approved"
	body := shedLabel + " weighing proof was approved."
	notificationType := "verification_approved"
	screen := "weighing_overview"
	priority := priorityNormal
	messageKey := "weighing.verdict.approved"
	reworkReason := ""
	if rework {
		title = "Weighing proof needs redo"
		body = shedLabel + " weighing proof was sent back. Please capture it again."
		if reason := strings.TrimSpace(payload.Reason); reason != "" {
			body = shedLabel + " weighing proof was sent back: " + reason
			reworkReason = reason
		}
		notificationType = NotificationTypeRework
		screen = "weighing"
		priority = priorityHigh
		messageKey = "weighing.verdict.rework"
	}
	// An APPROVED verdict is an evidence fact for leadership, so it opens the bucket whose proof
	// was accepted. A REWORK is work: its audience is the operator who must re-capture, and their
	// capture entry point is the module landing -- one push carries one target, and sending the
	// person who has to act to a read-only record would be the worse trade.
	verdictTarget := weighingBucketTarget(payload.CampaignID, payload.CampaignShedID)
	if rework {
		verdictTarget = weighingModuleTarget
	}
	eventKey := event.Type + ":" + observationID
	_, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + payload.CampaignShedID,
		TargetType:       "weighing_campaign_shed",
		TargetID:         payload.CampaignShedID,
		NotificationType: notificationType,
		Channel:          channelPushFCM,
		Priority:         priority,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":             "weighing_" + payload.VerificationStatus,
			"screen":           screen,
			"target":           verdictTarget,
			"message_key":      messageKey,
			"shed_label":       shedLabel,
			"rework_reason":    reworkReason,
			"campaign_id":      payload.CampaignID,
			"campaign_shed_id": payload.CampaignShedID,
			"observation_id":   observationID,
			"park_id":          payload.ParkID,
			"shed_id":          payload.ShedID,
			"decided_at":       payload.DecidedAt,
			"group_key":        "weighing:" + tenantID + ":verdict",
			"collapse_key":     "weighing:" + tenantID + ":verdict",
			"priority":         priority,
		},
		Recipients: recipients,
	})
	return err
}

// handleShedClosed tells leadership a bucket was ended, and tells the bucket's one
// operator only when their work was closed WITHOUT being accepted -- that is the
// case where the operator's outstanding work silently disappears from their app.
func (c *WeighingLifecycleEventConsumer) handleShedClosed(ctx context.Context, event eventbus.Event) error {
	var payload weighingShedClosedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing close notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignShedID := strings.TrimSpace(payload.CampaignShedID)
	if tenantID == "" || campaignShedID == "" {
		return nil
	}

	recipients, err := c.leadershipRecipients(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("weighing close notification: %w", err)
	}
	if payload.NotAcceptedCount > 0 {
		operatorID := strings.TrimSpace(payload.OperatorID)
		if operatorID != "" {
			devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
			if err != nil {
				return fmt.Errorf("weighing close notification: resolve operator recipients: %w", err)
			}
			recipients = append(recipients, toQueueRecipients(devices, roleLabelOperator)...)
		}
	}
	recipients = dedupeQueueRecipients(recipients)

	shedLabel := strings.TrimSpace(payload.ShedLabel)
	if shedLabel == "" {
		shedLabel = "A weighing shed"
	}
	body := shedLabel + " weighing was closed."
	if payload.NotAcceptedCount > 0 {
		body = fmt.Sprintf("%s weighing was closed with %d animals not weighed.", shedLabel, payload.NotAcceptedCount)
	}
	if reason := strings.TrimSpace(payload.Reason); reason != "" {
		body += " Reason: " + reason
	}
	abandoned := event.Type == EventWeighingShedAbandoned
	title := "Weighing shed closed"
	contextType := "weighing_shed_closed"
	messageKey := "weighing.shed_closed"
	if abandoned {
		title = "Weighing shed ended early"
		contextType = "weighing_shed_abandoned"
		messageKey = "weighing.shed_abandoned"
	}
	// Keyed on the ACTUAL event type: a close and an abandon for the same bucket
	// are different facts and must not collapse onto one notification key.
	eventKey := event.Type + ":" + campaignShedID + ":" + event.ID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignShedID,
		TargetType:       "weighing_campaign_shed",
		TargetID:         campaignShedID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":   contextType,
			"screen": "weighing_overview",
			// A closed bucket has no capture left to do for anyone, so both audiences want the
			// same thing: the record of the bucket that was closed.
			"target":             weighingBucketTarget(payload.CampaignID, campaignShedID),
			"message_key":        messageKey,
			"shed_label":         shedLabel,
			"reason":             strings.TrimSpace(payload.Reason),
			"campaign_id":        payload.CampaignID,
			"campaign_shed_id":   campaignShedID,
			"park_id":            payload.ParkID,
			"shed_id":            payload.ShedID,
			"not_accepted_count": fmt.Sprintf("%d", payload.NotAcceptedCount),
			"closed_at":          payload.ClosedAt,
			"group_key":          "weighing:" + tenantID + ":shed_closed",
			"collapse_key":       "weighing:" + tenantID + ":shed_closed",
			"priority":           priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}

// handleCampaignClosed tells leadership the whole task ended, and tells each
// operator whose bucket was closed without acceptance -- one message per operator,
// naming only their own buckets.
func (c *WeighingLifecycleEventConsumer) handleCampaignClosed(ctx context.Context, event eventbus.Event) error {
	var payload weighingCampaignClosedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing campaign close notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignID := strings.TrimSpace(payload.CampaignID)
	if tenantID == "" || campaignID == "" {
		return nil
	}

	byOperator := map[string][]string{}
	for _, bucket := range payload.Buckets {
		operatorID := strings.TrimSpace(bucket.OperatorID)
		label := strings.TrimSpace(bucket.ShedLabel)
		if operatorID == "" || label == "" {
			continue
		}
		byOperator[operatorID] = append(byOperator[operatorID], label)
	}
	for _, operatorID := range sortedKeys(byOperator) {
		labels := byOperator[operatorID]
		sort.Strings(labels)
		// scale-guard:ignore: bounded fan-out over the DISTINCT operators whose buckets this campaign close ended (capped by the producer at domain.CloseNotAcceptedSampleLimit); each operator must be told only about their own buckets
		devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
		if err != nil {
			return fmt.Errorf("weighing campaign close notification: resolve operator %s recipients: %w", operatorID, err)
		}
		eventKey := EventWeighingCampaignClosed + ":" + campaignID + ":" + operatorID
		body := "Weighing was closed before you finished: " + strings.Join(labels, ", ") + "."
		if reason := strings.TrimSpace(payload.Reason); reason != "" {
			body += " Reason: " + reason
		}
		// scale-guard:ignore: bounded per-operator write over the DISTINCT operators whose buckets this close ended (producer caps the bucket list at domain.CloseNotAcceptedSampleLimit). Each message names only that operator's own buckets, so it cannot be collapsed into one batched write.
		if _, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  "weighing:" + campaignID,
			TargetType:       "weighing_campaign",
			TargetID:         campaignID,
			NotificationType: "verification_closed",
			Channel:          channelPushFCM,
			Priority:         priorityNormal,
			Title:            "Weighing closed", // notification-copy:ignore: body variable names the shed and its not-accepted work
			Body:             body,
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context: map[string]string{
				"type":         "weighing_campaign_closed",
				"screen":       "weighing",
				"target":       "/weighing",
				"campaign_id":  campaignID,
				"park_id":      payload.ParkID,
				"closed_at":    payload.ClosedAt,
				"group_key":    "weighing:" + tenantID + ":campaign_closed",
				"collapse_key": "weighing:" + tenantID + ":campaign_closed",
				"priority":     priorityNormal,
			},
			Recipients: toQueueRecipients(devices, roleLabelOperator),
		}); err != nil {
			return err
		}
	}

	leadership, err := c.leadershipRecipients(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("weighing campaign close notification: %w", err)
	}
	c.warnIfNoRecipients(ctx, leadership, "weighing_campaign_close_notification_no_leadership_recipients", tenantID, campaignID)
	body := "A weighing task was closed."
	if payload.NotAcceptedCount > 0 {
		body = fmt.Sprintf("A weighing task was closed with %d sheds not completed.", payload.NotAcceptedCount)
	}
	if reason := strings.TrimSpace(payload.Reason); reason != "" {
		body += " Reason: " + reason
	}
	eventKey := EventWeighingCampaignClosed + ":" + campaignID + ":" + event.ID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignID,
		TargetType:       "weighing_campaign",
		TargetID:         campaignID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Weighing task closed", // notification-copy:ignore: body variable names each closed bucket's shed
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":               "weighing_campaign_closed",
			"screen":             "weighing_overview",
			"target":             weighingTaskTarget(campaignID),
			"message_key":        "weighing.campaign_closed",
			"campaign_id":        campaignID,
			"park_id":            payload.ParkID,
			"not_accepted_count": fmt.Sprintf("%d", payload.NotAcceptedCount),
			"closed_at":          payload.ClosedAt,
			"group_key":          "weighing:" + tenantID + ":campaign_closed",
			"collapse_key":       "weighing:" + tenantID + ":campaign_closed",
			"priority":           priorityNormal,
		},
		Recipients: leadership,
	})
	return err
}

// leadershipRecipients resolves the UPWARD audience from active role grants for the
// growth_director and ceo_internal positions. Never a hardcoded person.
func (c *WeighingLifecycleEventConsumer) leadershipRecipients(ctx context.Context, tenantID string) ([]calendarports.NotificationRecipient, error) {
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, positionGrowthDirector)
	if err != nil {
		return nil, fmt.Errorf("resolve growth director recipients: %w", err)
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, positionCEOInternal)
	if err != nil {
		return nil, fmt.Errorf("resolve CEO recipients: %w", err)
	}
	return dedupeQueueRecipients(append(
		toQueueRecipients(directorDevices, roleLabelGrowthDirector),
		toQueueRecipients(ceoDevices, roleLabelCEO)...,
	)), nil
}

func (c *WeighingLifecycleEventConsumer) warnIfNoRecipients(ctx context.Context, recipients []calendarports.NotificationRecipient, message, tenantID, targetID string) {
	if len(recipients) > 0 || c.logger == nil {
		return
	}
	c.logger.WarnContext(ctx, message, "tenant_id", tenantID, "target_id", targetID)
}

// sortedKeys keeps the per-operator fan-out deterministic so a redelivery produces
// the same message order (and the same idempotency keys) every time.
func sortedKeys(byOperator map[string][]string) []string {
	keys := make([]string, 0, len(byOperator))
	for key := range byOperator {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// weighingWorkItemBucketPayload is one bucket named in a cadence event. Every
// field is READ below; nothing is accepted and discarded.
type weighingWorkItemBucketPayload struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	ShedID              string `json:"shed_id"`
	ShedLabel           string `json:"shed_label"`
	PlannedBusinessDate string `json:"planned_business_date"`
	DueBusinessDate     string `json:"due_business_date"`
}

// weighingWorkItemCadencePayload is the shared payload of the three PHASE 2
// cadence events. OperatorID is the assignment-row operator the weighing writer
// read from weighing_campaign_sheds.operator_user_id; it is the ONLY downward
// routing key. Leadership resolves from active role grants. No name, phone, token,
// member id, or seed-time route is present anywhere.
type weighingWorkItemCadencePayload struct {
	TenantID     string                          `json:"tenant_id"`
	CampaignID   string                          `json:"campaign_id"`
	ParkID       string                          `json:"park_id"`
	OperatorID   string                          `json:"operator_id"`
	BusinessDate string                          `json:"business_date"`
	Buckets      []weighingWorkItemBucketPayload `json:"buckets"`
}

// handleWorkItemCadence routes the day-start / rolled-forward / delayed cadences.
//
// Direction per event is fixed by the event type, never by a payload flag, so a
// malformed producer cannot accidentally page the CEO for routine daily work.
func (c *WeighingLifecycleEventConsumer) handleWorkItemCadence(ctx context.Context, event eventbus.Event) error {
	var payload weighingWorkItemCadencePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("weighing work item cadence notification: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	campaignID := strings.TrimSpace(payload.CampaignID)
	if tenantID == "" || campaignID == "" || len(payload.Buckets) == 0 {
		return nil
	}

	labels := make([]string, 0, len(payload.Buckets))
	shedIDs := make([]string, 0, len(payload.Buckets))
	shedScopeIDs := make([]string, 0, len(payload.Buckets))
	earliestPlanned := ""
	dueDate := strings.TrimSpace(payload.BusinessDate)
	for _, bucket := range payload.Buckets {
		if label := strings.TrimSpace(bucket.ShedLabel); label != "" {
			labels = append(labels, label)
		}
		if id := strings.TrimSpace(bucket.ShedID); id != "" {
			shedIDs = append(shedIDs, id)
		}
		if id := strings.TrimSpace(bucket.CampaignShedID); id != "" {
			shedScopeIDs = append(shedScopeIDs, id)
		}
		planned := strings.TrimSpace(bucket.PlannedBusinessDate)
		if planned != "" && (earliestPlanned == "" || planned < earliestPlanned) {
			earliestPlanned = planned
		}
		if due := strings.TrimSpace(bucket.DueBusinessDate); due != "" && dueDate == "" {
			dueDate = due
		}
	}
	sort.Strings(labels)
	sort.Strings(shedIDs)
	sort.Strings(shedScopeIDs)
	shedList := strings.Join(labels, ", ")

	notifyOperator := event.Type != EventWeighingWorkItemDelayed
	notifyLeadership := event.Type != EventWeighingWorkItemDayStart

	recipients := []calendarports.NotificationRecipient(nil)
	if notifyOperator {
		operatorID := strings.TrimSpace(payload.OperatorID)
		if operatorID != "" {
			devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
			if err != nil {
				return fmt.Errorf("weighing work item cadence notification: resolve operator recipients: %w", err)
			}
			recipients = append(recipients, toQueueRecipients(devices, roleLabelOperator)...)
		}
	}
	if notifyLeadership {
		leadership, err := c.leadershipRecipients(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("weighing work item cadence notification: %w", err)
		}
		c.warnIfNoRecipients(ctx, leadership, "weighing_work_item_cadence_no_leadership_recipients", tenantID, campaignID)
		recipients = append(recipients, leadership...)
	}
	recipients = dedupeQueueRecipients(recipients)

	title := "Weighing due today"
	body := "Weighing is due today: " + shedList + "."
	notificationType := "due_today"
	screen := "weighing"
	priority := priorityNormal
	contextType := "weighing_work_item_day_start"
	messageKey := "weighing.work_item.due_today"
	switch event.Type {
	case EventWeighingWorkItemRolledForward:
		title = "Weighing moved to today"
		body = "Weighing not finished yesterday has moved to today: " + shedList + "."
		if earliestPlanned != "" {
			body = "Weighing first planned for " + earliestPlanned + " has moved to today: " + shedList + "."
		}
		notificationType = "reminder"
		contextType = "weighing_work_item_rolled_forward"
		messageKey = "weighing.work_item.rolled_forward"
	case EventWeighingWorkItemDelayed:
		title = "Weighing is running late"
		body = "Weighing is past its planned day: " + shedList + "."
		if earliestPlanned != "" {
			body = "Weighing planned for " + earliestPlanned + " is still not done: " + shedList + "."
		}
		notificationType = "escalation"
		screen = "weighing_overview"
		priority = priorityHigh
		contextType = "weighing_work_item_delayed"
		messageKey = "weighing.work_item.delayed"
	}

	// One durable request per (event type, campaign, operator, business date): the
	// same key the producer used, so an at-least-once redelivery collapses instead
	// of pushing the operator twice for the same business day.
	eventKey := event.Type + ":" + campaignID + ":" + strings.TrimSpace(payload.OperatorID) + ":" + strings.TrimSpace(payload.BusinessDate)
	_, err := c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "weighing:" + campaignID,
		TargetType:       "weighing_campaign",
		TargetID:         campaignID,
		NotificationType: notificationType,
		Channel:          channelPushFCM,
		Priority:         priority,
		Title:            title,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":              contextType,
			"screen":            screen,
			"target":            "/weighing",
			"message_key":       messageKey,
			"shed_list":         shedList,
			"campaign_id":       campaignID,
			"campaign_shed_ids": strings.Join(shedScopeIDs, ","),
			"park_id":           payload.ParkID,
			"shed_ids":          strings.Join(shedIDs, ","),
			"business_date":     strings.TrimSpace(payload.BusinessDate),
			"due_date":          dueDate,
			"planned_date":      earliestPlanned,
			"bucket_count":      fmt.Sprintf("%d", len(payload.Buckets)),
			"group_key":         "weighing:" + tenantID + ":work_item_cadence",
			"collapse_key":      "weighing:" + tenantID + ":" + contextType,
			"priority":          priority,
		},
		Recipients: recipients,
	})
	return err
}

// weighingPlanPublishedBody names the sheds and the start date so a director can act on the
// push itself. A bare count is not actionable; see docs/decisions/2026-08-02-meaningful-notification-copy.md.
func weighingPlanPublishedBody(shedLabels []string, startBusinessDate string) string {
	const maxNamedSheds = 4
	when := strings.TrimSpace(startBusinessDate)
	if when == "" {
		when = "the planned start date"
	}
	if len(shedLabels) == 0 {
		return "Weighing starts " + when + "."
	}
	named := shedLabels
	suffix := ""
	if len(named) > maxNamedSheds {
		remaining := len(named) - maxNamedSheds
		named = named[:maxNamedSheds]
		suffix = fmt.Sprintf(" and %d more", remaining)
	}
	return fmt.Sprintf("Weighing starts %s: %s%s.", when, strings.Join(named, ", "), suffix)
}
