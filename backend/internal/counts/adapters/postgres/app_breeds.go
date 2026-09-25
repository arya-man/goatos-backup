package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// appActiveBreedsQuery returns every breed an operator may give a newborn: each active breed in the
// Configuration breed register (Configuration > Items & settings > Breeds) PLUS any breed the live
// herd already carries, with its species and live head count, carried breeds first. A breed added in
// Configuration used to be missing here until some animal carried it -- which no newborn ever could,
// because this list is the only way the phone names a breed.
//
// The herd side is the same source and grain as the Counts Breakdown `breeds` facet (`goats.breed`
// over alive, non-merged goats), so the value an operator picks is exactly the value that screen
// shows and the birth write stores. The register is matched by name, case-insensitively; a register
// breed nobody carries reads 0. Blank breed is excluded. The register is the farm's own breed list
// (breeds.tenant_id, 000432), the same list the web breed pickers read (adminui allBreedsSQL).
//
// projection-review: membership=tenant live non-merged goats with a nonblank breed UNION active breed-register rows; group_key=lower(breed name); join_cardinality=herd side is pre-aggregated to one row per lower(breed) and the register side to one row per lower(canonical_name) before the FULL JOIN, so it is 1:1; pagination=whole bounded vocabulary independent of page size; scope=tenant_id plus alive and non-merged predicates on the herd side
//   - producer unique key: herd `lower(g.breed)` (GROUP BY), register `lower(canonical_name)` (DISTINCT ON).
//     consumer match key: lower(name) on both sides.
//   - multiplicity: each side is one row per key before the join; count(*) ranges over goat rows only.
//   - numerator/denominator: n/a (no ratio/cap).
const appActiveBreedsQuery = `
WITH herd AS (
  SELECT lower(g.breed) AS k, min(g.breed) AS breed, max(g.species) AS species, count(*) AS head_count
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND btrim(COALESCE(g.breed, '')) <> ''
  GROUP BY lower(g.breed)
), register AS (
  SELECT DISTINCT ON (lower(b.canonical_name)) lower(b.canonical_name) AS k, b.canonical_name, b.species
  FROM breeds b
  WHERE b.tenant_id = $1::uuid AND b.status = 'active' AND btrim(b.canonical_name) <> ''
  ORDER BY lower(b.canonical_name), b.species
)
SELECT COALESCE(h.breed, r.canonical_name) AS breed,
       COALESCE(r.species, h.species, '') AS species,
       COALESCE(h.head_count, 0) AS head_count
FROM herd h
FULL JOIN register r ON r.k = h.k
ORDER BY head_count DESC, breed`

// ActiveBreeds lists the breeds an operator may give a newborn (see appActiveBreedsQuery). Key and
// label are both the breed name `goats.breed` stores, matching the Counts Breakdown breed facet so
// the picked value round-trips through the birth write unchanged.
func (r *Repository) ActiveBreeds(ctx context.Context, tenantID string) ([]domain.BirthBreedOption, error) {
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

	breeds := []domain.BirthBreedOption{}
	for rows.Next() {
		var breed, species string
		var count int64
		if err := rows.Scan(&breed, &species, &count); err != nil {
			return nil, fmt.Errorf("counts: active breeds scan: %w", err)
		}
		breeds = append(breeds, domain.BirthBreedOption{
			Key:     breed,
			Label:   breed,
			Species: species,
			Count:   count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: active breeds rows: %w", err)
	}
	return breeds, nil
}
