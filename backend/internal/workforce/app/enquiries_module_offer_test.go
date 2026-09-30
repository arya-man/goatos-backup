package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestEnquiryAuthorityOffersTheForMeTab pins the phone half of the 2026-09-30 HRMS decision: a
// park head's own ticks carry `enquiries` (the park-head job default) and NOT the Tasks module,
// so without this offer the enquiry card would have no screen on the phone at all -- found on the
// device. Holding the authority adds the Tasks module, landing on For me because "Raised by me"
// stays gated on leadership_tasks.read; clearing the tick takes it away again.
//
// Mutation-tested when written: deleting the offer turns the first case red.
func TestEnquiryAuthorityOffersTheForMeTab(t *testing.T) {
	modulesFromTicks := func(ticked []string, assignments []permissions.ModuleAssignment) map[string]string {
		scope := scopeOf([]domain.GrantSummary{grantWithRole(permissions.RoleParkHead)})
		scope.held = map[string]struct{}{}
		for _, p := range permissions.PermissionsForAssignmentsWithBaseline(assignments) {
			scope.held[p] = struct{}{}
		}
		hrefs := map[string]string{}
		for _, m := range modulesForScope(scope, nil, "en", true, ticked) {
			hrefs[m.Key] = m.Href
		}
		return hrefs
	}

	t.Run("a park head ticked for enquiries lands on For me", func(t *testing.T) {
		keys := modulesFromTicks([]string{"sale_allocation", "enquiries"}, []permissions.ModuleAssignment{
			{Module: "sale_allocation", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
			{Module: "enquiries", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
		})
		if keys["leadership_tasks"] != "/pen-visits" {
			t.Fatalf("the enquiry authority must offer Tasks landing on For me; got %v", keys)
		}
	})

	t.Run("a park head whose enquiries tick was cleared gets no Tasks module", func(t *testing.T) {
		keys := modulesFromTicks([]string{"sale_allocation"}, []permissions.ModuleAssignment{
			{Module: "sale_allocation", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
		})
		if _, ok := keys["leadership_tasks"]; ok {
			t.Fatalf("no enquiry authority, no Tasks module; got %v", keys)
		}
	})
}
