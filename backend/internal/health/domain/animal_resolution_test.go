package domain

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

func adultDoe() GoatFacts {
	return GoatFacts{Species: "goat", Sex: "female", AgeBand: AgeBandAdult, LifecycleStatus: "alive"}
}

func days(n int) *int { return &n }

// GoatOS says female/male; the register gates on F/M. A wrong mapping does not
// misfire -- it silently disables MASTITIS on every doe or CALCULI on every buck.
func TestResolveAnimalMapsSex(t *testing.T) {
	for goatOS, want := range map[string]string{"female": "F", "male": "M", "FEMALE": "F", " male ": "M"} {
		facts := adultDoe()
		facts.Sex = goatOS
		got, err := ResolveAnimal(facts)
		if err != nil {
			t.Fatalf("sex %q: %v", goatOS, err)
		}
		if got.Sex != want {
			t.Errorf("sex %q -> %q, want %q", goatOS, got.Sex, want)
		}
	}
}

// An unrecognised sex or species is refused, never defaulted. Defaulting picks a
// half of the register to switch off, silently.
func TestResolveAnimalRefusesRatherThanDefaulting(t *testing.T) {
	t.Run("unknown sex", func(t *testing.T) {
		facts := adultDoe()
		facts.Sex = "F" // already-mapped value is NOT what GoatOS stores
		if _, err := ResolveAnimal(facts); !errors.Is(err, ErrGoatNotDiagnosable) {
			t.Errorf("want ErrGoatNotDiagnosable, got %v", err)
		}
	})
	t.Run("empty sex", func(t *testing.T) {
		facts := adultDoe()
		facts.Sex = ""
		if _, err := ResolveAnimal(facts); !errors.Is(err, ErrGoatNotDiagnosable) {
			t.Errorf("want ErrGoatNotDiagnosable, got %v", err)
		}
	})
	t.Run("unknown species", func(t *testing.T) {
		facts := adultDoe()
		facts.Species = "cow"
		if _, err := ResolveAnimal(facts); !errors.Is(err, ErrGoatNotDiagnosable) {
			t.Errorf("want ErrGoatNotDiagnosable, got %v", err)
		}
	})
}

// Status precedence. Periparturient must beat lactating, because a doe within 14
// days of kidding is also nursing -- and if lactating won, MILK_FEVER and
// METRITIS could never fire on the exact animals they exist for.
func TestResolveAnimalStatusPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		stage string
		since *int
		want  string
	}{
		{"fresh doe is periparturient", "Mother", days(3), AnimalStatusPeriparturient},
		{"periparturient beats a pregnant stage", "Pregnant", days(1), AnimalStatusPeriparturient},
		{"day 14 is still periparturient", "Mother", days(14), AnimalStatusPeriparturient},
		{"day 15 is not", "Mother", days(15), AnimalStatusLactating},
		{"nursing doe with an old kidding is lactating", "Mother", days(90), AnimalStatusLactating},
		{"pregnant with no kidding record", "Pregnant", nil, AnimalStatusPregnant},
		{"plain adult", "Adult", nil, AnimalStatusNormal},
		{"unknown stage falls back to normal", "Flushing", nil, AnimalStatusNormal},
		{"stage spelling is normalised", "ICU-Kid", nil, AnimalStatusNormal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := adultDoe()
			facts.ManagementStage = tc.stage
			facts.DaysSinceKidding = tc.since
			got, err := ResolveAnimal(facts)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q", got.Status, tc.want)
			}
		})
	}
}

// The whole reason the periparturient window exists: it is the only key that
// unlocks MILK_FEVER and METRITIS. This asserts the consequence end to end
// against the real register, not just the mapping in isolation.
func TestPeriparturientIsWhatUnlocksMilkFever(t *testing.T) {
	reg, err := diagnosis.AdultRegister()
	if err != nil {
		t.Fatalf("load register: %v", err)
	}
	temp := 101.0
	findings := diagnosis.Findings{
		Temp:      &temp,
		Eating:    diagnosis.MultiValue{"not_eating"},
		Activity:  "down",
		Lactation: "milk",
	}

	fresh := adultDoe()
	fresh.ManagementStage = "Mother"
	fresh.DaysSinceKidding = days(2)
	freshAnimal, err := ResolveAnimal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	freshProposal := reg.Evaluate(freshAnimal, findings, diagnosis.Context{})

	stale := adultDoe()
	stale.ManagementStage = "Mother"
	stale.DaysSinceKidding = days(60)
	staleAnimal, err := ResolveAnimal(stale)
	if err != nil {
		t.Fatal(err)
	}
	staleProposal := reg.Evaluate(staleAnimal, findings, diagnosis.Context{})

	if !containsString(freshProposal.Problems, "MILK_FEVER") {
		t.Errorf("a down, off-feed doe 2 days after kidding must reach MILK_FEVER; got %v", freshProposal.Problems)
	}
	if containsString(staleProposal.Problems, "MILK_FEVER") {
		t.Errorf("the same doe 60 days after kidding must NOT reach MILK_FEVER; got %v", staleProposal.Problems)
	}
	// The calcium emergency shares the same gate.
	if !containsString(freshProposal.Emergencies, diagnosis.EmergencyCalcium) {
		t.Errorf("fresh doe, down, must raise the calcium emergency; got %v", freshProposal.Emergencies)
	}
}

// A doe with no recorded kidding is diagnosable, but the caller must be able to
// tell the Director that her status could not be decided on evidence -- otherwise
// a quieter proposal reads as a healthier animal.
func TestMissingKiddingHistoryIsReportedNotFatal(t *testing.T) {
	facts := adultDoe()
	facts.ManagementStage = "Mother"
	if _, err := ResolveAnimal(facts); err != nil {
		t.Fatalf("a doe with no birth record must still be diagnosable: %v", err)
	}
	if !MissingKiddingHistory(facts) {
		t.Error("a female with no recorded kidding must be reported as unknown history")
	}

	withHistory := adultDoe()
	withHistory.DaysSinceKidding = days(30)
	if MissingKiddingHistory(withHistory) {
		t.Error("a doe with a recorded kidding has known history")
	}

	buck := adultDoe()
	buck.Sex = "male"
	if MissingKiddingHistory(buck) {
		t.Error("a buck has no kidding history to miss")
	}
}

// TestResolveAnimalRefusesAKidRatherThanGuessingItsClass pins a fail-closed
// decision.
//
// The engine now carries three kid registers and they differ in ways that make
// the wrong one worse than none: a fattening kid diagnosed off the milk register
// is never checked for acidosis, the thing most likely to kill it, and a milk
// kid diagnosed off the weaning register never gets the drop test, the only way
// floppy kid is caught early.
//
// GoatOS cannot yet tell the three apart -- `age_band` is only kid|adult and
// nothing maps management stage onto the milk / weaning / fattening split or the
// K0-K3 sub-stage. Until that mapping is a maintainer decision, a kid is REFUSED.
//
// This replaces an earlier test that asserted a kid resolved to `kid_milk`. That
// default was harmless while kids were out of diagnostic scope entirely, and
// became dangerous the moment the milk register started producing diagnoses.
func TestResolveAnimalRefusesAKidRatherThanGuessingItsClass(t *testing.T) {
	facts := adultDoe()
	facts.AgeBand = AgeBandKid

	got, err := ResolveAnimal(facts)
	if err == nil {
		t.Fatalf("a kid was resolved to class %q instead of being refused", got.Class)
	}
	if !errors.Is(err, ErrGoatNotDiagnosable) {
		t.Errorf("error = %v, want it to wrap ErrGoatNotDiagnosable", err)
	}
	if got.Class != "" {
		t.Errorf("a refused animal must carry no class, got %q", got.Class)
	}
}

// TestResolveAnimalClassesAnAdultAsAdult is the other half: the refusal above
// must not have made the ordinary path fail too.
func TestResolveAnimalClassesAnAdultAsAdult(t *testing.T) {
	got, err := ResolveAnimal(adultDoe())
	if err != nil {
		t.Fatal(err)
	}
	if got.Class != diagnosis.ClassAdult {
		t.Errorf("class = %q, want %q", got.Class, diagnosis.ClassAdult)
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
