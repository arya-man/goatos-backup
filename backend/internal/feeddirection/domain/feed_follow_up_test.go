package domain_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

func sheet(days ...domain.FeedFollowUpSheetDay) []domain.FeedFollowUpSheetDay { return days }

func day(d string, heads int, kg string) domain.FeedFollowUpSheetDay {
	return domain.FeedFollowUpSheetDay{FeedDay: d, HeadCount: heads, Kg: kg}
}

// The finding the tab exists for: an animal died and the next sheet still fed
// the pen for every mouth it had before.
func TestDeathTheSheetIgnoredIsNotFollowed(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-14", Kind: domain.FeedFollowUpDied, Animals: 1, Tags: []string{"RF-1"}, TagsTotal: 1}},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpNotFollowed {
		t.Fatalf("status = %q, want not_followed", got.Status)
	}
	if got.NetAnimals != -1 || got.HeadDelta != 0 {
		t.Fatalf("net=%d delta=%d, want net=-1 delta=0", got.NetAnimals, got.HeadDelta)
	}
	// The pen was over-fed by exactly the animal that is not there.
	if got.Unexplained != 1 {
		t.Fatalf("unexplained = %d, want 1", got.Unexplained)
	}
}

// The same death, handled: the next sheet carries one mouth fewer and less feed.
func TestDeathTheSheetActedOnIsFollowed(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-14", Kind: domain.FeedFollowUpDied, Animals: 1}},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 43, "17.8")),
	)
	if got.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed", got.Status)
	}
	if got.Unexplained != 0 {
		t.Fatalf("unexplained = %d, want 0", got.Unexplained)
	}
	if got.KgBefore != "18.2" || got.KgAfter != "17.8" {
		t.Fatalf("kg %q -> %q, want 18.2 -> 17.8", got.KgBefore, got.KgAfter)
	}
}

// A purchase lands and the pen is fed for more mouths.
func TestPurchaseRaisesTheSheet(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-14", Kind: domain.FeedFollowUpPurchased, Animals: 12}},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 56, "23.1")),
	)
	if got.Status != domain.FeedFollowUpFollowed || got.NetAnimals != 12 || got.HeadDelta != 12 {
		t.Fatalf("got %+v, want followed with net=delta=12", got)
	}
}

// A sale and a purchase on one day that cancel out: the sheet SHOULD stand
// still, and standing still is following. Blunt "delta must be non-zero" would
// call this a finding and send someone to look at a pen that is correct.
func TestCausesThatCancelOutAreFollowedWhenTheSheetHoldsSteady(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{
			{EventDate: "2026-09-14", Kind: domain.FeedFollowUpPurchased, Animals: 3},
			{EventDate: "2026-09-14", Kind: domain.FeedFollowUpSold, Animals: 3},
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
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-14", Kind: domain.FeedFollowUpDied, Animals: 1}},
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

// Nothing has been issued since the event: there is no verdict to give, and
// giving one either way would be inventing it.
func TestNoSheetSinceTheEventIsPendingNotAPass(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-15",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-15", Kind: domain.FeedFollowUpSold, Animals: 8}},
		sheet(day("2026-09-14", 44, "18.2"), day("2026-09-15", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if got.AfterDay != "" || got.HeadAfter != 0 {
		t.Fatalf("after reading = %q/%d, want empty", got.AfterDay, got.HeadAfter)
	}
}

// The pen's first sheet of the window lands after the event, so there is no
// earlier reading to compare against. Pending, never a pass.
func TestEventBeforeTheFirstSheetIsPending(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-10",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-10", Kind: domain.FeedFollowUpDied, Animals: 2}},
		sheet(day("2026-09-14", 44, "18.2")),
	)
	if got.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
}

// "The next day" means the next SHEET, not the next date: a day the farm
// issued nothing has no head count to read, and skipping to the following
// sheet is the only reading that exists.
func TestTheNextDayIsTheNextSHEETNotTheNextDate(t *testing.T) {
	got := domain.ResolveFeedFollowUpDay(
		"2026-09-14",
		[]domain.FeedFollowUpCause{{EventDate: "2026-09-14", Kind: domain.FeedFollowUpDied, Animals: 1}},
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
