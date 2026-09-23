package httpmiddleware

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestCEOFloorIsNeverNarrowedByPersonRows is the request-path half of the CEO/CXO floor
// (permissions/ceo_floor.go): whatever a ceo_internal principal's stored rows say -- absent,
// cleared, missing the module, or unreadable -- a route the ROLE authorizes stays open, and
// the person park scope never constricts the CEO to a park.
//
// Mutation-tested when written: deleting the CEOFloorApplies branch in decideAuthorization
// turns every case but "rows hold it" red.
func TestCEOFloorIsNeverNarrowedByPersonRows(t *testing.T) {
	route := permissions.Route{OperationID: "t", Permissions: []string{permissions.OperatorsRead}}
	if !permissions.AuthorizeRoute(route, []string{permissions.RoleCEOInternal}) {
		t.Fatal("fixture is wrong: the CEO role must allow the route")
	}
	for _, tc := range []struct {
		name string
		src  stubPersonAccess
	}{
		{"rows cleared on /people", stubPersonAccess{provisioned: true, perms: nil}},
		{"rows predate the module", stubPersonAccess{provisioned: true, perms: []string{permissions.RosterRead}}},
		{"rows unreadable", stubPersonAccess{provisioned: true, err: errors.New("connection reset")}},
		{"rows narrow scope to one park", stubPersonAccess{provisioned: true, perms: []string{permissions.OperatorsRead}, scopeMode: "park", parkIDs: []string{"park-1"}}},
		{"rows hold it", stubPersonAccess{provisioned: true, perms: []string{permissions.OperatorsRead}, scopeMode: "tenant"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, got, source := decideAuthorization(context.Background(), tc.src, route,
				[]string{permissions.RoleCEOInternal, permissions.RoleProcurementDirector}, "tenant-1", "user-1")
			if !got {
				t.Fatalf("CEO was denied (decided_by=%s); the floor must hold", source)
			}
			if _, constricted := PersonParkScopeFromContext(ctx); constricted {
				t.Fatal("a person park scope was attached to the CEO; the CEO stays tenant-wide")
			}
		})
	}

	// The floor is a floor, not a bypass: a route the role does NOT allow is still decided
	// by the rows, so a recorded exclusion (verification.verdict is verifier-only) holds.
	verdict := permissions.Route{OperationID: "v", Permissions: []string{permissions.VerificationVerdict}}
	if permissions.AuthorizeRoute(verdict, []string{permissions.RoleCEOInternal}) {
		t.Fatal("fixture is wrong: the CEO role must not hold verification.verdict")
	}
	_, got, _ := decideAuthorization(context.Background(), stubPersonAccess{provisioned: true, perms: nil}, verdict,
		[]string{permissions.RoleCEOInternal}, "tenant-1", "user-1")
	if got {
		t.Fatal("the floor granted past the role; it must never widen authority")
	}
}
