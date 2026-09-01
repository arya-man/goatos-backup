package diagnosis

import (
	"strings"
	"testing"
)

// The kid acceptance catalogs, scored by exactly the same harness as the adult
// one. A miss is a rule bug or an algorithm bug, not a skip.
//
// The counts are asserted per class so a truncated or half-loaded catalog cannot
// report green by running fewer stories than it ships.
var kidCatalogs = []struct {
	class   string
	path    string
	version string
	count   int
}{
	{ClassKidMilk, "testdata/catalog-kid-milk.json", "kid-milk-7", 46},
	{ClassKidWeaning, "testdata/catalog-kid-weaning.json", "kid-weaning-1", 21},
	{ClassKidFattening, "testdata/catalog-kid-fattening.json", "kid-fattening-1", 13},
}

func TestKidAcceptanceCatalogs(t *testing.T) {
	for _, kc := range kidCatalogs {
		t.Run(kc.class, func(t *testing.T) {
			reg, err := RegisterFor(kc.class)
			if err != nil {
				t.Fatalf("register for %s: %v", kc.class, err)
			}
			if reg.Version != kc.version {
				t.Fatalf("register version = %q, want %q", reg.Version, kc.version)
			}

			c := loadCatalogAt(t, kc.path)
			if len(c.Stories) != kc.count || c.Count != kc.count {
				t.Fatalf("catalog has %d stories (count field %d), want %d",
					len(c.Stories), c.Count, kc.count)
			}
			if c.Version != kc.version {
				t.Fatalf("catalog_version = %q, want %q", c.Version, kc.version)
			}

			seen := map[string]bool{}
			for _, s := range c.Stories {
				if seen[s.ID] {
					t.Fatalf("duplicate story id %s", s.ID)
				}
				seen[s.ID] = true
			}

			for _, s := range c.Stories {
				t.Run(s.ID, func(t *testing.T) {
					// Every story in a class catalog must be OF that class.
					// Otherwise the catalog could silently prove the wrong
					// register.
					if s.Animal.class() != kc.class {
						t.Fatalf("story animal class %q, catalog is %q",
							s.Animal.class(), kc.class)
					}
					got := reg.Evaluate(s.Animal, s.Find, s.Ctx)
					for _, msg := range checkStory(reg, s, got) {
						t.Errorf("%s (%s): %s", s.ID, s.Title, msg)
					}
				})
			}
		})
	}
}

// TestRegisterRefusesAnimalOfAnotherClass proves the last line of defence for
// the spec's loudest never: a register asked to diagnose a class it does not
// serve refuses, rather than producing a confident wrong answer off the wrong
// rule table.
func TestRegisterRefusesAnimalOfAnotherClass(t *testing.T) {
	adult, err := RegisterFor(ClassAdult)
	if err != nil {
		t.Fatalf("adult register: %v", err)
	}
	milkKid := Animal{Class: ClassKidMilk, Stage: "K1", Species: "goat", Sex: "M", Status: "normal"}

	got := adult.Evaluate(milkKid, Findings{}, Context{})
	if got.Valid {
		t.Fatalf("adult register diagnosed a milk kid: %+v", got.Problems)
	}
	if got.RejectReason != RejectRegisterClassMismatch {
		t.Errorf("reject = %q, want %q", got.RejectReason, RejectRegisterClassMismatch)
	}
}

// TestEvaluatePicksTheRegisterForTheClass proves the production entry point
// resolves the register itself, so no caller has to.
func TestEvaluatePicksTheRegisterForTheClass(t *testing.T) {
	for _, cr := range classRegisters {
		animal := Animal{Class: cr.class, Species: "goat", Sex: "M", Status: "normal"}
		if cr.class == ClassKidMilk {
			animal.Stage = "K1"
		}
		if cr.class == ClassKidWeaning {
			animal.Stage = "K3"
		}
		got, err := Evaluate(animal, Findings{}, Context{})
		if err != nil {
			t.Errorf("class %s: %v", cr.class, err)
			continue
		}
		if got.RegisterVersion != cr.version {
			t.Errorf("class %s used register %q, want %q", cr.class, got.RegisterVersion, cr.version)
		}
		if got.Scope != cr.class {
			t.Errorf("class %s scope = %q, want %q", cr.class, got.Scope, cr.class)
		}
	}
}

// TestEvaluateRefusesAnUnknownClass proves an unrecognised class is an error and
// never a quiet fallback to the adult table.
func TestEvaluateRefusesAnUnknownClass(t *testing.T) {
	_, err := Evaluate(Animal{Class: "kid_unknown", Species: "goat", Sex: "M"}, Findings{}, Context{})
	if err == nil {
		t.Fatal("an unknown class was diagnosed instead of refused")
	}
	if !strings.Contains(err.Error(), "kid_unknown") {
		t.Errorf("error does not name the offending class: %v", err)
	}
}
