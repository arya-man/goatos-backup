package http

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

// leadershipVideosRepo is a minimal in-memory ports.Repository fixture reproducing the exact
// live-data shape from the reported defect: 2 approved+CLOSED, 2 approved+open, 1 rejected+open.
// Real HTTP requests are sent through the actual Register()'d mux below, so this is the closest
// thing to the maintainer's own curl reproduction that a live Postgres-free test can offer.
type leadershipVideosRepo struct {
	ports.Repository
	items []domain.Item
}

func newLeadershipVideosRepo(tenantID string) *leadershipVideosRepo {
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	closedAt := now.Add(time.Hour)
	item := func(id, status string, closed bool) domain.Item {
		it := domain.Item{
			ItemID: id, TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination",
			Category: "vaccination_proof", Status: status, CapturedAt: now, RowVersion: 1,
			Source: domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: id},
		}
		if closed {
			ts := closedAt
			it.ClosedAt = &ts
		}
		return it
	}
	return &leadershipVideosRepo{items: []domain.Item{
		item("approved-closed-1", domain.StatusApproved, true),
		item("approved-closed-2", domain.StatusApproved, true),
		item("approved-open-1", domain.StatusApproved, false),
		item("approved-open-2", domain.StatusApproved, false),
		item("rejected-open-1", domain.StatusRejected, false),
	}}
}

func (r *leadershipVideosRepo) ListQueue(_ context.Context, params ports.ListQueueParams) ([]domain.Item, error) {
	out := make([]domain.Item, 0, len(r.items))
	for _, it := range r.items {
		if params.Category != "" && it.Category != params.Category {
			continue
		}
		if params.Status != "" && it.Status != params.Status {
			continue
		}
		if params.OpenOnly && it.ClosedAt != nil {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (r *leadershipVideosRepo) ListQueueFilterOptions(context.Context, ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	return domain.QueueFilterOptions{}, nil
}

func (r *leadershipVideosRepo) ListReadyVaccinationBatchClosures(context.Context, ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	return nil, nil
}

// TestLeadershipVideosHTTPReproducesReportedGapAndFix is the HTTP-layer twin of
// TestLeadershipReviewIncludesClosedItemsWhileActionQueueExcludesThem (verification/app):
// same fixture, but through the REAL registered mux and REAL query-string parsing, so it
// proves the exact two curl calls from the bug report:
//
//	GET /verification/action-queue?category=vaccination_proof         -> 3 items (bug: 2 closed approved missing)
//	GET /verification/queue?category=vaccination_proof&status=all     -> 5 items (leadership review: nothing missing)
func TestLeadershipVideosHTTPReproducesReportedGapAndFix(t *testing.T) {
	const (
		tenantID = "10000000-0000-4000-8000-000000000001"
		actorID  = "20000000-0000-4000-8000-000000000009"
	)
	repo := newLeadershipVideosRepo(tenantID)
	service := verificationapp.NewService(repo, nil)
	if err := service.RegisterCategory(domain.CategoryDefinition{
		Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination", PageKey: "vaccination", PageLabel: "Vaccination",
	}); err != nil {
		t.Fatalf("register category: %v", err)
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func(path string, role string) (int, []byte) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: role, ScopeType: "tenant", ScopeID: tenantID}})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	itemIDs := func(body []byte) []string {
		var parsed struct {
			Items []struct {
				ItemID string `json:"item_id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, body)
		}
		ids := make([]string, 0, len(parsed.Items))
		for _, it := range parsed.Items {
			ids = append(ids, it.ItemID)
		}
		return ids
	}

	// 1) The verifier/leadership ACTION queue: exactly the reported symptom -- 3 items
	//    (2 approved-open + 1 rejected-open), the 2 closed approved items MUST stay excluded.
	code, body := get("/verification/action-queue?category=vaccination_proof&limit=20", permissions.RoleCEOInternal)
	if code != http.StatusOK {
		t.Fatalf("action-queue status=%d body=%s", code, body)
	}
	actionIDs := itemIDs(body)
	if len(actionIDs) != 3 {
		t.Fatalf("action-queue items=%v, want 3 (matches the reported bug's own shape)", actionIDs)
	}
	for _, id := range actionIDs {
		if id == "approved-closed-1" || id == "approved-closed-2" {
			t.Fatalf("action queue must never include a closed item, got %s", id)
		}
	}

	// 2) The FIX: leadership's Videos nav item now lands on the REVIEW queue
	//    (verifyQueueHref -> GET /verification/queue), where "All" (status=all) surfaces every
	//    status including closed work -- the complete audit trail the CEO asked for.
	code, body = get("/verification/queue?category=vaccination_proof&status=all&limit=20", permissions.RoleCEOInternal)
	if code != http.StatusOK {
		t.Fatalf("review queue status=%d body=%s", code, body)
	}
	reviewIDs := itemIDs(body)
	if len(reviewIDs) != 5 {
		t.Fatalf("review queue (status=all) items=%v, want all 5 -- the closed approved items must be reachable", reviewIDs)
	}
	seen := map[string]bool{}
	for _, id := range reviewIDs {
		seen[id] = true
	}
	for _, want := range []string{"approved-closed-1", "approved-closed-2", "approved-open-1", "approved-open-2", "rejected-open-1"} {
		if !seen[want] {
			t.Fatalf("review queue missing %s; got %v", want, reviewIDs)
		}
	}
}
