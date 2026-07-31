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
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeReader struct {
	operatorCfg         *vaccexecapp.OperatorAssignmentConfigView
	lastOperatorCfgPark string
	parks               []domain.ParkOption
	parksErr            error

	rows          []domain.ExecutionRow
	executionPage domain.ExecutionResponse
	detail        domain.ShedDrilldown
	found         bool
	last          domain.ExecutionQuery
	ops           domain.OperationsResponse
	lastOps       domain.OperationsQuery
	schedule      domain.OperationsResponse
	lastSchedule  domain.ScheduleQuery
	scheduleErr   error
	assignments   domain.DriveAssignmentResponse
	lastAssign    domain.DriveAssignmentQuery
	roster        []domain.ScanRosterRow
	lastRoster    domain.ScanRosterQuery
	rosterNext    *domain.ScanRosterCursor
	gaps          domain.GapsResponse
	lastGaps      domain.GapsQuery
	coverage      domain.CoverageResponse
	lastCoverage  domain.OperationsQuery
	shedSummary   domain.ShedSummaryResponse
	lastShedSum   domain.ShedSummaryQuery
	shedDetail    domain.ShedDetailResponse
	shedFound     bool
	lastShedID    string
	shedAnimals   domain.ShedAnimalPage
	lastShedAnim  domain.ShedAnimalQuery
	capacityCfg   domain.CapacityConfig
	optionValues  domain.TaskOptionValuesResponse
}

func (f *fakeReader) VaccinationCommandBoard(_ context.Context, _ domain.CommandBoardQuery) (domain.CommandBoardResponse, error) {
	return domain.CommandBoardResponse{}, nil
}

type fakeWriter struct {
	rescheduleID          string
	rescheduleReplay      bool
	rescheduleErr         error
	lastRescheduleTenant  string
	lastRescheduleObl     string
	lastRescheduleIdemKey string
	lastAuthorizedParks   []string
	lastOverride          *obligationdomain.VaccineDriveDateOverride
}

func (f *fakeReader) VaccinationOperations(_ context.Context, q domain.OperationsQuery) (domain.OperationsResponse, error) {
	f.lastOps = q
	return f.ops, nil
}

func (f *fakeReader) VaccinationSchedule(_ context.Context, q domain.ScheduleQuery) (domain.OperationsResponse, error) {
	f.lastSchedule = q
	if f.scheduleErr != nil {
		return domain.OperationsResponse{}, f.scheduleErr
	}
	return f.schedule, nil
}

func (f *fakeReader) DriveAssignments(_ context.Context, q domain.DriveAssignmentQuery) (domain.DriveAssignmentResponse, error) {
	f.lastAssign = q
	return f.assignments, nil
}

func (f *fakeReader) VaccinationExecution(_ context.Context, q domain.ExecutionQuery) ([]domain.ExecutionRow, error) {
	f.last = q
	return f.rows, nil
}

func (f *fakeReader) VaccinationExecutionPage(_ context.Context, q domain.ExecutionQuery) (domain.ExecutionResponse, error) {
	f.last = q
	if f.executionPage.Source != "" {
		return f.executionPage, nil
	}
	return domain.ExecutionResponse{Source: domain.SourceAPI, Rows: f.rows, TotalCount: int64(len(f.rows))}, nil
}

func (f *fakeReader) ShedDrilldown(_ context.Context, q domain.ExecutionQuery) (domain.ShedDrilldown, bool, error) {
	f.last = q
	return f.detail, f.found, nil
}

func (f *fakeReader) ScanRoster(_ context.Context, q domain.ScanRosterQuery) (domain.ScanRosterResult, error) {
	f.lastRoster = q
	return domain.ScanRosterResult{Rows: f.roster, NextCursor: f.rosterNext}, nil
}

func (f *fakeReader) TaskOptionValues(context.Context, string, string) (domain.TaskOptionValuesResponse, error) {
	return f.optionValues, nil
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

func (f *fakeReader) GetOperatorAssignmentConfig(_ context.Context, _, parkID string) (vaccexecapp.OperatorAssignmentConfigView, error) {
	f.lastOperatorCfgPark = parkID
	if f.operatorCfg == nil {
		return vaccexecapp.OperatorAssignmentConfigView{}, vaccexecapp.ErrOperatorAssignmentConfigNotFound
	}
	return *f.operatorCfg, nil
}

func (f *fakeReader) AuthorizedParkOptions(_ context.Context, _ string, parkIDs []string) ([]domain.ParkOption, error) {
	if f.parksErr != nil {
		return nil, f.parksErr
	}
	if len(parkIDs) == 0 {
		return f.parks, nil
	}
	allowed := make(map[string]bool, len(parkIDs))
	for _, id := range parkIDs {
		allowed[id] = true
	}
	var out []domain.ParkOption
	for _, p := range f.parks {
		if allowed[p.ParkID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (w *fakeWriter) ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *obligationdomain.RecoveryReschedule) (string, bool, error) {
	return "obligation-id", false, nil
}

func (w *fakeWriter) RescheduleObligationByID(ctx context.Context, tenantID, obligationID, idempotencyKey string, authorizedParkIDs []string, dueAt, windowStart time.Time, windowEnd *time.Time, occurredAt time.Time) (string, bool, error) {
	w.lastRescheduleTenant = tenantID
	w.lastRescheduleObl = obligationID
	w.lastRescheduleIdemKey = idempotencyKey
	w.lastAuthorizedParks = append([]string(nil), authorizedParkIDs...)
	if w.rescheduleErr != nil {
		return "", false, w.rescheduleErr
	}
	id := w.rescheduleID
	if id == "" {
		id = obligationID
	}
	return id, w.rescheduleReplay, nil
}

func (w *fakeWriter) UpsertVaccinationDriveDateOverride(ctx context.Context, override obligationdomain.VaccineDriveDateOverride) (*obligationdomain.VaccineDriveDateOverride, error) {
	w.lastOverride = &override
	return &override, nil
}

func TestUpsertDriveDateOverrideRequiresActorAndPostpone(t *testing.T) {
	const testTenantID = "00000000-0000-4000-8000-000000000001"
	writer := &fakeWriter{}
	h := NewHandler(&fakeReader{}, writer).WithClock(func() time.Time {
		return time.Date(2026, 7, 22, 9, 0, 0, 0, biztime.DefaultLocation())
	})
	body := `{"park_id":"20000000-0000-4000-8000-000000000001","vaccine_code":"PPR","original_drive_date":"2026-08-01","override_date":"2026-08-08","reason":"CEO postponement"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/schedule/drive-date-overrides", strings.NewReader(body))
	req = req.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), "30000000-0000-4000-8000-000000000077"))
	rec := httptest.NewRecorder()

	h.UpsertDriveDateOverride(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if writer.lastOverride == nil || writer.lastOverride.VaccineCode != "PPR" || writer.lastOverride.CreatedBy == "" {
		t.Fatalf("override not carried: %#v", writer.lastOverride)
	}

	revert := httptest.NewRequest(http.MethodPost, "/vaccination/schedule/drive-date-overrides", strings.NewReader(`{"park_id":"20000000-0000-4000-8000-000000000001","vaccine_code":"PPR","original_drive_date":"2026-08-08","override_date":"2026-08-08","reason":"restore"}`))
	revert = revert.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(revert.Context(), testTenantID), "30000000-0000-4000-8000-000000000077"))
	revertRec := httptest.NewRecorder()
	h.UpsertDriveDateOverride(revertRec, revert)
	if revertRec.Code != http.StatusOK {
		t.Fatalf("revert status=%d body=%s", revertRec.Code, revertRec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPost, "/vaccination/schedule/drive-date-overrides", strings.NewReader(`{"park_id":"20000000-0000-4000-8000-000000000001","vaccine_code":"PPR","original_drive_date":"2026-08-08","override_date":"2026-08-01","reason":"bad"}`))
	bad = bad.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(bad.Context(), testTenantID), "30000000-0000-4000-8000-000000000077"))
	badRec := httptest.NewRecorder()
	h.UpsertDriveDateOverride(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestListVaccinationExecutionParsesQueryAndResponds(t *testing.T) {
	next, err := domain.EncodeExecutionCursor(domain.ExecutionCursor{SortRank: 2, SortDueMicros: 123, SortRowKey: "park|shed|rule|batch"})
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeReader{executionPage: domain.ExecutionResponse{Source: domain.SourceAPI, Rows: []domain.ExecutionRow{sampleRow()}, TotalCount: 21, NextCursor: &next}}
	writer := &fakeWriter{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, writer))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution?park_id=30000000-0000-4000-8000-000000000001&work_state=missed&severity=at_risk&cursor="+next+"&due_before=2026-07-01T00:00:00Z&limit=9000", nil)
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
	if reader.last.Severity == nil || *reader.last.Severity != domain.SeverityAtRisk {
		t.Fatalf("severity = %v", reader.last.Severity)
	}
	if reader.last.Cursor == nil || reader.last.Cursor.SortRowKey != "park|shed|rule|batch" {
		t.Fatalf("cursor = %#v", reader.last.Cursor)
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
	if resp.TotalCount != 21 || resp.NextCursor == nil || *resp.NextCursor != next {
		t.Fatalf("pagination response = %#v", resp)
	}
}

func TestVaccinationExecutionDefaultsAndRejectsScopedPark(t *testing.T) {
	const tenantID = "00000000-0000-4000-8000-000000000001"
	const parkID = "30000000-0000-4000-8000-000000000001"
	reader := &fakeReader{executionPage: domain.ExecutionResponse{Source: domain.SourceAPI}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenantID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: parkID}})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if reader.last.ParkID == nil || *reader.last.ParkID != parkID {
		t.Fatalf("park scope = %+v, want %s", reader.last.ParkID, parkID)
	}

	req = httptest.NewRequest(http.MethodGet, "/vaccination/execution?park_id=30000000-0000-4000-8000-000000000099", nil)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
		t.Fatalf("body=%s, want park_scope_forbidden", rec.Body.String())
	}
}

func TestAppVaccinationExecutionRequiresAndCarriesOperatorScope(t *testing.T) {
	const tenantID = "00000000-0000-4000-8000-000000000001"
	const actorID = "30000000-0000-4000-8000-000000000077"
	reader := &fakeReader{executionPage: domain.ExecutionResponse{Source: domain.SourceAPI}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution", nil)
	missingReq = missingReq.WithContext(httpmiddleware.WithTenantID(missingReq.Context(), tenantID))
	mux.ServeHTTP(missing, missingReq)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing actor status = %d want 400 body=%s", missing.Code, missing.Body.String())
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution", nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	req = req.WithContext(ctx)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.last.OperatorScopeActorID != actorID {
		t.Fatalf("operator scope actor = %q want %q", reader.last.OperatorScopeActorID, actorID)
	}
}

// TestAppVaccinationExecutionLeadershipSkipsOperatorScope pins that a leadership principal
// (CEO/CXO, PC Director, Park Head) reading the APP execution route is NOT
// operator-assignment scoped: they get the park-scoped read-only oversight view of all
// sheds, unlike a field operator who only sees their own assigned work. Without this,
// operator scoping returns zero rows for a leader (they are not an assigned operator), which
// is why a CEO's drive -> sheds view was empty.
func TestAppVaccinationExecutionLeadershipSkipsOperatorScope(t *testing.T) {
	const tenantID = "00000000-0000-4000-8000-000000000001"
	const actorID = "90000000-0000-4000-8000-000000000104"
	const parkID = "30000000-0000-4000-8000-000000000001"
	cases := []struct {
		role           string
		grant          permissions.ActiveGrant
		wantViewerOnly bool
	}{
		{permissions.RoleCEOInternal, permissions.ActiveGrant{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenantID}, true},
		{permissions.RolePCDirector, permissions.ActiveGrant{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: tenantID}, false},
		{permissions.RoleParkHead, permissions.ActiveGrant{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: parkID}, true},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			reader := &fakeReader{executionPage: domain.ExecutionResponse{Source: domain.SourceAPI}}
			mux := http.NewServeMux()
			Register(mux, NewHandler(reader, &fakeWriter{}))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution", nil)
			ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
			ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{tc.grant})
			req = req.WithContext(ctx)
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
			}
			if reader.last.OperatorScopeActorID != "" {
				t.Fatalf("leadership operator scope actor = %q want empty (park-scoped oversight)", reader.last.OperatorScopeActorID)
			}
			var resp domain.ExecutionResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.ViewerReadOnly != tc.wantViewerOnly {
				t.Fatalf("leadership response viewerReadOnly = %v want %v", resp.ViewerReadOnly, tc.wantViewerOnly)
			}
		})
	}
}

func TestVaccinationScheduleParsesMonthWindow(t *testing.T) {
	reader := &fakeReader{schedule: domain.OperationsResponse{Source: domain.SourceAPI}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/schedule?park_id=30000000-0000-4000-8000-000000000001&year=2026&month=8&limit=9000", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastSchedule.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant = %q", reader.lastSchedule.TenantID)
	}
	if reader.lastSchedule.ParkID == nil || *reader.lastSchedule.ParkID != "30000000-0000-4000-8000-000000000001" {
		t.Fatalf("park = %v", reader.lastSchedule.ParkID)
	}
	if reader.lastSchedule.MonthStart.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("month start = %s", reader.lastSchedule.MonthStart)
	}
	if reader.lastSchedule.Limit != maxExecutionLimit {
		t.Fatalf("limit = %d want %d", reader.lastSchedule.Limit, maxExecutionLimit)
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

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?severity=critical", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid severity status = %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?cursor=bad", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d want 400", rec.Code)
	}
}

func TestScanRosterRequiresTaskIdentityAndReturnsCursor(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "30000000-0000-4000-8000-000000000077"
		shedID   = "30000000-0000-4000-8000-000000000001"
		taskID   = "40000000-0000-4000-8000-000000000001"
		goatID   = "50000000-0000-4000-8000-000000000001"
		oblID    = "60000000-0000-4000-8000-000000000001"
	)
	next := &domain.ScanRosterCursor{GoatID: goatID, ObligationID: oblID}
	reader := &fakeReader{
		roster: []domain.ScanRosterRow{{
			GoatID: goatID, ObligationID: oblID, TaskID: taskID,
			BatchID:      "70000000-0000-4000-8000-000000000001",
			SOPVersionID: "80000000-0000-4000-8000-000000000001", TaskRowVersion: 3,
		}},
		rosterNext: next,
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	missingScope := httptest.NewRecorder()
	missingScopeReq := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution/sheds/"+shedID+"/roster", nil)
	missingScopeReq = missingScopeReq.WithContext(httpmiddleware.WithTenantID(missingScopeReq.Context(), tenantID))
	mux.ServeHTTP(missingScope, missingScopeReq)
	if missingScope.Code != http.StatusBadRequest {
		t.Fatalf("missing operator scope status=%d want 400", missingScope.Code)
	}

	// task_id is OPTIONAL after operator auth: absent -> shed-wide roster (200), not 400.
	shedWide := httptest.NewRecorder()
	shedWideReq := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution/sheds/"+shedID+"/roster", nil)
	shedWideReq = shedWideReq.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(shedWideReq.Context(), tenantID), actorID))
	mux.ServeHTTP(shedWide, shedWideReq)
	if shedWide.Code != http.StatusOK {
		t.Fatalf("missing task_id (shed-wide) status=%d want 200", shedWide.Code)
	}

	// A MALFORMED task_id is still rejected.
	badTask := httptest.NewRecorder()
	badTaskReq := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution/sheds/"+shedID+"/roster?task_id=not-a-uuid", nil)
	badTaskReq = badTaskReq.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(badTaskReq.Context(), tenantID), actorID))
	mux.ServeHTTP(badTask, badTaskReq)
	if badTask.Code != http.StatusBadRequest {
		t.Fatalf("malformed task_id status=%d want 400", badTask.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution/sheds/"+shedID+"/roster?task_id="+taskID+"&limit=1", nil)
	req = req.WithContext(httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastRoster.TaskID != taskID || reader.lastRoster.ShedID != shedID {
		t.Fatalf("query=%#v", reader.lastRoster)
	}
	if reader.lastRoster.OperatorScopeActorID != actorID {
		t.Fatalf("operator scope actor=%q want %q", reader.lastRoster.OperatorScopeActorID, actorID)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["taskId"] != taskID {
		t.Fatalf("taskId=%#v want %q; response=%#v", body["taskId"], taskID, body)
	}
	nextCursor, ok := body["next_cursor"].(string)
	if !ok || nextCursor == "" {
		t.Fatalf("next_cursor=%#v, want non-empty snake_case cursor; response=%#v", body["next_cursor"], body)
	}
	if _, legacyPresent := body["nextCursor"]; legacyPresent {
		t.Fatalf("legacy nextCursor key must be absent; response=%#v", body)
	}
	decoded, err := domain.DecodeScanRosterCursor(nextCursor)
	if err != nil {
		t.Fatalf("decode next_cursor: %v", err)
	}
	if decoded != *next {
		t.Fatalf("decoded next_cursor=%#v want %#v", decoded, *next)
	}
}

func TestVaccinationOperationsParsesKeysetCursor(t *testing.T) {
	cursor, err := domain.EncodeOperationsCursor(domain.OperationsCursor{
		ParkID: "30000000-0000-4000-8000-000000000001",
		ShedID: "40000000-0000-4000-8000-000000000001",
		Stage:  "K1",
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	reader := &fakeReader{ops: domain.OperationsResponse{Source: domain.SourceAPI}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))
	req := httptest.NewRequest(http.MethodGet, "/vaccination/operations?limit=25&cursor="+cursor, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastOps.Limit != 25 || reader.lastOps.Cursor == nil || reader.lastOps.Cursor.Stage != "K1" {
		t.Fatalf("operations query = %#v", reader.lastOps)
	}
}

func TestVaccinationOperationsRejectsInvalidCursor(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/operations?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400 body=%s", rec.Code, rec.Body.String())
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
	asOf := time.Date(2026, time.July, 21, 23, 59, 59, 0, biztime.DefaultLocation())
	serverNow := asOf.Add(-time.Minute)
	reader := &fakeReader{
		shedFound:   true,
		shedDetail:  domain.ShedDetailResponse{ParkID: "30000000-0000-4000-8000-000000000001"},
		shedAnimals: domain.ShedAnimalPage{Rows: []domain.ShedAnimalRow{{GoatID: "g1", DisplayID: "G-1", Status: "due"}}},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}).WithClock(func() time.Time { return serverNow }))
	req := httptest.NewRequest(http.MethodGet, "/vaccination/sheds/30000000-0000-4000-8000-000000000009/animals?cursor=40000000-0000-4000-8000-000000000001&limit=50&as_of=2026-07-21T23:59:59%2B05:30&drive_due_date=2026-07-22", nil)
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
	if !reader.lastShedAnim.AsOf.Equal(asOf) {
		t.Errorf("animal as_of = %s, want %s", reader.lastShedAnim.AsOf, asOf)
	}
	if reader.lastShedAnim.DriveDueDate == nil || reader.lastShedAnim.DriveDueDate.Format("2006-01-02") != "2026-07-22" {
		t.Errorf("drive_due_date = %v, want 2026-07-22", reader.lastShedAnim.DriveDueDate)
	}
	if !reader.lastOps.AsOf.Equal(asOf) {
		t.Errorf("shed validation as_of = %s, want %s", reader.lastOps.AsOf, asOf)
	}
}

func TestShedDetailAndAnimalsRejectScopedParkMismatch(t *testing.T) {
	const tenantID = "00000000-0000-4000-8000-000000000001"
	const allowedPark = "30000000-0000-4000-8000-000000000001"
	reader := &fakeReader{
		shedFound:  true,
		shedDetail: domain.ShedDetailResponse{ParkID: "30000000-0000-4000-8000-000000000099"},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))
	ctx := httpmiddleware.WithTenantID(context.Background(), tenantID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: allowedPark}})

	for _, path := range []string{
		"/vaccination/sheds/30000000-0000-4000-8000-000000000009",
		"/vaccination/sheds/30000000-0000-4000-8000-000000000009/animals",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d (%s)", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
			t.Fatalf("%s: body=%s, want park_scope_forbidden", path, rec.Body.String())
		}
	}
}

func TestExecutionParsesAsOf(t *testing.T) {
	serverNow := time.Date(2026, 7, 11, 18, 15, 0, 0, biztime.DefaultLocation())
	reader := &fakeReader{rows: []domain.ExecutionRow{sampleRow()}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}).WithClock(func() time.Time { return serverNow }))

	// A future as_of clamps to now and flows into the query (the current view). admin-web sends the
	// inclusive IST end-of-day for "today", which is future until day end and clamps to now.
	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution?as_of=2027-01-01T00:00:00Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reader.last.AsOf.Equal(serverNow) {
		t.Fatalf("as_of not clamped into query: got %v want %v", reader.last.AsOf, serverNow)
	}

	// A prior-day as_of is a historical point-in-time request: an honest 400, never a misleading
	// current-view response. 06:00Z on 2026-07-10 is 11:30 IST the prior business day.
	assertHistoricalAsOfRejected(t, mux, "/vaccination/execution?as_of=2026-07-10T06:00:00Z")
	// VE-001 guard: an EARLIER-SAME-DAY instant is also historical (06:00Z = 11:30 IST, before
	// serverNow 18:15 IST) and must not slip through to return a misleading current snapshot.
	assertHistoricalAsOfRejected(t, mux, "/vaccination/execution?as_of=2026-07-11T06:00:00Z")

	// Malformed as_of is a 400.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/execution?as_of=2026-06-24", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed as_of status = %d want 400", rec.Code)
	}
}

// assertHistoricalAsOfRejected asserts a past as_of on a vaccination-execution read is a 400
// historical_as_of_unsupported (current-view-only contract), not a 200 misleading current snapshot.
func assertHistoricalAsOfRejected(t *testing.T, mux http.Handler, target string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%s status = %d want 400 body=%s", target, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "historical_as_of_unsupported") {
		t.Fatalf("%s body = %s, want historical_as_of_unsupported", target, rec.Body.String())
	}
}

func TestExecutionClampsFutureAsOfButShedDrilldownAllowsScheduleDate(t *testing.T) {
	serverNow := time.Date(2026, 7, 11, 18, 15, 0, 0, biztime.DefaultLocation())
	scheduleAsOf := time.Date(2026, 9, 2, 5, 29, 59, 0, biztime.DefaultLocation())
	reader := &fakeReader{
		rows:        []domain.ExecutionRow{sampleRow()},
		shedSummary: domain.ShedSummaryResponse{Source: domain.SourceAPI},
		shedDetail:  domain.ShedDetailResponse{Source: domain.SourceAPI},
		shedFound:   true,
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}).WithClock(func() time.Time { return serverNow }))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/execution?as_of=2026-09-01T23:59:59Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execution status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reader.last.AsOf.Equal(serverNow) {
		t.Fatalf("execution as_of = %s, want clamped server now %s", reader.last.AsOf, serverNow)
	}
	if !reader.last.DueBefore.Equal(serverNow.Add(defaultExecutionHorizonDays * 24 * time.Hour)) {
		t.Fatalf("execution due_before = %s, want clamped horizon", reader.last.DueBefore)
	}

	req = httptest.NewRequest(http.MethodGet, "/vaccination/sheds?as_of=2026-09-01T23:59:59Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("shed summary status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reader.lastShedSum.AsOf.Equal(serverNow) {
		t.Fatalf("shed summary as_of = %s, want clamped server now %s", reader.lastShedSum.AsOf, serverNow)
	}
	if !reader.lastShedSum.DueBefore.Equal(serverNow.Add(defaultExecutionHorizonDays * 24 * time.Hour)) {
		t.Fatalf("shed summary due_before = %s, want clamped horizon", reader.lastShedSum.DueBefore)
	}

	req = httptest.NewRequest(http.MethodGet, "/vaccination/sheds/30000000-0000-4000-8000-000000000009?as_of=2026-09-01T23:59:59Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("shed detail status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reader.lastOps.AsOf.Equal(scheduleAsOf) {
		t.Fatalf("shed detail as_of = %s, want selected schedule date %s", reader.lastOps.AsOf, scheduleAsOf)
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

func TestRescheduleObligationPassesParkScopeToWriter(t *testing.T) {
	const parkID = "86000000-0000-4000-8000-000000000701"
	writer := &fakeWriter{rescheduleID: rescheduleObligationID}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, writer))

	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	req := buildRescheduleRequest(t, rescheduleObligationID, "idem-key-scoped", `{"due_at":"`+future+`"}`)
	req = req.WithContext(httpmiddleware.WithAuthGrants(req.Context(), []permissions.ActiveGrant{{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkID,
	}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.lastAuthorizedParks) != 1 || writer.lastAuthorizedParks[0] != parkID {
		t.Fatalf("authorized parks = %#v want [%s]", writer.lastAuthorizedParks, parkID)
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
		Source: domain.SourceAPI,
		Rows:   []domain.GapRow{{GoatID: "goat-1", DisplayID: "G-000001", ParkID: "park-1", ParkName: "CBE", ReasonCode: domain.GapReasonNoDateOfBirth, ReasonLabel: "No date of birth"}},
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
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 100, CapacityScope: "tenant", MaxBufferDays: 3, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 2}}
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

// fakeCapacityConfigWriter is the test double for CapacityConfigWriter.
type fakeCapacityConfigWriter struct {
	updated  domain.CapacityConfig
	code     string
	message  string
	err      error
	lastCfg  domain.CapacityConfig
	lastCall bool
}

func (f *fakeCapacityConfigWriter) UpdateCapacityConfig(_ context.Context, _ string, cfg domain.CapacityConfig) (domain.CapacityConfig, string, string, error) {
	f.lastCfg = cfg
	f.lastCall = true
	if f.err != nil {
		return domain.CapacityConfig{}, "", "", f.err
	}
	if f.code != "" {
		return domain.CapacityConfig{}, f.code, f.message, nil
	}
	return f.updated, "", "", nil
}

func TestPutCapacityConfigWithoutWriterIs500(t *testing.T) {
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 200, CapacityScope: "tenant", MaxBufferDays: 7, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 1}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))
	req := httptest.NewRequest(http.MethodPut, "/vaccination/capacity-config", strings.NewReader(`{"maxPerDay":150,"rowVersion":1}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d want 500 body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutCapacityConfigSuccess(t *testing.T) {
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 200, CapacityScope: "tenant", MaxBufferDays: 7, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 1}}
	shots := 3
	writer := &fakeCapacityConfigWriter{updated: domain.CapacityConfig{MaxPerDay: 150, CapacityScope: "tenant", MaxBufferDays: 7, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 2, MaxShotsPerAnimalPerDrive: &shots}}
	h := NewHandler(reader, &fakeWriter{}).WithCapacityConfigWriter(writer)
	mux := http.NewServeMux()
	Register(mux, h)

	req := httptest.NewRequest(http.MethodPut, "/vaccination/capacity-config", strings.NewReader(`{"maxPerDay":150,"rowVersion":1,"maxShotsPerAnimalPerDrive":3}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if !writer.lastCall {
		t.Fatal("expected writer to be called")
	}
	if writer.lastCfg.MaxPerDay != 150 || writer.lastCfg.MaxShotsPerAnimalPerDrive == nil || *writer.lastCfg.MaxShotsPerAnimalPerDrive != 3 {
		t.Fatalf("lastCfg = %+v", writer.lastCfg)
	}
	var resp domain.CapacityConfig
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RowVersion != 2 {
		t.Fatalf("resp.RowVersion = %d want 2", resp.RowVersion)
	}
}

func TestPutCapacityConfigValidationRejected(t *testing.T) {
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 200, CapacityScope: "tenant", MaxBufferDays: 7, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 1}}
	writer := &fakeCapacityConfigWriter{code: "invalid_max_per_day", message: "max animals per operator per day must be between 1 and 100000"}
	h := NewHandler(reader, &fakeWriter{}).WithCapacityConfigWriter(writer)
	mux := http.NewServeMux()
	Register(mux, h)

	req := httptest.NewRequest(http.MethodPut, "/vaccination/capacity-config", strings.NewReader(`{"maxPerDay":0,"rowVersion":1}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutCapacityConfigConflict(t *testing.T) {
	reader := &fakeReader{capacityCfg: domain.CapacityConfig{MaxPerDay: 200, CapacityScope: "tenant", MaxBufferDays: 7, OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", RowVersion: 1}}
	writer := &fakeCapacityConfigWriter{err: vaccexecapp.ErrCapacityConfigConflict}
	h := NewHandler(reader, &fakeWriter{}).WithCapacityConfigWriter(writer)
	mux := http.NewServeMux()
	Register(mux, h)

	req := httptest.NewRequest(http.MethodPut, "/vaccination/capacity-config", strings.NewReader(`{"maxPerDay":150,"rowVersion":1}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d want 409 body=%s", rec.Code, rec.Body.String())
	}
}
