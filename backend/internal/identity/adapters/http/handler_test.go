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

func TestNotImplementedUsesErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001/timeline?limit=10", nil)
	req.Header.Set("X-Request-ID", "req-deferred")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "goat_timeline_deferred" || envelope.TraceID != "req-deferred" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAnalyticsTenantScopeMismatchReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/analytics/identity/counts?grain=tenant_lifecycle&tenant_id=00000000-0000-4000-8000-000000000002", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-tenant-mismatch")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "tenant_scope_mismatch" || envelope.TraceID != "req-tenant-mismatch" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestCreateCorrectionRequestRequiresIdempotencyKey(t *testing.T) {
	rec := postCorrectionRequest(t, "90000000-0000-4000-8000-000000000001", "", validCorrectionBody())
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

func TestCreateCorrectionRequestRejectsInvalidActorID(t *testing.T) {
	rec := postCorrectionRequest(t, "not-a-uuid", "idem-handler-0001", validCorrectionBody())
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
}

func TestCreateCorrectionRequestRejectsUnknownJSONFields(t *testing.T) {
	body := `{"request_type":"missing_tag","location_scope":{"park_id":"00000000-0000-4000-8000-000000003001"},"description":"synthetic note","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"surprise":true}`
	rec := postCorrectionRequest(t, "90000000-0000-4000-8000-000000000001", "idem-handler-0002", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_json" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestCreateCorrectionRequestReplayReturnsOK(t *testing.T) {
	resultID := "40000000-0000-4000-8000-000000000001"
	rec := postCorrectionRequestWithRepo(t, &handlerRepo{
		correctionResult: &ports.CreateCorrectionRequestResult{
			CorrectionRequest: correctionResponseFixture(resultID),
			Replayed:          true,
			FirstResultID:     &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-handler-0003", validCorrectionBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CorrectionRequestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !response.Idempotency.Replayed || response.Idempotency.FirstResultID == nil || *response.Idempotency.FirstResultID != resultID {
		t.Fatalf("unexpected idempotency response: %#v", response.Idempotency)
	}
}

func TestResolveCorrectionRequestRejectsMissingIdempotencyKey(t *testing.T) {
	rec := postResolveCorrectionRequest(t, "90000000-0000-4000-8000-000000000001", "", validResolveBody())
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

func TestResolveCorrectionRequestRejectsInvalidActorID(t *testing.T) {
	rec := postResolveCorrectionRequest(t, "not-a-uuid", "idem-resolve-handler-0001", validResolveBody())
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
}

func TestResolveCorrectionRequestRejectsUnknownAndOldEvidenceIDsFields(t *testing.T) {
	for _, body := range []string{
		`{"state":"approved","reason":"synthetic reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"row_version":1,"surprise":true}`,
		`{"state":"approved","reason":"synthetic reason","evidence_ids":["synthetic-row-1"],"row_version":1}`,
	} {
		rec := postResolveCorrectionRequest(t, "90000000-0000-4000-8000-000000000001", "idem-resolve-handler-0002", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var envelope domain.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if envelope.Code != "invalid_json" {
			t.Fatalf("unexpected envelope: %#v", envelope)
		}
	}
}

func TestResolveCorrectionRequestRejectsMissingEvidenceRefs(t *testing.T) {
	rec := postResolveCorrectionRequest(t, "90000000-0000-4000-8000-000000000001", "idem-resolve-handler-0004", `{"state":"approved","reason":"synthetic reason","row_version":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_evidence_refs" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestResolveCorrectionRequestReplayReturnsDecision(t *testing.T) {
	resultID := "40000000-0000-4000-8000-000000000001"
	rec := postResolveCorrectionRequestWithRepo(t, &handlerRepo{
		resolveResult: &ports.ResolveCorrectionRequestResult{
			CorrectionRequest: resolvedCorrectionResponseFixture(resultID, "approved", 2),
			Decision:          decisionResponseFixture("50000000-0000-4000-8000-000000000001", "approved", "approved"),
			Replayed:          true,
			FirstResultID:     &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-resolve-handler-0003", validResolveBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CorrectionRequestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision == nil || response.Decision.DecisionType != "resolve_correction_request" {
		t.Fatalf("decision missing from response: %#v", response.Decision)
	}
	if !response.Idempotency.Replayed {
		t.Fatalf("expected replayed response: %#v", response.Idempotency)
	}
}

func postCorrectionRequest(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postCorrectionRequestWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postCorrectionRequestWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/identity/correction-requests", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-correction")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postResolveCorrectionRequest(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postResolveCorrectionRequestWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postResolveCorrectionRequestWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/identity/correction-requests/40000000-0000-4000-8000-000000000001/resolve", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-resolve")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func validCorrectionBody() string {
	return `{"request_type":"missing_tag","location_scope":{"park_id":"00000000-0000-4000-8000-000000003001"},"description":"synthetic note","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}]}`
}

func validResolveBody() string {
	return `{"state":"approved","reason":"synthetic review reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import","description":"Synthetic source row."}],"row_version":1}`
}

type handlerRepo struct {
	correctionResult *ports.CreateCorrectionRequestResult
	resolveResult    *ports.ResolveCorrectionRequestResult
}

func (handlerRepo) GetGoatByID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) GetGoatByDisplayID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) SearchGoats(context.Context, ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	return []domain.GoatSummary{handlerPassport().Summary}, nil, nil
}

func (handlerRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return nil, nil
}

func (handlerRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	return nil, nil
}

func (handlerRepo) ListConflicts(context.Context, ports.ListConflictsParams) ([]domain.ConflictSummary, *string, error) {
	return nil, nil, nil
}

func (handlerRepo) GetConflict(context.Context, string, string) (*domain.ConflictDetailResult, error) {
	return nil, ports.ErrNotFound
}

func (handlerRepo) ListIdentityCounts(context.Context, ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	return nil, domain.Freshness{}, nil
}

func (h handlerRepo) CreateCorrectionRequest(context.Context, ports.CreateCorrectionRequestCommand) (*ports.CreateCorrectionRequestResult, error) {
	if h.correctionResult != nil {
		return h.correctionResult, nil
	}
	return &ports.CreateCorrectionRequestResult{
		CorrectionRequest: correctionResponseFixture("40000000-0000-4000-8000-000000000001"),
	}, nil
}

func (h handlerRepo) ResolveCorrectionRequest(context.Context, ports.ResolveCorrectionRequestCommand) (*ports.ResolveCorrectionRequestResult, error) {
	if h.resolveResult != nil {
		return h.resolveResult, nil
	}
	return &ports.ResolveCorrectionRequestResult{
		CorrectionRequest: resolvedCorrectionResponseFixture("40000000-0000-4000-8000-000000000001", "approved", 2),
		Decision:          decisionResponseFixture("50000000-0000-4000-8000-000000000001", "approved", "approved"),
	}, nil
}

func (handlerRepo) Ping(context.Context) error { return nil }

func strPtr(value string) *string {
	return &value
}

func correctionResponseFixture(id string) domain.CorrectionRequest {
	return domain.CorrectionRequest{
		CorrectionRequestID: id,
		RequestType:         "missing_tag",
		State:               "open",
		LocationScope:       domain.LocationScope{ParkID: strPtr("00000000-0000-4000-8000-000000003001")},
		Description:         "synthetic note",
		EvidenceRefs:        []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-row-1"}},
	}
}

func resolvedCorrectionResponseFixture(id, state string, rowVersion int) domain.CorrectionRequest {
	resolvedAt := time.Now().UTC()
	return domain.CorrectionRequest{
		CorrectionRequestID: id,
		RequestType:         "missing_tag",
		State:               state,
		LocationScope:       domain.LocationScope{ParkID: strPtr("00000000-0000-4000-8000-000000003001")},
		Description:         "synthetic note",
		EvidenceRefs:        []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-row-1"}},
		RowVersion:          intPtr(rowVersion),
		CreatedAt:           resolvedAt.Add(-time.Minute),
		ResolvedAt:          &resolvedAt,
	}
}

func decisionResponseFixture(id, result, state string) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     id,
		DecisionType:   "resolve_correction_request",
		DecisionResult: result,
		DecisionState:  state,
		PolicyVersion:  "phase1-manual-correction-review-v1",
		CreatedAt:      time.Now().UTC(),
	}
}

func intPtr(value int) *int {
	return &value
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
