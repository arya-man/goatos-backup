package app

import (
	"strings"
	"testing"
)

// The Time-wise tab of the ADG Analytics page reads every heading, caption, empty state and note
// through a PAIR of authored strings: the calendar-week wording, and the 30-day wording the reader
// sees when they select Monthly (maintainer request 2026-09-21). The page picks one or the other by
// appending `.month`; it never edits a string.
//
// A missing twin is not a cosmetic gap. The page throws on an unknown copy key, so shipping a new
// Time-wise section with only its week wording takes the WHOLE analytics page down the moment a
// reader switches to Monthly. This test is the reason that cannot happen quietly.
func TestEveryTimeWiseCopyKeyHasItsThirtyDayTwin(t *testing.T) {
	copyMap := pageSpecificCopy("weighing-analytics")
	if len(copyMap) == 0 {
		t.Fatal("the ADG Analytics page must carry its own copy")
	}

	// The keys the page reads bucket-aware. Stated here rather than derived, so adding a section to
	// the tab means adding its key to this list and being told about the twin.
	bucketAware := []string{
		"section.time.title", "section.time.caption", "section.time.aria", "empty.time.body",
		"note.time.gaps",
		"section.time.breed.title", "section.time.breed.caption", "section.time.breed.aria", "empty.time.breed.body",
		"section.time.pen.title", "section.time.pen.caption", "section.time.pen.aria", "empty.time.pen.body",
		"section.time.load.title", "section.time.load.caption", "section.time.load.aria", "empty.time.load.body",
	}
	for _, key := range bucketAware {
		week, okWeek := copyMap[key]
		month, okMonth := copyMap[key+".month"]
		if !okWeek {
			t.Fatalf("%s: the week wording is missing", key)
		}
		if !okMonth {
			t.Fatalf("%s.month: the 30-day wording is missing; Monthly would throw on this key", key)
		}
		if strings.TrimSpace(month) == "" {
			t.Fatalf("%s.month: blank copy is not a wording", key)
		}
		// The twin must not still say "week" -- a wording copied from its sibling reads "Weekly
		// growth" over 30-day columns, which is the mislabelling the pair exists to prevent. Two
		// IDENTICAL strings are allowed and expected where the sentence never named the bucket at
		// all ("No breed has a kid weighed twice in this period"): the twin exists so the key
		// resolves, not to say something different for its own sake.
		if strings.Contains(strings.ToLower(month), "week") {
			t.Fatalf("%s.month still says week: %q", key, month)
		}
		// And where the week wording DOES name its bucket, the twin must actually differ.
		if strings.Contains(strings.ToLower(week), "week") && week == month {
			t.Fatalf("%s: the 30-day wording is a copy of a weekly one", key)
		}
	}

	// The two controls' own copy, and the vocabulary behind the Weekly / Monthly select.
	for _, key := range []string{
		"filter.gain_bucket.label", "filter.gain_bucket.note",
		"filter.time_pen.label", "filter.time_pen.note",
	} {
		if strings.TrimSpace(copyMap[key]) == "" {
			t.Fatalf("%s: the Time-wise controls' copy is backend-owned and missing", key)
		}
	}
}

// Weekly / Monthly is an OPTION GROUP, so the labels are the farm's words and the page renders no
// literal of its own. Monthly must say what it is -- a 30-day block, not a calendar month -- because
// that is the difference the reader is choosing.
func TestGainBucketOptionGroupNamesTheThirtyDayBlock(t *testing.T) {
	var found bool
	for _, group := range weighingWeightsOptionGroups() {
		if group.ID != "gain_bucket" {
			continue
		}
		found = true
		if len(group.Options) != 2 {
			t.Fatalf("gain_bucket must offer exactly weekly and monthly, got %d options", len(group.Options))
		}
		if group.Options[0].Key != "week" || group.Options[1].Key != "month" {
			t.Fatalf("gain_bucket keys must be week, month: got %q, %q", group.Options[0].Key, group.Options[1].Key)
		}
		if !strings.Contains(group.Options[1].Label, "30") {
			t.Fatalf("the monthly label must say 30 days, not imply a calendar month: %q", group.Options[1].Label)
		}
	}
	if !found {
		t.Fatal("the gain_bucket option group is missing")
	}
}
