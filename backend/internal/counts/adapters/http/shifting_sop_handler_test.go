package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP (2026-09-16) at the HTTP edge: a raise pins the published (or echoed known) version
// and is judged against its raise card BEFORE anything is written; an identical retry replays even
// after a later publish added a required question; the legacy request shapes hash exactly as they
// did before the SOP fields existed; the approvals queue carries the raise capture.

// ShiftingEventByIdempotencyKey lets the fake repo answer the raise replay pre-check.
func (f *fakeShiftingRepo) ShiftingEventByIdempotencyKey(_ context.Context, _, idempotencyKey, fingerprint string) (string, bool, error) {
	stored, ok := f.byIdempotencyKey[idempotencyKey]
	if !ok {
		return "", false, nil
	}
	if stored != fingerprint {
		return "", false, ports.ErrIdempotencyConflict
	}
	return f.idByKey[idempotencyKey], true, nil
}

func (f *fakeShiftingRepo) ShiftingSOPPin(context.Context, string, string) (ports.ShiftingSOPPin, error) {
	return ports.ShiftingSOPPin{}, ports.ErrShiftingEventNotFound
}

func raiseRules(version int, required bool) domain.ShiftingRules {
	r := domain.SeededShiftingRules()
	r.Version = version
	if required {
		r.Raise.Questions = []authored.Question{{ID: "why", Kind: authored.QuestionText, Title: "Why move", Required: true}}
	}
	return r
}

// sopTestServer wires the real execution service (static rules) into the write handler.
func sopTestServer(t *testing.T, repo *fakeShiftingRepo, approvals *fakeApprovalWorkflow, rules *ports.StaticShiftingSOPRules) (*http.ServeMux, *countsapp.ShiftingExecutionService) {
	t.Helper()
	svc := countsapp.NewShiftingExecutionService(repo, func() time.Time { return time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) }).
		WithSOPRules(rules, repo)
	mux := http.NewServeMux()
	handler := NewAppWriteHandler(countsapp.NewService(repo), nil).
		WithApprovalWorkflow(approvals, newFakeGoatValidator()).
		WithShiftingExecutionWorkflow(svc)
	RegisterAppWrites(mux, handler)
	RegisterApprovals(mux, handler)
	RegisterShiftingExecution(mux, handler)
	return mux, svc
}

func TestRecordShiftingEventPinsPublishedVersion(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux, _ := sopTestServer(t, repo, approvals, &ports.StaticShiftingSOPRules{Published: raiseRules(3, false)})
	rec := post(t, mux, appShiftingEventRoute, "shift-sop-pin-1", shiftingBody(4))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastEvent.SOPVersion == nil || *repo.lastEvent.SOPVersion != 3 {
		t.Fatalf("pinned version = %v, want 3", repo.lastEvent.SOPVersion)
	}
	var payload map[string]any
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	capture, ok := payload["capture"].(map[string]any)
	if !ok || capture["version_label"] != "SOP v3" {
		t.Fatalf("approval payload capture = %v", payload["capture"])
	}
}

func TestRecordShiftingEventPinsEchoedKnownVersion(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	rules := &ports.StaticShiftingSOPRules{Published: raiseRules(2, true), ByVersion: map[int]domain.ShiftingRules{1: raiseRules(1, false)}}
	mux, _ := sopTestServer(t, repo, approvals, rules)
	body := shiftingBody(4)
	body["sop_version"] = 1
	body["answers"] = map[string]any{}
	rec := post(t, mux, appShiftingEventRoute, "shift-sop-pin-2", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s (v1 asks nothing; the phone echoed v1)", rec.Code, rec.Body.String())
	}
	if repo.lastEvent.SOPVersion == nil || *repo.lastEvent.SOPVersion != 1 {
		t.Fatalf("pinned version = %v, want the echoed 1", repo.lastEvent.SOPVersion)
	}
	// An echoed version the farm never published is refused by name, writing nothing.
	body["sop_version"] = 9
	rec = post(t, mux, appShiftingEventRoute, "shift-sop-pin-3", body)
	if rec.Code != http.StatusConflict || repo.inserts != 1 {
		t.Fatalf("status=%d inserts=%d body=%s", rec.Code, repo.inserts, rec.Body.String())
	}
	var env struct {
		Code string `json:"code"`
	}
	decodeBody(t, rec, &env)
	if env.Code != "shifting_sop_version_unknown" {
		t.Fatalf("code = %q", env.Code)
	}
}

func TestRecordShiftingEventRefusesRequiredRaiseAnswerWritingNothing(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux, _ := sopTestServer(t, repo, approvals, &ports.StaticShiftingSOPRules{Published: raiseRules(2, true)})
	body := shiftingBody(4)
	body["answers"] = map[string]any{} // a NEW app: judged strictly
	rec := post(t, mux, appShiftingEventRoute, "shift-sop-answer-1", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Code     string `json:"code"`
		Question string `json:"question"`
	}
	decodeBody(t, rec, &env)
	if env.Code != "shifting_answer_invalid" || env.Question != "why" {
		t.Fatalf("envelope = %+v", env)
	}
	if repo.inserts != 0 || approvals.submits != 0 {
		t.Fatalf("a refused raise wrote: inserts=%d submits=%d", repo.inserts, approvals.submits)
	}
	// An OLDER app (no answers / proofs keys) is accepted, and the approver sees what it could not send.
	rec = post(t, mux, appShiftingEventRoute, "shift-sop-answer-2", shiftingBody(4))
	if rec.Code != http.StatusOK {
		t.Fatalf("older app refused: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Capture domain.CountsApprovalCapture `json:"capture"`
	}
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Capture.MissingNote != countsapp.NotCapturedOlderApp+": Why move" || len(payload.Capture.Rows) != 1 || payload.Capture.Rows[0].Value != countsapp.NotCapturedOlderApp {
		t.Fatalf("capture = %+v", payload.Capture)
	}
	// Answered: the row reads in farm words.
	body["answers"] = map[string]any{"why": "Overcrowded"}
	rec = post(t, mux, appShiftingEventRoute, "shift-sop-answer-3", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered raise refused: %d %s", rec.Code, rec.Body.String())
	}
	var answered struct {
		Capture domain.CountsApprovalCapture `json:"capture"`
	}
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &answered); err != nil {
		t.Fatal(err)
	}
	if len(answered.Capture.Rows) != 1 || answered.Capture.Rows[0].Label != "Why move" || answered.Capture.Rows[0].Value != "Overcrowded" || answered.Capture.MissingNote != "" {
		t.Fatalf("capture = %+v", answered.Capture)
	}
	if string(repo.lastEvent.RaiseSOPAnswers["why"]) != `"Overcrowded"` || len(repo.lastEvent.RaiseCaptureEvidence) == 0 {
		t.Fatalf("event raise fields = %v / %s", repo.lastEvent.RaiseSOPAnswers, repo.lastEvent.RaiseCaptureEvidence)
	}
}

func TestIdenticalRaiseRetryReplaysAfterPublishAddedRequiredQuestion(t *testing.T) {
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	rules := &ports.StaticShiftingSOPRules{Published: raiseRules(1, false)}
	mux, _ := sopTestServer(t, repo, approvals, rules)
	body := shiftingBody(4)
	body["answers"] = map[string]any{}
	first := post(t, mux, appShiftingEventRoute, "shift-sop-retry", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	// v2 publishes a required question between the phone's attempts.
	rules.Published = raiseRules(2, true)
	retry := post(t, mux, appShiftingEventRoute, "shift-sop-retry", body)
	if retry.Code != http.StatusOK {
		t.Fatalf("identical retry after publish: %d %s", retry.Code, retry.Body.String())
	}
	var got appShiftingEventResponse
	decodeBody(t, retry, &got)
	if !got.IdempotentReplay || repo.inserts != 1 || approvals.submits != 1 {
		t.Fatalf("replay=%v inserts=%d submits=%d", got.IdempotentReplay, repo.inserts, approvals.submits)
	}
	// A DIFFERENT body on the same key is still a conflict, not a fresh judgement.
	body["comment"] = "changed"
	conflict := post(t, mux, appShiftingEventRoute, "shift-sop-retry", body)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("same key different payload: %d %s", conflict.Code, conflict.Body.String())
	}
}

// The canonical hashes of the LEGACY shapes are byte-identical to what the pre-SOP handler
// produced. The goldens below were captured from the handler at the base commit (before the SOP
// fields existed) for exactly these bodies; a changed literal means an installed phone's retry
// would be refused as a same-key/different-payload conflict.
func TestLegacyRaiseAndCompleteFingerprintsUnchanged(t *testing.T) {
	const (
		goldenRaiseFingerprint    = "c45efdd82171f49ab05abc91f1ded0f7eed358ae24aec06bed737104cb847fbc"
		goldenRaisePayloadHash    = "c27891d89e12ca1d858ace5dd24255f17a8f47ed275120b4140328df4c521f47"
		goldenCompleteFingerprint = "065bcb8dcde96b18abf2f5e6a4cf9dc2027ded487c02af5b6dca561b24d66a1f"
	)
	repo := newFakeShiftingRepo()
	approvals := newFakeApprovalWorkflow()
	mux, _ := sopTestServer(t, repo, approvals, &ports.StaticShiftingSOPRules{Published: raiseRules(1, false)})
	if rec := post(t, mux, appShiftingEventRoute, "shift-sop-legacy-hash", shiftingBody(4)); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d %s", rec.Code, rec.Body.String())
	}
	if repo.lastEvent.RequestFingerprint != goldenRaiseFingerprint {
		t.Fatalf("legacy raise fingerprint changed: got %s want %s", repo.lastEvent.RequestFingerprint, goldenRaiseFingerprint)
	}
	if repo.lastEvent.PayloadHash != goldenRaisePayloadHash {
		t.Fatalf("legacy raise payload hash changed: got %s want %s", repo.lastEvent.PayloadHash, goldenRaisePayloadHash)
	}

	exec := &capturingExecution{}
	mux2 := http.NewServeMux()
	RegisterShiftingExecution(mux2, NewAppWriteHandler(countsapp.NewService(repo), nil).WithShiftingExecutionWorkflow(exec))
	legacyBody := map[string]any{"proof_ref": "v1", "feed_packing_proof_ref": "p1", "feed_given_proof_ref": "f1", "feed_config_fingerprint": "fp"}
	if rec := post(t, mux2, "/app/counts/shifting-events/ev1/complete", "complete-legacy", legacyBody); rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	if exec.last.RequestFingerprint != goldenCompleteFingerprint {
		t.Fatalf("legacy complete fingerprint changed: got %s want %s", exec.last.RequestFingerprint, goldenCompleteFingerprint)
	}
	if !exec.last.LegacyShape || exec.last.ProofRef != "v1" || exec.last.FeedGivenProofRef != "f1" {
		t.Fatalf("legacy input = %+v", exec.last)
	}
}

// A NEW app sending exactly the seeded triple as a proofs map hashes as the legacy triple, so the
// two shapes of one submission collapse onto one idempotent completion.
func TestSeededSlotMapFingerprintEqualsLegacyTriple(t *testing.T) {
	repo := newFakeShiftingRepo()
	exec := &capturingExecution{}
	mux := http.NewServeMux()
	RegisterShiftingExecution(mux, NewAppWriteHandler(countsapp.NewService(repo), nil).WithShiftingExecutionWorkflow(exec))
	legacyBody := map[string]any{"proof_ref": "v1", "feed_packing_proof_ref": "p1", "feed_given_proof_ref": "f1", "feed_config_fingerprint": "fp"}
	if rec := post(t, mux, "/app/counts/shifting-events/ev1/complete", "complete-a", legacyBody); rec.Code != http.StatusOK {
		t.Fatalf("legacy: %d %s", rec.Code, rec.Body.String())
	}
	legacyFP := exec.last.RequestFingerprint
	mapped := map[string]any{"proofs": map[string]string{
		domain.SlotShiftingVideo: "v1", domain.SlotShiftingPackingVideo: "p1", domain.SlotShiftingFeedingVideo: "f1",
	}, "feed_config_fingerprint": "fp"}
	if rec := post(t, mux, "/app/counts/shifting-events/ev1/complete", "complete-b", mapped); rec.Code != http.StatusOK {
		t.Fatalf("mapped: %d %s", rec.Code, rec.Body.String())
	}
	if exec.last.RequestFingerprint != legacyFP {
		t.Fatalf("seeded map fingerprint %s != legacy %s", exec.last.RequestFingerprint, legacyFP)
	}
	if exec.last.LegacyShape || exec.last.SOPProofs[domain.SlotShiftingVideo] != "v1" {
		t.Fatalf("mapped input = %+v", exec.last)
	}
	// An authored key changes the hash: it is a different submission.
	other := map[string]any{"proofs": map[string]string{domain.SlotShiftingVideo: "v1", "feed_clip": "f1"}, "feed_config_fingerprint": "fp"}
	if rec := post(t, mux, "/app/counts/shifting-events/ev1/complete", "complete-c", other); rec.Code != http.StatusOK {
		t.Fatalf("authored: %d %s", rec.Code, rec.Body.String())
	}
	if exec.last.RequestFingerprint == legacyFP {
		t.Fatal("an authored slot map must not hash as the legacy triple")
	}
}

func TestLegacyProofRequiredPrecheckOnlyForLegacyShape(t *testing.T) {
	repo := newFakeShiftingRepo()
	exec := &capturingExecution{}
	mux := http.NewServeMux()
	RegisterShiftingExecution(mux, NewAppWriteHandler(countsapp.NewService(repo), nil).WithShiftingExecutionWorkflow(exec))
	rec := post(t, mux, "/app/counts/shifting-events/ev1/complete", "complete-empty-legacy", map[string]any{"destination_tag": "x"})
	if rec.Code != http.StatusUnprocessableEntity || exec.calls != 0 {
		t.Fatalf("legacy proofless: status=%d calls=%d", rec.Code, exec.calls)
	}
	var env struct {
		Code string `json:"code"`
	}
	decodeBody(t, rec, &env)
	if env.Code != "proof_required" {
		t.Fatalf("code=%q", env.Code)
	}
	// A NEW app with an empty map reaches the service, which judges by slot name.
	rec = post(t, mux, "/app/counts/shifting-events/ev1/complete", "complete-empty-new", map[string]any{"proofs": map[string]string{}})
	if rec.Code != http.StatusOK || exec.calls != 1 {
		t.Fatalf("new-shaped empty: status=%d calls=%d body=%s", rec.Code, exec.calls, rec.Body.String())
	}
}

func TestShiftingApprovalListCarriesRaiseCaptureRowsAndMedia(t *testing.T) {
	repo := newFakeShiftingRepo()
	capture := domain.CountsApprovalCapture{VersionLabel: "SOP v2",
		Rows:  []domain.CountsApprovalCaptureRow{{Label: "Why move", Value: "Overcrowded", Group: "At raise"}},
		Media: []domain.CountsApprovalCaptureMedia{{ProofID: "r1", Label: "Pen photo", Kind: "photo"}}}
	payload, _ := json.Marshal(map[string]any{"shifting_event_id": "ev1", "destination_park_id": testParkID, "destination_shed_id": testShedID, "goat_ids": []string{testGoatID}, "capture": capture})
	approvals := &listingApprovalWorkflow{fakeApprovalWorkflow: newFakeApprovalWorkflow(), items: []domain.ApprovalRequestSummary{{
		ApprovalRequestID: "ar1", RequestType: domain.ApprovalRequestTypeShifting, Status: domain.ApprovalStatusPending,
		RaisedByUserID: testActorID, RaisedAt: time.Now(), Summary: payload,
	}}}
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
	rec := get(t, mux, appApprovalsRoute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			Capture *domain.CountsApprovalCapture `json:"capture"`
		} `json:"items"`
	}
	decodeBody(t, rec, &got)
	if len(got.Items) != 1 || got.Items[0].Capture == nil || got.Items[0].Capture.VersionLabel != "SOP v2" ||
		len(got.Items[0].Capture.Rows) != 1 || got.Items[0].Capture.Media[0].ProofID != "r1" {
		t.Fatalf("items = %+v", got.Items)
	}
}

// capturingExecution records CompleteShiftingInput without judging (the service's own tests own
// the judgement); the handler's fingerprint and shape detection are what is under test here.
type capturingExecution struct {
	last  countsapp.CompleteShiftingInput
	calls int
}

func (c *capturingExecution) Complete(_ context.Context, in countsapp.CompleteShiftingInput) (domain.ShiftingExecutionResult, bool, error) {
	c.calls++
	c.last = in
	return domain.ShiftingExecutionResult{ShiftingEventID: in.ShiftingEventID, EventStatus: domain.ShiftingEventStatusApplied}, false, nil
}

func (c *capturingExecution) Cancel(context.Context, countsapp.CancelShiftingInput) (domain.ShiftingExecutionResult, bool, error) {
	return domain.ShiftingExecutionResult{}, false, nil
}

func (c *capturingExecution) ListPendingExecution(context.Context, string, string, string, int, string) (domain.ShiftingExecutionPage, error) {
	return domain.ShiftingExecutionPage{}, nil
}

func (c *capturingExecution) JudgeRaise(_ context.Context, in countsapp.RaiseJudgeInput) (countsapp.RaiseJudgement, error) {
	return countsapp.RaiseJudgement{}, nil
}

func (c *capturingExecution) PublishedRaiseCard(context.Context, string) (domain.ShiftingCardRules, error) {
	return domain.SeededShiftingRules().RaiseCard(), nil
}

type listingApprovalWorkflow struct {
	*fakeApprovalWorkflow
	items []domain.ApprovalRequestSummary
}

func (l *listingApprovalWorkflow) ListPending(context.Context, string, string, []string, []string, int, string) (domain.ApprovalRequestPage, error) {
	return domain.ApprovalRequestPage{Items: l.items}, nil
}
