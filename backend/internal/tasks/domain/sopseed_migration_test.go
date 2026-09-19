package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	for code, f := range sopseed.FollowUpDocuments {
		if code == sopseed.SOPCodeGateVisitorCheck {
			continue // seeded by 000361; pinned by TestMigrationEmbedsTheGeneralSeed
		}
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

// TestMigrationEmbedsTheGeneralSeed pins the first general SOP (migration 000361) to its
// sopseed document byte for byte, the same way 000308 is pinned to the herd-operations seeds.
func TestMigrationEmbedsTheGeneralSeed(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000361_sop_kind_and_general_sops.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sopseed.Raw("general_gate_visitor_check.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "$seed$"+strings.TrimSpace(string(doc))+"$seed$") {
		t.Fatalf("migration 000361 does not embed general_gate_visitor_check.json verbatim")
	}
	// The seeded general document validates and compiles against the seeded registry, and its
	// branch is on the right question.
	dsl := loadSeeded(t, sopseed.SOPCodeGateVisitorCheck)
	reg, _ := SeededTaskTypes()
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("seeded general SOP: %v", problems)
	}
	track, ok := dsl.Track(GeneralTrackKey)
	if !ok {
		t.Fatalf("general SOP must carry the %q track", GeneralTrackKey)
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Module != ModuleGeneral || tmpl.Actions[2].AnswerGate == nil || tmpl.Actions[2].AnswerGate.Step != "from_other_farm" {
		t.Fatalf("compiled = module %s gate %+v", tmpl.Module, tmpl.Actions[2].AnswerGate)
	}
}
