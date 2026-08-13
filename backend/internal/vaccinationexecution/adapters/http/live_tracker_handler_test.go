package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

const (
	ltTenant    = "00000000-0000-4000-8000-000000000001"
	ltOwnPark   = "30000000-0000-4000-8000-000000000001"
	ltOtherPark = "30000000-0000-4000-8000-000000000099"
	ltUUID      = "40000000-0000-4000-8000-000000000001"
)

func liveTrackerRequest(t *testing.T, reader *fakeReader, query string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))
	ctx := httpmiddleware.WithTenantID(t.Context(), ltTenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: ltOwnPark},
	})
	req := httptest.NewRequest(http.MethodGet, "/vaccination/live-tracker"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A park-bound actor must not be able to read the other park's drive by simply omitting park_id,
// and naming a park they do not hold must be refused rather than silently widened.
func TestLiveTrackerClampsParkScopeToTheActorsGrants(t *testing.T) {
	reader := &fakeReader{}
	if rec := liveTrackerRequest(t, reader, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastLiveTracker.ParkID == nil || *reader.lastLiveTracker.ParkID != ltOwnPark {
		t.Fatalf("omitted park_id resolved to %v, want %s", reader.lastLiveTracker.ParkID, ltOwnPark)
	}

	rec := liveTrackerRequest(t, reader, "?park_id="+ltOtherPark)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("explicit other-park status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
		t.Fatalf("body=%s, want park_scope_forbidden", rec.Body.String())
	}
}

// business_date is a BUSINESS DATE, not a projection as_of. A past drive day is a legitimate read
// and must NOT be refused with historical_as_of_unsupported the way the current-view execution reads
// are — but it still has to stay inside a bounded window.
func TestLiveTrackerAcceptsPastDriveDaysWithinTheWindow(t *testing.T) {
	loc := biztime.DefaultLocation()
	now := time.Now().In(loc)
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	reader := &fakeReader{}
	rec := liveTrackerRequest(t, reader, "?business_date="+yesterday)
	if rec.Code != http.StatusOK {
		t.Fatalf("yesterday status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "historical_as_of_unsupported") {
		t.Fatal("a past business date must not be reported as an unsupported historical as_of")
	}
	if got := reader.lastLiveTracker.BusinessDate.Format("2006-01-02"); got != yesterday {
		t.Fatalf("business_date reached the reader as %s, want %s", got, yesterday)
	}

	tooOld := now.AddDate(0, 0, -(vaccexecd.LiveTrackerBusinessDateLookbackDays + 1)).Format("2006-01-02")
	rec = liveTrackerRequest(t, reader, "?business_date="+tooOld)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-window date status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_business_date") {
		t.Fatalf("body=%s, want invalid_business_date", rec.Body.String())
	}

	future := now.AddDate(0, 0, 1).Format("2006-01-02")
	if rec := liveTrackerRequest(t, reader, "?business_date="+future); rec.Code != http.StatusBadRequest {
		t.Fatalf("future date status = %d, want 400", rec.Code)
	}
}

func TestLiveTrackerDefaultsToTodayInBusinessTime(t *testing.T) {
	reader := &fakeReader{}
	if rec := liveTrackerRequest(t, reader, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want := time.Now().In(biztime.DefaultLocation()).Format("2006-01-02")
	if got := reader.lastLiveTracker.BusinessDate.Format("2006-01-02"); got != want {
		t.Fatalf("default business date = %s, want today in business time (%s)", got, want)
	}
}

// Every rejected input must name WHICH parameter was wrong. A generic 400 on a page with five
// filters leaves the caller guessing which one it was.
func TestLiveTrackerRejectsMalformedFiltersWithSpecificCodes(t *testing.T) {
	reader := &fakeReader{}
	cases := map[string]string{
		"?business_date=12-08-2026": "invalid_business_date",
		"?park_id=not-a-uuid":       "invalid_park_id",
		"?shed_id=not-a-uuid":       "invalid_shed_id",
		"?operator_id=not-a-uuid":   "invalid_operator_id",
		"?status=sideways":          "invalid_status",
		"?activity_limit=0":         "invalid_activity_limit",
		"?activity_limit=abc":       "invalid_activity_limit",
		"?activity_before=today":    "invalid_activity_before",
	}
	for query, code := range cases {
		rec := liveTrackerRequest(t, reader, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", query, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s body=%s, want %s", query, rec.Body.String(), code)
		}
	}
}

func TestLiveTrackerRejectsOverlongTextFilters(t *testing.T) {
	reader := &fakeReader{}
	long := strings.Repeat("x", 65)
	for query, code := range map[string]string{
		"?partition_label=" + long: "invalid_partition_label",
		"?vaccine_code=" + long:    "invalid_vaccine_code",
	} {
		rec := liveTrackerRequest(t, reader, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", query, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s body=%s, want %s", query, rec.Body.String(), code)
		}
	}
}

// TestLiveTrackerRefusesActivityLimitOutsideItsDeclaredBounds pins that BOTH declared bounds behave
// the same way. The contract declares minimum 1 / maximum 100; the handler used to reject anything
// below the minimum with 400 and silently clamp anything above the maximum to 100 with HTTP 200 —
// telling a paging caller that the page size it asked for was honoured when it was not.
func TestLiveTrackerRefusesActivityLimitOutsideItsDeclaredBounds(t *testing.T) {
	for _, query := range []string{"?activity_limit=0", "?activity_limit=101", "?activity_limit=100000"} {
		rec := liveTrackerRequest(t, &fakeReader{}, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400 (the declared maximum must be enforced, not clamped)", query, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "invalid_activity_limit") {
			t.Errorf("%s body = %s, want invalid_activity_limit", query, rec.Body.String())
		}
	}
	// The ceiling itself stays accepted.
	reader := &fakeReader{}
	if rec := liveTrackerRequest(t, reader, "?activity_limit=100"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastLiveTracker.ActivityLimit != vaccexecd.LiveTrackerMaxActivity {
		t.Fatalf("activity_limit reached the reader as %d, want %d",
			reader.lastLiveTracker.ActivityLimit, vaccexecd.LiveTrackerMaxActivity)
	}
}

func TestLiveTrackerPassesEveryFilterAxisThrough(t *testing.T) {
	reader := &fakeReader{}
	query := "?shed_id=" + ltUUID +
		"&partition_label=Part%203" +
		"&operator_id=" + ltUUID +
		"&vaccine_code=goat_pox" +
		"&status=review" +
		"&activity_before=2026-08-12T17%3A40%3A00%2B05%3A30"
	if rec := liveTrackerRequest(t, reader, query); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	q := reader.lastLiveTracker
	if q.ShedID == nil || *q.ShedID != ltUUID {
		t.Errorf("shed_id = %v", q.ShedID)
	}
	if q.PartitionLabel == nil || *q.PartitionLabel != "Part 3" {
		t.Errorf("partition_label = %v", q.PartitionLabel)
	}
	if q.OperatorID == nil || *q.OperatorID != ltUUID {
		t.Errorf("operator_id = %v", q.OperatorID)
	}
	if q.VaccineCode == nil || *q.VaccineCode != "goat_pox" {
		t.Errorf("vaccine_code = %v", q.VaccineCode)
	}
	if q.Status == nil || *q.Status != vaccexecd.LiveTrackerStatusReview {
		t.Errorf("status = %v", q.Status)
	}
	if q.ActivityBefore == nil {
		t.Error("activity_before cursor was dropped; the feed would restart from the newest event on every page")
	}
}

func TestLiveTrackerRouteCarriesTheSamePermissionsAsItsCommandBoardSibling(t *testing.T) {
	routes := permissions.ProtectedRoutes()
	var tracker, command *permissions.Route
	for i := range routes {
		switch routes[i].Pattern {
		case "/vaccination/live-tracker":
			tracker = &routes[i]
		case "/vaccination/command":
			command = &routes[i]
		}
	}
	if tracker == nil {
		t.Fatal("GET /vaccination/live-tracker has no permission entry; an unregistered route is unguarded")
	}
	if command == nil {
		t.Fatal("GET /vaccination/command permission entry disappeared")
	}
	if strings.Join(tracker.Permissions, ",") != strings.Join(command.Permissions, ",") {
		t.Fatalf("live tracker permissions %v differ from the command board's %v; both read the same vaccination execution data",
			tracker.Permissions, command.Permissions)
	}
}
