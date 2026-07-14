package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexehttp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/http"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryAD_LiveAsOfGuard proves a stale or hand-edited future URL as_of cannot make the
// live shed execution read model time-travel and mark future due work overdue.
func TestKernelStoryAD_LiveAsOfGuard(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ad", "Live vaccination reads clamp future as_of to backend now",
		"A vaccination obligation is open on the July 11 live business day and due on July 18. "+
			"If a browser URL carries as_of=2026-09-01, the backend must not evaluate the live table as "+
			"September and call the July 18 dose overdue. Historical as_of remains allowed, but future "+
			"as_of is clamped to the server's Asia/Kolkata now.")
	defer story.Finish()
	story.Certify("backend kernel + production vaccination execution HTTP handler")

	serverNow := time.Date(2026, 7, 11, 18, 15, 0, 0, biztime.DefaultLocation())
	dueAt := time.Date(2026, 7, 18, 0, 0, 0, 0, biztime.DefaultLocation())

	const (
		shedID  = "ad000000-0000-4000-8000-000000000001"
		stageID = "ad000000-0000-4000-8000-00000000000a"
		goatID  = "ad000000-0000-4000-8000-000000000010"
	)

	fx.SeedShed(shedID, "E2E-AD", stageID)
	dob := dueAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.PublishSimpleProtocol("vaccination.e2e.story_ad", 21, 14, nil)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, serverNow)

	svc := vaccexecapp.NewService(fx.VaccExec)
	handler := vaccexehttp.NewHandler(svc, nil).WithClock(func() time.Time { return serverNow })
	mux := http.NewServeMux()
	vaccexehttp.Register(mux, handler)

	// C35-002: GET /vaccination/sheds now reads the vaccination-shed projection exclusively. The
	// handler clamps any as_of (including the future "2026-09-01" URL param below) to serverNow via
	// biztime.ParseLiveAsOfRFC3339 BEFORE it reaches the repository, so both requests in this story
	// resolve to the identical effective instant (serverNow) -- canonical indexed projection serves both.

	story.Step("Future URL parameter is clamped by the backend",
		"Call the real /vaccination/sheds HTTP route with as_of=2026-09-01 while the backend clock is "+
			"2026-07-11 18:15 IST. The row must match live July semantics: due on July 18, not overdue.")

	row, ok := requestShedSummaryRow(t, mux, shedID, "2026-09-01T23:59:59Z")
	if !story.Assert("shed row is returned through the real HTTP route", ok, "found=%v", ok) {
		return
	}
	story.Assert("future as_of does not mark July 18 work overdue",
		row.Status == vaccexecdomain.ShedStatusScheduled,
		"status=%q next_due=%s due=%d done=%d", row.Status, detailString(row.NextDue), row.Due, row.Done)
	story.Assert("next due stays the July 18 business date",
		row.NextDue != nil && *row.NextDue == "2026-07-18",
		"next_due=%s", detailString(row.NextDue))

	story.Step("Explicit live as_of and future as_of produce the same live classification",
		"The same route with as_of set to the server's current July 11 instant should produce the same "+
			"status as the future URL, proving the future parameter was clamped instead of honored.")
	liveRow, ok := requestShedSummaryRow(t, mux, shedID, "2026-07-11T18:15:00+05:30")
	if !story.Assert("live as_of row is returned", ok, "found=%v", ok) {
		return
	}
	story.Assert("future URL and live URL agree on status",
		liveRow.Status == row.Status && liveRow.NextDue != nil && row.NextDue != nil && *liveRow.NextDue == *row.NextDue,
		"future_status=%q live_status=%q future_next=%s live_next=%s", row.Status, liveRow.Status, detailString(row.NextDue), detailString(liveRow.NextDue))
}

func requestShedSummaryRow(t *testing.T, mux *http.ServeMux, shedID, asOf string) (vaccexecdomain.ShedSummaryRow, bool) {
	t.Helper()
	q := url.Values{}
	q.Set("park_id", fxPark)
	q.Set("as_of", asOf)
	q.Set("limit", "10")
	req := httptest.NewRequest(http.MethodGet, "/vaccination/sheds?"+q.Encode(), nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), fxTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /vaccination/sheds status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp vaccexecdomain.ShedSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode shed summary: %v", err)
	}
	for _, row := range resp.Rows {
		if row.ShedID == shedID {
			return row, true
		}
	}
	return vaccexecdomain.ShedSummaryRow{}, false
}
