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
	if !service.lastCreate.FeedRemovalRequired {
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

// Copy firewall: the removal task's operator-visible contract copy is farm language — the
// backend-owned labels carry no internal words.
func TestFeedWaterRemovalContractCopyIsFarmLanguage(t *testing.T) {
	dto := taskDTOFrom(taskRowForCopyCheck())
	if dto.CaptureMode != domain.CaptureModeTaskProof {
		t.Fatalf("capture mode = %q, want task_proof", dto.CaptureMode)
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
		TaskID:         httpTask,
		Category:       domain.CategoryFeedWaterRemoval,
		ShedName:       "Castro",
		PartitionLabel: "2",
	}
}
