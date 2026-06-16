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

func TestReviewSummaryReturnsTotals(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/identity/review-summary", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-review")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["open_conflicts"] != float64(7) || body["open_candidates"] != float64(2) || body["trace_id"] != "req-review" {
		t.Fatalf("unexpected review summary: %#v", body)
	}
}

func TestBulkResolveConflictsRouteReturnsResult(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	reqBody := `{"decision_type":"keep_passport_value","reason":"reviewed legacy evidence","conflicts":[{"conflict_id":"20000000-0000-4000-8000-000000000001","row_version":3}]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/identity/conflicts/bulk-resolve", strings.NewReader(reqBody))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "90000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-bulk")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["decision_type"] != "keep_passport_value" || body["bulk_request_id"] != "req-bulk" || body["trace_id"] != "req-bulk" {
		t.Fatalf("unexpected bulk response: %#v", body)
	}
	if body["counters_rebuild_required"] != true || body["goats_returned_clean"] != float64(1) {
		t.Fatalf("unexpected bulk flags: %#v", body)
	}
	resolved, ok := body["resolved_conflict_ids"].([]any)
	if !ok || len(resolved) != 1 || resolved[0] != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected resolved ids: %#v", body["resolved_conflict_ids"])
	}
}

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

func TestNotImplementedUsesErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/goats", strings.NewReader(`{}`))
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
	if envelope.Code != "admin_goat_writes_deferred" || envelope.TraceID != "req-deferred" {
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

func TestListImportRunsContractShapeAndLimit(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/import-runs?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-import-runs")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ImportRunListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].ImportRunID == "" {
		t.Fatalf("unexpected import run list response: %#v", response)
	}
	if response.TraceID != "req-import-runs" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/admin/import-runs?limit=51", nil)
	badReq.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestListCorrectionRequestsContractShapeAndLimit(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/identity/correction-requests?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "90000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-corrections")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CorrectionRequestListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].CorrectionRequestID == "" || response.NextCursor == nil {
		t.Fatalf("unexpected correction response: %#v", response)
	}
	if response.Items[0].LocationScope.ParkID == nil || response.Items[0].RowVersion == nil || *response.Items[0].RowVersion != 1 {
		t.Fatalf("correction response missing location scope or row_version: %#v", response.Items[0])
	}
	if response.TraceID != "req-corrections" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/admin/identity/correction-requests?limit=10&state=open", nil)
	adminReq.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", adminRec.Code, adminRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodGet, "/admin/identity/correction-requests?limit=101", nil)
	badReq.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("limit status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestListConflictsContractShapeIncludesRowVersion(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/identity/conflicts?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-conflicts")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ConflictListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].RowVersion != 3 {
		t.Fatalf("conflict response missing row_version: %#v", response.Items)
	}
	if response.TraceID != "req-conflicts" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}
}

func TestGetImportRunContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/import-runs/30000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-import-run")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ImportRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.ImportRun.ImportRunID == "" || response.ImportRun.Summary.RowsProcessed != 1223 || response.ImportRun.Summary.CleanMatches != nil {
		t.Fatalf("unexpected import run response: %#v", response.ImportRun)
	}
	if response.TraceID != "req-import-run" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}
}

func TestListImportRunRowsContractShapeAndLimit(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/import-runs/30000000-0000-4000-8000-000000000001/rows?limit=500&processing_state=needs_review&reason_code=blank_old_tag_suffix", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-import-rows")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ImportRunRowsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected one row, got %#v", response.Items)
	}
	row := response.Items[0]
	if row.RowState != "needs_review" || len(row.ReviewReasons) != 1 || row.RFID == nil || *row.RFID != "RFID-SYNTHETIC-0042" {
		t.Fatalf("unexpected row: %#v", row)
	}
	if response.NextCursor == nil {
		t.Fatalf("expected next cursor")
	}

	badReq := httptest.NewRequest(http.MethodGet, "/admin/import-runs/30000000-0000-4000-8000-000000000001/rows?limit=501", nil)
	badReq.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("limit status = %d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestExportImportRunRowsCSVContractShapeAndSafety(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/import-runs/30000000-0000-4000-8000-000000000001/rows.csv?scope=messy", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-import-rows-csv")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("content-type=%q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "mesha-import-review-messy-") {
		t.Fatalf("content-disposition=%q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"import_run_id,import_row_id,row_number,row_state,review_reasons,rfid,old_tag,breed,gender,farm,shed,partition,source_record_id,source_row_key_ref,matched_goat_id,error_reason",
		"30000000-0000-4000-8000-000000000001",
		"RFID-SYNTHETIC-0042",
		"blank_old_tag_suffix",
		"'=cmd",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("CSV missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, ",=cmd,") {
		t.Fatalf("CSV did not escape spreadsheet formula cell:\n%s", body)
	}
}

func TestCreateImportRunRemainsNotImplemented(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/import-runs", strings.NewReader(`{}`))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
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

func TestResolveConflictRejectsOldEvidenceIDsAndCreateGoatNotImplemented(t *testing.T) {
	oldEvidenceBody := `{"decision_type":"merge_goats","decision_result":"same_goat_merge","survivor_goat_id":"10000000-0000-4000-8000-000000000001","affected_goat_ids":["10000000-0000-4000-8000-000000000002"],"identifier_actions":[],"evidence_ids":["synthetic-row-1"],"reason":"synthetic merge reason","row_version":1}`
	rec := postResolveConflict(t, "90000000-0000-4000-8000-000000000001", "idem-conflict-handler-0001", oldEvidenceBody)
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

	unsupportedBody := `{"decision_type":"create_goat","decision_result":"new_goat_required","affected_goat_ids":["10000000-0000-4000-8000-000000000002"],"identifier_actions":[],"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"reason":"synthetic unsupported reason","row_version":1}`
	rec = postResolveConflict(t, "90000000-0000-4000-8000-000000000001", "idem-conflict-handler-0002", unsupportedBody)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "unsupported_conflict_decision" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestResolveConflictReturnsMergeResponse(t *testing.T) {
	resultID := "20000000-0000-4000-8000-000000000001"
	rec := postResolveConflictWithRepo(t, &handlerRepo{
		resolveConflictResult: &ports.ResolveConflictResult{
			ConflictID: resultID,
			State:      "resolved",
			Decision: domain.DecisionRecordSummary{
				DecisionID:     "50000000-0000-4000-8000-000000000201",
				DecisionType:   "merge_goats",
				DecisionResult: "same_goat_merge",
				DecisionState:  "approved",
				PolicyVersion:  "phase1-manual-correction-review-v1",
				CreatedAt:      time.Now().UTC(),
			},
			Merge: &domain.MergeResult{
				SurvivorGoatID: "10000000-0000-4000-8000-000000000001",
				MergedGoatIDs:  []string{"10000000-0000-4000-8000-000000000002"},
			},
			Events:        []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000201", EventType: "goat.identity.merge_approved"}},
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-conflict-handler-0003", validResolveConflictBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ResolveConflictResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision.DecisionType != "merge_goats" || response.Merge == nil || len(response.Events) != 1 || !response.Idempotency.Replayed {
		t.Fatalf("unexpected conflict response: %#v", response)
	}
}

func TestResolveConflictReturnsNonMergeResponse(t *testing.T) {
	rec := postResolveConflictWithRepo(t, &handlerRepo{
		resolveConflictResult: &ports.ResolveConflictResult{
			ConflictID: "20000000-0000-4000-8000-000000000001",
			State:      "rejected",
			Decision: domain.DecisionRecordSummary{
				DecisionID:     "50000000-0000-4000-8000-000000000202",
				DecisionType:   "reject_match",
				DecisionResult: "candidate_rejected",
				DecisionState:  "rejected",
				PolicyVersion:  "phase1-manual-correction-review-v1",
				CreatedAt:      time.Now().UTC(),
			},
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-conflict-handler-0004", validRejectConflictBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.ResolveConflictResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.State != "rejected" || response.Decision.DecisionType != "reject_match" || response.Merge != nil || len(response.Events) != 0 {
		t.Fatalf("unexpected non-merge conflict response: %#v", response)
	}
}

func TestListCandidatesReturnsRowVersion(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/identity/candidates?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-candidates-list")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CandidateListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].RowVersion != 1 || response.TraceID != "req-candidates-list" || response.NextCursor == nil {
		t.Fatalf("unexpected candidate list response: %#v", response)
	}
}

func TestRejectCandidateRejectsBadRequestsAndOldEvidenceIDs(t *testing.T) {
	cases := []struct {
		name           string
		actorID        string
		idempotencyKey string
		body           string
		wantCode       string
	}{
		{name: "missing idempotency", actorID: "90000000-0000-4000-8000-000000000001", body: validRejectCandidateBody(), wantCode: "missing_idempotency_key"},
		{name: "invalid actor", actorID: "not-a-uuid", idempotencyKey: "idem-candidate-handler-0001", body: validRejectCandidateBody(), wantCode: "invalid_actor_id"},
		{name: "unknown field", actorID: "90000000-0000-4000-8000-000000000001", idempotencyKey: "idem-candidate-handler-0002", body: `{"reason":"synthetic","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"row_version":1,"surprise":true}`, wantCode: "invalid_json"},
		{name: "old evidence ids", actorID: "90000000-0000-4000-8000-000000000001", idempotencyKey: "idem-candidate-handler-0003", body: `{"reason":"synthetic","evidence_ids":["synthetic-row-1"],"row_version":1}`, wantCode: "invalid_json"},
		{name: "missing evidence refs", actorID: "90000000-0000-4000-8000-000000000001", idempotencyKey: "idem-candidate-handler-0004", body: `{"reason":"synthetic","evidence_refs":[],"row_version":1}`, wantCode: "missing_evidence_refs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postRejectCandidate(t, tc.actorID, tc.idempotencyKey, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var envelope domain.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if envelope.Code != tc.wantCode {
				t.Fatalf("unexpected envelope: %#v", envelope)
			}
		})
	}
}

func TestRejectCandidateReturnsDecisionAndReplay(t *testing.T) {
	resultID := "80000000-0000-4000-8000-000000000001"
	rec := postRejectCandidateWithRepo(t, &handlerRepo{
		rejectCandidateResult: &ports.RejectCandidateResult{
			Candidate:     candidateResponseFixture(resultID, "rejected", 2),
			Decision:      candidateDecisionFixture("50000000-0000-4000-8000-000000000301"),
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-candidate-handler-0005", validRejectCandidateBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CandidateDecisionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.CandidateID != resultID || response.State != "rejected" || response.Decision.DecisionType != "reject_match" || !response.Idempotency.Replayed {
		t.Fatalf("unexpected candidate decision response: %#v", response)
	}
}

func TestApproveCandidateDispatchesAttach(t *testing.T) {
	rec := postApproveCandidate(t, "90000000-0000-4000-8000-000000000001", "idem-candidate-approve-0001", validApproveCandidateAttachBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.CandidateDecisionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.State != "approved" || response.Decision.DecisionType != "attach_identifier" {
		t.Fatalf("unexpected approve response: %#v", response)
	}
}

func TestApproveCandidateRejectsUnsupportedDecisionType(t *testing.T) {
	// approve-to-create stays blocked: only attach_identifier and merge_goats are accepted.
	body := `{"decision_type":"create_goat","reason":"x","evidence_refs":[{"evidence_type":"source_record","evidence_id":"r1"}],"row_version":1}`
	rec := postApproveCandidate(t, "90000000-0000-4000-8000-000000000001", "idem-candidate-approve-bad-0001", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_decision_type" {
		t.Fatalf("unexpected envelope: %#v", envelope)
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

func postResolveConflict(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postResolveConflictWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postResolveConflictWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/identity/conflicts/20000000-0000-4000-8000-000000000001/resolve", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-resolve-conflict")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postRejectCandidate(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postRejectCandidateWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postRejectCandidateWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/reject", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-reject-candidate")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postApproveCandidate(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/approve", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-approve-candidate")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
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

func validCorrectionBody() string {
	return `{"request_type":"missing_tag","location_scope":{"park_id":"00000000-0000-4000-8000-000000003001"},"description":"synthetic note","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}]}`
}

func validResolveBody() string {
	return `{"state":"approved","reason":"synthetic review reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import","description":"Synthetic source row."}],"row_version":1}`
}

func validAddIdentifierBody() string {
	return `{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","scope_key":"global:rfid","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":1}`
}

func validRetireIdentifierBody() string {
	return `{"reason":"synthetic retire reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":2}`
}

func validResolveConflictBody() string {
	return `{"decision_type":"merge_goats","decision_result":"same_goat_merge","survivor_goat_id":"10000000-0000-4000-8000-000000000001","affected_goat_ids":["10000000-0000-4000-8000-000000000002"],"identifier_actions":[],"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"reason":"synthetic merge reason","row_version":1}`
}

func validRejectConflictBody() string {
	return `{"decision_type":"reject_match","decision_result":"candidate_rejected","affected_goat_ids":["10000000-0000-4000-8000-000000000002"],"identifier_actions":[],"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"reason":"synthetic rejection reason","row_version":1}`
}

func validRejectCandidateBody() string {
	return `{"reason":"synthetic candidate rejection reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-candidate-row-1","source_system":"synthetic_import"}],"row_version":1}`
}

func validApproveCandidateAttachBody() string {
	return `{"decision_type":"attach_identifier","reason":"same goat, attach scanned RFID","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-candidate-row-1","source_system":"synthetic_import"}],"row_version":1,"target_goat_id":"10000000-0000-4000-8000-000000000001","goat_row_version":1,"identifier_type":"rfid","identifier_value":"RFID_APPROVE_0001"}`
}

type handlerRepo struct {
	correctionResult       *ports.CreateCorrectionRequestResult
	resolveResult          *ports.ResolveCorrectionRequestResult
	addIdentifierResult    *ports.AdminGoatMutationResult
	retireIdentifierResult *ports.AdminGoatMutationResult
	resolveConflictResult  *ports.ResolveConflictResult
	rejectCandidateResult  *ports.RejectCandidateResult
	approveCandidateResult *ports.ApproveCandidateResult
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

func (handlerRepo) ListConflicts(context.Context, ports.ListConflictsParams) ([]domain.ConflictSummary, *string, error) {
	next := "eyJ2ZXJzaW9uIjoxLCJjcmVhdGVkX2F0Ijoic3ludGhldGljIiwiY29uZmxpY3RfaWQiOiJzeW50aGV0aWMifQ"
	return []domain.ConflictSummary{conflictResponseFixture("20000000-0000-4000-8000-000000000001", 3)}, &next, nil
}

func (handlerRepo) CountReviewQueues(context.Context, string) (int, int, error) {
	return 7, 2, nil
}

func (handlerRepo) BulkResolveConflicts(_ context.Context, cmd ports.BulkResolveConflictsCommand) (*domain.BulkResolveConflictsResult, error) {
	return &domain.BulkResolveConflictsResult{
		BulkRequestID:           cmd.BulkRequestID,
		DecisionType:            cmd.DecisionType,
		ResolvedConflictIDs:     cmd.ConflictIDs,
		GoatsReturnedClean:      len(cmd.ConflictIDs),
		CountersRebuildRequired: true,
	}, nil
}

func (handlerRepo) GetConflict(context.Context, string, string) (*domain.ConflictDetailResult, error) {
	return nil, ports.ErrNotFound
}

func (handlerRepo) ListCandidates(context.Context, ports.ListCandidatesParams) ([]domain.CandidateSummary, *string, error) {
	next := "eyJjcmVhdGVkX2F0Ijoic3ludGhldGljIiwiY2FuZGlkYXRlX2lkIjoic3ludGhldGljIn0"
	return []domain.CandidateSummary{candidateResponseFixture("80000000-0000-4000-8000-000000000001", "proposed", 1)}, &next, nil
}

func (handlerRepo) GetImportRun(context.Context, string, string) (*domain.ImportRun, error) {
	return importRunResponseFixture("30000000-0000-4000-8000-000000000001"), nil
}

func (handlerRepo) ListImportRuns(context.Context, ports.ListImportRunsParams) ([]domain.ImportRun, error) {
	return []domain.ImportRun{*importRunResponseFixture("30000000-0000-4000-8000-000000000001")}, nil
}

func (handlerRepo) ListImportRunRows(_ context.Context, params ports.ListImportRunRowsParams) ([]domain.ImportRunRow, *string, error) {
	if params.ProcessingState != nil && *params.ProcessingState != "needs_review" {
		return nil, nil, nil
	}
	if params.Cursor != nil {
		return nil, nil, nil
	}
	next := "eyJ2ZXJzaW9uIjoxLCJyb3dfbnVtYmVyIjo0MiwiaW1wb3J0X3Jvd19pZCI6IjcwMDAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwMSJ9"
	return []domain.ImportRunRow{importRunRowResponseFixture("70000000-0000-4000-8000-000000000001")}, &next, nil
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

func (handlerRepo) ListCorrectionRequests(context.Context, ports.ListCorrectionRequestsParams) ([]domain.CorrectionRequest, *string, error) {
	next := "eyJ2ZXJzaW9uIjoxLCJjcmVhdGVkX2F0IjoiMjAyNi0wNi0wOFQwMDowMDowMFoiLCJjb3JyZWN0aW9uX3JlcXVlc3RfaWQiOiI0MDAwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDEifQ"
	rowVersion := 1
	item := correctionResponseFixture("40000000-0000-4000-8000-000000000001")
	item.RowVersion = &rowVersion
	item.CreatedAt = time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	return []domain.CorrectionRequest{item}, &next, nil
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

func (h handlerRepo) ResolveConflict(_ context.Context, cmd ports.ResolveConflictCommand) (*ports.ResolveConflictResult, error) {
	if h.resolveConflictResult != nil {
		return h.resolveConflictResult, nil
	}
	return &ports.ResolveConflictResult{
		ConflictID: cmd.ConflictID,
		State:      "resolved",
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000201",
			DecisionType:   "merge_goats",
			DecisionResult: "same_goat_merge",
			DecisionState:  "approved",
			PolicyVersion:  "phase1-manual-correction-review-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Merge: &domain.MergeResult{
			SurvivorGoatID: cmd.SurvivorGoatID,
			MergedGoatIDs:  []string{cmd.AffectedGoatIDs[0]},
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000201", EventType: "goat.identity.merge_approved"}},
	}, nil
}

func (h handlerRepo) RejectCandidate(_ context.Context, cmd ports.RejectCandidateCommand) (*ports.RejectCandidateResult, error) {
	if h.rejectCandidateResult != nil {
		return h.rejectCandidateResult, nil
	}
	return &ports.RejectCandidateResult{
		Candidate: candidateResponseFixture(cmd.CandidateID, "rejected", cmd.RowVersion+1),
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000301",
			DecisionType:   "reject_match",
			DecisionResult: "candidate_rejected",
			DecisionState:  "rejected",
			PolicyVersion:  "phase1-manual-correction-review-v1",
			CreatedAt:      time.Now().UTC(),
		},
	}, nil
}

func (h handlerRepo) ApproveCandidate(_ context.Context, cmd ports.ApproveCandidateCommand) (*ports.ApproveCandidateResult, error) {
	if h.approveCandidateResult != nil {
		return h.approveCandidateResult, nil
	}
	return &ports.ApproveCandidateResult{
		Candidate: candidateResponseFixture(cmd.CandidateID, "approved", cmd.RowVersion+1),
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000401",
			DecisionType:   cmd.DecisionType,
			DecisionResult: "identifier_attached",
			DecisionState:  "approved",
			PolicyVersion:  "phase1-identifier-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000401", EventType: "goat.identifier.added"}},
	}, nil
}

func (handlerRepo) Ping(context.Context) error { return nil }

func strPtr(value string) *string {
	return &value
}

func correctionResponseFixture(id string) domain.CorrectionRequest {
	rowVersion := 1
	return domain.CorrectionRequest{
		CorrectionRequestID: id,
		RequestType:         "missing_tag",
		State:               "open",
		LocationScope:       domain.LocationScope{ParkID: strPtr("00000000-0000-4000-8000-000000003001")},
		Description:         "synthetic note",
		EvidenceRefs:        []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-row-1"}},
		RowVersion:          &rowVersion,
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

func candidateDecisionFixture(id string) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     id,
		DecisionType:   "reject_match",
		DecisionResult: "candidate_rejected",
		DecisionState:  "rejected",
		PolicyVersion:  "phase1-manual-correction-review-v1",
		CreatedAt:      time.Now().UTC(),
	}
}

func candidateResponseFixture(id, state string, rowVersion int) domain.CandidateSummary {
	return domain.CandidateSummary{
		CandidateID:     id,
		ProposedGoatID:  strPtr("10000000-0000-4000-8000-000000000001"),
		CandidateGoatID: strPtr("10000000-0000-4000-8000-000000000002"),
		MatchScore:      0.93,
		MatchReasons:    []string{"synthetic match reason"},
		State:           state,
		CreatedBy:       "system_rule",
		RowVersion:      rowVersion,
		CreatedAt:       time.Now().UTC(),
	}
}

func conflictResponseFixture(id string, rowVersion int) domain.ConflictSummary {
	return domain.ConflictSummary{
		ConflictID:        id,
		ConflictType:      "possible_duplicate_goat",
		Severity:          "medium",
		GoatCount:         2,
		SourceRecordCount: 1,
		State:             "open",
		RowVersion:        rowVersion,
		CreatedAt:         time.Now().UTC(),
	}
}

func importRunResponseFixture(id string) *domain.ImportRun {
	now := time.Now().UTC()
	return &domain.ImportRun{
		ImportRunID:   id,
		SourceSystem:  "legacy_rfid_db",
		SourceDataset: "rfid_db_first_import",
		PolicyVersion: "phase1-rfid-db-import-v1",
		Status:        "completed",
		DryRun:        false,
		Summary: domain.ImportRunSummary{
			RowsProcessed:     1223,
			GoatsCreated:      711,
			ConflictsOpened:   0,
			RowsNeedingReview: 8,
			ErrorCount:        0,
		},
		CreatedAt:   now.Add(-10 * time.Minute),
		CompletedAt: &now,
	}
}

func importRunRowResponseFixture(id string) domain.ImportRunRow {
	return domain.ImportRunRow{
		ImportRowID:     id,
		SourceRecordID:  strPtr("source-record-synthetic-1"),
		RowNumber:       42,
		RowState:        "needs_review",
		ReviewReasons:   []string{"blank_old_tag_suffix"},
		RFID:            strPtr("RFID-SYNTHETIC-0042"),
		OldTag:          strPtr("1900"),
		Breed:           strPtr("=cmd"),
		Gender:          strPtr("Female"),
		Farm:            strPtr("Synthetic farm"),
		Shed:            strPtr("Synthetic shed"),
		Partition:       strPtr("Synthetic partition"),
		SourceRowKeyRef: strPtr("sha256:synthetic"),
		MatchedGoatID:   nil,
		ErrorReason:     nil,
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
