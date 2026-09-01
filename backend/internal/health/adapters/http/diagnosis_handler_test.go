package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeDiagnosisService struct {
	submitted  domain.SubmitObservationInput
	confirmed  domain.ConfirmDiagnosisInput
	submitErr  error
	confirmErr error

	listed  domain.DiagnosisQueueFilter
	listErr error
}

func (f *fakeDiagnosisService) SubmitObservation(_ context.Context, in domain.SubmitObservationInput) (domain.SubmitObservationResult, error) {
	f.submitted = in
	if f.submitErr != nil {
		return domain.SubmitObservationResult{}, f.submitErr
	}
	return domain.SubmitObservationResult{DiagnosisRunID: "run-1", Status: domain.DiagnosisStatusProposed}, nil
}

func (f *fakeDiagnosisService) ConfirmDiagnosis(_ context.Context, in domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error) {
	f.confirmed = in
	if f.confirmErr != nil {
		return domain.ConfirmDiagnosisResult{}, f.confirmErr
	}
	return domain.ConfirmDiagnosisResult{DiagnosisRunID: in.DiagnosisRunID, Status: domain.DiagnosisStatusConfirmed}, nil
}

func (f *fakeDiagnosisService) GetDiagnosisRun(context.Context, string, string) (domain.DiagnosisRun, error) {
	return domain.DiagnosisRun{DiagnosisRunID: "run-1"}, nil
}

func (f *fakeDiagnosisService) ListDiagnosisRuns(_ context.Context, in domain.DiagnosisQueueFilter) (domain.DiagnosisQueuePage, error) {
	f.listed = in
	if f.listErr != nil {
		return domain.DiagnosisQueuePage{}, f.listErr
	}
	return domain.DiagnosisQueuePage{Items: []domain.DiagnosisQueueItem{{DiagnosisRunID: "run-1"}}}, nil
}

// queueRequest is a tenant-scoped GET on the queue.
func queueRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
}

func submitRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/app/health/observations", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "obs-1")
	return req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
}

// Both writes require an idempotency key. Without one a network-failed retry
// would submit a second observation or open a second course.
func TestDiagnosisWritesRequireAnIdempotencyKey(t *testing.T) {
	h := NewDiagnosisHandler(&fakeDiagnosisService{}, nil)

	t.Run("submit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/app/health/observations",
			strings.NewReader(`{"goat_id":"30000000-0000-4000-8000-000000000001"}`))
		w := httptest.NewRecorder()
		h.SubmitObservation(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("confirm", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/app/health/observations/run-1/confirm",
			strings.NewReader(`{"confirmed_problems":["FEVER"]}`))
		w := httptest.NewRecorder()
		h.ConfirmDiagnosis(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

// The form is decoded strictly. A misspelled finding key must fail loudly rather
// than be dropped: a silently discarded observation is a finding the manager
// recorded and the engine never saw.
func TestObservationRejectsUnknownFields(t *testing.T) {
	h := NewDiagnosisHandler(&fakeDiagnosisService{}, nil)
	w := httptest.NewRecorder()
	h.SubmitObservation(w, submitRequest(`{"goat_id":"30000000-0000-4000-8000-000000000001","findings":{"tempp":104.8}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a misspelled finding must be rejected, got %d: %s", w.Code, w.Body.String())
	}
}

// The engine's answer depends on the animal's own facts, so the request carries
// the goat id and nothing else about the animal. This pins that the handler
// forwards no client-supplied animal description.
func TestObservationCarriesOnlyTheGoatID(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	w := httptest.NewRecorder()
	h.SubmitObservation(w, submitRequest(`{"goat_id":"30000000-0000-4000-8000-000000000001","findings":{"temp":104.8}}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if svc.submitted.GoatID != "30000000-0000-4000-8000-000000000001" {
		t.Errorf("goat id not forwarded: %q", svc.submitted.GoatID)
	}
	if svc.submitted.Findings.Temp == nil || *svc.submitted.Findings.Temp != 104.8 {
		t.Errorf("findings not forwarded: %+v", svc.submitted.Findings)
	}
	if svc.submitted.IdempotencyKey != "obs-1" {
		t.Errorf("idempotency key not forwarded: %q", svc.submitted.IdempotencyKey)
	}
}

// Each failure maps to a status the app can act on. The one that matters most is
// a missing treatment card: it is neither a client mistake nor a server fault,
// so it answers 422 with a code the app can turn into "no plan set up yet".
func TestDiagnosisErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{"missing treatment card", ports.ErrSOPNotAuthored, http.StatusUnprocessableEntity, "treatment_plan_missing"},
		{"unproposed diagnosis", ports.ErrDiagnosisNotProposed, http.StatusUnprocessableEntity, "diagnosis_not_proposed"},
		{"already decided", ports.ErrDiagnosisAlreadyDecided, http.StatusConflict, "diagnosis_already_decided"},
		{"idempotency conflict", ports.ErrConflict, http.StatusConflict, "idempotency_conflict"},
		{"run not found", ports.ErrNotFound, http.StatusNotFound, "not_found"},
		{"invalid input", healthapp.ErrInvalidInput, http.StatusBadRequest, "invalid_request"},
		{"goat cannot be resolved", domain.ErrGoatNotDiagnosable, http.StatusUnprocessableEntity, "goat_not_diagnosable"},
		{"goat not alive", ports.ErrGoatNotAlive, http.StatusConflict, "goat_not_alive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewDiagnosisHandler(&fakeDiagnosisService{confirmErr: tc.err}, nil)
			req := httptest.NewRequest(http.MethodPost, "/app/health/observations/run-1/confirm",
				strings.NewReader(`{"confirmed_problems":["FEVER"]}`))
			req.Header.Set("Idempotency-Key", "confirm-1")
			w := httptest.NewRecorder()
			h.ConfirmDiagnosis(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantCode, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body["code"] != tc.wantBody {
				t.Errorf("code = %v, want %q", body["code"], tc.wantBody)
			}
		})
	}
}

// Confirming nothing is a legitimate override, not a malformed request.
func TestConfirmAcceptsAnEmptyDecision(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	req := httptest.NewRequest(http.MethodPost, "/app/health/observations/run-1/confirm",
		strings.NewReader(`{"confirmed_problems":[]}`))
	req.Header.Set("Idempotency-Key", "confirm-1")
	w := httptest.NewRecorder()
	h.ConfirmDiagnosis(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("declining everything is legal, got %d: %s", w.Code, w.Body.String())
	}
	if len(svc.confirmed.ConfirmedProblems) != 0 {
		t.Errorf("expected an empty decision, got %v", svc.confirmed.ConfirmedProblems)
	}
}

// The queue defaults to work still awaiting a decision. That default lives in the
// SERVICE, so the handler must pass a blank status through rather than inventing
// one -- two defaults in two layers drift.
func TestQueueRequestPassesFiltersThroughVerbatim(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	mux := http.NewServeMux()
	RegisterDiagnosis(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, queueRequest("/app/health/observations?status=confirmed&goat_id=g-1&cursor=abc&limit=7"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.listed.Status != "confirmed" {
		t.Errorf("status = %q, want it passed through", svc.listed.Status)
	}
	if svc.listed.GoatID != "g-1" || svc.listed.Cursor != "abc" || svc.listed.Limit != 7 {
		t.Errorf("filters not passed through: %+v", svc.listed)
	}
}

func TestQueueRequestLeavesTheStatusDefaultToTheService(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	mux := http.NewServeMux()
	RegisterDiagnosis(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, queueRequest("/app/health/observations"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.listed.Status != "" {
		t.Errorf("the handler must not default the status, got %q", svc.listed.Status)
	}
	if svc.listed.Limit != 0 {
		t.Errorf("the handler must not default the limit, got %d", svc.listed.Limit)
	}
}

// A non-numeric limit is a client mistake and is named as one. Silently falling
// back to the default would serve a page the caller did not ask for.
func TestQueueRejectsANonNumericLimit(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	mux := http.NewServeMux()
	RegisterDiagnosis(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, queueRequest("/app/health/observations?limit=many"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// The LIST and the SUBMIT share a path and are split only by method. A mux that
// resolves both to the same handler would make every queue read a write.
func TestQueueListAndSubmitAreDistinctRoutes(t *testing.T) {
	svc := &fakeDiagnosisService{}
	h := NewDiagnosisHandler(svc, nil)
	mux := http.NewServeMux()
	RegisterDiagnosis(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, queueRequest("/app/health/observations"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET must list, got %d", rec.Code)
	}
	if svc.submitted.GoatID != "" {
		t.Error("a queue read must never reach the submit path")
	}
}
