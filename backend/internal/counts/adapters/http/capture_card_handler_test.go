package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// handlerFor re-registers the mux's handler with the capture card seam: newTestServer returns
// only the mux, so the seam is wired by building the same handler again on a fresh mux.
func handlerFor(t *testing.T, mux *http.ServeMux) *AppWriteHandler {
	t.Helper()
	h, ok := handlersByMux[mux]
	if !ok {
		t.Fatal("mux was not built by newTestServer")
	}
	return h
}

// SOP capture card on the Add birth / Add death forms (maintainer decisions 4 and 7, 2026-09-16).

type fakeCaptureCards struct {
	judgeCalls []*domain.CaptureSubmission
	kinds      []string
	result     *domain.ApprovalCapture
	err        error
}

func (f *fakeCaptureCards) Card(context.Context, string, string) (countsapp.CaptureCardView, error) {
	return countsapp.CaptureCardView{Kind: "birth", SOPCode: "counts.birth", VersionID: "v-id", VersionLabel: "v2",
		Card: domain.CaptureCard{SchemaVersion: domain.CaptureSchemaVersion, Instruction: "Photograph the newborns."}}, nil
}

func (f *fakeCaptureCards) Judge(_ context.Context, _, kind string, sub *domain.CaptureSubmission) (*domain.ApprovalCapture, error) {
	f.judgeCalls = append(f.judgeCalls, sub)
	f.kinds = append(f.kinds, kind)
	return f.result, f.err
}

func sopCaptureBody() map[string]any {
	body := birthBodyAutoProvisional()
	body["sop_capture"] = map[string]any{
		"sop_version_id": "v-id",
		"proofs":         map[string]string{"newborns_with_mother": "ref-photo"},
		"answers":        map[string]any{"delivery_type": "assisted"},
	}
	return body
}

func TestRecordBirthEventStripsSopCaptureBeforeIdentity(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	cards := &fakeCaptureCards{result: &domain.ApprovalCapture{SOPVersionID: "v-id",
		Proofs: authored.ProofRefs{"newborns_with_mother": "ref-photo"}, Answers: authored.Answers{"delivery_type": json.RawMessage(`"assisted"`)},
		Evidence: authored.Evidence{VersionLabel: "v2", Media: []authored.EvidenceMedia{{Ref: "ref-photo", Kind: "photo", Label: "Newborns with the mother"}}}}}
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)
	handlerFor(t, mux).WithCaptureCards(cards)

	rec := post(t, mux, appBirthEventRoute, "birth-capture-1", sopCaptureBody())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Identity's strict decoder never saw sop_capture (it would 400 on the unknown key), and
	// the stored payload does not carry it either: the capture lives in its own columns.
	if raw := string(validator.lastCreate.RawBody); json.Valid(validator.lastCreate.RawBody) && containsKey(t, validator.lastCreate.RawBody, "sop_capture") {
		t.Fatalf("identity saw sop_capture: %s", raw)
	}
	if containsKey(t, approvals.lastSubmission.Payload, "sop_capture") {
		t.Fatalf("stored payload carries sop_capture: %s", approvals.lastSubmission.Payload)
	}
	if len(cards.judgeCalls) != 1 || cards.judgeCalls[0] == nil || cards.judgeCalls[0].SOPVersionID != "v-id" || cards.kinds[0] != "birth" {
		t.Fatalf("judge calls = %+v kinds=%v", cards.judgeCalls, cards.kinds)
	}
	if approvals.lastSubmission.Capture == nil || approvals.lastSubmission.Capture.Evidence.VersionLabel != "v2" {
		t.Fatalf("submission must carry the judged capture: %+v", approvals.lastSubmission.Capture)
	}
	// Exact replay collapses; a changed ANSWER with the same key conflicts (the capture is part
	// of the fingerprint when the app sent one).
	if rec := post(t, mux, appBirthEventRoute, "birth-capture-1", sopCaptureBody()); rec.Code != http.StatusAccepted || approvals.submits != 1 {
		t.Fatalf("replay status=%d submits=%d", rec.Code, approvals.submits)
	}
	changed := sopCaptureBody()
	changed["sop_capture"].(map[string]any)["answers"] = map[string]any{"delivery_type": "normal"}
	if rec := post(t, mux, appBirthEventRoute, "birth-capture-1", changed); rec.Code != http.StatusConflict {
		t.Fatalf("changed capture under the same key status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

func TestRecordBirthEventFingerprintUnchangedWithoutCapture(t *testing.T) {
	// The fingerprint of a request WITHOUT sop_capture is byte-identical whether or not a
	// capture card service is wired (an older APK retries the same key against a server that
	// gained the feature and must still collapse).
	fingerprint := func(cards *fakeCaptureCards) string {
		validator := newFakeGoatValidator()
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)
		if cards != nil {
			handlerFor(t, mux).WithCaptureCards(cards)
		}
		if rec := post(t, mux, appBirthEventRoute, "birth-legacy", birthBodyAutoProvisional()); rec.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		return approvals.lastSubmission.RequestFingerprint
	}
	olderApp := &fakeCaptureCards{result: &domain.ApprovalCapture{SOPVersionID: "v-id", Evidence: authored.Evidence{VersionLabel: "v2", MissingNote: "Newborns with the mother"}}}
	without, with := fingerprint(nil), fingerprint(olderApp)
	if without == "" || without != with {
		t.Fatalf("fingerprint moved for a legacy request: %q vs %q", without, with)
	}
	if olderApp.judgeCalls[0] != nil {
		t.Fatal("a request without sop_capture is judged as an older app (nil submission)")
	}
}

func TestRecordBirthEventRejectsMissingCompulsorySlotByName(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	cards := &fakeCaptureCards{err: errors.Join(authored.ErrProofInvalid, &authored.ProofError{SlotKey: "newborns_with_mother", Message: "Record: Newborns with the mother"})}
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)
	handlerFor(t, mux).WithCaptureCards(cards)
	rec := post(t, mux, appBirthEventRoute, "birth-capture-missing", sopCaptureBody())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s, want 422", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Code        string `json:"code"`
		FieldErrors []struct {
			Field string `json:"field"`
		} `json:"field_errors"`
	}
	decodeBody(t, rec, &envelope)
	if envelope.Code != "capture_proof_slot_invalid" || len(envelope.FieldErrors) != 1 || envelope.FieldErrors[0].Field != "newborns_with_mother" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if approvals.submits != 0 {
		t.Fatal("a refused capture must never reach the approver")
	}
	cards.err = errors.Join(authored.ErrAnswerInvalid, &authored.AnswerError{QuestionID: "delivery_type", Message: "Answer: How was the delivery?"})
	rec = post(t, mux, appBirthEventRoute, "birth-capture-answer", sopCaptureBody())
	decodeBody(t, rec, &envelope)
	if rec.Code != http.StatusUnprocessableEntity || envelope.Code != "capture_answer_invalid" || envelope.FieldErrors[0].Field != "delivery_type" {
		t.Fatalf("status=%d envelope=%+v", rec.Code, envelope)
	}
}

func TestRecordDeathEventRefusesUnknownPinnedVersion(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	cards := &fakeCaptureCards{err: countsapp.ErrCaptureSOPVersionUnknown}
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, validator)
	handlerFor(t, mux).WithCaptureCards(cards)
	body := deathBody("dead", "died")
	body["sop_capture"] = map[string]any{"sop_version_id": "retired-long-ago", "proofs": map[string]string{}, "answers": map[string]any{}}
	rec := post(t, mux, appDeathEventRoute, "death-capture-pin", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Code string `json:"code"`
	}
	decodeBody(t, rec, &envelope)
	if envelope.Code != "capture_sop_version_unknown" {
		t.Fatalf("code = %q", envelope.Code)
	}
	if cards.kinds[0] != "death" || approvals.submits != 0 {
		t.Fatalf("kind=%v submits=%d", cards.kinds, approvals.submits)
	}
}

func TestApprovalListCarriesCaptureRowsAndMedia(t *testing.T) {
	approvals := newFakeApprovalWorkflow()
	status := domain.CaptureReviewRework
	reason := "Mother's face not visible"
	approvals.listItems = []domain.ApprovalRequestSummary{{
		ApprovalRequestID: "11111111-1111-4111-8111-111111111112", RequestType: domain.ApprovalRequestTypeBirth, Status: domain.ApprovalStatusPending,
		RaisedByUserID: testActorID, RaisedAt: time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), Summary: json.RawMessage(`{"litter_size":1}`),
		Capture: authored.Evidence{VersionLabel: "v2",
			Media: []authored.EvidenceMedia{{Ref: "ref-photo", Kind: "photo", Label: "Newborns with the mother"}},
			Rows:  []authored.EvidenceRow{{Label: "How was the delivery?", Value: "Assisted", Group: "At report"}},
			MissingNote: "Pen video"},
		CaptureReviewStatus: &status, CaptureReviewReason: &reason,
	}, {
		ApprovalRequestID: "11111111-1111-4111-8111-111111111113", RequestType: domain.ApprovalRequestTypeDeath, Status: domain.ApprovalStatusPending,
		RaisedByUserID: testActorID, RaisedAt: time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC), Summary: json.RawMessage(`{}`),
	}}
	mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, newFakeGoatValidator())
	req := httptest.NewRequest(http.MethodGet, appApprovalsRoute, nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCountsApprover, ScopeType: "tenant"}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	decodeBody(t, rec, &page)
	if len(page.Items) != 2 {
		t.Fatalf("items=%d", len(page.Items))
	}
	var capture struct {
		VersionLabel string `json:"version_label"`
		Rows         []struct {
			Label, Value, Group string
		} `json:"rows"`
		Media []struct {
			ProofID string `json:"proof_id"`
			Label   string `json:"label"`
			Kind    string `json:"kind"`
		} `json:"media"`
		MissingNote  string `json:"missing_note"`
		ReviewStatus string `json:"review_status"`
		ReviewReason string `json:"review_reason"`
	}
	if err := json.Unmarshal(page.Items[0]["capture"], &capture); err != nil {
		t.Fatalf("capture: %v (%s)", err, page.Items[0]["capture"])
	}
	if capture.VersionLabel != "v2" || len(capture.Rows) != 1 || capture.Rows[0].Value != "Assisted" || capture.Rows[0].Group != "At report" {
		t.Fatalf("rows = %+v", capture)
	}
	if len(capture.Media) != 1 || capture.Media[0].ProofID != "ref-photo" || capture.Media[0].Kind != "photo" || capture.Media[0].Label != "Newborns with the mother" {
		t.Fatalf("media = %+v", capture.Media)
	}
	if capture.MissingNote != "Pen video" || capture.ReviewStatus != "rework" || capture.ReviewReason != reason {
		t.Fatalf("note/review = %+v", capture)
	}
	if _, present := page.Items[1]["capture"]; present {
		t.Fatalf("a row with no capture must omit the field: %s", page.Items[1]["capture"])
	}
}

func containsKey(t *testing.T, raw json.RawMessage, key string) bool {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	_, ok := fields[key]
	return ok
}
