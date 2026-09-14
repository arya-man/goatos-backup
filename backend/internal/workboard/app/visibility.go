package app

import (
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// moduleVisibility says which permission lets a caller see a module's rows on the board.
// ANY listed permission suffices. This is how a director sees exactly their module: the
// Feed Director holds feed_direction.read and nothing in health, so health rows never
// reach them, without a role string anywhere.
var moduleVisibility = map[domain.Module][]string{
	domain.ModuleFeed:        {permissions.FeedDirectionRead, permissions.FeedDirectionComplete},
	domain.ModuleHealth:      {permissions.HealthRead, permissions.HealthExecute},
	domain.ModuleVaccination: {permissions.VaccinationRead, permissions.TaskExecute},
	domain.ModuleWeighing:    {permissions.WeighingMonitor, permissions.WeighingExecute},
	domain.ModuleCounts:      {permissions.CountsRead, permissions.CountsWrite, permissions.CountsApproveAccess},
	domain.ModuleMilk:        {permissions.CountsRead, permissions.CountsWrite},
	domain.ModulePCCare:      {permissions.PCCareMonitor, permissions.PCCareExecute, permissions.PCCarePlan},
	domain.ModuleToxin:       {permissions.ToxinRead},
	// ProcurementReview, not ProcurementRead: the read is held by every operator and park
	// head for the intake screens they work, and would put a procurement lane on every
	// phone. The review authority is the desk that owns a load's state.
	domain.ModuleProcurement:  {permissions.ProcurementReview},
	domain.ModuleVerification: {permissions.VerificationReview, permissions.VerificationVerdict},
	// Tasks (2026-09-14): the pen visits the kernel owes a park's visitors. Visible to whoever
	// can record one (the Tasks module's Do tick) and to whoever reads the Tasks module at all,
	// so a director or the CXO desk sees the park's visits beside the work that raised them.
	domain.ModuleTasks: {permissions.PenVisitsExecute, permissions.LeadershipTasksRead},
}

// VisibleModules returns, in board order, the modules a caller holding perms may see.
func VisibleModules(perms []string) []domain.Module {
	have := map[string]struct{}{}
	for _, p := range perms {
		have[p] = struct{}{}
	}
	out := []domain.Module{}
	for _, m := range domain.Modules() {
		for _, p := range moduleVisibility[m] {
			if _, ok := have[p]; ok {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// IntersectModules keeps the requested modules the caller may see. An empty request means
// everything visible. A request naming only invisible modules yields an empty set, which
// the read serves as an empty board rather than a 403 -- the board is a lens, not a gate.
func IntersectModules(requested, visible []domain.Module) []domain.Module {
	if len(requested) == 0 {
		return visible
	}
	return StrictIntersect(requested, visible)
}

// StrictIntersect is the plain set intersection, in the order of the first list, with NO
// empty-means-all shortcut. It is what "the modules a caller may see" must be computed with:
// the caller's permission-derived set against the registered sources. Using IntersectModules
// there inverted an EMPTY permission set into the whole registry (found live 2026-09-11: a
// person ticked for the Work Board with no module row saw, drilled and flagged every
// module's rows), because that function reads an empty first argument as "no filter".
func StrictIntersect(requested, visible []domain.Module) []domain.Module {
	vis := map[domain.Module]struct{}{}
	for _, m := range visible {
		vis[m] = struct{}{}
	}
	out := []domain.Module{}
	for _, m := range requested {
		if _, ok := vis[m]; ok {
			out = append(out, m)
		}
	}
	return out
}

// VisibilityPermissions returns every permission the visibility map names, so a caller
// resolving permissions from role grants knows which ones to test.
func VisibilityPermissions() []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, m := range domain.Modules() {
		for _, p := range moduleVisibility[m] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}
