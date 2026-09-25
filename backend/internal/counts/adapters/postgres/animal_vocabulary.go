package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

var _ ports.AnimalVocabularySource = (*Repository)(nil)

// AnimalVocabulary returns the tenant's active species and genders, so the phone's birth form
// offers a species added on Configuration > Items & settings at once.
func (r *Repository) AnimalVocabulary(ctx context.Context, tenantID string) (animalvocab.Vocabulary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return animalvocab.Load(ctx, r.pool, tenantID)
}
