package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// The catalog is built from the REAL embedded registers, so this is the test that would
// notice a register losing its labels or a class dropping out of the binding.
func TestDeathCauseCatalogIsBuiltFromEveryShippedRegister(t *testing.T) {
	catalog, err := NewDeathCauseCatalogService().Catalog(context.Background())
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	// Four registers ship: adult plus the three kid classes.
	if len(catalog.RegisterVersions) != 4 {
		t.Errorf("register versions = %v, want all four shipped registers", catalog.RegisterVersions)
	}
	// The adult register alone carries 34 rules and the kid classes overlap heavily, so a
	// folded list well above 30 proves both that every register was read and that the fold
	// did not collapse distinct diseases into one another.
	if len(catalog.Options) < 30 {
		t.Fatalf("options = %d, want the whole diagnosis vocabulary", len(catalog.Options))
	}

	byKey := make(map[string]domain.DeathCauseOption, len(catalog.Options))
	for _, option := range catalog.Options {
		byKey[option.Key] = option
		// EVERY option must be readable by a person. An UNDERSCORED, SHOUTED key like
		// FOOT_ROT on a screen is the copy-firewall break this product bans; a short
		// all-caps disease abbreviation the farm genuinely says out loud (PPR) is not,
		// which is why the check is on the shape rather than on label != key.
		if option.Label == "" {
			t.Errorf("option %q has no label", option.Key)
		}
		if strings.Contains(option.Label, "_") {
			t.Errorf("option %q label %q is a machine key, not the farm's word", option.Key, option.Label)
		}
		if option.Kind != domain.DeathCauseKindRegisterRule {
			t.Errorf("option %q kind = %q, want the register-rule vocabulary", option.Key, option.Kind)
		}
		if len(option.AnimalClasses) == 0 {
			t.Errorf("option %q names no animal class", option.Key)
		}
	}

	// Spot-check the diseases the farm actually loses animals to, and the label the
	// operator reads for each.
	for key, label := range map[string]string{
		"MASTITIS":  "Mastitis",
		"DIARRHEA":  "Diarrhea",
		"BLOAT":     "Bloat",
		"FOOT_ROT":  "Foot rot",
		"PPR":       "PPR",
		"FLYSTRIKE": "Flystrike",
	} {
		option, ok := byKey[key]
		if !ok {
			t.Errorf("%s is not offerable as a cause of death", key)
			continue
		}
		if option.Label != label {
			t.Errorf("%s label = %q, want %q", key, option.Label, label)
		}
	}

	// A rule every register carries is ONE row with its classes collected, not four rows.
	if diarrhea := byKey["DIARRHEA"]; len(diarrhea.AnimalClasses) < 2 {
		t.Errorf("DIARRHEA classes = %v, want every register that carries it", diarrhea.AnimalClasses)
	}

	// FIELD ACTIONS ARE NOT DISEASES. The register carries `kind: field` rules beside its
	// problems -- TICKS, HOOF, ANTIHISTAMINE, SEPARATE_FEEDING -- which are things the
	// engine advises DOING, not conditions an animal can die of. Offering "Separate
	// feeding" as a cause of death is nonsense on the operator's screen and worse in the
	// mortality board, so only `problem` rules reach the dropdown.
	for _, fieldAction := range []string{"TICKS", "HOOF", "ANTIHISTAMINE", "SEPARATE_FEEDING"} {
		if _, ok := byKey[fieldAction]; ok {
			t.Errorf("%s is a field action and must not be offerable as a cause of death", fieldAction)
		}
	}
}

// THE LABEL CANNOT COME FROM `sop_ref`, and this is the test that says why.
//
// sop_ref names the TREATMENT SOP, not the disease: PPR, POX and UNDIFFERENTIATED all
// carry `sop_ref: Supportive`. A dropdown built from it would show the operator three
// different diseases under one identical label, and the mortality board would have no way
// to tell them apart. Every disease must therefore read distinctly.
func TestEveryDeathCauseReadsDistinctlyOnScreen(t *testing.T) {
	catalog, err := NewDeathCauseCatalogService().Catalog(context.Background())
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	seen := make(map[string]string, len(catalog.Options))
	for _, option := range catalog.Options {
		if first, clash := seen[option.Label]; clash {
			t.Errorf("%q labels both %s and %s; an operator cannot tell them apart", option.Label, first, option.Key)
			continue
		}
		seen[option.Label] = option.Key
	}
	// PPR and POX are the worked case: same SOP, different diseases, and the farm loses
	// animals to both. They must never collapse into one row.
	labels := map[string]string{}
	for _, option := range catalog.Options {
		labels[option.Key] = option.Label
	}
	if labels["PPR"] == labels["POX"] {
		t.Fatalf("PPR and POX both read %q -- the SOP name has leaked back in as the label", labels["PPR"])
	}
	if labels["PPR"] == "Supportive" || labels["POX"] == "Supportive" {
		t.Errorf("a disease is labelled with its treatment SOP: PPR=%q POX=%q", labels["PPR"], labels["POX"])
	}
}

// The validator is the gate between an operator's dropdown and the stored fact.
func TestValidateCauseAcceptsTheRegisterAndRefusesAnythingElse(t *testing.T) {
	svc := NewDeathCauseCatalogService()
	ctx := context.Background()

	if err := svc.ValidateCause(ctx, "MASTITIS", domain.DeathCauseKindRegisterRule); err != nil {
		t.Errorf("a real register rule was refused: %v", err)
	}
	// A normal death names no disease, and that is a complete answer.
	if err := svc.ValidateCause(ctx, "", ""); err != nil {
		t.Errorf("a normal death was refused: %v", err)
	}
	for _, cause := range []domain.DeathCause{
		{Key: "Mastitus", Kind: domain.DeathCauseKindRegisterRule},
		{Key: "mastitis", Kind: domain.DeathCauseKindRegisterRule},
		{Key: "SNAKE_BITE", Kind: domain.DeathCauseKindRegisterRule},
		{Key: "supportive", Kind: domain.DeathCauseKindDiseaseKey},
		{Key: "MASTITIS"},
	} {
		if err := svc.ValidateCause(ctx, cause.Key, cause.Kind); err == nil {
			t.Errorf("%+v was accepted as a cause of death", cause)
		}
	}
}
