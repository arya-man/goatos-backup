package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
)

var _ ports.AnimalVocabularySource = (*Repository)(nil)

// AnimalVocabulary returns the tenant's active species and genders, so a sale price can be set
// for a species added on Configuration > Items & settings. This reads Configuration's lookups
// only -- never an animal -- so the weighing isolation boundary is unchanged.
func (r *Repository) AnimalVocabulary(ctx context.Context, tenantID string) (animalvocab.Vocabulary, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	return animalvocab.Load(ctx, r.pool, tenantID)
}
