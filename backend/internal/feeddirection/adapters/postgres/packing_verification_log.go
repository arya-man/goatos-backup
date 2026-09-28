package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// packingVerificationLogSQL is the FEED VERIFICATION panel on /verify (maintainer decisions
// 2026-09-28): for ONE feed day, one row per park, pen and session with three totals -- the feed
// direction sheet's PLANNED total, the PACKED total the verifier entered when she approved
// yesterday's packing video (sum of her per-feed readings), and the FED total she entered when she
// approved today's feeding video (one combined weight) -- and fed minus packed.
//
// Grain: (park_id, shed_id, partition_key, session_no, workflow) for ONE feed day on every side --
// the natural key both feed_packing_completions and feed_distribution_completions are unique on.
//
// projection-review: membership=pen-session keys from the live sheets (feed_direction_issue_rows of issued/amended/locked issues for $3) UNION that day's feed_packing_completions UNION that day's feed_distribution_completions, so a pen-session packed or fed against a sheet re-issued since still lists; group_key=(park_id, shed_id, partition_key, session_no, workflow) on every side; join_cardinality=keys LEFT JOIN plan 1:0..1 (GROUP BY on exactly the key, the N ration/item cells SUMMED first), keys LEFT JOIN packed 1:0..1 and keys LEFT JOIN fed 1:0..1 (each unique on that key plus target_date by its natural_uq), readings SUMMED per completion through a LATERAL keyed on its PK prefix, locations 1:0..1 by primary key; pagination=none, one feed day bounded by the parks' pens x sessions -- physical infrastructure, never herd size -- and the day totals are window sums over exactly the compared rows returned; scope=tenant_id on every table plus the caller's authorized park set
//
// THE BLIND-ENTRY BOUNDARY LIVES HERE, NOT IN A RENDERER. Planned and fed are emitted only when the
// pen-session's FEEDING completion is 'completed'; packed additionally needs its PACKING completion
// 'completed' (domain.FeedCheckFiguresVisible / domain.PackedVisible). Before the feeding verdict the
// packed total is the feeding verifier's answer, so no client, export or future caller of this read
// may receive it.
//
// The day totals range over the COMPARED rows only (packed and fed both visible), so planned, packed
// and fed are always summed over the same pen-sessions and their difference means something.
//
// scale-guard:ignore: 5k-50k-envelope -- ONE feed day, bounded by pens x sessions, over the indexed
// (tenant, feed_day) / (tenant, target_date) columns; binds are cast, columns stay bare.
const packingVerificationLogSQL = `
WITH plan AS (
    SELECT i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow,
           SUM(r.quantity_kg)                    AS planned_kg,
           MAX(COALESCE(r.partition_label, ''))  AS partition_label,
           MAX(r.session_label)                  AS session_label,
           MAX(r.park_label)                     AS park_label,
           MAX(r.shed_label)                     AS shed_label
    FROM feed_direction_issues i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    WHERE i.tenant_id = $1
      AND ($2::uuid[] IS NULL OR i.park_id = ANY ($2::uuid[]))
      AND i.feed_day = $3::date
      AND i.state IN ('issued', 'amended', 'locked')
      AND i.workflow IN ('normal', 'experiment')
    GROUP BY i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow
),
packed AS (
    SELECT c.park_id, c.shed_id, c.partition_key, c.session_no, c.workflow, c.status,
           COALESCE(c.partition_label, '') AS partition_label,
           rd.packed_kg
    FROM feed_packing_completions c
    LEFT JOIN LATERAL (
        SELECT SUM(q.entered_kg) AS packed_kg
        FROM feed_packing_verified_quantities q
        WHERE q.tenant_id = $1 AND q.completion_id = c.completion_id
    ) rd ON true
    WHERE c.tenant_id = $1
      AND ($2::uuid[] IS NULL OR c.park_id = ANY ($2::uuid[]))
      AND c.target_date = $3::date
),
fed AS (
    SELECT d.park_id, d.shed_id, d.partition_key, d.session_no, d.workflow, d.status,
           COALESCE(d.partition_label, '') AS partition_label,
           d.verified_feed_kg
    FROM feed_distribution_completions d
    WHERE d.tenant_id = $1
      AND ($2::uuid[] IS NULL OR d.park_id = ANY ($2::uuid[]))
      AND d.target_date = $3::date
),
keys AS (
    SELECT park_id, shed_id, partition_key, session_no, workflow FROM plan
    UNION
    SELECT park_id, shed_id, partition_key, session_no, workflow FROM packed
    UNION
    SELECT park_id, shed_id, partition_key, session_no, workflow FROM fed
),
joined AS (
    SELECT k.park_id, k.shed_id, k.partition_key, k.session_no, k.workflow,
           -- The farm's CODE (CBE, CPT): the one spelling every table on the page uses, and the park
           -- ORDER (CBE before CPT; the full names sort Channapatna first).
           COALESCE(NULLIF(lp.location_code, ''), lp.name, pl.park_label, '')                AS park_label,
           COALESCE(ls.name, pl.shed_label, '')                                              AS shed_label,
           COALESCE(NULLIF(pl.partition_label, ''), NULLIF(p.partition_label, ''), f.partition_label, '') AS partition_label,
           COALESCE(pl.session_label, '')                                                    AS session_label,
           COALESCE(p.status, '')                                                            AS packing_status,
           COALESCE(f.status, '')                                                            AS feeding_status,
           -- Blind-entry boundary: see the doc comment.
           CASE WHEN f.status = 'completed' THEN pl.planned_kg END                           AS planned_kg,
           CASE WHEN f.status = 'completed' AND p.status = 'completed' THEN p.packed_kg END  AS packed_kg,
           CASE WHEN f.status = 'completed' THEN f.verified_feed_kg END                      AS fed_kg
    FROM keys k
    LEFT JOIN plan pl
      ON pl.park_id = k.park_id AND pl.shed_id = k.shed_id AND pl.partition_key = k.partition_key
     AND pl.session_no = k.session_no AND pl.workflow = k.workflow
    LEFT JOIN packed p
      ON p.park_id = k.park_id AND p.shed_id = k.shed_id AND p.partition_key = k.partition_key
     AND p.session_no = k.session_no AND p.workflow = k.workflow
    LEFT JOIN fed f
      ON f.park_id = k.park_id AND f.shed_id = k.shed_id AND f.partition_key = k.partition_key
     AND f.session_no = k.session_no AND f.workflow = k.workflow
    LEFT JOIN locations lp ON lp.tenant_id = $1 AND lp.location_id = k.park_id
    LEFT JOIN locations ls ON ls.tenant_id = $1 AND ls.location_id = k.shed_id
)
SELECT park_id::text, park_label, shed_id::text, shed_label, partition_label,
       session_no, session_label, workflow, packing_status, feeding_status,
       COALESCE(planned_kg::text, ''),
       COALESCE(packed_kg::text, ''),
       COALESCE(fed_kg::text, ''),
       CASE WHEN packed_kg IS NOT NULL AND fed_kg IS NOT NULL THEN (fed_kg - packed_kg)::text ELSE '' END,
       COALESCE((SUM(planned_kg) FILTER (WHERE packed_kg IS NOT NULL AND fed_kg IS NOT NULL) OVER ())::text, ''),
       COALESCE((SUM(packed_kg) FILTER (WHERE packed_kg IS NOT NULL AND fed_kg IS NOT NULL) OVER ())::text, ''),
       COALESCE((SUM(fed_kg) FILTER (WHERE packed_kg IS NOT NULL AND fed_kg IS NOT NULL) OVER ())::text, ''),
       COALESCE((SUM(fed_kg - packed_kg) FILTER (WHERE packed_kg IS NOT NULL AND fed_kg IS NOT NULL) OVER ())::text, '')
FROM joined
ORDER BY park_label, park_id, shed_label, shed_id, partition_label, partition_key, session_no, workflow`

// PackingVerificationLog serves the FEED VERIFICATION panel: one feed day's pen-sessions with
// planned, packed (yesterday) and fed (today) totals. See packingVerificationLogSQL.
func (r *Repository) PackingVerificationLog(ctx context.Context, tenantID string, parkIDs []uuid.UUID, feedDay time.Time) (domain.PackingVerificationLog, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var parkArg []uuid.UUID
	if len(parkIDs) > 0 {
		parkArg = parkIDs
	}
	day := feedDay.Format("2006-01-02")
	out := domain.PackingVerificationLog{
		FeedDay:    day,
		PackingDay: feedDay.AddDate(0, 0, -1).Format("2006-01-02"),
		Rows:       []domain.FeedCheckRow{},
	}

	bound := sqlbind.MustBind(packingVerificationLogSQL, tenantID, parkArg, day)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.PackingVerificationLog{}, fmt.Errorf("feed verification log: %w", err)
	}
	defer rows.Close()

	var dayPlanned, dayPacked, dayFed, dayDiff string
	for rows.Next() {
		var (
			row                    domain.FeedCheckRow
			rawPacking, rawFeeding string
		)
		if err := rows.Scan(
			&row.ParkID, &row.ParkLabel, &row.ShedID, &row.ShedLabel, &row.PartitionLabel,
			&row.SessionNo, &row.SessionLabel, &row.Workflow, &rawPacking, &rawFeeding,
			&row.PlannedKg, &row.PackedKg, &row.FedKg, &row.DifferenceKg,
			&dayPlanned, &dayPacked, &dayFed, &dayDiff,
		); err != nil {
			return domain.PackingVerificationLog{}, fmt.Errorf("feed verification log scan: %w", err)
		}
		row.PackingStatus = domain.FeedCheckStatus(rawPacking)
		row.FeedingStatus = domain.FeedCheckStatus(rawFeeding)
		// The SQL already withholds these; this is the second lock on the same door.
		if !domain.FeedCheckFiguresVisible(row.FeedingStatus) {
			row.PlannedKg, row.FedKg = "", ""
		}
		if !domain.PackedVisible(row.PackingStatus, row.FeedingStatus) {
			row.PackedKg = ""
		}
		if row.PackedKg == "" || row.FedKg == "" {
			row.DifferenceKg = ""
		}
		// Canonical composition, never hand-rolled (operational-location rule).
		row.OperationalLocationDisplay = oploc.OperationalLocation{ShedName: row.ShedLabel, PartitionLabel: row.PartitionLabel}.Display()
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return domain.PackingVerificationLog{}, fmt.Errorf("feed verification log rows: %w", err)
	}
	out.Totals = domain.CountFeedCheckRows(out.Rows, dayPlanned, dayPacked, dayFed, dayDiff)
	return out, nil
}
