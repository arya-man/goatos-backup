package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/app"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

type fakeConfig struct {
	gotVersion domain.NewVersion
	getErr     error
	publishErr error
	listItems  []domain.ConfigListItem
	listErr    error
	gotListCat string
}

func (f *fakeConfig) CreateDefinition(context.Context, domain.NewDefinition) (string, error) {
	return "def-1", nil
}
func (f *fakeConfig) CreateVersion(_ context.Context, in domain.NewVersion) (string, error) {
	f.gotVersion = in
	return "ver-1", nil
}
func (f *fakeConfig) AddRule(context.Context, domain.NewRule) (string, error) { return "rule-1", nil }
func (f *fakeConfig) GetVersion(context.Context, string, string) (domain.Version, error) {
	return domain.Version{}, f.getErr
}
func (f *fakeConfig) PublishVersion(context.Context, string, string, *string) error {
	return f.publishErr
}
func (f *fakeConfig) ListConfigs(_ context.Context, _ string, category string) ([]domain.ConfigListItem, error) {
	f.gotListCat = category
	return f.listItems, f.listErr
}

func serve(h *Handler, method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	Register(mux, h)
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, rdr))
	return rec
}

func TestCreateDefinitionAndVersionForcedDraft(t *testing.T) {
	fake := &fakeConfig{}
	h := NewHandler(fake)

	rec := serve(h, http.MethodPost, "/protocols", `{"code":"vaccination.x","name":"X","category":"vaccination"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create definition: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = serve(h, http.MethodPost, "/protocols/p1/versions",
		`{"scope_type":"tenant","version":1,"effective_from":"2026-06-01T00:00:00Z","rule_dsl":{"a":1}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotVersion.Status != "draft" {
		t.Fatalf("version must be forced draft, got %q", fake.gotVersion.Status)
	}
	if string(fake.gotVersion.RuleDsl) != `{"a":1}` {
		t.Fatalf("rule_dsl not passed through: %s", fake.gotVersion.RuleDsl)
	}
}

func TestPublishSurfacesSourceGate(t *testing.T) {
	// Not source-backed → 422 not_publishable.
	rec := serve(NewHandler(&fakeConfig{publishErr: app.ErrNotPublishable}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish gate: want 422, got %d", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "not_publishable" {
		t.Fatalf("publish gate envelope: %+v err=%v", env, err)
	}

	// Source-backed → 204.
	rec = serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("publish ok: want 204, got %d", rec.Code)
	}
}

func TestGetVersionNotFound(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{getErr: ports.ErrNotFound}), http.MethodGet, "/protocols/versions/v1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestListConfigsReturnsItemsAndDefaultsCategory(t *testing.T) {
	fake := &fakeConfig{listItems: []domain.ConfigListItem{
		{ProtocolID: "p1", Code: "vaccination.enterotox", Name: "Enterotoxaemia", Category: "vaccination",
			ProtocolVersionID: "v1", Version: 1, Status: "draft", SourceSystem: "manual_admin", ReviewStatus: "draft", RuleCount: 2},
	}}
	// No category param → defaults to vaccination.
	rec := serve(NewHandler(fake), http.MethodGet, "/protocols", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list configs: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotListCat != "vaccination" {
		t.Fatalf("category default: want vaccination, got %q", fake.gotListCat)
	}
	var resp configListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ProtocolVersionID != "v1" || resp.Items[0].RuleCount != 2 {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
	if resp.Items[0].Status != "draft" || resp.Items[0].SourceSystem != "manual_admin" {
		t.Fatalf("source-review state not surfaced: %+v", resp.Items[0])
	}

	// Explicit category param is passed through.
	rec = serve(NewHandler(fake), http.MethodGet, "/protocols?category=feed_direction", "")
	if rec.Code != http.StatusOK || fake.gotListCat != "feed_direction" {
		t.Fatalf("category passthrough: code=%d cat=%q", rec.Code, fake.gotListCat)
	}
}

func TestCreateDefinitionEmptyBody(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body: want 400, got %d", rec.Code)
	}
}
