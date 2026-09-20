package domain

import "testing"

// TestEveryEngineHookRefusesATap is the pin for the defect the 2026-09-20 E2E found by TAPPING:
// only the sale's tag step refused a by-hand completion, so the three purchase hooks could each be
// marked done from the phone — a load reading "arrived" while the ledger had it on the road,
// "screened" with no accepted aflatoxin round, or "decided" over an animal still waiting. Those
// are the exact disagreements the hooks exist to prevent.
//
// It is written over the HOOK LIST, not a fixed set of cases, so a hook added later without a
// refusal fails here instead of shipping tappable.
func TestEveryEngineHookRefusesATap(t *testing.T) {
	// Every engine hook a task type may declare, and whether a person may complete its step.
	// Only the hooks that shape a read or a label are tappable; the rest record a fact whose
	// owner is elsewhere.
	tappable := map[string]bool{
		EngineHookWeighKg:      true, // the operator types the weight
		EngineHookTagKid:       true, // the operator scans the RFID (gated separately)
		EngineHookRecordPen:    true, // the operator names the pen
		EngineHookColostrum:    true, // the operator feeds the kid
		EngineHookDeathVideo:   true, // the operator films it
		EngineHookReturnAnimal: true, // the operator walks the animal back
	}
	engineOwned := []string{
		EngineHookSaleTagAnimals,
		EngineHookFeedPurchaseReached,
		EngineHookToxinTestAccepted,
		EngineHookAnimalPurchaseDecision,
	}

	for _, hook := range engineOwned {
		action := WorkflowAction{EngineHook: hook}
		refusal := EngineCompletedStepRefusal(action)
		if refusal == nil {
			t.Fatalf("hook %q completes from an event elsewhere but a tap is not refused", hook)
		}
		if refusal.Error() == "" {
			t.Fatalf("hook %q refuses with no message; the person is owed where the work happens", hook)
		}
	}

	for hook := range tappable {
		if EngineCompletedStepRefusal(WorkflowAction{EngineHook: hook}) != nil {
			t.Fatalf("hook %q is the operator's own work and must stay tappable", hook)
		}
	}

	// A step with no hook at all is ordinary work.
	if EngineCompletedStepRefusal(WorkflowAction{}) != nil {
		t.Fatal("a step with no engine hook must be completable by hand")
	}
}
