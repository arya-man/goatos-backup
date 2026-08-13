package http

// Leadership must be able to filter the Actions queue by module, exactly like a verifier.
//
// The Actions screen is scoped entirely by the left nav: every leaf links to
// /actions?category=<category>. For a real verifier that worked -- resolveVerifierCategories
// validates her requested category against her verify duties and hands back []string{category}.
// For CEO/CxO it returned a NIL slice, meaning "no AUTHORIZATION narrowing is needed", and the
// caller then cleared `category` unconditionally. The requested filter was destroyed: clicking
// Vaccination in the sidebar returned every module's items, and the status pill counted the whole
// tenant (52) instead of the module (19 vaccination / 33 weighing on the reported data).
//
// This asserts the queue honours an explicitly requested category for an unrestricted role, and
// that omitting the category still returns everything that role may see.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// twoCategoryRepo holds items in two different modules, which is the whole point: a single-module
// fixture cannot tell "filtered correctly" apart from "filter ignored".
type twoCategoryRepo struct {
	ports.Repository
	items []domain.Item
}

func newTwoCategoryRepo(tenantID string) *twoCategoryRepo {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	item := func(id, vertical, module, category string) domain.Item {
		return domain.Item{
			ItemID: id, TenantID: tenantID, Vertical: vertical, Module: module,
			Category: category, Status: domain.StatusPending, CapturedAt: now, RowVersion: 1,
			Source: domain.SourceRef{Module: module, RefType: "proof", RefID: id},
		}
	}
	return &twoCategoryRepo{items: []domain.Item{
		item("vacc-1", "preventive_care", "vaccination", "vaccination_proof"),
		item("vacc-2", "preventive_care", "vaccination", "vaccination_proof"),
		item("weigh-1", "weighing", "weighing", "weighing_proof"),
	}}
}

func (r *twoCategoryRepo) ListQueue(_ context.Context, params ports.ListQueueParams) ([]domain.Item, error) {
	out := make([]domain.Item, 0, len(r.items))
	for _, it := range r.items {
		if params.Category != "" && it.Category != params.Category {
			continue
		}
		if len(params.Categories) > 0 {
			allowed := false
			for _, c := range params.Categories {
				if c == it.Category {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		if params.Status != "" && it.Status != params.Status {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (r *twoCategoryRepo) ListQueueFilterOptions(context.Context, ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	return domain.QueueFilterOptions{}, nil
}

func (r *twoCategoryRepo) ListReadyVaccinationBatchClosures(context.Context, ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	return nil, nil
}

func TestLeadershipQueueHonoursRequestedCategory(t *testing.T) {
	const (
		tenantID = "10000000-0000-4000-8000-000000000001"
		actorID  = "20000000-0000-4000-8000-000000000009"
	)
	service := verificationapp.NewService(newTwoCategoryRepo(tenantID), nil)
	for _, def := range []domain.CategoryDefinition{
		{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination", PageKey: "vaccination", PageLabel: "Vaccination"},
		{Vertical: "weighing", Module: "weighing", Category: "weighing_proof",
			NavigationModule: "weighing", NavigationModuleLabel: "Weighing", PageKey: "weighing", PageLabel: "Weighing"},
	} {
		if err := service.RegisterCategory(def); err != nil {
			t.Fatalf("register category %s: %v", def.Category, err)
		}
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service))

	get := func(path string) []string {
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
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var parsed struct {
			Items []struct {
				ItemID   string `json:"item_id"`
				Category string `json:"category"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		ids := make([]string, 0, len(parsed.Items))
		for _, it := range parsed.Items {
			ids = append(ids, it.ItemID+"("+it.Category+")")
		}
		return ids
	}

	if got := get("/verification/queue?category=vaccination_proof&status=pending&limit=20"); len(got) != 2 {
		t.Fatalf("vaccination queue = %v, want only the 2 vaccination items -- the sidebar's category must scope the queue", got)
	}
	if got := get("/verification/queue?category=weighing_proof&status=pending&limit=20"); len(got) != 1 {
		t.Fatalf("weighing queue = %v, want only the 1 weighing item", got)
	}
	// No category = the whole authorized queue, which is what an unrestricted role sees when it
	// lands on Actions without picking a module.
	if got := get("/verification/queue?status=pending&limit=20"); len(got) != 3 {
		t.Fatalf("unfiltered queue = %v, want all 3 items for an unrestricted role", got)
	}
}
