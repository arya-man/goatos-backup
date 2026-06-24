package identityhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestGetGoatPassportContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-contract")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["goat"] == nil || body["warnings"] == nil || body["trace_id"] != "req-contract" {
		t.Fatalf("unexpected contract shape: %#v", body)
	}
}

func TestSearchGoatsForwardsTableFilters(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=25&q=G-000001&goat_id=10000000-0000-4000-8000-000000000001&identifier_type=rfid&scope_key=global%3Arfid&breed=Sojat&sex=male&farm_id=20000000-0000-4000-8000-000000000001&park_id=30000000-0000-4000-8000-000000000001&location_id=40000000-0000-4000-8000-000000000001&status=alive", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.searchParams == nil {
		t.Fatal("search params were not captured")
	}
	assertPtr := func(name string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s = %v, want %q", name, got, want)
		}
	}
	if repo.searchParams.Limit != 25 || repo.searchParams.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected limit/tenant params: %#v", repo.searchParams)
	}
	assertPtr("q", repo.searchParams.Query, "G-000001")
	assertPtr("goat_id", repo.searchParams.GoatID, "10000000-0000-4000-8000-000000000001")
	assertPtr("identifier_type", repo.searchParams.IdentifierType, "rfid")
	assertPtr("scope_key", repo.searchParams.ScopeKey, "global:rfid")
	assertPtr("breed", repo.searchParams.Breed, "Sojat")
	assertPtr("sex", repo.searchParams.Sex, "male")
	assertPtr("farm_id", repo.searchParams.FarmID, "20000000-0000-4000-8000-000000000001")
	assertPtr("park_id", repo.searchParams.ParkID, "30000000-0000-4000-8000-000000000001")
	assertPtr("location_id", repo.searchParams.LocationID, "40000000-0000-4000-8000-000000000001")
	assertPtr("status", repo.searchParams.Status, "alive")
}

func TestSearchGoatsRejectsInvalidUUIDFilters(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=25&goat_id=not-a-uuid", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search-invalid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.searchParams != nil {
		t.Fatal("repository should not be called for invalid goat_id")
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "invalid_goat_id" {
		t.Fatalf("code = %q, want invalid_goat_id", envelope.Code)
	}
}

func TestMissingTenantReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-Request-ID", "req-missing-tenant")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_tenant_scope" || envelope.TraceID != "req-missing-tenant" || envelope.FieldErrors == nil {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGetGoatTimelineContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001/timeline?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-timeline")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.GoatTimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].EventType != "goat.created" || response.NextCursor == nil {
		t.Fatalf("unexpected timeline response: %#v", response)
	}
	if response.TraceID != "req-timeline" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}
}

func TestAddGoatIdentifierRequiresIdempotencyKey(t *testing.T) {
	rec := postAddGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "", validAddIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_idempotency_key" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAddGoatIdentifierRejectsInvalidActorAndUnknownFields(t *testing.T) {
	rec := postAddGoatIdentifier(t, "not-a-uuid", "idem-add-handler-0001", validAddIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_actor_id" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}

	body := `{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","scope_key":"global:rfid","evidence_ids":["synthetic-row-1"],"row_version":1}`
	rec = postAddGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "idem-add-handler-0002", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_json" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAddGoatIdentifierReplayReturnsAdminResponse(t *testing.T) {
	resultID := "30000000-0000-4000-8000-000000000001"
	rec := postAddGoatIdentifierWithRepo(t, &handlerRepo{
		addIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:          handlerPassport().Summary,
			Identifiers:   []domain.GoatIdentifier{identifierResponseFixture(resultID, "rfid", "RFID-SYNTHETIC-001", "active")},
			Decision:      identifierDecisionFixture("50000000-0000-4000-8000-000000000101", "attach_identifier", "identifier_attached"),
			Events:        []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-add-handler-0003", validAddIdentifierBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision.DecisionType != "attach_identifier" || len(response.Events) != 1 || !response.Idempotency.Replayed {
		t.Fatalf("unexpected admin response: %#v", response)
	}
}

func TestRetireGoatIdentifierRequiresIdempotencyKey(t *testing.T) {
	rec := postRetireGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "", validRetireIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_idempotency_key" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestRetireGoatIdentifierRejectsInvalidActorAndMissingEvidence(t *testing.T) {
	rec := postRetireGoatIdentifier(t, "not-a-uuid", "idem-retire-handler-0001", validRetireIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_actor_id" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}

	rec = postRetireGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "idem-retire-handler-0002", `{"reason":"synthetic retire reason","row_version":2}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_evidence_refs" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestRetireGoatIdentifierReplayReturnsAdminResponse(t *testing.T) {
	resultID := "30000000-0000-4000-8000-000000000001"
	rec := postRetireGoatIdentifierWithRepo(t, &handlerRepo{
		retireIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:          handlerPassport().Summary,
			Identifiers:   []domain.GoatIdentifier{identifierResponseFixture(resultID, "old_tag", "1900", "retired")},
			Decision:      identifierDecisionFixture("50000000-0000-4000-8000-000000000102", "retire_identifier", "identifier_retired"),
			Events:        []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-retire-handler-0003", validRetireIdentifierBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision.DecisionType != "retire_identifier" || len(response.Events) != 1 || !response.Idempotency.Replayed {
		t.Fatalf("unexpected admin response: %#v", response)
	}
}

func postAddGoatIdentifier(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postAddGoatIdentifierWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postAddGoatIdentifierWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-add-identifier")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postRetireGoatIdentifier(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postRetireGoatIdentifierWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postRetireGoatIdentifierWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-retire-identifier")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func validAddIdentifierBody() string {
	return `{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","scope_key":"global:rfid","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":1}`
}

func validRetireIdentifierBody() string {
	return `{"reason":"synthetic retire reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":2}`
}

type handlerRepo struct {
	addIdentifierResult    *ports.AdminGoatMutationResult
	retireIdentifierResult *ports.AdminGoatMutationResult
	searchParams           *ports.SearchGoatsParams
}

func (handlerRepo) GetGoatByID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) GetGoatByDisplayID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (r *handlerRepo) SearchGoats(_ context.Context, params ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	r.searchParams = &params
	return []domain.GoatSummary{handlerPassport().Summary}, nil, nil
}

func (handlerRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return nil, nil
}

func (handlerRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	return nil, nil
}

func (handlerRepo) GetGoatTimeline(context.Context, ports.GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error) {
	next := "eyJ2ZXJzaW9uIjoxLCJvY2N1cnJlZF9hdCI6IjIwMjYtMDYtMDhUMDA6MDA6MDBaIiwiZXZlbnRfaWQiOiI2MDAwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDEifQ"
	return []domain.GoatTimelineEvent{{
		EventID:      "60000000-0000-4000-8000-000000000001",
		EventType:    "goat.created",
		OccurredAt:   time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
		RecordedAt:   time.Date(2026, 6, 8, 0, 1, 0, 0, time.UTC),
		ActorType:    "system",
		EvidenceRefs: []domain.EvidenceRef{},
	}}, &next, nil
}

func (h handlerRepo) AddGoatIdentifier(_ context.Context, cmd ports.AddGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	if h.addIdentifierResult != nil {
		return h.addIdentifierResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{identifierResponseFixture("30000000-0000-4000-8000-000000000001", cmd.IdentifierType, cmd.IdentifierValue, "active")},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000101", "attach_identifier", "identifier_attached"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
	}, nil
}

func (h handlerRepo) RetireGoatIdentifier(_ context.Context, cmd ports.RetireGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	if h.retireIdentifierResult != nil {
		return h.retireIdentifierResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{identifierResponseFixture(cmd.IdentifierID, "old_tag", "1900", "retired")},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000102", "retire_identifier", "identifier_retired"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
	}, nil
}

func (handlerRepo) Ping(context.Context) error { return nil }

func strPtr(value string) *string {
	return &value
}

func identifierDecisionFixture(id, decisionType, result string) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     id,
		DecisionType:   decisionType,
		DecisionResult: result,
		DecisionState:  "approved",
		PolicyVersion:  "phase1-identifier-v1",
		CreatedAt:      time.Now().UTC(),
	}
}

func identifierResponseFixture(id, identifierType, value, status string) domain.GoatIdentifier {
	validFrom := time.Now().UTC().Add(-time.Hour)
	identifier := domain.GoatIdentifier{
		IdentifierID:     id,
		IdentifierType:   identifierType,
		IdentifierValue:  value,
		ScopeKey:         "global:rfid",
		Status:           status,
		IsPrimaryForGoat: false,
		ValidFrom:        validFrom,
	}
	if status == "retired" {
		validTo := time.Now().UTC()
		identifier.ValidTo = &validTo
	}
	return identifier
}

func handlerPassport() *domain.GoatPassport {
	return &domain.GoatPassport{
		GoatID:        "10000000-0000-4000-8000-000000000001",
		DisplayID:     "G-000001",
		Species:       "goat",
		IdentityState: "clean",
		Summary: domain.GoatSummary{
			GoatID:          "10000000-0000-4000-8000-000000000001",
			DisplayID:       "G-000001",
			LifecycleStatus: "alive",
			IdentityState:   "clean",
			LocationPath:    domain.LocationPath{Display: "Synthetic CBE"},
			Warnings:        []domain.Warning{},
		},
		Identifiers:  []domain.GoatIdentifier{},
		EvidenceRefs: []domain.EvidenceRef{},
		FamilyRefs:   []domain.FamilyRef{},
		RowVersion:   1,
	}
}
