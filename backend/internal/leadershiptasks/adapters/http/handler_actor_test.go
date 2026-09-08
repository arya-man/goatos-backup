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

func TestActorFromSeededDirectorPersonAccessStillRaises(t *testing.T) {
	perms := permissions.PermissionsForAssignments([]permissions.ModuleAssignment{{
		Module:       "leadership_tasks",
		Surface:      permissions.SurfaceMobile,
		Capabilities: []string{permissions.LevelView, permissions.LevelDo, permissions.LevelOversee, permissions.LevelConfigure},
	}})
	ctx := httpmiddleware.WithActorID(context.Background(), "11111111-1111-4111-8111-111111111111")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleFeedDirector}})
	ctx = httpmiddleware.WithPersonPermissions(ctx, perms)

	actor := actorFrom((&http.Request{}).WithContext(ctx))
	if !actor.CanRaise || !actor.CanAct || actor.CanMonitor {
		t.Fatalf("seeded director Tasks tick must keep raise/assignee act without CEO/COO monitor scope: perms=%v actor=%+v", perms, actor)
	}

	assigneePerms := permissions.PermissionsForAssignments([]permissions.ModuleAssignment{{
		Module:       "leadership_tasks",
		Surface:      permissions.SurfaceMobile,
		Capabilities: []string{permissions.LevelView, permissions.LevelOversee},
	}})
	ctx = httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleParkHead}})
	ctx = httpmiddleware.WithPersonPermissions(ctx, assigneePerms)

	actor = actorFrom((&http.Request{}).WithContext(ctx))
	if actor.CanRaise || !actor.CanAct || actor.CanMonitor {
		t.Fatalf("seeded assignee Tasks tick must act without raise: perms=%v actor=%+v", assigneePerms, actor)
	}
}

func TestActorFromCombinedLeadershipTaskAccessCanMonitor(t *testing.T) {
	perms := permissions.PermissionsForAssignments([]permissions.ModuleAssignment{{
		Module:       "leadership_tasks",
		Surface:      permissions.SurfaceMobile,
		Capabilities: []string{permissions.LevelView, permissions.LevelOversee, permissions.LevelConfigure},
	}})
	ctx := httpmiddleware.WithActorID(context.Background(), "11111111-1111-4111-8111-111111111111")
	ctx = httpmiddleware.WithPersonPermissions(ctx, perms)

	actor := actorFrom((&http.Request{}).WithContext(ctx))
	if !actor.CanRaise || !actor.CanAct || !actor.CanMonitor {
		t.Fatalf("combined Tasks access should unlock CEO/COO-style monitor scope: perms=%v actor=%+v", perms, actor)
	}
}
