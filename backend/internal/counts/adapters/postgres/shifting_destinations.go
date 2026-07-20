package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// Read-only support for the operator-facing single-animal shifting flow.
//
// Both queries in this file READ locations/goats rows that other modules own. That is deliberate
// and stays within the module boundary rule, which forbids counts from WRITING another module's
// tables -- counts already reads locations for park/shed labels in the herd-register rollups
// (repository.go) and reads goats for the census aggregates. Nothing here mutates.

// shiftingDestinationCatalogQuery returns every active park with its active sheds in one round trip.
//
// Shape notes:
//   - LEFT JOIN, not INNER: a park with no sheds yet must still appear in the dropdown, otherwise a
//     newly-created park is invisible and an operator cannot report a movement into it.
//   - The status/retired_at filters live in the JOIN's ON clause for the shed side. Putting them in
//     WHERE would silently convert the LEFT JOIN back into an inner join and drop exactly those
//     empty parks.
//   - ORDER BY name then id: name is the human sort, and the id tiebreak keeps the order stable
//     across calls when two sheds in the same park share a name (which happens).
//
// Index: locations_tenant_type_status_order_idx covers the park side
// (tenant_id, location_type, status, ...) and locations_tenant_parent_status_idx covers the shed
// side (tenant_id, parent_location_id, status, ...). No new index is required.
//
// mobile-guard:ignore: bounded location catalog cached on-device, not a paginated feed
// scale-guard:ignore: bounded location catalog cached on-device, not a paginated feed
const shiftingDestinationCatalogQuery = `
SELECT
    park.location_id::text,
    park.name,
    shed.location_id::text,
    shed.name
FROM locations park
LEFT JOIN locations shed
       ON shed.tenant_id = park.tenant_id
      AND shed.parent_location_id = park.location_id
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
WHERE park.tenant_id = $1::uuid
  AND park.location_type = 'park'
  AND park.status = 'active'
  AND park.retired_at IS NULL
ORDER BY park.name, park.location_id, shed.name, shed.location_id`

// ShiftingDestinationCatalog implements ports.Repository.ShiftingDestinationCatalog.
//
// Deliberately unpaginated. This is a bounded configuration catalog -- the current tenant has 2
// parks and ~154 sheds, and the number is governed by how many sheds the business physically builds,
// not by herd size or event volume. The client fetches it once and caches it; it is never a scrolling
// list. That is why the guard annotations above name a bounded catalog rather than disabling a guard.
func (r *Repository) ShiftingDestinationCatalog(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog: missing tenant id")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, shiftingDestinationCatalogQuery, tenantID)
	if err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog: %w", err)
	}
	defer rows.Close()

	out := domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{}}
	// The query is already ordered by park, so a single "did the park id change" check groups the
	// flat rows without a map -- and without re-sorting, which would fight the SQL ordering.
	parkIndex := map[string]int{}
	for rows.Next() {
		var parkID, parkName string
		var shedID, shedName *string
		if err := rows.Scan(&parkID, &parkName, &shedID, &shedName); err != nil {
			return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog scan: %w", err)
		}
		idx, ok := parkIndex[parkID]
		if !ok {
			out.Parks = append(out.Parks, domain.ShiftingDestinationPark{
				ParkID: parkID,
				Name:   parkName,
				Sheds:  []domain.ShiftingDestinationShed{},
			})
			idx = len(out.Parks) - 1
			parkIndex[parkID] = idx
		}
		// A NULL shed id is the LEFT JOIN's "this park has no active sheds" row, not a data error.
		if shedID == nil || shedName == nil {
			continue
		}
		out.Parks[idx].Sheds = append(out.Parks[idx].Sheds, domain.ShiftingDestinationShed{
			ShedID: *shedID,
			Name:   *shedName,
		})
	}
	if err := rows.Err(); err != nil {
		return domain.ShiftingDestinationCatalog{}, fmt.Errorf("counts: shifting destination catalog rows: %w", err)
	}
	return out, nil
}

// goatShiftingFactsQuery reads the narrow impact-shaped facts for the named animals.
//
// Predicate shape: goat_id is kept BARE and the bind array carries the cast
// (`goat_id = ANY($2::uuid[])`), never `goat_id::text = ANY($2::text[])` -- a column-side cast
// disables the ordinary index on the stored column.
//
// merged_into_goat_id IS NULL AND exited_at IS NULL mirrors the membership predicate identity's
// relocate path uses, so an animal this query happily describes is an animal the approval could
// actually move. A merged or exited goat resolves to zero rows and the caller fails closed.
//
// scale-guard:ignore: bounded by MaxRelocateGoatsPerCommand, single set-based read by id
const goatShiftingFactsQuery = `
SELECT
    g.goat_id::text,
    g.breed_id::text,
    COALESCE(
        NULLIF(btrim(b.canonical_name), ''),
        NULLIF(btrim(g.breed), ''),
        g.species
    ) AS breed_label,
    NULLIF(btrim(COALESCE(g.management_stage, '')), '') AS stage_tag,
    NULLIF(btrim(COALESCE(g.age_band, '')), '')         AS age_class,
    NULLIF(btrim(COALESCE(g.sex, '')), '')              AS sex,
    g.park_id::text,
    g.shed_id::text
FROM goats g
LEFT JOIN breeds b
       ON b.breed_id = g.breed_id
      AND b.status = 'active'
WHERE g.tenant_id = $1::uuid
  AND g.goat_id = ANY($2::uuid[])
  AND g.merged_into_goat_id IS NULL
  AND g.exited_at IS NULL
ORDER BY g.goat_id`

// GoatShiftingFacts implements ports.Repository.GoatShiftingFacts.
//
// BREED LABEL FALLBACK (deliberate, and the one derivation judgement here): the label prefers the
// canonical breeds.canonical_name, falls back to the goat's free-text goats.breed, and finally to
// goats.species (NOT NULL, defaults 'goat'). The species fallback exists so a missing breed
// attribute cannot BLOCK an operator from reporting a real movement. It is a truthful degradation
// to a coarser grain -- the animal genuinely is a goat -- not an invented label, and it keeps the
// derived impact satisfying the non-blank breed_key CHECK on shifting_event_impacts instead of
// failing at the database with an opaque error.
func (r *Repository) GoatShiftingFacts(ctx context.Context, tenantID string, goatIDs []string) ([]domain.GoatShiftingFact, error) {
	if strings.TrimSpace(tenantID) == "" || len(goatIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, goatShiftingFactsQuery, tenantID, goatIDs)
	if err != nil {
		return nil, fmt.Errorf("counts: goat shifting facts: %w", err)
	}
	defer rows.Close()

	out := make([]domain.GoatShiftingFact, 0, len(goatIDs))
	for rows.Next() {
		var fact domain.GoatShiftingFact
		var breedLabel string
		if err := rows.Scan(&fact.GoatID, &fact.BreedID, &breedLabel, &fact.StageTag, &fact.AgeClass, &fact.Sex,
			&fact.ParkID, &fact.ShedID); err != nil {
			return nil, fmt.Errorf("counts: goat shifting facts scan: %w", err)
		}
		fact.BreedLabel = strings.TrimSpace(breedLabel)
		// countAliasNorm is the SAME normalization the counts breed-alias resolver applies
		// (lowercase, whitespace runs collapsed to '_'). Reusing it is what makes a derived
		// single-animal impact group onto the same grain as an imported cohort impact for the same
		// breed, instead of forking the projection into two near-duplicate rows.
		fact.BreedKey = countAliasNorm(fact.BreedLabel)
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: goat shifting facts rows: %w", err)
	}
	return out, nil
}
