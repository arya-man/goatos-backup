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

// TestActorFromMonitorIsAnExplicitTickNeverInferred pins the review finding on PR 295
// (2026-09-18): View + Oversee + Configure on Tasks -- the seeded CEO shape, and also what a
// director can be ticked -- must NOT unlock the tenant-wide monitor scope by itself. Only the
// `leadership_tasks_monitor` module's tick (leadership_tasks.monitor) does, and the role path
// only through a role that holds that permission.
func TestActorFromMonitorIsAnExplicitTickNeverInferred(t *testing.T) {
	combined := permissions.PermissionsForAssignments([]permissions.ModuleAssignment{{
		Module:       "leadership_tasks",
		Surface:      permissions.SurfaceMobile,
		Capabilities: []string{permissions.LevelView, permissions.LevelOversee, permissions.LevelConfigure},
	}})
	ctx := httpmiddleware.WithActorID(context.Background(), "11111111-1111-4111-8111-111111111111")
	actor := actorFrom((&http.Request{}).WithContext(httpmiddleware.WithPersonPermissions(ctx, combined)))
	if !actor.CanRaise || !actor.CanAct || actor.CanMonitor {
		t.Fatalf("raise + act ticks must not infer monitor: perms=%v actor=%+v", combined, actor)
	}

	desk := permissions.PermissionsForAssignments([]permissions.ModuleAssignment{
		{Module: "leadership_tasks", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView, permissions.LevelOversee, permissions.LevelConfigure}},
		{Module: "leadership_tasks_monitor", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelOversee}},
	})
	actor = actorFrom((&http.Request{}).WithContext(httpmiddleware.WithPersonPermissions(ctx, desk)))
	if !actor.CanMonitor {
		t.Fatalf("the leadership desk tick must unlock monitor: perms=%v actor=%+v", desk, actor)
	}

	// Role path (no person rows yet): the CEO/CXO role holds the permission; a director's does not.
	ceo := actorFrom((&http.Request{}).WithContext(httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal}})))
	director := actorFrom((&http.Request{}).WithContext(httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleFeedDirector}})))
	if !ceo.CanMonitor || director.CanMonitor {
		t.Fatalf("role path: ceo=%+v director=%+v", ceo, director)
	}
	if !permissions.RoleHasPermission(permissions.RoleCEOInternal, permissions.LeadershipTasksMonitor) {
		t.Fatal("ceo_internal must hold leadership_tasks.monitor")
	}
	for _, role := range []string{permissions.RoleFeedDirector, permissions.RolePCDirector, permissions.RoleGrowthDirector, permissions.RoleParkHead} {
		if permissions.RoleHasPermission(role, permissions.LeadershipTasksMonitor) {
			t.Fatalf("%s must not hold leadership_tasks.monitor", role)
		}
	}
}
