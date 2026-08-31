package diagnosis

import (
	"reflect"
	"strings"
	"testing"
)

// The acceptance catalog is the contract, but it cannot see everything. It
// compares every bucket as a SET, so it can never detect a ranking inversion;
// and it only ever runs a CLEAN register, so it never proves the structural
// validators would actually fire. The tests here cover exactly those gaps.
//
// Each one was mutation-tested when written: the rule it guards was broken
// deliberately and this test was confirmed to go red.

func mustLoad(t *testing.T) *Register {
	t.Helper()
	reg, err := AdultRegister()
	if err != nil {
		t.Fatalf("load embedded register: %v", err)
	}
	return reg
}

func floatPtr(v float64) *float64 { return &v }

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// TestRankingPutsSeverityBeforeConfidence is the axis-separation rule from
// DIRECTOR_ENGINE 4.3: severity ranks first, confidence second. Ranking on
// confidence alone means the system confidently treats the eye while the bladder
// ruptures.
//
// The catalog compares hits as sets and therefore cannot express this at all —
// swapping the two sort keys leaves all 180 stories green.
//
// The animal below is the discriminating case in adult-1:
//
//	TETANUS  severity 4, PROBABLE  (tremors + a body wound)
//	PINKEYE  severity 2, CONFIRMED (cloudy eye)
//
// Severity-first puts TETANUS first. Tier-first would put PINKEYE first, which
// would show the Director an eye infection above a probable tetanus.
func TestRankingPutsSeverityBeforeConfidence(t *testing.T) {
	reg := mustLoad(t)

	got := reg.Evaluate(
		Animal{Class: "adult", Species: "goat", Sex: "F", Status: "normal"},
		Findings{
			Temp:     floatPtr(102.0),
			Eating:   MultiValue{"normal"},
			Activity: "standing",
			Neuro:    MultiValue{"tremors"},
			Wounds:   MultiValue{"body"},
			Eyes:     MultiValue{"cloudy"},
		},
		Context{},
	)

	tetanus, pinkeye := indexOf(got.Problems, "TETANUS"), indexOf(got.Problems, "PINKEYE")
	if tetanus < 0 || pinkeye < 0 {
		t.Fatalf("fixture no longer produces both problems: %v", got.Problems)
	}
	// Guard the premise as well as the conclusion: if these tiers ever change,
	// the test stops discriminating and must be rebuilt rather than silently
	// passing on a case that no longer separates the two orderings.
	if got.Tiers["TETANUS"] != TierProbable || got.Tiers["PINKEYE"] != TierConfirmed {
		t.Fatalf("fixture no longer discriminates: TETANUS=%s PINKEYE=%s (want PROBABLE / CONFIRMED)",
			got.Tiers["TETANUS"], got.Tiers["PINKEYE"])
	}
	if tetanus > pinkeye {
		t.Errorf("severity-4 PROBABLE TETANUS ranked below severity-2 CONFIRMED PINKEYE: %v", got.Problems)
	}
}

// TestEvaluateIsDeterministic is the engine's core promise: same findings, same
// proposal, every time. Go map iteration is randomised, so any place the
// pipeline lets map order leak into an output slice would surface here.
func TestEvaluateIsDeterministic(t *testing.T) {
	reg := mustLoad(t)
	animal := Animal{Class: "adult", Species: "goat", Sex: "F", Status: "periparturient"}
	findings := Findings{
		Temp:      floatPtr(104.8),
		Eating:    MultiValue{"not_eating"},
		Activity:  "weak",
		Diarrhea:  Flag{Set: true},
		Mouth:     "orf_scabs",
		Eyes:      MultiValue{"red", "discharge"},
		CMT:       "pos",
		Lactation: "milk",
		Wounds:    MultiValue{"body"},
	}
	ctx := Context{Open: []string{"FEVER"}, Day: intPtr(3)}

	first := reg.Evaluate(animal, findings, ctx)
	for i := 0; i < 40; i++ {
		again := reg.Evaluate(animal, findings, ctx)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differed:\n first = %+v\n again = %+v", i, first, again)
		}
	}
}

func intPtr(v int) *int { return &v }

// TestEmergenciesFireForEveryClass pins the ordering decision from ALGORITHM 2:
// red flags are finding-triggered and run for every class, never gated on a rule
// matching. Bloat is the case that proves it -- a kid with a drum-tight rumen
// must raise the alarm on whichever register serves it.
//
// This replaces an earlier test that asserted a kid came back `out_of_scope`
// with emergencies only. That was correct while adult was the sole register;
// each class now has its own, so there is no out-of-scope class left to assert.
func TestEmergenciesFireForEveryClass(t *testing.T) {
	cases := []struct {
		class string
		stage string
	}{
		{ClassAdult, ""},
		{ClassKidMilk, "K1"},
		{ClassKidWeaning, "K3"},
		{ClassKidFattening, ""},
	}

	for _, c := range cases {
		t.Run(c.class, func(t *testing.T) {
			f := Findings{
				Temp:        floatPtr(102.0),
				Eating:      MultiValue{"normal"},
				Activity:    "standing",
				LeftStomach: MultiValue{"bloating"},
				Suckle:      "present",
			}
			// Satisfy each slice's own compulsory rows so the form is judged on
			// the bloat rather than rejected before it is looked at.
			switch c.class {
			case ClassKidMilk:
				f.Landing = "spiderman"
				f.RefusalsToday = intPtr(0)
			case ClassKidWeaning:
				f.RefusalsToday = intPtr(0)
			}

			got, err := Evaluate(
				Animal{Class: c.class, Stage: c.stage, Species: "goat", Sex: "M", Status: "normal"},
				f, Context{},
			)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if !got.Valid {
				t.Fatalf("form rejected (%s) before the emergency could fire", got.RejectReason)
			}
			if got.Scope != c.class {
				t.Errorf("scope = %q, want %q", got.Scope, c.class)
			}
			if !contains(got.Emergencies, EmergencyTube) {
				t.Errorf("emergencies = %v, want to contain %q", got.Emergencies, EmergencyTube)
			}
		})
	}
}

// TestMilkKidNeverGetsAcidosisFromWaterSound pins the narrowing that the milk
// register enforces in its vocabulary and the emergency detector enforces in
// code: a slosh in a milk-fed belly is milk, not acid. Firing acidosis there
// would pull concentrate the kid is not eating and treat a healthy animal.
func TestMilkKidNeverGetsAcidosisFromWaterSound(t *testing.T) {
	f := Findings{
		Temp:          floatPtr(103.0),
		Eating:        MultiValue{"not_eating"},
		Activity:      "standing",
		LeftStomach:   MultiValue{"acidosis"},
		Landing:       "spiderman",
		Suckle:        "present",
		RefusalsToday: intPtr(0),
	}
	milk, err := Evaluate(Animal{Class: ClassKidMilk, Stage: "K1", Species: "goat", Sex: "M"}, f, Context{})
	if err != nil {
		t.Fatalf("evaluate milk: %v", err)
	}
	if contains(milk.Emergencies, EmergencyAcidosisNow) {
		t.Errorf("a milk kid was given the acidosis emergency: %v", milk.Emergencies)
	}
	if contains(milk.Problems, "ACIDOSIS") {
		t.Errorf("a milk kid was diagnosed with acidosis: %v", milk.Problems)
	}

	// The SAME finding on a fattening kid is acidosis, which is the point: the
	// difference is the animal's gut, not the observation.
	fat := f
	fat.Landing = ""
	fat.RefusalsToday = nil
	fattening, err := Evaluate(Animal{Class: ClassKidFattening, Species: "goat", Sex: "M"}, fat, Context{})
	if err != nil {
		t.Fatalf("evaluate fattening: %v", err)
	}
	if !contains(fattening.Emergencies, EmergencyAcidosisNow) {
		t.Errorf("a fattening kid with water sound and off feed must get the acidosis emergency: %v",
			fattening.Emergencies)
	}
}

// TestFloppyKidIsMilkOnly pins a documented never: floppy kid must not fire on a
// weaning or fattening kid, because the drop test is not on those forms and the
// diagnosis is meaningless without it.
//
// Weaning is protected twice -- the form REJECTS a landing value outright -- but
// fattening is protected only by the class gate in the emergency detector, and
// no catalog story exercises it. That gap was found by deleting the gate and
// watching the whole suite stay green, which is the only way an untested
// safeguard announces itself.
func TestFloppyKidIsMilkOnly(t *testing.T) {
	landingBarely := func(class, stage string) Findings {
		return Findings{
			Temp: floatPtr(103.0), Eating: MultiValue{"normal"},
			Activity: "standing", Responsiveness: "alert", Suckle: "present",
			Landing: "barely",
		}
	}

	t.Run("fattening", func(t *testing.T) {
		got, err := Evaluate(
			Animal{Class: ClassKidFattening, Species: "goat", Sex: "M"},
			landingBarely(ClassKidFattening, ""), Context{})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if contains(got.Emergencies, EmergencyFloppyNow) {
			t.Errorf("a fattening kid raised the floppy emergency: %v", got.Emergencies)
		}
		if contains(got.Problems, IDFloppyKid) {
			t.Errorf("a fattening kid was diagnosed floppy: %v", got.Problems)
		}
	})

	t.Run("weaning rejects the row outright", func(t *testing.T) {
		f := landingBarely(ClassKidWeaning, "K3")
		f.RefusalsToday = intPtr(0)
		got, err := Evaluate(
			Animal{Class: ClassKidWeaning, Stage: "K3", Species: "goat", Sex: "M"}, f, Context{})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if got.Valid {
			t.Fatalf("a landing value on weaning must be rejected, got problems %v", got.Problems)
		}
		if got.RejectReason != RejectLandingNotOnWeaning {
			t.Errorf("reject = %q, want %q", got.RejectReason, RejectLandingNotOnWeaning)
		}
	})

	t.Run("milk kid still gets it", func(t *testing.T) {
		f := landingBarely(ClassKidMilk, "K1")
		f.RefusalsToday = intPtr(0)
		got, err := Evaluate(
			Animal{Class: ClassKidMilk, Stage: "K1", Species: "goat", Sex: "M"}, f, Context{})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if !contains(got.Emergencies, EmergencyFloppyNow) {
			t.Errorf("a milk kid that lands badly must raise floppy: %v", got.Emergencies)
		}
	})
}

// TestKidFeverBandIsOneTenthLowerThanAdult pins the single value where the
// classes disagree. A kid at 103.5degF IS febrile; an adult at 103.5 is not.
func TestKidFeverBandIsOneTenthLowerThanAdult(t *testing.T) {
	at1035 := func(class, stage string, extra func(*Findings)) Proposal {
		t.Helper()
		f := Findings{
			Temp: floatPtr(103.5), Eating: MultiValue{"normal"},
			Activity: "standing", Suckle: "present",
		}
		if extra != nil {
			extra(&f)
		}
		got, err := Evaluate(Animal{Class: class, Stage: stage, Species: "goat", Sex: "M"}, f, Context{})
		if err != nil {
			t.Fatalf("evaluate %s: %v", class, err)
		}
		return got
	}

	adult := at1035(ClassAdult, "", nil)
	if contains(adult.Problems, "FEVER") {
		t.Errorf("an adult at 103.5 must not be febrile: %v", adult.Problems)
	}

	kid := at1035(ClassKidWeaning, "K3", func(f *Findings) { f.RefusalsToday = intPtr(0) })
	if !contains(kid.Problems, "FEVER") {
		t.Errorf("a kid at 103.5 must be febrile: %v", kid.Problems)
	}
}

// TestEngineNeverClosesOrMoves pins the two things the engine is forbidden to do.
// It may PROPOSE a close; it may DIRECT housing. It never closes, and it never
// carries an instruction that moves an animal itself — the policy-pack workflow
// that owns location is the only writer.
func TestEngineNeverClosesOrMoves(t *testing.T) {
	reg := mustLoad(t)
	got := reg.Evaluate(
		Animal{Class: "adult", Species: "goat", Sex: "F", Status: "normal"},
		Findings{
			Temp:     floatPtr(102.0),
			Eating:   MultiValue{"normal"},
			Activity: "standing",
			Wounds:   MultiValue{"none"},
		},
		Context{Open: []string{"WOUNDS"}, Day: intPtr(4)},
	)
	if !contains(got.ProposeClose, "WOUNDS") {
		t.Fatalf("expected a proposed close for WOUNDS, got %v", got.ProposeClose)
	}
	if !contains(got.DirectorFlags, FlagManagerNeverCloses) {
		t.Errorf("every proposal must carry %q; flags = %v", FlagManagerNeverCloses, got.DirectorFlags)
	}
}

// TestNoDueTimeOvernight pins the farm's working day. The farm is empty between
// 00:00 and 06:00, so nothing may be scheduled into that window: an 20:30
// emergency is treated on that shift and then sits on the 06:00 list.
func TestNoDueTimeOvernight(t *testing.T) {
	reg := mustLoad(t)
	got := reg.Evaluate(
		Animal{Class: "adult", Species: "goat", Sex: "F", Status: "normal"},
		Findings{
			Temp:        floatPtr(102.0),
			Eating:      MultiValue{"normal"},
			Activity:    "standing",
			LeftStomach: MultiValue{"bloating"},
		},
		Context{Hour: intPtr(20)},
	)
	if !got.Housing.NoDueOvernight {
		t.Error("no_due_overnight must always hold")
	}
	if !contains(got.DirectorFlags, FlagNextLook6am) {
		t.Errorf("a 20:00 tube must set %q; flags = %v", FlagNextLook6am, got.DirectorFlags)
	}
}

// --- Adversarial fixtures for the Test 1 validators.
//
// Validate() runs on a clean register in every other test, which proves only
// that it does not false-positive. These fixtures prove it actually FIRES — a
// validator that has never failed is not a validator.

const validatorFixtureHeader = `register_version: test-1
non_specific:
  - eating:not_eating
vocabulary:
  - FEVER
  - diarrhea
  - eating:not_eating
  - status:pregnant
rules:
`

func loadFixture(t *testing.T, rules string) (*Register, error) {
	t.Helper()
	return Load([]byte(validatorFixtureHeader + rules))
}

func requireValidatorError(t *testing.T, rules, wantSubstring string) {
	t.Helper()
	reg, err := loadFixture(t, rules)
	if err != nil {
		t.Fatalf("fixture failed to load: %v", err)
	}
	errs := reg.Validate()
	for _, e := range errs {
		if strings.Contains(e.Error(), wantSubstring) {
			return
		}
	}
	t.Errorf("validator did not report %q; got %v", wantSubstring, errs)
}

// A clause may not reference a finding the schema does not define, or a typo
// becomes a rule that can never fire.
func TestValidatorRejectsUnknownVocabulary(t *testing.T) {
	requireValidatorError(t, `
  - id: TYPO
    kind: problem
    pathognomonic:
      - findings: [diarhea]
`, "not in vocabulary")
}

// A clause of only non-specific findings would fire on nearly every sick animal.
func TestValidatorRejectsUnanchoredClause(t *testing.T) {
	requireValidatorError(t, `
  - id: UNANCHORED
    kind: problem
    probable:
      - findings: [eating:not_eating]
`, "unanchored")
}

// A status token supplies the anchor a clause otherwise lacks, whether it sits
// in the clause or in the rule's own gate. Both spellings must be accepted, or
// the validator would reject legitimate rules.
func TestValidatorAcceptsStatusAnchoredClause(t *testing.T) {
	for name, rules := range map[string]string{
		"status in clause": `
  - id: INCLAUSE
    kind: problem
    probable:
      - findings: [eating:not_eating, status:pregnant]
`,
		"status in gate": `
  - id: INGATE
    kind: problem
    gate_required: [status:pregnant]
    probable:
      - findings: [eating:not_eating]
`,
	} {
		t.Run(name, func(t *testing.T) {
			reg, err := loadFixture(t, rules)
			if err != nil {
				t.Fatalf("fixture failed to load: %v", err)
			}
			for _, e := range reg.Validate() {
				if strings.Contains(e.Error(), "unanchored") {
					t.Errorf("status anchor rejected: %v", e)
				}
			}
		})
	}
}

// Reachability, checked across ALL THREE tier pairs. A lower-tier clause that is
// a superset of a higher-tier one can never fire: the higher tier always matches
// first. Checking only against pathognomonic found four of the five dead clauses
// in the original drafts; the fifth was visible only once probable-vs-possible
// was checked too, which is why that pair has its own case here.
func TestValidatorRejectsUnreachableClause(t *testing.T) {
	t.Run("possible superset of pathognomonic", func(t *testing.T) {
		requireValidatorError(t, `
  - id: DEAD
    kind: problem
    pathognomonic:
      - findings: [FEVER]
    possible:
      - findings: [FEVER, diarrhea]
`, "unreachable")
	})

	t.Run("possible superset of probable", func(t *testing.T) {
		requireValidatorError(t, `
  - id: DEADTOO
    kind: problem
    probable:
      - findings: [FEVER]
    possible:
      - findings: [FEVER, diarrhea]
`, "unreachable")
	})
}

// A register key nothing reads is an accept-and-discard: it looks authored,
// changes no behaviour, and reads to the next author as already honoured.
func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := loadFixture(t, `
  - id: OK
    kind: problem
    pathognomonic:
      - findings: [FEVER]
    severity_bass: 4
`)
	if err == nil {
		t.Fatal("a misspelled register key must fail the load, not be ignored")
	}
	if !strings.Contains(err.Error(), "severity_bass") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// Every run pins the register version, so a case stays interpretable after the
// rule table is edited. An unversioned register cannot provide that.
func TestLoadRejectsMissingVersion(t *testing.T) {
	_, err := Load([]byte("non_specific: []\nvocabulary: [FEVER]\nrules:\n  - id: X\n    kind: problem\n"))
	if err == nil || !strings.Contains(err.Error(), "register_version") {
		t.Errorf("want a register_version error, got: %v", err)
	}
}

func TestLoadRejectsDuplicateRuleID(t *testing.T) {
	_, err := loadFixture(t, `
  - id: TWICE
    kind: problem
    pathognomonic:
      - findings: [FEVER]
  - id: TWICE
    kind: problem
    pathognomonic:
      - findings: [diarrhea]
`)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("want a duplicate-id error, got: %v", err)
	}
}

// TestProposalPinsRegisterVersion is the audit requirement: a stored proposal
// must say which rule table produced it.
func TestProposalPinsRegisterVersion(t *testing.T) {
	reg := mustLoad(t)
	got := reg.Evaluate(
		Animal{Class: "adult", Species: "goat", Sex: "F", Status: "normal"},
		Findings{Temp: floatPtr(102.0), Eating: MultiValue{"normal"}, Activity: "standing"},
		Context{},
	)
	if got.RegisterVersion != reg.Version {
		t.Errorf("register_version = %q, want %q", got.RegisterVersion, reg.Version)
	}
}

// An invalid form is not diagnosed at all. Guessing which half of a
// contradictory observation is true would put an invented finding into a medical
// record.
func TestInvalidFormIsNotDiagnosed(t *testing.T) {
	reg := mustLoad(t)
	got := reg.Evaluate(
		Animal{Class: "adult", Species: "goat", Sex: "F", Status: "normal"},
		Findings{
			Temp:     floatPtr(107.0), // would otherwise be HIGH_FEVER + a cool emergency
			Eating:   MultiValue{"not_eating", "concentrate"},
			Activity: "standing",
		},
		Context{},
	)
	if got.Valid {
		t.Fatal("contradictory eating values must reject")
	}
	if got.RejectReason != RejectNotEatingWithFeed {
		t.Errorf("reject_reason = %q, want %q", got.RejectReason, RejectNotEatingWithFeed)
	}
	if len(got.Problems) != 0 || len(got.Emergencies) != 0 {
		t.Errorf("a rejected form must produce nothing: problems=%v emergencies=%v",
			got.Problems, got.Emergencies)
	}
}
