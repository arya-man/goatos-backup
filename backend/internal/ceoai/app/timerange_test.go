package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// trNow is a Wednesday (2026-07-22) 15:30 IST; every expected window below is
// derived from that business day in Asia/Kolkata.
var trNow = time.Date(2026, 7, 22, 15, 30, 0, 0, biztime.DefaultLocation())

func TestResolveWindowTable(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		from, to string
		cmpFrom  string // "" when no comparison expected
		cmpTo    string
	}{
		// relative days
		{"today", "how many deaths today", "2026-07-22", "2026-07-22", "", ""},
		{"yesterday", "vaccinations done yesterday", "2026-07-21", "2026-07-21", "", ""},
		{"day before yesterday", "feed fed day before yesterday", "2026-07-20", "2026-07-20", "", ""},
		// this / last units (Monday-start weeks)
		{"this week", "overdue this week", "2026-07-20", "2026-07-26", "", ""},
		{"current week", "current week feed variance", "2026-07-20", "2026-07-26", "", ""},
		{"last week", "deaths last week by park", "2026-07-13", "2026-07-19", "", ""},
		{"previous week", "previous week", "2026-07-13", "2026-07-19", "", ""},
		{"this month", "births this month", "2026-07-01", "2026-07-31", "", ""},
		{"current month", "current month", "2026-07-01", "2026-07-31", "", ""},
		{"last month", "mortality last month", "2026-06-01", "2026-06-30", "", ""},
		{"previous month", "previous month's feed", "2026-06-01", "2026-06-30", "", ""},
		{"this quarter", "sales this quarter", "2026-07-01", "2026-09-30", "", ""},
		{"last quarter", "last quarter mortality", "2026-04-01", "2026-06-30", "", ""},
		{"this year (ytd semantics)", "deaths this year", "2026-01-01", "2026-07-22", "", ""},
		{"last year", "how did last year go", "2025-01-01", "2025-12-31", "", ""},
		// to-date shorthands
		{"mtd", "mtd deaths", "2026-07-01", "2026-07-22", "", ""},
		{"month to date", "month to date", "2026-07-01", "2026-07-22", "", ""},
		{"ytd", "ytd births", "2026-01-01", "2026-07-22", "", ""},
		{"year to date", "year to date", "2026-01-01", "2026-07-22", "", ""},
		{"qtd", "qtd", "2026-07-01", "2026-07-22", "", ""},
		{"wtd", "week to date", "2026-07-20", "2026-07-22", "", ""},
		// last N units
		{"last 7 days", "deaths in the last 7 days", "2026-07-16", "2026-07-22", "", ""},
		{"past 30 days", "past 30 days", "2026-06-23", "2026-07-22", "", ""},
		{"last 2 weeks", "last 2 weeks", "2026-07-09", "2026-07-22", "", ""},
		{"last 3 months", "last 3 months", "2026-04-23", "2026-07-22", "", ""},
		{"last six months (word)", "last six months", "2026-01-23", "2026-07-22", "", ""},
		{"last 1 year", "last 1 year", "2025-07-23", "2026-07-22", "", ""},
		{"trailing 2 quarters", "trailing 2 quarters", "2026-01-23", "2026-07-22", "", ""},
		// quarters
		{"q2 explicit year", "q2 2026 mortality", "2026-04-01", "2026-06-30", "", ""},
		{"q4 no year (most recent past)", "q4 sales", "2025-10-01", "2025-12-31", "", ""},
		{"q1 short year", "q1'26", "2026-01-01", "2026-03-31", "", ""},
		{"third quarter of 2025", "third quarter of 2025", "2025-07-01", "2025-09-30", "", ""},
		// month names
		{"in june (this year)", "deaths in june", "2026-06-01", "2026-06-30", "", ""},
		{"august (rolls to last year)", "how many died in august", "2025-08-01", "2025-08-31", "", ""},
		{"august 2026 explicit", "august 2026", "2026-08-01", "2026-08-31", "", ""},
		{"aug '25", "aug '25 feed", "2025-08-01", "2025-08-31", "", ""},
		{"in may with preposition", "deaths in may", "2026-05-01", "2026-05-31", "", ""},
		{"may 2025 explicit", "feed cost may 2025", "2025-05-01", "2025-05-31", "", ""},
		// ISO / DMY
		{"iso range dotdot", "2026-07-01..2026-07-10", "2026-07-01", "2026-07-10", "", ""},
		{"iso range to", "from 2026-07-01 to 2026-07-10", "2026-07-01", "2026-07-10", "", ""},
		{"iso range reversed", "2026-07-10 to 2026-07-01", "2026-07-01", "2026-07-10", "", ""},
		{"single iso", "on 2026-07-05", "2026-07-05", "2026-07-05", "", ""},
		{"dmy range", "01/07/2026 to 10/07/2026", "2026-07-01", "2026-07-10", "", ""},
		{"single dmy", "deaths on 05/07/2026", "2026-07-05", "2026-07-05", "", ""},
		// since
		{"since iso", "deaths since 2026-07-01", "2026-07-01", "2026-07-22", "", ""},
		{"since dmy", "since 01/06/2026", "2026-06-01", "2026-07-22", "", ""},
		{"since month", "births since june", "2026-06-01", "2026-07-22", "", ""},
		{"since day month", "since 5 july", "2026-07-05", "2026-07-22", "", ""},
		{"since month day year", "since july 5, 2026", "2026-07-05", "2026-07-22", "", ""},
		// calendar year
		{"in 2025", "how many births in 2025", "2025-01-01", "2025-12-31", "", ""},
		// comparisons
		{"month vs month", "aug vs sep deaths", "2025-08-01", "2025-08-31", "2025-09-01", "2025-09-30"},
		{"this month vs last month", "feed this month vs last month", "2026-07-01", "2026-07-31", "2026-06-01", "2026-06-30"},
		{"compared to", "last week compared to previous week", "2026-07-13", "2026-07-19", "2026-07-13", "2026-07-19"},
		{"compare X and Y", "compare june and july mortality", "2026-06-01", "2026-06-30", "2026-07-01", "2026-07-31"},
		{"q1 versus q2", "q1 2026 versus q2 2026", "2026-01-01", "2026-03-31", "2026-04-01", "2026-06-30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := ResolveWindow(tc.in, trNow, nil)
			if !ok {
				t.Fatalf("expected a window for %q", tc.in)
			}
			if w.FromDate() != tc.from || w.ToDate() != tc.to {
				t.Fatalf("%q: got %s..%s want %s..%s", tc.in, w.FromDate(), w.ToDate(), tc.from, tc.to)
			}
			if tc.cmpFrom == "" {
				if w.Compare != nil {
					t.Fatalf("%q: unexpected comparison window %+v", tc.in, w.Compare)
				}
				return
			}
			if w.Compare == nil {
				t.Fatalf("%q: expected a comparison window", tc.in)
			}
			if w.Compare.FromDate() != tc.cmpFrom || w.Compare.ToDate() != tc.cmpTo {
				t.Fatalf("%q: compare got %s..%s want %s..%s", tc.in, w.Compare.FromDate(), w.Compare.ToDate(), tc.cmpFrom, tc.cmpTo)
			}
		})
	}
}

func TestResolveWindowUngroundable(t *testing.T) {
	for _, in := range []string{
		"", "   ", "sometime soon", "when convenient", "q3ish",
		"how many animals may be sick",      // bare "may" is not a month here
		"how many goats do we have",         // no period at all
		"2026-13-45",                        // malformed ISO
		"31/02/2026",                        // impossible DMY
		"since 2027-01-01",                  // future anchor cannot ground
		"which operators are behind today?", // wait: today IS a window
	} {
		w, ok := ResolveWindow(in, trNow, nil)
		if in == "which operators are behind today?" {
			if !ok || w.FromDate() != "2026-07-22" {
				t.Fatalf("today inside a question must resolve: %v %+v", ok, w)
			}
			continue
		}
		if ok {
			t.Fatalf("expected no window for %q, got %+v", in, w)
		}
	}
}

func TestResolveWindowLabelAndDescribe(t *testing.T) {
	w, ok := ResolveWindow("Deaths LAST MONTH vs this month", trNow, nil)
	if !ok {
		t.Fatal("expected window")
	}
	if w.Label != "last month" {
		t.Fatalf("label %q", w.Label)
	}
	if w.Compare == nil || w.Compare.Label != "this month" {
		t.Fatalf("compare label: %+v", w.Compare)
	}
	got := w.Describe()
	want := "last month (01/06/2026 to 30/06/2026) vs this month (01/07/2026 to 31/07/2026)"
	if got != want {
		t.Fatalf("Describe: got %q want %q", got, want)
	}
	single, _ := ResolveWindow("yesterday", trNow, nil)
	if single.Describe() != "yesterday (21/07/2026)" {
		t.Fatalf("single-day describe: %q", single.Describe())
	}
	if (Window{}).Describe() != "" {
		t.Fatal("zero window must describe as empty")
	}
}

func TestResolveWindowUsesBusinessCalendarNotUTC(t *testing.T) {
	// 2026-07-22 23:30 UTC is already 2026-07-23 05:00 IST: "today" must be the
	// IST business day, never the UTC date.
	utcLate := time.Date(2026, 7, 22, 23, 30, 0, 0, time.UTC)
	w, ok := ResolveWindow("today", utcLate, nil)
	if !ok || w.FromDate() != "2026-07-23" {
		t.Fatalf("today in IST: ok=%v from=%s", ok, w.FromDate())
	}
	if w.From.Location().String() != biztime.DefaultLocation().String() {
		t.Fatalf("window must be in the business location, got %s", w.From.Location())
	}
}

func TestResolveWindowFirstPhraseWinsWithoutConnector(t *testing.T) {
	// Two phrases with no comparison connector: the first is the window, no Compare.
	w, ok := ResolveWindow("last month deaths, and how about yesterday", trNow, nil)
	if !ok || w.FromDate() != "2026-06-01" || w.Compare != nil {
		t.Fatalf("got ok=%v %+v", ok, w)
	}
}
