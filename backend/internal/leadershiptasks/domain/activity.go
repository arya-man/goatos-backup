package domain

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Activity of a task (CEO instruction 2026-09-18, the Jira issue panel): one row per FACT
// that changed, written by the same transaction that changed it, read newest first.
// Migration 000349 is the store; the closed kind vocabulary below mirrors its CHECK.
//
// A row carries the two PLAIN stored values (a status key, an RFC3339 deadline, a title) and
// never a rendered label. The words a screen shows -- "Doing", "20/09/2026 17:00", the
// sentence -- are composed HERE, so the phone and the console read the same feed and a
// rewording never needs a data fix.
const (
	EventCreated         = "created"
	EventStatusChanged   = "status_changed"
	EventAssigneeChanged = "assignee_changed"
	EventDeadlineChanged = "deadline_changed"
	EventTitleChanged    = "title_changed"
	EventBriefChanged    = "brief_changed"
	EventCommented       = "commented"
	EventCancelled       = "cancelled"
)

// EventKinds is the closed vocabulary, in no particular order.
var EventKinds = []string{
	EventCreated, EventStatusChanged, EventAssigneeChanged, EventDeadlineChanged,
	EventTitleChanged, EventBriefChanged, EventCommented, EventCancelled,
}

// IsKnownEventKind reports whether k is in the closed vocabulary.
func IsKnownEventKind(k string) bool {
	for _, known := range EventKinds {
		if k == known {
			return true
		}
	}
	return false
}

// Event is one activity row as stored, plus the actor's display name resolved at read time.
type Event struct {
	EventID     string
	Kind        string
	OccurredAt  time.Time
	ActorUserID string
	ActorName   string
	// FromValue / ToValue are the plain stored values of the fact that moved (see the package
	// note); blank for `created`, `commented` and `cancelled`. For `commented`, NoteID names the
	// note and the note itself carries the text.
	FromValue string
	ToValue   string
	NoteID    string
}

// EventValueLabel renders one stored value of a kind as the words a screen shows: a status
// key becomes its chip ("Doing"), a deadline instant becomes the farm-clock label the Details
// panel uses ("20/09/2026 17:00"), and free text (a title, a brief) is shown as typed. Blank
// stays blank, so a deadline that was SET (from nothing) reads "— → 20/09/2026 17:00".
func EventValueLabel(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch kind {
	case EventStatusChanged, EventCancelled:
		return StatusChip(value)
	case EventDeadlineChanged:
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return DeadlineLabel(&parsed)
		}
		return value
	}
	return value
}

// EventOccurredLabel is the console's date-and-time for a feed row, on the farm clock:
// "18/09/2026 14:05" -- the same shape as the deadline label, so the two read as one clock.
func EventOccurredLabel(at time.Time) string {
	return at.In(biztime.DefaultLocation()).Format(biztime.FarmDateFormat + " 15:04")
}

// EventSummary is the one-line sentence of a feed row, actor first, the way Jira's history
// tab reads: "Hemant created the task", "Ravi Teja changed the status Open → Doing". The
// admin console renders the same facts from its own copy contract; this sentence is what the
// phone shows verbatim and what a test can assert.
func EventSummary(e Event) string {
	actor := nameOr(e.ActorName, "Someone")
	from := EventValueLabel(e.Kind, e.FromValue)
	to := EventValueLabel(e.Kind, e.ToValue)
	arrow := func(what string) string {
		return actor + " changed the " + what + " " + nameOr(from, "—") + " → " + nameOr(to, "—")
	}
	switch e.Kind {
	case EventCreated:
		return actor + " created the task"
	case EventStatusChanged:
		return arrow("status")
	case EventAssigneeChanged:
		return arrow("assignee")
	case EventDeadlineChanged:
		return arrow("deadline")
	case EventTitleChanged:
		return arrow("title")
	case EventBriefChanged:
		return actor + " edited the brief"
	case EventCommented:
		return actor + " commented"
	case EventCancelled:
		return actor + " cancelled the task"
	}
	return actor + " updated the task"
}

// IsHistoryEvent reports whether a row belongs on the History tab (everything that is not a
// comment); comments are the Comments tab, and All is both, newest first.
func IsHistoryEvent(kind string) bool { return kind != EventCommented }
