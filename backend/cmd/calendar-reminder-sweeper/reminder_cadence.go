package main

import (
	"context"
	"fmt"
	"time"

	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Audience for the vaccination reminder cadence (vaccination-notification-rules.md §4a): park-scoped
// operators + the park head, plus the PHC manager when a seat for it is resolvable. NEVER a
// tenant-scope (leadership) position -- the leadership daily digest is a separate, unbuilt increment
// (§4b); this sweeper only ever calls ResolvePositionRecipientsBatch with scopeType="center", so a
// tenant-scope leadership position structurally cannot appear in its result.
const (
	scopeCenter           = "center"
	positionOperator      = "operator"
	positionParkHead      = "park_head"
	positionPHCManager    = "phc_manager"
	channelPushFCM        = "push_fcm"
	reminderCadenceSource = "calendar-reminder-sweeper"
)

var reminderCadencePositions = []string{positionOperator, positionParkHead, positionPHCManager}

// reminderCadenceTick is the input to one sweep tick: the tenant, the as-of instant, and the ladder
// config. Now is an explicit parameter (never time.Now() called deep inside) so a test can drive the
// REAL sweep/queue services at fixed as-of times.
type reminderCadenceTick struct {
	TenantID           string
	Now                time.Time
	Ladder             []calendardomain.ReminderLadderStep
	QuietHoursStartIST string
	QuietHoursEndIST   string
	Limit              int
}

// sweepReminderCadence drives the REAL calendar + workforce app services end to end for one tick:
//  1. calendarService.SweepReminderCadence computes the collapsed, deduped, quiet-hours-aware fires
//     due right now (all set-based reads inside calendar -- no N+1);
//  2. rosterService.ResolvePositionRecipientsBatch resolves the audience for every distinct park in
//     ONE batched call (no per-park fan-out);
//  3. calendarService.QueueReminderCadenceBatch claims + writes every fire's recipient rows in one
//     set-based pass.
//
// Returns the number of notification_requests rows actually inserted.
func sweepReminderCadence(ctx context.Context, calendarService *calendarapp.Service, rosterService *workforceapp.RosterService, tick reminderCadenceTick) (int, error) {
	fires, err := calendarService.SweepReminderCadence(ctx, calendarports.ReminderCadenceQuery{
		TenantID:           tick.TenantID,
		Now:                tick.Now,
		Ladder:             tick.Ladder,
		QuietHoursStartIST: tick.QuietHoursStartIST,
		QuietHoursEndIST:   tick.QuietHoursEndIST,
		Limit:              tick.Limit,
	})
	if err != nil {
		return 0, fmt.Errorf("calendar-reminder-sweeper: sweep cadence: %w", err)
	}
	if len(fires) == 0 {
		return 0, nil
	}

	parkIDs := distinctParkIDs(fires)
	recipientsByParkPosition, err := rosterService.ResolvePositionRecipientsBatch(ctx, tick.TenantID, scopeCenter, parkIDs, reminderCadencePositions, tick.Now)
	if err != nil {
		return 0, fmt.Errorf("calendar-reminder-sweeper: resolve audience: %w", err)
	}

	inputs := make([]calendarports.ReminderCadenceFireInput, 0, len(fires))
	for _, fire := range fires {
		recipients := dedupeRecipientsByDevice(collectFireRecipients(recipientsByParkPosition, fire.ParkID))
		title, body := reminderCadenceMessage(fire)
		inputs = append(inputs, calendarports.ReminderCadenceFireInput{
			Fire:  fire,
			Title: title,
			Body:  body,
			Context: map[string]string{
				"type":          fire.NotificationType,
				"obligation_id": fire.RepresentativeObligationID,
				"park_id":       fire.ParkID,
				"priority":      fire.Priority,
			},
			Recipients: recipients,
		})
	}

	n, err := calendarService.QueueReminderCadenceBatch(ctx, calendarports.QueueReminderCadenceBatch{
		TenantID: tick.TenantID,
		Channel:  channelPushFCM,
		TraceID:  reminderCadenceSource + ":" + tick.Now.Format(time.RFC3339),
		Fires:    inputs,
	})
	if err != nil {
		return 0, fmt.Errorf("calendar-reminder-sweeper: queue cadence batch: %w", err)
	}
	return n, nil
}

func distinctParkIDs(fires []calendarports.ReminderCadenceFire) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fires {
		if f.ParkID == "" || seen[f.ParkID] {
			continue
		}
		seen[f.ParkID] = true
		out = append(out, f.ParkID)
	}
	return out
}

// collectFireRecipients gathers the operator + park_head + phc_manager recipients resolved for one
// park (vaccination-notification-rules.md §4a). A missing position (e.g. no phc_manager seat, or no
// device for a resolved seat) simply contributes nothing -- "PHC manager if resolvable".
func collectFireRecipients(byParkPosition map[string][]workforcedomain.NotificationRecipient, parkID string) []calendarports.NotificationRecipient {
	var out []calendarports.NotificationRecipient
	for _, position := range reminderCadencePositions {
		key := parkID + "|" + position
		for _, r := range byParkPosition[key] {
			out = append(out, calendarports.NotificationRecipient{
				MemberID:  r.WorkforceMemberID,
				DeviceID:  r.DeviceID,
				FCMToken:  r.FCMToken,
				RoleLabel: position,
			})
		}
	}
	return out
}

// dedupeRecipientsByDevice collapses duplicate devices (e.g. the same member holding two of the three
// audience positions, or appearing via more than one collapsed obligation) so one device gets exactly
// one notification_requests row per fire -- QueueReminderCadenceBatch's idempotency key is already
// per-device, but deduping here avoids sending redundant rows into that insert in the first place.
func dedupeRecipientsByDevice(recipients []calendarports.NotificationRecipient) []calendarports.NotificationRecipient {
	seen := map[string]bool{}
	out := make([]calendarports.NotificationRecipient, 0, len(recipients))
	for _, r := range recipients {
		if r.DeviceID == "" || seen[r.DeviceID] {
			continue
		}
		seen[r.DeviceID] = true
		out = append(out, r)
	}
	return out
}

// reminderCadenceMessage renders the push title/body for a collapsed cadence fire. Body mentions the
// collapsed obligation count when it batches more than one (vaccination-notification-rules.md §3
// "6 drives due today across K1, K2, Fattening-M" example).
func reminderCadenceMessage(fire calendarports.ReminderCadenceFire) (title, body string) {
	countPhrase := "a vaccination drive"
	if fire.ObligationCount > 1 {
		countPhrase = fmt.Sprintf("%d vaccination drives", fire.ObligationCount)
	}
	switch fire.NotificationType {
	case "advance_notice":
		return "Vaccination due in a week", fmt.Sprintf("%s due on %s -- plan ahead.", countPhrase, fire.FireDayIST)
	case "due_today":
		return "Vaccination due today", fmt.Sprintf("%s due today (%s).", countPhrase, fire.FireDayIST)
	default: // "reminder"
		return "Vaccination reminder", fmt.Sprintf("%s coming up on %s.", countPhrase, fire.FireDayIST)
	}
}
