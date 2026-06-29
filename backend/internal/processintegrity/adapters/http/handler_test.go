package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

const (
	handlerTenant = "00000000-0000-4000-8000-000000000001"
	handlerPark   = "70000000-0000-4000-8000-000000000001"
	handlerShed   = "70000000-0000-4000-8000-000000000002"
	handlerOwner  = "70000000-0000-4000-8000-000000000011"
	handlerVer    = "70000000-0000-4000-8000-000000000006"
	handlerRowID  = "batch:70000000-0000-4000-8000-000000000008:rule:70000000-0000-4000-8000-000000000007:shed:70000000-0000-4000-8000-000000000002"
)

type fakeReader struct {
	actionQuery domain.Query
	workflowID  string
}

func (f *fakeReader) ActionCenter(_ context.Context, q domain.Query) (domain.ActionCenterResponse, error) {
	f.actionQuery = q
	return domain.ActionCenterResponse{Source: domain.SourceAPI, Items: []domain.Row{}}, nil
}

func (f *fakeReader) ProtocolAdherence(_ context.Context, q domain.Query) (domain.ProtocolAdherenceResponse, error) {
	return domain.ProtocolAdherenceResponse{Source: domain.SourceAPI}, nil
}

func (f *fakeReader) ControlTower(_ context.Context, q domain.Query) (domain.ControlTowerResponse, error) {
	return domain.ControlTowerResponse{Source: domain.SourceAPI}, nil
}

func (f *fakeReader) WorkflowDrilldown(_ context.Context, _ domain.Query, rowID string) (domain.WorkflowDrilldownResponse, bool, error) {
	f.workflowID = rowID
	return domain.WorkflowDrilldownResponse{}, false, nil
}

func TestActionCenterParsesBoundedVaccinationQuery(t *testing.T) {
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader))

	cursor, err := domain.EncodeCursor(domain.Cursor{
		SortPriority: 5,
		DueAt:        time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC),
		RowID:        handlerRowID,
	})
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/vaccination/action-center?park_id="+handlerPark+
		"&shed_id="+handlerShed+
		"&work_state=verification_pending&severity=watch&owner_id="+handlerOwner+
		"&protocol_version_id="+handlerVer+
		"&due_after=2026-06-20T00:00:00Z&due_before=2026-07-01T00:00:00Z&limit=999&cursor="+cursor, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	q := reader.actionQuery
	if q.TenantID != handlerTenant || q.ParkID == nil || *q.ParkID != handlerPark || q.ShedID == nil || *q.ShedID != handlerShed {
		t.Fatalf("scope query = %+v", q)
	}
	if q.WorkState == nil || *q.WorkState != domain.WorkStateVerificationPending {
		t.Fatalf("work_state = %v", q.WorkState)
	}
	if q.Severity == nil || *q.Severity != domain.SeverityWatch {
		t.Fatalf("severity = %v", q.Severity)
	}
	if q.OwnerID == nil || *q.OwnerID != handlerOwner || q.ProtocolVersionID == nil || *q.ProtocolVersionID != handlerVer {
		t.Fatalf("owner/version = %v/%v", q.OwnerID, q.ProtocolVersionID)
	}
	if q.DueAfter == nil || !q.DueBefore.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("due window = %v..%v", q.DueAfter, q.DueBefore)
	}
	if q.Limit != maxLimit {
		t.Fatalf("limit = %d want %d", q.Limit, maxLimit)
	}
	if q.Cursor == nil || q.Cursor.SortPriority != 5 {
		t.Fatalf("cursor = %+v", q.Cursor)
	}
}

func TestActionCenterParsesAsOfAndReanchorsHorizon(t *testing.T) {
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader))

	// as_of provided, no due_before: the horizon default must re-anchor to as_of (not wall-clock now).
	req := httptest.NewRequest(http.MethodGet, "/vaccination/action-center?as_of=2026-06-24T12:00:00Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if !reader.actionQuery.AsOf.Equal(asOf) {
		t.Fatalf("as_of not parsed into query: got %v want %v", reader.actionQuery.AsOf, asOf)
	}
	if !reader.actionQuery.DueBefore.Equal(asOf.Add(defaultHorizonDays * 24 * time.Hour)) {
		t.Fatalf("due_before should re-anchor to as_of+horizon, got %v", reader.actionQuery.DueBefore)
	}

	// Explicit due_before still wins over the re-anchored default.
	req = httptest.NewRequest(http.MethodGet, "/vaccination/action-center?as_of=2026-06-24T12:00:00Z&due_before=2026-06-30T00:00:00Z", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !reader.actionQuery.DueBefore.Equal(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("explicit due_before must win, got %v", reader.actionQuery.DueBefore)
	}

	// Malformed as_of is a 400.
	req = httptest.NewRequest(http.MethodGet, "/vaccination/action-center?as_of=2026-06-24", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed as_of status = %d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestActionCenterRejectsMalformedOrOversizedCursor(t *testing.T) {
	tests := map[string]string{
		"bad_base64":   "not+url+base64",
		"bad_shape":    base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"batch:missing"}`)),
		"unknown_key":  base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"` + handlerRowID + `","extra":true}`)),
		"oversized":    strings.Repeat("a", domain.MaxCursorLength+1),
		"huge_payload": base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"` + strings.Repeat("a", domain.MaxRowIDLength+1) + `"}`)),
	}
	for name, cursor := range tests {
		t.Run(name, func(t *testing.T) {
			reader := &fakeReader{}
			mux := http.NewServeMux()
			Register(mux, NewHandler(reader))

			req := httptest.NewRequest(http.MethodGet, "/vaccination/action-center?cursor="+cursor, nil)
			req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if reader.actionQuery.TenantID != "" {
				t.Fatalf("reader was called for invalid cursor: %+v", reader.actionQuery)
			}
			var body errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if body.Code != "invalid_cursor" {
				t.Fatalf("error code = %q", body.Code)
			}
		})
	}
}

func TestActionCenterRejectsInvalidWorkState(t *testing.T) {
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader))

	req := httptest.NewRequest(http.MethodGet, "/action-center/obligations?work_state=not_a_state", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Code != "invalid_work_state" {
		t.Fatalf("error code = %q", body.Code)
	}
}

func TestActionCenterAcceptsMissedWorkState(t *testing.T) {
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader))

	req := httptest.NewRequest(http.MethodGet, "/action-center/obligations?work_state=missed", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.actionQuery.WorkState == nil || *reader.actionQuery.WorkState != domain.WorkStateMissed {
		t.Fatalf("work_state = %v", reader.actionQuery.WorkState)
	}
}

func TestWorkflowDrilldownReturnsNotFoundForMissingVaccinationRow(t *testing.T) {
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/workflows/"+handlerRowID, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.workflowID != handlerRowID {
		t.Fatalf("workflow row id = %q", reader.workflowID)
	}
}

func TestWorkflowDrilldownRejectsMalformedOrOversizedRowID(t *testing.T) {
	tests := map[string]string{
		"bad_shape":  "batch:missing",
		"wrong_lens": "feed:70000000-0000-4000-8000-000000000008",
		"oversized":  "batch:" + strings.Repeat("a", domain.MaxRowIDLength),
	}
	for name, rowID := range tests {
		t.Run(name, func(t *testing.T) {
			reader := &fakeReader{}
			mux := http.NewServeMux()
			Register(mux, NewHandler(reader))

			req := httptest.NewRequest(http.MethodGet, "/vaccination/workflows/"+rowID, nil)
			req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), handlerTenant))
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if reader.workflowID != "" {
				t.Fatalf("reader was called for invalid row id %q", reader.workflowID)
			}
			var body errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if body.Code != "invalid_row_id" {
				t.Fatalf("error code = %q", body.Code)
			}
		})
	}
}
