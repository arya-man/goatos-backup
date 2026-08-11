package http

// The Actions screen's MODULE filter (maintainer request 2026-08-11): the same Vaccination /
// Weighing / Feed / Counts grouping the phone's verifier drawer uses, sent as ?nav_module=.
//
// A module is not an action type. Feed registers three categories (distribution, packing,
// transport), so a per-category filter cannot express "show me Feed" and a fixture with one category
// per module cannot tell a correct expansion apart from a coincidence. This uses a two-category Feed
// module beside a one-category Vaccination module, and asserts on the ITEMS the route returns —
// the wire result, not the params handed to a fake.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

func newMultiCategoryModuleRepo(tenantID string) *twoCategoryRepo {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	item := func(id, vertical, module, category string) domain.Item {
		return domain.Item{
			ItemID: id, TenantID: tenantID, Vertical: vertical, Module: module,
			Category: category, Status: domain.StatusPending, CapturedAt: now, RowVersion: 1,
			Source: domain.SourceRef{Module: module, RefType: "proof", RefID: id},
		}
	}
	return &twoCategoryRepo{items: []domain.Item{
		item("vacc-1", "preventive_care", "vaccination", "vaccination_proof"),
		item("pack-1", "feed", "feed", "feed_packing"),
		item("dist-1", "feed", "feed", "feed_distribution"),
	}}
}

func registerFeedAndVaccination(t *testing.T, service *verificationapp.Service) {
	t.Helper()
	for _, def := range []domain.CategoryDefinition{
		{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination", PageKey: "vaccination", PageLabel: "Vaccination", PageOrder: 1},
		{Vertical: "feed", Module: "feed", Category: "feed_distribution",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed", PageKey: "feed_distribution", PageLabel: "Feed Distribution", PageOrder: 1},
		{Vertical: "feed", Module: "feed", Category: "feed_packing",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed", PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2},
	} {
		if err := service.RegisterCategory(def); err != nil {
			t.Fatalf("register category %s: %v", def.Category, err)
		}
	}
}

func TestQueueNavModuleFilterReturnsEveryCategoryOfThatModule(t *testing.T) {
	const (
		tenantID = "10000000-0000-4000-8000-000000000001"
		actorID  = "20000000-0000-4000-8000-000000000009"
	)
	service := verificationapp.NewService(newMultiCategoryModuleRepo(tenantID), nil)
	registerFeedAndVaccination(t, service)
	mux := http.NewServeMux()
	Register(mux, NewHandler(service))

	get := func(path string) (int, []string, []domain.QueueModuleOption) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenantID},
		})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req.WithContext(ctx))
		if rec.Code != http.StatusOK {
			return rec.Code, nil, nil
		}
		var parsed struct {
			Items []struct {
				ItemID string `json:"item_id"`
			} `json:"items"`
			FilterOptions struct {
				Modules   []domain.QueueModuleOption `json:"modules"`
				ModuleKey string                     `json:"module_key"`
			} `json:"filter_options"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		ids := make([]string, 0, len(parsed.Items))
		for _, it := range parsed.Items {
			ids = append(ids, it.ItemID)
		}
		return rec.Code, ids, parsed.FilterOptions.Modules
	}

	code, ids, modules := get("/verification/queue?nav_module=feed_direction&status=pending&limit=20")
	if code != http.StatusOK {
		t.Fatalf("feed module queue status=%d", code)
	}
	if len(ids) != 2 {
		t.Fatalf("feed module queue = %v, want both feed categories (packing + distribution) and no vaccination item", ids)
	}
	for _, id := range ids {
		if id == "vacc-1" {
			t.Fatalf("feed module queue leaked a vaccination item: %v", ids)
		}
	}
	// The chip vocabulary is one entry per MODULE, not one per category — Feed's two pages must not
	// produce two Feed chips.
	if len(modules) != 2 {
		t.Fatalf("modules = %+v, want one entry each for Feed and Vaccination", modules)
	}

	if code, ids, _ := get("/verification/queue?nav_module=vaccination&status=pending&limit=20"); code != http.StatusOK || len(ids) != 1 || ids[0] != "vacc-1" {
		t.Fatalf("vaccination module queue status=%d items=%v, want only vacc-1", code, ids)
	}

	// An unregistered key is a bad request. Serving an empty queue instead would read to the
	// operator as "nothing to verify in this module", which is a different and wrong statement.
	if code, _, _ := get("/verification/queue?nav_module=not_a_module&status=pending&limit=20"); code != http.StatusBadRequest {
		t.Fatalf("unknown module status=%d, want 400", code)
	}

	// A page selection from a DIFFERENT module contradicts the module chip; refusing beats
	// silently picking one of the two.
	if code, _, _ := get("/verification/queue?nav_module=feed_direction&category=vaccination_proof&status=pending&limit=20"); code != http.StatusBadRequest {
		t.Fatalf("conflicting category+module status=%d, want 400", code)
	}
}

