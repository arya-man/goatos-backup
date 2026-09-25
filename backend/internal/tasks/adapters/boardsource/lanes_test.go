package boardsource

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// moduleCheck matches the engine's closed module list as a migration (re)declares it.
var moduleCheck = regexp.MustCompile(`(?s)workflow_instances_module_check\s+CHECK\s*\(\s*module\s+IN\s*\(([^)]*)\)`)

// liveEngineModules reads the module list the LAST migration's Up section declares for
// workflow_instances -- the set the database actually admits today.
func liveEngineModules(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	sort.Strings(files)
	var last []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		up := string(raw)
		if i := strings.Index(up, "-- +goose Down"); i >= 0 {
			up = up[:i]
		}
		for _, m := range moduleCheck.FindAllStringSubmatch(up, -1) {
			var mods []string
			for _, part := range strings.Split(m[1], ",") {
				if v := strings.Trim(strings.TrimSpace(part), "'"); v != "" {
					mods = append(mods, v)
				}
			}
			last = mods
		}
	}
	if len(last) == 0 {
		t.Fatal("workflow_instances_module_check not found in any migration")
	}
	return last
}

// TestEveryEngineModuleHasABoardLane: every module the shared tasks engine may store names the
// Work Board lane its workflows row under. An unnamed one would still row (under Tasks, the
// catch-all), but the lane is a decision, so the build stops until someone makes it.
func TestEveryEngineModuleHasABoardLane(t *testing.T) {
	for _, m := range liveEngineModules(t) {
		if _, ok := engineModuleLanes[m]; !ok {
			t.Errorf("engine module %q has no Work Board lane in engineModuleLanes", m)
		}
	}
}

func TestUnmappedEngineModuleStillRowsUnderTheCatchAll(t *testing.T) {
	if got := BoardLaneFor("some_future_module"); got != domain.ModuleTasks {
		t.Fatalf("an unmapped engine module rows under %q, want tasks", got)
	}
	var catchAll *Source
	for _, s := range Sources(nil, 0) {
		if s.catchAll {
			if catchAll != nil {
				t.Fatal("two catch-all sources")
			}
			catchAll = s
		}
	}
	if catchAll == nil || catchAll.Module() != domain.ModuleTasks {
		t.Fatal("no catch-all source under Tasks")
	}
	if len(catchAll.knownArg()) != len(engineModuleLanes) {
		t.Fatalf("catch-all excludes %d mapped modules, want %d", len(catchAll.knownArg()), len(engineModuleLanes))
	}
}

func TestSourcesCoverEveryMappedModuleExactlyOnce(t *testing.T) {
	seen := map[string]domain.Module{}
	lanes := map[domain.Module]bool{}
	for _, s := range Sources(nil, 0) {
		if lanes[s.Module()] {
			t.Fatalf("two sources for lane %q", s.Module())
		}
		lanes[s.Module()] = true
		if s.catchAll && s.knownArg() == nil {
			t.Fatal("the catch-all must bind the mapped set")
		}
		if !s.catchAll && s.knownArg() != nil {
			t.Fatalf("lane %q binds the mapped set but is not the catch-all", s.Module())
		}
		for _, m := range s.modules {
			if prev, dup := seen[m]; dup {
				t.Fatalf("engine module %q is in lanes %q and %q", m, prev, s.Module())
			}
			seen[m] = s.Module()
		}
	}
	for m, lane := range engineModuleLanes {
		if seen[m] != lane {
			t.Errorf("engine module %q is served by lane %q, want %q", m, seen[m], lane)
		}
	}
}
