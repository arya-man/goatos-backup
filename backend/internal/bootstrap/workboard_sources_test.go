package bootstrap

import (
	"testing"

	workboardapp "github.com/vgoats/goatos/backend/internal/workboard/app"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// TestEveryWorkBoardLaneHasASource: every lane on the board's module list, and every lane a
// module declares its work rows under, has at least one registered source. Procurement and
// Toxin were listed for two weeks with nothing feeding them and the board hid them without a
// word; this is the check that would have failed.
func TestEveryWorkBoardLaneHasASource(t *testing.T) {
	svc := workboardapp.NewService(newWorkBoardSources(nil, 0, nil)...)
	registered := map[domain.Module]struct{}{}
	for _, m := range svc.RegisteredModules() {
		registered[m] = struct{}{}
	}
	for _, m := range domain.Modules() {
		if _, ok := registered[m]; !ok {
			t.Errorf("board lane %q has no registered source: its work would never show", m)
		}
	}
	lanes, _ := workboardapp.ModuleLanes()
	for key, ls := range lanes {
		for _, l := range ls {
			if _, ok := registered[l]; !ok {
				t.Errorf("module %q rows under lane %q, which has no registered source", key, l)
			}
		}
	}
}
