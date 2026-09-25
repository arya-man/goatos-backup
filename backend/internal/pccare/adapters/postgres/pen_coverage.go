package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// penCoverageCursor encodes/decodes the keyset cursor — the exact ORDER BY tuple (park sort
// key, shed name, shed id, partition key), so the walk resumes where the page stopped.
func encodePenCoverageCursor(parkKey, shedName, shedID, partitionKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{parkKey, shedName, shedID, partitionKey}, "\x1f")))
}

func decodePenCoverageCursor(cursor string) (parkKey, shedName, shedID, partitionKey string, err error) {
	if strings.TrimSpace(cursor) == "" {
		return "", "", "", "", nil
	}
	raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
	if decodeErr != nil {
		return "", "", "", "", ports.ErrInvalidArgument
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 4 {
		return "", "", "", "", ports.ErrInvalidArgument
	}
	return parts[0], parts[1], parts[2], parts[3], nil
}

// PenCareCoverage pages the Care Coverage board: the caller's pens (shed x active catalog
// partition; an undivided shed is one 'whole' row) and, per pen, the latest DONE business date of
// each hands-on-the-animal category. Done means the verifier approved the task's evidence
// (status 'completed') — a submitted task still awaiting its verdict is not done.
func (r *Repository) PenCareCoverage(ctx context.Context, q ports.PenCareCoverageQuery) (ports.PenCareCoveragePage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	afterPark, afterShedName, afterShed, afterPartition, err := decodePenCoverageCursor(q.Cursor)
	if err != nil {
		return ports.PenCareCoveragePage{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	parkIDs := q.AuthorizedParkIDs
	if parkIDs == nil {
		parkIDs = []string{}
	}

	// projection-review: membership=the active pens of the caller's parks (locations sheds x
	// active shed_partitions, an undivided shed contributing one 'whole' row, legacy alias rows
	// suppressed by oploc.PartitionAliasExclusionSQL); group_key=(park_id, shed_id,
	// partition_key) on BOTH the pen side and the task side — pc_care_tasks.partition_key is the
	// same lower(btrim(label))/'whole' expression; join_cardinality=the lateral side is
	// pre-aggregated to at most one row per category per pen (GROUP BY category), so a pen never
	// fans out, and total counts the whole scoped pen set independent of the page;
	// pagination=keyset on the ORDER BY tuple; scope=tenant_id + authorized parks + optional park.
	// scale-guard:ignore: one keyset page (<=100) of the caller's pen catalog (physical infrastructure, never herd-sized); each pen's lateral aggregate hits pc_care_tasks_natural_uq (tenant, category, park, shed, partition_key, ...).
	bound := sqlbind.MustBind(`
WITH pens AS (
  SELECT park.location_id AS park_id, park.name AS park_name,
         COALESCE(NULLIF(BTRIM(park.location_code), ''), park.name) AS park_key,
         shed.location_id AS shed_id, shed.name AS shed_name,
         COALESCE(NULLIF(BTRIM(sp.partition_label), ''), '') AS partition_label,
         COALESCE(NULLIF(LOWER(BTRIM(sp.partition_label)), ''), 'whole') AS partition_key
  FROM locations park
  JOIN locations shed
    ON shed.tenant_id = park.tenant_id
   AND shed.parent_location_id = park.location_id
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
   AND shed.retired_at IS NULL
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = shed.tenant_id AND sp.shed_id = shed.location_id AND sp.status = 'active'
   AND COALESCE(NULLIF(BTRIM(sp.partition_label), ''), 'whole') <> 'whole'
  WHERE park.tenant_id = $1::uuid
    AND park.location_type = 'park'
    AND park.status = 'active'
    AND park.retired_at IS NULL
    AND ($2::bool OR park.location_id = ANY($3::uuid[]))
    AND ($4::text = '' OR park.location_id = nullif($4::text, '')::uuid)
    AND `+oploc.PartitionAliasExclusionSQL("shed")+`
),
page AS (
  SELECT p.*
  FROM pens p
  WHERE ($5::text = '' OR (p.park_key, p.shed_name, p.shed_id::text, p.partition_key) > ($5, $6, $7, $8))
  ORDER BY p.park_key, p.shed_name, p.shed_id::text, p.partition_key
  LIMIT $9
)
SELECT pg.park_id::text, pg.park_name, pg.park_key, pg.shed_id::text, pg.shed_name,
       pg.partition_label, pg.partition_key,
       COALESCE(done.categories, ARRAY[]::text[]),
       COALESCE(done.done_dates, ARRAY[]::text[]),
       (SELECT count(*) FROM pens)::int
FROM page pg
LEFT JOIN LATERAL (
  SELECT array_agg(d.category ORDER BY d.category) AS categories,
         array_agg(d.done_date::text ORDER BY d.category) AS done_dates
  FROM (
    SELECT t.category,
           max(COALESCE((t.submitted_at AT TIME ZONE 'Asia/Kolkata')::date, t.due_business_date)) AS done_date
    FROM pc_care_tasks t
    WHERE t.tenant_id = $1::uuid
      AND t.category = ANY($10::text[])
      AND t.park_id = pg.park_id
      AND t.shed_id = pg.shed_id
      AND t.partition_key = pg.partition_key
      AND t.work_state <> 'canceled'
      AND t.status = 'completed'
    GROUP BY t.category
  ) d
) done ON true
ORDER BY pg.park_key, pg.shed_name, pg.shed_id::text, pg.partition_key`,
		q.TenantID, q.TenantWide, parkIDs, strings.TrimSpace(q.ParkID),
		afterPark, afterShedName, afterShed, afterPartition, limit+1,
		domain.PlannerCategories)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return ports.PenCareCoveragePage{}, fmt.Errorf("pccare: pen care coverage: %w", err)
	}
	defer rows.Close()

	type cursorParts struct{ park, shedName, shedID, partitionKey string }
	out := ports.PenCareCoveragePage{Rows: []ports.PenCareCoverageRow{}}
	keys := []cursorParts{}
	for rows.Next() {
		var (
			row        ports.PenCareCoverageRow
			parkKey    string
			partKey    string
			categories []string
			doneDates  []string
		)
		if err := rows.Scan(&row.ParkID, &row.ParkName, &parkKey, &row.ShedID, &row.ShedName,
			&row.PartitionLabel, &partKey, &categories, &doneDates, &out.Total); err != nil {
			return ports.PenCareCoveragePage{}, err
		}
		done := make(map[string]string, len(categories))
		for i, category := range categories {
			if i < len(doneDates) {
				done[category] = doneDates[i]
			}
		}
		// One cell per category, in the domain's display order, so every row carries the same
		// columns whether or not the pen has ever had that work.
		row.Cells = make([]ports.PenCareCoverageCell, 0, len(domain.PlannerCategories))
		for _, category := range domain.PlannerCategories {
			row.Cells = append(row.Cells, ports.PenCareCoverageCell{Category: category, LastDoneBusinessDate: done[category]})
		}
		out.Rows = append(out.Rows, row)
		keys = append(keys, cursorParts{parkKey, row.ShedName, row.ShedID, partKey})
	}
	if err := rows.Err(); err != nil {
		return ports.PenCareCoveragePage{}, err
	}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
		last := keys[limit-1]
		out.NextCursor = encodePenCoverageCursor(last.park, last.shedName, last.shedID, last.partitionKey)
	}
	return out, nil
}
