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
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccports "github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

type fakeImpact struct {
	got           domain.ImpactRequest
	preview       domain.ImpactPreview
	queue         domain.RecordedCompletionPage
	gotLimit      int32
	gotCursor     *domain.RecordedCompletionCursor
	gotQueuePark  string
	reviewItems   []domain.StageReviewItem
	resolveResult bool
	resolveErr    error
	resolvedID    string
	resolvedBy    string
}

func (f *fakeImpact) ImpactPreview(_ context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error) {
	f.got = req
	return f.preview, nil
}

func (f *fakeImpact) VerificationQueue(_ context.Context, _ string, parkID string, cursor *domain.RecordedCompletionCursor, limit int32) (domain.RecordedCompletionPage, error) {
	f.gotLimit = limit
	f.gotCursor = cursor
	f.gotQueuePark = parkID
	return f.queue, nil
}

func (f *fakeImpact) ListOpenStageReviewItems(_ context.Context, _ string, _ *domain.StageReviewItemCursor, _ int) (domain.StageReviewItemPage, error) {
	return domain.StageReviewItemPage{Items: f.reviewItems}, nil
}

func (f *fakeImpact) ResolveStageReviewItem(_ context.Context, _, reviewItemID, resolvedBy, _, _ string, _ time.Time) (bool, error) {
	f.resolvedID = reviewItemID
	f.resolvedBy = resolvedBy
	return f.resolveResult, f.resolveErr
}

type fakeCampaign struct {
	tenantID   string
	versionID  string
	campaignID string
	asOf       time.Time
	key        string
	hash       string
	called     bool
	err        error
}

func (f *fakeCampaign) GenerateManualCampaignForVersionWithHTTPRun(_ context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error) {
	f.called = true
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

func TestRunManualCampaignRejectsFutureAsOf(t *testing.T) {
	campaign := &fakeCampaign{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeImpact{}, nil).WithManualCampaignGenerator(campaign))

	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"protocol_version_id":"65000000-0000-4000-8000-000000000001","campaign_id":"catchup:2026-06-27","as_of":"` + future + `"}`
	req := httptest.NewRequest(http.MethodPost, "/vaccination/manual-campaigns", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "manual-campaign-test-0003")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "future_as_of") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if campaign.called {
		t.Fatalf("generator was called for future as_of")
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
		PlannedSessions: []domain.ImpactPlannedSession{{Date: "2026-01-01", Vaccinations: 24, DailyLimit: 100, Capacity: "within_cap"}},
		DosesAvailable:  "50", SourceRevision: 1700000000000, Warnings: nil,
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
	if len(resp.PlannedSessions) != 1 || resp.PlannedSessions[0].DailyLimit != 100 {
		t.Fatalf("planned_sessions body: %+v", resp.PlannedSessions)
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
	next, err := domain.EncodeRecordedCompletionCursor(domain.RecordedCompletionCursor{
		CompletionID:   "00000000-0000-4000-8000-000000000001",
		AdministeredAt: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	fake := &fakeImpact{queue: domain.RecordedCompletionPage{
		Items: []domain.RecordedCompletion{
			{CompletionID: "c1", GoatID: "g1", SOPTaskID: "task-1", SOPTaskVersion: 7, Doses: 1},
		},
		TotalCount: 251,
		NextCursor: &next,
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Items) != 1 || resp.Items[0].CompletionID != "c1" || resp.Items[0].SOPTaskID != "task-1" || resp.Items[0].SOPTaskVersion != 7 || resp.TotalCount != 251 || resp.NextCursor == nil || *resp.NextCursor != next {
		t.Fatalf("queue response: %+v err=%v", resp, err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?limit=-3", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: want 400, got %d", rec.Code)
	}

	cursor, err := domain.EncodeRecordedCompletionCursor(domain.RecordedCompletionCursor{
		CompletionID:   "00000000-0000-4000-8000-0000000000aa",
		AdministeredAt: time.Date(2026, 7, 12, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?park_id=00000000-0000-4000-8000-0000000000bb&cursor="+cursor, nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor request: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotCursor == nil || fake.gotCursor.CompletionID != "00000000-0000-4000-8000-0000000000aa" || fake.gotQueuePark != "00000000-0000-4000-8000-0000000000bb" {
		t.Fatalf("cursor/park not forwarded: cursor=%+v park=%q", fake.gotCursor, fake.gotQueuePark)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?cursor=not-a-real-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: want 400, got %d", rec.Code)
	}
}

// TestStageReviewItemsListAndResolveHTTP is the VACC-REV-10 operator-visibility guard at the HTTP
// layer: the list route returns open items and the resolve route resolves (200) or reports
// not-open (404) idempotently.
func TestStageReviewItemsListAndResolveHTTP(t *testing.T) {
	fake := &fakeImpact{
		reviewItems: []domain.StageReviewItem{{
			ReviewItemID: "11111111-0000-4000-8000-000000000001", GoatID: "22222222-0000-4000-8000-000000000002",
			Reason: "kid_stage_past_age_cutoff", ObservedStage: "K1", ObservedAgeWeeks: 26, Status: "open",
		}},
		resolveResult: true,
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake, nil))

	// list
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/vaccination/stage-review-items", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list stageReviewListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Reason != "kid_stage_past_age_cutoff" || list.Items[0].ObservedStage != "K1" {
		t.Fatalf("unexpected list: %#v", list.Items)
	}

	// resolve requires a typed resolution mode: empty body (no resolution) -> 400.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resolve without resolution status=%d, want 400", rec.Code)
	}

	// resolve with a resolution mode but no note -> 400 (note is required).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"corrected"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resolve without note status=%d, want 400", rec.Code)
	}

	// note over 500 chars -> 400.
	rec = httptest.NewRecorder()
	longNote := strings.Repeat("x", 501)
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"exception","note":"`+longNote+`"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resolve with 501-char note status=%d, want 400", rec.Code)
	}

	// 400 multibyte runes (800 bytes) is under the 500-CHARACTER limit -> accepted (guards against a
	// byte-length check rejecting valid multilingual notes).
	rec = httptest.NewRecorder()
	multibyteNote := strings.Repeat("é", 400) // 400 runes, 800 bytes
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"exception","note":"`+multibyteNote+`"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve with 400-rune (800-byte) note status=%d, want 200", rec.Code)
	}

	// corrected while the mismatch is still active -> 409 (service returns ErrStageReviewStillActive).
	fake.resolveErr = vaccinationapp.ErrStageReviewStillActive
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"corrected","note":"claims fixed but not"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("corrected-while-active status=%d, want 409", rec.Code)
	}
	fake.resolveErr = nil

	// resolve OK: corrected + note.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"corrected","note":"tag corrected to adult"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.resolvedID != "11111111-0000-4000-8000-000000000001" {
		t.Fatalf("resolved id = %q", fake.resolvedID)
	}

	// resolve on already-resolved/missing -> 404 (still requires a valid body).
	fake.resolveResult = false
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/vaccination/stage-review-items/11111111-0000-4000-8000-000000000001/resolve", strings.NewReader(`{"resolution":"exception","note":"already vaccinated; tag fix pending"}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("re-resolve status=%d, want 404", rec.Code)
	}
}
