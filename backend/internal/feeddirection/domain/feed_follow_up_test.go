package domain_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

func sheet(days ...domain.FeedFollowUpSheetDay) []domain.FeedFollowUpSheetDay { return days }

func day(d string, heads int, kg string) domain.FeedFollowUpSheetDay {
	return domain.FeedFollowUpSheetDay{FeedDay: d, HeadCount: heads, Kg: kg}
}

func cause(kind, eventDate string, animals int) domain.FeedFollowUpCause {
	return domain.FeedFollowUpCause{Kind: kind, EventDate: eventDate, Animals: animals}
}

// THE DEFECT THIS RULE WAS WRITTEN FOR (live data, 2026-09-23). CBE Godel 2 -
// Part 1 was fed for 5 animals on the 12th and 1 on the 13th -- exactly the 4
// sold that day. The farm reacted. The old rule read the sale's recorded
// 19:47 stamp, decided the 13th was already packed, compared the 13th with the
// 14th (1 against 1) and reported "feed unchanged".
//
// That stamp is when the day's sales were ENTERED, not when the animals left.
// The window spans both candidate sheets so a bookkeeping habit cannot produce
// a false accusation.
func TestAnEveningEntryStillSeesTheCutMadeTheNextDay(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-12", 4)},
		sheet(day("2026-09-12", 5, "6.0"), day("2026-09-13", 1, "1.2"), day("2026-09-14", 1, "1.2")), nil)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the pen went 5 to 1 on the 13th", got.Status)
	}
	if got.BeforeDay != "2026-09-12" || got.AfterDay != "2026-09-14" {
		t.Fatalf("readings %q -> %q, want the event day against two days later", got.BeforeDay, got.AfterDay)
	}
	if got.HeadBefore != 5 || got.HeadAfter != 1 {
		t.Fatalf("mouths %d -> %d, want 5 -> 1", got.HeadBefore, got.HeadAfter)
	}
	if got.Unexplained != 0 {
		t.Fatalf("unexplained = %d, want 0 -- the drop is exactly the four sold", got.Unexplained)
	}
}

// The same shape, genuinely ignored: two days on and the pen is still fed for
// every mouth. THAT is the finding, and the wider window must still catch it.
func TestAChangeTheSheetNeverTookUpIsStillCaught(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-12", 4)},
		sheet(day("2026-09-12", 5, "6.0"), day("2026-09-13", 5, "6.0"), day("2026-09-14", 5, "6.0")), nil)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed", got.Status)
	}
	if got.Unexplained != 4 {
		t.Fatalf("unexplained = %d, want 4 -- four mouths fed that are not there", got.Unexplained)
	}
}

// THE FED COUNT FALLING DOES NOT EXCUSE A FROZEN SHEET. Live case, CBE Godel 2
// - Part 5 on 2026-08-29: nine of its ten animals were sold, the fed count fell
// 10 -> 1 exactly as it should, and the experiment sheet stayed on the 18.00 kg
// authored for ten. An earlier rule ANDed "the fed count is stuck too" into the
// red test, so this read "Feed changed" -- the one shape the tab exists to
// catch. The count moving makes it worse, not better.
func TestAFallingHeadCountDoesNotExcuseAFrozenSheet(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-08-29",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-08-29", 9)},
		sheet(day("2026-08-29", 10, "18.00"), day("2026-08-31", 1, "18.00")), nil)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed -- 9 of 10 sold and the sheet held 18.00 kg", got.Status)
	}
	if got.HeadDelta != -9 {
		t.Fatalf("head delta = %d, want -9 -- the register did drop, and that is the point", got.HeadDelta)
	}
}

// The same shape with a death, and the fed count moving the WRONG way: CBE
// Castro 3 on 2026-08-16 lost an animal, was then fed for one MORE mouth, and
// stayed on 58.0 kg. The feed did not move, so the verdict is not_followed.
func TestADeathTheSheetNeverPricedIsCaught(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-08-16",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-08-16", 1)},
		sheet(day("2026-08-16", 65, "58.0"), day("2026-08-18", 66, "58.0")), nil)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed -- the feed held at 58.0 kg", got.Status)
	}
}

// A cut made on the FIRST of the two days counts, and so does one made on the
// second: the window asks whether the farm reacted, not which day it chose.
func TestEitherDayOfTheWindowCounts(t *testing.T) {
	first := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-12", 1)},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-13", 43, "17.8"), day("2026-09-14", 43, "17.8")), nil)
	second := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-12", 1)},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-13", 44, "18.2"), day("2026-09-14", 43, "17.8")), nil)
	if first.Status != domain.FeedFollowUpFollowed || second.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("both days must count: first=%q second=%q", first.Status, second.Status)
	}
}

// The deadline is two days out, so the day in between is never the reading --
// judging it would fail a pen the farm still had a day to handle.
func TestTheDayInBetweenIsNeverTheVerdict(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-12", 2)},
		sheet(day("2026-09-12", 10, "12.0"), day("2026-09-13", 10, "12.0")), nil)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending -- the deadline sheet is not issued yet", got.Status)
	}
	if got.AfterDay != "" {
		t.Fatalf("after day = %q, want empty", got.AfterDay)
	}
}

// A purchase raises the count.
func TestPurchaseRaisesTheSheet(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpPurchased, "2026-09-12", 12)},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-14", 56, "23.1")), nil)
	if got.Status != domain.FeedFollowUpFollowed || got.NetAnimals != 12 || got.HeadDelta != 12 {
		t.Fatalf("got %+v, want followed with net=delta=12", got)
	}
}

// A sale and a purchase that cancel out: the sheet SHOULD stand still, and
// standing still is following. A blunt "delta must be non-zero" would send
// someone to look at a pen that is correct.
func TestCausesThatCancelOutAreFollowedWhenTheSheetHoldsSteady(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{
			cause(domain.FeedFollowUpPurchased, "2026-09-12", 3),
			cause(domain.FeedFollowUpSold, "2026-09-12", 3),
		},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-14", 44, "18.2")), nil)
	if got.NetAnimals != 0 || got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("got net=%d status=%q, want 0 / followed", got.NetAnimals, got.Status)
	}
}

// Same mouths on less feed IS the sheet reacting; the verdict is about the
// FEED, and calling this unchanged contradicts the two columns beside it.
func TestSameMouthsOnLessFeedIsTheSheetReacting(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-17",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-17", 5)},
		sheet(day("2026-09-17", 53, "100.4"), day("2026-09-19", 53, "90.4")), nil)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the feed dropped 100.4 -> 90.4", got.Status)
	}
	if got.Unexplained != 5 {
		t.Fatalf("unexplained = %d, want 5", got.Unexplained)
	}
}

// Shifting is NOT one of the three causes, so a pen that gained animals from
// another pen reports the move as UNEXPLAINED. The tab says the number moved
// and does not claim to know why.
func TestAMoveWithNoCauseReportsUnexplainedRatherThanACause(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-12", 1)},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-14", 53, "21.0")), nil)
	if got.Purchased != 0 {
		t.Fatalf("purchased = %d, want 0 -- a shift is never reported as a purchase", got.Purchased)
	}
	if got.Unexplained != 10 {
		t.Fatalf("unexplained = %d, want 10", got.Unexplained)
	}
}

// The pen's first sheet in the window is already past the event, so there is
// no earlier reading to compare against. Pending, never a pass.
func TestNoSheetBeforeTheEventIsPending(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-10",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-10", 2)},
		sheet(day("2026-09-14", 44, "18.2")), nil)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
}

// "Two days later" means the next SHEET at or past the deadline, not that
// calendar date: a day the farm issued nothing has no head count to read.
func TestAMissingDeadlineDayWalksOnToTheNextSheet(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay("2026-09-12",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpDied, "2026-09-12", 1)},
		sheet(day("2026-09-12", 44, "18.2"), day("2026-09-17", 43, "17.8")), nil)
	if got.AfterDay != "2026-09-17" {
		t.Fatalf("after day = %q, want 2026-09-17", got.AfterDay)
	}
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed", got.Status)
	}
}

// The deadline is whole business days, and a bad date is returned unchanged
// rather than silently becoming a different day.
func TestAddBusinessDays(t *testing.T) {
	if got := domain.AddBusinessDays("2026-09-12", domain.FeedFollowUpReactionDays); got != "2026-09-14" {
		t.Fatalf("got %q, want 2026-09-14", got)
	}
	if got := domain.AddBusinessDays("2026-02-27", 2); got != "2026-03-01" {
		t.Fatalf("month boundary: got %q, want 2026-03-01", got)
	}
	if got := domain.AddBusinessDays("not-a-date", 2); got != "not-a-date" {
		t.Fatalf("a bad date must come back unchanged, got %q", got)
	}
}

// A pen with one ignored change among four handled ones is still a pen to look
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
// check shows THAT check's two sheets, not the window's ends.
func TestRowFiguresComeFromTheCheckThatDecidedTheVerdict(t *testing.T) {
	days := []domain.FeedFollowUpDay{
		{Status: domain.FeedFollowUpFollowed, BeforeDay: "2026-09-01", AfterDay: "2026-09-03", HeadBefore: 19, HeadAfter: 18},
		{Status: domain.FeedFollowUpNotFollowed, BeforeDay: "2026-09-10", AfterDay: "2026-09-12", HeadBefore: 12, HeadAfter: 12},
		{Status: domain.FeedFollowUpPending, BeforeDay: "2026-09-20", AfterDay: ""},
	}
	got, ok := domain.DecidingFeedFollowUpDay(days)
	if !ok || got.BeforeDay != "2026-09-10" {
		t.Fatalf("want the failed check, got %+v (ok=%v)", got, ok)
	}
	got, _ = domain.DecidingFeedFollowUpDay([]domain.FeedFollowUpDay{days[0], days[2]})
	if got.BeforeDay != "2026-09-20" {
		t.Fatalf("want the pending check, got %+v", got)
	}
	got, _ = domain.DecidingFeedFollowUpDay([]domain.FeedFollowUpDay{days[0], {Status: domain.FeedFollowUpFollowed, BeforeDay: "2026-09-05"}})
	if got.BeforeDay != "2026-09-05" {
		t.Fatalf("want the last passing check, got %+v", got)
	}
	if _, ok := domain.DecidingFeedFollowUpDay(nil); ok {
		t.Fatalf("no checks means no figures to show")
	}
}

// A PEN THAT EMPTIES LEAVES THE SHEET (maintainer question, 2026-09-23). Sell
// every animal out of a pen and it stops appearing on the sheet at all --
// there is nothing to pack for it. That absence is the STRONGEST reaction
// possible, and reporting it as "sheet not issued yet" a week later is false.
//
// Live case: CBE Godel 1 - Part 5 sold its last 13 on 16 Sep, is on the sheet
// on the 16th and 17th, and gone from the 18th on -- while the farm issued a
// sheet every one of those days.
func TestAPenThatEmptiesAndLeavesTheSheetReadsAsFedNothing(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-16",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-16", 13)},
		// The pen's own rows stop after the 17th.
		sheet(day("2026-09-16", 13, "20.0"), day("2026-09-17", 13, "20.0")),
		// The farm kept issuing sheets regardless.
		[]string{"2026-09-16", "2026-09-17", "2026-09-18", "2026-09-19", "2026-09-20"},
	)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- nothing is packed for an empty pen", got.Status)
	}
	if got.AfterDay != "2026-09-18" {
		t.Fatalf("after day = %q, want the first sheet the pen is absent from", got.AfterDay)
	}
	if got.HeadAfter != 0 || got.KgAfter != "0" {
		t.Fatalf("after reading = %d head / %q kg, want nothing fed", got.HeadAfter, got.KgAfter)
	}
	if got.Unexplained != 0 {
		t.Fatalf("unexplained = %d, want 0 -- 13 sold, 13 gone", got.Unexplained)
	}
}

// The other half of the same rule: when the farm has issued NO sheet at or past
// the deadline, absence means exactly what it says. Pending, not a pass, and
// never a fabricated zero.
func TestAbsenceWithNoIssuedSheetIsStillPending(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-16",
		[]domain.FeedFollowUpCause{cause(domain.FeedFollowUpSold, "2026-09-16", 13)},
		sheet(day("2026-09-16", 13, "20.0")),
		[]string{"2026-09-15", "2026-09-16", "2026-09-17"}, // nothing on or after the 18th
	)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if got.AfterDay != "" || got.KgAfter != "" {
		t.Fatalf("no reading may be invented: day=%q kg=%q", got.AfterDay, got.KgAfter)
	}
}
