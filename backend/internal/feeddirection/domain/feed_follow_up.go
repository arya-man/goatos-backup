package domain

import (
	"sort"
	"time"
)

// Feed follow-up: did the sheet react when animals entered or left a pen?
// ---------------------------------------------------------------------------
//
// The farm buys animals, sells animals and loses animals. Each of those changes
// how many mouths a pen holds, and a later sheet is supposed to carry the new
// number. The question this answers is the one the maintainer actually asks
// after a death or a sale: was the feed changed for that pen, or is the crew
// still packing for an animal that is not there?
//
// WHICH SHEETS ARE COMPARED IS THE WHOLE RULE, and getting it wrong makes the
// tab accuse the farm of ignoring an animal it demonstrably did not.
//
// The feed day runs on a fixed clock (AGENTS.md -> Feed Direction stage-clock
// rule): the sheet for tomorrow is issued this MORNING, corrected at the park's
// correction time, and locked for packing mid-afternoon. A change entered
// before that lock can reach tomorrow's sheet; one entered after it cannot --
// that feed is already bagged -- and the first sheet that can carry it is the
// day after tomorrow.
//
// SO THE CHECK SPANS BOTH, and does NOT try to decide which of the two it
// should have been. It compares the last sheet ON OR BEFORE the day the animal
// moved against the first sheet FeedFollowUpReactionDays later. If the pen's
// count moved anywhere across that span, the farm reacted.
//
// WHY NOT USE THE TIME OF DAY (this is the important part, found on live data
// 2026-09-23): the recorded instant is when the day's sales were ENTERED, not
// when the animals left. All 154 sale rows on the live database share TEN
// distinct instants -- one per day, identical to the microsecond -- and most of
// them fall in the evening, after the lock. Keying the cut-off on that stamp
// pushed nearly every sale to "judge the day after tomorrow" and then compared
// two sheets that BOTH already carried the change: CBE Godel 2 - Part 1 was fed
// for 5 animals on the 12th and 1 on the 13th, exactly the 4 sold, and the tab
// still reported "Feed unchanged" because it was looking at the 13th against
// the 14th. A rule keyed on a data-entry timestamp reports the farm's
// bookkeeping habits, not its feeding.
//
// The cost of the wider span is stated plainly: it cannot say WHETHER the farm
// reacted on the first day or the second, only that it did. Nobody asks that
// question, and the alternative is a false accusation.
//
// WHAT MAKES THIS HONEST RATHER THAN DERIVED. Both halves are already stored
// facts. The pen's head count and kg come from feed_direction_issue_rows -- the
// FROZEN sheet, what the farm was actually instructed to pack, not a recompute
// of what it should have been. The events come from the herd register's own
// exits and intakes. Nothing here re-plans a sheet; it reads what was planned
// and what happened, and puts them side by side.
//
// THE RULE IS DELIBERATELY BLUNT: a pen that lost an animal and whose expected
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

// FeedFollowUpReactionDays is how far ahead the check looks for the sheet that
// had to carry a change. TWO days, because a change entered after a day's sheet
// is locked for packing cannot reach tomorrow at all, and the day after is the
// first one that can. See the header for why the recorded time of day cannot be
// used to narrow this to one day.
const FeedFollowUpReactionDays = 2

// FeedFollowUpMaxTags caps the identifiers carried per cause per day. The
// expanded row names the animals so the reader can walk out and check the pen;
// a sale of eighty animals does not need eighty strings on screen to make that
// point, and the count beside them stays exact either way.
const FeedFollowUpMaxTags = 12

// FeedFollowUpEvent is one cause on one day in one pen: how many animals, and
// which ones. Tags are RFID / tag numbers as the register holds them, never
// internal goat ids (AGENTS.md -> Mesha / Goat OS RFID Language).
type FeedFollowUpEvent struct {
	Kind string
	// EventDate is the IST business day the register records the animal moving
	// on. Only the DAY is trusted -- see the header on the time of day.
	EventDate string
	Animals   int
	// Tags is a sample of at most FeedFollowUpMaxTags identifiers. TagsTotal
	// is how many there really were, so a truncated list can say so rather
	// than reading as the whole sale.
	Tags      []string
	TagsTotal int
}

// FeedFollowUpDay is ONE CHECK in one pen: a day animals moved, and the sheet
// on either side of the span the farm had to react in.
type FeedFollowUpDay struct {
	// EventDate is the day the animals moved.
	EventDate string

	Purchased int
	Sold      int
	Died      int
	// NetAnimals is purchased - sold - died: how many mouths the three causes
	// say the pen gained (negative when it lost).
	NetAnimals int

	// BeforeDay is the last sheet day ON OR BEFORE the event -- the sheet that
	// was already packed when the animals moved. AfterDay is the first sheet
	// day at or after EventDate + FeedFollowUpReactionDays, which is the first
	// one that must carry the change however late in the day it was entered.
	// AfterDay is empty when no such sheet has been issued yet.
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

	// FirstDay / LastDay and the four readings below are THE COMPARISON THAT
	// PRODUCED Status -- the deciding check's two sheets, not the window's
	// ends. They were the window's ends once, and a row then read "19 -> 19
	// animals, 26 -> 30.6 kg, Not followed": three figures contradicting each
	// other on one line, because the verdict came from one day's comparison
	// while the numbers beside it spanned a month. A reader has to be able to
	// explain the verdict from the row it sits on.
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

// ResolveFeedFollowUpDay judges ONE event day against the sheets on either side
// of the span the farm had to react in. Pure: the caller supplies the pen's
// sheet days and this picks the two readings and applies the rule.
//
// sheet must be ascending by FeedDay. Days are compared as ISO strings, which
// sort identically to dates and keeps business-date semantics (AGENTS.md: the
// vaccination/feed grain is the BUSINESS DAY, never an instant).
// issuedDays are the days the pen's PARK issued a sheet at all, ascending. They
// are what tells "the pen is no longer fed" apart from "no sheet exists yet".
func ResolveFeedFollowUpDay(eventDate string, causes []FeedFollowUpCause, sheet []FeedFollowUpSheetDay, issuedDays []string) FeedFollowUpDay {
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
			EventDate: c.EventDate,
			Animals:   c.Animals,
			Tags:      c.Tags,
			TagsTotal: c.TagsTotal,
		})
	}
	sort.SliceStable(day.Events, func(i, j int) bool { return day.Events[i].Kind < day.Events[j].Kind })
	day.NetAnimals = day.Purchased - day.Sold - day.Died

	// BEFORE is the last sheet ON OR BEFORE the event day: what the pen was
	// being fed when the animals moved. AFTER is the first sheet at or after
	// the reaction deadline -- the first that must carry the change whatever
	// time of day it was entered. A day the farm issued no sheet simply is not
	// there, so this walks on to the next sheet rather than the next date.
	deadline := AddBusinessDays(eventDate, FeedFollowUpReactionDays)
	var before, after *FeedFollowUpSheetDay
	for i := range sheet {
		d := &sheet[i]
		if d.FeedDay <= eventDate {
			before = d
		}
		if after == nil && d.FeedDay >= deadline {
			after = d
		}
	}
	if before != nil {
		day.BeforeDay = before.FeedDay
		day.HeadBefore = before.HeadCount
		day.KgBefore = before.Kg
	}
	if after == nil {
		// THE PEN LEFT THE SHEET. A pen whose animals have all gone stops
		// appearing on the sheet at all -- there is nothing to pack for it --
		// and that absence is the STRONGEST possible reaction, not a missing
		// verdict. CBE Godel 1 - Part 5 sold its last 13 animals on 16 Sep, is
		// on the sheet on the 16th and 17th and gone from the 18th onward, and
		// the farm issued a sheet every one of those days. Reporting that as
		// "sheet not issued yet" a week later is simply false.
		//
		// So absence only means PENDING when the farm issued no sheet at all.
		// When it issued one and this pen is not on it, the pen was fed
		// nothing, and that reads as a zero.
		if firstIssued, ok := firstDayFrom(issuedDays, deadline); ok {
			day.AfterDay = firstIssued
			day.HeadAfter = 0
			day.KgAfter = "0"
		} else {
			// Genuinely nothing to compare against yet.
			day.Status = FeedFollowUpPending
			return day
		}
	} else {
		day.AfterDay = after.FeedDay
		day.HeadAfter = after.HeadCount
		day.KgAfter = after.Kg
	}
	if before == nil {
		// The pen's first sheet in the window is already past the event, so
		// there is no earlier reading to compare against. Pending, not a pass.
		day.Status = FeedFollowUpPending
		return day
	}
	day.HeadDelta = day.HeadAfter - day.HeadBefore
	day.Unexplained = day.HeadDelta - day.NetAnimals
	// NOTHING MOVED AT ALL is the finding -- not "the head count did not move".
	// The maintainer's question is whether the FEED changed, and a pen can be
	// fed for the same number of mouths on a different quantity (a ration
	// change, a stage move). Judging on the head count alone called a pen that
	// dropped 100.4 kg to 90.4 kg "not followed", which is plainly false to
	// anyone reading the two columns beside the verdict.
	feedHeld := day.KgBefore != "" && day.KgBefore == day.KgAfter
	switch {
	case day.NetAnimals != 0 && day.HeadDelta == 0 && feedHeld:
		day.Status = FeedFollowUpNotFollowed
	default:
		day.Status = FeedFollowUpFollowed
	}
	return day
}

// firstDayFrom returns the first day in an ascending list at or after `from`.
// Only the AFTER side of a check uses it: an absent pen there means "fed
// nothing", which is unambiguous, while an absent pen BEFORE an event that sold
// animals out of it is contradictory data and stays a missing reading rather
// than being read as a zero.
func firstDayFrom(days []string, from string) (string, bool) {
	for _, d := range days {
		if d >= from {
			return d, true
		}
	}
	return "", false
}

// AddBusinessDays shifts an ISO business date by whole days. Dates travel as
// strings through this read (they are business days, never instants), so this
// is the one place that has to parse one. An unparseable date is returned
// unchanged rather than defaulted, so a bad value cannot silently become a
// different day's deadline.
func AddBusinessDays(date string, days int) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}

// DecidingFeedFollowUpDay picks the check a pen's verdict came from: the first
// day that failed, else the first still waiting, else the last one that passed.
// The row's own figures are taken from it, so the numbers on a row always
// explain the verdict printed beside them.
func DecidingFeedFollowUpDay(days []FeedFollowUpDay) (FeedFollowUpDay, bool) {
	var pending *FeedFollowUpDay
	for i := range days {
		switch days[i].Status {
		case FeedFollowUpNotFollowed:
			return days[i], true
		case FeedFollowUpPending:
			if pending == nil {
				pending = &days[i]
			}
		}
	}
	if pending != nil {
		return *pending, true
	}
	if len(days) == 0 {
		return FeedFollowUpDay{}, false
	}
	return days[len(days)-1], true
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
