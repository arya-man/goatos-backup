package domain

import (
	"testing"
	"time"
)

// The feed's words are composed HERE and rendered verbatim by the phone, so the sentence a
// reader sees for each kind -- and the rendering of each stored value -- is pinned.
func TestEventSummaryReadsActorFirstLikeJiraHistory(t *testing.T) {
	at := time.Date(2026, 9, 18, 4, 30, 0, 0, time.UTC)
	cases := []struct {
		e    Event
		want string
	}{
		{Event{Kind: EventCreated, ActorName: "Hemant"}, "Hemant created the task"},
		{Event{Kind: EventStatusChanged, ActorName: "Ravi Teja", FromValue: StatusOpen, ToValue: StatusInProgress}, "Ravi Teja changed the status To do → In progress"},
		{Event{Kind: EventCancelled, ActorName: "Hemant", FromValue: StatusInProgress, ToValue: StatusCancelled}, "Hemant cancelled the task"},
		{Event{Kind: EventDeadlineChanged, ActorName: "Hemant", FromValue: "2026-09-17T11:30:00Z", ToValue: "2026-09-20T11:30:00Z"}, "Hemant changed the deadline 17/09/2026 17:00 → 20/09/2026 17:00"},
		// A deadline SET where none was: the blank end reads as a dash, never as an empty gap.
		{Event{Kind: EventDeadlineChanged, ActorName: "Hemant", ToValue: "2026-09-20T11:30:00Z"}, "Hemant changed the deadline — → 20/09/2026 17:00"},
		{Event{Kind: EventTitleChanged, ActorName: "Hemant", FromValue: "Old", ToValue: "New"}, "Hemant changed the title Old → New"},
		{Event{Kind: EventBriefChanged, ActorName: "Hemant", FromValue: "a", ToValue: "b"}, "Hemant edited the brief"},
		{Event{Kind: EventCommented, ActorName: "Ravi"}, "Ravi commented"},
		{Event{Kind: EventAssigneeChanged, ActorName: "Ravi", FromValue: "Dinakar", ToValue: "Chandrakant"}, "Ravi changed the assignee Dinakar → Chandrakant"},
		// A departed actor still reads as a sentence, never as a raw id.
		{Event{Kind: EventCreated}, "Someone created the task"},
	}
	for _, c := range cases {
		c.e.OccurredAt = at
		if got := EventSummary(c.e); got != c.want {
			t.Errorf("%s: got %q want %q", c.e.Kind, got, c.want)
		}
	}
}

func TestEventValueLabelRendersKeysAsWordsAndLeavesTextAlone(t *testing.T) {
	if got := EventValueLabel(EventStatusChanged, StatusInProgress); got != "In progress" {
		t.Fatalf("status label = %q", got)
	}
	if got := EventValueLabel(EventDeadlineChanged, "2026-09-20T11:30:00Z"); got != "20/09/2026 17:00" {
		t.Fatalf("deadline label = %q", got)
	}
	// An unparseable stored deadline is shown as stored rather than dropped.
	if got := EventValueLabel(EventDeadlineChanged, "not-a-time"); got != "not-a-time" {
		t.Fatalf("bad deadline label = %q", got)
	}
	if got := EventValueLabel(EventTitleChanged, "  Fence repair  "); got != "Fence repair" {
		t.Fatalf("title label = %q", got)
	}
	if got := EventValueLabel(EventStatusChanged, ""); got != "" {
		t.Fatalf("blank must stay blank, got %q", got)
	}
}

func TestEventOccurredLabelIsTheFarmClockWithTime(t *testing.T) {
	if got := EventOccurredLabel(time.Date(2026, 9, 18, 4, 5, 0, 0, time.UTC)); got != "18/09/2026 09:35" {
		t.Fatalf("occurred label = %q", got)
	}
}

func TestEventKindsAreClosedAndOnlyCommentsLeaveHistory(t *testing.T) {
	for _, k := range EventKinds {
		if !IsKnownEventKind(k) {
			t.Fatalf("%s not known", k)
		}
		if IsHistoryEvent(k) == (k == EventCommented) {
			t.Fatalf("%s history membership wrong", k)
		}
	}
	if IsKnownEventKind("reassigned") {
		t.Fatal("unknown kind accepted")
	}
}
