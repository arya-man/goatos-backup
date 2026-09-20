package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
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

func (s *stubService) GetWorkflowBySubject(ctx context.Context, tenantID, _, _ string) (domain.WorkflowDetail, error) {
	return s.GetWorkflow(ctx, tenantID, "")
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

func (s *stubService) ListGeneralSOPs(context.Context, string) ([]ports.GeneralSOP, error) {
	return []ports.GeneralSOP{}, nil
}

func (s *stubService) StartGeneralWorkflow(context.Context, tasksapp.StartGeneralWorkflowInput) (string, error) {
	return "", nil
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
	var perms []string
	if tenant {
		perms = []string{permissions.CountsWrite}
	}
	return doRequestWithPermissions(mux, method, target, body, headers, tenant, perms)
}

func doRequestWithPermissions(mux *http.ServeMux, method, target, body string, headers map[string]string, tenant bool, perms []string) *httptest.ResponseRecorder {
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
	if perms != nil {
		req = req.WithContext(httpmiddleware.WithPersonPermissions(req.Context(), perms))
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

func TestWorkInstructionsPermissionCannotReadCountsWorkflowLists(t *testing.T) {
	svc := &stubService{}
	mux := newTestMux(svc)
	perms := []string{permissions.WorkInstructionsExecute}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows?module=birth", "", nil, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("birth list with work-instructions tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	if svc.listCalls != 0 {
		t.Fatalf("birth list reached service with only work-instructions permission: %d calls", svc.listCalls)
	}

	rec = doRequestWithPermissions(mux, http.MethodGet, "/app/workflows?module=general", "", nil, true, perms)
	if rec.Code != http.StatusOK || svc.listCalls != 1 {
		t.Fatalf("general list with work-instructions tick: status=%d calls=%d (%s)", rec.Code, svc.listCalls, rec.Body.String())
	}
}

func TestWorkInstructionsPermissionCannotReadOrWriteCountsWorkflowDetail(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-death", Module: domain.ModuleDeath, TemplateKey: domain.TemplateKeyDeath},
	}}
	mux := newTestMux(svc)
	perms := []string{permissions.WorkInstructionsExecute}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows/wf-death", "", nil, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("death detail with work-instructions tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}

	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-death/actions/act-1/answer",
		`{"answer_value":"yes"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("death write with work-instructions tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	if svc.writeCalls != 0 {
		t.Fatalf("death write reached mutation service with only work-instructions permission: %d calls", svc.writeCalls)
	}
}

func TestWorkInstructionsPermissionCanReadAndWriteGeneralWorkflow(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-general", Module: domain.ModuleGeneral, TemplateKey: domain.GeneralTemplateKey("general.gate_visitor_check")},
	}}
	mux := newTestMux(svc)
	perms := []string{permissions.WorkInstructionsExecute}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows/wf-general", "", nil, true, perms)
	if rec.Code != http.StatusOK {
		t.Fatalf("general detail with work-instructions tick: status=%d (%s)", rec.Code, rec.Body.String())
	}

	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-general/actions/act-1/answer",
		`{"answer_value":"yes"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusOK || svc.writeCalls != 1 {
		t.Fatalf("general write with work-instructions tick: status=%d calls=%d (%s)", rec.Code, svc.writeCalls, rec.Body.String())
	}
}

// The mirror of the two tests above (PR #308 review, 2026-09-19): the route table ORs CountsWrite
// with WorkInstructionsExecute, so a counts-only tick passes the route gate and the HANDLER must
// narrow by module. It did not -- `module=general` skipped the CountsWrite check, a general detail
// was open to anyone past the route, and every write returned true on CountsWrite alone -- so a
// person with only the counts tick could list, open, answer and complete general work-instruction
// runs without work_instructions.execute.
func TestCountsPermissionCannotReadGeneralWorkflowList(t *testing.T) {
	svc := &stubService{}
	mux := newTestMux(svc)
	perms := []string{permissions.CountsWrite}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows?module=general", "", nil, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("general list with counts tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	if svc.listCalls != 0 {
		t.Fatalf("general list reached service with only counts permission: %d calls", svc.listCalls)
	}

	// The same tick still lists its own module, so the narrowing is by module and not a lockout.
	rec = doRequestWithPermissions(mux, http.MethodGet, "/app/workflows?module=birth", "", nil, true, perms)
	if rec.Code != http.StatusOK || svc.listCalls != 1 {
		t.Fatalf("birth list with counts tick: status=%d calls=%d (%s)", rec.Code, svc.listCalls, rec.Body.String())
	}
}

func TestCountsPermissionCannotReadOrWriteGeneralWorkflowDetail(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-general", Module: domain.ModuleGeneral, TemplateKey: domain.GeneralTemplateKey("general.gate_visitor_check")},
	}}
	mux := newTestMux(svc)
	perms := []string{permissions.CountsWrite}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows/wf-general", "", nil, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("general detail with counts tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}

	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-general/actions/act-1/answer",
		`{"answer_value":"yes"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("general answer with counts tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-general/actions/act-1/complete",
		`{}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("general complete with counts tick: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	if svc.writeCalls != 0 {
		t.Fatalf("general write reached mutation service with only counts permission: %d calls", svc.writeCalls)
	}
}

// Holding BOTH ticks (a director layered with operator, say) opens both modules -- the gate is
// per module, never an either/or between the two permissions.
func TestBothTicksReachBothModules(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-general", Module: domain.ModuleGeneral, TemplateKey: domain.GeneralTemplateKey("general.gate_visitor_check")},
	}}
	mux := newTestMux(svc)
	perms := []string{permissions.CountsWrite, permissions.WorkInstructionsExecute}
	for _, target := range []string{"/app/workflows?module=general", "/app/workflows?module=birth", "/app/workflows/wf-general"} {
		rec := doRequestWithPermissions(mux, http.MethodGet, target, "", nil, true, perms)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s with both ticks: status=%d (%s)", target, rec.Code, rec.Body.String())
		}
	}
}

func TestSalesReadCanOpenButCannotMutateSalesWorkflow(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-sale", Module: domain.ModuleSales, TemplateKey: domain.TemplateKeySalesDeal},
	}}
	mux := newTestMux(svc)
	perms := []string{permissions.SalesRead}

	rec := doRequestWithPermissions(mux, http.MethodGet, "/app/workflows/wf-sale", "", nil, true, perms)
	if rec.Code != http.StatusOK {
		t.Fatalf("sales detail with sales.read: status=%d (%s)", rec.Code, rec.Body.String())
	}

	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-sale/actions/act-1/answer",
		`{"answer_value":"yes"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("sales answer with sales.read: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	rec = doRequestWithPermissions(mux, http.MethodPost, "/app/workflows/wf-sale/actions/act-1/complete",
		`{}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, perms)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "permission_denied" {
		t.Fatalf("sales complete with sales.read: status=%d code=%s", rec.Code, errCode(t, rec))
	}
	if svc.writeCalls != 0 {
		t.Fatalf("sales.read reached mutation service: %d calls", svc.writeCalls)
	}
}

func TestSalesWriteCanMutateSalesWorkflow(t *testing.T) {
	svc := &stubService{detail: domain.WorkflowDetail{
		Card: domain.WorkflowCard{WorkflowID: "wf-sale", Module: domain.ModuleSales, TemplateKey: domain.TemplateKeySalesDeal},
	}}
	rec := doRequestWithPermissions(newTestMux(svc), http.MethodPost, "/app/workflows/wf-sale/actions/act-1/answer",
		`{"answer_value":"yes"}`, map[string]string{"Idempotency-Key": "long-enough-key"}, true, []string{permissions.SalesWrite})
	if rec.Code != http.StatusOK || svc.writeCalls != 1 {
		t.Fatalf("sales answer with sales.write: status=%d calls=%d (%s)", rec.Code, svc.writeCalls, rec.Body.String())
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
