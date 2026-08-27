package app

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Simulates the cutover against the REAL STG roster: for every person, compose the phone bar
// the way it works TODAY and the way it will work after the backfill, and report any
// difference. Nothing is written; the roster is a read-only export.
func TestSTGCutoverSimulation(t *testing.T) {
	f, err := os.Open("/tmp/stg_people.txt")
	if err != nil {
		t.Skip("no STG roster export")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	losses, gains, people := 0, 0, 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		name, roleStr, deptStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if roleStr == "(none)" {
			continue
		}
		people++
		roles := strings.Split(roleStr, "+")
		dept := []string{}
		if deptStr != "-" {
			dept = strings.Split(deptStr, ",")
		}
		grants := make([]domain.GrantSummary, 0, len(roles))
		for _, r := range roles {
			g := grant()
			g.Role = r
			grants = append(grants, g)
		}

		// TODAY: the offered list is the department's (or the curated leadership drawer),
		// filtered by what the ROLE map says this principal may do.
		old := moduleKeySet(modulesFor(grants, dept, "en"))

		// AFTER: the SAME offered list, filtered by what this person's own ticks grant.
		assignments := permissions.FillDefaultPages(
			permissions.NarrowForRetiredLenses(roles, permissions.AssignmentsForRoles(roles)))
		ticked := []string(nil)
		if !isStandaloneVerifierPrincipal(grants) {
			ticked = []string{}
			for _, a := range assignments {
				if a.Surface == permissions.SurfaceMobile && len(a.Capabilities) > 0 {
					ticked = append(ticked, a.Module)
				}
			}
		}
		scope := scopeOf(grants)
		if !isStandaloneVerifierPrincipal(grants) {
			held := map[string]struct{}{}
			for _, perm := range permissions.PermissionsForAssignmentsWithBaseline(assignments) {
				held[perm] = struct{}{}
			}
			scope.held = held
		}
		// The ticks ARE the offer (maintainer decision 2026-08-28) -- except for a standalone
		// verifier, whose modules come from her verify DUTIES and whose offer the service
		// deliberately leaves alone. Mirror that here or the sim invents a loss the product
		// does not have.
		offer := ticked
		if offer == nil {
			offer = dept
		}
		now := moduleKeySet(modulesForScope(scope, offer, "en", ticked != nil, ticked))

		for k := range old {
			if _, ok := now[k]; !ok {
				t.Errorf("STG %s (%s) LOSES phone module %q", name, roleStr, k)
				losses++
			}
		}

		for k := range now {
			if _, ok := old[k]; !ok {
				// Expected since the ticks became authoritative; the maintainer approved the
				// measured list. A LOSS is still a failure.
				t.Logf("STG %s (%s) gains %q", name, roleStr, k)
				gains++
			}
		}
	}
	t.Logf("simulated %d STG people: %d losses, %d gains", people, losses, gains)
}
