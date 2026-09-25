package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// configuredVocabRepo is the fake repository with the farm's Configuration lists behind it.
type configuredVocabRepo struct {
	fakeRepo
	vocab animalvocab.Vocabulary
}

func (r *configuredVocabRepo) AnimalVocabulary(context.Context, string) (animalvocab.Vocabulary, error) {
	return r.vocab, nil
}

// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): a species and gender the farm added on
// Configuration > Items & settings can be put on a procurement load, while one it never added --
// or archived, which the reader no longer lists -- is refused before the repository is reached.
func TestAddGoatToLoadAcceptsTheFarmsConfiguredSpeciesAndGenders(t *testing.T) {
	repo := &configuredVocabRepo{vocab: animalvocab.Vocabulary{
		Species: []animalvocab.Entry{{Code: "goat", Name: "Goat"}, {Code: "sheep", Name: "Sheep"}, {Code: "alpaca", Name: "Alpaca"}},
		Sexes:   []animalvocab.Entry{{Code: "female", Name: "Female"}, {Code: "male", Name: "Male"}, {Code: "castrated", Name: "Castrated male"}},
	}}
	svc := NewService(repo)
	add := func(species, sex, key string) error {
		_, err := svc.AddGoatToLoad(context.Background(), ports.AddGoatToLoad{
			TenantID:          testTenant,
			LoadID:            testLoad,
			AnimalIdentifier1: strPtr("SRC-" + key),
			Species:           species,
			Sex:               sex,
			IdempotencyKey:    "add-" + key,
		})
		return err
	}
	if err := add("alpaca", "castrated", "third"); err != nil {
		t.Fatalf("a configured third species/gender was refused: %v", err)
	}
	if repo.lastAdd.Species != "alpaca" || repo.lastAdd.Sex != "castrated" {
		t.Fatalf("stored %q/%q, want alpaca/castrated", repo.lastAdd.Species, repo.lastAdd.Sex)
	}
	for _, tc := range []struct{ species, sex, code string }{
		{"camel", "female", "invalid_species"},
		{"goat", "hermaphrodite", "invalid_sex"},
	} {
		repo.lastAdd = ports.AddGoatToLoad{}
		err := add(tc.species, tc.sex, tc.species+"-"+tc.sex)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != tc.code {
			t.Fatalf("%s/%s: err = %v, want %s", tc.species, tc.sex, err, tc.code)
		}
		if repo.lastAdd.LoadID != "" {
			t.Fatalf("%s/%s reached the repository", tc.species, tc.sex)
		}
	}
}
