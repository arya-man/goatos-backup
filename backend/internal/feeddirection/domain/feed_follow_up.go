package domain

import "sort"

// Feed follow-up: did the sheet react when animals entered or left a pen?
// ---------------------------------------------------------------------------
//
// The farm buys animals, sells animals and loses animals. Each of those changes
// how many mouths a pen holds, and the NEXT day's feed sheet is supposed to
// carry the new number. The question this answers is the one the maintainer
// actually asks after a death or a sale: "was the feed reduced for that pen the
// next day, or is the crew still packing for an animal that is not there?"
//
// WHAT MAKES THIS HONEST RATHER THAN DERIVED. Both halves are already stored
// facts. The pen's head count and kg come from feed_direction_issue_rows -- the
// FROZEN sheet, what the farm was actually instructed to pack, not a recompute
// of what it should have been. The events come from the herd register's own
// exits and intakes. Nothing here re-plans a sheet; it reads what was planned
// and what happened, and puts them side by side.
//
// THE RULE IS DELIBERATELY BLUNT: a pen that lost an animal and whose next
// sheet carries the SAME head count did not follow. That is the state worth
// acting on, and softening it into "close enough" would hide exactly the case
// the tab exists for.
//
// WHY SHIFTING IS NOT A CAUSE HERE (maintainer decision 2026-09-22). Animals
// moved between pens also change a pen's mouths, and the tab does NOT count
// them: the three causes are purchased, sold and died. A shifted pen therefore
// shows its head-count move as UNEXPLAINED rather than as a cause, which is the
// honest reading -- the tab says the number moved and does not claim to know
// why. Inventing a fourth cause it was not asked for would be worse than
// leaving the remainder visible.

// Feed follow-up causes. A cause is why a pen's mouths changed; it is never a
// verdict about the sheet.
const (
	// FeedFollowUpPurchased is an animal accepted into the herd off a
	// procurement load: the pen gained a mouth.
	FeedFollowUpPurchased = "purchased"
	// FeedFollowUpSold is an animal tagged to a sale: the pen lost a mouth.
	FeedFollowUpSold = "sold"
	// FeedFollowUpDied is an animal recorded dead: the pen lost a mouth.
	FeedFollowUpDied = "died"
)

// Feed follow-up verdicts, one per event day and rolled up per pen.
const (
	// FeedFollowUpFollowed means the pen's fed head count moved after the
	// event day. It does NOT claim the move was the right size -- an
	// Unexplained remainder rides alongside and says so.
	FeedFollowUpFollowed = "followed"
	// FeedFollowUpNotFollowed means animals entered or left and the NEXT
	// issued sheet fed the pen for exactly as many mouths as before. This is
	// the only state anyone has to act on.
	FeedFollowUpNotFollowed = "not_followed"
	// FeedFollowUpPending means no sheet has been issued for the pen after
	// the event day yet, so there is nothing to judge. Absence of a verdict,
	// never a pass.
	FeedFollowUpPending = "pending"
)

// FeedFollowUpMaxTags caps the identifiers carried per cause per day. The
// expanded row names the animals so the reader can walk out and check the pen;
// a sale of eighty animals does not need eighty strings on screen to make that
// point, and the count beside them stays exact either way.
const FeedFollowUpMaxTags = 12

// FeedFollowUpEvent is one cause on one day in one pen: how many animals, and
// which ones. Tags are RFID / tag numbers as the register holds them, never
// internal goat ids (AGENTS.md -> Mesha / Goat OS RFID Language).
type FeedFollowUpEvent struct {
	Kind      string
	EventDate string
	Animals   int
	// Tags is a sample of at most FeedFollowUpMaxTags identifiers. TagsTotal
	// is how many there really were, so a truncated list can say so rather
	// than reading as the whole sale.
	Tags      []string
	TagsTotal int
}

// FeedFollowUpDay is ONE event day in one pen, with the sheet on either side of
// it. Before is the last sheet day on or before the event; After is the first
// sheet day strictly after it -- "the next day's feed", which is what the farm
// means, and which is not always literally tomorrow because a day with no
// issued sheet has no row to read.
type FeedFollowUpDay struct {
	EventDate string
	Purchased int
	Sold      int
	Died      int
	// NetAnimals is purchased - sold - died: how many mouths the three causes
	// say the pen gained (negative when it lost).
	NetAnimals int

	// BeforeDay / AfterDay are the sheet days the two readings come from.
	// AfterDay is empty when no sheet has been issued since the event.
	BeforeDay string
	AfterDay  string

	HeadBefore int
	HeadAfter  int
	KgBefore   string
	KgAfter    string

	// HeadDelta is HeadAfter - HeadBefore: what the sheet actually did.
	HeadDelta int
	// Unexplained is HeadDelta - NetAnimals. Non-zero means the head count
	// moved by an amount these three causes do not account for -- a shift in
	// or out, a birth, or a correction. It is REPORTED, never forced to zero.
	Unexplained int

	Status string
	Events []FeedFollowUpEvent
}

// FeedFollowUpPenRow is one operational location's whole window: what entered
// and left, what the sheet fed at each end, and whether it kept up.
//
// The location is carried as SEPARATE identity fields plus the backend-composed
// display (oploc.Display), per the operational-location convention -- a client
// renders OperationalLocationDisplay verbatim and never re-derives it.
type FeedFollowUpPenRow struct {
	ParkID    string
	ParkLabel string
	ShedID    string
	ShedLabel string
	// PartitionLabel is the human label ("Part 3", "2"); empty for an
	// undivided shed. Never the normalized matching key, and never 'whole'.
	PartitionLabel             string
	OperationalLocationDisplay string

	Purchased int
	Sold      int
	Died      int

	// FirstDay / LastDay are the window's first and last sheet days FOR THIS
	// PEN, and the four readings below are taken on them. A pen fed on one day
	// only has both equal and no movement to show, which is the truth.
	FirstDay   string
	LastDay    string
	HeadBefore int
	HeadAfter  int
	KgBefore   string
	KgAfter    string

	// Status is the row's worst day: NotFollowed if any day was, else Pending
	// if any day is still waiting for its next sheet, else Followed.
	Status string
	// Days are the pen's event days, ascending. A day with no cause is absent
	// -- this tab answers for the days something happened.
	Days []FeedFollowUpDay
}

// FeedFollowUpTotals is the window's walk from the animals the farm started
// with to the animals it ended with, by cause. It is what the waterfall draws.
//
// Start and End are sums of the pens' own first/last SHEET head counts, so they
// are "mouths the sheet fed", not a census -- the same basis every other figure
// on this page uses. Start + Purchased - Sold - Died therefore need NOT equal
// End, and Unexplained carries that difference openly rather than the chart
// quietly closing its own walk.
type FeedFollowUpTotals struct {
	StartAnimals int
	Purchased    int
	Sold         int
	Died         int
	EndAnimals   int
	Unexplained  int

	// StartKg / EndKg are the same two ends measured in directed kg, for the
	// second reading of the same walk.
	StartKg string
	EndKg   string

	// Pen verdict counts, which is what the status strip shows.
	Followed    int
	NotFollowed int
	Pending     int
}

// FeedFollowUp is the /feed-analytics/follow-up payload.
type FeedFollowUp struct {
	Totals FeedFollowUpTotals
	Rows   []FeedFollowUpPenRow
}

// FeedFollowUpSheetDay is one pen-day of the frozen sheet, as read from
// feed_direction_issue_rows: the mouths it was packed for and the kg it
// directed. Ascending by day within a pen.
type FeedFollowUpSheetDay struct {
	FeedDay   string
	HeadCount int
	Kg        string
}

// FeedFollowUpCause is one (pen, day, cause) tally straight off the herd
// register, before any sheet is looked at.
type FeedFollowUpCause struct {
	EventDate string
	Kind      string
	Animals   int
	Tags      []string
	TagsTotal int
}

// ResolveFeedFollowUpDay judges ONE event day against the sheet on either side
// of it. Pure: the caller supplies the pen's sheet days, this picks the two
// readings and applies the rule.
//
// sheet must be ascending by FeedDay. Days are compared as ISO strings, which
// sort identically to dates and keeps business-date semantics (AGENTS.md: the
// vaccination/feed grain is the BUSINESS DAY, never an instant).
func ResolveFeedFollowUpDay(eventDate string, causes []FeedFollowUpCause, sheet []FeedFollowUpSheetDay) FeedFollowUpDay {
	day := FeedFollowUpDay{EventDate: eventDate, Events: []FeedFollowUpEvent{}}
	for _, c := range causes {
		switch c.Kind {
		case FeedFollowUpPurchased:
			day.Purchased += c.Animals
		case FeedFollowUpSold:
			day.Sold += c.Animals
		case FeedFollowUpDied:
			day.Died += c.Animals
		}
		day.Events = append(day.Events, FeedFollowUpEvent{
			Kind:      c.Kind,
			EventDate: eventDate,
			Animals:   c.Animals,
			Tags:      c.Tags,
			TagsTotal: c.TagsTotal,
		})
	}
	sort.SliceStable(day.Events, func(i, j int) bool { return day.Events[i].Kind < day.Events[j].Kind })
	day.NetAnimals = day.Purchased - day.Sold - day.Died

	// BEFORE is the last sheet day ON OR BEFORE the event: the sheet that was
	// already packed when the animal arrived or left. AFTER is the first sheet
	// day STRICTLY after it: the first sheet that could have carried the new
	// number. A day the farm issued no sheet simply is not there, so "the next
	// day" is the next sheet, not the next date.
	var before, after *FeedFollowUpSheetDay
	for i := range sheet {
		d := &sheet[i]
		if d.FeedDay <= eventDate {
			before = d
			continue
		}
		after = d
		break
	}
	if before != nil {
		day.BeforeDay = before.FeedDay
		day.HeadBefore = before.HeadCount
		day.KgBefore = before.Kg
	}
	if after == nil {
		// Nothing has been issued since. No verdict is available, and
		// declaring one would be inventing it.
		day.Status = FeedFollowUpPending
		return day
	}
	day.AfterDay = after.FeedDay
	day.HeadAfter = after.HeadCount
	day.KgAfter = after.Kg
	if before == nil {
		// The pen's first sheet in the window lands AFTER the event, so there
		// is no earlier reading to compare against. Pending, not a pass.
		day.Status = FeedFollowUpPending
		return day
	}
	day.HeadDelta = day.HeadAfter - day.HeadBefore
	day.Unexplained = day.HeadDelta - day.NetAnimals
	switch {
	case day.NetAnimals != 0 && day.HeadDelta == 0:
		// Animals entered or left and the sheet fed the pen for exactly as
		// many mouths as before. This is the finding.
		day.Status = FeedFollowUpNotFollowed
	default:
		day.Status = FeedFollowUpFollowed
	}
	return day
}

// RollUpFeedFollowUpStatus folds a pen's event days into the row's verdict:
// the worst day wins, because a pen that ignored one death is a pen to look at
// even if it handled the other four.
func RollUpFeedFollowUpStatus(days []FeedFollowUpDay) string {
	status := FeedFollowUpFollowed
	for _, d := range days {
		if d.Status == FeedFollowUpNotFollowed {
			return FeedFollowUpNotFollowed
		}
		if d.Status == FeedFollowUpPending {
			status = FeedFollowUpPending
		}
	}
	return status
}
