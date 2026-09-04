package httpmiddleware

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

type stubPersonAccess struct {
	perms       []string
	provisioned bool
	err         error
	scopeMode   string
	parkIDs     []string
	scopeErr    error
}

func (s stubPersonAccess) ResolvePermissions(context.Context, string, string) ([]string, bool, error) {
	return s.perms, s.provisioned, s.err
}

func (s stubPersonAccess) ResolveParkScope(context.Context, string, string) (string, []string, bool, error) {
	if s.scopeErr != nil {
		return "", nil, true, s.scopeErr
	}
	return s.scopeMode, s.parkIDs, s.provisioned, nil
}

// The request path must never restore authority a person's ticks removed.
//
// A read error used to fall back to the ROLE path "rather than locking the farm out". That
// is fail-open on the one path this model exists to control: a person whose ticks
// deliberately dropped a role-derived permission got it back from a database blip. The
// deploy window before the migration runs is the ONE case that still falls back, and it is
// reported as not-provisioned rather than as an error precisely so the two can differ.
func TestPersonAccessFailsClosedOnErrorAndOpenOnlyBeforeProvisioning(t *testing.T) {
	route := permissions.Route{OperationID: "t", Permissions: []string{permissions.OperatorsRead}}
	// The role would allow this; the person's ticks do not.
	roleAllows := permissions.AuthorizeRoute(route, []string{permissions.RoleCEOInternal})
	if !roleAllows {
		t.Fatal("fixture is wrong: the role path must allow the route for this test to mean anything")
	}

	for _, tc := range []struct {
		name       string
		src        stubPersonAccess
		wantAuthzd bool
		why        string
	}{
		{
			name:       "read error",
			src:        stubPersonAccess{provisioned: true, err: errors.New("connection reset")},
			wantAuthzd: false,
			why:        "a blip restored role authority the ticks had removed",
		},
		{
			name:       "tables not provisioned yet",
			src:        stubPersonAccess{provisioned: false},
			wantAuthzd: true,
			why:        "the deploy window before the migration would 403 the whole farm",
		},
		{
			name:       "person has no rows",
			src:        stubPersonAccess{provisioned: false, perms: nil},
			wantAuthzd: true,
			why:        "someone the backfill has not reached must keep their job",
		},
		{
			name:       "person is provisioned with no ticks",
			src:        stubPersonAccess{provisioned: true, perms: nil},
			wantAuthzd: false,
			why:        "an admin clearing all ticks must not restore role authority",
		},
		{
			name:       "person is ticked and holds it",
			src:        stubPersonAccess{provisioned: true, perms: []string{permissions.OperatorsRead}},
			wantAuthzd: true,
			why:        "the ticks grant it",
		},
		{
			name:       "person is ticked and does NOT hold it",
			src:        stubPersonAccess{provisioned: true, perms: []string{permissions.RosterRead}},
			wantAuthzd: false,
			why:        "the ticks removed it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, got, _ := decideAuthorization(context.Background(), tc.src, route,
				[]string{permissions.RoleCEOInternal}, "tenant-1", "user-1")
			if got != tc.wantAuthzd {
				t.Fatalf("authorized=%v, want %v -- %s", got, tc.wantAuthzd, tc.why)
			}
		})
	}
}

func TestPersonAccessParkScopeConstrictsRuntimeGrantScope(t *testing.T) {
	ctx := WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{
			Role:      permissions.RoleCEOInternal,
			ScopeType: "tenant",
			ScopeID:   "tenant-1",
		},
	})
	route := permissions.Route{OperationID: "t", Permissions: []string{permissions.FeedDirectionRead}}
	ctx, authorized, source := decideAuthorization(ctx, stubPersonAccess{
		perms:       []string{permissions.FeedDirectionRead},
		provisioned: true,
		scopeMode:   "parks",
		parkIDs:     []string{"park-a"},
	}, route, []string{permissions.RoleCEOInternal}, "tenant-1", "user-1")
	if !authorized || source != "person" {
		t.Fatalf("authorized=%v source=%q, want person authorization", authorized, source)
	}
	if decision := ResolveAuthorizedParkScopeForCapabilities(ctx, "tenant-1", "park-b", permissions.FeedDirectionRead); decision.Allowed {
		t.Fatalf("park-b allowed through old tenant-wide role grant; person_park_scope must constrict runtime scope")
	}
	if decision := ResolveAuthorizedParkScopeForCapabilities(ctx, "tenant-1", "park-a", permissions.FeedDirectionRead); !decision.Allowed {
		t.Fatalf("park-a denied, want selected person_park_scope park allowed: %#v", decision)
	}
}

func TestPersonAccessScopeLookupFailureFailsClosed(t *testing.T) {
	route := permissions.Route{OperationID: "t", Permissions: []string{permissions.FeedDirectionRead}}
	_, authorized, source := decideAuthorization(context.Background(), stubPersonAccess{
		perms:       []string{permissions.FeedDirectionRead},
		provisioned: true,
		scopeErr:    errors.New("scope query failed"),
	}, route, []string{permissions.RoleCEOInternal}, "tenant-1", "user-1")
	if authorized || source != "person_unavailable" {
		t.Fatalf("authorized=%v source=%q, want fail-closed person_unavailable", authorized, source)
	}
}

// TestPersonDecisionLeavesTheResolvedPermissionSetOnTheContext pins the middleware half of
// PR #181 finding PC-181-002: when the person's rows decided, the SAME set they decided from is
// on the context for the module's own re-check; on the role path nothing is attached, so a
// module falls back to the role map exactly as before.
func TestPersonDecisionLeavesTheResolvedPermissionSetOnTheContext(t *testing.T) {
	route := permissions.Route{OperationID: "t", Permissions: []string{permissions.PCCarePlanTrimming}}
	held := []string{permissions.PCCareMonitor, permissions.PCCarePlanTrimming}
	ctx, authorized, source := decideAuthorization(context.Background(), stubPersonAccess{
		provisioned: true, perms: held, scopeMode: "tenant",
	}, route, []string{permissions.RoleOperator}, "t1", "u1")
	if !authorized || source != "person" {
		t.Fatalf("authorized=%v source=%s, want person-path allow", authorized, source)
	}
	got, ok := PersonPermissionsFromContext(ctx)
	if !ok || len(got) != 2 {
		t.Fatalf("PersonPermissionsFromContext = %v,%v; want the resolved set attached", got, ok)
	}

	ctx, _, source = decideAuthorization(context.Background(), stubPersonAccess{provisioned: false}, route, []string{permissions.RoleCEOInternal}, "t1", "u1")
	if source != "role" {
		t.Fatalf("source=%s, want role fallback", source)
	}
	if _, ok := PersonPermissionsFromContext(ctx); ok {
		t.Fatal("role path must attach no per-person set")
	}
}
