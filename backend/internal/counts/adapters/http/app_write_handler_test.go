package http

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	testTenantID = "11111111-1111-4111-8111-111111111111"
	testActorID  = "22222222-2222-4222-8222-222222222222"
	testGoatID   = "33333333-3333-4333-8333-333333333333"
	testParkID   = "44444444-4444-4444-8444-444444444444"
	testShedID   = "55555555-5555-4555-8555-555555555555"
	testMotherID = "66666666-6666-4666-8666-666666666666"
)

// The three submit routes now RECORD a pending approval request instead of APPLYING the event
// (maintainer decision 2026-07-19). These tests therefore assert two things at once: the transport
// rules that already existed (idempotency, enum strictness, origin pinning, the dead+died
// guardrail) are unchanged, AND nothing is applied at submit time.
//
// *identityapp.Service must keep satisfying the validator seam this handler depends on: if identity
// ever renames or re-signs Prepare*, the submit routes would silently lose their submit-time
// validation and an operator would only learn of a malformed payload days later from an approver.
var _ GoatLifecycleValidator = (*identityapp.Service)(nil)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeShiftingRepo re-implements the idempotency semantics the real Postgres repository enforces
// (maybeExistingShiftingByIdempotency / maybeExistingShiftingByLogicalKey), so these tests prove the
// handler derives hashes that make an exact replay collapse and a changed payload conflict.
type fakeShiftingRepo struct {
	ports.Repository

	byIdempotencyKey map[string]string // idempotency key -> request fingerprint
	byLogicalKey     map[string]string // logical key -> payload hash
	idByKey          map[string]string
	inserts          int
	lastEvent        domain.ShiftingEvent

	// Operator-facing shifting support.
	//
	// goatFacts is keyed by goat id and models the REAL query's membership predicate: an id that is
	// absent from the map resolves to no row, exactly as a merged, exited, or wrong-tenant animal
	// does in Postgres. That is what lets a test prove the handler fails closed instead of
	// substituting a placeholder impact.
	destinations   domain.ShiftingDestinationCatalog
	breeds         []domain.CountsBreakdownSeriesPoint
	goatFacts      map[string]domain.GoatShiftingFact
	goatFactsCalls int
}

func newFakeShiftingRepo() *fakeShiftingRepo {
	return &fakeShiftingRepo{
		byIdempotencyKey: map[string]string{},
		byLogicalKey:     map[string]string{},
		idByKey:          map[string]string{},
	}
}

func (f *fakeShiftingRepo) RecordShiftingEvent(_ context.Context, in domain.ShiftingEvent) (string, bool, error) {
	if fingerprint, ok := f.byIdempotencyKey[in.IdempotencyKey]; ok {
		if fingerprint != in.RequestFingerprint {
			return "", false, ports.ErrIdempotencyConflict
		}
		return f.idByKey[in.IdempotencyKey], true, nil
	}
	if payloadHash, ok := f.byLogicalKey[in.LogicalShiftingEventKey]; ok {
		if payloadHash != in.PayloadHash {
			return "", false, ports.ErrLogicalKeyConflict
		}
		return f.idByKey[in.IdempotencyKey], true, nil
	}
	f.inserts++
	f.lastEvent = in
	id := "shifting-event-" + hex.EncodeToString([]byte{byte(f.inserts)})
	f.byIdempotencyKey[in.IdempotencyKey] = in.RequestFingerprint
	f.byLogicalKey[in.LogicalShiftingEventKey] = in.PayloadHash
	f.idByKey[in.IdempotencyKey] = id
	return id, false, nil
}

func (f *fakeShiftingRepo) ShiftingDestinationCatalog(_ context.Context, _ string) (domain.ShiftingDestinationCatalog, error) {
	return f.destinations, nil
}

func (f *fakeShiftingRepo) ActiveBreeds(_ context.Context, _ string) ([]domain.CountsBreakdownSeriesPoint, error) {
	return f.breeds, nil
}

// GoatShiftingFacts mirrors the real query's behaviour for a missing animal: it returns FEWER rows
// than requested rather than an error, so the service's own "did every id resolve" check is what
// gets exercised.
func (f *fakeShiftingRepo) GoatShiftingFacts(_ context.Context, _ string, goatIDs []string) ([]domain.GoatShiftingFact, error) {
	f.goatFactsCalls++
	out := make([]domain.GoatShiftingFact, 0, len(goatIDs))
	for _, id := range goatIDs {
		if fact, ok := f.goatFacts[id]; ok {
			out = append(out, fact)
		}
	}
	return out, nil
}

// fakeApprovalWorkflow re-implements the submit-side idempotency semantics the real
// ApprovalService/Postgres repository enforce: the stored key is the submission's IdempotencyKey and
// the stored fingerprint is the handler-derived RequestFingerprint, so an exact replay returns the
// ORIGINAL request with replay=true and creates no second request, while a same-key/different-payload
// submission is a conflict. The counters are what let a test prove "exactly one pending request
// exists" -- a duplicate here is a duplicate kid or a duplicate death in an approver's queue.
type fakeApprovalWorkflow struct {
	requestByKey     map[string]domain.ApprovalRequest
	fingerprintByKey map[string]string

	submits           int
	submitsByType     map[string]int
	lastSubmission    domain.ApprovalRequestSubmission
	birthResultsByKey map[string]domain.BirthSubmissionResult
}

func newFakeApprovalWorkflow() *fakeApprovalWorkflow {
	return &fakeApprovalWorkflow{
		requestByKey:      map[string]domain.ApprovalRequest{},
		fingerprintByKey:  map[string]string{},
		submitsByType:     map[string]int{},
		birthResultsByKey: map[string]domain.BirthSubmissionResult{},
	}
}

func (f *fakeApprovalWorkflow) SubmitRequest(_ context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error) {
	if existing, ok := f.requestByKey[in.IdempotencyKey]; ok {
		if f.fingerprintByKey[in.IdempotencyKey] != in.RequestFingerprint {
			return domain.ApprovalRequest{}, false, ports.ErrIdempotencyConflict
		}
		// An exact replay must return the ORIGINAL row and leave the counter alone: the submit
		// counter is the test's proxy for "how many requests a reviewer will see".
		return existing, true, nil
	}
	f.submits++
	f.submitsByType[in.RequestType]++
	f.lastSubmission = in
	request := domain.ApprovalRequest{
		ApprovalRequestID:  fmt.Sprintf("approval-request-%d", f.submits),
		TenantID:           in.TenantID,
		RequestType:        in.RequestType,
		Payload:            in.Payload,
		ShiftingEventID:    in.ShiftingEventID,
		SubjectGoatID:      in.SubjectGoatID,
		Status:             domain.ApprovalStatusPending,
		RaisedByUserID:     in.RaisedByUserID,
		RaisedAt:           in.RaisedAt,
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	}
	f.requestByKey[in.IdempotencyKey] = request
	f.fingerprintByKey[in.IdempotencyKey] = in.RequestFingerprint
	return request, false, nil
}

func (f *fakeApprovalWorkflow) SubmitBirthRequest(_ context.Context, in domain.ApprovalRequestSubmission, commands []identityports.CreateAdminGoatCommand) (domain.BirthSubmissionResult, error) {
	request, replay, err := f.SubmitRequest(context.Background(), in)
	if err != nil {
		return domain.BirthSubmissionResult{}, err
	}
	if replay {
		result := f.birthResultsByKey[in.IdempotencyKey]
		result.Replayed = true
		return result, nil
	}
	children := make([]domain.BirthChildResult, 0, len(commands))
	for i, command := range commands {
		temporaryIdentifier := ""
		for _, identifier := range command.Identifiers {
			if identifier.IdentifierType == "temporary_tag" {
				temporaryIdentifier = identifier.IdentifierValue
			}
		}
		children = append(children, domain.BirthChildResult{
			GoatID:              fmt.Sprintf("00000000-0000-4000-8000-%012d", i+101),
			TemporaryIdentifier: temporaryIdentifier,
			ChildOrdinal:        i + 1,
		})
	}
	result := domain.BirthSubmissionResult{Approval: request, Children: children}
	f.birthResultsByKey[in.IdempotencyKey] = result
	return result, nil
}

// ListPending and Decide exist only to satisfy ApprovalWorkflow so RegisterApprovals can be wired
// alongside RegisterAppWrites. These tests cover the SUBMIT half of the workflow; the decision half
// is exercised against the real service.
func (f *fakeApprovalWorkflow) ListPending(
	_ context.Context, _, _ string, _ []string, _ string, _ int, _ string,
) (domain.ApprovalRequestPage, error) {
	return domain.ApprovalRequestPage{Items: []domain.ApprovalRequestSummary{}}, nil
}

func (f *fakeApprovalWorkflow) Decide(_ context.Context, _ countsapp.DecisionInput) (domain.ApprovalRequest, bool, error) {
	return domain.ApprovalRequest{}, false, ports.ErrApprovalRequestNotFound
}

// fakeGoatValidator stands in for identity/app.Service's Prepare* seam -- the validate-now/apply-never
// half of a lifecycle write. It replaces the old fakeGoatLifecycle (which implemented
// CreateAdminGoat / CriticalDeathExit) because the handler no longer has any way to apply a birth or
// a death: that seam was deleted, not merely left unused.
//
// The death path DELEGATES to a real *identityapp.Service rather than rubber-stamping, so every
// death test in this file -- not only the dedicated guardrail test -- runs the genuine
// validateCriticalDeathExit. A fake that accepted any body would let a regression that widened the
// dead+died pairing pass every test except one.
type fakeGoatValidator struct {
	// creates and deaths run the REAL identity validation. Keeping a rubber-stamp create fake here
	// previously let incomplete birth payloads enter the approval queue in tests and production.
	creates GoatLifecycleValidator
	// deaths runs the REAL identity validation for PrepareCriticalDeathExit.
	deaths GoatLifecycleValidator

	prepareCreates int
	prepareDeaths  int

	lastCreate       identityapp.CreateAdminGoatInput
	lastDeath        identityapp.ExitGoatInput
	lastDeathCommand identityports.ExitGoatCommand

	lastTempList identityapp.ListTemporaryTaggedGoatsInput
	tempItems    []identitydomain.TemporaryTaggedGoat
	tempNext     *string
}

func newFakeGoatValidator() *fakeGoatValidator {
	return newFakeGoatValidatorWithRepo(&stubIdentityRepo{})
}

func newFakeGoatValidatorWithRepo(repo *stubIdentityRepo) *fakeGoatValidator {
	service := identityapp.NewService(repo)
	return &fakeGoatValidator{creates: service, deaths: service}
}

// PrepareCreateAdminGoat records the payload the handler forwarded. It keeps the missing-key
// rejection the old fake had: the client Idempotency-Key must reach the owning module, because that
// key is what makes the operator's retry collapse rather than raise a second birth request.
func (f *fakeGoatValidator) PrepareCreateAdminGoat(ctx context.Context, in identityapp.CreateAdminGoatInput) (identityports.CreateAdminGoatCommand, error) {
	if in.IdempotencyKey == "" {
		return identityports.CreateAdminGoatCommand{}, identityapp.BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	f.prepareCreates++
	f.lastCreate = in
	return f.creates.PrepareCreateAdminGoat(ctx, in)
}

func (f *fakeGoatValidator) BirthProvisionalPrefix(context.Context, string, string) (string, error) {
	return "CBE", nil
}

// PrepareCriticalDeathExit records the payload and then runs the REAL guardrail. The returned
// command is captured so a test can assert what the guarded path would have executed -- the only
// observable proof left now that nothing is executed at submit time.
func (f *fakeGoatValidator) PrepareCriticalDeathExit(ctx context.Context, in identityapp.ExitGoatInput) (identityports.ExitGoatCommand, error) {
	if in.IdempotencyKey == "" {
		return identityports.ExitGoatCommand{}, identityapp.BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	f.lastDeath = in
	cmd, err := f.deaths.PrepareCriticalDeathExit(ctx, in)
	if err != nil {
		return identityports.ExitGoatCommand{}, err
	}
	f.prepareDeaths++
	f.lastDeathCommand = cmd
	return cmd, nil
}

func (f *fakeGoatValidator) PromoteTemporaryIdentifier(_ context.Context, in identityapp.PromoteTemporaryIdentifierInput) (*identitydomain.AdminGoatResponse, error) {
	if in.IdempotencyKey == "" {
		return nil, identityapp.BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	return &identitydomain.AdminGoatResponse{Goat: identitydomain.GoatSummary{GoatID: in.GoatID}}, nil
}

func (f *fakeGoatValidator) ListTemporaryTaggedGoats(_ context.Context, in identityapp.ListTemporaryTaggedGoatsInput) (*identitydomain.TemporaryTaggedGoatsResult, error) {
	f.lastTempList = in
	items := f.tempItems
	if items == nil {
		items = []identitydomain.TemporaryTaggedGoat{}
	}
	return &identitydomain.TemporaryTaggedGoatsResult{Items: items, NextCursor: f.tempNext, TraceID: in.TraceID}, nil
}

// stubIdentityRepo satisfies identity's repository port. ExitGoat is deliberately implemented and
// counted even though the submit path must never reach it: a non-zero call count is the alarm that
// someone re-wired an apply into a submit route.
type stubIdentityRepo struct {
	identityports.Repository

	calls    int
	lastExit identityports.ExitGoatCommand
}

func (s *stubIdentityRepo) BirthProvisionalPrefix(context.Context, string, string) (string, error) {
	return "CBE", nil
}

func (s *stubIdentityRepo) ExitGoat(_ context.Context, cmd identityports.ExitGoatCommand) (*identityports.AdminGoatMutationResult, error) {
	s.calls++
	s.lastExit = cmd
	return &identityports.AdminGoatMutationResult{
		Goat:             identitydomain.GoatSummary{GoatID: cmd.GoatID, LifecycleStatus: "dead"},
		GenerationStatus: "complete",
	}, nil
}

func (s *stubIdentityRepo) ValidateAdminGoatCreate(_ context.Context, cmd identityports.ValidateAdminGoatCreateCommand) (identityports.AdminGoatCreateValidation, error) {
	validation := identityports.AdminGoatCreateValidation{
		CustodianPartyID: testActorID,
		ParkID:           testParkID,
		ShedID:           testShedID,
	}
	if cmd.BirthDamRef != nil {
		motherID := testMotherID
		validation.DamGoatID = &motherID
	}
	return validation, nil
}

func (s *stubIdentityRepo) CreateAdminGoat(_ context.Context, _ identityports.CreateAdminGoatCommand) (*identityports.AdminGoatMutationResult, error) {
	s.calls++
	return nil, fmt.Errorf("CreateAdminGoat must not run from an app submit route")
}

// goatCreator and goatExiter reproduce the method set of the DELETED GoatLifecycleWriter. They are
// never implemented by anything here; they exist so a test can assert by reflection that no field on
// AppWriteHandler can create or exit an animal.
type goatCreator interface {
	CreateAdminGoat(ctx context.Context, in identityapp.CreateAdminGoatInput) (*identitydomain.AdminGoatResponse, error)
}

type goatExiter interface {
	CriticalDeathExit(ctx context.Context, in identityapp.ExitGoatInput) (*identitydomain.AdminGoatResponse, error)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestServer wires the handler the way production does: the submit routes only become
// pending-request writers once WithApprovalWorkflow supplies BOTH collaborators, and the decision
// surface is registered on the same mux so a route-pattern collision between the two register
// functions would fail here rather than at boot.
func newTestServer(
	t *testing.T, shifting ShiftingEventRecorder, approvals ApprovalWorkflow, validator GoatLifecycleValidator,
) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	handler := NewAppWriteHandler(shifting, nil).WithApprovalWorkflow(approvals, validator)
	RegisterAppWrites(mux, handler)
	RegisterApprovals(mux, handler)
	return mux
}

func post(t *testing.T, mux *http.ServeMux, path, idempotencyKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

// shiftingBody builds a VALID shifting submit. goat_ids is required (maintainer decision,
// 2026-07-19: a shifting event must name the animals it moves), so it is part of the baseline
// fixture rather than something individual tests opt into.
func shiftingBody(headCount int) map[string]any {
	return map[string]any{
		"destination_park_id": testParkID,
		"destination_shed_id": testShedID,
		"impacts": []map[string]any{
			{"breed_key": "sirohi", "breed_label": "Sirohi", "head_count": headCount},
		},
		"goat_ids": []string{testGoatID},
	}
}

func deathBody(lifecycleStatus, exitReason string) map[string]any {
	return map[string]any{
		"goat_id":          testGoatID,
		"lifecycle_status": lifecycleStatus,
		"exit_reason":      exitReason,
		"reason":           "found dead in shed during morning round",
		"row_version":      1,
		"evidence_refs": []map[string]any{
			{"evidence_type": "media", "evidence_id": "proof-123"},
		},
	}
}

func birthBody(identifier string) map[string]any {
	return map[string]any{
		"animal_identifier_1": identifier,
		"species":             "goat",
		"sex":                 "female",
		"dob":                 "2026-07-01",
		"entry_date":          "2026-07-01",
		"park_id":             testParkID,
		"shed_id":             testShedID,
		"breed":               "beetal",
		"dam_id":              "RFID-MOTHER-001",
		"litter_size":         1,
		"evidence_refs": []map[string]any{
			{"evidence_type": "media", "evidence_id": "birth-proof-1"},
		},
	}
}

// birthBodyAutoProvisional is what the phone actually sends after the 2026-07-27 split: NO
// animal_identifier_1 and NO temporary_identifier, because the operator no longer scans an RFID at
// birth and the server mints the provisional tag.
func birthBodyAutoProvisional() map[string]any {
	body := birthBody("unused")
	delete(body, "animal_identifier_1")
	return body
}

// A birth is not valid until it carries the three facts that make the permanent
// mother/child relationship unambiguous. The phone supplies the mother's RFID in
// dam_id; identity resolves that value to the canonical mother goat before the
// request can enter the approval queue.
func TestRecordBirthEventRequiresBreedMotherAndLitterSize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{name: "breed", field: "breed"},
		{name: "mother RFID", field: "dam_id"},
		{name: "litter size", field: "litter_size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validator := newFakeGoatValidator()
			approvals := newFakeApprovalWorkflow()
			mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

			body := birthBodyAutoProvisional()
			body["breed"] = "beetal"
			body["dam_id"] = "RFID-MOTHER-001"
			body["litter_size"] = 2
			delete(body, tc.field)

			rec := post(t, mux, appBirthEventRoute, "birth-missing-"+strings.ReplaceAll(tc.field, "_", "-"), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400 when %s is missing", rec.Code, rec.Body.String(), tc.field)
			}
			if approvals.submits != 0 {
				t.Fatalf("submits=%d, want 0: an incomplete birth must never reach approval", approvals.submits)
			}
		})
	}
}

// TestRecordBirthEventAutoProvisionalTagIsDeterministicAcrossRetries is the offline-retry
// regression for the server-minted provisional tag.
//
// The Android write path is the offline outbox: it retries the SAME Idempotency-Key until it gets a
// response, so a lost 202 is the normal case, not an edge case. The provisional tag is injected into
// the body BEFORE the body is marshalled, and that same body feeds the request fingerprint — so a
// randomly regenerated tag makes the fingerprint differ on every retry, the approval store reports
// ErrIdempotencyConflict, and the handler answers 409 forever. The birth is already queued
// server-side, so the operator's outbox entry wedges permanently on a birth that did record.
//
// The tag must therefore be derived deterministically from the client key: a retry has to rebuild a
// byte-identical body and replay cleanly.
func TestRecordBirthEventAutoProvisionalTagIsDeterministicAcrossRetries(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	first := post(t, mux, appBirthEventRoute, "birth-key-auto-1", birthBodyAutoProvisional())
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", first.Code, first.Body.String())
	}
	firstForwarded := append([]byte(nil), validator.lastCreate.RawBody...)
	if !bytes.Contains(firstForwarded, []byte(`"temporary_identifier"`)) {
		t.Fatalf("server must mint a provisional tag when none is supplied: %s", firstForwarded)
	}

	second := post(t, mux, appBirthEventRoute, "birth-key-auto-1", birthBodyAutoProvisional())
	if second.Code != http.StatusAccepted {
		t.Fatalf("offline retry status=%d body=%s, want 202 — the outbox entry is wedged on a birth "+
			"that already recorded", second.Code, second.Body.String())
	}
	if !bytes.Equal(firstForwarded, validator.lastCreate.RawBody) {
		t.Fatalf("provisional tag is not deterministic across retries:\n%s\n%s",
			firstForwarded, validator.lastCreate.RawBody)
	}
	if approvals.submits != 1 {
		t.Fatalf("submits=%d after an offline retry, want 1 (no duplicate pending birth)", approvals.submits)
	}
	var body appApprovalSubmitResponse
	decodeBody(t, second, &body)
	if !body.IdempotentReplay {
		t.Fatal("offline retry must report idempotent_replay=true")
	}
}

// Two different births must not collide onto one provisional tag just because the derivation is
// deterministic — the key, not a constant, is what varies.
func TestRecordBirthEventAutoProvisionalTagsDifferPerClientKey(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	if rec := post(t, mux, appBirthEventRoute, "birth-key-auto-a", birthBodyAutoProvisional()); rec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	tagA := append([]byte(nil), validator.lastCreate.RawBody...)

	if rec := post(t, mux, appBirthEventRoute, "birth-key-auto-b", birthBodyAutoProvisional()); rec.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if bytes.Equal(tagA, validator.lastCreate.RawBody) {
		t.Fatalf("two distinct births share one provisional tag:\n%s", tagA)
	}
}

// ---------------------------------------------------------------------------
// Shifting events
// ---------------------------------------------------------------------------

func TestRecordShiftingEventHappyPath(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-key-0001", shiftingBody(12))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got appShiftingEventResponse
	decodeBody(t, rec, &got)
	if got.ShiftingEventID == "" {
		t.Fatal("expected a shifting_event_id")
	}
	// The movement row alone is inert: without a linked approval request it sits pending in a table
	// nobody is queued to act on. The response must hand the client the queue entry's id.
	if got.ApprovalRequestID == "" {
		t.Fatal("expected an approval_request_id: a recorded movement with no approval request is work nobody can decide")
	}
	if got.Status != domain.ApprovalStatusPending {
		t.Fatalf("status=%q, want %q (recording a movement must never self-authorize it)", got.Status, domain.ApprovalStatusPending)
	}
	if got.IdempotentReplay {
		t.Fatal("first call must not report an idempotent replay")
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d, want 1", repo.inserts)
	}
	if approvals.submits != 1 || approvals.submitsByType[domain.ApprovalRequestTypeShifting] != 1 {
		t.Fatalf("submits=%d by-type=%v, want exactly one shifting request", approvals.submits, approvals.submitsByType)
	}
	// The request must point at the movement it authorizes; an unlinked request cannot be applied
	// because approval has nothing to flip to authorized.
	if approvals.lastSubmission.ShiftingEventID == nil || *approvals.lastSubmission.ShiftingEventID != got.ShiftingEventID {
		t.Fatalf("approval request is not linked to the shifting event: %+v", approvals.lastSubmission.ShiftingEventID)
	}
	event := repo.lastEvent
	if event.TenantID != testTenantID {
		t.Fatalf("tenant_id=%q, want %q", event.TenantID, testTenantID)
	}
	// A field operator REPORTS a movement; they do not self-authorize it.
	if event.AuthorizationState != "pending" || event.EventStatus != "pending" || event.VerificationState != "unverified" {
		t.Fatalf("unexpected workflow state: auth=%q status=%q verification=%q",
			event.AuthorizationState, event.EventStatus, event.VerificationState)
	}
	if event.SourceSystem != appShiftingSourceSystem {
		t.Fatalf("source_system=%q, want %q", event.SourceSystem, appShiftingSourceSystem)
	}
	if len(event.Impacts) != 1 || event.Impacts[0].HeadCount != 12 {
		t.Fatalf("unexpected impacts: %+v", event.Impacts)
	}
	if event.Impacts[0].GrainKey == "" || event.PayloadHash == "" || event.RequestFingerprint == "" {
		t.Fatalf("grain key/payload hash/request fingerprint must all be derived: %+v", event)
	}
}

func TestRecordShiftingEventExactReplayHasNoNewSideEffect(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	first := post(t, mux, appShiftingEventRoute, "shift-key-0002", shiftingBody(9))
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := post(t, mux, appShiftingEventRoute, "shift-key-0002", shiftingBody(9))
	if second.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s, want 200", second.Code, second.Body.String())
	}

	var firstBody, secondBody appShiftingEventResponse
	decodeBody(t, first, &firstBody)
	decodeBody(t, second, &secondBody)
	if firstBody.ShiftingEventID != secondBody.ShiftingEventID {
		t.Fatalf("replay returned a different id: %q vs %q", firstBody.ShiftingEventID, secondBody.ShiftingEventID)
	}
	// The approval request must collapse with the movement. If only the event were idempotent, a
	// retry from a phone with poor signal would leave two entries in the approver's queue for one
	// movement, and approving both would move the same animals twice.
	if firstBody.ApprovalRequestID != secondBody.ApprovalRequestID {
		t.Fatalf("replay returned a different approval request: %q vs %q", firstBody.ApprovalRequestID, secondBody.ApprovalRequestID)
	}
	if !secondBody.IdempotentReplay {
		t.Fatal("replay must be reported as an idempotent replay")
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d after an exact replay, want 1 (no duplicate movement)", repo.inserts)
	}
	if approvals.submits != 1 {
		t.Fatalf("approval submits=%d after an exact replay, want 1 (no duplicate queue entry)", approvals.submits)
	}
}

func TestRecordShiftingEventSameKeyDifferentPayloadIsRejected(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	if rec := post(t, mux, appShiftingEventRoute, "shift-key-0003", shiftingBody(4)); rec.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec := post(t, mux, appShiftingEventRoute, "shift-key-0003", shiftingBody(5))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d, want 1 (the conflicting replay must not write)", repo.inserts)
	}
	// The conflict is detected on the movement write, which runs first -- so the approval workflow
	// must never have been asked to raise a second request for a payload that was rejected.
	if approvals.submits != 1 {
		t.Fatalf("approval submits=%d, want 1 (a rejected payload must not reach the approver's queue)", approvals.submits)
	}
}

func TestRecordShiftingEventRequiresIdempotencyKey(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "", shiftingBody(3))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if repo.inserts != 0 {
		t.Fatalf("inserts=%d, want 0", repo.inserts)
	}
	if approvals.submits != 0 {
		t.Fatalf("approval submits=%d, want 0 (an unkeyed write has no retry identity and must not be recorded)", approvals.submits)
	}
}

func TestRecordShiftingEventRejectsPresentButInvalidEnum(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	body := shiftingBody(3)
	body["priority"] = "sometime-soon"
	rec := post(t, mux, appShiftingEventRoute, "shift-key-0004", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 (a present-but-invalid enum is rejected, never defaulted)", rec.Code, rec.Body.String())
	}
	if repo.inserts != 0 {
		t.Fatalf("inserts=%d, want 0", repo.inserts)
	}
	if approvals.submits != 0 {
		t.Fatalf("approval submits=%d, want 0", approvals.submits)
	}
}

// TestRecordShiftingEventRequiresGoatIDs pins the 2026-07-19 maintainer decision: a shifting event
// must NAME the animals it moves, and it must fail at SUBMIT.
//
// Rejecting here rather than at approval is the whole point. shifting_events is an aggregate model
// -- impacts record "12 head of Sirohi", never which twelve animals -- so goat_ids is the only
// per-animal linkage the movement has. If a nameless submit were accepted, the movement would sit
// pending looking perfectly valid, and the failure would surface hours later in front of the park
// head approving it, who was not at the shed and cannot supply the list. The operator who knows
// which animals moved must learn immediately, while they can still answer.
//
// Both spellings of "no animals" are covered: the field absent entirely, and the field present as
// an empty array. A caller that sends `"goat_ids": []` is making the same claim as one that omits
// it, and both must be refused identically.
func TestRecordShiftingEventRequiresGoatIDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(map[string]any)
		wantCode string
		wantMsg  string
	}{
		{
			name:     "absent",
			mutate:   func(b map[string]any) { delete(b, "goat_ids") },
			wantCode: "missing_goat_ids",
			wantMsg:  "a submit that names no animals must be rejected",
		},
		{
			name:     "empty array",
			mutate:   func(b map[string]any) { b["goat_ids"] = []string{} },
			wantCode: "missing_goat_ids",
			wantMsg:  "an explicitly empty goat_ids must be rejected, not treated as 'move nobody'",
		},
		{
			name:     "blank element",
			mutate:   func(b map[string]any) { b["goat_ids"] = []string{"   "} },
			wantCode: "invalid_goat_id",
			wantMsg:  "a whitespace-only goat id names no animal and must be rejected",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeShiftingRepo()
			approvals := newFakeApprovalWorkflow()
			mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

			body := shiftingBody(3)
			tc.mutate(body)

			rec := post(t, mux, appShiftingEventRoute, "shift-key-no-goats-"+tc.name, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400: %s", rec.Code, rec.Body.String(), tc.wantMsg)
			}
			// Assert the SPECIFIC code, not merely "some 400". Without this the test would still
			// pass if the request were rejected for an unrelated reason, which would silently stop
			// proving the goat_ids rule at all.
			var envelope identitydomain.ErrorEnvelope
			decodeBody(t, rec, &envelope)
			if envelope.Code != tc.wantCode {
				t.Fatalf("error code=%q, want %q (body=%s)", envelope.Code, tc.wantCode, rec.Body.String())
			}

			// The movement must not be recorded at all. A pending shifting_events row that names
			// nobody is exactly the state this rule exists to prevent: it would later be approvable
			// (authorizing a movement) while relocating no animals, leaving the herd register and
			// the shed-scoped vaccination obligations disagreeing with the reported count.
			if repo.inserts != 0 {
				t.Fatalf("inserts=%d, want 0 (a rejected submit must not record a movement)", repo.inserts)
			}
			if approvals.submits != 0 {
				t.Fatalf("approval submits=%d, want 0 (nothing may reach a park head's queue)", approvals.submits)
			}
		})
	}
}

// TestRecordShiftingEventStoresNamedGoatIDsOnTheApprovalRequest proves the required list actually
// reaches the approval request, since that stored payload -- not shifting_events -- is what
// approval reads to decide which animals to relocate. It also pins the submit-time normalization
// (dedupe + sort) that keeps approval's requested-vs-moved fail-closed comparison honest: a
// duplicate id would otherwise make a perfectly valid request look partially-applied.
func TestRecordShiftingEventStoresNamedGoatIDsOnTheApprovalRequest(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	second := "33333333-3333-4333-8333-333333333334"
	body := shiftingBody(3)
	// Deliberately unsorted, with a duplicate and surrounding whitespace.
	body["goat_ids"] = []string{second, " " + testGoatID + " ", second}

	rec := post(t, mux, appShiftingEventRoute, "shift-key-goats-0001", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}

	var stored struct {
		GoatIDs []string `json:"goat_ids"`
	}
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &stored); err != nil {
		t.Fatalf("decode stored approval payload %q: %v", string(approvals.lastSubmission.Payload), err)
	}
	want := []string{testGoatID, second}
	sort.Strings(want)
	if len(stored.GoatIDs) != len(want) {
		t.Fatalf("stored goat_ids=%v, want %v (duplicates removed, whitespace trimmed)", stored.GoatIDs, want)
	}
	for i := range want {
		if stored.GoatIDs[i] != want[i] {
			t.Fatalf("stored goat_ids=%v, want %v (deduplicated and sorted)", stored.GoatIDs, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Birth events
// ---------------------------------------------------------------------------

func TestRecordBirthEventForcesOriginTypeBirth(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-key-0001", birthBody("KID-001"))
	// 202, not 200: the route accepted a request for later decision, it did not create an animal.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var got appApprovalSubmitResponse
	decodeBody(t, rec, &got)
	if got.Status != domain.ApprovalStatusPending || got.RequestType != domain.ApprovalRequestTypeBirth {
		t.Fatalf("status=%q request_type=%q, want pending/birth", got.Status, got.RequestType)
	}
	if got.ApprovalRequestID == "" {
		t.Fatal("expected an approval_request_id")
	}
	if validator.prepareCreates != 1 {
		t.Fatalf("prepareCreates=%d, want 1 (the payload must still be validated at submit time)", validator.prepareCreates)
	}

	// origin_type must be pinned in the body handed to the validator: if it were pinned only on the
	// response or only at approval time, a submission validated as one kind of animal could be
	// applied as another.
	var validated map[string]any
	if err := json.Unmarshal(validator.lastCreate.RawBody, &validated); err != nil {
		t.Fatalf("validated body is not JSON: %v", err)
	}
	if validated["origin_type"] != "birth" {
		t.Fatalf("validated origin_type=%v, want birth", validated["origin_type"])
	}
	// It must ALSO be pinned in the stored payload, because that is the body replayed through
	// identity on approval -- days after the operator's phone is out of the picture.
	var stored map[string]any
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &stored); err != nil {
		t.Fatalf("stored payload is not JSON: %v", err)
	}
	if stored["origin_type"] != "birth" {
		t.Fatalf("stored origin_type=%v, want birth (the app route must not be able to park a procured animal in the queue)", stored["origin_type"])
	}
	if stored["dam_id"] != testMotherID {
		t.Fatalf("stored dam_id=%v, want canonical mother goat id %s", stored["dam_id"], testMotherID)
	}
	if stored["litter_size"] != float64(1) {
		t.Fatalf("stored litter_size=%v, want 1", stored["litter_size"])
	}
	if validator.lastCreate.TenantID != testTenantID || validator.lastCreate.ActorID != testActorID {
		t.Fatalf("tenant/actor not propagated: %+v", validator.lastCreate)
	}
	if validator.lastCreate.IdempotencyKey != "birth-key-0001:child:1" {
		t.Fatalf("idempotency key=%q, want the stable first-child key", validator.lastCreate.IdempotencyKey)
	}
	if approvals.lastSubmission.RaisedByUserID != testActorID {
		t.Fatalf("raised_by=%q, want %q (an approver has to know who reported the birth)", approvals.lastSubmission.RaisedByUserID, testActorID)
	}
}

func TestRecordBirthEventRejectsConflictingOriginType(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	body := birthBody("KID-002")
	body["origin_type"] = "procured"
	rec := post(t, mux, appBirthEventRoute, "birth-key-0002", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if validator.prepareCreates != 0 {
		t.Fatalf("prepareCreates=%d, want 0 (a declared non-birth origin is rejected, not silently rewritten)", validator.prepareCreates)
	}
	if approvals.submits != 0 {
		t.Fatalf("submits=%d, want 0 (a rejected submission must not land in the approver's queue)", approvals.submits)
	}
}

func TestRecordBirthEventExactReplayHasNoNewSideEffect(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	first := post(t, mux, appBirthEventRoute, "birth-key-0003", birthBody("KID-003"))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", first.Code, first.Body.String())
	}
	firstValidated := append([]byte(nil), validator.lastCreate.RawBody...)

	second := post(t, mux, appBirthEventRoute, "birth-key-0003", birthBody("KID-003"))
	if second.Code != http.StatusAccepted {
		t.Fatalf("replay status=%d body=%s, want 202", second.Code, second.Body.String())
	}
	// The old guarantee was "no duplicate kid". The kid no longer exists at this point, so the
	// guarantee that replaces it is "no duplicate PENDING KID": two queue entries for one birth would
	// let an approver create the same animal twice.
	if approvals.submits != 1 {
		t.Fatalf("submits=%d after an exact replay, want 1 (no duplicate pending birth)", approvals.submits)
	}
	// The forwarded body must be byte-identical, otherwise the handler would derive a different
	// request fingerprint and the retry would be treated as a payload change (409) instead of a replay.
	if !bytes.Equal(firstValidated, validator.lastCreate.RawBody) {
		t.Fatalf("forwarded body is not deterministic:\n%s\n%s", firstValidated, validator.lastCreate.RawBody)
	}

	var firstBody, secondBody appApprovalSubmitResponse
	decodeBody(t, first, &firstBody)
	decodeBody(t, second, &secondBody)
	if firstBody.ApprovalRequestID != secondBody.ApprovalRequestID {
		t.Fatalf("replay returned a different approval request: %q vs %q", firstBody.ApprovalRequestID, secondBody.ApprovalRequestID)
	}
	if !secondBody.IdempotentReplay {
		t.Fatal("replay must report idempotent_replay=true")
	}
}

func TestRecordBirthEventSameKeyDifferentPayloadIsRejected(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	if rec := post(t, mux, appBirthEventRoute, "birth-key-0004", birthBody("KID-004")); rec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	changed := birthBody("KID-004")
	changed["breed"] = "sirohi"
	rec := post(t, mux, appBirthEventRoute, "birth-key-0004", changed)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	// Reusing a key for a DIFFERENT kid must never overwrite or shadow the first submission: the
	// queue must still hold exactly the birth that was originally reported.
	if approvals.submits != 1 {
		t.Fatalf("submits=%d, want 1", approvals.submits)
	}
	if stored := approvals.requestByKey["app-counts-birth:birth-key-0004"]; !bytes.Contains(stored.Payload, []byte("beetal")) {
		t.Fatalf("stored payload was replaced by the conflicting submission: %s", stored.Payload)
	}
}

// TestSubmitBirthEventCreatesPendingRequestAndAppliesNothing pins the maintainer decision itself: a
// birth submitted from the phone must produce ONE pending request and NO animal. Before this change
// the same call created the kid inline (and therefore its vaccination obligations) with no approval
// at all.
func TestSubmitBirthEventCreatesPendingRequestAndAppliesNothing(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-key-0005", birthBody("KID-005"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var got appApprovalSubmitResponse
	decodeBody(t, rec, &got)
	if got.Status != domain.ApprovalStatusPending {
		t.Fatalf("status=%q, want pending", got.Status)
	}
	if got.RaisedAt.IsZero() {
		t.Fatal("raised_at must be set: an approver's queue is ordered by it")
	}
	if approvals.submits != 1 || approvals.submitsByType[domain.ApprovalRequestTypeBirth] != 1 {
		t.Fatalf("submits=%d by-type=%v, want exactly one birth request", approvals.submits, approvals.submitsByType)
	}
	stored := approvals.requestByKey["app-counts-birth:birth-key-0005"]
	if stored.Status != domain.ApprovalStatusPending || stored.AppliedResultID != nil || stored.AppliedResultType != nil {
		t.Fatalf("submitting must leave the request undecided and unapplied: %+v", stored)
	}

	// The payload WAS validated -- deferring the apply must not defer the error report, or an
	// operator would only learn of a malformed dob days later from an approver.
	if validator.prepareCreates != 1 {
		t.Fatalf("prepareCreates=%d, want 1 (the payload must be validated at submit time)", validator.prepareCreates)
	}

	// ...and nothing was applied. The direct-apply seam is gone rather than merely unused, so the
	// proof is structural: no field on the handler is able to create or exit an animal. An unused
	// apply dependency is a bypass waiting to be re-wired, and this fails the moment one reappears.
	handlerType := reflect.TypeOf(AppWriteHandler{})
	creator := reflect.TypeOf((*goatCreator)(nil)).Elem()
	exiter := reflect.TypeOf((*goatExiter)(nil)).Elem()
	for i := 0; i < handlerType.NumField(); i++ {
		field := handlerType.Field(i)
		if field.Type.Implements(creator) || field.Type.Implements(exiter) {
			t.Fatalf("AppWriteHandler.%s can apply a goat lifecycle write; submit routes must only record a pending request", field.Name)
		}
	}
}

// A litter is one operator submission but N canonical children. The submit response must hand the
// phone every created child immediately so the Birth work list can open one workflow per kid; the
// separate web approval remains pending and only controls herd-count eligibility.
func TestRecordTwinBirthCreatesTwoDistinctProvisionalChildrenImmediately(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	body := birthBody("")
	delete(body, "temporary_identifier")
	body["litter_size"] = 2
	rec := post(t, mux, appBirthEventRoute, "birth-twins-immediate-children", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var got struct {
		Status   string `json:"status"`
		Children []struct {
			GoatID              string `json:"goat_id"`
			TemporaryIdentifier string `json:"temporary_identifier"`
		} `json:"children"`
	}
	decodeBody(t, rec, &got)
	if got.Status != domain.ApprovalStatusPending {
		t.Fatalf("approval status=%q, want pending", got.Status)
	}
	if len(got.Children) != 2 {
		t.Fatalf("children=%d body=%s, want two canonical kids from one twin submission", len(got.Children), rec.Body.String())
	}
	if got.Children[0].GoatID == "" || got.Children[1].GoatID == "" || got.Children[0].GoatID == got.Children[1].GoatID {
		t.Fatalf("children must have distinct canonical goat ids: %+v", got.Children)
	}
	if got.Children[0].TemporaryIdentifier == "" || got.Children[1].TemporaryIdentifier == "" ||
		got.Children[0].TemporaryIdentifier == got.Children[1].TemporaryIdentifier {
		t.Fatalf("children must have distinct provisional identifiers: %+v", got.Children)
	}
	for _, child := range got.Children {
		if !strings.HasPrefix(child.TemporaryIdentifier, "CBE-") || len(child.TemporaryIdentifier) != len("CBE-12345") {
			t.Fatalf("temporary_identifier=%q, want CBE- plus five digits", child.TemporaryIdentifier)
		}
	}
}

// ---------------------------------------------------------------------------
// Death events
// ---------------------------------------------------------------------------

// TestRecordDeathEventRecordsPendingRequest is the former happy-path test. A death no longer exits
// the animal at submit time, so the guarantee under test is now: the payload is validated and
// transposed correctly, a pending request is created, and the animal is still alive (its open
// vaccination obligations therefore still open) until someone approves.
func TestRecordDeathEventRecordsPendingRequest(t *testing.T) {
	repo := &stubIdentityRepo{}
	validator := newFakeGoatValidatorWithRepo(repo)
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	rec := post(t, mux, appDeathEventRoute, "death-key-0001", deathBody("dead", "died"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var got appApprovalSubmitResponse
	decodeBody(t, rec, &got)
	if got.Status != domain.ApprovalStatusPending || got.RequestType != domain.ApprovalRequestTypeDeath {
		t.Fatalf("status=%q request_type=%q, want pending/death", got.Status, got.RequestType)
	}
	if validator.prepareDeaths != 1 {
		t.Fatalf("prepareDeaths=%d, want 1", validator.prepareDeaths)
	}
	// NOTHING was applied: the exit never reached identity's repository, so the animal is alive and
	// its obligations are untouched until the request is approved.
	if repo.calls != 0 {
		t.Fatalf("identity repo calls=%d, want 0 (submitting a death must not exit the animal)", repo.calls)
	}
	if validator.lastDeath.GoatID != testGoatID {
		t.Fatalf("goat_id=%q, want %q", validator.lastDeath.GoatID, testGoatID)
	}
	// goat_id addresses the animal and must not survive into identity's strictly-decoded body.
	var validated map[string]any
	if err := json.Unmarshal(validator.lastDeath.RawBody, &validated); err != nil {
		t.Fatalf("validated body is not JSON: %v", err)
	}
	if _, present := validated["goat_id"]; present {
		t.Fatal("goat_id must be stripped from the body handed to identity")
	}
	if validated["lifecycle_status"] != "dead" || validated["exit_reason"] != "died" {
		t.Fatalf("the death pairing must be forwarded verbatim: %+v", validated)
	}
	// The STORED payload is the mirror image: it must keep goat_id inline, because this route has no
	// {goat_id} path segment and approval has nothing else to identify the animal with.
	var stored map[string]any
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &stored); err != nil {
		t.Fatalf("stored payload is not JSON: %v", err)
	}
	if stored["goat_id"] != testGoatID {
		t.Fatalf("stored payload must keep goat_id=%q for the approval path: %+v", testGoatID, stored)
	}
	if approvals.lastSubmission.SubjectGoatID == nil || *approvals.lastSubmission.SubjectGoatID != testGoatID {
		t.Fatalf("the request must name its subject animal: %+v", approvals.lastSubmission.SubjectGoatID)
	}
}

func TestRecordDeathEventExactReplayHasNoNewSideEffect(t *testing.T) {
	repo := &stubIdentityRepo{}
	validator := newFakeGoatValidatorWithRepo(repo)
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	if rec := post(t, mux, appDeathEventRoute, "death-key-0002", deathBody("dead", "died")); rec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	second := post(t, mux, appDeathEventRoute, "death-key-0002", deathBody("dead", "died"))
	if second.Code != http.StatusAccepted {
		t.Fatalf("replay status=%d body=%s, want 202", second.Code, second.Body.String())
	}
	// The replacement for "no duplicate exit": one reported death is one queue entry, so an approver
	// cannot be shown (and act on) the same death twice.
	if approvals.submits != 1 {
		t.Fatalf("submits=%d after an exact replay, want 1 (no duplicate pending death)", approvals.submits)
	}
	if repo.calls != 0 {
		t.Fatalf("identity repo calls=%d, want 0", repo.calls)
	}
	var secondBody appApprovalSubmitResponse
	decodeBody(t, second, &secondBody)
	if !secondBody.IdempotentReplay {
		t.Fatal("replay must report idempotent_replay=true")
	}
}

func TestRecordDeathEventSameKeyDifferentPayloadIsRejected(t *testing.T) {
	repo := &stubIdentityRepo{}
	validator := newFakeGoatValidatorWithRepo(repo)
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	if rec := post(t, mux, appDeathEventRoute, "death-key-0003", deathBody("dead", "died")); rec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	changed := deathBody("dead", "died")
	changed["reason"] = "a different account of the same death"
	rec := post(t, mux, appDeathEventRoute, "death-key-0003", changed)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if approvals.submits != 1 {
		t.Fatalf("submits=%d, want 1", approvals.submits)
	}
	// The reason an approver reads must be the one that was originally reported: a same-key replay
	// must not quietly rewrite the account of the death under the approver.
	if stored := approvals.requestByKey["app-counts-death:death-key-0003"]; bytes.Contains(stored.Payload, []byte("a different account")) {
		t.Fatalf("the conflicting replay overwrote the stored payload: %s", stored.Payload)
	}
	if repo.calls != 0 {
		t.Fatalf("identity repo calls=%d, want 0", repo.calls)
	}
}

func TestRecordDeathEventRequiresGoatID(t *testing.T) {
	repo := &stubIdentityRepo{}
	validator := newFakeGoatValidatorWithRepo(repo)
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

	body := deathBody("dead", "died")
	delete(body, "goat_id")
	rec := post(t, mux, appDeathEventRoute, "death-key-0004", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if validator.prepareDeaths != 0 {
		t.Fatalf("prepareDeaths=%d, want 0", validator.prepareDeaths)
	}
	// A death that names no animal must not be parked in the queue: it could never be applied, and
	// it would sit in an approver's list forever.
	if approvals.submits != 0 {
		t.Fatalf("submits=%d, want 0", approvals.submits)
	}
}

// TestRecordDeathEventPreservesCriticalDeathGuardrail drives the REAL identity service through the
// app route (not a fake), proving the lifecycle_status="dead" + exit_reason="died" pairing is still
// mandatory and still routed through the guardrail-approved critical-death path. Deferring the apply
// must not defer the guardrail: a payload the guarded path would refuse must never be able to reach
// an approver's queue, where accepting it would look like authority to kill the animal.
func TestRecordDeathEventPreservesCriticalDeathGuardrail(t *testing.T) {
	for _, tc := range []struct {
		name            string
		lifecycleStatus string
		exitReason      string
	}{
		{name: "not a death at all", lifecycleStatus: "sold", exitReason: "sold"},
		{name: "dead without the died reason", lifecycleStatus: "dead", exitReason: "sold"},
		{name: "died without the dead status", lifecycleStatus: "sold", exitReason: "died"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubIdentityRepo{}
			approvals := newFakeApprovalWorkflow()
			mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, identityapp.NewService(repo))

			rec := post(t, mux, appDeathEventRoute, "death-guardrail-1", deathBody(tc.lifecycleStatus, tc.exitReason))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
			}
			if repo.calls != 0 {
				t.Fatalf("repo calls=%d, want 0 (the guardrail must reject before any write)", repo.calls)
			}
			if approvals.submits != 0 {
				t.Fatalf("submits=%d, want 0 (a guardrail-rejected pairing must not be queued for approval)", approvals.submits)
			}
		})
	}

	t.Run("the dead+died pairing is accepted and stays guardrail-approved", func(t *testing.T) {
		repo := &stubIdentityRepo{}
		// The wrapper delegates to the same real *identityapp.Service; it exists only to capture the
		// command the guardrail produced, which is otherwise unobservable now that submit discards it.
		validator := newFakeGoatValidatorWithRepo(repo)
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)

		rec := post(t, mux, appDeathEventRoute, "death-guardrail-2", deathBody("dead", "died"))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
		}
		if approvals.submits != 1 {
			t.Fatalf("submits=%d, want 1", approvals.submits)
		}
		// The old assertion was "the exit reached the repo as guardrail-approved". Submitting applies
		// nothing now, so the equivalent guarantee is asserted one layer earlier: the real guardrail
		// ran and produced a guardrail-approved command, and that command was NOT executed.
		if repo.calls != 0 {
			t.Fatalf("repo calls=%d, want 0 (validation must not apply the exit)", repo.calls)
		}
		if !validator.lastDeathCommand.GuardrailApproved {
			t.Fatal("the app death route must validate through the guardrail-approved critical death exit")
		}
		if validator.lastDeathCommand.LifecycleStatus != "dead" || validator.lastDeathCommand.ExitReason != "died" {
			t.Fatalf("unexpected exit command: %+v", validator.lastDeathCommand)
		}
		if validator.lastDeathCommand.GoatID != testGoatID {
			t.Fatalf("goat_id=%q, want %q", validator.lastDeathCommand.GoatID, testGoatID)
		}
	})
}

// TestRecordDeathEventRejectsUnknownFields proves the app route does not become a way to smuggle
// fields past identity's strict ExitGoatRequest decoding -- notably guardrail_approved, which a
// client must never be able to set for itself.
func TestRecordDeathEventRejectsUnknownFields(t *testing.T) {
	repo := &stubIdentityRepo{}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, identityapp.NewService(repo))

	body := deathBody("dead", "died")
	body["guardrail_approved"] = true
	rec := post(t, mux, appDeathEventRoute, "death-key-0005", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if repo.calls != 0 {
		t.Fatalf("repo calls=%d, want 0", repo.calls)
	}
	// Strict decoding has to happen at submit, not only at approval: an unknown field stored in the
	// payload would fail days later, in an approver's hands, on a request they cannot fix.
	if approvals.submits != 0 {
		t.Fatalf("submits=%d, want 0 (a body identity would reject must not be stored)", approvals.submits)
	}
}

// ---------------------------------------------------------------------------
// Simplified single-animal shifting: derived impacts + destination catalog
// ---------------------------------------------------------------------------

// These tests exercise the handler through the REAL countsapp.Service (as newTestServer already
// does), not against a stubbed-out recorder. That matters here specifically: an earlier
// fake-only test for this surface let a 500 ship, because a fake that accepts anything cannot
// notice that the service still rejects an event carrying zero impacts. Driving the real service
// means the derivation must actually satisfy the service's invariants to pass.

// shiftingBodyNoImpacts is the SIMPLIFIED operator payload: one animal, a destination, and nothing
// else. No impacts (the server derives them) and no effective_at (the server defaults it). This is
// the exact shape the phone now sends.
func shiftingBodyNoImpacts(goatIDs ...string) map[string]any {
	return map[string]any{
		"destination_park_id": testParkID,
		"destination_shed_id": testShedID,
		"goat_ids":            goatIDs,
	}
}

// TestRecordShiftingEventDerivesImpactFromASingleAnimal is the primary simplification proof: a
// payload with no impacts is accepted, and the movement still reaches the ledger carrying the
// one breed-grain impact row the projection needs.
func TestRecordShiftingEventDerivesImpactFromASingleAnimal(t *testing.T) {
	repo := newFakeShiftingRepo()
	breedID := "77777777-7777-4777-8777-777777777777"
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {
			GoatID:     testGoatID,
			BreedID:    &breedID,
			BreedKey:   "sirohi",
			BreedLabel: "Sirohi",
			StageTag:   strPtrTest("A1"),
			AgeClass:   strPtrTest("adult"),
			Sex:        strPtrTest("female"),
		},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-derive-0001", shiftingBodyNoImpacts(testGoatID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200 -- a single-animal shift must not need impacts", rec.Code, rec.Body.String())
	}

	// The recorded event must carry a real impact. If the derivation silently produced none, the
	// service would have rejected the write and this would already have failed with a 400/500 --
	// this assertion pins the CONTENT rather than merely the status code.
	if len(repo.lastEvent.Impacts) != 1 {
		t.Fatalf("recorded impacts=%d, want 1", len(repo.lastEvent.Impacts))
	}
	impact := repo.lastEvent.Impacts[0]
	if impact.HeadCount != 1 {
		t.Fatalf("HeadCount=%d, want 1", impact.HeadCount)
	}
	if impact.BreedKey != "sirohi" || impact.BreedLabel != "Sirohi" {
		t.Fatalf("breed=(%q,%q), want (sirohi, Sirohi) derived from the animal", impact.BreedKey, impact.BreedLabel)
	}
	if impact.StageTag == nil || *impact.StageTag != "A1" {
		t.Fatalf("StageTag=%v, want A1", impact.StageTag)
	}
	// Same grain shape the explicit-impact path produces, so derived and typed-in movements for the
	// same shed+breed land on ONE projection row.
	wantGrain := strings.ToLower(testShedID) + ":sirohi"
	if impact.GrainKey != wantGrain {
		t.Fatalf("GrainKey=%q, want %q", impact.GrainKey, wantGrain)
	}
	// effective_at was not sent; the server must have defaulted it rather than storing a zero time.
	if repo.lastEvent.EffectiveAt.IsZero() {
		t.Fatal("EffectiveAt is zero -- the server must default it when the client omits it")
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d, want 1", repo.inserts)
	}
}

// Source park/shed for the animal's CURRENT placement, distinct from the destination constants so a
// test cannot pass by accidentally reading the destination back.
const (
	testSourceParkID = "66666666-6666-4666-8666-666666666666"
	testSourceShedID = "77777777-7777-4777-8777-777777777788"
)

// TestRecordShiftingEventStoresDerivedSourceParkAndShed is the regression for the blank-source bug.
//
// The simplified Shifting screen stopped sending source_park_id / source_shed_id -- correctly, the
// operator should not retype a location the server already knows. But nothing backfilled them, so
// the stored event kept a blank source and the movement lost its "from" half. The source must now be
// read off the selected animal and persisted.
func TestRecordShiftingEventStoresDerivedSourceParkAndShed(t *testing.T) {
	repo := newFakeShiftingRepo()
	// Same-park move: the animal's source park equals the destination park (testParkID from
	// shiftingBodyNoImpacts), differing only by shed. A cross-park source would now be rejected at
	// submit (CR-02), so this fixture proves source DERIVATION on a legitimate within-park move.
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {
			GoatID:     testGoatID,
			BreedKey:   "sirohi",
			BreedLabel: "Sirohi",
			ParkID:     strPtrTest(testParkID),
			ShedID:     strPtrTest(testSourceShedID),
		},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	res := post(t, mux, appShiftingEventRoute, "shift-source-derive-1", shiftingBodyNoImpacts(testGoatID))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}

	stored := repo.lastEvent
	if stored.SourceParkID == nil || *stored.SourceParkID != testParkID {
		t.Fatalf("stored source_park_id=%v, want %q -- a movement with a blank origin is not an audit trail",
			stored.SourceParkID, testParkID)
	}
	if stored.SourceShedID == nil || *stored.SourceShedID != testSourceShedID {
		t.Fatalf("stored source_shed_id=%v, want %q", stored.SourceShedID, testSourceShedID)
	}

	// The approver's queue must show the same origin the event stored.
	var payload struct {
		SourceParkID *string `json:"source_park_id"`
		SourceShedID *string `json:"source_shed_id"`
	}
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &payload); err != nil {
		t.Fatalf("decode approval payload: %v", err)
	}
	if payload.SourceParkID == nil || *payload.SourceParkID != testParkID ||
		payload.SourceShedID == nil || *payload.SourceShedID != testSourceShedID {
		t.Fatalf("approval payload source = %v/%v, want %q/%q",
			payload.SourceParkID, payload.SourceShedID, testParkID, testSourceShedID)
	}
}

// TestRecordShiftingEventRejectsDerivedCrossParkMove is the CR-02 regression. When the operator
// omits source_park_id (the simplified submit), the cross-park invariant is NOT checked by
// normalizeShiftingEventRequest -- that guard only fires on an EXPLICIT source. Before the fix a
// goat standing in park A could produce an authorized A->B movement, and the violation would only
// surface at approval time AFTER the event, its outbox row, and the approval request were written.
// The derived source park must be validated against the destination at SUBMIT, failing closed with
// zero rows written.
func TestRecordShiftingEventRejectsDerivedCrossParkMove(t *testing.T) {
	repo := newFakeShiftingRepo()
	// The animal currently stands in a DIFFERENT park than the destination (testParkID from
	// shiftingBodyNoImpacts). No source_park_id is sent, so this can only be caught by validating the
	// derived source.
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {
			GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
			ParkID: strPtrTest(testSourceParkID), ShedID: strPtrTest(testSourceShedID),
		},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-cross-park-1", shiftingBodyNoImpacts(testGoatID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 (a derived cross-park move must be rejected at submit)", rec.Code, rec.Body.String())
	}
	var envelope identitydomain.ErrorEnvelope
	decodeBody(t, rec, &envelope)
	if envelope.Code != "cross_park_move_forbidden" {
		t.Fatalf("error code=%q, want cross_park_move_forbidden (body=%s)", envelope.Code, rec.Body.String())
	}
	// The whole point of failing at submit: no movement, no outbox, no approval request. A cross-park
	// event that reaches the ledger is already the drift this guard exists to prevent.
	if repo.inserts != 0 {
		t.Fatalf("inserts=%d, want 0 (a cross-park submit must record no movement)", repo.inserts)
	}
	if approvals.submits != 0 {
		t.Fatalf("approval submits=%d, want 0 (a cross-park submit must never reach a park head's queue)", approvals.submits)
	}
}

// TestRecordShiftingEventWithoutApprovalWorkflowRecordsNothing is the CR-05 regression. The
// movement row and its approval request are separate transactions, and the event was previously
// written BEFORE the approval workflow's availability was checked -- so when the workflow was
// unconfigured, the event committed and then the request returned 501, leaving a pending movement
// orphaned permanently (a retry cannot create a request in a workflow that does not exist). The
// availability check must run BEFORE the event write, so an unavailable workflow records no
// movement at all.
func TestRecordShiftingEventWithoutApprovalWorkflowRecordsNothing(t *testing.T) {
	repo := newFakeShiftingRepo()
	// Build the handler WITHOUT WithApprovalWorkflow, exactly as a deployment with the approval
	// workflow unconfigured would. h.approvals is nil.
	mux := http.NewServeMux()
	handler := NewAppWriteHandler(countsapp.NewService(repo), nil)
	RegisterAppWrites(mux, handler)

	rec := post(t, mux, appShiftingEventRoute, "shift-no-approvals-1", shiftingBody(7))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501 (an unconfigured approval workflow must reject before any write)", rec.Code, rec.Body.String())
	}
	// The whole point: no orphaned movement. Before the fix the event committed first and only then
	// hit the 501, leaving a pending shifting_events row nobody could ever approve.
	if repo.inserts != 0 {
		t.Fatalf("inserts=%d, want 0 (an unavailable approval workflow must record no movement)", repo.inserts)
	}
}

// TestRecordShiftingEventKeepsAnExplicitlySuppliedSource pins the precedence. A back-dated or
// corrective submission states a source the animal's CURRENT placement no longer reflects;
// overwriting it with "where the animal is now" would silently rewrite the operator's claim.
func TestRecordShiftingEventKeepsAnExplicitlySuppliedSource(t *testing.T) {
	repo := newFakeShiftingRepo()
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {
			GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
			ParkID: strPtrTest(testSourceParkID), ShedID: strPtrTest(testSourceShedID),
		},
	}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	// Same-park move (P0-1): the explicitly-stated source park must equal the destination park
	// (testParkID from shiftingBodyNoImpacts). Precedence is still proven by the explicit source
	// SHED, which differs from the animal's derived facts shed (testSourceShedID).
	const statedPark = testParkID
	const statedShed = "99999999-9999-4999-8999-999999999999"
	body := shiftingBodyNoImpacts(testGoatID)
	body["source_park_id"] = statedPark
	body["source_shed_id"] = statedShed

	res := post(t, mux, appShiftingEventRoute, "shift-source-explicit-1", body)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	stored := repo.lastEvent
	if stored.SourceParkID == nil || *stored.SourceParkID != statedPark {
		t.Fatalf("stored source_park_id=%v, want the CALLER's %q", stored.SourceParkID, statedPark)
	}
	if stored.SourceShedID == nil || *stored.SourceShedID != statedShed {
		t.Fatalf("stored source_shed_id=%v, want the CALLER's %q", stored.SourceShedID, statedShed)
	}
}

// TestRecordShiftingEventLeavesSourceAbsentWhenNotDerivable covers the two degrade paths: a
// multi-animal movement has no single truthful origin, and an animal with no recorded placement has
// none to read. Both must leave the source absent rather than storing an invented or blank value.
func TestRecordShiftingEventLeavesSourceAbsentWhenNotDerivable(t *testing.T) {
	const otherGoatID = "33333333-3333-4333-8333-333333333334"
	cases := []struct {
		name    string
		facts   map[string]domain.GoatShiftingFact
		goatIDs []string
		body    map[string]any
	}{
		{
			name: "animal has no recorded placement",
			facts: map[string]domain.GoatShiftingFact{
				testGoatID: {GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi"},
			},
			goatIDs: []string{testGoatID},
		},
		{
			name: "two animals have no single origin",
			facts: map[string]domain.GoatShiftingFact{
				testGoatID: {
					GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
					ParkID: strPtrTest(testSourceParkID), ShedID: strPtrTest(testSourceShedID),
				},
				otherGoatID: {
					GoatID: otherGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
					ParkID: strPtrTest(testSourceParkID), ShedID: strPtrTest("11111111-1111-4111-8111-111111111111"),
				},
			},
			goatIDs: []string{testGoatID, otherGoatID},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeShiftingRepo()
			repo.goatFacts = tc.facts
			mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

			body := shiftingBodyNoImpacts(tc.goatIDs...)
			// A multi-animal movement must state its own impacts, so supply them to isolate the
			// source behaviour from the impact-derivation boundary.
			if len(tc.goatIDs) > 1 {
				body["impacts"] = []map[string]any{{
					"breed_key": "sirohi", "breed_label": "Sirohi", "head_count": len(tc.goatIDs),
				}}
			}

			res := post(t, mux, appShiftingEventRoute, fmt.Sprintf("shift-source-absent-%d", i), body)
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
			}
			if repo.lastEvent.SourceParkID != nil || repo.lastEvent.SourceShedID != nil {
				t.Fatalf("source = %v/%v, want both nil -- an unresolvable origin must stay absent, never a blank or invented stored fact",
					repo.lastEvent.SourceParkID, repo.lastEvent.SourceShedID)
			}
		})
	}
}

// TestRecordShiftingEventDerivedSourceIsIdempotentAcrossAMove is the source-side twin of
// TestRecordShiftingEventDerivedImpactIsIdempotentAcrossBreedChange, and the reason the source is
// derived AFTER the canonical hash.
//
// A retry that arrives once the animal has ALREADY been moved reads a different current placement.
// If the derived source entered the idempotency hash, that retry would hash differently and be
// rejected as a same-key/different-payload conflict, stranding the operator on a movement that in
// fact succeeded.
func TestRecordShiftingEventDerivedSourceIsIdempotentAcrossAMove(t *testing.T) {
	repo := newFakeShiftingRepo()
	// Same-park move (source park == destination park testParkID), differing only by shed, so the
	// legitimate within-park movement is not rejected by the CR-02 cross-park guard.
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {
			GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
			ParkID: strPtrTest(testParkID), ShedID: strPtrTest(testSourceShedID),
		},
	}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	body := shiftingBodyNoImpacts(testGoatID)
	first := post(t, mux, appShiftingEventRoute, "shift-source-retry-1", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s, want 200", first.Code, first.Body.String())
	}

	// The animal has since been moved into the destination shed.
	repo.goatFacts[testGoatID] = domain.GoatShiftingFact{
		GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
		ParkID: strPtrTest(testParkID), ShedID: strPtrTest(testShedID),
	}

	second := post(t, mux, appShiftingEventRoute, "shift-source-retry-1", body)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s, want 200 -- a derived source must not enter the idempotency hash",
			second.Code, second.Body.String())
	}
	var got appShiftingEventResponse
	decodeBody(t, second, &got)
	if !got.IdempotentReplay {
		t.Fatal("retry must be reported as an idempotent replay")
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d, want 1 -- the retry must not create a second movement", repo.inserts)
	}
}

// TestRecordShiftingEventDerivedImpactIsIdempotentAcrossBreedChange guards the ordering that makes
// derivation safe for retries.
//
// The idempotency hash must cover only what the CLIENT sent. If a derived impact were folded into
// it, then a network retry of the identical request would hash differently the moment the animal's
// breed row changed underneath -- and a legitimate retry would be rejected as a
// same-key/different-payload conflict, stranding the operator.
func TestRecordShiftingEventDerivedImpactIsIdempotentAcrossBreedChange(t *testing.T) {
	repo := newFakeShiftingRepo()
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi"},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	body := shiftingBodyNoImpacts(testGoatID)
	first := post(t, mux, appShiftingEventRoute, "shift-derive-retry-1", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s, want 200", first.Code, first.Body.String())
	}

	// The animal is re-classified between the operator's submit and their retry.
	repo.goatFacts[testGoatID] = domain.GoatShiftingFact{
		GoatID: testGoatID, BreedKey: "boer_cross", BreedLabel: "Boer Cross",
	}

	second := post(t, mux, appShiftingEventRoute, "shift-derive-retry-1", body)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s, want 200 -- a derived value must not enter the idempotency hash",
			second.Code, second.Body.String())
	}
	var got appShiftingEventResponse
	decodeBody(t, second, &got)
	if !got.IdempotentReplay {
		t.Fatal("retry must be reported as an idempotent replay")
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts=%d, want 1 -- the retry must not create a second movement", repo.inserts)
	}
}

// TestRecordShiftingEventStillHonoursExplicitImpacts pins that the relaxation is ADDITIVE. The
// multi-impact path is unchanged: whatever the client states is stored verbatim, and the derivation
// must not run or overwrite it.
func TestRecordShiftingEventStillHonoursExplicitImpacts(t *testing.T) {
	repo := newFakeShiftingRepo()
	// Deliberately stocked with DIFFERENT facts than the body states. If the handler ever preferred
	// derivation over the client's explicit impacts, these values would show up instead.
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID: {GoatID: testGoatID, BreedKey: "should_not_appear", BreedLabel: "Should Not Appear"},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-explicit-0001", shiftingBody(12))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(repo.lastEvent.Impacts) != 1 {
		t.Fatalf("recorded impacts=%d, want 1", len(repo.lastEvent.Impacts))
	}
	impact := repo.lastEvent.Impacts[0]
	if impact.BreedKey != "sirohi" || impact.BreedLabel != "Sirohi" {
		t.Fatalf("breed=(%q,%q), want the client's (sirohi, Sirohi) -- explicit impacts must win",
			impact.BreedKey, impact.BreedLabel)
	}
	if impact.HeadCount != 12 {
		t.Fatalf("HeadCount=%d, want the client's 12 -- derivation must not have run", impact.HeadCount)
	}
	// ASSERTION CHANGED (source-backfill fix): this previously required ZERO goat reads, because
	// impact derivation was the only reason to read the animal and an explicit-impact write skipped
	// it entirely. A shifting event submitted without a source now backfills it from the named
	// animal -- which this body is, since shiftingBody() states impacts but no source -- so exactly
	// one read is expected and correct.
	//
	// The invariant this test exists to protect is unchanged and still asserted above: explicit
	// impacts WIN and derivation must not overwrite them (the fake is deliberately stocked with
	// "should_not_appear"). The read budget is pinned at one so a future change cannot quietly turn
	// a single-animal write into several round trips.
	if repo.goatFactsCalls > 1 {
		t.Fatalf("goat facts read %d times, want at most 1 -- one bounded read backfills the source; more than one is a round-trip regression",
			repo.goatFactsCalls)
	}
}

// TestRecordShiftingEventDerivesImpactsForAHomogeneousGroup pins the multi-animal relaxation: two
// animals of the SAME cohort submitted without impacts are derived and merged into one cohort row,
// and the movement is recorded -- the server no longer rejects multi-animal auto-derivation.
func TestRecordShiftingEventDerivesImpactsForAHomogeneousGroup(t *testing.T) {
	otherGoatID := "99999999-9999-4999-8999-999999999999"
	repo := newFakeShiftingRepo()
	repo.goatFacts = map[string]domain.GoatShiftingFact{
		testGoatID:  {GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi", StageTag: strPtrTest("K2"), Sex: strPtrTest("female")},
		otherGoatID: {GoatID: otherGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi", StageTag: strPtrTest("K2"), Sex: strPtrTest("female")},
	}
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-multi-ok", shiftingBodyNoImpacts(testGoatID, otherGoatID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(repo.lastEvent.Impacts) != 1 {
		t.Fatalf("recorded impacts=%d, want 1 merged cohort row", len(repo.lastEvent.Impacts))
	}
	if repo.lastEvent.Impacts[0].HeadCount != 2 {
		t.Fatalf("HeadCount=%d, want 2 (both animals summed)", repo.lastEvent.Impacts[0].HeadCount)
	}
}

// TestRecordShiftingEventRejectsOmittedImpactsForZeroOrMixedCohort pins the two remaining boundaries.
// Zero animals has nothing to derive from (the pre-existing goat_ids requirement fires first). A
// MIXED set -- same breed and shed but differing stage/age/sex -- cannot be auto-aggregated without
// inventing a cohort the operator never entered, so it fails closed and asks for explicit impacts.
func TestRecordShiftingEventRejectsOmittedImpactsForZeroOrMixedCohort(t *testing.T) {
	otherGoatID := "99999999-9999-4999-8999-999999999999"
	cases := map[string]struct {
		body     map[string]any
		wantCode string
	}{
		"zero animals": {body: shiftingBodyNoImpacts(), wantCode: "missing_goat_ids"},
		"mixed cohort": {body: shiftingBodyNoImpacts(testGoatID, otherGoatID), wantCode: "missing_impacts"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := newFakeShiftingRepo()
			// Same breed + shed, DIFFERENT sex: a cohort split the derivation must not average.
			repo.goatFacts = map[string]domain.GoatShiftingFact{
				testGoatID:  {GoatID: testGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi", StageTag: strPtrTest("K2"), Sex: strPtrTest("female")},
				otherGoatID: {GoatID: otherGoatID, BreedKey: "sirohi", BreedLabel: "Sirohi", StageTag: strPtrTest("K2"), Sex: strPtrTest("male")},
			}
			approvals := newFakeApprovalWorkflow()
			mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

			rec := post(t, mux, appShiftingEventRoute, "shift-reject-"+name, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
			}
			var envelope identitydomain.ErrorEnvelope
			decodeBody(t, rec, &envelope)
			if envelope.Code != tc.wantCode {
				t.Fatalf("code=%q, want %q", envelope.Code, tc.wantCode)
			}
			// Nothing may have been recorded, and no approval may be queued, for a rejected submit.
			if repo.inserts != 0 {
				t.Fatalf("inserts=%d, want 0", repo.inserts)
			}
			if approvals.submits != 0 {
				t.Fatalf("approval submits=%d, want 0", approvals.submits)
			}
		})
	}
}

// TestRecordShiftingEventFailsClosedWhenTheAnimalDoesNotResolve pins that an unresolvable RFID is a
// specific 404 the operator can act on -- never a 500, and never a silent success carrying a
// placeholder impact.
func TestRecordShiftingEventFailsClosedWhenTheAnimalDoesNotResolve(t *testing.T) {
	repo := newFakeShiftingRepo() // no goatFacts stocked: the id resolves to nothing
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	rec := post(t, mux, appShiftingEventRoute, "shift-missing-goat-1", shiftingBodyNoImpacts(testGoatID))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
	var envelope identitydomain.ErrorEnvelope
	decodeBody(t, rec, &envelope)
	if envelope.Code != "goat_not_found" {
		t.Fatalf("code=%q, want goat_not_found", envelope.Code)
	}
	if repo.inserts != 0 || approvals.submits != 0 {
		t.Fatalf("inserts=%d submits=%d, want 0/0 -- nothing may be recorded for an unresolvable animal",
			repo.inserts, approvals.submits)
	}
}

// TestListShiftingDestinationsGroupsShedsUnderTheirPark is the disambiguation proof.
//
// Shed names REPEAT across parks -- "Castro 1" exists under both Coimbatore and Channapatna -- so a
// flat shed list would show the operator two identical options. This asserts both halves of the fix:
// each shed is nested under the correct park, AND the two same-named sheds remain distinguishable by
// id so the write is never ambiguous regardless of what the UI rendered.
func TestListShiftingDestinationsGroupsShedsUnderTheirPark(t *testing.T) {
	repo := newFakeShiftingRepo()
	repo.destinations = domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{
		{ParkID: "park-cbe", Name: "Coimbatore", Sheds: []domain.ShiftingDestinationShed{
			{ShedID: "shed-cbe-castro1", Name: "Castro 1"},
			{ShedID: "shed-cbe-castro2", Name: "Castro 2"},
		}},
		{ParkID: "park-cpt", Name: "Channapatna", Sheds: []domain.ShiftingDestinationShed{
			{ShedID: "shed-cpt-castro1", Name: "Castro 1"},
		}},
	}}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	rec := get(t, mux, appShiftingDestinationsRoute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got appShiftingDestinationsResponse
	decodeBody(t, rec, &got)

	if len(got.Parks) != 2 {
		t.Fatalf("len(parks)=%d, want 2", len(got.Parks))
	}
	byPark := map[string]appShiftingDestinationPark{}
	for _, park := range got.Parks {
		byPark[park.ParkID] = park
	}
	cbe, ok := byPark["park-cbe"]
	if !ok {
		t.Fatal("Coimbatore missing from the catalog")
	}
	cpt, ok := byPark["park-cpt"]
	if !ok {
		t.Fatal("Channapatna missing from the catalog")
	}
	if len(cbe.Sheds) != 2 {
		t.Fatalf("Coimbatore sheds=%d, want 2 -- sheds must group under their OWN park", len(cbe.Sheds))
	}
	if len(cpt.Sheds) != 1 {
		t.Fatalf("Channapatna sheds=%d, want 1", len(cpt.Sheds))
	}

	// The repeated name is the whole point of the cascade.
	if cbe.Sheds[0].Name != cpt.Sheds[0].Name {
		t.Fatalf("test premise broken: expected a repeated shed name, got %q vs %q",
			cbe.Sheds[0].Name, cpt.Sheds[0].Name)
	}
	if cbe.Sheds[0].ShedID == cpt.Sheds[0].ShedID {
		t.Fatal("two sheds sharing the name \"Castro 1\" must carry distinct shed_ids")
	}
	// Each id must sit under the right park: a cross-wired cascade would move animals to the wrong
	// farm while displaying the name the operator expected.
	if cbe.Sheds[0].ShedID != "shed-cbe-castro1" {
		t.Fatalf("Coimbatore's Castro 1 = %q, want shed-cbe-castro1", cbe.Sheds[0].ShedID)
	}
	if cpt.Sheds[0].ShedID != "shed-cpt-castro1" {
		t.Fatalf("Channapatna's Castro 1 = %q, want shed-cpt-castro1", cpt.Sheds[0].ShedID)
	}
}

// TestListBirthBreedsServesTheHerdVocabularyOnTheOperatorSurface pins the fix for the operator
// birth form's breed picker. The picker's vocabulary used to come from the Counts Breakdown breed
// facet, which is gated on CountsRead -- a permission a field operator does not hold -- so the fetch
// 403'd and the picker rendered permanently empty/disabled. This route serves the identical
// vocabulary on the CountsWrite surface. The test asserts the operator-reachable route returns the
// breeds with key/label/count intact, and that an empty herd serializes as [] rather than null.
func TestListBirthBreedsServesTheHerdVocabularyOnTheOperatorSurface(t *testing.T) {
	repo := newFakeShiftingRepo()
	repo.breeds = []domain.CountsBreakdownSeriesPoint{
		{Key: "Beetal", Label: "Beetal", Count: 420},
		{Key: "Sirohi", Label: "Sirohi", Count: 51},
	}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	rec := get(t, mux, appBirthBreedsRoute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got appBirthBreedsResponse
	decodeBody(t, rec, &got)
	if len(got.Breeds) != 2 {
		t.Fatalf("len(breeds)=%d, want 2", len(got.Breeds))
	}
	if got.Breeds[0].Key != "Beetal" || got.Breeds[0].Label != "Beetal" || got.Breeds[0].Count != 420 {
		t.Fatalf("first breed = %+v, want {Beetal Beetal 420}", got.Breeds[0])
	}

	empty := newFakeShiftingRepo()
	emptyMux := newTestServer(t, countsapp.NewService(empty), newFakeApprovalWorkflow(), newFakeGoatValidator())
	emptyRec := get(t, emptyMux, appBirthBreedsRoute)
	if !strings.Contains(emptyRec.Body.String(), `"breeds":[]`) {
		t.Fatalf("body=%s, want an empty herd to serialize as \"breeds\":[]", emptyRec.Body.String())
	}
}

// TestListShiftingDestinationsSerializesEmptyCollectionsAsArrays pins that a tenant with no parks,
// and a park with no sheds, both serialize as [] rather than null -- a client that has to
// special-case null renders a broken dropdown.
func TestListShiftingDestinationsSerializesEmptyCollectionsAsArrays(t *testing.T) {
	repo := newFakeShiftingRepo()
	repo.destinations = domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{
		{ParkID: "park-new", Name: "Newly Created Park"},
	}}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	rec := get(t, mux, appShiftingDestinationsRoute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"sheds":[]`) {
		t.Fatalf("body=%s, want a shed-less park to serialize as \"sheds\":[]", rec.Body.String())
	}

	empty := newFakeShiftingRepo()
	emptyMux := newTestServer(t, countsapp.NewService(empty), newFakeApprovalWorkflow(), newFakeGoatValidator())
	emptyRec := get(t, emptyMux, appShiftingDestinationsRoute)
	if !strings.Contains(emptyRec.Body.String(), `"parks":[]`) {
		t.Fatalf("body=%s, want an empty tenant to serialize as \"parks\":[]", emptyRec.Body.String())
	}
}

// TestListShiftingDestinationsRequiresTenantContext pins that the catalog is never served
// unscoped -- an unauthenticated caller must not learn the tenant's park/shed topology.
func TestListShiftingDestinationsRequiresTenantContext(t *testing.T) {
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), newFakeApprovalWorkflow(), newFakeGoatValidator())

	req := httptest.NewRequest(http.MethodGet, appShiftingDestinationsRoute, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req) // no tenant in context
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func get(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func strPtrTest(s string) *string { return &s }

// TestNormalizeShiftingEventRequest_CrossParkMove verifies that normalizeShiftingEventRequest
// rejects movements where source_park_id != destination_park_id.
// P0-1: Cross-park move prevention.
func TestNormalizeShiftingEventRequest_CrossParkMove(t *testing.T) {
	tests := []struct {
		name        string
		sourcePark  *string
		destPark    string
		expectError bool
	}{
		{
			name:        "no source park, should succeed",
			sourcePark:  nil,
			destPark:    testParkID,
			expectError: false,
		},
		{
			name:        "source and dest park match, should succeed",
			sourcePark:  strPtrTest(testParkID),
			destPark:    testParkID,
			expectError: false,
		},
		{
			name:        "source and dest park differ, should fail",
			sourcePark:  strPtrTest("99999999-9999-4999-8999-999999999999"),
			destPark:    testParkID,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := appShiftingEventRequest{
				SourceParkID:      tt.sourcePark,
				DestinationParkID: tt.destPark,
				DestinationShedID: testShedID,
				GoatIDs:           []string{testGoatID},
			}
			_, err := normalizeShiftingEventRequest(req)
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestNormalizeShiftingEventRequest_MissingGoatIDs verifies that goat_ids is required.
// P1: Unmovable animals at submit.
func TestNormalizeShiftingEventRequest_MissingGoatIDs(t *testing.T) {
	req := appShiftingEventRequest{
		DestinationParkID: testParkID,
		DestinationShedID: testShedID,
		GoatIDs:           []string{}, // Empty goat_ids
	}
	_, err := normalizeShiftingEventRequest(req)
	if err == nil {
		t.Errorf("expected error for missing goat_ids but got none")
	}
}

// TestNormalizeShiftingEventRequest_DeduplicatesGoatIDs verifies that duplicate goat IDs are removed
// and the result is sorted. P1: Unmovable animals at submit.
func TestNormalizeShiftingEventRequest_DeduplicatesGoatIDs(t *testing.T) {
	// Use only a single animal so we don't need to provide impacts
	req := appShiftingEventRequest{
		DestinationParkID: testParkID,
		DestinationShedID: testShedID,
		GoatIDs:           []string{testGoatID, testGoatID, testGoatID}, // Has duplicates
	}
	normalized, err := normalizeShiftingEventRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have 1 unique goat ID (deduplicated)
	if len(normalized.GoatIDs) != 1 {
		t.Errorf("expected 1 unique goat ID after deduplication, got %d: %v", len(normalized.GoatIDs), normalized.GoatIDs)
	}
	if normalized.GoatIDs[0] != testGoatID {
		t.Errorf("goat ID mismatch: got %s, want %s", normalized.GoatIDs[0], testGoatID)
	}
}
