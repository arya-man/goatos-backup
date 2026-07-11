package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccports "github.com/vgoats/goatos/backend/internal/vaccination/ports"
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

type fakeCampaign struct {
	tenantID   string
	versionID  string
	campaignID string
	asOf       time.Time
	key        string
	hash       string
	err        error
}

func (f *fakeCampaign) GenerateManualCampaignForVersionWithHTTPRun(_ context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error) {
	f.tenantID = tenantID
	f.versionID = versionID
	f.campaignID = campaignID
	f.asOf = asOf
	f.key = idempotencyKey
	f.hash = requestHash
	if f.err != nil {
		return domain.GenerationRun{}, domain.GenerateResult{}, f.err
	}
	completedAt := time.Date(2026, time.June, 27, 9, 0, 0, 0, time.UTC)
	return domain.GenerationRun{
			RunID:                      "70000000-0000-4000-8000-000000000001",
			ProtocolVersionID:          versionID,
			TriggerType:                "manual_campaign",
			TriggerRef:                 campaignID,
			Status:                     "completed",
			StartedAt:                  completedAt.Add(-time.Minute),
			CompletedAt:                &completedAt,
			Generated:                  4,
			Deferred:                   1,
			Reopened:                   3,
			FailedGoats:                1,
			SuppressedByTrustedHistory: 2,
		}, domain.GenerateResult{
			Generated:                  4,
			Deferred:                   1,
			Reopened:                   3,
			FailedGoats:                1,
			SuppressedByTrustedHistory: 2,
		}, nil
}

func TestRunManualCampaignCallsGenerator(t *testing.T) {
	campaign := &fakeCampaign{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil).WithManualCampaignGenerator(campaign))

	body := `{"protocol_version_id":"65000000-0000-4000-8000-000000000001","campaign_id":"catchup:2026-06-27","as_of":"2026-06-27T08:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/manual-campaigns", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "manual-campaign-test-0001")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if campaign.tenantID != "00000000-0000-4000-8000-000000000001" || campaign.versionID != "65000000-0000-4000-8000-000000000001" || campaign.campaignID != "catchup:2026-06-27" {
		t.Fatalf("campaign generator got tenant=%s version=%s campaign=%s", campaign.tenantID, campaign.versionID, campaign.campaignID)
	}
	if campaign.asOf.Format(time.RFC3339) != "2026-06-27T13:30:00+05:30" {
		t.Fatalf("as_of = %s", campaign.asOf.Format(time.RFC3339))
	}
	if campaign.key != "manual-campaign-test-0001" || campaign.hash == "" {
		t.Fatalf("idempotency not passed key=%q hash=%q", campaign.key, campaign.hash)
	}
	var resp manualCampaignRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if resp.RunID == "" || resp.TriggerType != "manual_campaign" || resp.Reopened != 3 || resp.ResultReopened != 3 || resp.FailedGoats != 1 || resp.ResultFailedGoats != 1 || resp.ResultGenerated != 4 || resp.ResultSuppressedByTrustedHistory != 2 {
		t.Fatalf("response body: %+v", resp)
	}
}

func TestRunManualCampaignIdempotencyConflictReturns409(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil).WithManualCampaignGenerator(&fakeCampaign{err: vaccports.ErrIdempotencyConflict}))

	body := `{"protocol_version_id":"65000000-0000-4000-8000-000000000001","campaign_id":"catchup:2026-06-27"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/manual-campaigns", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "manual-campaign-test-0001")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "idempotency_conflict") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRunManualCampaignDoesNotTreatWrappedConflictAsInternal(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil).WithManualCampaignGenerator(&fakeCampaign{err: errors.Join(vaccports.ErrIdempotencyConflict)}))

	body := `{"protocol_version_id":"65000000-0000-4000-8000-000000000001","campaign_id":"catchup:2026-06-27"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/manual-campaigns", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "manual-campaign-test-0002")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestRunManualCampaignRequiresIdempotencyKey(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil).WithManualCampaignGenerator(&fakeCampaign{}))

	body := `{"protocol_version_id":"65000000-0000-4000-8000-000000000001","campaign_id":"catchup:2026-06-27"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/manual-campaigns", strings.NewReader(body))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestImpactPreviewParsesFilterAndReturnsJSON(t *testing.T) {
	fake := &fakeImpact{preview: domain.ImpactPreview{
		EligibleAnimals: 12, VaccinationCells: 24, AffectedSheds: 2, EstimatedDays: 1, DailyCap: 100,
		DosesAvailable: "50", SourceRevision: 1700000000000, Warnings: nil,
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake, nil))

	body := `{"species":"goat","stage":"K1","sex":"female","health":"healthy","dose_rows":2,"vaccine_item_id":"item-1"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/protocols/vaccination/impact-preview", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.got.Filter.Species != "goat" || fake.got.Filter.Stage != "K1" || fake.got.Filter.Sex != "female" || fake.got.Filter.Health != "healthy" || fake.got.DoseRows != 2 {
		t.Fatalf("filter/inputs not parsed: %+v", fake.got)
	}
	if fake.got.VaccineItemID == nil || *fake.got.VaccineItemID != "item-1" {
		t.Fatalf("vaccine_item_id not parsed: %+v", fake.got.VaccineItemID)
	}
	var resp impactPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if resp.EligibleAnimals != 12 || resp.VaccinationCells != 24 || resp.AffectedSheds != 2 || resp.EstimatedDays != 1 || resp.DailyCap != 100 || resp.DosesAvailable != "50" || resp.SourceRevision != 1700000000000 {
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

func TestDirectCompletionReviewEndpointsAreNotMounted(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/vaccination/completions/c1/accept", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("direct accept route should not be mounted, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/vaccination/completions/c2/reject", strings.NewReader(`{"reason":"rework_requested"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("direct reject route should not be mounted, got %d", rec.Code)
	}
}

func TestVerificationQueueShapeAndLimit(t *testing.T) {
	fake := &fakeImpact{queue: []domain.RecordedCompletion{
		{CompletionID: "c1", GoatID: "g1", SOPTaskID: "task-1", SOPTaskVersion: 7, Doses: 1},
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Items) != 1 || resp.Items[0].CompletionID != "c1" || resp.Items[0].SOPTaskID != "task-1" || resp.Items[0].SOPTaskVersion != 7 {
		t.Fatalf("queue response: %+v err=%v", resp, err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?limit=-3", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: want 400, got %d", rec.Code)
	}
}
