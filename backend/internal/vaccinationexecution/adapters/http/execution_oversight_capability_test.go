package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// projection-review: app vaccination-execution read scope must be decided by a CAPABILITY
// (vaccination.oversee_execution), not by a hardcoded role-name allowlist. The org-role catalog's
// preventive-care Director/Head are the composed successors of the flat pc_director/park_head
// roles and hold no task.execute, so under the name allowlist they fell into the
// operator-assignment branch: zero rows (they are assigned no drive) and viewerReadOnly=false,
// i.e. a tappable shed whose every write is refused task_not_assigned.
func TestAppVaccinationExecutionOversightIsCapabilityNotRoleName(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const actorID = "90000000-0000-4000-8000-000000000104"
	const parkID = "30000000-0000-4000-8000-000000000001"

	roles := []string{
		permissions.RoleKey(permissions.TierDirector, permissions.VerticalPreventiveCare),
		permissions.RoleKey(permissions.TierHead, permissions.VerticalPreventiveCare),
	}
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			if !permissions.RoleHasPermission(role, permissions.VaccinationOverseeExecution) {
				t.Fatalf("%s must hold %s", role, permissions.VaccinationOverseeExecution)
			}
			reader := &fakeReader{executionPage: domain.ExecutionResponse{Source: domain.SourceAPI}}
			mux := http.NewServeMux()
			Register(mux, NewHandler(reader, &fakeWriter{}))

			ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(t.Context(), tenant), actorID)
			ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
				{Role: role, ScopeType: "park", ScopeID: parkID},
			})
			req := httptest.NewRequest(http.MethodGet, "/app/vaccination/execution", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if reader.last.OperatorScopeActorID != "" {
				t.Fatalf("oversight actor operator scope = %q, want empty (park-scoped oversight)", reader.last.OperatorScopeActorID)
			}
			if reader.last.ParkID == nil || *reader.last.ParkID != parkID {
				t.Fatalf("park scope = %v want %s", reader.last.ParkID, parkID)
			}
			var resp domain.ExecutionResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !resp.ViewerReadOnly {
				t.Fatalf("viewerReadOnly = false, want true (oversight is read-only)")
			}
		})
	}
}
