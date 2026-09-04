package e2e

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	workforcehttp "github.com/vgoats/goatos/backend/internal/workforce/adapters/http"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	localeActor    = "90000000-0000-4000-8000-000000000901"
	localeOperator = "91000000-0000-4000-8000-000000000901"
)

func TestAppBootstrapLocalizesBackendOwnedStringsFromLocaleHeader(t *testing.T) {
	mux := http.NewServeMux()
	svc := workforceapp.NewService(&bootstrapLocaleRepo{
		profile: domain.OperatorProfile{
			OperatorID:      localeOperator,
			UserID:          localeStringPtr(localeActor),
			DisplayCode:     "OP-E2E",
			DisplayName:     "Locale Operator",
			Status:          "active",
			PrimaryRoleHint: "operator",
			RowVersion:      1,
		},
		grants: []domain.GrantSummary{{
			GrantID:   "93000000-0000-4000-8000-000000000901",
			UserID:    localeActor,
			Role:      "operator",
			ScopeType: "tenant",
			ScopeID:   fxTenant,
			Status:    "active",
		}},
		// Nav is composed from department_module_grants (mig 000002); a principal with
		// no granted module gets no nav, so this locale fixture must grant one to have
		// any labels to localize.
		grantedModules: []string{"vaccination"},
		caps:           []domain.CapabilityAssignment{{CapabilityCode: "movement.execute", Status: "active"}},
	})
	workforcehttp.Register(mux, workforcehttp.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/app/bootstrap", nil)
	req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
	req.Header.Set("X-GoatOS-Actor-ID", localeActor)
	req.Header.Set(httpmiddleware.LocaleContextHeader, "hi")
	req.Header.Set(httpmiddleware.AcceptLanguageHeader, "en")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got domain.BootstrapResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertNavigationLabel(t, got.VisibleNavigation, "vaccination", "ड्राइव")
	assertNavigationMissing(t, got.VisibleNavigation, "calendar")
	assertQueueLabel(t, got.TaskQueueDescriptors, "assigned", "सौंपा गया काम")
	assertQueueLabel(t, got.TaskQueueDescriptors, "shifting", "शिफ्टिंग")
}

func assertNavigationLabel(t *testing.T, items []domain.BootstrapNavigationItem, key, want string) {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			if item.Label != want {
				t.Fatalf("navigation %q label=%q want %q", key, item.Label, want)
			}
			return
		}
	}
	t.Fatalf("navigation %q missing from %#v", key, items)
}

func assertNavigationMissing(t *testing.T, items []domain.BootstrapNavigationItem, key string) {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			t.Fatalf("navigation %q must be absent for vaccination-only operator, got %#v", key, items)
		}
	}
}

func assertQueueLabel(t *testing.T, items []domain.BootstrapTaskQueue, key, want string) {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			if item.Label != want {
				t.Fatalf("queue %q label=%q want %q", key, item.Label, want)
			}
			return
		}
	}
	t.Fatalf("queue %q missing from %#v", key, items)
}

func localeStringPtr(value string) *string {
	return &value
}

type bootstrapLocaleRepo struct {
	profile        domain.OperatorProfile
	grants         []domain.GrantSummary
	caps           []domain.CapabilityAssignment
	grantedModules []string
}

func (r *bootstrapLocaleRepo) GetMemberForActor(context.Context, string, string) (domain.OperatorProfile, error) {
	return r.profile, nil
}

func (r *bootstrapLocaleRepo) ListActiveGrantsForActor(context.Context, string, string) ([]domain.GrantSummary, error) {
	return r.grants, nil
}

func (r *bootstrapLocaleRepo) ListCapabilities(context.Context, string, string) ([]domain.CapabilityAssignment, error) {
	return r.caps, nil
}

func (r *bootstrapLocaleRepo) ListPersonAssignments(context.Context, string, string) ([]permissions.ModuleAssignment, error) {
	return nil, nil
}

func (r *bootstrapLocaleRepo) ListGrantedModuleKeys(context.Context, string, string) ([]string, error) {
	return r.grantedModules, nil
}

func (r *bootstrapLocaleRepo) GetDeviceForActor(context.Context, string, string, string) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) ListOperators(context.Context, ports.ListOperatorsParams) ([]domain.OperatorProfile, error) {
	return nil, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) CreateOperator(context.Context, ports.CreateOperatorCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) GetOperator(context.Context, string, string) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) UpdateOperator(context.Context, ports.UpdateOperatorCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) SetOperatorStatus(context.Context, ports.StatusCommand) (domain.OperatorProfile, error) {
	return domain.OperatorProfile{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) ListGrants(context.Context, string, string) ([]domain.GrantSummary, error) {
	return nil, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) CreateGrant(context.Context, ports.CreateGrantCommand) ([]domain.GrantSummary, error) {
	return nil, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) AssignCapability(context.Context, ports.CapabilityCommand) (domain.CapabilityAssignment, error) {
	return domain.CapabilityAssignment{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) RemoveCapability(context.Context, ports.RemoveCapabilityCommand) error {
	return ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) ListDevices(context.Context, string, string) ([]domain.DeviceSummary, error) {
	return nil, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) RevokeDevice(context.Context, ports.RevokeDeviceCommand) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) ListSourceCandidates(context.Context, ports.ListSourceCandidatesParams) ([]domain.SourceCandidate, error) {
	return nil, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) MapSourceCandidate(context.Context, ports.MapSourceCandidateCommand) (domain.SourceCandidate, error) {
	return domain.SourceCandidate{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) RejectSourceCandidate(context.Context, ports.RejectSourceCandidateCommand) (domain.SourceCandidate, error) {
	return domain.SourceCandidate{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) RegisterDevice(context.Context, ports.RegisterDeviceCommand) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) HeartbeatDevice(context.Context, ports.HeartbeatDeviceCommand) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}

func (r *bootstrapLocaleRepo) DeregisterDevice(context.Context, ports.DeregisterDeviceCommand) (domain.DeviceSummary, error) {
	return domain.DeviceSummary{}, ports.ErrNotFound
}
