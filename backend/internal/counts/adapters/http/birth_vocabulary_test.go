package http

import (
	"context"
	"net/http"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

// vocabularyShiftingRepo is the fake repository with the farm's Configuration lists behind it.
type vocabularyShiftingRepo struct {
	*fakeShiftingRepo
	vocab animalvocab.Vocabulary
}

func (r vocabularyShiftingRepo) AnimalVocabulary(context.Context, string) (animalvocab.Vocabulary, error) {
	return r.vocab, nil
}

// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): the phone's birth form lists the farm's
// Configuration species and genders, in their order and under their names, so a species added
// there is offered at once; a service with no Configuration behind it serves the four built-ins.
func TestBirthFormServesTheFarmsConfiguredSpeciesAndGenders(t *testing.T) {
	repo := vocabularyShiftingRepo{fakeShiftingRepo: newFakeShiftingRepo(), vocab: animalvocab.Vocabulary{
		Species: []animalvocab.Entry{{Code: "goat", Name: "Goat"}, {Code: "sheep", Name: "Sheep"}, {Code: "alpaca", Name: "Alpaca"}},
		Sexes:   []animalvocab.Entry{{Code: "female", Name: "Female"}, {Code: "male", Name: "Male"}, {Code: "castrated", Name: "Castrated male"}},
	}}
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())
	rec := get(t, mux, appBirthBreedsRoute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got appBirthBreedsResponse
	decodeBody(t, rec, &got)
	if len(got.Species) != 3 || got.Species[2] != (appBirthVocabularyOption{Key: "alpaca", Label: "Alpaca"}) {
		t.Fatalf("species = %+v, want goat, sheep, alpaca", got.Species)
	}
	if len(got.Sexes) != 3 || got.Sexes[2] != (appBirthVocabularyOption{Key: "castrated", Label: "Castrated male"}) {
		t.Fatalf("sexes = %+v, want female, male, castrated", got.Sexes)
	}

	plain := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), newFakeApprovalWorkflow(), newFakeGoatValidator())
	decodeBody(t, get(t, plain, appBirthBreedsRoute), &got)
	if len(got.Species) != 2 || got.Species[0].Key != "goat" || len(got.Sexes) != 2 || got.Sexes[0].Key != "female" {
		t.Fatalf("built-ins = %+v / %+v", got.Species, got.Sexes)
	}
}
