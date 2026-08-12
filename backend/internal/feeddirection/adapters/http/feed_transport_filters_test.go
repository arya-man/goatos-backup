package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type transportFilterService struct{}

func (transportFilterService) Preview(context.Context, domain.PreviewQuery) (domain.PreviewPage, error) {
	return domain.PreviewPage{}, nil
}
func (transportFilterService) PackingWorklist(context.Context, domain.PackingQuery) (domain.PackingPage, error) {
	return domain.PackingPage{}, nil
}
func (transportFilterService) CompleteSession(context.Context, app.CompleteSessionInput) (ports.CompleteSessionResult, error) {
	return ports.CompleteSessionResult{}, nil
}
func (transportFilterService) CompleteDistribution(context.Context, app.CompleteDistributionInput) (ports.CompleteDistributionResult, error) {
	return ports.CompleteDistributionResult{}, nil
}
func (transportFilterService) CompletePacking(context.Context, app.CompletePackingInput) (ports.CompletePackingResult, error) {
	return ports.CompletePackingResult{}, nil
}
func (transportFilterService) ListTransportTasks(_ context.Context, in app.ListTransportTasksInput) (ports.FeedTransportTaskPage, error) {
	tasks := []ports.FeedTransportTask{
		{TaskID: "10000000-0000-4000-8000-000000000001", ParkID: "20000000-0000-4000-8000-000000000001", ParkLabel: "Farm A", ShedID: "30000000-0000-4000-8000-000000000001", ShedLabel: "Shed A", BusinessDate: "2026-07-29", Status: "due", ScheduledAt: time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)},
		{TaskID: "10000000-0000-4000-8000-000000000002", ParkID: "20000000-0000-4000-8000-000000000002", ParkLabel: "Farm B", ShedID: "30000000-0000-4000-8000-000000000002", ShedLabel: "Shed B", BusinessDate: "2026-07-29", Status: "completed", ScheduledAt: time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)},
	}
	filtered := make([]ports.FeedTransportTask, 0, len(tasks))
	for _, task := range tasks {
		if in.ParkID != "" && task.ParkID != in.ParkID {
			continue
		}
		if in.ShedID != "" && task.ShedID != in.ShedID {
			continue
		}
		if in.Status != "" && task.Status != in.Status {
			continue
		}
		filtered = append(filtered, task)
	}
	return ports.FeedTransportTaskPage{
		Items: filtered,
		Filters: ports.FeedTransportFilterOptions{
			Parks: []ports.FeedTransportFilterOption{{ID: tasks[0].ParkID, Label: tasks[0].ParkLabel}, {ID: tasks[1].ParkID, Label: tasks[1].ParkLabel}},
			Sheds: []ports.FeedTransportFilterOption{{ID: tasks[1].ShedID, Label: tasks[1].ShedLabel}},
		},
	}, nil
}
func (transportFilterService) SubmitTransport(context.Context, app.SubmitTransportInput) (ports.SubmitTransportResult, error) {
	return ports.SubmitTransportResult{}, nil
}

func (transportFilterService) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{}, nil
}

func TestGetTransportTasksAppliesFarmShedAndStatusBeforePaging(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/feed-transport/tasks?business_date=2026-07-29&park_id=20000000-0000-4000-8000-000000000002&shed_id=30000000-0000-4000-8000-000000000002&status=completed&limit=20", nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"), "40000000-0000-4000-8000-000000000001")
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()

	NewHandler(transportFilterService{}, slog.Default()).GetTransportTasks(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Items   []transportTaskDTO  `json:"items"`
		Filters transportFiltersDTO `json:"filters"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].ParkLabel != "Farm B" || body.Items[0].ShedLabel != "Shed B" || body.Items[0].Status != "completed" {
		t.Fatalf("filtered items=%+v", body.Items)
	}
	if len(body.Filters.Parks) != 2 || len(body.Filters.Sheds) != 1 || body.Filters.Sheds[0].Label != "Shed B" {
		t.Fatalf("filters=%+v", body.Filters)
	}
}

type transportScopeSpyService struct {
	transportFilterService
	listCalls   int
	listInput   app.ListTransportTasksInput
	submitCalls int
	submitInput app.SubmitTransportInput
}

func (s *transportScopeSpyService) ListTransportTasks(_ context.Context, in app.ListTransportTasksInput) (ports.FeedTransportTaskPage, error) {
	s.listCalls++
	s.listInput = in
	return ports.FeedTransportTaskPage{}, nil
}

func (s *transportScopeSpyService) SubmitTransport(_ context.Context, in app.SubmitTransportInput) (ports.SubmitTransportResult, error) {
	s.submitCalls++
	s.submitInput = in
	return ports.SubmitTransportResult{AttemptID: "attempt-1", Status: "verification_due", AttemptNo: 1, NewlyPending: true}, nil
}

func TestGetTransportTasksResolvesCapabilityAwareParkScope(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-000000000001"
		parkB    = "20000000-0000-4000-8000-000000000002"
	)

	t.Run("omitted park defaults to caller feed transport park", func(t *testing.T) {
		service := &transportScopeSpyService{}
		req := httptest.NewRequest(http.MethodGet, "/feed-transport/tasks?business_date=2026-07-29", nil)
		ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}})
		recorder := httptest.NewRecorder()

		NewHandler(service, slog.Default()).GetTransportTasks(recorder, req.WithContext(ctx))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if service.listInput.ParkID != parkA {
			t.Fatalf("park_id=%q, want caller park %q", service.listInput.ParkID, parkA)
		}
		if got := service.listInput.AuthorizedParkIDs; len(got) != 1 || got[0] != parkA {
			t.Fatalf("authorized parks=%v, want [%s]", got, parkA)
		}
	})

	t.Run("foreign park is forbidden before list service read", func(t *testing.T) {
		service := &transportScopeSpyService{}
		req := httptest.NewRequest(http.MethodGet, "/feed-transport/tasks?business_date=2026-07-29&park_id="+parkB, nil)
		ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}})
		recorder := httptest.NewRecorder()

		NewHandler(service, slog.Default()).GetTransportTasks(recorder, req.WithContext(ctx))

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if service.listCalls != 0 {
			t.Fatalf("list calls=%d, want 0", service.listCalls)
		}
	})

	t.Run("unrelated park grant does not widen feed transport read scope", func(t *testing.T) {
		service := &transportScopeSpyService{}
		req := httptest.NewRequest(http.MethodGet, "/feed-transport/tasks?business_date=2026-07-29&park_id="+parkA, nil)
		ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
			{Role: permissions.RoleHealthDirector, ScopeType: "park", ScopeID: parkA},
			{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkB},
		})
		recorder := httptest.NewRecorder()

		NewHandler(service, slog.Default()).GetTransportTasks(recorder, req.WithContext(ctx))

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if service.listCalls != 0 {
			t.Fatalf("list calls=%d, want 0", service.listCalls)
		}
	})
}

func TestPostTransportSubmitPassesAuthorizedParkScope(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-000000000001"
	)
	service := &transportScopeSpyService{}
	req := httptest.NewRequest(http.MethodPost, "/feed-transport/tasks/task-1/submit", strings.NewReader(`{"proof_ref":"proof-1"}`))
	req.SetPathValue("task_id", "task-1")
	req.Header.Set("Idempotency-Key", "transport-submit-0001")
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}})
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).PostTransportSubmit(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.submitInput.TaskID != "task-1" {
		t.Fatalf("task_id=%q, want task-1", service.submitInput.TaskID)
	}
	if got := service.submitInput.AuthorizedParkIDs; len(got) != 1 || got[0] != parkA {
		t.Fatalf("authorized parks=%v, want [%s]", got, parkA)
	}
}
