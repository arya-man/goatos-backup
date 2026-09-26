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
// A breed the farm ARCHIVED on Configuration is not offered even while older animals still carry
// it (audit 2026-09-26): archiving is how the farm says "no new animal gets this breed", and the
// herd side used to put it straight back. A carried breed the register has never heard of (legacy
// text) is still offered, as before. The grain is (breed, species), so a name the farm keeps under
// two species is two options and the phone's species filter shows the right one.
//
// The herd side is the same source as the Counts Breakdown `breeds` facet (`goats.breed` over
// alive, non-merged goats), so the value an operator picks is exactly the value that screen shows
// and the birth write stores. The register is matched by name, case-insensitively, within a
// species; a register breed nobody carries reads 0. Blank breed is excluded. The register is the
// farm's own breed list (breeds.tenant_id, 000442), the same list the web breed pickers read.
//
// projection-review: membership=tenant live non-merged goats with a nonblank breed UNION non-archived breed-register rows; group_key=(lower(breed name), species); join_cardinality=herd side is pre-aggregated to one row per (lower(breed), species) and the register side to one row per (lower(canonical_name), species) before the FULL JOIN, so it is 1:1; pagination=whole bounded vocabulary independent of page size; scope=tenant_id plus alive and non-merged predicates on the herd side
//   - producer unique key: herd (lower(g.breed), g.species) (GROUP BY); register (lower(canonical_name), species)
//     (GROUP BY; breeds is unique on (tenant, species, canonical_name)).
//     consumer match key: (lower(name), species) on both sides.
//   - multiplicity: each side is one row per key before the join; count(*) ranges over goat rows only.
//   - numerator/denominator: n/a (no ratio/cap).
const appActiveBreedsQuery = `
WITH herd AS (
  SELECT lower(g.breed) AS k, g.species AS sp, min(g.breed) AS breed, count(*) AS head_count
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND btrim(COALESCE(g.breed, '')) <> ''
  GROUP BY lower(g.breed), g.species
), register AS (
  SELECT lower(b.canonical_name) AS k, b.species AS sp, min(b.canonical_name) AS canonical_name,
         bool_or(b.status = 'active') AS offered, bool_and(b.status = 'inactive') AS archived
  FROM breeds b
  WHERE b.tenant_id = $1::uuid AND btrim(b.canonical_name) <> ''
  GROUP BY lower(b.canonical_name), b.species
)
SELECT COALESCE(h.breed, r.canonical_name) AS breed,
       COALESCE(r.sp, h.sp, '') AS species,
       COALESCE(h.head_count, 0) AS head_count
FROM herd h
FULL JOIN register r ON r.k = h.k AND r.sp = h.sp
WHERE NOT COALESCE(r.archived, false)
  AND (h.k IS NOT NULL OR r.offered)
ORDER BY head_count DESC, breed`

// ActiveBreeds lists the breeds an operator may give a newborn (see appActiveBreedsQuery). Key and
// label are both the breed name `goats.breed` stores, matching the Counts Breakdown breed facet so
// the picked value round-trips through the birth write unchanged.
// scale-guard:plan-proof-exempt: the goats side keeps the same predicate (tenant_id, merged_into_goat_id IS NULL, lifecycle_status = 'alive', non-blank breed) and access path; only its group key becomes lower(breed), and the FULL JOIN is to the farm's breeds catalogue (tens of rows).
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
