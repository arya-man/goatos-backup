package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
)

// TestMigrationEmbedsTheSeededDocuments pins migration 000308 to the embedded seed JSON: every
// document the golden test proves equal to the old code template must be the SAME bytes the
// database is seeded from, or the golden test proves nothing about what a tenant actually runs.
func TestMigrationEmbedsTheSeededDocuments(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000308_sop_driven_herd_operations.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	files := []string{"categories.json", "task_types.json"}
	for _, f := range sopseed.FollowUpDocuments {
		files = append(files, f)
	}
	for _, name := range files {
		doc, err := sopseed.Raw(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "$seed$"+strings.TrimSpace(string(doc))+"$seed$") {
			t.Fatalf("migration 000308 does not embed %s verbatim; regenerate the SQL from sopseed/%s", name, name)
		}
		var v any
		if err := json.Unmarshal(doc, &v); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
	}
}

// TestTaskTypeRegistryCoversEveryHookAndAnswerKind keeps the seeded registry honest: every engine
// hook the compiler knows is offered by exactly one task type, and no row names an unknown kind.
func TestTaskTypeRegistryCoversEveryHookAndAnswerKind(t *testing.T) {
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	hooks := map[string]int{}
	for _, tt := range reg {
		switch tt.AnswerKind {
		case AnswerKindNone, AnswerKindYesNo, AnswerKindSelect, AnswerKindMultiSelect, AnswerKindNumber, AnswerKindText:
		default:
			t.Fatalf("task type %q has answer kind %q", tt.Key, tt.AnswerKind)
		}
		if tt.EngineHook != "" {
			hooks[tt.EngineHook]++
		}
	}
	for _, h := range []string{EngineHookWeighKg, EngineHookTagKid, EngineHookRecordPen, EngineHookColostrum, EngineHookDeathVideo, EngineHookReturnAnimal} {
		if hooks[h] != 1 {
			t.Fatalf("engine hook %q must be offered by exactly one task type, found %d", h, hooks[h])
		}
	}
}
