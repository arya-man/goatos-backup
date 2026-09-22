package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// The transport contract for /health-config/registers/*: the same three things the
// protocol routes must never let through -- a write with no idempotency key, an unknown
// field silently dropped, and a validation failure flattened into one opaque message --
// plus the one that is specific to a document this size: the whole problem list must
// come back at once.

type fakeRegisterService struct {
	lastSave    domain.SaveRegisterDraftCommand
	lastVersion domain.RegisterVersionCommand
	lastClass   string
	err         error
	result      domain.RegisterAuthoringResult
}

func (f *fakeRegisterService) ListRegisters(context.Context, string) ([]domain.RegisterSummary, error) {
	return []domain.RegisterSummary{}, f.err
}
func (f *fakeRegisterService) GetRegister(context.Context, string, string) (domain.RegisterDetail, error) {
	return domain.RegisterDetail{}, f.err
}
func (f *fakeRegisterService) GetDraftForEdit(_ context.Context, cmd domain.RegisterVersionCommand, class string) (domain.RegisterDetail, error) {
	f.lastVersion, f.lastClass = cmd, class
	return domain.RegisterDetail{}, f.err
}
func (f *fakeRegisterService) SaveDraft(_ context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error) {
	f.lastSave = cmd
	return f.result, f.err
}
func (f *fakeRegisterService) PublishDraft(_ context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	f.lastVersion = cmd
	return f.result, f.err
}
func (f *fakeRegisterService) DiscardDraft(_ context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	f.lastVersion = cmd
	return f.result, f.err
}

func registerServer(svc RegisterConfigService) *http.ServeMux {
	mux := http.NewServeMux()
	RegisterRegisterConfig(mux, NewRegisterConfigHandler(svc, nil))
	return mux
}

const sampleDocument = `{"animal_class":"adult","document":{` +
	`"register_version":"adult-2","applies_class":["adult"],` +
	`"questions":[{"id":"nasal","kind":"choice","title":"Nasal discharge","options":[` +
	`{"value":"no","label":"No"},{"value":"yes","label":"Yes","emits":["nasal_discharge"]}]}],` +
	`"rules":[{"id":"FEVER","pathognomonic":[{"findings":["nasal_discharge"]}],"severity_base":3}]}}`

func TestEveryRegisterWriteRequiresAnIdempotencyKey(t *testing.T) {
	mux := registerServer(&fakeRegisterService{})
	const id = "11111111-1111-4111-8111-111111111111"

	for _, c := range []struct{ path, body string }{
		{"/health-config/registers/drafts", `{"animal_class":"adult"}`},
		{"/health-config/registers/drafts/save", sampleDocument},
		{"/health-config/registers/" + id + "/publish", ``},
		{"/health-config/registers/" + id + "/discard", ``},
	} {
		rec := post(t, mux, c.path, c.body, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s without a key: want 400, got %d", c.path, rec.Code)
		}
	}
}

// A renamed field must not be silently dropped. Accepting it would author a register
// with the old value still in place while the client believes it changed -- the
// accept-and-discard failure AGENTS.md bans, on a clinical rule table.
func TestAnUnknownRegisterFieldIsRejected(t *testing.T) {
	mux := registerServer(&fakeRegisterService{})
	rec := post(t, mux, "/health-config/registers/drafts/save",
		`{"animal_class":"adult","documnet":{}}`, "k1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown field, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The same rejection one level down: an unknown key INSIDE the document is caught by
// the strict loader, not by the request decoder, so both layers are proven here.
func TestAnUnknownKeyInsideTheDocumentIsRejected(t *testing.T) {
	mux := registerServer(&fakeRegisterService{})
	body := strings.Replace(sampleDocument, `"register_version":"adult-2"`,
		`"register_version":"adult-2","severity_fudge":3`, 1)
	rec := post(t, mux, "/health-config/registers/drafts/save", body, "k1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown document key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A register with forty questions rejected one field per round trip is not authorable,
// and the two-direction check usually fires in pairs: an author adding a symptom and
// the rule that reads it gets both halves wrong at once or neither.
func TestEveryRegisterProblemComesBackAtOnce(t *testing.T) {
	svc := &fakeRegisterService{err: &domain.ValidationError{Errors: []domain.FieldError{
		{Field: "questions.3.options.1.emits.0", Message: "reaches no rule"},
		{Field: "rules.7.probable.0.findings.0", Message: "no answer emits this"},
	}}}
	mux := registerServer(svc)

	rec := post(t, mux, "/health-config/registers/drafts/save", sampleDocument, "k1")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
	var out struct {
		Code   string              `json:"code"`
		Errors []domain.FieldError `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != "invalid_register" {
		t.Errorf("code = %q", out.Code)
	}
	if len(out.Errors) != 2 {
		t.Fatalf("both problems must come back together, got %d: %s", len(out.Errors), rec.Body.String())
	}
	if out.Errors[0].Field == "" || out.Errors[1].Field == "" {
		t.Error("each problem must name its own path so the editor can mark the row")
	}
}

// The fingerprint covers the version id, not the empty body, so two DIFFERENT registers
// published under one reused key are a conflict rather than a silent replay of the first.
func TestTwoRegisterPublishesUnderOneKeyCarryDifferentFingerprints(t *testing.T) {
	svc := &fakeRegisterService{}
	mux := registerServer(svc)

	post(t, mux, "/health-config/registers/11111111-1111-4111-8111-111111111111/publish", "", "same-key")
	first := svc.lastVersion.RequestFingerprint
	post(t, mux, "/health-config/registers/22222222-2222-4222-8222-222222222222/publish", "", "same-key")
	second := svc.lastVersion.RequestFingerprint

	if first == "" || first == second {
		t.Fatalf("publishing two registers under one key must differ: %q vs %q", first, second)
	}
}

// A save and a publish carrying the same key must never collide: the command name is
// part of the hash precisely so one route's key cannot replay another's write.
func TestARegisterSaveAndPublishNeverShareAFingerprint(t *testing.T) {
	svc := &fakeRegisterService{}
	mux := registerServer(svc)

	post(t, mux, "/health-config/registers/drafts/save", sampleDocument, "k")
	save := svc.lastSave.RequestFingerprint
	post(t, mux, "/health-config/registers/11111111-1111-4111-8111-111111111111/publish", "", "k")
	publish := svc.lastVersion.RequestFingerprint

	if save == publish {
		t.Fatal("a save and a publish under one key must not hash alike")
	}
}

func TestRegisterErrorsMapToTheirStatus(t *testing.T) {
	for _, c := range []struct {
		err  error
		want int
		code string
	}{
		{ports.ErrRegisterNotFound, http.StatusNotFound, "not_found"},
		{ports.ErrRegisterDraftExists, http.StatusConflict, "draft_exists"},
		{ports.ErrRegisterNotADraft, http.StatusConflict, "not_a_draft"},
		{ports.ErrConflict, http.StatusConflict, "idempotency_conflict"},
	} {
		mux := registerServer(&fakeRegisterService{err: c.err})
		rec := post(t, mux, "/health-config/registers/drafts", `{"animal_class":"adult"}`, "k1")
		if rec.Code != c.want {
			t.Errorf("%v: want %d, got %d", c.err, c.want, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%v: body should carry %q, got %s", c.err, c.code, rec.Body.String())
		}
	}
}

// The document decodes into the SAME type the engine evaluates, so a shape the handler
// accepts is a shape the engine can run. Two parallel types would drift.
func TestTheDocumentDecodesIntoTheEnginesOwnType(t *testing.T) {
	svc := &fakeRegisterService{}
	mux := registerServer(svc)

	if rec := post(t, mux, "/health-config/registers/drafts/save", sampleDocument, "k1"); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var _ diagnosis.AuthoredRegister = svc.lastSave.Document
	if svc.lastSave.Document.RegisterVersion != "adult-2" {
		t.Errorf("register label = %q", svc.lastSave.Document.RegisterVersion)
	}
	if len(svc.lastSave.Document.Questions) != 1 || len(svc.lastSave.Document.Rules) != 1 {
		t.Fatalf("document did not round-trip: %+v", svc.lastSave.Document)
	}
	if got := svc.lastSave.Document.Questions[0].Options[1].Emits; len(got) != 1 || got[0] != "nasal_discharge" {
		t.Errorf("the emitted token is what makes a question mean anything: %v", got)
	}
}
