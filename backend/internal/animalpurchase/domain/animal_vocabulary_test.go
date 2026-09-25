package domain

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): the inspection's species and gender
// choices are the tenant's Configuration lists, filled in at read time over whatever the published
// document holds, so a species added there is offered and accepted on every version at once --
// and a species the farm never added is refused.
func TestInspectionSpeciesAndSexAreTheFarmsConfiguredLists(t *testing.T) {
	species := AnimalOptions([]animalvocab.Entry{{Code: "goat", Name: "Goat"}, {Code: "sheep", Name: "Sheep"}, {Code: "alpaca", Name: "Alpaca"}})
	sexes := AnimalOptions([]animalvocab.Entry{{Code: "female", Name: "Female"}, {Code: "male", Name: "Male"}, {Code: "castrated", Name: "Castrated male"}})
	cat := SeededCatalog().WithAnimalVocabulary(species, sexes)

	a := fullAnswers()
	a["species"] = j("alpaca")
	a["sex"] = j("castrated")
	// Female-only questions do not apply to a castrated male; drop them as the phone would.
	for _, id := range []string{"pregnant", "lactating", "mastitis", "teats", "teat_discharge"} {
		delete(a, id)
	}
	if err := cat.ValidateAnswers(a, fullMedia()); err != nil {
		t.Fatalf("a configured third species/gender was refused: %v", err)
	}

	a["species"] = j("camel")
	if got := field(cat.ValidateAnswers(a, fullMedia())); got != "species" {
		t.Fatalf("an unconfigured species: field = %q, want species", got)
	}

	// A document whose species options name a third species still publishes: the options are
	// placeholders now, never a fixed goat/sheep pair.
	dsl, err := ParseInspection(map[string]any{"inspection": json.RawMessage(SeededInspectionJSON())})
	if err != nil {
		t.Fatal(err)
	}
	for pi := range dsl.Pages {
		for qi := range dsl.Pages[pi].Questions {
			if dsl.Pages[pi].Questions[qi].ID == "species" {
				dsl.Pages[pi].Questions[qi].Options = append(dsl.Pages[pi].Questions[qi].Options, Option{Value: "alpaca", Label: "Alpaca"})
			}
		}
	}
	if problems := ValidateInspection(dsl); len(problems) != 0 {
		t.Fatalf("a document naming a third species no longer validates: %v", problems)
	}
	// An empty list (the vocabulary could not be read) keeps the document's own choices.
	if q, _ := SeededCatalog().WithAnimalVocabulary(nil, nil).ByID("species"); len(q.Options) == 0 {
		t.Fatal("an unavailable vocabulary must not empty the species question")
	}
}
