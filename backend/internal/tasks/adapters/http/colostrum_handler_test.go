package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func colostrumIST(day, hour, minute int) time.Time {
	return time.Date(2026, time.August, day, hour, minute, 0, 0, biztime.DefaultLocation())
}

// module=colostrum must reach the colostrum-day read, NOT the birth list. They are different
// grains: birth keys on the birth date and counts every operator action, so serving the colostrum
// page from ListWorkflows would answer a different question with a plausible-looking number.
func TestListWorkflowsRoutesColostrumToTheDayLens(t *testing.T) {
	svc := &stubService{}
	rec := doRequest(newTestMux(svc), http.MethodGet,
		"/app/workflows?module=colostrum&date=2026-08-06&filter=overdue", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.colostrumCalls != 1 {
		t.Fatalf("colostrum lens calls = %d, want 1", svc.colostrumCalls)
	}
	if svc.listCalls != 0 {
		t.Fatal("the colostrum lens must not fall through to the birth/death list read")
	}
	if svc.lastColostrum.Date != "2026-08-06" || svc.lastColostrum.Filter != "overdue" {
		t.Fatalf("lens input = %+v, want date 2026-08-06 filter overdue", svc.lastColostrum)
	}
}

func TestListWorkflowsColostrumRejectsAwaitingVideoFilter(t *testing.T) {
	svc := &stubService{}
	rec := doRequest(newTestMux(svc), http.MethodGet,
		"/app/workflows?module=colostrum&filter=awaiting_video", "", nil, true)
	// Verification is enqueued per WHOLE kid workflow, so a single day's feeds can never sit in an
	// awaiting-video bucket. Accepting the filter would render a chip that is permanently zero.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if svc.colostrumCalls != 0 {
		t.Fatal("an invalid filter must be rejected before the read")
	}
	// The same filter stays valid for birth/death, whose grain does have that bucket.
	rec = doRequest(newTestMux(svc), http.MethodGet, "/app/workflows?module=birth&filter=awaiting_video", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("birth awaiting_video status = %d, want 200", rec.Code)
	}
}

// THE TRAP THIS TEST EXISTS FOR: the colostrum detail renders only that day's feeds, but a feed's
// blocked state is computed against its SIBLINGS — and 1st Colostrum sits at seq 5 of section
// `main`, behind kid-clean, iodine dipping, front teeth and suck reflex. If the handler passed the
// filtered (colostrum-only) list as the sibling set, those four prerequisites would vanish and the
// feed would render as READY. A milk operator would tap it and get a bare 409 action_out_of_sequence
// instead of an honest reason.
func TestColostrumDetailComputesBlockedAgainstTheFullSiblingSet(t *testing.T) {
	firstColostrumDue := colostrumIST(6, 6, 0)
	sessionDue := colostrumIST(6, 11, 0)

	full := []domain.WorkflowAction{
		{ActionID: "clean", ActionKey: domain.ActionKeyKidClean, Seq: 1, Section: domain.SectionMain, ActionType: domain.ActionTypeQuestion, Status: domain.ActionStatusPending},
		{ActionID: "iodine", ActionKey: domain.ActionKeyIodineDipping, Seq: 2, Section: domain.SectionMain, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending},
		{ActionID: "teeth", ActionKey: domain.ActionKeyFrontTeeth, Seq: 3, Section: domain.SectionMain, ActionType: domain.ActionTypeQuestion, Status: domain.ActionStatusPending},
		{ActionID: "suck", ActionKey: domain.ActionKeySuckReflex, Seq: 4, Section: domain.SectionMain, ActionType: domain.ActionTypeQuestion, Status: domain.ActionStatusPending},
		{ActionID: "first", ActionKey: domain.ActionKeyFirstColostrum, Seq: 5, Section: domain.SectionMain, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending, DueAt: &firstColostrumDue},
		{ActionID: "s1100", ActionKey: "colostrum_day_1_1100", Seq: 9, Section: domain.SectionColostrumSession, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending, DueAt: &sessionDue},
	}
	visible := []domain.WorkflowAction{full[4], full[5]}

	svc := &stubService{colostrumDetail: tasksapp.ColostrumDetail{
		Detail: domain.WorkflowDetail{
			Card: domain.WorkflowCard{
				WorkflowID: "wf-kid", Module: domain.ModuleColostrum, TemplateKey: domain.TemplateKeyBirthKid,
				ActionsDone: 0, ActionsTotal: 2,
			},
			Actions: full,
		},
		Visible: visible,
	}}

	rec := doRequest(newTestMux(svc), http.MethodGet,
		"/app/workflows/wf-kid?lens=colostrum&date=2026-08-06", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.lastDetailDate != "2026-08-06" {
		t.Fatalf("date passed to the lens = %q, want 2026-08-06", svc.lastDetailDate)
	}
	var response workflowDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only the feeds are rendered — iodine, teeth and suck reflex stay in Birth.
	if len(response.Actions) != 2 {
		t.Fatalf("rendered actions = %d, want 2 feeds", len(response.Actions))
	}
	first := response.Actions[0]
	if first.ActionKey != domain.ActionKeyFirstColostrum {
		t.Fatalf("first row = %s, want 1st Colostrum", first.ActionKey)
	}
	if !first.Blocked || first.BlockedReason != "previous_action" {
		t.Fatalf("1st Colostrum blocked=%v reason=%q; want blocked with previous_action — the four "+
			"earlier birth steps are pending and the operator must be told, not 409'd on tap",
			first.Blocked, first.BlockedReason)
	}
}

// The detail card must be re-counted to the DAY grain, so the header cannot disagree with the list
// card the operator just tapped (e.g. showing 3/11 under a card that read 1/5).
func TestColostrumDetailRendersTheDayGrainCard(t *testing.T) {
	due := colostrumIST(6, 11, 0)
	svc := &stubService{colostrumDetail: tasksapp.ColostrumDetail{
		Detail: domain.WorkflowDetail{
			Card: domain.WorkflowCard{
				WorkflowID: "wf-kid", Module: domain.ModuleColostrum, TemplateKey: domain.TemplateKeyBirthKid,
				ActionsDone: 1, ActionsTotal: 5, State: domain.WorkflowStateOpen,
				NextAction: &domain.WorkflowNextAction{Key: "colostrum_day_2_1100", Title: "6th Colostrum", DueAt: &due},
			},
			Actions: []domain.WorkflowAction{
				{ActionID: "s1100", ActionKey: "colostrum_day_2_1100", Seq: 10, Section: domain.SectionColostrumSession, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending, DueAt: &due},
			},
		},
		Visible: []domain.WorkflowAction{
			{ActionID: "s1100", ActionKey: "colostrum_day_2_1100", Seq: 10, Section: domain.SectionColostrumSession, ActionType: domain.ActionTypeAction, Status: domain.ActionStatusPending, DueAt: &due},
		},
	}}
	rec := doRequest(newTestMux(svc), http.MethodGet, "/app/workflows/wf-kid?lens=colostrum", "", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var response workflowDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.ActionsDone != 1 || response.ActionsTotal != 5 {
		t.Fatalf("card = %d/%d, want the day grain 1/5", response.ActionsDone, response.ActionsTotal)
	}
	if response.Module != domain.ModuleColostrum {
		t.Fatalf("module = %q, want colostrum so the response is self-describing", response.Module)
	}
	if response.NextAction == nil || response.NextAction.Title != "6th Colostrum" {
		t.Fatalf("next action = %+v, want the day's next feed", response.NextAction)
	}
}

func TestGetWorkflowRejectsAnUnknownLens(t *testing.T) {
	svc := &stubService{}
	rec := doRequest(newTestMux(svc), http.MethodGet, "/app/workflows/wf-kid?lens=weighing", "", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown lens (%s)", rec.Code, rec.Body.String())
	}
}
