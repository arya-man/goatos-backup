package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// Animals (maintainer instruction 2026-09-18, "one lakh animals"): the herd register, READ-ONLY
// here. It exists on this screen so the farm can download the herd as a sheet and upload new
// animals in bulk; the rows themselves are written by identity's own bulk pipeline (the
// importer hands each chunk to it), never by this store, so every herd rule -- identifier
// uniqueness, partition grain, stage vocabulary -- stays in the one place that owns it.
//
// The projection reads each animal's two active tags, its park code, pen name and partition,
// so a downloaded sheet re-uploads through the same header. Sorted by display id, the herd's
// own stable key, so the export's keyset pages never repeat or skip an animal.
//
// scale-guard:ignore: 5k-50k-envelope -- the herd's own read-only register; keyset paged on the
// indexed (tenant_id, display_id) key, served at the current release envelope like the herd
// register it links to.
const animalProjectionSQL = `
SELECT g.goat_id::text AS id,
       COALESCE(i1.identifier_value, g.display_id) AS display,
       CASE WHEN g.lifecycle_status = 'alive' THEN 'active' ELSE 'archived' END AS status,
       1 AS row_version,
       false AS is_builtin,
       jsonb_strip_nulls(jsonb_build_object(
         'animal_identifier_1', i1.identifier_value,
         'animal_identifier_2', i2.identifier_value,
         'species', g.species,
         'breed', g.breed,
         'sex', g.sex,
         'park', COALESCE(pp.park_code, p.location_code),
         'pen_name', s.name,
         'partition_label', gp.partition_label,
         'management_stage', g.management_stage,
         'dob', g.dob::text,
         'entry_date', g.entry_date::text,
         'origin', g.origin_type,
         'reproductive_status', g.reproductive_status,
         'display_id', g.display_id,
         'lifecycle_status', g.lifecycle_status
       )) AS fields,
       '{}'::jsonb AS labels,
       NULL::jsonb AS counts,
       g.display_id AS sort_key
FROM goats g
LEFT JOIN LATERAL (
  SELECT i.identifier_value FROM goat_identifiers i
  WHERE i.tenant_id = g.tenant_id AND i.goat_id = g.goat_id AND i.identifier_type = 'animal_identifier_1' AND i.status = 'active'
  ORDER BY i.created_at LIMIT 1) i1 ON true
LEFT JOIN LATERAL (
  SELECT i.identifier_value FROM goat_identifiers i
  WHERE i.tenant_id = g.tenant_id AND i.goat_id = g.goat_id AND i.identifier_type = 'animal_identifier_2' AND i.status = 'active'
  ORDER BY i.created_at LIMIT 1) i2 ON true
LEFT JOIN locations s ON s.tenant_id = g.tenant_id AND s.location_id = g.shed_id
LEFT JOIN locations p ON p.tenant_id = g.tenant_id AND p.location_id = g.park_id
LEFT JOIN park_profiles pp ON pp.tenant_id = g.tenant_id AND pp.location_id = g.park_id
LEFT JOIN goat_shed_partitions gp ON gp.tenant_id = g.tenant_id AND gp.goat_id = g.goat_id
WHERE g.tenant_id = $1 AND g.merged_into_goat_id IS NULL`

var animalProjection = projection{sql: animalProjectionSQL}

type animalStore struct{}

// The rail count reads goats alone -- no tag or place joins -- because it runs on every
// Configuration page load.
func (animalStore) count(ctx context.Context, q querier, t string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM goats WHERE tenant_id = $1 AND lifecycle_status = 'alive' AND merged_into_goat_id IS NULL`, t).Scan(&n)
	return n, err
}
func (animalStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return animalProjection.list(ctx, q, t, p)
}
func (animalStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	if !isUUID(id) {
		return domain.Row{}, ports.ErrNotFound
	}
	return animalProjection.get(ctx, q, t, id)
}
func (animalStore) options(context.Context, querier, string) ([]ports.RefOption, error) {
	// Nothing points at an animal from a register; the option list would be the herd.
	return []ports.RefOption{}, nil
}
func (animalStore) usage(context.Context, querier, string, string) (domain.Usage, error) {
	return domain.Usage{}, nil
}
func (animalStore) insert(context.Context, pgx.Tx, string, map[string]any) (string, error) {
	return "", domain.ErrReadOnlyRegister
}
func (animalStore) update(context.Context, pgx.Tx, string, string, map[string]any, int) (string, error) {
	return "", domain.ErrReadOnlyRegister
}
func (animalStore) setStatus(context.Context, pgx.Tx, string, string, string, int) error {
	return domain.ErrReadOnlyRegister
}
func (animalStore) del(context.Context, pgx.Tx, string, string, int) error {
	return domain.ErrReadOnlyRegister
}
