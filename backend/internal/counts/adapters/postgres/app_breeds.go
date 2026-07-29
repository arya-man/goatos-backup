package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// appActiveBreedsQuery returns the distinct breeds PRESENT on the tenant's live herd, each with a
// head count, ordered most-common first. It is the same source and grain as the Counts Breakdown
// `breeds` facet (`COALESCE(g.breed,”)` over alive, non-merged goats) so the value an operator
// picks here is exactly the value that screen shows and that the birth write stores -- but served
// on the operator (CountsWrite) surface, which the read-only Counts Breakdown (`CountsRead`) is not.
//
// Blank breed is excluded: a birth's breed picker offers real breeds to choose from, never an
// empty option. The tenant+lifecycle predicate is index-friendly and the GROUP BY collapses to at
// most a handful of breed rows, so this is bounded well within the current scale envelope.
//
// projection-review: membership=tenant live non-merged goats with a nonblank breed; group_key=normalized breed; join_cardinality=single-table aggregate counts each goat row once; pagination=whole bounded breed facet independent of page size; scope=tenant_id plus alive and non-merged predicates
//   - membership source: canonical `goats` (tenant-scoped, alive, non-merged) -- the same source and
//     predicate as the Counts Breakdown `breeds` facet (countsBreakdownFacetsSQL), so both surfaces
//     resolve this count to one authoritative source/grain (cross-surface count parity).
//   - producer unique key: `goats.goat_id` (one row per animal). consumer group key: `g.breed`.
//   - multiplicity: goats -> breed is many-to-one; count(*) over a single table cannot fan out.
//   - numerator/denominator: n/a (no ratio/cap); count(*) ranges over the grouped goat rows only.
const appActiveBreedsQuery = `
SELECT g.breed AS breed, count(*) AS head_count
FROM goats g
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND g.lifecycle_status = 'alive'
  AND btrim(COALESCE(g.breed, '')) <> ''
GROUP BY g.breed
ORDER BY head_count DESC, g.breed`

// ActiveBreeds lists the breeds present on the tenant's live herd for the operator birth form's
// breed picker. Key and label are both the canonical `goats.breed` value, matching the Counts
// Breakdown breed facet so the picked value round-trips through the birth write unchanged.
func (r *Repository) ActiveBreeds(ctx context.Context, tenantID string) ([]domain.CountsBreakdownSeriesPoint, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("counts: active breeds: missing tenant id")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, appActiveBreedsQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("counts: active breeds: %w", err)
	}
	defer rows.Close()

	breeds := []domain.CountsBreakdownSeriesPoint{}
	for rows.Next() {
		var breed string
		var count int64
		if err := rows.Scan(&breed, &count); err != nil {
			return nil, fmt.Errorf("counts: active breeds scan: %w", err)
		}
		breeds = append(breeds, domain.CountsBreakdownSeriesPoint{
			Key:   breed,
			Label: breed,
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: active breeds rows: %w", err)
	}
	return breeds, nil
}
