package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/alerts/app"
	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	tenant = "00000000-0000-4000-8000-000000000001"
	parkA  = "00000000-0000-4000-8000-00000000000a"
	parkB  = "00000000-0000-4000-8000-00000000000b"
)

type fakeService struct {
	stored []domain.StoredRuleConfig
	lastIn domain.SetRuleConfig
	rows   map[string][]domain.Alert
}

func (f *fakeService) List(_ context.Context, _ string, parkID, businessDate string) (app.Page, error) {
	return app.Page{Rows: f.rows[parkID], RulesRun: []domain.RuleKey{domain.RulePenFeedQuantityChange, domain.RuleFeedLowStock}}, nil
}

func (f *fakeService) Config(_ context.Context, _ string) ([]domain.RuleConfig, error) {
	return domain.EffectiveConfig(f.stored), nil
}

func (f *fakeService) SetConfig(_ context.Context, in domain.SetRuleConfig) (domain.RuleConfig, error) {
	if err := in.Validate(); err != nil {
		return domain.RuleConfig{}, err
	}
	f.lastIn = in
	f.stored = append(f.stored, domain.StoredRuleConfig{Key: in.Key, Enabled: in.Enabled, Threshold: in.Threshold})
	for _, c := range domain.EffectiveConfig(f.stored) {
		if c.Key == in.Key {
			return c, nil
		}
	}
	return domain.RuleConfig{}, domain.ErrUnknownRule
}

func do(t *testing.T, h *Handler, method, path, body string, grants []permissions.ActiveGrant, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithActorID(ctx, "00000000-0000-4000-8000-000000000099")
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	req = req.WithContext(ctx)
	mux := http.NewServeMux()
	Register(mux, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func ceo() []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}}
}

func TestRowsClampsAParkScopedCallerToTheirPark(t *testing.T) {
	svc := &fakeService{rows: map[string][]domain.Alert{
		parkA: {{Key: "a", Severity: domain.SeverityCritical}},
		parkB: {{Key: "b", Severity: domain.SeverityWarning}},
	}}
	h := NewHandler(svc, nil)
	grants := []permissions.ActiveGrant{{Role: permissions.RolePCDirector, ScopeType: "park", ScopeID: parkA}}
	rec, out := do(t, h, http.MethodGet, "/alerts/rows?park="+parkB, "", grants, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a park-scoped caller asking for another park must be refused, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodGet, "/alerts/rows", "", grants, nil)
	if rec.Code != http.StatusOK || out["park_id"] != parkA || out["total"].(float64) != 1 || out["critical"].(float64) != 1 {
		t.Fatalf("a park-scoped caller with no park named reads their own park: %d %v", rec.Code, out)
	}
}

func TestRowsRequiresAParkForATenantWideCaller(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)
	rec, out := do(t, h, http.MethodGet, "/alerts/rows", "", ceo(), nil)
	if rec.Code != http.StatusBadRequest || out["error"] != "park_required" {
		t.Fatalf("tenant-wide caller must name a park, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodGet, "/alerts/rows?park="+parkA+"&business_date=16-09-2026", "", ceo(), nil)
	if rec.Code != http.StatusBadRequest || out["error"] != "invalid_business_date" {
		t.Fatalf("bad date must be refused, got %d %v", rec.Code, out)
	}
}

func TestSetConfigRefusesBlankIdempotencyOutOfRangeAndUnknownRule(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, out := do(t, h, http.MethodPut, "/alerts/config/feed_low_stock", `{"enabled":true,"threshold":7}`, ceo(), nil)
	if rec.Code != http.StatusBadRequest || out["error"] != "idempotency_key_required" {
		t.Fatalf("missing Idempotency-Key must be refused, got %d %v", rec.Code, out)
	}
	idem := map[string]string{"Idempotency-Key": "k1"}
	rec, out = do(t, h, http.MethodPut, "/alerts/config/feed_low_stock", `{"enabled":true}`, ceo(), idem)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a blank threshold is not a zero and not 'keep': must be refused, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodPut, "/alerts/config/feed_low_stock", `{"enabled":true,"threshold":0}`, ceo(), idem)
	if rec.Code != http.StatusUnprocessableEntity || out["error"] != "threshold_out_of_range" {
		t.Fatalf("out-of-range threshold must be refused, never clamped, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodPut, "/alerts/config/made_up", `{"enabled":true,"threshold":3}`, ceo(), idem)
	if rec.Code != http.StatusNotFound || out["error"] != "unknown_rule" {
		t.Fatalf("unknown rule must be refused, got %d %v", rec.Code, out)
	}
	rec, out = do(t, h, http.MethodPut, "/alerts/config/feed_low_stock", `{"enabled":false,"threshold":7}`, ceo(), idem)
	if rec.Code != http.StatusOK || out["key"] != "feed_low_stock" || out["enabled"] != false || out["threshold"].(float64) != 7 || out["stored"] != true {
		t.Fatalf("valid write returns the effective row, got %d %v", rec.Code, out)
	}
	if svc.lastIn.IdempotencyKey != "k1" || svc.lastIn.ActorID == "" {
		t.Fatalf("write must carry the idempotency key and actor: %+v", svc.lastIn)
	}
	rec, out = do(t, h, http.MethodGet, "/alerts/config", "", ceo(), nil)
	rules := out["rules"].([]any)
	if rec.Code != http.StatusOK || len(rules) != len(domain.Rules()) {
		t.Fatalf("config lists exactly the catalog, got %d %v", rec.Code, out)
	}
}
