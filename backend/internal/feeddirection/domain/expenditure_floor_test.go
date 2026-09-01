package domain

import (
	"testing"
	"time"
)

// The daily expenditure series floor (maintainer decision 2026-08-31): the
// chart starts at 11 Aug 2026 whatever range the page asks for; the rest of
// the stock read keeps the caller's window.
func TestClampExpenditureWindowFloorsTheSeriesStart(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	day := func(s string) time.Time {
		d, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return d
	}

	from, to := ClampExpenditureWindow(day("2026-07-01"), day("2026-08-30"))
	if from.Format("2006-01-02") != ExpenditureSeriesFloorDate {
		t.Errorf("window starting before the floor: want from %s, got %s", ExpenditureSeriesFloorDate, from.Format("2006-01-02"))
	}
	if to.Format("2006-01-02") != "2026-08-30" {
		t.Errorf("window end must be untouched, got %s", to.Format("2006-01-02"))
	}

	// A window already past the floor is untouched.
	from, to = ClampExpenditureWindow(day("2026-08-20"), day("2026-08-30"))
	if from.Format("2006-01-02") != "2026-08-20" || to.Format("2006-01-02") != "2026-08-30" {
		t.Errorf("post-floor window must be untouched, got %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}

	// A window entirely before the floor inverts (from > to) so a BETWEEN
	// serves absence, never fabricated zeros.
	from, to = ClampExpenditureWindow(day("2026-07-01"), day("2026-07-31"))
	if !to.Before(from) {
		t.Errorf("pre-floor window must come back empty-serving (from > to), got %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
}
