package notificationbridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Missed-work notification.
//
// A missed obligation is the exact failure the operational kernel exists to catch, and it was the
// one lifecycle state that delivered no message to anyone: the missed handler opened an escalation
// row and stopped. An escalation is a screen state -- if nobody opens the screen, the miss is
// silent. This bridge is the message.
//
// Routing (module-owned, no fallback profile):
//   - DOWN to the operator the drive was assigned to. It was their work.
//   - UP to the park head and to the OWNING MODULE'S director. Vaccination's director is pc_director;
//     that is a per-module fact read from missedModuleProfiles, never a hardcoded default. A module
//     with no profile notifies NOBODY and logs loudly, exactly as the pending-verification routing
//     does -- silence is safer than telling the wrong department in another module's words.

// NotificationTypeObligationMissed is the notification_requests.notification_type for missed work.
const NotificationTypeObligationMissed = "obligation_missed"

// missedModuleProfile is one module's missed-work routing: whose director owns it, what the message
// says, and where a tap lands. Each module speaks in its OWN words; none of these fields is shared
// or defaulted across modules.
type missedModuleProfile struct {
	// directorPosition is the tenant-scoped seat that owns the module (module-ownership decision).
	directorPosition string
	// operatorScreen / leadershipScreen are the tap routes: the operator opens the work itself, the
	// leader opens the module overview.
	operatorScreen   string
	leadershipScreen string
	// workNoun is the farm word for the thing that was missed, used in the message body.
	workNoun string
	// targetType labels the notification row's subject.
	targetType string
}

// missedModuleProfiles is keyed by the protocol category the obligation belongs to. Vaccination is
// the only module producing obligations today; adding a module here is a deliberate act that names
// its own director, wording and tap routes.
var missedModuleProfiles = map[string]missedModuleProfile{
	"vaccination": {
		directorPosition: positionPCDirector,
		operatorScreen:   "vaccination",
		leadershipScreen: "vaccination_overview",
		workNoun:         "vaccination",
		targetType:       "obligation",
	},
}

// MissedObligationContextResolver is the slice of the calendar app service this bridge needs.
type MissedObligationContextResolver interface {
	ResolveMissedObligationContext(ctx context.Context, tenantID, obligationID string) (calendarports.MissedObligationContext, error)
}

// ObligationMissedNotifier turns a missed obligation into push messages for the operator, the park
// head, and the module's director. It is invoked from the calendar missed handler (which is already
// registered on the durable bus) rather than being a second subscriber, so the miss and its message
// stay on one path.
type ObligationMissedNotifier struct {
	contextResolver MissedObligationContextResolver
	recipients      RecipientResolver
	queue           NotificationQueue
	logger          *slog.Logger
}

func NewObligationMissedNotifier(
	contextResolver MissedObligationContextResolver,
	recipients RecipientResolver,
	queue NotificationQueue,
	logger *slog.Logger,
) *ObligationMissedNotifier {
	return &ObligationMissedNotifier{
		contextResolver: contextResolver,
		recipients:      recipients,
		queue:           queue,
		logger:          logger,
	}
}

// NotifyObligationMissed resolves the missed obligation's module/park/shed/operator and queues the
// downward and upward messages. Idempotent: every write is keyed by the missed event plus the
// recipient device, so an at-least-once redelivery inserts nothing new.
func (n *ObligationMissedNotifier) NotifyObligationMissed(ctx context.Context, tenantID, obligationID string) error {
	if n == nil || n.contextResolver == nil || n.recipients == nil || n.queue == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	obligationID = strings.TrimSpace(obligationID)
	if tenantID == "" || obligationID == "" {
		return nil
	}

	missed, err := n.contextResolver.ResolveMissedObligationContext(ctx, tenantID, obligationID)
	if err != nil {
		if errors.Is(err, calendarports.ErrNotFound) {
			// The obligation is gone (cancelled/reaped between the event and this read). Nothing to
			// say, and nothing a retry would fix.
			return nil
		}
		return fmt.Errorf("missed work notification: resolve context: %w", err)
	}

	profile, ok := missedModuleProfiles[missed.Module]
	if !ok {
		// No fallback profile, on purpose: an unclaimed module notifies nobody LOUDLY rather than
		// borrowing another module's director and wording.
		n.warn(ctx, "obligation_missed_notification_unclaimed_module", tenantID, obligationID, "module", missed.Module)
		return nil
	}

	eventKey := "obligation.missed:" + obligationID
	businessDate := biztime.BusinessDate(missed.DueAt)
	where := profile.workNoun + " work"
	if missed.ShedLabel != "" {
		where = profile.workNoun + " work for " + missed.ShedLabel
	}
	baseContext := map[string]string{
		"type":          "obligation_missed",
		"obligation_id": obligationID,
		"park_id":       missed.ParkID,
		"shed_id":       missed.ShedID,
		"due_date":      businessDate,
		"priority":      priorityHigh,
		"group_key":     "missed:" + tenantID + ":" + missed.Module,
		"collapse_key":  "missed:" + tenantID + ":" + obligationID,
	}

	// ---- DOWN: the operator whose work it was. --------------------------------------------------
	if missed.OperatorID != "" {
		devices, err := n.recipients.ResolveMemberRecipients(ctx, tenantID, missed.OperatorID)
		if err != nil {
			return fmt.Errorf("missed work notification: resolve operator: %w", err)
		}
		n.warnIfEmpty(ctx, devices, "obligation_missed_notification_no_operator_devices", tenantID, obligationID)
		operatorContext := copyContext(baseContext)
		operatorContext["screen"] = profile.operatorScreen
		if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  "obligation:" + obligationID,
			TargetType:       profile.targetType,
			TargetID:         obligationID,
			NotificationType: NotificationTypeObligationMissed,
			Channel:          channelPushFCM,
			Priority:         priorityHigh,
			Title:            "Missed " + profile.workNoun,
			Body:             upperFirst(where) + " was not finished on " + businessDate + ". Please finish it today.",
			TraceID:          eventKey + ":operator",
			EventKey:         eventKey + ":operator",
			Context:          operatorContext,
			Recipients:       toQueueRecipients(devices, roleLabelOperator),
		}); err != nil {
			return fmt.Errorf("missed work notification: queue operator: %w", err)
		}
	} else {
		n.warn(ctx, "obligation_missed_notification_no_assigned_operator", tenantID, obligationID, "park_id", missed.ParkID)
	}

	// ---- UP: the park head and the module's own director. ---------------------------------------
	leadership, err := n.missedLeadership(ctx, tenantID, missed.ParkID, profile.directorPosition)
	if err != nil {
		return err
	}
	n.warnIfEmpty(ctx, leadership, "obligation_missed_notification_no_leadership_devices", tenantID, obligationID)
	leadershipContext := copyContext(baseContext)
	leadershipContext["screen"] = profile.leadershipScreen
	if _, err := n.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  "obligation:" + obligationID,
		TargetType:       profile.targetType,
		TargetID:         obligationID,
		NotificationType: NotificationTypeObligationMissed,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            "Missed " + profile.workNoun,
		Body:             upperFirst(where) + " was not finished on " + businessDate + ".",
		TraceID:          eventKey + ":leadership",
		EventKey:         eventKey + ":leadership",
		Context:          leadershipContext,
		Recipients:       leadership,
	}); err != nil {
		return fmt.Errorf("missed work notification: queue leadership: %w", err)
	}
	return nil
}

// missedLeadership resolves the park head (park scope) plus the module's director (tenant scope),
// deduped by device so one person holding both seats is pushed once.
func (n *ObligationMissedNotifier) missedLeadership(ctx context.Context, tenantID, parkID, directorPosition string) ([]calendarports.NotificationRecipient, error) {
	out := make([]calendarports.NotificationRecipient, 0, 4)
	seen := map[string]bool{}
	appendDevices := func(devices []workforcedomain.NotificationRecipient, roleLabel string) {
		for _, device := range devices {
			if device.DeviceID == "" || seen[device.DeviceID] {
				continue
			}
			seen[device.DeviceID] = true
			out = append(out, calendarports.NotificationRecipient{
				MemberID:  device.WorkforceMemberID,
				DeviceID:  device.DeviceID,
				FCMToken:  device.FCMToken,
				RoleLabel: roleLabel,
			})
		}
	}
	if parkID != "" {
		parkHead, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
		if err != nil {
			return nil, fmt.Errorf("missed work notification: resolve park head: %w", err)
		}
		appendDevices(parkHead, positionParkHead)
	}
	director, err := n.recipients.ResolvePositionRecipients(ctx, tenantID, scopeTenant, tenantID, directorPosition)
	if err != nil {
		return nil, fmt.Errorf("missed work notification: resolve director: %w", err)
	}
	appendDevices(director, directorPosition)
	return out, nil
}

func (n *ObligationMissedNotifier) warn(ctx context.Context, msg, tenantID, obligationID string, extra ...any) {
	if n.logger == nil {
		return
	}
	args := append([]any{"tenant_id", tenantID, "obligation_id", obligationID}, extra...)
	n.logger.WarnContext(ctx, msg, args...)
}

func (n *ObligationMissedNotifier) warnIfEmpty(ctx context.Context, devices any, msg, tenantID, obligationID string) {
	switch typed := devices.(type) {
	case []workforcedomain.NotificationRecipient:
		if len(typed) == 0 {
			n.warn(ctx, msg, tenantID, obligationID)
		}
	case []calendarports.NotificationRecipient:
		if len(typed) == 0 {
			n.warn(ctx, msg, tenantID, obligationID)
		}
	}
}

func copyContext(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

// upperFirst capitalises the first letter of user-facing copy assembled from parts.
func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
