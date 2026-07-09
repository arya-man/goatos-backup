package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeReader struct {
	rows    []domain.ExecutionRow
	detail  domain.ShedDrilldown
	found   bool
	last    domain.ExecutionQuery
	ops     domain.OperationsResponse
	lastOps domain.OperationsQuery
	roster  []domain.ScanRosterRow
}

type fakeWriter struct {
}

func (f *fakeReader) VaccinationOperations(_ context.Context, q domain.OperationsQuery) (domain.OperationsResponse, error) {
	f.lastOps = q
	return f.ops, nil
}

func (f *fakeReader) VaccinationExecution(_ context.Context, q domain.ExecutionQuery) ([]domain.ExecutionRow, error) {
	f.last = q
	return f.rows, nil
}

func (f *fakeReader) ShedDrilldown(_ context.Context, q domain.ExecutionQuery) (domain.ShedDrilldown, bool, error) {
	f.last = q
	return f.detail, f.found, nil
}

func (f *fakeReader) ScanRoster(_ context.Context, q domain.ScanRosterQuery) ([]domain.ScanRosterRow, error) {
	return f.roster, nil
}

func (w *fakeWriter) ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *obligationdomain.RecoveryReschedule) (string, bool, error) {
	return "obligation-id", false, nil
}

func TestListVaccinationExecutionParsesQueryAndResponds(t *testing.T) {
	reader := &fakeReader{rows: []domain.ExecutionRow{sampleRow()}}
	writer := &fakeWriter{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, writer))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution?park_id=30000000-0000-4000-8000-000000000001&work_state=missed&due_before=2026-07-01T00:00:00Z&limit=9000", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.last.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant = %q", reader.last.TenantID)
	}
	if reader.last.ParkID == nil || *reader.last.ParkID != "30000000-0000-4000-8000-000000000001" {
		t.Fatalf("park id = %v", reader.last.ParkID)
	}
	if reader.last.WorkState == nil || *reader.last.WorkState != domain.WorkStateMissed {
		t.Fatalf("work state = %v", reader.last.WorkState)
	}
	if reader.last.Limit != maxExecutionLimit {
		t.Fatalf("limit = %d want %d", reader.last.Limit, maxExecutionLimit)
	}
	var resp domain.ExecutionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Source != domain.SourceAPI || len(resp.Rows) != 1 || resp.Rows[0].ShedName != "K1 Shed" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestListVaccinationExecutionRejectsInvalidQuery(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?work_state=not_a_state", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid work_state status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?park_id=not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid park_id status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?due_before=not-time", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid due_before status = %d want 400", rec.Code)
	}
}

func TestExecutionParsesAsOf(t *testing.T) {
	reader := &fakeReader{rows: []domain.ExecutionRow{sampleRow()}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	// Valid as_of flows into the query (so the top-bar date actually scopes execution/shed reads).
	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution?as_of=2026-06-24T12:00:00Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if !reader.last.AsOf.Equal(want) {
		t.Fatalf("as_of not parsed into query: got %v want %v", reader.last.AsOf, want)
	}

	// Malformed as_of is a 400.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?as_of=2026-06-24", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed as_of status = %d want 400", rec.Code)
	}
}

func TestGetShedDrilldownValidatesPathAndNotFound(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution/sheds/not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid shed_id status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d want 404", rec.Code)
	}
}

func TestGetShedDrilldownReturnsDetail(t *testing.T) {
	reader := &fakeReader{
		detail: domain.ShedDrilldown{
			ParkID:       "30000000-0000-4000-8000-000000000001",
			ParkName:     "CBE Park",
			ShedID:       "55000000-0000-4000-8000-000000000001",
			ShedName:     "K1 Shed",
			AnimalStages: []string{"K1"},
			Drives:       []domain.DriveSummary{{WorkState: domain.WorkStateDue, Severity: domain.SeverityWatch}},
			Rows:         []domain.ExecutionRow{sampleRow()},
			Summary:      domain.ShedDrilldownSummary{Total: 1, Due: 1},
		},
		found: true,
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution/sheds/55000000-0000-4000-8000-000000000001?limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.last.ShedID == nil || *reader.last.ShedID != "55000000-0000-4000-8000-000000000001" {
		t.Fatalf("shed id = %v", reader.last.ShedID)
	}
	if reader.last.Limit != 10 {
		t.Fatalf("limit = %d want 10", reader.last.Limit)
	}
}

func sampleRow() domain.ExecutionRow {
	return domain.ExecutionRow{
		ParkID:             "30000000-0000-4000-8000-000000000001",
		ParkName:           "CBE Park",
		ShedID:             "55000000-0000-4000-8000-000000000001",
		ShedName:           "K1 Shed",
		AnimalStage:        "K1",
		WorkState:          domain.WorkStateDue,
		Severity:           domain.SeverityWatch,
		SOPStatus:          domain.SOPStatusNotStarted,
		ProofStatus:        domain.ProofStatusMissing,
		VerificationStatus: domain.VerificationStatusNotReady,
		NextAction:         "Start scheduled vaccination SOP",
	}
}
