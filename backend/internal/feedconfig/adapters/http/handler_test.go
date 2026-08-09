package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	feedconfigapp "github.com/vgoats/goatos/backend/internal/feedconfig/app"
	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// Handler tests cover what only the HTTP layer can get wrong: the Idempotency-Key gate, the request
// fingerprint's stability and sensitivity, strict body decoding, and the status-code mapping a
// retrying client depends on.

type fakeService struct {
	rateInput     feedconfigapp.UpsertRationRateInput
	scheduleInput feedconfigapp.UpsertScheduleConfigInput
	factorInput   feedconfigapp.UpsertShedFactorInput

	feedItemInput feedconfigapp.CreateFeedItemInput

	lastExperimentBatch feedconfigapp.UpsertExperimentConfigBatchInput

	experimentInput       feedconfigapp.UpsertExperimentConfigInput
	experimentStatusInput feedconfigapp.SetExperimentShedStatusInput

	result domain.WriteResult
	err    error
	calls  int
}

func (f *fakeService) ListRationRates(context.Context, string, string, string, string, string, *int32, *int32) (domain.RationRatePage, error) {
	return domain.RationRatePage{}, f.err
}
func (f *fakeService) ListRationGroups(context.Context, string, *int32, *int32) (domain.RationGroupPage, error) {
	return domain.RationGroupPage{}, f.err
}
func (f *fakeService) ListShedTags(context.Context, string, string, *int32, *int32) (domain.ShedTagPage, error) {
	return domain.ShedTagPage{}, f.err
}
func (f *fakeService) ListFeedItems(context.Context, string, *int32, *int32) (domain.FeedItemPage, error) {
	return domain.FeedItemPage{}, f.err
}
func (f *fakeService) ListSessionTemplates(context.Context, string, string, *int32, *int32) (domain.SessionTemplatePage, error) {
	return domain.SessionTemplatePage{}, f.err
}
func (f *fakeService) ListScheduleConfig(context.Context, string, string, string, *int32, *int32) (domain.ScheduleConfigPage, error) {
	return domain.ScheduleConfigPage{}, f.err
}
func (f *fakeService) ListShedFactors(context.Context, string, string, string, string, *int32, *int32) (domain.ShedFactorPage, error) {
	return domain.ShedFactorPage{}, f.err
}

func (f *fakeService) ListExperimentConfig(context.Context, string, string, string, string, *int32, *int32) (domain.ExperimentConfigPage, error) {
	return domain.ExperimentConfigPage{}, f.err
}

func (f *fakeService) ListPens(context.Context, string, string, *int32, *int32) (domain.PenPage, error) {
	return domain.PenPage{}, f.err
}

func (f *fakeService) UpsertExperimentConfigBatch(_ context.Context, in feedconfigapp.UpsertExperimentConfigBatchInput) (domain.WriteResult, error) {
	f.lastExperimentBatch = in
	return domain.WriteResult{}, f.err
}

func (f *fakeService) UpsertExperimentConfig(_ context.Context, in feedconfigapp.UpsertExperimentConfigInput) (domain.WriteResult, error) {
	f.calls++
	f.experimentInput = in
	return f.result, f.err
}

func (f *fakeService) SetExperimentShedStatus(_ context.Context, in feedconfigapp.SetExperimentShedStatusInput) (domain.WriteResult, error) {
	f.calls++
	f.experimentStatusInput = in
	return f.result, f.err
}

func (f *fakeService) UpsertRationRate(_ context.Context, in feedconfigapp.UpsertRationRateInput) (domain.WriteResult, error) {
	f.calls++
	f.rateInput = in
	return f.result, f.err
}

func (f *fakeService) CreateFeedItem(_ context.Context, in feedconfigapp.CreateFeedItemInput) (domain.WriteResult, error) {
	f.calls++
	f.feedItemInput = in
	return f.result, f.err
}

func (f *fakeService) UpsertShedFactor(_ context.Context, in feedconfigapp.UpsertShedFactorInput) (domain.WriteResult, error) {
	f.calls++
	f.factorInput = in
	return f.result, f.err
}

func (f *fakeService) UpsertScheduleConfig(_ context.Context, in feedconfigapp.UpsertScheduleConfigInput) (domain.WriteResult, error) {
	f.calls++
	f.scheduleInput = in
	return f.result, f.err
}

var _ Service = (*fakeService)(nil)

const testTenant = "00000000-0000-4000-8000-000000000001"

// newRequest builds a tenant-scoped request. The tenant lives in the context because that is where
// the auth middleware puts it in production.
func newRequest(t *testing.T, method, target, body, idempotencyKey string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	ctx := httpmiddleware.WithTenantID(req.Context(), testTenant)
	return req.WithContext(ctx)
}

func serve(t *testing.T, svc Service, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, nil))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestWriteRequiresIdempotencyKey proves an authored edit cannot be made without a client key. These
// routes are hit by browsers, so an unkeyed retry would author a second edit.
func TestWriteRequiresIdempotencyKey(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   string
	}{
		{name: "ration rate", target: "/feed-config/ration-rates", body: `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`},
		{name: "shed factor", target: "/feed-config/shed-factors", body: `{"park_id":"p","shed_id":"s","feed_item":"Concentrate","multiplier":1.5}`},
		{name: "schedule", target: "/feed-config/schedule", body: `{"park_id":"p","workflow":"normal","direction_time":"07:00","correction_time":"14:00"}`},
		// The catalog add is keyed like every other write even though it authors no quantity: an
		// unkeyed browser retry would otherwise turn one add into two attempts, and the second one
		// answers "already exists" for a name the operator only submitted once.
		{name: "feed item", target: "/feed-config/feed-items", body: `{"feed_item":"RGS Concentrate"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := serve(t, svc, newRequest(t, http.MethodPost, tc.target, tc.body, ""))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if svc.calls != 0 {
				t.Fatalf("service was called without an Idempotency-Key")
			}
		})
	}
}

// TestFingerprintIsStableForIdenticalRequests is the property that makes an EXACT REPLAY work.
//
// The same body sent twice must hash identically, so the repository recognises the second call as a
// replay and returns the original result. If the fingerprint folded in anything server-generated (a
// timestamp, the business date, the actor), a legitimate retry would hash differently and be
// rejected as a same-key/different-payload conflict.
func TestFingerprintIsStableForIdenticalRequests(t *testing.T) {
	body := `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`

	svc1 := &fakeService{}
	serve(t, svc1, newRequest(t, http.MethodPost, "/feed-config/ration-rates", body, "key-12345678"))
	svc2 := &fakeService{}
	serve(t, svc2, newRequest(t, http.MethodPost, "/feed-config/ration-rates", body, "key-12345678"))

	if svc1.rateInput.RequestFingerprint == "" {
		t.Fatalf("fingerprint was not computed")
	}
	if svc1.rateInput.RequestFingerprint != svc2.rateInput.RequestFingerprint {
		t.Fatalf("identical requests produced different fingerprints: %q vs %q",
			svc1.rateInput.RequestFingerprint, svc2.rateInput.RequestFingerprint)
	}

	// Label whitespace is normalized BEFORE hashing, so a cosmetic difference is the same request and
	// still replays rather than 409ing.
	spaced := `{"park_id":" p ","ration_group":"  Boer ","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`
	svc3 := &fakeService{}
	serve(t, svc3, newRequest(t, http.MethodPost, "/feed-config/ration-rates", spaced, "key-12345678"))
	if svc3.rateInput.RequestFingerprint != svc1.rateInput.RequestFingerprint {
		t.Fatalf("whitespace-only difference changed the fingerprint")
	}
}

// TestFingerprintChangesWithPayload is the other half: a DIFFERENT edit under the same key must hash
// differently, which is what lets the repository detect the same-key/different-payload conflict
// instead of silently replaying the first edit's result for a second, different one.
func TestFingerprintChangesWithPayload(t *testing.T) {
	base := `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`
	variants := map[string]string{
		"different rate":         `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":300}`,
		"different park":         `{"park_id":"q","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`,
		"different ration group": `{"park_id":"p","ration_group":"Sojat","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`,
		"different shed tag":     `{"park_id":"p","ration_group":"Boer","shed_tag":"Lactating","feed_item":"Concentrate","grams_per_head":250}`,
		"different feed item":    `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Green Fodder","grams_per_head":250}`,
		// 250 and 250.0 are the same number but a different authored string. They must NOT collide,
		// because the fingerprint's job is to detect that the client sent something different.
		"different decimal text": `{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250.0}`,
	}

	baseSvc := &fakeService{}
	serve(t, baseSvc, newRequest(t, http.MethodPost, "/feed-config/ration-rates", base, "key-12345678"))

	for name, body := range variants {
		t.Run(name, func(t *testing.T) {
			svc := &fakeService{}
			serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates", body, "key-12345678"))
			if svc.rateInput.RequestFingerprint == baseSvc.rateInput.RequestFingerprint {
				t.Fatalf("a different payload produced the same fingerprint")
			}
		})
	}
}

// TestFingerprintIsNamespacedPerRoute proves two different write surfaces cannot collide on one key.
func TestFingerprintIsNamespacedPerRoute(t *testing.T) {
	rateFP, err := requestFingerprint(testTenant, upsertRationRateCommand, rationRatesRoute, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	factorFP, err := requestFingerprint(testTenant, upsertShedFactorCommand, shedFactorsRoute, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	if rateFP == factorFP {
		t.Fatalf("identical bodies on different write routes produced the same fingerprint")
	}

	// The tenant is part of the hash too, so one tenant's key can never replay onto another's.
	otherTenant, err := requestFingerprint("00000000-0000-4000-8000-000000000002", upsertRationRateCommand, rationRatesRoute, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	if rateFP == otherTenant {
		t.Fatalf("the same body in two tenants produced the same fingerprint")
	}
}

// TestAbsentGramsIsNotDefaultedToZero proves the absent-vs-zero distinction survives JSON decoding.
// An omitted field must arrive at the service as nil, and an explicit 0 as "0" -- if the handler
// decoded into a plain float64, both would arrive as 0 and the distinction would be gone before any
// validation could see it.
func TestAbsentGramsIsNotDefaultedToZero(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate"}`, "key-12345678"))
	if svc.rateInput.GramsPerHead != nil {
		t.Fatalf("omitted grams_per_head arrived as %q, want nil", *svc.rateInput.GramsPerHead)
	}

	zeroSvc := &fakeService{}
	serve(t, zeroSvc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":0}`, "key-12345678"))
	if zeroSvc.rateInput.GramsPerHead == nil || *zeroSvc.rateInput.GramsPerHead != "0" {
		t.Fatalf("explicit zero arrived as %v, want \"0\"", zeroSvc.rateInput.GramsPerHead)
	}
}

// TestExactDecimalTextIsPreserved proves an authored decimal reaches the service as its exact text
// rather than through a float64 round trip.
func TestExactDecimalTextIsPreserved(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":149.995}`, "key-12345678"))
	if svc.rateInput.GramsPerHead == nil || *svc.rateInput.GramsPerHead != "149.995" {
		t.Fatalf("grams_per_head = %v, want the exact text \"149.995\"", svc.rateInput.GramsPerHead)
	}
}

// TestStrictDecodingRejectsUnknownFields matters for an AUTHORING api: a typo'd field name that is
// silently dropped leaves the author believing they set something they did not.
func TestStrictDecodingRejectsUnknownFields(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250,"gramz":1}`, "key-12345678"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown field", rec.Code)
	}
	if svc.calls != 0 {
		t.Fatalf("service was called for a body with an unknown field")
	}
}

// TestServiceErrorStatusMapping pins the codes a retrying client branches on.
//
// The critical one is 409 for a same-key/different-payload replay: it is the only answer that lets a
// client distinguish "my own retry succeeded" from "this key already means a different edit". A 400
// or a silent 200 would both hide a real collision.
func TestServiceErrorStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "idempotency conflict", err: ports.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: "idempotency_conflict"},
		{name: "future dated open row", err: ports.ErrFutureDatedRow, wantStatus: http.StatusConflict, wantCode: "future_dated_config"},
		{name: "park not found", err: ports.ErrParkNotFound, wantStatus: http.StatusNotFound, wantCode: "park_not_found"},
		{name: "shed not found", err: ports.ErrShedNotFound, wantStatus: http.StatusNotFound, wantCode: "shed_not_found"},
		{name: "missing park", err: feedconfigapp.ErrMissingPark, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "invalid paging", err: feedconfigapp.ErrInvalidPaging, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "field error", err: &domain.FieldError{Field: "grams_per_head", Reason: domain.ErrNegativeValue}, wantStatus: http.StatusBadRequest, wantCode: "invalid_field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{err: tc.err}
			rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
				`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`, "key-12345678"))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var body errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Code != tc.wantCode {
				t.Fatalf("error code = %q, want %q", body.Code, tc.wantCode)
			}
		})
	}
}

// TestFieldErrorNamesTheOffendingInput proves a validation failure tells the UI WHICH control to
// attach the message to. That is the difference between "validate or reject" being usable and being
// a generic wall.
func TestFieldErrorNamesTheOffendingInput(t *testing.T) {
	svc := &fakeService{err: &domain.FieldError{Field: "grams_per_head", Reason: domain.ErrNegativeValue, Detail: "-5"}}
	rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":-5}`, "key-12345678"))

	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Field != "grams_per_head" {
		t.Fatalf("error.field = %q, want grams_per_head", body.Field)
	}
}

// TestReplayFlagIsSurfaced proves the handler passes idempotent_replay through to the client rather
// than flattening it into an ordinary success.
func TestReplayFlagIsSurfaced(t *testing.T) {
	svc := &fakeService{result: domain.WriteResult{
		WriteID: "w1", Kind: domain.WriteKindRationRate, Outcome: domain.OutcomeSuperseded,
		ResultRowID: "r2", SupersededRowID: "r1", EffectiveFrom: "2026-07-19", Replayed: true,
	}}
	rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/ration-rates",
		`{"park_id":"p","ration_group":"Boer","shed_tag":"Pregnant","feed_item":"Concentrate","grams_per_head":250}`, "key-12345678"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body domain.WriteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Replayed {
		t.Fatalf("idempotent_replay = false, want true")
	}
	if body.Outcome != domain.OutcomeSuperseded || body.SupersededRowID != "r1" {
		t.Fatalf("outcome/superseded_row_id = %q/%q, want superseded/r1", body.Outcome, body.SupersededRowID)
	}
}

// TestMissingTenantIsUnauthorized covers the tenant gate on both a read and a write.
func TestMissingTenantIsUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeService{}, nil))

	for _, target := range []string{"/feed-config/ration-rates?park_id=p", "/feed-config/feed-items"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d, want 401", target, rec.Code)
		}
	}
}

// TestNonNumericPagingIsRejected proves a malformed paging value is a 400 rather than being ignored
// and replaced by the default.
func TestNonNumericPagingIsRejected(t *testing.T) {
	rec := serve(t, &fakeService{}, newRequest(t, http.MethodGet, "/feed-config/ration-rates?park_id=p&limit=lots", "", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Feed items (the catalog)
// ---------------------------------------------------------------------------

// TestCreateFeedItemKeepsOmittedAttributesAbsent is the catalog twin of
// TestAbsentGramsIsNotDefaultedToZero, and it guards the same decoding hazard reaching the opposite
// (correct) conclusion.
//
// An omitted attribute must arrive at the service as nil so it can be stored as NULL -- "nobody
// measured this". An explicit 0 must arrive as "0" -- "measured as none". Decoding into a plain
// float64 would collapse both into 0 before any layer could tell them apart, and the row would then
// claim a measurement that was never taken.
func TestCreateFeedItemKeepsOmittedAttributesAbsent(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/feed-items",
		`{"feed_item":"RGS Concentrate"}`, "key-12345678"))
	in := svc.feedItemInput
	if in.FeedItemLabel != "RGS Concentrate" {
		t.Fatalf("feed_item = %q, want %q", in.FeedItemLabel, "RGS Concentrate")
	}
	for name, got := range map[string]*string{
		"energy_kcal_per_kg": in.EnergyKcalPerKg,
		"dry_matter_factor":  in.DryMatterFactor,
		"wastage_factor":     in.WastageFactor,
	} {
		if got != nil {
			t.Fatalf("omitted %s arrived as %q, want nil", name, *got)
		}
	}
	if in.DisplayOrder != nil {
		t.Fatalf("omitted display_order arrived as %d, want nil so the write path appends to the end", *in.DisplayOrder)
	}

	zeroSvc := &fakeService{}
	serve(t, zeroSvc, newRequest(t, http.MethodPost, "/feed-config/feed-items",
		`{"feed_item":"Dry Masoor Bhusa","energy_kcal_per_kg":0,"wastage_factor":0}`, "key-12345678"))
	if got := zeroSvc.feedItemInput.EnergyKcalPerKg; got == nil || *got != "0" {
		t.Fatalf("explicit zero energy arrived as %v, want \"0\"", got)
	}
	if got := zeroSvc.feedItemInput.WastageFactor; got == nil || *got != "0" {
		t.Fatalf("explicit zero wastage arrived as %v, want \"0\"", got)
	}
}

// TestCreateFeedItemPreservesExactAttributeText proves an authored factor survives as its exact
// decimal text. 0.8500 through a float64 round trip is where a catalog attribute quietly becomes
// 0.8499999, on a screen whose whole purpose is authoring exact numbers.
func TestCreateFeedItemPreservesExactAttributeText(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/feed-items",
		`{"feed_item":"Green Fodder","dry_matter_factor":0.8500}`, "key-12345678"))
	if got := svc.feedItemInput.DryMatterFactor; got == nil || *got != "0.8500" {
		t.Fatalf("dry_matter_factor = %v, want the exact text \"0.8500\"", got)
	}
}

// TestCreateFeedItemDuplicateIsConflictNotSuccess maps ErrFeedItemExists to 409.
//
// Not 200, and not 400. A success would leave the author believing the catalog now holds two
// entries when the normalized key makes them one -- and every rate keyed on that label resolves to
// the original item, so the "new" one would appear to author nothing. 409 says the name is taken,
// which is the fact that decides their next move.
func TestCreateFeedItemDuplicateIsConflictNotSuccess(t *testing.T) {
	svc := &fakeService{err: ports.ErrFeedItemExists}
	rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/feed-items",
		`{"feed_item":"RGS Concentrate"}`, "key-12345678"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	// The code is what the UI keys its "that name already exists" message on, so it is part of the
	// contract rather than incidental text.
	if body.Code != "feed_item_exists" {
		t.Fatalf("error code = %q, want %q", body.Code, "feed_item_exists")
	}
}

// TestCreateFeedItemRejectsAParkID proves the catalog add refuses park scoping outright rather than
// ignoring it. feed_item_catalog is keyed (tenant, item), so a park_id in the body means the caller
// believes items can be scoped to one park -- and silently dropping it would confirm that belief
// while creating a tenant-wide item.
func TestCreateFeedItemRejectsAParkID(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, newRequest(t, http.MethodPost, "/feed-config/feed-items",
		`{"feed_item":"RGS Concentrate","park_id":"p"}`, "key-12345678"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if svc.calls != 0 {
		t.Fatalf("service was called with an unknown field present")
	}
}

// TestCreateFeedItemFingerprintIsItsOwnNamespace: the catalog add and the ration-rate write must
// never collide on one idempotency key, even for structurally similar bodies.
func TestCreateFeedItemFingerprintIsItsOwnNamespace(t *testing.T) {
	itemFP, err := requestFingerprint(testTenant, createFeedItemCommand, feedItemsRoute, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	rateFP, err := requestFingerprint(testTenant, upsertRationRateCommand, rationRatesRoute, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	if itemFP == rateFP {
		t.Fatalf("identical bodies on the catalog and ration-rate routes produced the same fingerprint")
	}
}
