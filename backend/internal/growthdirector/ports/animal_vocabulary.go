package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

// AnimalVocabularySource answers which species and genders the tenant keeps (Configuration >
// Items & settings, read through platform/animalvocab). The postgres Repository implements it; a
// repository that does not (a unit-test fake) is read as the four built-ins, which is exactly the
// behaviour before OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25).
type AnimalVocabularySource interface {
	AnimalVocabulary(ctx context.Context, tenantID string) (animalvocab.Vocabulary, error)
}
