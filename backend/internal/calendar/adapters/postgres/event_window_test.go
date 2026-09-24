package postgres

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestParseCanonicalEventAnchorCoversEveryDatedShape(t *testing.T) {
	const u = "730a15d6-ae25-4d9a-b2e7-44c464f8903a"
	cases := map[string]canonicalEventAnchor{
		"vaccinationdrive:assignment:" + u:                     {kind: anchorAssignment, id: u},
		"batch:" + u:                                           {kind: anchorBatch, id: u},
		"batch:" + u + ":rule:" + u + ":shed:" + u:             {kind: anchorBatch, id: u},
		"parkdrive:park:" + u + ":date:2026-09-04":             {kind: anchorDay, day: "2026-09-04"},
		"parkdrive:tenant:" + u + ":date:2026-09-04":           {kind: anchorDay, day: "2026-09-04"},
		"catchup:park:" + u + ":due:2026-09-05":                {kind: anchorDay, day: "2026-09-05"},
		"catchup:shed:" + u + ":rule:" + u + ":due:2026-09-06": {kind: anchorDay, day: "2026-09-06"},
		"obligation:" + u:                                      {kind: anchorObligation, id: u},
		"completion:" + u:                                      {kind: anchorCompletion, id: u},
		"calendar:" + u:                                        {kind: anchorNone},
		"obligation:not-a-uuid":                                {kind: anchorNone},
		"something-else":                                       {kind: anchorNone},
	}
	for id, want := range cases {
		if got := parseCanonicalEventAnchor(id); got != want {
			t.Errorf("parseCanonicalEventAnchor(%q) = %+v, want %+v", id, got, want)
		}
	}
}

// The narrowed window must be business-DAY anchored (Asia/Kolkata midnights, never hour offsets),
// contain the anchor day, and always contain today so the drive-rollover gate
// (calendarTodayInRequestedWindow) evaluates exactly as it does under the wide window.
func TestCanonicalNarrowWindowIsBusinessDayAnchoredAndSpansToday(t *testing.T) {
	loc := biztime.DefaultLocation()
	// 23:30 IST on 24 Sep is still the 24th in India (and already the 24th 18:00 UTC).
	now := time.Date(2026, 9, 24, 23, 30, 0, 0, loc)
	day := func(s string) time.Time {
		d, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	cases := []struct {
		minDay, maxDay, from, to string
	}{
		{"2026-09-04", "2026-09-04", "2026-09-03", "2026-09-26"}, // past anchor: [anchor-1, today+2)
		{"2026-10-20", "2026-10-20", "2026-09-23", "2026-10-22"}, // future anchor: [today-1, anchor+2)
		{"2026-09-24", "2026-09-24", "2026-09-23", "2026-09-26"}, // today
		{"2026-09-01", "2026-10-02", "2026-08-31", "2026-10-04"}, // batch span straddling today
	}
	for _, tc := range cases {
		from, to, err := canonicalNarrowWindow(tc.minDay, tc.maxDay, now)
		if err != nil {
			t.Fatal(err)
		}
		if !from.Equal(day(tc.from)) || !to.Equal(day(tc.to)) {
			t.Errorf("window(%s..%s) = [%s, %s), want [%s, %s)", tc.minDay, tc.maxDay, from.In(loc), to.In(loc), tc.from, tc.to)
		}
		if from.In(loc).Hour() != 0 || to.In(loc).Hour() != 0 {
			t.Errorf("window bounds must be IST midnights: %s %s", from.In(loc), to.In(loc))
		}
		today := biztime.BusinessDayStart(now)
		if today.Before(from) || !today.Before(to) {
			t.Errorf("window [%s, %s) does not contain today %s", from, to, today)
		}
		if to.Sub(from) > 60*24*time.Hour && tc.minDay == tc.maxDay {
			t.Errorf("window unexpectedly wide: %s", to.Sub(from))
		}
	}
}

// GetEventDetail must not run the canonical existence CTE a second time through History: the detail
// query itself already resolved the event under the caller's scope.
func TestGetEventDetailDoesNotReResolveEventThroughHistory(t *testing.T) {
	src := readRepositorySource(t)
	start := strings.Index(src, "func (r *Repository) GetEventDetail(")
	if start < 0 {
		t.Fatal("GetEventDetail not found")
	}
	body := src[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}
	if strings.Contains(body, "r.History(") {
		t.Fatal("GetEventDetail calls r.History, which re-runs eventExists (a second full canonical CTE); call historyItems instead")
	}
	if strings.Contains(body, "canonicalUnboundedWindow(") {
		t.Fatal("GetEventDetail must derive a narrow window from the event_id (canonicalEventWindow), not the +/-2 year window")
	}
}

func readRepositorySource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Drive targets must start from the identity-shaped candidate set (assignment members, batch index,
// matched park-drive batches, the unbatched business-day window) and reach obligation_instances by
// primary key. The previous plan started from a tenant-wide bitmap scan of obligation_instances and
// applied the assignment filter last; a 1-row member misestimate re-ran that scan once per member
// (stg: 692k heap blocks, 5.7 s for 42 targets).
func TestDriveTargetsSQLDrivesFromIdentityCandidates(t *testing.T) {
	sql := calendarDriveTargetsSQL
	cand := strings.Index(sql, "target_candidates AS MATERIALIZED (")
	if cand < 0 {
		t.Fatal("calendarDriveTargetsSQL has no target_candidates row source")
	}
	for _, branch := range []string{
		"FROM vaccination_drive_assignment_members m\n  WHERE $15::uuid IS NOT NULL",
		"WHERE $2::uuid IS NOT NULL\n    AND oi.tenant_id = $1::uuid\n    AND oi.batch_id = $2::uuid",
		"FROM matched_batches mb\n  JOIN obligation_instances oi",
		"AND oi.due_at >= ($3::date)::timestamp AT TIME ZONE 'Asia/Kolkata'",
	} {
		if !strings.Contains(sql[cand:], branch) {
			t.Errorf("target_candidates is missing the branch %q", branch)
		}
	}
	if !strings.Contains(sql, "FROM target_candidates tc\nJOIN obligation_instances oi\n  ON oi.tenant_id = $1::uuid\n AND oi.obligation_id = tc.obligation_id") {
		t.Fatal("matched_obligations must reach obligation_instances by primary key from target_candidates")
	}
}

// The week view's list, date markers, reminder rail and filter options are independent reads; they
// must not be issued one after another (stg: ~750 ms summed).
func TestListEventsRunsSectionsConcurrently(t *testing.T) {
	src := readRepositorySource(t)
	start := strings.Index(src, "func (r *Repository) ListEvents(")
	body := src[start:]
	body = body[:strings.Index(body[1:], "\nfunc ")+1]
	if !strings.Contains(body, "errgroup.WithContext(") {
		t.Fatal("ListEvents must run its sections concurrently")
	}
	for _, call := range []string{"r.listEventsCanonical(gctx", "r.dateMarkers(gctx", "r.reminderRail(gctx", "r.listFilterOptions(gctx"} {
		if !strings.Contains(body, call) {
			t.Errorf("ListEvents section %q is not run on the group context", call)
		}
	}
}
