package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// The transport contract for /health-config/*: what the browser sends, what it gets back, and the
// three things this layer must never let through — a write with no idempotency key, an unknown
// field silently dropped, and a validation failure flattened into one opaque message.

type fakeConfigService struct {
	saveCalls   int
	lastSave    domain.SaveDraftCommand
	lastCreate  domain.CreateDiseaseCommand
	lastPublish domain.ProtocolVersionCommand
	err         error
	result      domain.AuthoringResult
}

func (f *fakeConfigService) ListProtocolCatalog(context.Context, domain.ProtocolCatalogQuery) (domain.ProtocolCatalogPage, error) {
	return domain.ProtocolCatalogPage{Items: []domain.ProtocolCatalogItem{}}, f.err
}
func (f *fakeConfigService) GetProtocolDetail(context.Context, string, string) (domain.ProtocolDetail, error) {
	return domain.ProtocolDetail{}, f.err
}
func (f *fakeConfigService) GetDraftForEdit(context.Context, domain.ProtocolVersionCommand, string, string) (domain.ProtocolDetail, error) {
	return domain.ProtocolDetail{}, f.err
}
func (f *fakeConfigService) CreateDisease(_ context.Context, cmd domain.CreateDiseaseCommand) (domain.AuthoringResult, error) {
	f.lastCreate = cmd
	return f.result, f.err
}
func (f *fakeConfigService) SaveDraft(_ context.Context, cmd domain.SaveDraftCommand) (domain.AuthoringResult, error) {
	f.saveCalls++
	f.lastSave = cmd
	return f.result, f.err
}
func (f *fakeConfigService) PublishDraft(_ context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	f.lastPublish = cmd
	return f.result, f.err
}
func (f *fakeConfigService) DiscardDraft(_ context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	f.lastPublish = cmd
	return f.result, f.err
}

func configServer(svc ConfigService) *http.ServeMux {
	mux := http.NewServeMux()
	RegisterConfig(mux, NewConfigHandler(svc, nil))
	return mux
}

func post(t *testing.T, mux *http.ServeMux, path, body, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Every mutating route requires the header. Without it a retry after a network failure would apply
// the edit twice — and for a publish, that means two versions where the author intended one.
func TestEveryWriteRequiresAnIdempotencyKey(t *testing.T) {
	svc := &fakeConfigService{}
	mux := configServer(svc)

	cases := []struct{ path, body string }{
		{"/health-config/diseases", `{"display_name":"Foot rot"}`},
		{"/health-config/drafts/save", `{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","steps":[]}`},
		{"/health-config/protocols/11111111-1111-4111-8111-111111111111/publish", ``},
		{"/health-config/protocols/11111111-1111-4111-8111-111111111111/discard", ``},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := post(t, mux, tc.path, tc.body, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 without an Idempotency-Key", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "idempotency_key_required") {
				t.Fatalf("body must name the missing header, got %s", rec.Body.String())
			}
		})
	}
	if svc.saveCalls != 0 {
		t.Fatal("a request rejected for a missing key must not reach the service")
	}
}

// The draft-open route is the ONE write with no key, because at most one draft can exist per
// protocol — the uniqueness constraint IS the idempotency.
func TestOpeningADraftNeedsNoIdempotencyKey(t *testing.T) {
	mux := configServer(&fakeConfigService{})
	rec := post(t, mux, "/health-config/drafts", `{"disease_key":"foot_rot","age_band":"adult"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 — opening a draft carries no key by design", rec.Code)
	}
}

// Unknown fields are REJECTED, not ignored. A client that renamed a field would otherwise author a
// protocol with the old value silently still in place — the accept-and-discard failure mode, on a
// write path that carries dosages.
func TestUnknownFieldsAreRejected(t *testing.T) {
	svc := &fakeConfigService{}
	mux := configServer(svc)
	rec := post(t, mux, "/health-config/drafts/save",
		`{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","steps":[],"dosage_units":"ml"}`, "k1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 for an unknown field", rec.Code)
	}
	if svc.saveCalls != 0 {
		t.Fatal("a request with an unknown field must not reach the service")
	}
}

// A blank duration must reach the service as ABSENT so the backend default applies; a present
// out-of-range value must reach it VERBATIM so the backend rejects it. Neither may be coerced here.
func TestDurationAbsentTakesTheDefaultAndOutOfRangeIsForwarded(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		svc := &fakeConfigService{}
		mux := configServer(svc)
		rec := post(t, mux, "/health-config/drafts/save",
			`{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","steps":[]}`, "k1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if svc.lastSave.Protocol.DurationDays != domain.DefaultDurationDays {
			t.Fatalf("absent duration must become the declared default %d, got %d",
				domain.DefaultDurationDays, svc.lastSave.Protocol.DurationDays)
		}
	})
	t.Run("present and out of range", func(t *testing.T) {
		svc := &fakeConfigService{}
		mux := configServer(svc)
		post(t, mux, "/health-config/drafts/save",
			`{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","duration_days":999,"steps":[]}`, "k2")
		if svc.lastSave.Protocol.DurationDays != 999 {
			t.Fatalf("an out-of-range duration must be forwarded verbatim for the backend to reject, got %d",
				svc.lastSave.Protocol.DurationDays)
		}
	})
	t.Run("explicit zero is not absent", func(t *testing.T) {
		svc := &fakeConfigService{}
		mux := configServer(svc)
		post(t, mux, "/health-config/drafts/save",
			`{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","duration_days":0,"steps":[]}`, "k3")
		// An explicit 0 is an authored (invalid) value, NOT the absent case. Turning it into the
		// default would author a course length nobody typed.
		if svc.lastSave.Protocol.DurationDays != 0 {
			t.Fatalf("an explicit 0 must stay 0 so the backend rejects it, got %d", svc.lastSave.Protocol.DurationDays)
		}
	})
}

// Validation failures come back as 422 with EVERY offending field named, so the editor can mark
// every bad row at once. Flattening them into one message makes a 28-step protocol unauthorable.
func TestValidationErrorsAreReturnedPerField(t *testing.T) {
	svc := &fakeConfigService{err: &domain.ValidationError{Errors: []domain.FieldError{
		{Field: "steps[0].medicine_route", Message: "Choose how the medicine is given."},
		{Field: "steps[3].day_no", Message: "Day 9 is beyond this protocol's 7 day(s)."},
	}}}
	mux := configServer(svc)
	rec := post(t, mux, "/health-config/drafts/save",
		`{"disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","steps":[]}`, "k1")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422", rec.Code)
	}
	var body struct {
		Code   string `json:"code"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body.Code != "invalid_protocol" {
		t.Fatalf("code=%q, want invalid_protocol", body.Code)
	}
	if len(body.Errors) != 2 {
		t.Fatalf("expected both field errors, got %+v", body.Errors)
	}
	if body.Errors[0].Field != "steps[0].medicine_route" || body.Errors[1].Field != "steps[3].day_no" {
		t.Fatalf("field errors must survive in order, got %+v", body.Errors)
	}
}

// The fingerprint covers the COMMAND and the addressed version, not just the body. Publish and
// discard carry no body at all, so a body-only fingerprint would make one reused key silently
// replay a publish when the author asked for a discard — or publish the wrong version.
func TestFingerprintDistinguishesCommandsAndVersions(t *testing.T) {
	svc := &fakeConfigService{}
	mux := configServer(svc)

	post(t, mux, "/health-config/protocols/11111111-1111-4111-8111-111111111111/publish", "", "same-key")
	publishA := svc.lastPublish.RequestFingerprint

	post(t, mux, "/health-config/protocols/22222222-2222-4222-8222-222222222222/publish", "", "same-key")
	publishB := svc.lastPublish.RequestFingerprint

	post(t, mux, "/health-config/protocols/11111111-1111-4111-8111-111111111111/discard", "", "same-key")
	discardA := svc.lastPublish.RequestFingerprint

	if publishA == publishB {
		t.Fatal("publishing two DIFFERENT versions must not share a fingerprint")
	}
	if publishA == discardA {
		t.Fatal("publish and discard of the SAME version must not share a fingerprint")
	}
}

// Backend errors map to statuses a client can act on, not to a generic 500.
func TestDomainErrorsMapToActionableStatuses(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{ports.ErrDraftExists, http.StatusConflict, "draft_exists"},
		{ports.ErrDiseaseExists, http.StatusConflict, "disease_exists"},
		{ports.ErrNotADraft, http.StatusConflict, "not_a_draft"},
		{ports.ErrProtocolInUse, http.StatusConflict, "protocol_in_use"},
		{ports.ErrConflict, http.StatusConflict, "idempotency_conflict"},
		{ports.ErrNotFound, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			mux := configServer(&fakeConfigService{err: tc.err})
			rec := post(t, mux, "/health-config/diseases", `{"display_name":"Foot rot"}`, "k1")
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d", rec.Code, tc.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body must carry %q, got %s", tc.wantCode, rec.Body.String())
			}
		})
	}
}

// A create returns 201; an idempotent replay of that create returns 200. The distinction is what
// tells a client "this happened now" from "this already happened".
func TestCreateReturns201AndAReplayReturns200(t *testing.T) {
	fresh := &fakeConfigService{result: domain.AuthoringResult{Outcome: domain.OutcomeCreated, DiseaseKey: "foot_rot"}}
	if rec := post(t, configServer(fresh), "/health-config/diseases", `{"display_name":"Foot rot"}`, "k1"); rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 for a fresh create", rec.Code)
	}
	replay := &fakeConfigService{result: domain.AuthoringResult{
		Outcome: domain.OutcomeCreated, DiseaseKey: "foot_rot", IdempotentReplay: true,
	}}
	if rec := post(t, configServer(replay), "/health-config/diseases", `{"display_name":"Foot rot"}`, "k1"); rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 for a replay", rec.Code)
	}
}

// The steps a client sends carry no seq: order is positional and the server assigns 1..N.
func TestSavedStepsArriveInTheOrderTheClientSentThem(t *testing.T) {
	svc := &fakeConfigService{}
	mux := configServer(svc)
	post(t, mux, "/health-config/drafts/save", `{
      "disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","duration_days":1,
      "steps":[
        {"day_no":1,"session":"evening","record_type":"action","instruction":"Second"},
        {"day_no":1,"session":"morning","record_type":"action","instruction":"First"}
      ]}`, "k1")

	if len(svc.lastSave.Protocol.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(svc.lastSave.Protocol.Steps))
	}
	// The handler preserves the wire order verbatim; ORDERING is the service's job, so a handler
	// that quietly sorted would hide a service regression.
	if svc.lastSave.Protocol.Steps[0].Instruction != "Second" {
		t.Fatalf("the handler must forward the client's order untouched, got %q",
			svc.lastSave.Protocol.Steps[0].Instruction)
	}
}
