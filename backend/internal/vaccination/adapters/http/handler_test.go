package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type fakeImpact struct {
	got     domain.ImpactRequest
	preview domain.ImpactPreview
}

func (f *fakeImpact) ImpactPreview(_ context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error) {
	f.got = req
	return f.preview, nil
}

func TestImpactPreviewParsesFilterAndReturnsJSON(t *testing.T) {
	fake := &fakeImpact{preview: domain.ImpactPreview{
		EligibleGoats: 12, CatchupGoats: 3, Obligations: 12, Batches: 2, DosesRequired: 12,
		DosesAvailable: "50", Warnings: nil,
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake))

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
	Register(mux, NewHandler(&fakeImpact{}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/protocols/vaccination/impact-preview", strings.NewReader("{not json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad json, got %d", rec.Code)
	}
}
