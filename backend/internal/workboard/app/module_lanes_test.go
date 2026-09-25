package app

import (
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// TestEveryModuleDeclaresItsWorkBoardLane: every module the farm can be given access to either
// names the board lane its work rows under or says, in words, why it owns no board work. A
// module added to the capability catalog with neither fails here -- the defect this closes is
// Procurement and Toxin sitting on the board's module list with nothing feeding them.
func TestEveryModuleDeclaresItsWorkBoardLane(t *testing.T) {
	lanes, excluded := ModuleLanes()
	var missing []string
	for _, mod := range permissions.ModuleCapabilities() {
		_, hasLane := lanes[mod.Key]
		reason, isExcluded := excluded[mod.Key]
		switch {
		case hasLane && isExcluded:
			t.Errorf("module %q both names a board lane and says it owns no board work; pick one", mod.Key)
		case !hasLane && !isExcluded:
			missing = append(missing, mod.Key)
		case isExcluded && strings.TrimSpace(reason) == "":
			t.Errorf("module %q is excluded from the Work Board without a reason", mod.Key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("modules with no Work Board declaration (add them to moduleLanes with the lane their work rows under, or to notBoardWork with the reason they own no work): %v", missing)
	}
	known := map[string]struct{}{}
	for _, mod := range permissions.ModuleCapabilities() {
		known[mod.Key] = struct{}{}
	}
	for key, ls := range lanes {
		if _, ok := known[key]; !ok {
			t.Errorf("moduleLanes names %q, which is not in the capability catalog", key)
		}
		if len(ls) == 0 {
			t.Errorf("module %q names an empty lane list", key)
		}
		for _, l := range ls {
			if !domain.IsModule(string(l)) {
				t.Errorf("module %q names unknown board lane %q", key, l)
			}
		}
	}
	for key := range excluded {
		if _, ok := known[key]; !ok {
			t.Errorf("notBoardWork names %q, which is not in the capability catalog", key)
		}
	}
}

// TestEveryWorkBoardLaneIsVisibleToSomeone: a lane nobody's permissions open is a lane nobody
// sees, which is the same silent hole one layer over.
func TestEveryWorkBoardLaneIsVisibleToSomeone(t *testing.T) {
	for _, m := range domain.Modules() {
		if len(moduleVisibility[m]) == 0 {
			t.Errorf("board lane %q has no permission that opens it", m)
		}
	}
}
