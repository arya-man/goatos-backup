package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

var _ ports.AnimalVocabularySource = (*Repository)(nil)

// AnimalVocabulary returns the tenant's active species and genders (Configuration > Items &
// settings), so a species added there is offered on the inspection form and accepted at once.
func (r *Repository) AnimalVocabulary(ctx context.Context, tenantID string) (animalvocab.Vocabulary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return animalvocab.Load(ctx, r.pool, tenantID)
}
