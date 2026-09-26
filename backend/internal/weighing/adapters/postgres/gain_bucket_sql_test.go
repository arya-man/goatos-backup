package postgres

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The WEEK variant must be byte-for-byte the query these reads carried before the bucket was
// selectable: the parameter exists to ADD a 30-day cut, never to move the weekly one. It must also
// not name the anchor parameter, because the week call does not bind it and Postgres refuses a
// statement whose parameter count and bind count disagree.
func TestWeekBucketKeepsTheCalendarWeekAndNeverNamesTheAnchor(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"growth", growthWeeklyGainQuery},
		{"demographics", bucketedQuery(demographicsTemplateFromSource(t), domain.GainBucketWeek, weightDemographicsAnchorParam)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.query, "{{BUCKET") {
				t.Fatalf("a bucket token survived rendering: %s", tc.name)
			}
			if !strings.Contains(tc.query, "date_trunc('week'") {
				t.Fatalf("the week variant must bucket on the calendar week")
			}
			if strings.Contains(tc.query, "$15::date") || strings.Contains(tc.query, "$37::date") {
				t.Fatalf("the week variant must not name the anchor parameter it does not bind")
			}
		})
	}
}

// The MONTH variant must drop the calendar week entirely -- a query carrying both would bucket one
// arm by week and the other by 30 days, and the page would show a headline that disagreed with the
// grid beside it -- and must count its blocks back from the anchor.
func TestMonthBucketReplacesEveryCalendarWeekWithA30DayBlock(t *testing.T) {
	for _, tc := range []struct {
		name, query, anchor string
	}{
		{"growth", growthWeeklyGainMonthQuery, "$15::date"},
		{"demographics", bucketedQuery(demographicsTemplateFromSource(t), domain.GainBucketMonth, weightDemographicsAnchorParam), "$37::date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.query, "date_trunc('week'") {
				t.Fatalf("a calendar week survived in the month variant")
			}
			if !strings.Contains(tc.query, tc.anchor) {
				t.Fatalf("the month variant must count back from %s", tc.anchor)
			}
			if !strings.Contains(tc.query, "/ 30)") {
				t.Fatalf("the month variant must divide the gap into 30-day blocks")
			}
		})
	}
}

// The arithmetic itself, stated once so a reader can check it without a database: the anchor's own
// day sits in the most recent block, the block before it starts 30 days earlier, and the block
// start is always 29 days before its last day.
func TestRollingBucketExprIsAnchorMinus29MinusWholeBlocks(t *testing.T) {
	got := rollingBucketExpr("$37::date", "d")
	want := "($37::date - 29 - 30 * (($37::date - d) / 30))"
	if got != want {
		t.Fatalf("rolling bucket expression:\n got %s\nwant %s", got, want)
	}
}

// The anchor is the window's LAST day, never the exclusive end. periodEnd is midnight IST of the
// day AFTER the window, so reading its business date directly would anchor on a day the window does
// not contain and shift every block by one.
func TestGainBucketAnchorIsTheWindowsLastDay(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	periodEnd := time.Date(2026, 9, 22, 0, 0, 0, 0, ist) // exclusive: the window ends on the 21st
	if got := gainBucketAnchor(periodEnd); got != "2026-09-21" {
		t.Fatalf("anchor = %s, want 2026-09-21", got)
	}
}

func TestNormalizeGainBucketDefaultsToWeekAndRefusesTheUnknown(t *testing.T) {
	for _, raw := range []string{"", "week", " WEEK "} {
		if got, ok := domain.NormalizeGainBucket(raw); !ok || got != domain.GainBucketWeek {
			t.Fatalf("NormalizeGainBucket(%q) = %q, %v; want week, true", raw, got, ok)
		}
	}
	if got, ok := domain.NormalizeGainBucket("month"); !ok || got != domain.GainBucketMonth {
		t.Fatalf("NormalizeGainBucket(month) = %q, %v", got, ok)
	}
	// Refused, never defaulted: a page that asked for 30-day blocks must not silently receive weeks.
	if _, ok := domain.NormalizeGainBucket("quarter"); ok {
		t.Fatalf("an unknown bucket must be refused")
	}
}

// demographicsTemplateFromSource reads the demographics template out of the file.
//
// It is a function-local `const q` on purpose: two source guards in this package pin that shape
// (the sectioned fast path prunes it by name, and one of them extracts the SQL by regex), so
// hoisting it to package scope to make it reachable from a test would break the thing the guards
// protect. Reading it here keeps both.
func demographicsTemplateFromSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("weight_demographics.go")
	if err != nil {
		t.Fatalf("read weight_demographics.go: %v", err)
	}
	match := regexp.MustCompile("(?s)const q = `(.*?)`\\n").FindStringSubmatch(string(src))
	if len(match) != 2 {
		t.Fatal("could not find the weight demographics template")
	}
	return match[1]
}
