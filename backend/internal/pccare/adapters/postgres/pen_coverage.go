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

// penCoverageScopedPensSQL is the caller's pen catalog ($1 tenant, $2 tenant-wide, $3 authorized
// parks, $4 optional park): shed x active catalog partition, an undivided shed contributing one
// 'whole' row, legacy partition-alias shed rows suppressed. Shared by the board page and the
// filter vocabulary so the two can never disagree about which pens exist.
// naturalSortKeySQL returns a SQL expression that sorts expr the way the farm reads pen names:
// every run of digits is zero-padded, so "Part 2" sorts before "Part 10" and "Yashoda 9" before
// "Yashoda 10". Letters are lower-cased. A NULL or blank input yields ”. Compare the result
// under COLLATE "C" so a locale collation cannot reorder the padded digits or the separator.
func naturalSortKeySQL(expr string) string {
	// substring(... from '^[0-9]+$') is NULL for a non-digit run, so COALESCE falls through to
	// the lower-cased text: a digit run is padded, anything else is kept as it reads.
	return `COALESCE((SELECT string_agg(COALESCE(lpad(substring(nk.m[1] from '^[0-9]+$'), 12, '0'), lower(nk.m[1])), '' ORDER BY nk.o)
    FROM regexp_matches(COALESCE(BTRIM(` + expr + `), ''), '([0-9]+|[^0-9]+)', 'g') WITH ORDINALITY AS nk(m, o)), '')`
}

// pen_sort is an ORDER key, never display: the shed's natural key, a chr(1) separator (lower
// than every printable byte under COLLATE "C", so "Castro" sorts before "Castro"'s pens), then
// the partition's natural key. The pen's display name still comes only from oploc.Display.
var (
	penShedSortSQL      = naturalSortKeySQL("shed.name")
	penPartitionSortSQL = naturalSortKeySQL("sp.partition_label")
)

var penCoverageScopedPensSQL = `  SELECT park.location_id AS park_id, park.name AS park_name,
         COALESCE(NULLIF(BTRIM(park.location_code), ''), park.name) AS park_key,
         shed.location_id AS shed_id, shed.name AS shed_name,
         COALESCE(NULLIF(BTRIM(sp.partition_label), ''), '') AS partition_label,
         COALESCE(NULLIF(LOWER(BTRIM(sp.partition_label)), ''), 'whole') AS partition_key,
         (` + penShedSortSQL + ` || chr(1) || ` + penPartitionSortSQL + `) COLLATE "C" AS pen_sort
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
    AND ` + oploc.PartitionAliasExclusionSQL("shed") + `
`

// penKeys normalises the Pen filter values to the "<shed_id>|<partition_key>" form the SQL
// compares against (partition keys are lower-case matching keys); never nil.
func penKeys(pens []ports.PenCareCoveragePen) []string {
	out := make([]string, 0, len(pens))
	for _, pen := range pens {
		out = append(out, strings.ToLower(strings.TrimSpace(pen.ShedID))+"|"+strings.ToLower(strings.TrimSpace(pen.PartitionKey)))
	}
	return out
}

// PenCareCoverage pages the Care Coverage board: the caller's pens (shed x active catalog
// partition; an undivided shed is one 'whole' row) and, per pen, the latest DONE business date of
// each hands-on-the-animal category. Done means the operator's work is submitted: the verifier
// approved it (status 'completed') or it is waiting for the verdict ('pending_verification') —
// maintainer decision 2026-09-26. Work sent back for rework, open or canceled work is not done.
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
	// pagination=keyset on the ORDER BY tuple; scope=tenant_id + authorized parks + optional park +
	// optional pen set (shed_id, partition_key).
	// scale-guard:ignore: one keyset page (<=100) of the caller's pen catalog (physical infrastructure, never herd-sized); each pen's lateral aggregate hits pc_care_tasks_natural_uq (tenant, category, park, shed, partition_key, ...).
	bound := sqlbind.MustBind(`
WITH scoped AS (
`+penCoverageScopedPensSQL+`),
pens AS (
  SELECT s.* FROM scoped s
  WHERE (cardinality($11::text[]) = 0 OR (s.shed_id::text || '|' || s.partition_key) = ANY($11::text[]))
),
page AS (
  SELECT p.*
  FROM pens p
  WHERE ($5::text = '' OR (p.park_key, p.pen_sort, p.shed_id::text, p.partition_key) > ($5, $6, $7, $8))
  ORDER BY p.park_key, p.pen_sort, p.shed_id::text, p.partition_key
  LIMIT $9
)
SELECT pg.park_id::text, pg.park_name, pg.park_key, pg.shed_id::text, pg.shed_name,
       pg.partition_label, pg.partition_key, pg.pen_sort,
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
      -- Done = the operator's work is SUBMITTED: approved, or still waiting for the verifier
      -- (maintainer decision 2026-09-26: both read as a tick). Rework was sent back and is not done.
      AND t.status IN ('completed', 'pending_verification')
    GROUP BY t.category
  ) d
) done ON true
ORDER BY pg.park_key, pg.pen_sort, pg.shed_id::text, pg.partition_key`,
		q.TenantID, q.TenantWide, parkIDs, strings.TrimSpace(q.ParkID),
		afterPark, afterShedName, afterShed, afterPartition, limit+1,
		domain.PlannerCategories,
		penKeys(q.Pens))
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return ports.PenCareCoveragePage{}, fmt.Errorf("pccare: pen care coverage: %w", err)
	}
	defer rows.Close()

	type cursorParts struct{ park, penSort, shedID, partitionKey string }
	out := ports.PenCareCoveragePage{Rows: []ports.PenCareCoverageRow{}}
	keys := []cursorParts{}
	for rows.Next() {
		var (
			row        ports.PenCareCoverageRow
			parkKey    string
			partKey    string
			penSort    string
			categories []string
			doneDates  []string
		)
		if err := rows.Scan(&row.ParkID, &row.ParkName, &parkKey, &row.ShedID, &row.ShedName,
			&row.PartitionLabel, &partKey, &penSort, &categories, &doneDates, &out.Total); err != nil {
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
		keys = append(keys, cursorParts{parkKey, penSort, row.ShedID, partKey})
	}
	if err := rows.Err(); err != nil {
		return ports.PenCareCoveragePage{}, err
	}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
		last := keys[limit-1]
		out.NextCursor = encodePenCoverageCursor(last.park, last.penSort, last.shedID, last.partitionKey)
	}
	if err := r.penCoverageOptions(ctx, q, parkIDs, &out); err != nil {
		return ports.PenCareCoveragePage{}, err
	}
	return out, nil
}

// penCoverageOptionsCap bounds the filter vocabulary read. The farm has a few hundred pens; the
// cap exists so a misconfigured catalog can never turn a dropdown into a table scan.
const penCoverageOptionsCap = 2000

// penCoverageOptions fills the Park and Pen filter vocabularies from the SAME scoped pen catalog
// the board reads. It ignores the park and pen filters for the park list (so another park can
// always be picked) and ignores only the pen filter for the pen list (so choosing a pen never
// empties the list it was chosen from). A pen value is "<shed_id>|<partition_key>".
func (r *Repository) penCoverageOptions(ctx context.Context, q ports.PenCareCoverageQuery, parkIDs []string, out *ports.PenCareCoveragePage) error {
	// scale-guard:ignore: the caller's pen catalog (physical infrastructure, never herd-sized), hard-capped at penCoverageOptionsCap.
	bound := sqlbind.MustBind(`
WITH scoped AS (
`+penCoverageScopedPensSQL+`)
SELECT park_id::text, park_name, shed_id::text, shed_name, partition_label, partition_key
FROM scoped
ORDER BY park_key, pen_sort, shed_id::text, partition_key
LIMIT $5`, q.TenantID, q.TenantWide, parkIDs, "", penCoverageOptionsCap)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return fmt.Errorf("pccare: pen care coverage options: %w", err)
	}
	defer rows.Close()
	selectedPark := strings.TrimSpace(q.ParkID)
	seenPark := map[string]bool{}
	out.ParkOptions = []ports.PenCareCoverageOption{}
	out.PenOptions = []ports.PenCareCoverageOption{}
	for rows.Next() {
		var parkID, parkName, shedID, shedName, partitionLabel, partitionKey string
		if err := rows.Scan(&parkID, &parkName, &shedID, &shedName, &partitionLabel, &partitionKey); err != nil {
			return err
		}
		if !seenPark[parkID] {
			seenPark[parkID] = true
			out.ParkOptions = append(out.ParkOptions, ports.PenCareCoverageOption{Value: parkID, Label: parkName})
		}
		if selectedPark != "" && parkID != selectedPark {
			continue
		}
		label := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
		if selectedPark == "" {
			// Pen names repeat across parks (both parks have a Castro 1): name the park too.
			label += " · " + parkName
		}
		out.PenOptions = append(out.PenOptions, ports.PenCareCoverageOption{Value: shedID + "|" + partitionKey, Label: label})
	}
	return rows.Err()
}
