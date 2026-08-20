package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ist builds an Asia/Kolkata instant. The whole colostrum lens is business-day grained, so every
// fixture here is anchored to a fixed IST date rather than to now±N hours (AGENTS.md: vaccination
// and birth scheduling are day-grained; hour-anchored fixtures pass or fail by time of day).
func ist(day int, hour, minute int) time.Time {
	return time.Date(2026, time.August, day, hour, minute, 0, 0, biztime.DefaultLocation())
}

func feed(key string, section string, seq int, status string, due time.Time) WorkflowAction {
	d := due
	return WorkflowAction{
		ActionID:   key,
		ActionKey:  key,
		Seq:        seq,
		Section:    section,
		ActionType: ActionTypeAction,
		Title:      key,
		Status:     status,
		DueAt:      &d,
	}
}

func TestIsColostrumActionCoversTheScheduledSeriesAndTheFirstFeed(t *testing.T) {
	// 1st Colostrum lives in section 'main' because it is part of the delivery sequence, but it IS
	// the first feed of the series the operator sees. A section-only predicate would drop it from
	// the page it belongs on.
	if !IsColostrumAction(SectionMain, ActionKeyFirstColostrum) {
		t.Fatal("1st Colostrum must be a colostrum action even though its section is main")
	}
	if !IsColostrumAction(SectionColostrumSession, "colostrum_day_2_0700") {
		t.Fatal("scheduled session rows must be colostrum actions")
	}
	// Everything else on the kid track must stay out of the lens.
	for _, key := range []string{ActionKeyKidClean, ActionKeyIodineDipping, ActionKeyTakeWeight, ActionKeyTagTheKid} {
		if IsColostrumAction(SectionMain, key) {
			t.Fatalf("%s is birth work, not a colostrum feed", key)
		}
	}
}

func TestColostrumDayWindowIsAHalfOpenISTDay(t *testing.T) {
	start, end, err := ColostrumDayWindow("2026-08-06")
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if !start.Equal(ist(6, 0, 0)) {
		t.Fatalf("start = %s, want 2026-08-06 00:00 IST", start)
	}
	if !end.Equal(ist(7, 0, 0)) {
		t.Fatalf("end = %s, want 2026-08-07 00:00 IST", end)
	}
	// Half-open: the 22:00 feed of the 6th is inside, and the 7th's midnight boundary is not.
	if ist(6, 22, 0).Before(start) || !ist(6, 22, 0).Before(end) {
		t.Fatal("22:00 on the selected day must fall inside the window")
	}
	if ist(7, 0, 0).Before(end) {
		t.Fatal("the next day's midnight must fall outside the window")
	}
	if _, _, err := ColostrumDayWindow("06-08-2026"); err == nil {
		t.Fatal("a malformed date must be rejected, not silently treated as today")
	}
}

// The headline rule (maintainer decision 2026-08-06): a card counts THAT DAY's feeds only. A kid
// born on the 5th has feeds on both the 5th and the 6th, and each date is counted independently —
// the 6th must never inherit the 5th's completions into its denominator or its numerator.
func TestColostrumDayCardCountsOnlyTheSelectedDaysFeeds(t *testing.T) {
	actions := []WorkflowAction{
		// Birth-day work.
		feed(ActionKeyKidClean, SectionMain, 1, ActionStatusCompleted, ist(5, 20, 0)),
		feed(ActionKeyFirstColostrum, SectionMain, 5, ActionStatusCompleted, ist(5, 20, 30)),
		feed("colostrum_day_1_2200", SectionColostrumSession, 8, ActionStatusCompleted, ist(5, 22, 0)),
		// Next-day series.
		feed("colostrum_day_2_0700", SectionColostrumSession, 9, ActionStatusCompleted, ist(6, 7, 0)),
		feed("colostrum_day_2_1100", SectionColostrumSession, 10, ActionStatusPending, ist(6, 11, 0)),
		feed("colostrum_day_2_1500", SectionColostrumSession, 11, ActionStatusPending, ist(6, 15, 0)),
		feed("colostrum_day_2_1830", SectionColostrumSession, 12, ActionStatusPending, ist(6, 18, 30)),
		feed("colostrum_day_2_2200", SectionColostrumSession, 13, ActionStatusPending, ist(6, 22, 0)),
		// Tagging is due on the 7th and is not a feed at all.
		feed(ActionKeyTagTheKid, SectionMain, 14, ActionStatusPending, ist(7, 7, 0)),
	}

	birthStart, birthEnd, _ := ColostrumDayWindow("2026-08-05")
	birthDay := ColostrumDayCard(actions, birthStart, birthEnd, ist(5, 23, 0))
	// 1st Colostrum + the 22:00 session. kid-clean is NOT a feed and must not inflate the total.
	if birthDay.Total != 2 || birthDay.Done != 2 {
		t.Fatalf("birth day = %d/%d, want 2/2", birthDay.Done, birthDay.Total)
	}
	if !birthDay.Complete() {
		t.Fatal("a fully fed birth day must read complete")
	}

	nextStart, nextEnd, _ := ColostrumDayWindow("2026-08-06")
	nextDay := ColostrumDayCard(actions, nextStart, nextEnd, ist(6, 12, 0))
	if nextDay.Total != 5 {
		t.Fatalf("next day total = %d, want 5 (that day's sessions only)", nextDay.Total)
	}
	if nextDay.Done != 1 {
		t.Fatalf("next day done = %d, want 1 — yesterday's completions must not carry over", nextDay.Done)
	}
	if nextDay.Complete() {
		t.Fatal("a day with outstanding feeds must not read complete")
	}
	if nextDay.Next == nil || nextDay.Next.Key != "colostrum_day_2_1100" {
		t.Fatalf("next feed = %+v, want the 11:00 session (first incomplete by seq)", nextDay.Next)
	}
	// 11:00 has passed at 12:00, so the card is overdue.
	if !nextDay.Next.Overdue {
		t.Fatal("a feed whose time has passed must read overdue")
	}
}

func TestColostrumDayCardTreatsReworkAsOutstandingAndSkipsCanceled(t *testing.T) {
	start, end, _ := ColostrumDayWindow("2026-08-06")
	actions := []WorkflowAction{
		feed("colostrum_day_2_0700", SectionColostrumSession, 9, ActionStatusCompleted, ist(6, 7, 0)),
		// A verifier sent this one back: the operator still owes a re-shoot, so it is NOT done.
		feed("colostrum_day_2_1100", SectionColostrumSession, 10, ActionStatusRework, ist(6, 11, 0)),
		// Canceled rows leave the denominator entirely rather than counting as outstanding work.
		feed("colostrum_day_2_1500", SectionColostrumSession, 11, ActionStatusCanceled, ist(6, 15, 0)),
	}
	summary := ColostrumDayCard(actions, start, end, ist(6, 12, 0))
	if summary.Total != 2 || summary.Done != 1 {
		t.Fatalf("summary = %d/%d, want 1/2 (rework outstanding, canceled excluded)", summary.Done, summary.Total)
	}
	if summary.Next == nil || summary.Next.Key != "colostrum_day_2_1100" {
		t.Fatalf("next = %+v, want the reworked 11:00 feed", summary.Next)
	}
}

// A kid with no feeds on the selected date produces no card at all. Complete() must not report a
// finished day for an empty one, or an untouched date would render as "all done".
func TestColostrumDayCardWithNoFeedsIsNotAFinishedDay(t *testing.T) {
	start, end, _ := ColostrumDayWindow("2026-08-09")
	summary := ColostrumDayCard([]WorkflowAction{
		feed("colostrum_day_2_0700", SectionColostrumSession, 9, ActionStatusCompleted, ist(6, 7, 0)),
	}, start, end, ist(9, 12, 0))
	if summary.Total != 0 || summary.Done != 0 || summary.Next != nil {
		t.Fatalf("summary = %+v, want an empty day", summary)
	}
	if summary.Complete() {
		t.Fatal("a day holding no feeds must not read as complete")
	}
}

// The lens deliberately has no awaiting_video bucket: verification is enqueued once per WHOLE kid
// workflow, so one day's feeds can never occupy it.
func TestColostrumFilterRejectsAwaitingVideo(t *testing.T) {
	for _, allowed := range []string{"", FilterAll, FilterOverdue, FilterDue, FilterCompleted} {
		if !ColostrumFilterAllowed(allowed) {
			t.Fatalf("%q must be a valid colostrum filter", allowed)
		}
	}
	if ColostrumFilterAllowed(FilterAwaitingVideo) {
		t.Fatal("awaiting_video is not a colostrum bucket and must be rejected, not shown as a permanent zero")
	}
	if ColostrumFilterAllowed("nonsense") {
		t.Fatal("unknown filters must be rejected")
	}
}

// Cross-check against the real template: the feeds TemplateBirthKidAt generates must be exactly the
// rows the lens picks up, split across the two dates the schedule spans. This is what stops the
// predicate and the generator from drifting apart.
func TestColostrumLensPicksUpEveryFeedTheBirthTemplateGenerates(t *testing.T) {
	birthAt := ist(5, 6, 0) // early morning: every birth-day slot is still ahead
	template := TemplateBirthKidAt(birthAt, false)

	actions := make([]WorkflowAction, 0, len(template.Actions))
	for _, at := range template.Actions {
		due := at.Schedule.DueAt(birthAt)
		actions = append(actions, WorkflowAction{
			ActionID: at.Key, ActionKey: at.Key, Seq: at.Seq, Section: at.Section,
			ActionType: at.Type, Title: at.Title, Status: ActionStatusPending, DueAt: &due,
		})
	}

	var feeds int
	for _, a := range actions {
		if IsColostrumAction(a.Section, a.ActionKey) {
			feeds++
		}
	}
	if feeds != 11 {
		t.Fatalf("template produced %d feeds for a 06:00 birth, want 11 (1st + 5 birth-day + 5 next-day)", feeds)
	}

	day1Start, day1End, _ := ColostrumDayWindow("2026-08-05")
	day2Start, day2End, _ := ColostrumDayWindow("2026-08-06")
	day1 := ColostrumDayCard(actions, day1Start, day1End, birthAt)
	day2 := ColostrumDayCard(actions, day2Start, day2End, birthAt)

	if day1.Total != 6 {
		t.Fatalf("birth day total = %d, want 6 (1st Colostrum + five slots)", day1.Total)
	}
	if day2.Total != 5 {
		t.Fatalf("next day total = %d, want 5 slots", day2.Total)
	}
	// Every generated feed lands on exactly one date: no feed is dropped, none is double-counted.
	if day1.Total+day2.Total != feeds {
		t.Fatalf("feeds split %d + %d, but the template generated %d", day1.Total, day2.Total, feeds)
	}
}
