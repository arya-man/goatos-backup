package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// stubService records calls and returns canned results/errors.
type stubService struct {
	listErr         error
	answerErr       error
	completeErr     error
	listCalls       int
	colostrumCalls  int
	lastColostrum   tasksapp.ListColostrumDayInput
	writeCalls      int
	lastAnswer      tasksapp.AnswerActionInput
	detail          domain.WorkflowDetail
	detailErr       error
	colostrumDetail tasksapp.ColostrumDetail
	lastDetailDate  string
}

func (s *stubService) ListWorkflows(_ context.Context, _ tasksapp.ListWorkflowsInput) (domain.WorkflowListPage, error) {
	s.listCalls++
	return domain.WorkflowListPage{Items: []domain.WorkflowCard{}}, s.listErr
}

func (s *stubService) ListColostrumDay(_ context.Context, in tasksapp.ListColostrumDayInput) (domain.WorkflowListPage, error) {
	s.colostrumCalls++
	s.lastColostrum = in
	return domain.WorkflowListPage{Items: []domain.WorkflowCard{}}, s.listErr
}

func (s *stubService) GetWorkflow(_ context.Context, _, _ string) (domain.WorkflowDetail, error) {
	if s.detailErr != nil {
		return domain.WorkflowDetail{}, s.detailErr
	}
	return s.detail, nil
}

func (s *stubService) GetColostrumDay(_ context.Context, _, _, date string) (tasksapp.ColostrumDetail, error) {
	s.lastDetailDate = date
	if s.detailErr != nil {
		return tasksapp.ColostrumDetail{}, s.detailErr
	}
	return s.colostrumDetail, nil
}

func TestGetWorkflowHidesInternalApprovalAndBlocksLaterOperatorAction(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{
			WorkflowID: "wf-death", Module: domain.ModuleDeath, TemplateKey: domain.TemplateKeyDeath,
			ActionsDone: 0, ActionsTotal: 3,
		},
		Actions: []domain.WorkflowAction{
			{ActionID: "death", ActionKey: domain.ActionKeyDeathVideo, Seq: 1, Section: domain.SectionMain, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending},
			{ActionID: "postmortem", ActionKey: domain.ActionKeyPostMortemVideo, Seq: 2, Section: domain.SectionMain, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending},
			{ActionID: "approval", ActionKey: domain.ActionKeyParkHeadSignoff, Seq: 3, Section: domain.SectionMain, ActionType: domain.ActionTypeApproval, Status: domain.ActionStatusPending},
		},
	}}
	rec := doRequest(newTestMux(svc), http.MethodGet, "/app/workflows/wf-death", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var response workflowDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Actions) != 2 {
		t.Fatalf("operator actions = %d, want 2", len(response.Actions))
	}
	if response.ActionsTotal != 2 {
		t.Fatalf("operator actions_total = %d, want 2", response.ActionsTotal)
	}
	if response.Actions[0].Blocked {
		t.Fatal("first death video must be enabled")
	}
	if !response.Actions[1].Blocked {
		t.Fatal("post-mortem video must be blocked until death video completes")
	}
}

func TestGetWorkflowBlocksORSRoundTwoUntilRecordedDueTime(t *testing.T) {
	due := time.Now().UTC().Add(time.Hour)
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{
			WorkflowID: "wf-mother", Module: domain.ModuleBirth, TemplateKey: domain.TemplateKeyBirthMother,
			ActionsDone: 5, ActionsTotal: 6,
		},
		Actions: []domain.WorkflowAction{
			{ActionID: "ors-2", ActionKey: domain.ActionKeyORSWater2, Seq: 6, Section: domain.SectionMain, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending, DueAt: &due},
		},
	}}
	rec := doRequest(newTestMux(svc), http.MethodGet, "/app/workflows/wf-mother", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var response workflowDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Actions) != 1 || !response.Actions[0].Blocked {
		t.Fatalf("ORS round 2 action = %+v, want blocked before due_at", response.Actions)
	}
	if response.Actions[0].BlockedReason != "not_yet_due" {
		t.Fatalf("blocked_reason = %q, want not_yet_due", response.Actions[0].BlockedReason)
	}
}

func (s *stubService) AnswerAction(_ context.Context, in tasksapp.AnswerActionInput) (domain.ActionWriteResult, error) {
	s.writeCalls++
	s.lastAnswer = in
	if s.answerErr != nil {
		return domain.ActionWriteResult{}, s.answerErr
	}
	return domain.ActionWriteResult{}, nil
}

func (s *stubService) CompleteAction(_ context.Context, _ tasksapp.CompleteActionInput) (domain.ActionWriteResult, error) {
	s.writeCalls++
	if s.completeErr != nil {
		return domain.ActionWriteResult{}, s.completeErr
	}
	return domain.ActionWriteResult{}, nil
}

func newTestMux(svc *stubService) *http.ServeMux {
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, nil))
	return mux
}

func doRequest(mux *http.ServeMux, method, target, body string, headers map[string]string, tenant bool) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if tenant {
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "11111111-1111-1111-1111-111111111111"))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, rec.Body.String())
	}
	return envelope.Code
}

func TestListWorkflowsParamValidation(t *testing.T) {
	svc := &stubService{}
	mux := newTestMux(svc)

	cases := []struct {
		name       string
		target     string
		tenant     bool
		wantStatus int
		wantCode   string
	}{
		{"missing tenant", "/app/workflows?module=birth", false, http.StatusUnauthorized, "missing_tenant"},
		{"missing module", "/app/workflows", true, http.StatusBadRequest, "invalid_module"},
		{"bad module", "/app/workflows?module=feed", true, http.StatusBadRequest, "invalid_module"},
		{"bad date", "/app/workflows?module=birth&date=27-07-2026", true, http.StatusBadRequest, "invalid_date"},
		{"bad filter", "/app/workflows?module=birth&filter=everything", true, http.StatusBadRequest, "invalid_filter"},
		{"bad page size", "/app/workflows?module=birth&page_size=zero", true, http.StatusBadRequest, "invalid_page_size"},
		{"negative page size", "/app/workflows?module=birth&page_size=-2", true, http.StatusBadRequest, "invalid_page_size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(mux, http.MethodGet, tc.target, "", nil, tc.tenant)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := errCode(t, rec); got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
	if svc.listCalls != 0 {
		t.Fatalf("service reached on invalid params: %d calls", svc.listCalls)
	}

	// Valid request goes through (death module, explicit filter/date/page_size).
	rec := doRequest(mux, http.MethodGet, "/app/workflows?module=death&date=2026-07-27&filter=overdue&page_size=10", "", nil, true)
	if rec.Code != http.StatusOK || svc.listCalls != 1 {
		t.Fatalf("valid list: status=%d calls=%d (%s)", rec.Code, svc.listCalls, rec.Body.String())
	}
}

func TestListWorkflowsReturnsPreviousOverdueDates(t *testing.T) {
	svc := &stubService{}
	rec := doRequest(newTestMux(svc), http.MethodGet,
		"/app/workflows?module=birth&date=2026-07-28", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, ok := body["overdue_dates"]
	if !ok {
		t.Fatalf("response is missing overdue_dates: %s", rec.Body.String())
	}
	if string(raw) != "[]" {
		t.Fatalf("overdue_dates = %s, want [] (never null)", raw)
	}
}

func TestActionWriteParamValidation(t *testing.T) {
	svc := &stubService{}
	mux := newTestMux(svc)
	answerTarget := "/app/workflows/wf-1/actions/act-1/answer"

	// Missing Idempotency-Key.
	rec := doRequest(mux, http.MethodPost, answerTarget, `{"answer_value":"yes"}`, nil, true)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("missing key: status=%d code=%s", rec.Code, errCode(t, rec))
	}

	// Too-short Idempotency-Key.
	rec = doRequest(mux, http.MethodPost, answerTarget, `{"answer_value":"yes"}`,
		map[string]string{"Idempotency-Key": "short"}, true)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "invalid_idempotency_key" {
		t.Fatalf("short key: status=%d code=%s", rec.Code, errCode(t, rec))
	}

	// Unknown body field is rejected (strict decode).
	rec = doRequest(mux, http.MethodPost, answerTarget, `{"answer_value":"yes","extra":1}`,
		map[string]string{"Idempotency-Key": "long-enough-key"}, true)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "invalid_json" {
		t.Fatalf("strict decode: status=%d code=%s", rec.Code, errCode(t, rec))
	}

	if svc.writeCalls != 0 {
		t.Fatalf("service reached on invalid writes: %d calls", svc.writeCalls)
	}
}

func TestAnswerActionAcceptsVideoProof(t *testing.T) {
	svc := &stubService{}
	rec := doRequest(newTestMux(svc), http.MethodPost, "/app/workflows/wf-1/actions/act-1/answer",
		`{"answer_value":"yes","proof_ref":"proof-mother-1"}`,
		map[string]string{"Idempotency-Key": "long-enough-key"}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.lastAnswer.ProofRef != "proof-mother-1" {
		t.Fatalf("proof_ref = %q, want proof-mother-1", svc.lastAnswer.ProofRef)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"proof required", domain.ErrProofRequired, http.StatusUnprocessableEntity, "proof_required"},
		{"idempotency conflict", domain.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
		{"already completed", domain.ErrActionAlreadyCompleted, http.StatusConflict, "action_already_completed"},
		{"in review", domain.ErrActionInReview, http.StatusConflict, "action_in_review"},
		{"out of sequence", domain.ErrActionOutOfSequence, http.StatusConflict, "action_out_of_sequence"},
		{"not yet due", domain.ErrActionNotYetDue, http.StatusConflict, "action_not_yet_due"},
		{"not completable", domain.ErrActionNotCompletable, http.StatusBadRequest, "action_not_completable"},
		{"not found", domain.ErrNotFound, http.StatusNotFound, "workflow_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{completeErr: tc.err}
			mux := newTestMux(svc)
			rec := doRequest(mux, http.MethodPost, "/app/workflows/wf-1/actions/act-1/complete",
				`{"proof_ref":"p-1"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := errCode(t, rec); got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestCompleteAllowsEmptyBody(t *testing.T) {
	svc := &stubService{}
	mux := newTestMux(svc)
	rec := doRequest(mux, http.MethodPost, "/app/workflows/wf-1/actions/act-1/complete", "",
		map[string]string{"Idempotency-Key": "long-enough-key"}, true)
	if rec.Code != http.StatusOK || svc.writeCalls != 1 {
		t.Fatalf("empty-body complete: status=%d calls=%d (%s)", rec.Code, svc.writeCalls, rec.Body.String())
	}
}
