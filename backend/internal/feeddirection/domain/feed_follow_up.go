package domain

import "sort"

// Feed follow-up: did the sheet react when animals entered or left a pen?
// ---------------------------------------------------------------------------
//
// The farm buys animals, sells animals and loses animals. Each of those changes
// how many mouths a pen holds, and a later sheet is supposed to carry the new
// number. The question this answers is the one the maintainer actually asks
// after a death or a sale: was the feed changed for that pen, or is the crew
// still packing for an animal that is not there?
//
// WHICH SHEET IS ALLOWED TO REACT IS THE WHOLE RULE, and getting it wrong makes
// the tab accuse the farm of ignoring an animal it could not physically have
// acted on. The feed day runs on a fixed clock (AGENTS.md -> Feed Direction
// stage-clock rule): the sheet for tomorrow is issued this MORNING, corrected
// at the park's CORRECTION TIME (14:00 today on both parks), packed, and staged
// out of the sheds by 15:00. So:
//
//	sold at 10:00 today   -> today's 14:00 correction can still fix it,
//	                         so TOMORROW's sheet must carry the new count
//	sold at 16:00 today   -> tomorrow's feed is already packed and frozen;
//	                         nothing can be done about it, and the first sheet
//	                         that can carry it is the DAY AFTER tomorrow
//
// The verdict is therefore read on the EXPECTED DAY -- the first sheet that
// could carry the change -- against the sheet immediately before it. An event
// after the cut-off is never judged against tomorrow's frozen sheet.
//
// The cut-off is NOT a constant here: it is read per park and per date from
// feed_schedule_config, the same row the generator and the correction run on.
// DefaultCorrectionTime is only the fallback for a park with no configured
// clock, so a farm that moves its correction to 13:00 moves this rule with it.
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

// DefaultCorrectionTime is the fallback cut-off for a park with NO configured
// clock, as HH:MM in Asia/Kolkata. Every park on the farm today configures
// 14:00 in feed_schedule_config and that row is what the rule reads; this
// exists so a park missing a clock still gets the farm's ordinary answer
// rather than no answer at all.
const DefaultCorrectionTime = "14:00"

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
	// EventDate is the IST business day the animal actually moved.
	EventDate string
	// AfterCutoff is true when it happened at or after the park's correction
	// time, so the NEXT day's sheet was already packed and frozen and the first
	// sheet that could carry it is the day after that.
	AfterCutoff bool
	Animals     int
	// Tags is a sample of at most FeedFollowUpMaxTags identifiers. TagsTotal
	// is how many there really were, so a truncated list can say so rather
	// than reading as the whole sale.
	Tags      []string
	TagsTotal int
}

// FeedFollowUpDay is ONE CHECK in one pen: the day the feed was supposed to
// change, everything that made it change, and the sheet on either side of it.
//
// The grain is the EXPECTED DAY, not the day the animal moved. Two events can
// land on one check -- a sale after yesterday's cut-off and a death before
// today's both point at tomorrow's sheet -- and that is correct: tomorrow's
// sheet has to account for both. Each event keeps its own date and its own
// side of the cut-off, so the reader can see why they arrived together.
type FeedFollowUpDay struct {
	// ExpectedDay is the first sheet day that could carry these changes.
	ExpectedDay string
	// CutoffTime is the park's correction time (HH:MM, IST) that decided it,
	// carried so the screen can name the actual cut-off rather than assume one.
	CutoffTime string

	Purchased int
	Sold      int
	Died      int
	// NetAnimals is purchased - sold - died: how many mouths the three causes
	// say the pen gained (negative when it lost).
	NetAnimals int

	// BeforeDay is the last sheet day BEFORE the expected day -- the sheet that
	// could not carry the change. AfterDay is the first sheet day ON OR AFTER
	// the expected day; it is empty when no such sheet has been issued yet.
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
	// ExpectedDay is the first sheet day that could carry this change: the day
	// after the event when it happened before the park's correction time, and
	// the day after that when it did not. Computed where the clock is read,
	// never re-derived by a caller.
	ExpectedDay string
	AfterCutoff bool
	Kind        string
	Animals     int
	Tags        []string
	TagsTotal   int
}

// ResolveFeedFollowUpDay judges ONE expected day against the sheet on either
// side of it. Pure: the caller supplies the pen's sheet days and the causes
// already carrying their expected day, and this picks the two readings and
// applies the rule.
//
// sheet must be ascending by FeedDay. Days are compared as ISO strings, which
// sort identically to dates and keeps business-date semantics (AGENTS.md: the
// vaccination/feed grain is the BUSINESS DAY, never an instant).
func ResolveFeedFollowUpDay(expectedDay string, causes []FeedFollowUpCause, sheet []FeedFollowUpSheetDay) FeedFollowUpDay {
	day := FeedFollowUpDay{ExpectedDay: expectedDay, Events: []FeedFollowUpEvent{}}
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
			Kind:        c.Kind,
			EventDate:   c.EventDate,
			AfterCutoff: c.AfterCutoff,
			Animals:     c.Animals,
			Tags:        c.Tags,
			TagsTotal:   c.TagsTotal,
		})
	}
	// Oldest event first, then by cause, so a check built from two days reads
	// in the order the farm lived it.
	sort.SliceStable(day.Events, func(i, j int) bool {
		if day.Events[i].EventDate != day.Events[j].EventDate {
			return day.Events[i].EventDate < day.Events[j].EventDate
		}
		return day.Events[i].Kind < day.Events[j].Kind
	})
	day.NetAnimals = day.Purchased - day.Sold - day.Died

	// BEFORE is the last sheet day STRICTLY BEFORE the expected day: the sheet
	// that could not carry the change. AFTER is the first sheet day ON OR AFTER
	// it -- the first sheet that could. A day the farm issued no sheet simply
	// is not there, so this walks to the next sheet rather than the next date.
	var before, after *FeedFollowUpSheetDay
	for i := range sheet {
		d := &sheet[i]
		if d.FeedDay < expectedDay {
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
		// The sheet that should carry this has not been issued yet. No verdict
		// is available, and declaring one would be inventing it.
		day.Status = FeedFollowUpPending
		return day
	}
	day.AfterDay = after.FeedDay
	day.HeadAfter = after.HeadCount
	day.KgAfter = after.Kg
	if before == nil {
		// The pen's first sheet in the window IS the expected day or later, so
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
	//
	// So a verdict is negative only when animals entered or left AND the sheet
	// carried the same mouths AND the same kg. Either figure moving is the
	// sheet reacting; whether it moved by the RIGHT amount is what Unexplained
	// and the feed-unchanged note beside it are for.
	feedHeld := day.KgBefore != "" && day.KgBefore == day.KgAfter
	switch {
	case day.NetAnimals != 0 && day.HeadDelta == 0 && feedHeld:
		day.Status = FeedFollowUpNotFollowed
	default:
		day.Status = FeedFollowUpFollowed
	}
	return day
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
