package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	obligationports "github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeReader struct {
	rows         []domain.ExecutionRow
	detail       domain.ShedDrilldown
	found        bool
	last         domain.ExecutionQuery
	ops          domain.OperationsResponse
	lastOps      domain.OperationsQuery
	roster       []domain.ScanRosterRow
	gaps         domain.GapsResponse
	lastGaps     domain.GapsQuery
	coverage     domain.CoverageResponse
	lastCoverage domain.OperationsQuery
	shedSummary  domain.ShedSummaryResponse
	lastShedSum  domain.ShedSummaryQuery
	shedDetail   domain.ShedDetailResponse
	shedFound    bool
	lastShedID   string
	shedAnimals  domain.ShedAnimalPage
	lastShedAnim domain.ShedAnimalQuery
	capacityCfg  domain.CapacityConfig
}

type fakeWriter struct {
	rescheduleID          string
	rescheduleReplay      bool
	rescheduleErr         error
	lastRescheduleTenant  string
	lastRescheduleObl     string
	lastRescheduleIdemKey string
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

func (f *fakeReader) VaccinationGaps(_ context.Context, q domain.GapsQuery) (domain.GapsResponse, error) {
	f.lastGaps = q
	return f.gaps, nil
}

func (f *fakeReader) CoverageRollup(_ context.Context, q domain.OperationsQuery) (domain.CoverageResponse, error) {
	f.lastCoverage = q
	return f.coverage, nil
}

func (f *fakeReader) ShedSummary(_ context.Context, q domain.ShedSummaryQuery) (domain.ShedSummaryResponse, error) {
	f.lastShedSum = q
	return f.shedSummary, nil
}

func (f *fakeReader) ShedDetail(_ context.Context, shedID string, q domain.OperationsQuery) (domain.ShedDetailResponse, bool, error) {
	f.lastShedID = shedID
	f.lastOps = q
	return f.shedDetail, f.shedFound, nil
}

func (f *fakeReader) ShedAnimals(_ context.Context, q domain.ShedAnimalQuery) (domain.ShedAnimalPage, error) {
	f.lastShedAnim = q
	return f.shedAnimals, nil
}

func (f *fakeReader) CapacityConfig(_ context.Context, _ string) (domain.CapacityConfig, error) {
	return f.capacityCfg, nil
}

func (w *fakeWriter) ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *obligationdomain.RecoveryReschedule) (string, bool, error) {
	return "obligation-id", false, nil
}

func (w *fakeWriter) RescheduleObligationByID(ctx context.Context, tenantID, obligationID, idempotencyKey string, dueAt, windowStart time.Time, windowEnd *time.Time, occurredAt time.Time) (string, bool, error) {
	w.lastRescheduleTenant = tenantID
	w.lastRescheduleObl = obligationID
	w.lastRescheduleIdemKey = idempotencyKey
	if w.rescheduleErr != nil {
		return "", false, w.rescheduleErr
	}
	id := w.rescheduleID
	if id == "" {
		id = obligationID
	}
	return id, w.rescheduleReplay, nil
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

func TestListShedSummaryParsesFiltersAndPaginates(t *testing.T) {
	reader := &fakeReader{shedSummary: domain.ShedSummaryResponse{
		Source: domain.SourceAPI,
		Rows:   []domain.ShedSummaryRow{{ParkName: "CBE", ShedName: "Castro 1", Animals: 54, Due: 1, Done: 53, Status: domain.ShedStatusOverdue}},
		Page:   domain.PageInfo{Total: 1, Limit: 25, Offset: 25},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/sheds?park_id=30000000-0000-4000-8000-000000000001&status=overdue&capacity=over_cap&sort=due_desc&q=castro&limit=25&page=2", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	got := reader.lastShedSum
	if got.Capacity == nil || *got.Capacity != domain.CapacityOverCap {
		t.Errorf("capacity filter = %v", got.Capacity)
	}
	if got.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("tenant = %q", got.TenantID)
	}
	if got.ParkID == nil || *got.ParkID != "30000000-0000-4000-8000-000000000001" {
		t.Errorf("park = %v", got.ParkID)
	}
	if got.Status == nil || *got.Status != domain.ShedStatusOverdue {
		t.Errorf("status = %v", got.Status)
	}
	if got.Sort != domain.ShedSortDueDesc {
		t.Errorf("sort = %q", got.Sort)
	}
	if got.Search == nil || *got.Search != "castro" {
		t.Errorf("search = %v", got.Search)
	}
	if got.Limit != 25 || got.Offset != 25 { // page 2 * limit 25 -> offset 25
		t.Errorf("pagination limit=%d offset=%d want 25/25", got.Limit, got.Offset)
	}
	var resp domain.ShedSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 || resp.Page.Total != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestListShedSummaryRejectsInvalidQuery(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))
	for _, q := range []string{
		"/vaccination/sheds?status=not_a_status",
		"/vaccination/sheds?capacity=not_a_capacity",
		"/vaccination/sheds?sort=not_a_sort",
		"/vaccination/sheds?park_id=not-a-uuid",
		"/vaccination/sheds?page=0",
		"/vaccination/sheds?limit=-1",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d want 400", q, rec.Code)
		}
	}
}

func TestGetShedDetailNotFound(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{shedFound: false}, &fakeWriter{}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/vaccination/sheds/30000000-0000-4000-8000-000000000009", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d want 404", rec.Code)
	}
}

func TestGetShedAnimalsParsesCursor(t *testing.T) {
	reader := &fakeReader{shedAnimals: domain.ShedAnimalPage{Rows: []domain.ShedAnimalRow{{GoatID: "g1", DisplayID: "G-1", Status: "due"}}}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))
	req := httptest.NewRequest(http.MethodGet, "/vaccination/sheds/30000000-0000-4000-8000-000000000009/animals?cursor=40000000-0000-4000-8000-000000000001&limit=50", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastShedAnim.ShedID != "30000000-0000-4000-8000-000000000009" {
		t.Errorf("shed = %q", reader.lastShedAnim.ShedID)
	}
	if reader.lastShedAnim.Cursor == nil || *reader.lastShedAnim.Cursor != "40000000-0000-4000-8000-000000000001" {
		t.Errorf("cursor = %v", reader.lastShedAnim.Cursor)
	}
	if reader.lastShedAnim.Limit != 50 {
		t.Errorf("limit = %d", reader.lastShedAnim.Limit)
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

const rescheduleObligationID = "70000000-0000-4000-8000-000000000001"

func buildRescheduleRequest(t *testing.T, obligationID, idempotencyKey, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/app/vaccination/obligations/"+obligationID+"/reschedule", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	return req
}

func TestRescheduleObligationSucceeds(t *testing.T) {
	writer := &fakeWriter{rescheduleID: rescheduleObligationID}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, writer))

	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"due_at":"` + future + `"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "idem-key-1", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	var resp rescheduleResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ObligationID != rescheduleObligationID || resp.IdempotentReplay {
		t.Fatalf("response = %#v", resp)
	}
	if writer.lastRescheduleObl != rescheduleObligationID {
		t.Fatalf("writer obligation id = %q", writer.lastRescheduleObl)
	}
	if writer.lastRescheduleIdemKey != "idem-key-1" {
		t.Fatalf("writer idempotency key = %q", writer.lastRescheduleIdemKey)
	}
}

func TestRescheduleObligationMapsNotFoundTo404(t *testing.T) {
	writer := &fakeWriter{rescheduleErr: obligationports.ErrNotFound}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, writer))

	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"due_at":"` + future + `"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "idem-key-404", body))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d want 404 body=%s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != "not_found" {
		t.Fatalf("error code = %q want not_found", env.Code)
	}
}

func TestRescheduleObligationMapsIdempotencyConflictTo409(t *testing.T) {
	writer := &fakeWriter{rescheduleErr: obligationports.ErrIdempotencyConflict}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, writer))

	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"due_at":"` + future + `"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "idem-key-409", body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d want 409 body=%s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != "idempotency_conflict" {
		t.Fatalf("error code = %q want idempotency_conflict", env.Code)
	}
}

func TestRescheduleObligationValidatesRequest(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, "not-a-uuid", "idem-key", `{"due_at":"2027-01-01T00:00:00Z"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid obligation_id status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "idem-key", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing due_at status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "", `{"due_at":"2027-01-01T00:00:00Z"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status = %d want 400", rec.Code)
	}

	futureStart := time.Now().Add(48 * time.Hour).UTC()
	futureEndBeforeStart := futureStart.Add(-1 * time.Hour)
	body := `{"due_at":"` + futureStart.Format(time.RFC3339) + `","window_start":"` + futureStart.Format(time.RFC3339) + `","window_end":"` + futureEndBeforeStart.Format(time.RFC3339) + `"}`
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, buildRescheduleRequest(t, rescheduleObligationID, "idem-window", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid window status = %d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestVaccinationGapsParsesQueryAndResponds(t *testing.T) {
	reader := &fakeReader{gaps: domain.GapsResponse{
		Source:  domain.SourceAPI,
		Reasons: []domain.GapReasonSummary{{ReasonCode: domain.GapReasonNoDateOfBirth, ReasonLabel: "No date of birth", Count: 7}},
		Rows:    []domain.GapRow{{GoatID: "goat-1", DisplayID: "G-000001", ParkID: "park-1", ParkName: "CBE", ReasonCode: domain.GapReasonNoDateOfBirth, ReasonLabel: "No date of birth"}},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/app/vaccination/gaps?park_id=30000000-0000-4000-8000-000000000001&limit=9000", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastGaps.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant = %q", reader.lastGaps.TenantID)
	}
	if reader.lastGaps.ParkID == nil || *reader.lastGaps.ParkID != "30000000-0000-4000-8000-000000000001" {
		t.Fatalf("park id = %v", reader.lastGaps.ParkID)
	}
	if reader.lastGaps.Limit != maxExecutionLimit {
		t.Fatalf("limit = %d want %d (clamped)", reader.lastGaps.Limit, maxExecutionLimit)
	}
	var resp domain.GapsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Reasons) != 1 || resp.Reasons[0].Count != 7 {
		t.Fatalf("response reasons = %#v", resp.Reasons)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].DisplayID != "G-000001" {
		t.Fatalf("response rows = %#v", resp.Rows)
	}
}

func TestVaccinationGapsRejectsInvalidQuery(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/vaccination/gaps?park_id=not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid park_id status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/vaccination/gaps?cursor=not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/vaccination/gaps?limit=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d want 400", rec.Code)
	}
}

func TestVaccinationCoverageParsesQueryAndResponds(t *testing.T) {
	reader := &fakeReader{coverage: domain.CoverageResponse{
		Source:    domain.SourceAPI,
		Protocols: []domain.CoverageProtocol{{ProtocolID: "ppr", Name: "PPR", GivenCount: 10, TotalCount: 20, CoveragePercent: 50}},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/app/vaccination/coverage?park_id=30000000-0000-4000-8000-000000000001", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastCoverage.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant = %q", reader.lastCoverage.TenantID)
	}
	if reader.lastCoverage.ParkID == nil || *reader.lastCoverage.ParkID != "30000000-0000-4000-8000-000000000001" {
		t.Fatalf("park id = %v", reader.lastCoverage.ParkID)
	}
	var resp domain.CoverageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Protocols) != 1 || resp.Protocols[0].CoveragePercent != 50 {
		t.Fatalf("response protocols = %#v", resp.Protocols)
	}
}

func TestVaccinationCoverageRejectsInvalidQuery(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/vaccination/coverage?park_id=not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid park_id status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/vaccination/coverage?as_of=not-time", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid as_of status = %d want 400", rec.Code)
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

func TestGetCapacityConfig(t *testing.T) {
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 100, CapacityScope: "tenant", MaxBufferDays: 3, OverflowPolicy: "split_within_safe_window_then_mark_needs_review", RowVersion: 2}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/capacity-config", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	var resp domain.CapacityConfig
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.MaxPerDay != 100 || resp.MaxBufferDays != 3 || resp.RowVersion != 2 {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestUpdateCapacityConfigRouteIsNotRegistered(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))
	req := httptest.NewRequest(http.MethodPut, "/vaccination/capacity-config", strings.NewReader(`{"maxPerDay":150}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d want 405 body=%s", rec.Code, rec.Body.String())
	}
}
