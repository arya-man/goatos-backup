package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func getTemporaryTagged(t *testing.T, mux *http.ServeMux, rawQuery string) *httptest.ResponseRecorder {
	return getTemporaryTaggedWithGrants(t, mux, rawQuery, nil)
}

func getTemporaryTaggedWithGrants(
	t *testing.T,
	mux *http.ServeMux,
	rawQuery string,
	grants []permissions.ActiveGrant,
) *httptest.ResponseRecorder {
	t.Helper()
	path := "/app/counts/goats/temporary-tagged"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
	if grants != nil {
		ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestListTemporaryTaggedGoatsReturnsItems(t *testing.T) {
	next := "G-000200"
	validator := newFakeGoatValidator()
	validator.tempItems = []identitydomain.TemporaryTaggedGoat{
		{GoatID: "11111111-1111-4111-8111-111111111111", DisplayID: "G-000101", TemporaryIdentifier: "T-77", LocationDisplay: "Shed A", RowVersion: 3},
		{GoatID: "22222222-2222-4222-8222-222222222222", DisplayID: "G-000200", TemporaryIdentifier: "T-81", LocationDisplay: "Shed B", RowVersion: 1},
	}
	validator.tempNext = &next

	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), newFakeApprovalWorkflow(), validator)

	rec := getTemporaryTagged(t, mux, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body appTemporaryTaggedGoatsResponse
	decodeBody(t, rec, &body)
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(body.Items))
	}
	if body.Items[0].TemporaryIdentifier != "T-77" || body.Items[0].RowVersion != 3 {
		t.Fatalf("unexpected first item: %+v", body.Items[0])
	}
	if body.NextCursor == nil || *body.NextCursor != next {
		t.Fatalf("expected next_cursor %q, got %v", next, body.NextCursor)
	}
	// Default page size is capped server-side at one phone screen.
	if validator.lastTempList.Limit != appTemporaryTaggedPageSize {
		t.Fatalf("expected default limit %d, got %d", appTemporaryTaggedPageSize, validator.lastTempList.Limit)
	}
}

func TestListTemporaryTaggedGoatsCapsPageSize(t *testing.T) {
	validator := newFakeGoatValidator()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), newFakeApprovalWorkflow(), validator)

	// A client asking for 500 rows must still get at most one phone screen.
	if rec := getTemporaryTagged(t, mux, "page_size=500"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if validator.lastTempList.Limit != appTemporaryTaggedPageSize {
		t.Fatalf("expected capped limit %d, got %d", appTemporaryTaggedPageSize, validator.lastTempList.Limit)
	}

	// A smaller explicit page size is honoured; the cursor is threaded through.
	if rec := getTemporaryTagged(t, mux, "page_size=5&cursor=G-000101"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if validator.lastTempList.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", validator.lastTempList.Limit)
	}
	if validator.lastTempList.Cursor == nil || *validator.lastTempList.Cursor != "G-000101" {
		t.Fatalf("expected cursor G-000101, got %v", validator.lastTempList.Cursor)
	}
}

func TestListTemporaryTaggedGoatsThreadsLocationFilter(t *testing.T) {
	validator := newFakeGoatValidator()
	park := "20000000-0000-4000-8000-000000000002"
	shed := "30000000-0000-4000-8000-000000000003"
	repo := newFakeShiftingRepo()
	repo.destinations = domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{
		{ParkID: park, Sheds: []domain.ShiftingDestinationShed{{ShedID: shed}}},
	}}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), validator)

	if rec := getTemporaryTagged(t, mux, "park_id="+park+"&shed_id="+shed); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if validator.lastTempList.ParkID != park {
		t.Fatalf("expected park_id %q, got %q", park, validator.lastTempList.ParkID)
	}
	if validator.lastTempList.ShedID != shed {
		t.Fatalf("expected shed_id %q, got %q", shed, validator.lastTempList.ShedID)
	}
}

func TestListTemporaryTaggedGoatsRejectsBadPageSize(t *testing.T) {
	validator := newFakeGoatValidator()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), newFakeApprovalWorkflow(), validator)
	if rec := getTemporaryTagged(t, mux, "page_size=0"); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for page_size=0, got %d", rec.Code)
	}
	if rec := getTemporaryTagged(t, mux, "page_size=abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric page_size, got %d", rec.Code)
	}
}

func TestListTemporaryTaggedGoatsEnforcesCountsWriteParkAndShedScope(t *testing.T) {
	const (
		parkA = "20000000-0000-4000-8000-00000000000a"
		parkB = "20000000-0000-4000-8000-00000000000b"
		shedA = "30000000-0000-4000-8000-00000000000a"
		shedB = "30000000-0000-4000-8000-00000000000b"
	)

	newServer := func() (*http.ServeMux, *fakeGoatValidator) {
		repo := newFakeShiftingRepo()
		repo.destinations = domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{
			{ParkID: parkA, Sheds: []domain.ShiftingDestinationShed{{ShedID: shedA}}},
			{ParkID: parkB, Sheds: []domain.ShiftingDestinationShed{{ShedID: shedB}}},
		}}
		validator := newFakeGoatValidator()
		return newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), validator), validator
	}

	parkAGrant := permissions.ActiveGrant{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}

	t.Run("omitted park defaults to the operator's authorized park", func(t *testing.T) {
		mux, validator := newServer()
		rec := getTemporaryTaggedWithGrants(t, mux, "", []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if validator.lastTempList.ParkID != parkA {
			t.Fatalf("park_id=%q, want authorized park %q", validator.lastTempList.ParkID, parkA)
		}
	})

	t.Run("foreign park is forbidden", func(t *testing.T) {
		mux, _ := newServer()
		rec := getTemporaryTaggedWithGrants(t, mux, "park_id="+parkB, []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("shed inside the authorized park is allowed", func(t *testing.T) {
		mux, validator := newServer()
		rec := getTemporaryTaggedWithGrants(t, mux, "park_id="+parkA+"&shed_id="+shedA, []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if validator.lastTempList.ParkID != parkA || validator.lastTempList.ShedID != shedA {
			t.Fatalf("location filter=(%q, %q), want (%q, %q)", validator.lastTempList.ParkID, validator.lastTempList.ShedID, parkA, shedA)
		}
	})

	t.Run("shed from another park is forbidden", func(t *testing.T) {
		mux, _ := newServer()
		rec := getTemporaryTaggedWithGrants(t, mux, "park_id="+parkA+"&shed_id="+shedB, []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unrelated tenant-wide grant does not bypass counts scope", func(t *testing.T) {
		mux, validator := newServer()
		grants := []permissions.ActiveGrant{
			parkAGrant,
			{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: testTenantID},
		}
		rec := getTemporaryTaggedWithGrants(t, mux, "", grants)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if validator.lastTempList.ParkID != parkA {
			t.Fatalf("park_id=%q, want counts-authorized park %q", validator.lastTempList.ParkID, parkA)
		}
	})

	t.Run("tenant-wide counts writer may query any park", func(t *testing.T) {
		mux, validator := newServer()
		grants := []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenantID}}
		rec := getTemporaryTaggedWithGrants(t, mux, "park_id="+parkB+"&shed_id="+shedB, grants)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if validator.lastTempList.ParkID != parkB || validator.lastTempList.ShedID != shedB {
			t.Fatalf("location filter=(%q, %q), want (%q, %q)", validator.lastTempList.ParkID, validator.lastTempList.ShedID, parkB, shedB)
		}
	})
}
