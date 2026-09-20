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
		switch code {
		case sopseed.SOPCodeGateVisitorCheck:
			continue // seeded by 000361; pinned by TestMigrationEmbedsTheGeneralSeed
		case sopseed.SOPCodeSalesDeal:
			continue // seeded by 000369; pinned by TestMigrationEmbedsTheSalesSeed
		case sopseed.SOPCodeAnimalPurchaseIntake:
			continue // seeded by 000371; pinned by TestMigrationEmbedsTheProcurementSeed
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
	for _, h := range []string{EngineHookWeighKg, EngineHookTagKid, EngineHookRecordPen, EngineHookColostrum, EngineHookDeathVideo, EngineHookReturnAnimal, EngineHookSaleTagAnimals} {
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

// TestMigrationEmbedsTheSalesSeed pins the sale SOP (migration 000366) and its task type to their
// sopseed files byte for byte, and proves the seeded document compiles: five steps, the tag step
// hooked for the engine, every step owned by a designation, and the balance step on the "No" branch
// of the payment question.
func TestMigrationEmbedsTheSalesSeed(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000369_sales_sop.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sales_deal.json", "task_types_sales.json"} {
		doc, err := sopseed.Raw(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "$seed$"+strings.TrimSpace(string(doc))+"$seed$") {
			t.Fatalf("migration 000366 does not embed %s verbatim", name)
		}
	}
	dsl := loadSeeded(t, sopseed.SOPCodeSalesDeal)
	reg, _ := SeededTaskTypes()
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("seeded sale SOP: %v", problems)
	}
	track, ok := dsl.Track(TemplateKeySalesDeal)
	if !ok {
		t.Fatalf("sale SOP must carry the %q track", TemplateKeySalesDeal)
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Module != ModuleSales || len(tmpl.Actions) != 5 {
		t.Fatalf("compiled = module %s, %d steps", tmpl.Module, len(tmpl.Actions))
	}
	if tmpl.Actions[0].EngineHook != EngineHookSaleTagAnimals || tmpl.Actions[0].Owner != "park_head" {
		t.Fatalf("tag step = hook %q owner %q", tmpl.Actions[0].EngineHook, tmpl.Actions[0].Owner)
	}
	for _, a := range tmpl.Actions {
		if a.Owner == "" {
			t.Fatalf("seeded step %q names no designation", a.Key)
		}
	}
	last := tmpl.Actions[4]
	if last.AnswerGate == nil || last.AnswerGate.Step != "full_payment" || last.AnswerGate.Value[0] != "no" {
		t.Fatalf("balance step gate = %+v", last.AnswerGate)
	}
}
