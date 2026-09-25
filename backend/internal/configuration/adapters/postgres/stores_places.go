package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// Farm places live on the tables the whole product reads: locations (farm / park / shed rows,
// status active|inactive), the per-type profile tables, and shed_partitions. Nothing here
// invents a second copy of a place.
//
// A location's status maps onto the register's: 'active' is active, anything else is archived.
// Archiving sets status = 'inactive' and retired_at, exactly as the locations module's retire
// does, so every reader that already filters on status keeps agreeing.

const locationStatusSQL = `CASE WHEN l.status = 'active' THEN 'active' ELSE 'archived' END`

// ---------------------------------------------------------------------------------------------
// Parks

type parkStore struct{}

var parkProjection = projection{sql: `
SELECT l.location_id::text AS id,
       l.name AS display,
       ` + locationStatusSQL + ` AS status,
       l.row_version,
       false AS is_builtin,
       jsonb_build_object('name', l.name, 'code', COALESCE(pp.park_code, l.location_code), 'capacity', pp.capacity, 'notes', NULLIF(pp.notes, '')) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object(
         'pens', (SELECT count(*) FROM locations c WHERE c.tenant_id = l.tenant_id AND c.parent_location_id = l.location_id AND c.location_type = 'shed' AND c.status = 'active' AND ` + penAliasExclusion + `),
         'animals', (SELECT count(*) FROM goats g JOIN locations s ON s.tenant_id = g.tenant_id AND s.location_id = g.shed_id WHERE g.tenant_id = l.tenant_id AND s.parent_location_id = l.location_id AND g.lifecycle_status = 'alive')
       ) AS counts,
       lower(COALESCE(pp.park_code, l.location_code, l.name)) AS sort_key
FROM locations l
LEFT JOIN park_profiles pp ON pp.tenant_id = l.tenant_id AND pp.location_id = l.location_id
WHERE l.tenant_id = $1 AND l.location_type = 'park'`}

// penAliasExclusion keeps the legacy "Castro 1" alias rows (one shed row per partition, kept
// active for older readers) out of every pen count and list: a pen is the physical building.
// The predicate is the shared oploc one, aliased to the count's own correlation name.
var penAliasExclusion = oploc.PartitionAliasExclusionSQL("c")

func (parkStore) count(ctx context.Context, q querier, t string) (int, error) {
	return parkProjection.count(ctx, q, t)
}
func (parkStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return parkProjection.list(ctx, q, t, p)
}
func (parkStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return parkProjection.get(ctx, q, t, id)
}
func (parkStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return parkProjection.options(ctx, q, t)
}
func (parkStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	return usageOf(ctx, q, t, id,
		usageCheck{"pens", `SELECT count(*) FROM locations WHERE tenant_id = $1 AND parent_location_id = $2::uuid AND location_type = 'shed' AND status = 'active'`},
		usageCheck{"animals", `SELECT count(*) FROM goats g JOIN locations s ON s.tenant_id = g.tenant_id AND s.location_id = g.shed_id WHERE g.tenant_id = $1 AND s.parent_location_id = $2::uuid AND g.lifecycle_status = 'alive'`},
		usageCheck{"people", `SELECT count(*) FROM workforce_members WHERE tenant_id = $1 AND primary_location_id = $2::uuid AND status = 'active'`},
	)
}

func (parkStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	code := strings.ToUpper(domain.FieldString(f, "code"))
	if err := validParkCode(code); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, sqlPlaces4, t, code, domain.FieldString(f, "name")).Scan(&id); err != nil {
		return "", locationWriteError(err, "park")
	}
	if _, err := tx.Exec(ctx, sqlPlaces5, id, t, code, nullInt(f, "capacity"), nullText(f, "notes")); err != nil {
		return "", err
	}
	return id, nil
}

func (parkStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	if sent(f, "code") {
		f["code"] = strings.ToUpper(domain.FieldString(f, "code"))
		if err := validParkCode(domain.FieldString(f, "code")); err != nil {
			return "", err
		}
		if err := refuseRecodingAParkInUse(ctx, tx, t, id, domain.FieldString(f, "code")); err != nil {
			return "", err
		}
	}
	if err := updateLocation(ctx, tx, t, id, f, rv, "park"); err != nil {
		return "", err
	}
	set, args := setClause(f, []colBind{{"code", "park_code", textArg("code")}, {"capacity", "capacity", intArg("capacity")}, {"notes", "notes", textOrEmpty("notes")}}, 3)
	if set == "" {
		return "", nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO park_profiles (location_id, tenant_id) VALUES ($1::uuid, $2) ON CONFLICT (location_id) DO NOTHING`, id, t); err != nil {
		return "", err
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE park_profiles SET %s, updated_at = now(), row_version = row_version + 1 WHERE location_id = $1::uuid AND tenant_id = $2`, set), append([]any{id, t}, args...)...)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, q.SQL(), q.Args()...)
	return "", err
}

func (parkStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	return setLocationStatus(ctx, tx, t, id, status, rv)
}

func (parkStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	return deleteLocation(ctx, tx, t, id, rv, "park_profiles")
}

// ---------------------------------------------------------------------------------------------
// Pens (shed rows under a park, with their shed_profiles)

type penStore struct{}

var penProjection = projection{parentField: "park_id", sql: `
SELECT l.location_id::text AS id,
       l.name AS display,
       ` + locationStatusSQL + ` AS status,
       l.row_version,
       false AS is_builtin,
       jsonb_build_object('park_id', l.parent_location_id::text, 'name', l.name, 'capacity', sp.capacity,
                          'notes', NULLIF(sp.notes, '')) AS fields,
       jsonb_strip_nulls(jsonb_build_object('park_id', p.name)) AS labels,
       jsonb_build_object(
         'partitions', (SELECT count(*) FROM shed_partitions x WHERE x.tenant_id = l.tenant_id AND x.shed_id = l.location_id AND x.status = 'active'),
         'animals', (SELECT count(*) FROM goats g WHERE g.tenant_id = l.tenant_id AND g.shed_id = l.location_id AND g.lifecycle_status = 'alive')
       ) AS counts,
       lower(COALESCE(p.location_code, p.name)) || ' ' || lower(l.name) AS sort_key
FROM locations l
JOIN locations p ON p.tenant_id = l.tenant_id AND p.location_id = l.parent_location_id AND p.location_type = 'park'
LEFT JOIN shed_profiles sp ON sp.tenant_id = l.tenant_id AND sp.location_id = l.location_id
WHERE l.tenant_id = $1 AND l.location_type = 'shed'
  AND ` + oploc.PartitionAliasExclusionSQL("l")}

func (penStore) count(ctx context.Context, q querier, t string) (int, error) {
	return penProjection.count(ctx, q, t)
}
func (penStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return penProjection.list(ctx, q, t, p)
}
func (penStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return penProjection.get(ctx, q, t, id)
}
func (penStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return penProjection.options(ctx, q, t)
}
func (penStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	return usageOf(ctx, q, t, id,
		usageCheck{"animals", `SELECT count(*) FROM goats WHERE tenant_id = $1 AND shed_id = $2::uuid AND lifecycle_status = 'alive'`},
		usageCheck{"partitions", `SELECT count(*) FROM shed_partitions WHERE tenant_id = $1 AND shed_id = $2::uuid AND status = 'active'`},
	)
}

// penCode is the location_code a new pen gets: <PARK>_SHED_<NAME>, the shape every seeded pen
// already carries (CPT_SHED_GODEL_1). Codes are unique per tenant, which is how two pens of one
// park cannot share a name.
func penCode(parkCode, name string) string {
	up := strings.ToUpper(domain.NormalizeCode(name))
	return strings.ToUpper(parkCode) + "_SHED_" + up
}

func (penStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	parkID := domain.FieldString(f, "park_id")
	parkCode, err := locationCode(ctx, tx, t, parkID, "park", "park_id", "Park")
	if err != nil {
		return "", err
	}
	name := domain.FieldString(f, "name")
	var id string
	if err := tx.QueryRow(ctx, sqlPlaces6, t, penCode(parkCode, name), name, parkID).Scan(&id); err != nil {
		return "", locationWriteError(err, "pen")
	}
	if _, err := tx.Exec(ctx, sqlPlaces7, id, t, nullInt(f, "capacity"), nullText(f, "notes")); err != nil {
		return "", err
	}
	return id, nil
}

func (penStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	if sent(f, "park_id") {
		// A pen may be re-parented only while it holds no animals: the herd never moves parks.
		var animals int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM goats WHERE tenant_id = $1 AND shed_id = $2::uuid AND lifecycle_status = 'alive'`, t, id).Scan(&animals); err != nil {
			return "", err
		}
		if animals > 0 {
			var current string
			if err := tx.QueryRow(ctx, `SELECT parent_location_id::text FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id).Scan(&current); err != nil {
				return "", err
			}
			if current != domain.FieldString(f, "park_id") {
				return "", &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "animals in this pen; animals never move between parks", Count: animals}}}}
			}
		}
	}
	if sent(f, "name") || sent(f, "park_id") {
		// The code follows the name and the park.
		var parkID, name string
		if err := tx.QueryRow(ctx, `SELECT parent_location_id::text, name FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid AND location_type = 'shed'`, t, id).Scan(&parkID, &name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ports.ErrNotFound
			}
			return "", err
		}
		if sent(f, "park_id") {
			parkID = domain.FieldString(f, "park_id")
		}
		if sent(f, "name") {
			name = domain.FieldString(f, "name")
		}
		parkCode, err := locationCode(ctx, tx, t, parkID, "park", "park_id", "Park")
		if err != nil {
			return "", err
		}
		f["code"] = penCode(parkCode, name)
		if sent(f, "park_id") {
			if _, err := tx.Exec(ctx, `UPDATE locations SET parent_location_id = $3::uuid WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id, parkID); err != nil {
				return "", err
			}
		}
	}
	if err := updateLocation(ctx, tx, t, id, f, rv, "pen"); err != nil {
		return "", err
	}
	set, args := setClause(f, []colBind{
		{"capacity", "capacity", intArg("capacity")},
		{"notes", "notes", textOrEmpty("notes")},
	}, 3)
	if set == "" {
		return "", nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO shed_profiles (location_id, tenant_id) VALUES ($1::uuid, $2) ON CONFLICT (location_id) DO NOTHING`, id, t); err != nil {
		return "", err
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE shed_profiles SET %s, updated_at = now(), row_version = row_version + 1 WHERE location_id = $1::uuid AND tenant_id = $2`, set), append([]any{id, t}, args...)...)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, q.SQL(), q.Args()...)
	return "", err
}

func (penStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	return setLocationStatus(ctx, tx, t, id, status, rv)
}

func (penStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	return deleteLocation(ctx, tx, t, id, rv, "shed_profiles")
}

// ---------------------------------------------------------------------------------------------
// Partitions (shed_partitions; id = shed_id:normalized_label since the table has no surrogate)

type partitionStore struct{}

var partitionProjection = projection{parentField: "pen_id", decorate: decoratePartition, sql: `
SELECT concat_ws(':', x.shed_id::text, x.normalized_label) AS id,
       -- The shed name alone: the operational label is composed in Go by decoratePartition
       -- through oploc, never here (the list wrapper also searches fields->>'label').
       s.name AS display,
       replace(x.status, 'retired', 'archived') AS status,
       0 AS row_version,
       false AS is_builtin,
       jsonb_build_object('park_id', s.parent_location_id::text, 'pen_id', x.shed_id::text, 'label', x.partition_label, 'sort_order', x.display_order, 'shed_name', s.name, 'shed_type', x.shed_type) AS fields,
       -- The pen type's NAME from the Pen types register (000428), so a rename shows at once.
       jsonb_strip_nulls(jsonb_build_object('park_id', p.name, 'pen_id', s.name, 'shed_type', pt.name)) AS labels,
       jsonb_build_object(
         'animals', (SELECT count(*) FROM goat_shed_partitions gp JOIN goats g ON g.tenant_id = gp.tenant_id AND g.goat_id = gp.goat_id
                     WHERE gp.tenant_id = x.tenant_id AND gp.shed_id = x.shed_id AND g.lifecycle_status = 'alive'
                       AND regexp_replace(lower(btrim(gp.partition_label)), '^part[[:space:]]+', '') = x.normalized_label)
       ) AS counts,
       concat_ws(' ', lower(COALESCE(p.location_code, p.name)), lower(s.name), lpad(COALESCE(x.display_order, 0)::text, 4, '0'), lpad(x.normalized_label, 6, '0')) AS sort_key
FROM shed_partitions x
JOIN locations s ON s.tenant_id = x.tenant_id AND s.location_id = x.shed_id
JOIN locations p ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
-- 1:{0,1}: pen_types is keyed (tenant_id, pen_type_key), so this can never multiply a partition.
LEFT JOIN pen_types pt ON pt.tenant_id = x.tenant_id AND pt.pen_type_key = x.shed_type
WHERE x.tenant_id = $1`}

// decoratePartition composes the display through oploc so it reads "Godel 1 - Part 3" here
// exactly as it does on every other surface; SQL never composes a partition name.
func decoratePartition(row *domain.Row) {
	loc := oploc.OperationalLocation{ShedID: domain.FieldString(row.Fields, "pen_id"), ShedName: domain.FieldString(row.Fields, "shed_name"), PartitionLabel: domain.FieldString(row.Fields, "label")}
	row.Display = loc.Display()
	delete(row.Fields, "shed_name")
}

func (partitionStore) count(ctx context.Context, q querier, t string) (int, error) {
	return partitionProjection.count(ctx, q, t)
}
func (partitionStore) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return partitionProjection.list(ctx, q, t, p)
}
func (partitionStore) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return partitionProjection.get(ctx, q, t, id)
}
func (partitionStore) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return partitionProjection.options(ctx, q, t)
}

func splitPartitionID(id string) (shedID, normalized string, ok bool) {
	shedID, normalized, ok = strings.Cut(id, ":")
	return shedID, normalized, ok && isUUID(shedID) && normalized != ""
}

func (partitionStore) usage(ctx context.Context, q querier, t, id string) (domain.Usage, error) {
	shedID, normalized, ok := splitPartitionID(id)
	if !ok {
		return domain.Usage{}, ports.ErrNotFound
	}
	var n int
	if err := q.QueryRow(ctx, sqlPlaces8, t, shedID, normalized).Scan(&n); err != nil {
		return domain.Usage{}, err
	}
	return domain.Usage{Blocked: n > 0, Uses: []domain.UsageCount{{Noun: "animals", Count: n}}}, nil
}

func (partitionStore) insert(ctx context.Context, tx pgx.Tx, t string, f map[string]any) (string, error) {
	penID := domain.FieldString(f, "pen_id")
	if err := requireLocation(ctx, tx, t, penID, "shed", "pen_id", "Pen"); err != nil {
		return "", err
	}
	if parkID := domain.FieldString(f, "park_id"); parkID != "" {
		var parent string
		if err := tx.QueryRow(ctx, `SELECT parent_location_id::text FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid`, t, penID).Scan(&parent); err == nil && parent != parkID {
			return "", &ports.RefError{Field: "pen_id", Label: "pen of that park"}
		}
	}
	label := domain.FieldString(f, "label")
	normalized := oploc.NormalizePartition(label)
	if normalized == oploc.WholeSentinel {
		return "", &ports.DuplicateError{Field: "label", Message: "Give the partition a label such as Part 3 or 2."}
	}
	if err := requirePenType(ctx, tx, t, domain.FieldString(f, "shed_type"), ""); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, sqlPlaces9, t, penID, label, normalized, nullInt(f, "sort_order"), nullText(f, "shed_type")); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", &ports.DuplicateError{Field: "label", Message: "This pen already has that partition."}
		}
		return "", err
	}
	return penID + ":" + normalized, nil
}

func (partitionStore) update(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int) (string, error) {
	shedID, normalized, ok := splitPartitionID(id)
	if !ok {
		return "", ports.ErrNotFound
	}
	if sent(f, "pen_id") && domain.FieldString(f, "pen_id") != shedID {
		return "", &ports.DuplicateError{Field: "pen_id", Message: "A partition cannot move to another pen; add it there instead."}
	}
	if sent(f, "label") {
		label := domain.FieldString(f, "label")
		if oploc.NormalizePartition(label) != normalized {
			// Renaming past the matching key would orphan every animal filed under the old one.
			var animals int
			if err := tx.QueryRow(ctx, sqlPlaces10, t, shedID, normalized).Scan(&animals); err != nil {
				return "", err
			}
			if animals > 0 {
				return "", &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "animals filed under this label", Count: animals}}}}
			}
			newNorm := oploc.NormalizePartition(label)
			if newNorm == oploc.WholeSentinel {
				return "", &ports.DuplicateError{Field: "label", Message: "Give the partition a label such as Part 3 or 2."}
			}
			tag, err := tx.Exec(ctx, `UPDATE shed_partitions SET partition_label = $4, normalized_label = $5, updated_at = now() WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`, t, shedID, normalized, label, newNorm)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					return "", &ports.DuplicateError{Field: "label", Message: "This pen already has that partition."}
				}
				return "", err
			}
			if tag.RowsAffected() == 0 {
				return "", ports.ErrNotFound
			}
			normalized = newNorm
		} else if _, err := tx.Exec(ctx, `UPDATE shed_partitions SET partition_label = $4, updated_at = now() WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`, t, shedID, normalized, label); err != nil {
			return "", err
		}
	}
	if sent(f, "sort_order") {
		if _, err := tx.Exec(ctx, `UPDATE shed_partitions SET display_order = $4, updated_at = now() WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`, t, shedID, normalized, nullInt(f, "sort_order")); err != nil {
			return "", err
		}
	}
	if sent(f, "shed_type") {
		var current string
		if err := tx.QueryRow(ctx, sqlPlacesPenTypeOf, t, shedID, normalized).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if err := requirePenType(ctx, tx, t, domain.FieldString(f, "shed_type"), current); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, sqlPlaces11, t, shedID, normalized, nullText(f, "shed_type")); err != nil {
			return "", err
		}
	}
	// A rename past the matching key moves the row's id with it.
	return shedID + ":" + normalized, nil
}

func (partitionStore) setStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	shedID, normalized, ok := splitPartitionID(id)
	if !ok {
		return ports.ErrNotFound
	}
	dbStatus := "retired"
	if status == domain.StatusActive {
		dbStatus = "active"
	}
	tag, err := tx.Exec(ctx, `UPDATE shed_partitions SET status = $4, updated_at = now() WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`, t, shedID, normalized, dbStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (partitionStore) del(ctx context.Context, tx pgx.Tx, t, id string, rv int) error {
	shedID, normalized, ok := splitPartitionID(id)
	if !ok {
		return ports.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `DELETE FROM shed_partitions WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`, t, shedID, normalized)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Shared location helpers

// updateLocation applies name / code to the locations row, fenced on row_version.
func updateLocation(ctx context.Context, tx pgx.Tx, t, id string, f map[string]any, rv int, noun string) error {
	set, args := setClause(f, []colBind{{"name", "name", textOrEmpty("name")}, {"code", "location_code", textArg("code")}}, 4)
	if set == "" {
		set = "updated_at = now(), row_version = row_version + 1"
	} else {
		set += ", updated_at = now(), row_version = row_version + 1"
	}
	q, err := sqlbind.Bind(fmt.Sprintf(`UPDATE locations SET %s WHERE tenant_id = $1 AND location_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, set), append([]any{t, id, rv}, args...)...)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return locationWriteError(err, noun)
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id)
}

func setLocationStatus(ctx context.Context, tx pgx.Tx, t, id, status string, rv int) error {
	var tag pgconn.CommandTag
	var err error
	if status == domain.StatusActive {
		tag, err = tx.Exec(ctx, `UPDATE locations SET status = 'active', retired_at = NULL, retired_by = NULL, updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND location_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv)
	} else {
		tag, err = tx.Exec(ctx, `UPDATE locations SET status = 'inactive', retired_at = now(), updated_at = now(), row_version = row_version + 1 WHERE tenant_id = $1 AND location_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv)
	}
	if err != nil {
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id)
}

func deleteLocation(ctx context.Context, tx pgx.Tx, t, id string, rv int, profileTable string) error {
	q, err := sqlbind.Bind(fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND location_id = $2::uuid`, profileTable), t, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, q.SQL(), q.Args()...); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM location_operational_attributes WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid AND ($3 = 0 OR row_version = $3)`, t, id, rv)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// Something outside the usage checks still points here: history rows, tasks. Keep it.
			return &ports.InUseError{Usage: domain.Usage{Blocked: true, Uses: []domain.UsageCount{{Noun: "past records; archive it instead", Count: 1}}}}
		}
		return err
	}
	return fenced(ctx, tx, tag.RowsAffected(), `SELECT 1 FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid`, t, id)
}

// requireLocation refuses a ref to a location that is not an active row of the wanted type.
func requireLocation(ctx context.Context, q querier, t, id, locationType, field, label string) error {
	if !isUUID(id) {
		return &ports.RefError{Field: field, Label: label}
	}
	var one int
	err := q.QueryRow(ctx, `SELECT 1 FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid AND location_type = $3 AND status = 'active'`, t, id, locationType).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ports.RefError{Field: field, Label: label}
	}
	return err
}

// locationCode is requireLocation plus the row's code (a park's CBE / CPT).
func locationCode(ctx context.Context, q querier, t, id, locationType, field, label string) (string, error) {
	if !isUUID(id) {
		return "", &ports.RefError{Field: field, Label: label}
	}
	var code *string
	var name string
	err := q.QueryRow(ctx, `SELECT location_code, name FROM locations WHERE tenant_id = $1 AND location_id = $2::uuid AND location_type = $3 AND status = 'active'`, t, id, locationType).Scan(&code, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &ports.RefError{Field: field, Label: label}
	}
	if err != nil {
		return "", err
	}
	if code == nil || strings.TrimSpace(*code) == "" {
		return strings.ToUpper(domain.NormalizeCode(name)), nil
	}
	return *code, nil
}

func requireStage(ctx context.Context, q querier, t, id string) error {
	if !isUUID(id) {
		return &ports.RefError{Field: "stage_id", Label: "Stage"}
	}
	var one int
	err := q.QueryRow(ctx, `SELECT 1 FROM animal_stage_lookup WHERE tenant_id = $1 AND animal_stage_id = $2::uuid AND status = 'active'`, t, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ports.RefError{Field: "stage_id", Label: "Stage"}
	}
	return err
}

// locationWriteError names the field a locations unique violation lands on: the code is derived
// from the name for a pen, typed for a park or farm.
// parkCodePattern is the shape a park code must have. Sales, feed purchases and purchase loads
// store a park BY ITS CODE, and a newborn's provisional tag starts with it ("HSR-04217"), so the code
// is letters and digits only -- a code with a space or a dash would refuse every birth in that park.
var parkCodePattern = regexp.MustCompile(`^[A-Z0-9]{1,12}$`)

func validParkCode(code string) error {
	if parkCodePattern.MatchString(code) {
		return nil
	}
	return &domain.ValidationError{Fields: []domain.FieldError{{Field: "code", Code: "invalid",
		Message: "Use 1 to 12 letters and numbers, such as CBE or CPT. The code starts every kid's tag in this park."}}}
}

// refuseRecodingAParkInUse refuses changing a park's code once any record stores the park BY that
// code (a sale, a feed purchase, an animal purchase load): those rows would stop matching the park
// and drop out of its stock cards, filters and totals. A code that nothing has used yet may change.
func refuseRecodingAParkInUse(ctx context.Context, q querier, t, id, newCode string) error {
	var used bool
	if err := q.QueryRow(ctx, sqlParkCodeInUse, t, id, newCode).Scan(&used); err != nil {
		return err
	}
	if !used {
		return nil
	}
	return &domain.ValidationError{Fields: []domain.FieldError{{Field: "code", Code: "in_use",
		Message: "This park's code is already on sales, feed purchases or purchase loads recorded under it, so it cannot change. Keep the code and rename the park instead."}}}
}

func locationWriteError(err error, noun string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if noun == "pen" {
			return &ports.DuplicateError{Field: "name", Message: "A pen with that name already exists in this park."}
		}
		return &ports.DuplicateError{Field: "code", Message: "A " + noun + " with that code already exists."}
	}
	return err
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	// sqlParkCodeInUse: $1 tenant, $2 park id, $3 the new code. True when the park's CURRENT code
	// differs from the new one and some farm-keyed record already stores the current code.
	sqlParkCodeInUse = `
WITH cur AS (
  SELECT upper(btrim(location_code)) AS code
  FROM locations
  WHERE tenant_id = $1 AND location_id = $2::uuid AND location_type = 'park'
)
SELECT EXISTS (
  SELECT 1 FROM cur
  WHERE cur.code IS NOT NULL AND cur.code <> '' AND cur.code <> $3
    AND (
      EXISTS (SELECT 1 FROM sales_deals d WHERE d.tenant_id = $1 AND d.farm = cur.code)
      OR EXISTS (SELECT 1 FROM feed_purchases p WHERE p.tenant_id = $1 AND upper(p.farm_label) = cur.code)
      OR EXISTS (SELECT 1 FROM animal_purchase_loads l WHERE l.tenant_id = $1 AND l.farm_label = cur.code)
    )
)`
	sqlPlaces4 = `
INSERT INTO locations (tenant_id, location_type, location_code, name, status)
VALUES ($1, 'park', $2, $3, 'active')
RETURNING location_id::text`
	sqlPlaces5 = `
INSERT INTO park_profiles (location_id, tenant_id, park_code, capacity, notes)
VALUES ($1::uuid, $2, $3, $4, COALESCE($5, ''))`
	sqlPlaces6 = `
INSERT INTO locations (tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, 'shed', $2, $3, $4::uuid, 'active')
RETURNING location_id::text`
	sqlPlaces7 = `
INSERT INTO shed_profiles (location_id, tenant_id, capacity, notes)
VALUES ($1::uuid, $2, $3, COALESCE($4, ''))`
	sqlPlaces8 = `
SELECT count(*) FROM goat_shed_partitions gp
JOIN goats g ON g.tenant_id = gp.tenant_id AND g.goat_id = gp.goat_id
WHERE gp.tenant_id = $1 AND gp.shed_id = $2::uuid AND g.lifecycle_status = 'alive'
  AND regexp_replace(lower(btrim(gp.partition_label)), '^part[[:space:]]+', '') = $3`
	sqlPlaces9 = `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, display_order, source, shed_type)
VALUES ($1, $2::uuid, $3, $4, 'active', $5, 'manual', $6)`
	// The pen type a partition carries now, so an edit that leaves it alone is not refused
	// merely because that type has since been archived.
	sqlPlacesPenTypeOf = `
SELECT COALESCE(shed_type, '') FROM shed_partitions
WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`
	sqlPlacesPenTypeActive = `
SELECT status FROM pen_types WHERE tenant_id = $1 AND pen_type_key = $2`
	sqlPlaces11 = `
UPDATE shed_partitions SET shed_type = $4, updated_at = now()
WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = $3`
	sqlPlaces10 = `
SELECT count(*) FROM goat_shed_partitions gp JOIN goats g ON g.tenant_id = gp.tenant_id AND g.goat_id = gp.goat_id
WHERE gp.tenant_id = $1 AND gp.shed_id = $2::uuid AND g.lifecycle_status = 'alive'
  AND regexp_replace(lower(btrim(gp.partition_label)), '^part[[:space:]]+', '') = $3`
)

// requirePenType refuses a pen type the farm has not authored on the Pen types register, or one it
// has archived (migration 000428). Empty clears the type and is always allowed; a partition that
// already carries an archived type may keep it, because refusing an unrelated edit to that row
// would force the farm to reclassify a pen just to rename it. The foreign key is the backstop; this
// is what turns the refusal into a sentence on the Pen type field.
func requirePenType(ctx context.Context, tx pgx.Tx, tenantID, code, current string) error {
	if code == "" || code == current {
		return nil
	}
	var status string
	err := tx.QueryRow(ctx, sqlPlacesPenTypeActive, tenantID, code).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ports.RefError{Field: "shed_type", Label: "pen type"}
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return &ports.RefError{Field: "shed_type", Label: "active pen type"}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Pen types (pen_types, migration 000428): the kinds of pen a partition is given, authored by the
// farm. Writes are the keyed shape the SOP vocabularies use; a type still given to a partition
// cannot be removed -- archive it instead. The READ is its own, because the list answers the
// question the farm opens it with -- which kinds of pen do we have, and how many pens carry each
// (maintainer instruction 2026-09-25) -- so each row carries its partition count.

var penTypeStore = penTypeRegister{keyedStore{table: "pen_types", keyCol: "pen_type_key", checks: []usageCheck{{"partitions", `SELECT count(*) FROM shed_partitions WHERE tenant_id = $1 AND shed_type = $2`}}}}

type penTypeRegister struct{ keyedStore }

// projection-review: membership=every pen_types row of the tenant; group_key=(tenant_id,
// pen_type_key), pen_types' primary key; join_cardinality=none -- the partition count is a scalar
// subquery per pen type, answered by shed_partitions_shed_type_idx (tenant_id, shed_type), so it
// cannot multiply a row; pagination=the list wrapper's keyset over sort_key, after this projection;
// scope=tenant_id on the row and inside the count.
var penTypeProjection = projection{sql: `
SELECT r.pen_type_key AS id,
       r.name AS display,
       r.status,
       r.row_version,
       false AS is_builtin,
       jsonb_build_object('name', r.name, 'code', r.pen_type_key, 'description', NULLIF(r.description, ''), 'sort_order', r.sort_order) AS fields,
       '{}'::jsonb AS labels,
       jsonb_build_object('pens', (SELECT count(*) FROM shed_partitions sp
                                   WHERE sp.tenant_id = r.tenant_id AND sp.shed_type = r.pen_type_key AND sp.status = 'active')) AS counts,
       lpad(r.sort_order::text, 6, '0') || ' ' || lower(r.name) AS sort_key
FROM pen_types r
WHERE r.tenant_id = $1`}

func (penTypeRegister) count(ctx context.Context, q querier, t string) (int, error) {
	return penTypeProjection.count(ctx, q, t)
}
func (penTypeRegister) list(ctx context.Context, q querier, t string, p ports.ListParams) (ports.Page, error) {
	return penTypeProjection.list(ctx, q, t, p)
}
func (penTypeRegister) get(ctx context.Context, q querier, t, id string) (domain.Row, error) {
	return penTypeProjection.get(ctx, q, t, id)
}
func (penTypeRegister) options(ctx context.Context, q querier, t string) ([]ports.RefOption, error) {
	return penTypeProjection.options(ctx, q, t)
}
