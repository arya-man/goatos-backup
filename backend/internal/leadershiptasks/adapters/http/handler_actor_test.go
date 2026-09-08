package http

import (
	"context"
	"net/http"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestActorFromUsesResolvedPersonPermissionsBeforeRoleGrants(t *testing.T) {
	ctx := httpmiddleware.WithActorID(context.Background(), "11111111-1111-4111-8111-111111111111")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal}})
	ctx = httpmiddleware.WithPersonPermissions(ctx, []string{permissions.LeadershipTasksRead})
	actor := actorFrom((&http.Request{}).WithContext(ctx))
	if actor.CanRaise || actor.CanAct {
		t.Fatalf("person view-only ticks must remove role-derived raise/act: %+v", actor)
	}

	ctx = httpmiddleware.WithPersonPermissions(ctx, []string{permissions.LeadershipTasksRead, permissions.LeadershipTasksAct})
	actor = actorFrom((&http.Request{}).WithContext(ctx))
	if actor.CanRaise || !actor.CanAct {
		t.Fatalf("person oversee tick should grant act without raise: %+v", actor)
	}
}
