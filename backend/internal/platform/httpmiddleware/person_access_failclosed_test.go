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
}

func (s stubPersonAccess) ResolvePermissions(context.Context, string, string) ([]string, bool, error) {
	return s.perms, s.provisioned, s.err
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
			src:        stubPersonAccess{provisioned: true, perms: nil},
			wantAuthzd: true,
			why:        "someone the backfill has not reached must keep their job",
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
			got, _ := decideAuthorization(context.Background(), tc.src, route,
				[]string{permissions.RoleCEOInternal}, "tenant-1", "user-1")
			if got != tc.wantAuthzd {
				t.Fatalf("authorized=%v, want %v -- %s", got, tc.wantAuthzd, tc.why)
			}
		})
	}
}
