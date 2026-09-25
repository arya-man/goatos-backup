// Package parkcatalog is THE answer to "which parks does this farm have", for every module that
// stores a park by its short CODE (sales farms, feed purchases, animal purchase loads, births).
//
// Parks are authored on Configuration > Items & settings > Parks, which writes a `locations` row
// with an upper-cased, required code. Until this package existed several modules answered the
// question with a constant pair of codes, so a park added there was refused by sales, feed
// purchases, animal purchases and births, and was missing from their dropdowns, while every other
// screen showed it. Reading the catalog here makes a new park reach every one of them the moment
// it is saved, with no deploy.
//
// Only ACTIVE parks are listed: an archived park keeps its history (rows stored under its code
// still read back and still count under "All farms") but no new work may be recorded against it.
package parkcatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ActiveParksSQL lists the tenant's active parks that carry a code, in the order every park
// picker uses: display_order, then CODE (CBE before CPT), never name.
//
// Parameters: $1 tenant_id.
const ActiveParksSQL = `
SELECT btrim(location_code), COALESCE(NULLIF(btrim(name), ''), btrim(location_code)), location_id::text
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
  AND btrim(COALESCE(location_code, '')) <> ''
ORDER BY display_order, btrim(location_code), location_id
LIMIT 500`

// Park is one active park: its stored code, its display name and its location id.
type Park struct {
	Code string
	Name string
	ID   string
}

// Querier is the subset of a pgx pool or transaction ListActive needs.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ListActive returns the tenant's active parks.
func ListActive(ctx context.Context, q Querier, tenantID string) ([]Park, error) {
	rows, err := q.Query(ctx, ActiveParksSQL, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, fmt.Errorf("parkcatalog: list active parks: %w", err)
	}
	defer rows.Close()
	out := []Park{}
	for rows.Next() {
		var p Park
		if err := rows.Scan(&p.Code, &p.Name, &p.ID); err != nil {
			return nil, fmt.Errorf("parkcatalog: scan park: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parkcatalog: list active parks: %w", err)
	}
	return out, nil
}

// Codes returns the parks' codes in catalog order.
func Codes(parks []Park) []string {
	out := make([]string, 0, len(parks))
	for _, p := range parks {
		out = append(out, p.Code)
	}
	return out
}

// Has reports whether code is exactly one of the parks' codes, as stored.
func Has(parks []Park, code string) bool {
	for _, p := range parks {
		if p.Code == code {
			return true
		}
	}
	return false
}

// Resolve matches raw against the parks ignoring case and surrounding space, returning the stored
// code. ok is false when no active park carries that code.
func Resolve(parks []Park, raw string) (code string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	for _, p := range parks {
		if strings.EqualFold(p.Code, trimmed) {
			return p.Code, true
		}
	}
	return "", false
}

// ReasonUnknownPark is the farm-worded refusal every module returns for a code no active park
// carries.
const ReasonUnknownPark = "must be one of your parks"
