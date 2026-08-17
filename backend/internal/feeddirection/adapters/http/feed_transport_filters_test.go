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

// ListPenSessionCaptures: these handler tests drive filters and completion bodies, not the
// multi-operator discovery read.
func (transportFilterService) ListPenSessionCaptures(
	context.Context, app.PenSessionCapturesInput,
) (app.PenSessionCapturesResult, error) {
	return app.PenSessionCapturesResult{}, nil
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
	listCalls            int
	listInput            app.ListTransportTasksInput
	submitCalls          int
	submitInput          app.SubmitTransportInput
	completePackingCalls int
	completePackingInput app.CompletePackingInput
	completePackingErr   error

	completeDistributionCalls int
	completeDistributionInput app.CompleteDistributionInput
	completeDistributionErr   error
}

func (s *transportScopeSpyService) CompleteDistribution(
	_ context.Context, in app.CompleteDistributionInput,
) (ports.CompleteDistributionResult, error) {
	s.completeDistributionCalls++
	s.completeDistributionInput = in
	if s.completeDistributionErr != nil {
		return ports.CompleteDistributionResult{}, s.completeDistributionErr
	}
	return ports.CompleteDistributionResult{
		CompletionID: "50000000-0000-4000-8000-000000000002",
		Status:       "pending_verification",
		NewlyPending: true,
	}, nil
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

func (s *transportScopeSpyService) CompletePacking(_ context.Context, in app.CompletePackingInput) (ports.CompletePackingResult, error) {
	s.completePackingCalls++
	s.completePackingInput = in
	if s.completePackingErr != nil {
		return ports.CompletePackingResult{}, s.completePackingErr
	}
	return ports.CompletePackingResult{CompletionID: "50000000-0000-4000-8000-000000000001", Status: "pending_verification", NewlyPending: true}, nil
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

func TestPostCompletePackingPassesPartitionLabel(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
	)
	service := &transportScopeSpyService{}
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/packing/complete", strings.NewReader(`{
		"park_id":"20000000-0000-4000-8000-000000000001",
		"shed_id":"30000000-0000-4000-8000-000000000001",
		"partition_label":" Castro 1 ",
		"session_no":1,
		"target_date":"2026-08-13",
		"workflow":"normal",
		"packing_proof_ref":"proof-feed-packing-video-1"
	}`))
	req.Header.Set("Idempotency-Key", "feed-packing-complete-test-0001")
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).PostCompletePacking(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.completePackingCalls != 1 {
		t.Fatalf("complete packing calls=%d, want 1", service.completePackingCalls)
	}
	if got := service.completePackingInput.PartitionLabel; got != "Castro 1" {
		t.Fatalf("partition_label=%q, want Castro 1", got)
	}
}

// TestPostCompleteDistributionPassesPartitionLabel is the DISTRIBUTION twin of the packing test
// above, and its absence is why the defect shipped: packing declared partition_label and was pinned
// by a test, distribution declared it in OpenAPI and never in the Go request struct, so
// encoding/json dropped the pen silently.
//
// Consequence when it was missing: the write path saw a blank pen, resolved the completion to the
// shed as a whole, and the catalog check rejected it with ErrInvalidPartition -- a 400 that made
// feed distribution UNSUBMITTABLE on every partitioned shed. Reported 2026-08-13 for Castro - 1
// session 2, and reproduced against the live local API before this test was written.
//
// The padded input also pins the trim, so " 1 " and "1" cannot become two different pens.
func TestPostCompleteDistributionPassesPartitionLabel(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
	)
	service := &transportScopeSpyService{}
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/distribution/complete", strings.NewReader(`{
		"park_id":"20000000-0000-4000-8000-000000000001",
		"shed_id":"30000000-0000-4000-8000-000000000001",
		"partition_label":" 1 ",
		"session_no":2,
		"target_date":"2026-08-13",
		"workflow":"experiment",
		"feed_weight_proof_ref":"proof-feed-weight-photo-1",
		"distribution_proof_ref":"proof-feed-distribution-video-1",
		"water_proof_ref":"proof-water-video-1"
	}`))
	req.Header.Set("Idempotency-Key", "feed-distribution-complete-test-0001")
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).PostCompleteDistribution(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.completeDistributionCalls != 1 {
		t.Fatalf("complete distribution calls=%d, want 1", service.completeDistributionCalls)
	}
	if got := service.completeDistributionInput.PartitionLabel; got != "1" {
		t.Fatalf("partition_label=%q, want 1 -- the pen the phone sent must reach the service", got)
	}
}

// TestPostCompleteDistributionMissingProofIsUnprocessable pins that EVERY mandatory distribution
// proof reports the same client error, one per step.
//
// The feed-weight photo was added as a third mandatory proof but never given a branch in
// writeCompletionError, so it fell to the default arm and answered 500 -- a server fault, for an
// operator who simply had not taken the photo yet. The distribution video and water video, added
// earlier, both map to 422 proof_required. Two consequences of the 500: the phone shows a generic
// failure instead of naming the capture that is missing, and a retryable-looking server error
// invites a retry loop against a request that can never succeed until the operator shoots it.
//
// RequireLiveCamera routes through the SAME error, so a weight photo picked from the gallery lands
// here too -- the case most likely to be hit in the field, since the phone offers a picker.
func TestPostCompleteDistributionMissingProofIsUnprocessable(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
	)
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "feed weight photo", err: ports.ErrFeedWeightProofRequired},
		{name: "distribution video", err: ports.ErrDistributionProofRequired},
		{name: "water video", err: ports.ErrWaterProofRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &transportScopeSpyService{completeDistributionErr: tc.err}
			req := httptest.NewRequest(http.MethodPost, "/feed-direction/distribution/complete", strings.NewReader(`{
				"park_id":"20000000-0000-4000-8000-000000000001",
				"shed_id":"30000000-0000-4000-8000-000000000001",
				"partition_label":"1",
				"session_no":1,
				"target_date":"2026-08-13",
				"workflow":"experiment",
				"feed_weight_proof_ref":"proof-feed-weight-photo-1",
				"distribution_proof_ref":"proof-feed-distribution-video-1",
				"water_proof_ref":"proof-water-video-1"
			}`))
			req.Header.Set("Idempotency-Key", "feed-distribution-complete-test-0003")
			ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
			recorder := httptest.NewRecorder()

			NewHandler(service, slog.Default()).PostCompleteDistribution(recorder, req.WithContext(ctx))

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d, want 422 -- a missing capture is the operator's to fix, not a server fault; body=%s",
					recorder.Code, recorder.Body.String())
			}
			if body := recorder.Body.String(); !strings.Contains(body, `"proof_required"`) {
				t.Fatalf("body=%s, want code proof_required so the client can tell this apart from a real failure", body)
			}
		})
	}
}

func TestPostCompletePackingInvalidPartitionIsBadRequest(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
	)
	service := &transportScopeSpyService{completePackingErr: ports.ErrInvalidPartition}
	req := httptest.NewRequest(http.MethodPost, "/feed-direction/packing/complete", strings.NewReader(`{
		"park_id":"20000000-0000-4000-8000-000000000001",
		"shed_id":"30000000-0000-4000-8000-000000000001",
		"session_no":1,
		"target_date":"2026-08-13",
		"workflow":"normal",
		"packing_proof_ref":"proof-feed-packing-video-1"
	}`))
	req.Header.Set("Idempotency-Key", "feed-packing-complete-test-0002")
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).PostCompletePacking(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "partition_label") {
		t.Fatalf("body=%s, want partition_label explanation", recorder.Body.String())
	}
}

// DirectedAnalytics: these handler tests drive transport filters, not analytics.
func (transportFilterService) DirectedAnalytics(
	context.Context, app.DirectedAnalyticsInput,
) (domain.DirectedAnalytics, error) {
	return domain.DirectedAnalytics{}, nil
}

func (transportFilterService) ExecutionAnalytics(
	context.Context, app.DirectedAnalyticsInput,
) (domain.ExecutionAnalytics, error) {
	return domain.ExecutionAnalytics{}, nil
}

func (transportFilterService) ExperimentAnalytics(
	context.Context, app.DirectedAnalyticsInput,
) (domain.ExperimentAnalytics, error) {
	return domain.ExperimentAnalytics{}, nil
}

func (transportFilterService) StockAnalytics(
	context.Context, app.DirectedAnalyticsInput,
) (domain.StockAnalytics, error) {
	return domain.StockAnalytics{}, nil
}
