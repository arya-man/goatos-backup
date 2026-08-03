package http

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// dutyStubRepo answers the queue with one vaccination row and records whether it was
// reached at all. A refusal that still queried the repository would be a leak with a
// cosmetic status code on top, so "did the read happen" is the assertion that matters.
type dutyStubRepo struct{ listed bool }

func (r *dutyStubRepo) CreateItem(context.Context, domain.CreateItem) (domain.CreateItemResult, error) {
	return domain.CreateItemResult{}, nil
}
func (r *dutyStubRepo) GetItem(context.Context, string, string) (domain.Item, error) {
	return domain.Item{}, ports.ErrNotFound
}
func (r *dutyStubRepo) GetSubmissionItems(context.Context, string, string) ([]domain.Item, error) {
	return nil, nil
}
func (r *dutyStubRepo) ListQueue(context.Context, ports.ListQueueParams) ([]domain.Item, error) {
	r.listed = true
	return []domain.Item{{
		ItemID: "11111111-1111-4111-8111-111111111111", Vertical: "preventive_care",
		Module: "vaccination", Category: "vaccination_proof", Status: domain.StatusPending,
	}}, nil
}
func (r *dutyStubRepo) ListQueueFilterOptions(context.Context, ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	return domain.QueueFilterOptions{}, nil
}
func (r *dutyStubRepo) RecordVerdict(context.Context, domain.Verdict) (domain.Item, error) {
	return domain.Item{}, nil
}
func (r *dutyStubRepo) CloseItem(context.Context, domain.CloseAction) (domain.Item, error) {
	return domain.Item{}, nil
}
func (r *dutyStubRepo) CloseSubmission(context.Context, domain.CloseSubmissionAction) ([]domain.Item, error) {
	return nil, nil
}
func (r *dutyStubRepo) ListReadyVaccinationBatchClosures(context.Context, ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	return nil, nil
}
func (r *dutyStubRepo) CloseVaccinationBatch(context.Context, domain.CloseVaccinationBatchAction) ([]domain.Item, error) {
	return nil, nil
}
func (r *dutyStubRepo) WithdrawItemsBySource(context.Context, string, string, string, []string) (int, error) {
	return 0, nil
}

type dutyStubMedia struct{}

func (dutyStubMedia) ResolveMedia(_ context.Context, _ string, ids []string) ([]domain.MediaItem, error) {
	out := make([]domain.MediaItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: "https://signed.example/" + id})
	}
	return out, nil
}

type dutyStubReader struct{ keys []string }

func (d dutyStubReader) ListGrantedModuleKeys(context.Context, string, string) ([]string, error) {
	return d.keys, nil
}

const dutyTestTenant = "22222222-2222-4222-8222-222222222222"

func newDutyHandler(t *testing.T, duties *dutyStubReader) (*Handler, *dutyStubRepo) {
	t.Helper()
	repo := &dutyStubRepo{}
	svc := app.NewService(repo, dutyStubMedia{})
	for _, def := range []domain.CategoryDefinition{
		{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof"},
		{Vertical: "counts", Module: "counts", Category: "shifting_move"},
	} {
		if err := svc.RegisterCategory(def); err != nil {
			t.Fatalf("register category: %v", err)
		}
	}
	if duties != nil {
		svc = svc.WithModuleDutyReader(*duties)
	}
	return NewHandler(svc), repo
}

func dutyRequest(category string) *nethttp.Request {
	r := httptest.NewRequest(nethttp.MethodGet, "/verify/alerts?category="+category, nil)
	ctx := httpmiddleware.WithTenantID(r.Context(), dutyTestTenant)
	ctx = httpmiddleware.WithActorID(ctx, "33333333-3333-4333-8333-333333333333")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		{Role: permissions.RoleVerifier, ScopeType: "tenant", ScopeID: dutyTestTenant},
	})
	return r.WithContext(ctx)
}

func TestListAlertsServesTheCategoryTheVerifierHoldsDutyFor(t *testing.T) {
	h, repo := newDutyHandler(t, &dutyStubReader{keys: []string{"pc.vaccination"}})
	w := httptest.NewRecorder()
	h.ListAlerts(w, dutyRequest("vaccination_proof"))
	if w.Code != nethttp.StatusOK {
		t.Fatalf("expected 200 for the verifier's own module, got %d: %s", w.Code, w.Body.String())
	}
	var body queueListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("the verifier must still get their rows, got %d", len(body.Items))
	}
	if !repo.listed {
		t.Fatal("queue read did not happen")
	}
}

func TestListAlertsRefusesAnotherModulesCategory(t *testing.T) {
	// A counts verifier asking for vaccination proofs: this is the leak the gate closes.
	h, repo := newDutyHandler(t, &dutyStubReader{keys: []string{"counts"}})
	w := httptest.NewRecorder()
	h.ListAlerts(w, dutyRequest("vaccination_proof"))
	if w.Code != nethttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if repo.listed {
		t.Fatal("refused request must not have read the queue")
	}
}

func TestListAlertsUnaffectedWhenDutyDataIsAbsent(t *testing.T) {
	// Unseeded position_module_duties, and the composition root that never wired the
	// reader. Both must behave exactly as before the gate existed.
	for name, duties := range map[string]*dutyStubReader{
		"no duty rows":     {keys: nil},
		"reader not wired": nil,
	} {
		t.Run(name, func(t *testing.T) {
			h, repo := newDutyHandler(t, duties)
			w := httptest.NewRecorder()
			h.ListAlerts(w, dutyRequest("vaccination_proof"))
			if w.Code != nethttp.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if !repo.listed {
				t.Fatal("queue read did not happen")
			}
		})
	}
}

func TestListQueueIsGatedToo(t *testing.T) {
	// /verification/queue takes the same caller-supplied category, so closing only the
	// alerts endpoint would leave the identical read one path over.
	h, repo := newDutyHandler(t, &dutyStubReader{keys: []string{"counts"}})
	w := httptest.NewRecorder()
	h.ListQueue(w, dutyRequest("vaccination_proof"))
	if w.Code != nethttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if repo.listed {
		t.Fatal("refused request must not have read the queue")
	}
}
