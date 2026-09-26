package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
)

func projectionFixture() domain.BootstrapResponse {
	page := func(id string) domain.PageContract {
		return domain.PageContract{
			RouteID: id, Href: "/" + id, PathPattern: "/" + id, Title: id, Subtitle: "sub", SurfaceKind: "list",
			SourceScope:     []string{"tenant"},
			Sections:        []domain.Section{{ID: "s", Title: "S", Kind: "k", ChipKeys: []string{}}},
			Tables:          []domain.TableContract{{ID: "t"}},
			Drawers:         []domain.DrawerContract{{ID: "d"}},
			Controls:        []domain.Control{{ID: "c"}},
			Copy:            map[string]string{"k": "v"},
			OptionGroups:    []domain.OptionGroup{{ID: "g"}},
			MigrationStatus: "live",
			ValidationNotes: []string{"n"},
		}
	}
	return domain.BootstrapResponse{
		CachePolicy: domain.ContractCachePolicy{ETag: `W/"rev-1"`},
		Pages:       []domain.PageContract{page("a"), page("b")},
	}
}

// The three views carry the SAME page field set; only the heavy contents differ, and the ETag
// names the projection so a 304 can never answer one view with another.
func TestBootstrapPageProjections(t *testing.T) {
	service := &fakeBootstrapService{resp: projectionFixture()}
	handler := NewHandler(service)
	read := func(target string) (domain.BootstrapResponse, string, map[string]any) {
		rec := httptest.NewRecorder()
		handler.Bootstrap(rec, httptest.NewRequest("GET", target, nil))
		var resp domain.BootstrapResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		return resp, rec.Header().Get("ETag"), raw
	}
	full, fullTag, _ := read("/admin-web/bootstrap")
	single, singleTag, singleRaw := read("/admin-web/bootstrap?page=b")
	summary, summaryTag, _ := read("/admin-web/bootstrap?pages=summary")
	both, bothTag, _ := read("/admin-web/bootstrap?pages=summary&page=a")

	if fullTag != `W/"rev-1"` || singleTag != `W/"rev-1;page=b"` || summaryTag != `W/"rev-1;pages=summary"` || bothTag != `W/"rev-1;page=a"` {
		t.Fatalf("etags = %q %q %q %q", fullTag, singleTag, summaryTag, bothTag)
	}
	if len(full.Pages[0].Copy) != 1 || len(full.Pages[1].Copy) != 1 {
		t.Fatalf("full view must carry every page's copy: %+v", full.Pages)
	}
	if len(single.Pages) != 2 || single.Pages[1].RouteID != "b" || len(single.Pages[1].Copy) != 1 || len(single.Pages[1].Tables) != 1 {
		t.Fatalf("page=b must carry b in full: %+v", single.Pages)
	}
	if len(single.Pages[0].Copy) != 0 || len(single.Pages[0].Tables) != 0 || len(single.Pages[0].OptionGroups) != 0 || single.Pages[0].Href != "/a" || single.Pages[0].Title != "a" {
		t.Fatalf("page=b must summarise a (identity kept, heavy fields emptied): %+v", single.Pages[0])
	}
	if len(summary.Pages) != 2 || len(summary.Pages[0].Copy) != 0 || len(summary.Pages[1].Copy) != 0 {
		t.Fatalf("pages=summary must summarise every page: %+v", summary.Pages)
	}
	if len(both.Pages[0].Copy) != 1 || len(both.Pages[1].Copy) != 0 {
		t.Fatalf("page wins over pages=summary: %+v", both.Pages)
	}
	// Field-set parity: a summarised page serialises with exactly the keys a full page has, and the
	// withheld fields are EMPTY arrays/objects, never null (the generated client types are non-null).
	pages := singleRaw["pages"].([]any)
	fullKeys := pages[1].(map[string]any)
	lightKeys := pages[0].(map[string]any)
	if len(fullKeys) != len(lightKeys) {
		t.Fatalf("summarised page has %d keys, full page %d", len(lightKeys), len(fullKeys))
	}
	for k := range fullKeys {
		if _, ok := lightKeys[k]; !ok {
			t.Fatalf("summarised page lost key %q", k)
		}
		if lightKeys[k] == nil {
			t.Fatalf("summarised page key %q is null", k)
		}
	}
	// The service's cached value must not have been mutated by projecting it.
	if len(service.resp.Pages[0].Copy) != 1 || len(service.resp.Pages[1].Copy) != 1 {
		t.Fatalf("projection mutated the cached response: %+v", service.resp.Pages)
	}
	// A 304 for the projected entity only matches the projected ETag.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin-web/bootstrap?page=b", nil)
	req.Header.Set("If-None-Match", `W/"rev-1"`)
	handler.Bootstrap(rec, req)
	if rec.Code != 200 {
		t.Fatalf("full-entity ETag must not 304 the page view: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req.Header.Set("If-None-Match", `W/"rev-1;page=b"`)
	handler.Bootstrap(rec, req)
	if rec.Code != 304 {
		t.Fatalf("projected ETag must 304 the page view: %d", rec.Code)
	}
}
