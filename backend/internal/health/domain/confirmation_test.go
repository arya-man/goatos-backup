package domain

import (
	"errors"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

func validProposal(problems ...string) diagnosis.Proposal {
	return diagnosis.Proposal{Valid: true, Scope: diagnosis.ScopeAdult, Problems: problems}
}

func confirmables(ids ...string) []ConfirmableProblem {
	out := make([]ConfirmableProblem, 0, len(ids))
	for _, id := range ids {
		out = append(out, ConfirmableProblem{ID: id, Tier: string(diagnosis.TierProbable)})
	}
	return out
}

// The Director may confirm some and decline the rest. A declined diagnosis is
// carried through, not dropped: an override is a decision the manager should see
// and the weekly review should count.
func TestPlanConfirmationSeparatesConfirmedFromDeclined(t *testing.T) {
	plan, err := PlanConfirmation(
		validProposal("MASTITIS", "FEVER", "PINKEYE"),
		confirmables("MASTITIS", "FEVER", "PINKEYE"),
		[]string{"MASTITIS", "PINKEYE"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := []string{}
	for _, c := range plan.Confirmed {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, []string{"MASTITIS", "PINKEYE"}) {
		t.Errorf("confirmed = %v, want [MASTITIS PINKEYE]", got)
	}
	if !reflect.DeepEqual(plan.Declined, []string{"FEVER"}) {
		t.Errorf("declined = %v, want [FEVER]", plan.Declined)
	}
}

// The gate. Confirming a diagnosis the engine never proposed must FAIL, not be
// silently ignored: ignoring it reports success for a course that never opened.
func TestPlanConfirmationRejectsAnUnproposedDiagnosis(t *testing.T) {
	_, err := PlanConfirmation(
		validProposal("MASTITIS"),
		confirmables("MASTITIS"),
		[]string{"MASTITIS", "TETANUS"},
	)
	if !errors.Is(err, ErrConfirmNotProposed) {
		t.Fatalf("want ErrConfirmNotProposed, got %v", err)
	}
}

// Confirming nothing is a legitimate override: the Director rejected the whole
// proposal. The run is still decided.
func TestPlanConfirmationAllowsConfirmingNothing(t *testing.T) {
	plan, err := PlanConfirmation(validProposal("FEVER"), confirmables("FEVER"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Confirmed) != 0 {
		t.Errorf("confirmed = %v, want none", plan.Confirmed)
	}
	if !reflect.DeepEqual(plan.Declined, []string{"FEVER"}) {
		t.Errorf("declined = %v, want [FEVER]", plan.Declined)
	}
}

// A rejected form and an out-of-scope animal both produce nothing confirmable,
// and neither may be confirmed.
func TestPlanConfirmationRefusesUndiagnosableRuns(t *testing.T) {
	t.Run("rejected form", func(t *testing.T) {
		p := diagnosis.Proposal{Valid: false, RejectReason: diagnosis.RejectNotEatingWithFeed, Scope: diagnosis.ScopeAdult}
		if _, err := PlanConfirmation(p, nil, nil); !errors.Is(err, ErrConfirmNotDiagnosable) {
			t.Fatalf("want ErrConfirmNotDiagnosable, got %v", err)
		}
	})
	t.Run("out-of-scope animal", func(t *testing.T) {
		p := diagnosis.Proposal{Valid: true, Scope: diagnosis.ScopeOutOfScope}
		if _, err := PlanConfirmation(p, nil, nil); !errors.Is(err, ErrConfirmNotDiagnosable) {
			t.Fatalf("want ErrConfirmNotDiagnosable, got %v", err)
		}
	})
}

// Every in-scope class can be confirmed, not just adults. confirmableFrom builds
// decision lists for the kid classes, so the gate here must accept the same set:
// a kid the Director can see choices for but never confirm is a dead end that
// reads as diagnosis_not_confirmable on the phone.
func TestPlanConfirmationAcceptsEveryInScopeClass(t *testing.T) {
	for _, scope := range []string{diagnosis.ClassKidMilk, diagnosis.ClassKidWeaning, diagnosis.ClassKidFattening, diagnosis.ScopeAdult} {
		p := diagnosis.Proposal{Valid: true, Scope: scope, Problems: []string{"PNEUMONIA"}}
		plan, err := PlanConfirmation(p, confirmables("PNEUMONIA"), []string{"PNEUMONIA"})
		if err != nil {
			t.Fatalf("scope %s: %v", scope, err)
		}
		if len(plan.Confirmed) != 1 || plan.Confirmed[0].ID != "PNEUMONIA" {
			t.Fatalf("scope %s: plan = %+v", scope, plan)
		}
	}
}

// A replay must write identical rows, so the plan is ordered by id regardless of
// the order the Director's client sent them in.
func TestPlanConfirmationIsOrderStable(t *testing.T) {
	first, err := PlanConfirmation(validProposal("A", "B", "C"), confirmables("A", "B", "C"), []string{"C", "A"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanConfirmation(validProposal("A", "B", "C"), confirmables("A", "B", "C"), []string{"A", "C"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("plan depends on client ordering:\n %+v\n %+v", first, second)
	}
}

// Only a Type F course carries a closure day count. This is maintainer decision
// D: 15 of the 34 register rules have no honest fixed length, and defaulting one
// stamps an end date on a wound course that the clinical contract refuses to
// state.
func TestCourseShapeOnlyGivesTypeFADuration(t *testing.T) {
	t.Run("fixed course closes on days", func(t *testing.T) {
		duration, horizon, err := CourseShapeFor(ExitTypeFixed, 3)
		if err != nil {
			t.Fatal(err)
		}
		if duration == nil || *duration != 3 {
			t.Errorf("duration = %v, want 3", duration)
		}
		if horizon != 3 {
			t.Errorf("horizon = %d, want 3", horizon)
		}
	})

	for _, exit := range []string{ExitTypeTest, ExitTypeDirector, ExitTypeSupportive} {
		t.Run(exit+" carries no duration", func(t *testing.T) {
			duration, horizon, err := CourseShapeFor(exit, 3)
			if err != nil {
				t.Fatal(err)
			}
			if duration != nil {
				t.Errorf("duration = %d, want nil -- %s does not close on a calendar", *duration, exit)
			}
			// The card's steps are still snapshotted; only the closure rule differs.
			if horizon != 3 {
				t.Errorf("horizon = %d, want 3 -- the card's steps still apply", horizon)
			}
		})
	}
}

// An unknown exit type is rejected rather than defaulted. Guessing a closure
// model decides when an animal stops being treated.
func TestCourseShapeRejectsUnknownExitType(t *testing.T) {
	if _, _, err := CourseShapeFor("fixed", 3); err == nil {
		t.Error("an unknown exit type must be rejected, not defaulted to F")
	}
	if _, _, err := CourseShapeFor(ExitTypeFixed, 0); err == nil {
		t.Error("a card with no days cannot open a course")
	}
}
