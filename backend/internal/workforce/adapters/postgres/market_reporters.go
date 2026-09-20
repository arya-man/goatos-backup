package postgres

import (
	"context"
	"fmt"

	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// listPeopleWithModuleSQL: every active person with a login, flagged by whether they hold the
// named module on the named surface (any level). One indexed read; bounded by the roster size.
// projection-review: membership=workforce_members (active, with a login), one row per person;
// group_key=workforce_member_id; join_cardinality=person_module_access is 0..1 per (person,
// surface, module) by its primary key, so the EXISTS cannot fan out; locations 1:1 on
// primary_location_id and workforce_member_titles 1:1 on the member PK; pagination=none (a roster of tens); scope=tenant_id.
const listPeopleWithModuleSQL = `
SELECT m.workforce_member_id::text, m.display_name, COALESCE(wt.title, ''), COALESCE(l.name, ''),
       EXISTS (
         SELECT 1 FROM person_module_access pma
         WHERE pma.tenant_id = m.tenant_id AND pma.workforce_member_id = m.workforce_member_id
           AND pma.surface = $2 AND pma.module_key = $3 AND cardinality(pma.capabilities) > 0
       ) AS holds
FROM workforce_members m
LEFT JOIN workforce_member_titles wt ON wt.tenant_id = m.tenant_id AND wt.workforce_member_id = m.workforce_member_id
LEFT JOIN locations l ON l.tenant_id = m.tenant_id AND l.location_id = m.primary_location_id
WHERE m.tenant_id = $1::uuid AND m.status = 'active' AND m.user_id IS NOT NULL
ORDER BY holds DESC, m.display_name
LIMIT 300`

// ListPeopleWithModule implements workforceapp.MarketReporterLister.
func (r *AccessRepository) ListPeopleWithModule(ctx context.Context, tenantID, surface, moduleKey string) ([]workforceapp.MarketReporterRow, error) {
	rows, err := r.pool.Query(ctx, listPeopleWithModuleSQL, tenantID, surface, moduleKey)
	if err != nil {
		return nil, fmt.Errorf("workforce: list people with module: %w", err)
	}
	defer rows.Close()
	out := []workforceapp.MarketReporterRow{}
	for rows.Next() {
		var p workforceapp.MarketReporterRow
		if err := rows.Scan(&p.PersonID, &p.DisplayName, &p.Title, &p.ParkLabel, &p.Holds); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
