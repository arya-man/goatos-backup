package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type fakeImpact struct {
	got      domain.ImpactRequest
	preview  domain.ImpactPreview
	queue    []domain.RecordedCompletion
	gotLimit int32
}

func (f *fakeImpact) ImpactPreview(_ context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error) {
	f.got = req
	return f.preview, nil
}

func (f *fakeImpact) VerificationQueue(_ context.Context, _ string, _ string, limit int32) ([]domain.RecordedCompletion, error) {
	f.gotLimit = limit
	return f.queue, nil
}

func TestImpactPreviewParsesFilterAndReturnsJSON(t *testing.T) {
	fake := &fakeImpact{preview: domain.ImpactPreview{
		EligibleGoats: 12, CatchupGoats: 3, Obligations: 12, Batches: 2, DosesRequired: 12,
		DosesAvailable: "50", Warnings: nil,
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake, nil))

	body := `{"stage":"K1","sex":"female","doses_per_goat":1,"dose_rows":1,"vaccine_item_id":"item-1"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/protocols/vaccination/impact-preview", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.got.Filter.Stage != "K1" || fake.got.Filter.Sex != "female" || fake.got.DosesPerGoat != 1 {
		t.Fatalf("filter/inputs not parsed: %+v", fake.got)
	}
	if fake.got.VaccineItemID == nil || *fake.got.VaccineItemID != "item-1" {
		t.Fatalf("vaccine_item_id not parsed: %+v", fake.got.VaccineItemID)
	}
	var resp impactPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if resp.EligibleGoats != 12 || resp.Batches != 2 || resp.DosesAvailable != "50" {
		t.Fatalf("response body: %+v", resp)
	}
	if resp.Warnings == nil {
		t.Fatalf("warnings must serialize as [] not null")
	}
}

func TestImpactPreviewRejectsBadJSON(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/protocols/vaccination/impact-preview", strings.NewReader("{not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad json, got %d", rec.Code)
	}
}

type fakeVerifier struct {
	acceptedID string
	rejectedID string
	reason     string
}

func (f *fakeVerifier) AcceptExisting(_ context.Context, in app.AcceptExistingInput) (app.AcceptResult, error) {
	f.acceptedID = in.CompletionID
	return app.AcceptResult{CompletionID: in.CompletionID, Applied: true, Completed: true}, nil
}
func (f *fakeVerifier) RejectExisting(_ context.Context, _, completionID, reason string, _ *string) (app.RejectResult, error) {
	f.rejectedID, f.reason = completionID, reason
	return app.RejectResult{CompletionID: completionID, Applied: true}, nil
}

func TestAcceptRejectEndpoints(t *testing.T) {
	fv := &fakeVerifier{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, fv))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/vaccination/completions/c1/accept", nil))
	if rec.Code != http.StatusOK || fv.acceptedID != "c1" {
		t.Fatalf("accept: code=%d acceptedID=%s", rec.Code, fv.acceptedID)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/vaccination/completions/c2/reject", strings.NewReader(`{"reason":"rework_requested"}`)))
	if rec.Code != http.StatusOK || fv.rejectedID != "c2" || fv.reason != "rework_requested" {
		t.Fatalf("reject: code=%d rejectedID=%s reason=%s", rec.Code, fv.rejectedID, fv.reason)
	}
}

func TestVerifyUnavailableWhenNotWired(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/vaccination/completions/c1/accept", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when verify not wired, got %d", rec.Code)
	}
}

func TestVerificationQueueShapeAndLimit(t *testing.T) {
	fake := &fakeImpact{queue: []domain.RecordedCompletion{
		{CompletionID: "c1", GoatID: "g1", Doses: 1},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake, nil))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?limit=9000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotLimit != 500 {
		t.Fatalf("limit must clamp to 500, got %d", fake.gotLimit)
	}
	var resp queueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Items) != 1 || resp.Items[0].CompletionID != "c1" {
		t.Fatalf("queue response: %+v err=%v", resp, err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?limit=-3", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: want 400, got %d", rec.Code)
	}
}
