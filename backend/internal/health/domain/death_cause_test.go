package domain

import "testing"

func testCatalog() DeathCauseCatalog {
	return BuildDeathCauseCatalog([]RegisterRule{
		{ID: "DIARRHEA", Label: "Diarrhea", Class: "adult"},
		{ID: "DIARRHEA", Label: "Diarrhea", Class: "kid_milk"},
		{ID: "MASTITIS", Label: "Mastitis", Class: "adult"},
		{ID: "FOOT_ROT", Label: "Foot rot", Class: "kid_fattening"},
		{ID: "BLOAT", Label: "Bloat", Class: "adult"},
	}, []string{"adult-1", "kid-milk-7"})
}

// A rule carried by several registers is ONE row in the dropdown, with its classes
// collected. Four identical "Diarrhea" rows would make the operator choose between
// answers that are the same answer.
func TestDeathCauseCatalogFoldsARuleCarriedBySeveralRegisters(t *testing.T) {
	catalog := testCatalog()
	seen := 0
	for _, option := range catalog.Options {
		if option.Key != "DIARRHEA" {
			continue
		}
		seen++
		if len(option.AnimalClasses) != 2 {
			t.Errorf("DIARRHEA classes = %v, want both registers that carry it", option.AnimalClasses)
		}
	}
	if seen != 1 {
		t.Fatalf("DIARRHEA appears %d times in the dropdown, want once", seen)
	}
}

// The dropdown reads alphabetically by the farm's own word, not by the machine key --
// which would order Bloat, Diarrhea, Foot rot as BLOAT, DIARRHEA, FOOT_ROT and put
// "Udder edema" under U while "Foot rot" sits under F.
func TestDeathCauseCatalogSortsByTheLabelTheOperatorReads(t *testing.T) {
	catalog := testCatalog()
	want := []string{"Bloat", "Diarrhea", "Foot rot", "Mastitis"}
	if len(catalog.Options) != len(want) {
		t.Fatalf("options = %d, want %d: %+v", len(catalog.Options), len(want), catalog.Options)
	}
	for i, label := range want {
		if catalog.Options[i].Label != label {
			t.Errorf("option %d = %q, want %q", i, catalog.Options[i].Label, label)
		}
	}
}

// A rule with no label is SKIPPED, never shown under its raw id. 'FOOT_ROT' in front of an
// operator is the copy-firewall break this product bans, and every shipped register
// carries a label -- so a missing one means the register lost it, and the omission is the
// signal rather than a machine key leaking onto a screen.
func TestDeathCauseCatalogSkipsARuleWithNoFarmReadableLabel(t *testing.T) {
	catalog := BuildDeathCauseCatalog([]RegisterRule{
		{ID: "MASTITIS", Label: "Mastitis", Class: "adult"},
		{ID: "NO_LABEL", Label: "  ", Class: "adult"},
	}, nil)
	for _, option := range catalog.Options {
		if option.Key == "NO_LABEL" {
			t.Fatal("a rule with no label reached the dropdown")
		}
	}
	if len(catalog.Options) != 1 {
		t.Fatalf("options = %+v, want only the labelled rule", catalog.Options)
	}
}

// An EMPTY cause is a complete answer: the death was normal, no disease was established.
func TestValidateDeathCauseAcceptsANormalDeath(t *testing.T) {
	if err := ValidateDeathCause(DeathCause{}, testCatalog()); err != nil {
		t.Fatalf("a normal death was rejected: %v", err)
	}
}

func TestValidateDeathCauseAcceptsARuleFromTheRegister(t *testing.T) {
	cause := DeathCause{Key: "MASTITIS", Kind: DeathCauseKindRegisterRule}
	if err := ValidateDeathCause(cause, testCatalog()); err != nil {
		t.Fatalf("a real register rule was rejected: %v", err)
	}
}

// THE POINT OF A CODED CAUSE IS THAT IT GROUPS. One death filed under 'MASTITIS' and
// another under a near-miss is two diseases on the board and one in the barn, so an
// unknown key is refused rather than stored as typed. An operator who cannot find the
// disease records a normal death and says so in the note, which is honest.
func TestValidateDeathCauseRefusesADiseaseTheRegisterDoesNotName(t *testing.T) {
	for _, key := range []string{"Mastitus", "mastitis", "SNAKE_BITE", " "} {
		cause := DeathCause{Key: key, Kind: DeathCauseKindRegisterRule}
		if err := ValidateDeathCause(cause, testCatalog()); err == nil {
			t.Errorf("%q was accepted as a cause of death", key)
		}
	}
}

// The pair is both-or-neither, matching the database constraint: a kind alone names
// nothing, and a key alone cannot be read because the same string can live in both
// vocabularies.
func TestValidateDeathCauseRefusesHalfAPair(t *testing.T) {
	if err := ValidateDeathCause(DeathCause{Key: "MASTITIS"}, testCatalog()); err == nil {
		t.Error("a key with no kind was accepted")
	}
	if err := ValidateDeathCause(DeathCause{Kind: DeathCauseKindRegisterRule}, testCatalog()); err == nil {
		t.Error("a kind with no key was accepted")
	}
}

// A treatment-card cause is resolved by the SERVER from the animal's own case. Accepting
// one from a client would let a caller file a death under a card the animal was never
// treated on -- the same fabrication the register check prevents, one vocabulary over.
func TestValidateDeathCauseRefusesAClientSuppliedTreatmentCard(t *testing.T) {
	cause := DeathCause{Key: "supportive", Kind: DeathCauseKindDiseaseKey}
	if err := ValidateDeathCause(cause, testCatalog()); err == nil {
		t.Fatal("a client-supplied treatment-card cause was accepted")
	}
}

// The register rule WINS where a case has one; the treatment card is the fallback for a
// pre-engine case, and the kind says which -- because the card is many-to-one across
// diseases and a reader must be able to tell the precise answer from the approximate one.
func TestDeathCauseFromCasePrefersTheRegisterRule(t *testing.T) {
	if got := DeathCauseFromCase("MASTITIS", "mastitis"); got.Key != "MASTITIS" || got.Kind != DeathCauseKindRegisterRule {
		t.Errorf("cause = %+v, want the register rule", got)
	}
	if got := DeathCauseFromCase("", "supportive"); got.Key != "supportive" || got.Kind != DeathCauseKindDiseaseKey {
		t.Errorf("cause = %+v, want the treatment card fallback", got)
	}
	if got := DeathCauseFromCase("  ", " "); !got.IsZero() {
		t.Errorf("cause = %+v, want none -- this case can supply no coded cause", got)
	}
}
