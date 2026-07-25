package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ReminderLadderStep is one rung of the vaccination reminder cadence
// (docs/decisions/vaccination-notification-rules.md §3): a contiguous range of days relative to an
// obligation's due date, the IST wall-clock slot(s) that fire on each day in that range, and the
// notification type/priority the fire carries. Every offset/slot/count is a rule parameter (never a
// hardcoded constant) so the ladder can be tuned per vaccine priority or overridden via CLI/env on
// cmd/calendar-reminder-sweeper.
type ReminderLadderStep struct {
	// OffsetDaysFrom/OffsetDaysTo bound the range of days-before-due (negative) or on/after due
	// (zero/positive) this step covers, inclusive. A single-day step sets both to the same value.
	OffsetDaysFrom int
	OffsetDaysTo   int
	// Slots are IST wall-clock fire times in "HH:MM" 24h format, e.g. "08:00".
	Slots []string
	// Type is the notification_requests.notification_type this step fires with:
	// "advance_notice" | "reminder" | "due_today".
	Type string
	// Priority is carried into notification_requests.context (no dedicated column): "normal" | "high".
	Priority string
}

// Notification types the default reminder cadence emits (docs/decisions/vaccination-notification-rules.md §3).
const (
	ReminderTypeAdvanceNotice = "advance_notice"
	ReminderTypeReminder      = "reminder"
	ReminderTypeDueToday      = "due_today"
)

const (
	ReminderPriorityNormal = "normal"
	ReminderPriorityHigh   = "high"
)

// DefaultReminderLadder is the vaccination reminder cadence for an actionable scheduled/open
// vaccination shed with due date D (all times local/IST):
//   - D-7d: one advance_notice at 08:00 (normal)
//   - D-6d..D-1d: three reminders/day at 08:00, 13:00, and 20:30 (normal)
//   - D-0 (due day): three due_today reminders at 08:00, 13:00, and 20:30 (high)
//
// The repository candidate filter stops this ladder once the shed enters in_progress (operator
// scanning) or a later submitted/review state; the separate submission notification path owns those
// notifications.
func DefaultReminderLadder() []ReminderLadderStep {
	return []ReminderLadderStep{
		{OffsetDaysFrom: -7, OffsetDaysTo: -7, Slots: []string{"08:00"}, Type: ReminderTypeAdvanceNotice, Priority: ReminderPriorityNormal},
		{OffsetDaysFrom: -6, OffsetDaysTo: -1, Slots: []string{"08:00", "13:00", "20:30"}, Type: ReminderTypeReminder, Priority: ReminderPriorityNormal},
		{OffsetDaysFrom: 0, OffsetDaysTo: 0, Slots: []string{"08:00", "13:00", "20:30"}, Type: ReminderTypeDueToday, Priority: ReminderPriorityHigh},
	}
}

// Default quiet-hours window (vaccination-notification-rules.md §3): no push 21:00-07:00 IST,
// deferred to the next allowed slot -- field staff, not on-call.
const (
	DefaultQuietHoursStartIST = "21:00"
	DefaultQuietHoursEndIST   = "07:00"
)

// IsQuietHoursIST reports whether `now` (any timezone) falls inside the IST quiet-hours window
// [startHHMM, endHHMM), which may wrap past midnight (the default 21:00 -> 07:00 does). A zero-width
// window (start == end) is treated as "never quiet" rather than "always quiet".
func IsQuietHoursIST(now time.Time, startHHMM, endHHMM string) (bool, error) {
	nowIST := now.In(biztime.DefaultLocation())
	nowMinutes := nowIST.Hour()*60 + nowIST.Minute()
	startMinutes, err := parseHHMMMinutes(startHHMM)
	if err != nil {
		return false, fmt.Errorf("calendar: parse quiet-hours start %q: %w", startHHMM, err)
	}
	endMinutes, err := parseHHMMMinutes(endHHMM)
	if err != nil {
		return false, fmt.Errorf("calendar: parse quiet-hours end %q: %w", endHHMM, err)
	}
	if startMinutes == endMinutes {
		return false, nil
	}
	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes, nil
	}
	// Wraps midnight (e.g. 21:00 -> 07:00 the next day).
	return nowMinutes >= startMinutes || nowMinutes < endMinutes, nil
}

// ReminderFire is one concrete ladder fire computed for a single obligation's due date: a specific
// (day, slot) instant, already resolved to its notification type/priority.
type ReminderFire struct {
	OffsetDays     int       // days relative to the obligation's due date (negative = before, 0 = due day)
	Slot           string    // "HH:MM" IST
	Type           string    // advance_notice | reminder | due_today
	Priority       string    // normal | high
	ReminderNumber int       // 1-based position within the current ladder step's day range (0 for single-day steps)
	At             time.Time // absolute IST instant this fire is scheduled for
	// FireDayKey identifies the CALENDAR DAY + type + slot this fire belongs to, independent of which
	// due date it was computed from -- this is the batch/collapse key
	// (vaccination-notification-rules.md §3 "Dedup/collapse": multiple obligations due the same park
	// the same fire day+slot collapse into ONE push). Format: "YYYY-MM-DD:type:HH:MM".
	FireDayKey string
}

// LatestDueReminderFire returns the LATEST ladder fire for an obligation with due date `dueAt` that is
// (a) scheduled at or before `now`, and (b) not already fired per `fired(fireDayKey)`. Only the single
// latest such fire is returned -- never a backlog -- so a sweeper catching up after downtime (or a
// slow tick) sends the current correct reminder once instead of bursting every missed slot. Returns
// ok=false when no ladder fire is currently due.
func LatestDueReminderFire(dueAt, now time.Time, ladder []ReminderLadderStep, fired func(fireDayKey string) bool) (ReminderFire, bool) {
	var best ReminderFire
	found := false
	iterateDueLadderFires(dueAt, now, ladder, func(f ReminderFire) {
		if fired(f.FireDayKey) {
			return
		}
		if !found || f.At.After(best.At) {
			best = f
			found = true
		}
	})
	return best, found
}

// PendingLadderFireKeys returns every fire-day key the ladder could produce for an obligation with
// due date `dueAt` whose scheduled instant is at or before `now`, REGARDLESS of whether it has
// already fired. Used to gather the full candidate key set to check against the fire-marker table in
// one batched read, before re-running LatestDueReminderFire with the real fired-state.
func PendingLadderFireKeys(dueAt, now time.Time, ladder []ReminderLadderStep) []string {
	seen := map[string]bool{}
	var keys []string
	iterateDueLadderFires(dueAt, now, ladder, func(f ReminderFire) {
		if !seen[f.FireDayKey] {
			seen[f.FireDayKey] = true
			keys = append(keys, f.FireDayKey)
		}
	})
	return keys
}

// iterateDueLadderFires calls fn once for every ladder fire scheduled at or before `now` for an
// obligation with due date `dueAt`, in no particular order. Pure computation, no I/O.
func iterateDueLadderFires(dueAt, now time.Time, ladder []ReminderLadderStep, fn func(ReminderFire)) {
	loc := biztime.DefaultLocation()
	dueDateIST := biztime.BusinessDayStart(dueAt)
	nowIST := now.In(loc)

	for _, step := range ladder {
		if step.OffsetDaysTo < step.OffsetDaysFrom || len(step.Slots) == 0 {
			continue
		}
		span := step.OffsetDaysTo - step.OffsetDaysFrom + 1
		for offset := step.OffsetDaysFrom; offset <= step.OffsetDaysTo; offset++ {
			fireDay := dueDateIST.AddDate(0, 0, offset)
			for slotIdx, slot := range step.Slots {
				at, err := combineDaySlot(fireDay, slot, loc)
				if err != nil || at.After(nowIST) {
					continue
				}
				reminderNumber := 0
				if span > 1 {
					dayIndex := offset - step.OffsetDaysFrom
					reminderNumber = dayIndex*len(step.Slots) + slotIdx + 1
				}
				fn(ReminderFire{
					OffsetDays:     offset,
					Slot:           slot,
					Type:           step.Type,
					Priority:       step.Priority,
					ReminderNumber: reminderNumber,
					At:             at,
					FireDayKey:     fireDayKeyFor(fireDay, step.Type, slot),
				})
			}
		}
	}
}

// fireDayKeyFor builds the batch/collapse identity for a fire: the calendar fire day (not the
// obligation's due date), its notification type, and its slot. Deliberately excludes the obligation's
// due date and event id so multiple obligations in the same park due on different dates, but whose
// ladders both land a fire on the SAME calendar day+type+slot, collapse into one push
// (vaccination-notification-rules.md §3).
func fireDayKeyFor(fireDayIST time.Time, notifType, slot string) string {
	return fireDayIST.Format("2006-01-02") + ":" + notifType + ":" + slot
}

func combineDaySlot(dayIST time.Time, hhmm string, loc *time.Location) (time.Time, error) {
	minutes, err := parseHHMMMinutes(hhmm)
	if err != nil {
		return time.Time{}, err
	}
	y, m, d := dayIST.In(loc).Date()
	return time.Date(y, m, d, minutes/60, minutes%60, 0, 0, loc), nil
}

func parseHHMMMinutes(hhmm string) (int, error) {
	parts := strings.SplitN(strings.TrimSpace(hhmm), ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM, got %q", hhmm)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in %q", hhmm)
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil || min < 0 || min > 59 {
		return 0, fmt.Errorf("invalid minute in %q", hhmm)
	}
	return h*60 + min, nil
}
