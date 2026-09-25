package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/app"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// listSpyService records the ListRequest the transport built and answers with an empty page,
// so the test can assert what the query string became without a database.
type listSpyService struct {
	Service
	got  app.ListRequest
	err  error
	page ports.Page
}

func (s *listSpyService) ListTasks(_ context.Context, req app.ListRequest) (ports.Page, error) {
	s.got = req
	return s.page, s.err
}

func (s *listSpyService) Now() time.Time { return time.Unix(0, 0).UTC() }

// listRequest builds a GET the permission middleware has already stamped.
func listRequest(query string) *http.Request {
	ctx := httpmiddleware.WithTenantID(context.Background(), "00000000-0000-4000-8000-00000000a001")
	ctx = httpmiddleware.WithActorID(ctx, "00000000-0000-4000-8000-00000000c001")
	ctx = httpmiddleware.WithPersonPermissions(ctx, []string{permissions.LeadershipTasksRead})
	return httptest.NewRequest(http.MethodGet, "/app/leadership-tasks?"+query, nil).WithContext(ctx)
}

// TestListTransportCarriesTheWorklistParamsAndRefusesABadSort closes the transport half of the
// worklist contract: the new query params must actually REACH the service (a param parsed into
// a variable nobody forwards reads as a working filter that silently does nothing), and a sort
// the enum does not carry must come back 400 `invalid_sort` rather than a silently reordered
// list.
func TestListTransportCarriesTheWorklistParamsAndRefusesABadSort(t *testing.T) {
	spy := &listSpyService{page: ports.Page{StatusCounts: map[string]int{}, ScopeCounts: map[string]int{}}}
	h := NewHandler(spy, nil)

	rec := httptest.NewRecorder()
	h.ListTasks(rec, listRequest("q=vendor+contract&assignee_user_id=00000000-0000-4000-8000-00000000c002"+
		"&raised_by=00000000-0000-4000-8000-00000000d001&deadline_from=2026-09-01&deadline_to=2026-09-30"+
		"&raised_from=2026-08-01&raised_to=2026-08-31&sort=deadline_asc&filter=open&limit=7&cursor=abc"+
		"&unknown_param=ignored"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	got := spy.got
	if got.Query != "vendor contract" || got.Sort != "deadline_asc" || got.Cursor != "abc" || got.Limit != 7 {
		t.Fatalf("text/sort/cursor/limit did not reach the service: %+v", got)
	}
	if got.AssigneeUserID != "00000000-0000-4000-8000-00000000c002" || got.RaisedBy != "00000000-0000-4000-8000-00000000d001" {
		t.Fatalf("person filters did not reach the service: %+v", got)
	}
	if got.DeadlineFrom != "2026-09-01" || got.DeadlineTo != "2026-09-30" || got.RaisedFrom != "2026-08-01" || got.RaisedTo != "2026-08-31" {
		t.Fatalf("date ranges did not reach the service: %+v", got)
	}
	if got.FilterKey != domain.FilterOpen {
		t.Fatalf("filter key = %q", got.FilterKey)
	}
	// An unknown query param stays silently ignored on this READ path: the filter bar and the
	// Android client evolve separately, and a stale extra param must not blank the list.

	// A sort outside the enum is a 400 with the contract's code, carried through from the app
	// layer's *app.Error rather than collapsing into a generic 500.
	spy.err = app.BadRequest("invalid_sort", "That sort order is not one of the ones on offer.")
	rec = httptest.NewRecorder()
	h.ListTasks(rec, listRequest("sort=title_asc"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad sort status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	if body.Error != "invalid_sort" {
		t.Fatalf("error code = %q, want invalid_sort", body.Error)
	}
	if spy.got.Sort != "title_asc" {
		t.Fatalf("the refused sort must still be the one the caller asked for: %q", spy.got.Sort)
	}
}

// TestListTransportCarriesTheOverdueFilterAndItsChipCount: `filter=overdue` reaches the service
// as its own key (never normalised to All), and the response's filters[] carries an "overdue"
// chip whose count is the page's OverdueCount -- the late subset -- selected when asked for.
func TestListTransportCarriesTheOverdueFilterAndItsChipCount(t *testing.T) {
	spy := &listSpyService{page: ports.Page{StatusCounts: map[string]int{"open": 4, "in_progress": 2}, ScopeCounts: map[string]int{}, OverdueCount: 3}}
	h := NewHandler(spy, nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, listRequest("scope=team_progress&filter=overdue"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	if spy.got.FilterKey != domain.FilterOverdue {
		t.Fatalf("filter key = %q, want overdue", spy.got.FilterKey)
	}
	var body struct {
		Filters []struct {
			Key      string `json:"key"`
			Label    string `json:"label"`
			Count    int    `json:"count"`
			Selected bool   `json:"selected"`
		} `json:"filters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	var found bool
	for _, f := range body.Filters {
		if f.Key != "overdue" {
			if f.Selected {
				t.Fatalf("chip %q selected under filter=overdue", f.Key)
			}
			continue
		}
		found = true
		if f.Label != "Overdue" || f.Count != 3 || !f.Selected {
			t.Fatalf("overdue chip = %+v, want label Overdue, count 3 (not open+in_progress = 6), selected", f)
		}
	}
	if !found {
		t.Fatalf("no overdue chip in filters: %+v", body.Filters)
	}
}

// compile-time proof the spy satisfies the transport's Service seam without the real service.
var _ Service = (*listSpyService)(nil)

// TestListTransportForwardsTheRawFilterKey pins that the transport hands the service the key the
// client SENT. Normalising it here first (FilterKeyOrDefault) turned filter=bogus into "all"
// before the service's unknown-key refusal could see it, so the refusal never fired (2026-09-25).
func TestListTransportForwardsTheRawFilterKey(t *testing.T) {
	spy := &listSpyService{page: ports.Page{StatusCounts: map[string]int{}, ScopeCounts: map[string]int{}}}
	h := NewHandler(spy, nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, listRequest("filter=bogus"))
	if spy.got.FilterKey != "bogus" {
		t.Fatalf("filter key reaching the service = %q, want the raw %q", spy.got.FilterKey, "bogus")
	}
}
