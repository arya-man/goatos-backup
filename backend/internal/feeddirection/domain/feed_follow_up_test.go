package domain_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

func sheet(days ...domain.FeedFollowUpSheetDay) []domain.FeedFollowUpSheetDay { return days }

func day(d string, heads int, kg string) domain.FeedFollowUpSheetDay {
	return domain.FeedFollowUpSheetDay{FeedDay: d, HeadCount: heads, Kg: kg}
}

// cause is an event that happened BEFORE the park's cut-off, so the NEXT day's
// sheet is the one that had to carry it.
func cause(kind, eventDate, expectedDay string, animals int) domain.FeedFollowUpCause {
	return domain.FeedFollowUpCause{
		Kind: kind, EventDate: eventDate, ExpectedDay: expectedDay, Animals: animals,
	}
}

// lateCause is an event at or after the cut-off: the next day's feed is already
// packed, so the day AFTER that is the first sheet that could carry it.
func lateCause(kind, eventDate, expectedDay string, animals int) domain.FeedFollowUpCause {
	c := cause(kind, eventDate, expectedDay, animals)
	c.AfterCutoff = true
	return c
}

// The finding the tab exists for: an animal died in the morning, so that
// afternoon's correction could still fix the next day's sheet -- and it did not.
func TestDeathBeforeTheCutoffTheNextSheetIgnoredIsNotFollowed(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-15",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-14", "2026-09-15", 1)},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed", got.Status)
	}
	if got.BeforeDay != "2026-09-14" || got.AfterDay != "2026-09-15" {
		t.Fatalf("readings %q -> %q, want 14th -> 15th", got.BeforeDay, got.AfterDay)
	}
	// The pen was over-fed by exactly the animal that is not there.
	if got.Unexplained != 1 {
		t.Fatalf("unexplained = %d, want 1", got.Unexplained)
	}
}

// THE RULE THE MAINTAINER CORRECTED (2026-09-22). An animal sold at 16:00 is
// sold AFTER the 14:00 correction: tomorrow's feed is already packed and
// frozen, so tomorrow's sheet must NOT be judged. The day after is the first
// one that could carry it.
//
// Judging the frozen sheet would accuse the farm of ignoring an animal it could
// not physically have acted on -- the exact defect this test pins.
func TestSaleAfterTheCutoffIsJudgedOnTheDayAfterTomorrowNotTheFrozenSheet(t *testing.T) {
	// Sold on the 14th at 16:00 -> expected day is the 16th, not the 15th.
	causes := []domain.FeedFollowUpCause{lateCause(domain.FeedFollowUpSold, "2026-09-14", "2026-09-16", 5)}
	// The 15th is FROZEN and still carries 44. The 16th drops to 39.
	got := domain.ResolveFeedFollowUpDay("2026-09-16", causes,
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2"), day("2026-09-16", 39, "16.0")),
	)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the 15th was already packed and must not be judged", got.Status)
	}
	// The comparison is the FROZEN sheet against the first one that could move.
	if got.BeforeDay != "2026-09-15" || got.AfterDay != "2026-09-16" {
		t.Fatalf("readings %q -> %q, want 15th -> 16th", got.BeforeDay, got.AfterDay)
	}
	if got.HeadBefore != 44 || got.HeadAfter != 39 {
		t.Fatalf("mouths %d -> %d, want 44 -> 39", got.HeadBefore, got.HeadAfter)
	}
	if !got.Events[0].AfterCutoff {
		t.Errorf("the event must remember it landed after the cut-off: %+v", got.Events[0])
	}
}

// The same late sale, genuinely ignored: the day after tomorrow still feeds the
// pen for every mouth. THAT is a finding.
func TestSaleAfterTheCutoffStillFailsWhenTheLaterSheetIgnoresIt(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-16",
		[]domain.FeedFollowUpCause{lateCause(domain.FeedFollowUpSold, "2026-09-14", "2026-09-16", 5)},
		sheet(day("2026-09-15", 44, "18.2"), day("2026-09-16", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed", got.Status)
	}
}

// A late event on one day and an early event on the next both point at the SAME
// sheet, and that sheet has to account for both. They arrive as one check,
// each keeping its own date and its own side of the cut-off.
func TestTwoDaysOfEventsMeetOnTheOneSheetThatMustCarryThemBoth(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-16",
		[]domain.FeedFollowUpCause{
			lateCause(domain.FeedFollowUpSold, "2026-09-14", "2026-09-16", 5),
			cause(domain.FeedFollowUpDied, "2026-09-15", "2026-09-16", 1),
		},
		sheet(day("2026-09-15", 44, "18.2"), day("2026-09-16", 38, "15.4")),
	)
	if got.Sold != 5 || got.Died != 1 || got.NetAnimals != -6 {
		t.Fatalf("want 5 sold + 1 died = net -6, got %+v", got)
	}
	if got.Status != domain.FeedFollowUpFollowed || got.Unexplained != 0 {
		t.Fatalf("44 -> 38 accounts for both, want followed with nothing unexplained; got %q / %d",
			got.Status, got.Unexplained)
	}
	// Oldest first, so the check reads in the order the farm lived it.
	if got.Events[0].EventDate != "2026-09-14" || got.Events[1].EventDate != "2026-09-15" {
		t.Errorf("events must be oldest first, got %+v", got.Events)
	}
}

// THE FALSE POSITIVE THIS RULE EXISTS TO KILL (found on live data 2026-09-22):
// the pen was fed for the same number of mouths but on 10 kg less feed. The
// sheet plainly reacted, and calling that "not followed" contradicts the two
// columns printed beside the verdict.
func TestSameMouthsOnLessFeedIsTheSheetReacting(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-19",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-18", "2026-09-19", 5)},
		sheet(day("2026-09-18", 53, "100.4"), day("2026-09-19", 53, "90.4")),
	)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the feed dropped 100.4 -> 90.4", got.Status)
	}
	// The head count did not move, so the gap is still reported honestly.
	if got.Unexplained != 5 {
		t.Fatalf("unexplained = %d, want 5", got.Unexplained)
	}
}

// Nothing moved at all -- same mouths AND same kg. That is the finding.
func TestNeitherMouthsNorFeedMovedIsNotFollowed(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-19",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-18", "2026-09-19", 5)},
		sheet(day("2026-09-18", 53, "100.4"), day("2026-09-19", 53, "100.4")),
	)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed", got.Status)
	}
}

// A purchase lands and the pen is fed for more mouths.
func TestPurchaseRaisesTheSheet(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-15",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpPurchased, "2026-09-14", "2026-09-15", 12)},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 56, "23.1")),
	)
	if got.Status != domain.FeedFollowUpFollowed || got.NetAnimals != 12 || got.HeadDelta != 12 {
		t.Fatalf("got %+v, want followed with net=delta=12", got)
	}
}

// A sale and a purchase that cancel out: the sheet SHOULD stand still, and
// standing still is following. A blunt "delta must be non-zero" would call this
// a finding and send someone to look at a pen that is correct.
func TestCausesThatCancelOutAreFollowedWhenTheSheetHoldsSteady(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-15",
		[]domain.FeedFollowUpCause{
			cause(domain.FeedFollowUpPurchased, "2026-09-14", "2026-09-15", 3),
			cause(domain.FeedFollowUpSold, "2026-09-14", "2026-09-15", 3),
		},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2")),
	)
	if got.NetAnimals != 0 {
		t.Fatalf("net = %d, want 0", got.NetAnimals)
	}
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed", got.Status)
	}
}

// Shifting is NOT one of the three causes (maintainer decision 2026-09-22), so
// a pen that gained animals from another pen reports the move as UNEXPLAINED.
// The tab says the number changed and does not claim to know why -- it must not
// silently attribute the move to a purchase.
func TestAMoveWithNoCauseReportsUnexplainedRatherThanACause(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-15",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-14", "2026-09-15", 1)},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 53, "21.0")),
	)
	if got.Purchased != 0 {
		t.Fatalf("purchased = %d, want 0 -- a shift is never reported as a purchase", got.Purchased)
	}
	// Lost one, gained nine: ten mouths arrived from somewhere this tab does
	// not track.
	if got.Unexplained != 10 {
		t.Fatalf("unexplained = %d, want 10", got.Unexplained)
	}
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the sheet did move", got.Status)
	}
}

// The sheet that should carry this has not been issued yet. There is no verdict
// to give, and giving one either way would be inventing it. This is the normal
// state for an event late today: tomorrow's sheet is frozen, and the day
// after's does not exist yet.
func TestNoSheetYetForTheExpectedDayIsPendingNotAPass(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-16",
		[]domain.FeedFollowUpCause{lateCause(domain.FeedFollowUpSold, "2026-09-14", "2026-09-16", 8)},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if got.AfterDay != "" || got.HeadAfter != 0 {
		t.Fatalf("after reading = %q/%d, want empty", got.AfterDay, got.HeadAfter)
	}
}

// The pen's first sheet of the window is the expected day itself, so there is
// no earlier reading to compare against. Pending, never a pass.
func TestNoSheetBeforeTheExpectedDayIsPending(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-14",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-13", "2026-09-14", 2)},
		sheet(day("2026-09-14", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
}

// "The next sheet" means the next SHEET, not the next date: a day the farm
// issued nothing has no head count to read, and walking on to the following
// sheet is the only reading that exists.
func TestAMissingSheetDayWalksOnToTheNextSheetThatExists(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-15",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-14", "2026-09-15", 1)},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-17", 43, "17.8")),
	)
	if got.AfterDay != "2026-09-17" {
		t.Fatalf("after day = %q, want 2026-09-17", got.AfterDay)
	}
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed", got.Status)
	}
}

// A pen with one ignored death among four handled ones is still a pen to look
// at. The worst day wins.
func TestPenVerdictIsItsWorstDay(t *testing.T) {
	cases := []struct {
		name string
		in   []domain.FeedFollowUpDay
		want string
	}{
		{"all followed", []domain.FeedFollowUpDay{{Status: domain.FeedFollowUpFollowed}, {Status: domain.FeedFollowUpFollowed}}, domain.FeedFollowUpFollowed},
		{"one pending", []domain.FeedFollowUpDay{{Status: domain.FeedFollowUpFollowed}, {Status: domain.FeedFollowUpPending}}, domain.FeedFollowUpPending},
		{"one ignored beats pending", []domain.FeedFollowUpDay{{Status: domain.FeedFollowUpPending}, {Status: domain.FeedFollowUpNotFollowed}, {Status: domain.FeedFollowUpFollowed}}, domain.FeedFollowUpNotFollowed},
		{"no days", nil, domain.FeedFollowUpFollowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.RollUpFeedFollowUpStatus(tc.in); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// The row's own figures must explain the row's verdict: a pen that failed one
// check shows THAT check's two sheets, not the window's ends. Showing the ends
// once put "19 -> 19 animals, 26 -> 30.6 kg, Not followed" on one line.
func TestRowFiguresComeFromTheCheckThatDecidedTheVerdict(t *testing.T) {
	days := []domain.FeedFollowUpDay{
		{Status: domain.FeedFollowUpFollowed, BeforeDay: "2026-09-01", AfterDay: "2026-09-02", HeadBefore: 19, HeadAfter: 18},
		{Status: domain.FeedFollowUpNotFollowed, BeforeDay: "2026-09-10", AfterDay: "2026-09-11", HeadBefore: 12, HeadAfter: 12},
		{Status: domain.FeedFollowUpPending, BeforeDay: "2026-09-20", AfterDay: ""},
	}
	got, ok := domain.DecidingFeedFollowUpDay(days)
	if !ok || got.BeforeDay != "2026-09-10" {
		t.Fatalf("want the failed check, got %+v (ok=%v)", got, ok)
	}

	// With nothing failed, a still-waiting check decides -- the row must not
	// claim a passing comparison while a check is unanswered.
	got, _ = domain.DecidingFeedFollowUpDay([]domain.FeedFollowUpDay{days[0], days[2]})
	if got.BeforeDay != "2026-09-20" {
		t.Fatalf("want the pending check, got %+v", got)
	}

	// All passed: the most recent one is the row's story.
	got, _ = domain.DecidingFeedFollowUpDay([]domain.FeedFollowUpDay{days[0], {Status: domain.FeedFollowUpFollowed, BeforeDay: "2026-09-05"}})
	if got.BeforeDay != "2026-09-05" {
		t.Fatalf("want the last passing check, got %+v", got)
	}

	if _, ok := domain.DecidingFeedFollowUpDay(nil); ok {
		t.Fatalf("no checks means no figures to show")
	}
}
