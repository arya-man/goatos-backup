package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// The kid stage ladder's due-set read (maintainer decision 2026-08-20): every ALIVE kid whose
// management_stage is the step's FromStage and whose date of birth (dob, falling back to
// approx_dob) is on or before the step's cutoff date, excluding kids already named in an IN-FLIGHT shifting so the sweeper can run
// any number of times without double-raising an animal.
//
// projection-review: membership=canonical goats rows (tenant, alive, unmerged, stage match,
// born on/before cutoff); group_key=none — one row per goat, grouping to (park, step) happens in
// the app layer from these rows; join_cardinality=breeds and goat_shed_partitions are both
// 1:{0,1} per goat (breed_id FK; gsp PK is (tenant_id, goat_id)) and the exclusion is NOT EXISTS
// (never a fan-out join); pagination=none — the due set for one business day is bounded by the
// farm's births/day (single digits), not by herd size, and the sweeper needs the WHOLE set or a
// partial raise would silently strand the rest; scope=tenant_id + management_stage + cutoff.
//
// The in-flight exclusion ranges over counts_approval_requests joined 1:1 to its shifting event:
// a goat named in the payload goat_ids of a movement whose event_status is still 'pending' or
// 'authorized' is already on its way somewhere, so raising it again would queue a second movement
// of the same animal. Later states need no clause: 'applied'/'pending_verification' movements have
// already relocated the goat (its stage changed, so the stage filter drops it), and
// 'rejected'/'canceled' movements are dead and must NOT block a re-raise.
const kidStageDueGoatsQuery = `
SELECT
    g.goat_id::text,
    g.display_id,
    COALESCE(aid1.identifier_value, '') AS tag,
    g.park_id::text,
    g.shed_id::text,
    COALESCE(park.name, '') AS park_name,
    COALESCE(shed.name, '') AS shed_name,
    CASE
        WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
            THEN btrim(gsp.partition_label)
        ELSE NULL
    END AS shed_partition_label,
    btrim(g.management_stage) AS management_stage,
    COALESCE(g.dob, g.approx_dob) AS born_on
FROM goats g
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN goat_shed_partitions gsp
       ON gsp.tenant_id = g.tenant_id
      AND gsp.goat_id = g.goat_id
-- Display identifier for the operator card / basket rows. 1:{0,1} today for RFID-carrying kids;
-- newborn kids may carry only a temporary tag, so the join is LEFT and blank degrades to ''.
LEFT JOIN LATERAL (
    SELECT gi.identifier_value
    FROM goat_identifiers gi
    WHERE gi.tenant_id = g.tenant_id
      AND gi.goat_id = g.goat_id
      AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'temporary_tag')
    ORDER BY CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 ELSE 1 END, gi.identifier_value
    LIMIT 1
) aid1 ON true
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND g.lifecycle_status = 'alive'
  AND upper(btrim(COALESCE(g.management_stage, ''))) = upper(btrim($2))
  AND COALESCE(g.dob, g.approx_dob) IS NOT NULL
  AND COALESCE(g.dob, g.approx_dob) <= $3::date
  AND g.park_id IS NOT NULL
  AND g.shed_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM counts_approval_requests car
      JOIN shifting_events se
        ON se.tenant_id = car.tenant_id
       AND se.shifting_event_id = car.shifting_event_id
      WHERE car.tenant_id = g.tenant_id
        AND car.request_type = 'shifting'
        AND se.event_status IN ('pending', 'authorized')
        AND car.payload->'goat_ids' ? g.goat_id::text
  )
ORDER BY g.park_id, COALESCE(g.dob, g.approx_dob), g.goat_id`

// ListKidStageDueGoats returns the kids due to leave fromStage: alive, unmerged, placed, born on
// or before bornOnOrBefore, and not already named in an in-flight movement.
func (r *Repository) ListKidStageDueGoats(
	ctx context.Context,
	tenantID string,
	fromStage string,
	bornOnOrBefore time.Time,
) ([]domain.KidStageDueGoat, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(fromStage) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, kidStageDueGoatsQuery, tenantID, fromStage, bornOnOrBefore.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("counts: kid stage due goats: %w", err)
	}
	defer rows.Close()

	var out []domain.KidStageDueGoat
	for rows.Next() {
		var goat domain.KidStageDueGoat
		if err := rows.Scan(&goat.GoatID, &goat.DisplayID, &goat.Tag, &goat.ParkID, &goat.ShedID,
			&goat.ParkName, &goat.ShedName, &goat.PartitionLabel, &goat.ManagementStage, &goat.BornOn); err != nil {
			return nil, fmt.Errorf("counts: kid stage due goats scan: %w", err)
		}
		out = append(out, goat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: kid stage due goats rows: %w", err)
	}
	return out, nil
}
