package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
)

// TestMigrationEmbedsTheProcurementSeed pins the animal-purchase intake SOP (migration 000371)
// and its task type to their sopseed files byte for byte, and proves the seeded document
// compiles: seven steps, the decision step hooked for the engine, every step owned by a
// designation, and the health-team step on the "Yes" branch of the arrival question.
func TestMigrationEmbedsTheProcurementSeed(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000371_procurement_animal_purchase_intake_sop.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"procurement_animal_purchase_intake.json", "task_types_procurement.json"} {
		doc, err := sopseed.Raw(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "$seed$"+strings.TrimSpace(string(doc))+"$seed$") {
			t.Fatalf("migration 000371 does not embed %s verbatim", name)
		}
	}
	dsl := loadSeeded(t, sopseed.SOPCodeAnimalPurchaseIntake)
	reg, _ := SeededTaskTypes()
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("seeded intake SOP: %v", problems)
	}
	track, ok := dsl.Track(TemplateKeyAnimalPurchaseIntake)
	if !ok {
		t.Fatalf("intake SOP must carry the %q track", TemplateKeyAnimalPurchaseIntake)
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Module != ModuleProcurement || len(tmpl.Actions) != 7 {
		t.Fatalf("compiled = module %s, %d steps", tmpl.Module, len(tmpl.Actions))
	}
	// The office's decision is the engine's, never a tap: a load must not read "decided" while an
	// animal sits unanswered.
	var decision ActionTemplate
	for _, a := range tmpl.Actions {
		if a.Key == "decision" {
			decision = a
		}
		if a.Owner == "" {
			t.Fatalf("seeded step %q names no designation", a.Key)
		}
	}
	if decision.EngineHook != EngineHookAnimalPurchaseDecision {
		t.Fatalf("decision step hook = %q", decision.EngineHook)
	}
	last := tmpl.Actions[len(tmpl.Actions)-1]
	if last.AnswerGate == nil || last.AnswerGate.Step != "arrival_condition" || last.AnswerGate.Value[0] != "yes" {
		t.Fatalf("health-team step gate = %+v", last.AnswerGate)
	}
	// The arrival is PROVEN, not asserted: a video off the vehicle and a photo at the seller.
	proofs := map[string]FollowUpProof{}
	for _, a := range tmpl.Actions {
		proofs[a.Key] = a.Proof
	}
	if proofs["arrival_video"].Video != 1 || proofs["loading_photo"].Photo != 1 {
		t.Fatalf("proof counts = %+v", proofs)
	}
}
