package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The feed & water removal create fields ride POST /app/pc-care/tasks and reach the app layer
// verbatim, and the three precondition refusals map to stable 422 machine codes with
// farm-worded copy (no internal words in the visible message).
func TestCreateTaskCarriesFeedRemovalFieldsAndMapsPreconditionErrors(t *testing.T) {
	do := func(service *fakePCCareHTTPService, body string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		Register(mux, NewHandler(service, nil))
		req := httptest.NewRequest(http.MethodPost, "/app/pc-care/tasks", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "pc-fasting-http-create-1")
		ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
		ctx = httpmiddleware.WithActorID(ctx, httpActor)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
			Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: httpTenant,
		}})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req)
		return rec
	}

	service := &fakePCCareHTTPService{}
	rec := do(service, `{
		"category": "deworming",
		"park_id": "9c000000-0000-4000-8000-000000001001",
		"shed_id": "9c000000-0000-4000-8000-000000001002",
		"planned_business_date": "2026-09-11",
		"assignee_user_ids": ["`+httpActor+`"],
		"feed_removal_required": true,
		"removal_operator_user_ids": ["9c000000-0000-4000-8000-00000000ffff"]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if service.lastCreate.FeedRemovalRequested == nil || !*service.lastCreate.FeedRemovalRequested {
		t.Fatal("feed_removal_required must reach the app input")
	}
	if len(service.lastCreate.RemovalOperatorUserIDs) != 1 ||
		service.lastCreate.RemovalOperatorUserIDs[0] != "9c000000-0000-4000-8000-00000000ffff" {
		t.Fatalf("removal operators = %v, want the request's list verbatim", service.lastCreate.RemovalOperatorUserIDs)
	}

	for _, tc := range []struct {
		err      error
		wantCode string
	}{
		{domain.ErrFastingWindowClosed, "fasting_window_closed"},
		{domain.ErrRemovalOperatorsRequired, "removal_operators_required"},
		{domain.ErrFeedRemovalNotApplicable, "feed_removal_not_applicable"},
	} {
		rec := do(&fakePCCareHTTPService{createErr: tc.err}, `{"category":"deworming"}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.wantCode) {
			t.Fatalf("error %v -> status=%d body=%s, want 422 with code %q", tc.err, rec.Code, rec.Body.String(), tc.wantCode)
		}
	}
}

// The refusal for a category the card does not cover must NOT name a category. Which work the
// removal accompanies is the PUBLISHED card's call (PC CARE SOP, 2026-09-22); this sentence read
// "feed & water removal applies to deworming only" while the live document also listed ticks
// removal and hoof trimming, so it told the planner a rule that was no longer true.
func TestRemovalNotApplicableRefusalNamesTheCardNotACategory(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakePCCareHTTPService{createErr: domain.ErrFeedRemovalNotApplicable}, nil))
	req := httptest.NewRequest(http.MethodPost, "/app/pc-care/tasks", strings.NewReader(`{"category":"hair_trimming"}`))
	req.Header.Set("Idempotency-Key", "pc-removal-not-applicable-copy")
	ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
	ctx = httpmiddleware.WithActorID(ctx, httpActor)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: httpTenant,
	}})
	rec := httptest.NewRecorder()
	httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req.WithContext(ctx))

	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "feed_removal_not_applicable") {
		t.Fatalf("status=%d body=%s, want 422 feed_removal_not_applicable", rec.Code, body)
	}
	lower := strings.ToLower(body)
	for _, category := range []string{"deworming", "ticks removal", "hoof trimming", "hair trimming", "anti protozoan"} {
		if strings.Contains(lower, category) {
			t.Fatalf("the refusal names %q; which work the removal covers is the card's call, so the sentence must point at the card: %s", category, body)
		}
	}
	if !strings.Contains(lower, "sop") && !strings.Contains(lower, "card") {
		t.Fatalf("the refusal should point the planner at the document: %s", body)
	}
}

// Copy firewall: the removal task's operator-visible contract copy is farm language — the
// backend-owned labels carry no internal words.
func TestFeedWaterRemovalContractCopyIsFarmLanguage(t *testing.T) {
	dto := taskDTOFrom(taskRowForCopyCheck())
	if dto.CaptureMode != domain.CaptureModeTaskProof {
		t.Fatalf("capture mode = %q, want task_proof", dto.CaptureMode)
	}
	if dto.TaskLabel != "Remove feed & water · Castro - 2" {
		t.Fatalf("task label = %q, want removal action with pen name", dto.TaskLabel)
	}
	if len(dto.ExpectedSlots) != 2 {
		t.Fatalf("expected slots = %d, want 2", len(dto.ExpectedSlots))
	}
	visible := []string{
		domain.CategoryLabel(domain.CategoryFeedWaterRemoval),
		dto.ExpectedSlots[0].Label, dto.ExpectedSlots[0].Description,
		dto.ExpectedSlots[1].Label, dto.ExpectedSlots[1].Description,
	}
	for _, copyText := range visible {
		lower := strings.ToLower(copyText)
		for _, banned := range []string{"fasting", "gate", "kernel", "debug", "backend", "api", "task_proof"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("visible copy %q carries banned internal word %q", copyText, banned)
			}
		}
	}
}

func taskRowForCopyCheck() ports.TaskRow {
	return ports.TaskRow{
		TaskID:           httpTask,
		Category:         domain.CategoryFeedWaterRemoval,
		ShedName:         "Castro",
		PartitionLabel:   "2",
		RemovalPenLabels: []string{"Castro - 2"},
	}
}

func TestFeedWaterRemovalTaskLabelNamesMultiplePensCompactly(t *testing.T) {
	dto := taskDTOFrom(ports.TaskRow{
		TaskID:           httpTask,
		Category:         domain.CategoryFeedWaterRemoval,
		RemovalPenLabels: []string{"Yashoda 10", "Castro 2", "Yashoda 8"},
	})
	if dto.TaskLabel != "Remove feed & water · Yashoda 10, Castro 2 +1 more" {
		t.Fatalf("task label = %q", dto.TaskLabel)
	}
	if dto.OperationalLocationDisplay != "" {
		t.Fatalf("round-grain removal has no single operational location, got %q", dto.OperationalLocationDisplay)
	}
}

func TestTaskDTOFallsBackToScannedAnimalPensWhenTaskPartitionMissing(t *testing.T) {
	dto := taskDTOFrom(ports.TaskRow{
		TaskID:          httpTask,
		Category:        domain.CategoryDeworming,
		ShedName:        "Mandela 2",
		AnimalPenLabels: []string{"Mandela 2 - Part 1", "Mandela 2 - Part 3"},
	})
	if dto.TaskLabel != "Mandela 2 - Part 1, Mandela 2 - Part 3" {
		t.Fatalf("task label = %q", dto.TaskLabel)
	}
	if dto.OperationalLocationDisplay != "Mandela 2" {
		t.Fatalf("operational location display = %q", dto.OperationalLocationDisplay)
	}
}
