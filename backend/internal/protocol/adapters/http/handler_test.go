package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/app"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

type fakeConfig struct {
	gotDefinition domain.NewDefinition
	gotVersion    domain.NewVersion
	gotRule       domain.NewRule
	createErr     error
	versionErr    error
	getErr        error
	addRuleErr    error
	publishErr    error
	listItems     []domain.ConfigListItem
	listErr       error
	gotListCat    string
	stages        []domain.AnimalStage
	stagesErr     error
	gotStagesTn   string
	discardErr    error
	discardedID   string
}

func (f *fakeConfig) CreateDefinition(_ context.Context, in domain.NewDefinition) (string, error) {
	f.gotDefinition = in
	return "def-1", f.createErr
}
func (f *fakeConfig) CreateVersion(_ context.Context, in domain.NewVersion) (string, error) {
	f.gotVersion = in
	return "ver-1", f.versionErr
}
func (f *fakeConfig) AddRule(_ context.Context, in domain.NewRule) (string, error) {
	f.gotRule = in
	return "rule-1", f.addRuleErr
}
func (f *fakeConfig) GetVersion(context.Context, string, string) (domain.Version, error) {
	return domain.Version{}, f.getErr
}
func (f *fakeConfig) PublishVersion(context.Context, string, string, *string, ...string) error {
	return f.publishErr
}
func (f *fakeConfig) DiscardVersion(_ context.Context, _ string, versionID string) error {
	f.discardedID = versionID
	return f.discardErr
}
func (f *fakeConfig) ListConfigs(_ context.Context, _ string, category string) ([]domain.ConfigListItem, error) {
	f.gotListCat = category
	return f.listItems, f.listErr
}
func (f *fakeConfig) ListAnimalStages(_ context.Context, tenantID string) ([]domain.AnimalStage, error) {
	f.gotStagesTn = tenantID
	return f.stages, f.stagesErr
}

func serve(h *Handler, method, target, body string) *httptest.ResponseRecorder {
	return serveWithIdempotency(h, method, target, body, "test-idempotency-key")
}

func serveWithIdempotency(h *Handler, method, target, body, idempotencyKey string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	Register(mux, h)
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, rdr)
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateDefinitionAndVersionForcedDraft(t *testing.T) {
	fake := &fakeConfig{}
	h := NewHandler(fake)

	rec := serve(h, http.MethodPost, "/protocols", `{"code":"vaccination.x","name":"X","category":"vaccination"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create definition: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotDefinition.IdempotencyKey != "test-idempotency-key" {
		t.Fatalf("definition idempotency key not forwarded: %q", fake.gotDefinition.IdempotencyKey)
	}

	rec = serve(h, http.MethodPost, "/protocols/p1/versions",
		`{"scope_type":"tenant","version":1,"effective_from":"2026-06-01T00:00:00Z","rule_dsl":{"a":1}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotVersion.Status != "draft" {
		t.Fatalf("version must be forced draft, got %q", fake.gotVersion.Status)
	}
	if fake.gotVersion.IdempotencyKey != "test-idempotency-key" {
		t.Fatalf("version idempotency key not forwarded: %q", fake.gotVersion.IdempotencyKey)
	}
	if string(fake.gotVersion.RuleDsl) != `{"a":1}` {
		t.Fatalf("rule_dsl not passed through: %s", fake.gotVersion.RuleDsl)
	}
}

func TestProtocolWritesRequireIdempotencyKey(t *testing.T) {
	rec := serveWithIdempotency(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols", `{"code":"vaccination.x","name":"X","category":"vaccination"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "invalid_idempotency_key" {
		t.Fatalf("missing idempotency envelope: %+v err=%v", env, err)
	}
}

func TestProtocolWritesRejectUnknownJSONFields(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols", `{"code":"vaccination.x","name":"X","category":"vaccination","ignored":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "invalid_json" {
		t.Fatalf("unknown field envelope: %+v err=%v", env, err)
	}
}

func TestCreateDefinitionSurfacesIdempotencyConflict(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{createErr: ports.ErrIdempotencyConflict}), http.MethodPost, "/protocols", `{"code":"vaccination.x","name":"X","category":"vaccination"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "idempotency_conflict" {
		t.Fatalf("idempotency conflict envelope: %+v err=%v", env, err)
	}
}

func TestCreateVersionSurfacesInvalidRuleDSL(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{versionErr: app.ErrInvalidRuleDSL}), http.MethodPost, "/protocols/p1/versions",
		`{"scope_type":"tenant","version":1,"effective_from":"2026-06-01T00:00:00Z","rule_dsl":{"eligibilty":{}}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule_dsl: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "invalid_rule_dsl" {
		t.Fatalf("invalid rule_dsl envelope: %+v err=%v", env, err)
	}
}

func TestPublishSurfacesExecutableContractGate(t *testing.T) {
	// Not executable → 422 not_publishable.
	rec := serve(NewHandler(&fakeConfig{publishErr: app.ErrNotPublishable}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish gate: want 422, got %d", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "not_publishable" {
		t.Fatalf("publish gate envelope: %+v err=%v", env, err)
	}

	// Executable version → 204.
	rec = serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("publish ok: want 204, got %d", rec.Code)
	}
}

func TestPublishSurfacesUnsupportedRepeatPolicy(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{publishErr: fmt.Errorf("%w: schedule[0] %w", app.ErrNotPublishable, app.ErrUnsupportedRepeatPolicy)}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish unsupported repeat: want 422, got %d", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "unsupported_repeat_policy" {
		t.Fatalf("publish unsupported repeat envelope: %+v err=%v", env, err)
	}
}

func TestPublishSurfacesInvalidRuleDSL(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{publishErr: app.ErrInvalidRuleDSL}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish invalid rule_dsl: want 422, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "invalid_rule_dsl" {
		t.Fatalf("publish invalid rule_dsl envelope: %+v err=%v", env, err)
	}
}

func TestPublishSurfacesVersionNotDraft(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{publishErr: ports.ErrVersionNotDraft}), http.MethodPost, "/protocols/versions/v1/publish", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("publish conflict: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "version_not_draft" {
		t.Fatalf("publish conflict envelope: %+v err=%v", env, err)
	}
}

func TestAddRuleSurfacesPublishedVersionImmutable(t *testing.T) {
	body := `{"dose_code":"primary","sequence":1,"trigger_type":"post_arrival","repeat":"none","catch_up":"immediate","eligibility_json":{},"proof_policy":{}}`
	rec := serve(NewHandler(&fakeConfig{addRuleErr: ports.ErrVersionNotDraft}), http.MethodPost, "/protocols/versions/v1/rules", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("add rule conflict: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "version_not_draft" {
		t.Fatalf("add rule conflict envelope: %+v err=%v", env, err)
	}
}

func TestAddRuleRejectsMissingRequiredFields(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols/versions/v1/rules", `{"dose_code":"primary"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "missing_required_field" {
		t.Fatalf("missing fields envelope: %+v err=%v", env, err)
	}
}

func TestAddRuleAcceptsRowProofPolicyArray(t *testing.T) {
	fake := &fakeConfig{}
	body := `{"dose_code":"primary","sequence":1,"trigger_type":"post_arrival","repeat":"none","catch_up":"immediate","eligibility_json":{},"proof_policy":["shed_video","vial_photo"]}`
	rec := serve(NewHandler(fake), http.MethodPost, "/protocols/versions/v1/rules", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add rule with proof array: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := string(fake.gotRule.ProofPolicy); got != `["shed_video","vial_photo"]` {
		t.Fatalf("proof_policy array not passed through: %s", got)
	}
}

func TestAddRuleSurfacesUnsupportedRepeatPolicy(t *testing.T) {
	body := `{"dose_code":"primary","sequence":1,"trigger_type":"post_arrival","repeat":"until_age","catch_up":"immediate","eligibility_json":{},"proof_policy":{}}`
	rec := serve(NewHandler(&fakeConfig{addRuleErr: app.ErrUnsupportedRepeatPolicy}), http.MethodPost, "/protocols/versions/v1/rules", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add rule unsupported repeat: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != "unsupported_repeat_policy" {
		t.Fatalf("unsupported repeat envelope: %+v err=%v", env, err)
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
			ProtocolVersionID: "v1", Version: 1, Status: "draft", RuleCount: 2},
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
	if resp.Items[0].Status != "draft" {
		t.Fatalf("status not surfaced: %+v", resp.Items[0])
	}

	// Explicit category param is passed through.
	rec = serve(NewHandler(fake), http.MethodGet, "/protocols?category=feed_direction", "")
	if rec.Code != http.StatusOK || fake.gotListCat != "feed_direction" {
		t.Fatalf("category passthrough: code=%d cat=%q", rec.Code, fake.gotListCat)
	}
}

func TestListAnimalStagesReturnsStages(t *testing.T) {
	minK1 := int32(0)
	maxK1 := int32(7)
	fake := &fakeConfig{stages: []domain.AnimalStage{
		{AnimalStageID: "as-1", StageCode: "K1", Name: "milk training", MinAgeDays: &minK1, MaxAgeDays: &maxK1, SortOrder: 1},
		{AnimalStageID: "as-2", StageCode: "K2", Name: "milk drinking", SortOrder: 2},
	}}
	rec := serve(NewHandler(fake), http.MethodGet, "/protocols/animal-stages", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("animal stages: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp animalStageListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0].StageCode != "K1" || resp.Items[1].StageCode != "K2" {
		t.Fatalf("unexpected stages: %+v", resp.Items)
	}
	if resp.Items[0].MaxAgeDays == nil || *resp.Items[0].MaxAgeDays != 7 {
		t.Fatalf("K1 max_age_days not surfaced: %+v", resp.Items[0])
	}
	// K2 has open-ended age bands → both nil (omitted), not a fake 0.
	if resp.Items[1].MinAgeDays != nil || resp.Items[1].MaxAgeDays != nil {
		t.Fatalf("K2 open-ended bands should be nil: %+v", resp.Items[1])
	}
}

// An empty animal_stage_lookup is an honest empty list (the UI shows a seed-stages state); it is a
// 200 with zero items, never an error and never hardcoded fallback codes.
func TestListAnimalStagesEmptyIsHonest(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{}), http.MethodGet, "/protocols/animal-stages", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty stages: want 200, got %d", rec.Code)
	}
	var resp animalStageListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("want zero items, got %+v", resp.Items)
	}
}

func TestCreateDefinitionEmptyBody(t *testing.T) {
	rec := serve(NewHandler(&fakeConfig{}), http.MethodPost, "/protocols", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body: want 400, got %d", rec.Code)
	}
}

// TestDiscardVersionRefusesPublished proves the handler maps a not-draft refusal to
// 409 rather than a generic error. The restriction itself lives in SQL; this asserts
// the caller is told WHY, because a 500 would send someone looking for an outage.
func TestDiscardVersionRefusesPublished(t *testing.T) {
	fake := &fakeConfig{discardErr: ports.ErrVersionNotDraft}
	rec := serve(NewHandler(fake), http.MethodPost, "/protocols/versions/ver-1/discard", "")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "version_not_draft") {
		t.Fatalf("body = %s, want version_not_draft", rec.Body.String())
	}
}

// TestDiscardVersionDeletesDraft proves the success path answers 204 and passes the
// version id straight through.
func TestDiscardVersionDeletesDraft(t *testing.T) {
	fake := &fakeConfig{}
	rec := serve(NewHandler(fake), http.MethodPost, "/protocols/versions/draft-9/discard", "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if fake.discardedID != "draft-9" {
		t.Fatalf("discarded %q, want draft-9", fake.discardedID)
	}
}
