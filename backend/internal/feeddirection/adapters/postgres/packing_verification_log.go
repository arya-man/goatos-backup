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

// packingVerificationLogSQL is the FEED VERIFICATION panel on /verify (maintainer decision
// 2026-09-28): one feed day, one row per (bag, feed item), where a bag is one park, pen, session and
// workflow of that day.
//
// projection-review: grain=(park_id, shed_id, partition_key, session_no, workflow, feed_item_key)
// for ONE feed day on every side.
// projection-review: membership=item keys from the live sheets (feed_direction_issue_rows of
// issued/amended/locked issues for $3) UNION reading keys from feed_packing_verified_quantities of
// that day's COMPLETED feed_packing_completions UNION an item-less key for a completion with neither
// (a bag packed against a sheet re-issued since, which must still list); group_key=the six columns
// above; join_cardinality=keys LEFT JOIN expected_items 1:0..1 (GROUP BY on exactly the key),
// keys LEFT JOIN readings 1:0..1 (verified quantities are unique per (completion, feed_item_key) and
// completions are unique per bag by feed_packing_completions_natural_uq), keys LEFT JOIN done 1:0..1
// (same natural key), keys LEFT JOIN bag_meta 1:0..1 (GROUP BY on the bag key), locations and
// workforce_members 1:0..1 by primary key; pagination=none, one feed day bounded by the parks' pens x
// sessions x items -- physical infrastructure, never herd size; the per-bag and whole-day kg totals
// are window SUMs over exactly the returned rows, so no total can describe a different set than the
// table; scope=tenant_id on every table plus the caller's authorized park set.
//
//	producer `expected_items` unique columns after GROUP BY: (park_id, shed_id, partition_key,
//	  session_no, workflow, feed_item_key) -- the ration-grain rows (one per breed/tag grain) are
//	  SUMMED before any join, the same summation the packer's worklist does.
//	producer `readings` unique columns: (completion_id, feed_item_key) = the table's PK, and
//	  completion_id is 1:1 with the bag key for one target_date.
//	consumer match columns: the six above on every item-level join, the first five on bag joins.
//
// THE BLIND-ENTRY BOUNDARY LIVES HERE, NOT IN A RENDERER. `planned_kg` is emitted only when the
// bag's completion is 'completed' (domain.PackingLogPlanVisible) -- an undecided or reworked bag
// returns NULL for every plan figure, so no client, export or future caller of this read can hand
// the verifier the answer to a bag she has not judged. Readings exist only for completed bags anyway
// (the approve carries the numbers), so the entered side needs no filter beyond the join.
//
// The planned figure for a decided bag is the one her reading was CHECKED AGAINST at approve time
// (feed_packing_verified_quantities.planned_kg, 2026-09-09), falling back to the sheet's sum. After an
// afternoon correction the live sheet can differ from what she judged; showing her own check keeps
// this panel agreeing with the verdict it reports.
//
// scale-guard:ignore: 5k-50k-envelope -- ONE feed day, bounded by pens x sessions x items, over the
// indexed (tenant, feed_day) / (tenant, target_date) columns; binds are cast, columns stay bare.
const packingVerificationLogSQL = `
WITH expected_items AS (
    SELECT i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow, r.feed_item_key,
           MAX(r.feed_item_label)                AS feed_item_label,
           SUM(r.quantity_kg)                    AS planned_kg,
           MIN(r.item_seq)                       AS item_seq,
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
    GROUP BY i.park_id, r.shed_id, r.partition_key, r.session_no, r.workflow, r.feed_item_key
    -- A feed the sheet authors at exactly 0 kg for this pen is not in the bag: listing it would name
    -- a feed nobody packed on an undecided bag and a "0 / -" row on a decided one. A BLOCKED cell
    -- (NULL sum -- no plan could be computed) is kept, because that is a real gap. A zero item the
    -- verifier nevertheless weighed comes back through the readings leg of keys.
    HAVING SUM(r.quantity_kg) IS DISTINCT FROM 0
),
bag_meta AS (
    SELECT park_id, shed_id, partition_key, session_no, workflow,
           MAX(partition_label) AS partition_label,
           MAX(session_label)   AS session_label,
           MAX(park_label)      AS park_label,
           MAX(shed_label)      AS shed_label
    FROM expected_items
    GROUP BY park_id, shed_id, partition_key, session_no, workflow
),
done AS (
    SELECT c.completion_id, c.park_id, c.shed_id, c.partition_key, c.session_no, c.workflow,
           COALESCE(c.partition_label, '') AS partition_label,
           c.status, c.verified_at, c.verified_by
    FROM feed_packing_completions c
    WHERE c.tenant_id = $1
      AND ($2::uuid[] IS NULL OR c.park_id = ANY ($2::uuid[]))
      AND c.target_date = $3::date
),
readings AS (
    SELECT d.park_id, d.shed_id, d.partition_key, d.session_no, d.workflow,
           q.feed_item_key, q.feed_item_label, q.entered_kg, q.planned_kg, q.variance_acknowledged
    FROM done d
    JOIN feed_packing_verified_quantities q
      ON q.tenant_id = $1
     AND q.completion_id = d.completion_id
    WHERE d.status = 'completed'
),
keys AS (
    SELECT park_id, shed_id, partition_key, session_no, workflow, feed_item_key FROM expected_items
    UNION
    SELECT park_id, shed_id, partition_key, session_no, workflow, feed_item_key FROM readings
    UNION
    SELECT d.park_id, d.shed_id, d.partition_key, d.session_no, d.workflow, ''
    FROM done d
    WHERE NOT EXISTS (
            SELECT 1 FROM bag_meta b
            WHERE b.park_id = d.park_id AND b.shed_id = d.shed_id AND b.partition_key = d.partition_key
              AND b.session_no = d.session_no AND b.workflow = d.workflow)
      AND NOT EXISTS (
            SELECT 1 FROM readings rd
            WHERE rd.park_id = d.park_id AND rd.shed_id = d.shed_id AND rd.partition_key = d.partition_key
              AND rd.session_no = d.session_no AND rd.workflow = d.workflow)
),
joined AS (
    SELECT k.park_id, k.shed_id, k.partition_key, k.session_no, k.workflow, k.feed_item_key,
           -- The farm's CODE (CBE, CPT): the one spelling every table on the page uses, and the park
           -- ORDER (CBE before CPT; the full names sort Channapatna first).
           COALESCE(NULLIF(lp.location_code, ''), lp.name, bm.park_label, '') AS park_label,
           COALESCE(ls.name, bm.shed_label, '')                              AS shed_label,
           COALESCE(NULLIF(bm.partition_label, ''), d.partition_label, '')   AS partition_label,
           COALESCE(bm.session_label, '')                                    AS session_label,
           COALESCE(d.status, '')                                            AS raw_status,
           d.verified_at,
           COALESCE(wv.display_name, '')                                     AS verified_by_name,
           COALESCE(e.feed_item_label, rd.feed_item_label, '')               AS feed_item_label,
           e.item_seq,
           -- Blind-entry boundary: see the doc comment. NULL unless the bag's verdict stands.
           CASE WHEN d.status = 'completed' AND k.feed_item_key <> ''
                THEN COALESCE(rd.planned_kg, e.planned_kg) END               AS planned_kg,
           rd.entered_kg,
           COALESCE(rd.variance_acknowledged, false)                         AS variance_acknowledged
    FROM keys k
    LEFT JOIN expected_items e
      ON e.park_id = k.park_id AND e.shed_id = k.shed_id AND e.partition_key = k.partition_key
     AND e.session_no = k.session_no AND e.workflow = k.workflow AND e.feed_item_key = k.feed_item_key
    LEFT JOIN readings rd
      ON rd.park_id = k.park_id AND rd.shed_id = k.shed_id AND rd.partition_key = k.partition_key
     AND rd.session_no = k.session_no AND rd.workflow = k.workflow AND rd.feed_item_key = k.feed_item_key
    LEFT JOIN done d
      ON d.park_id = k.park_id AND d.shed_id = k.shed_id AND d.partition_key = k.partition_key
     AND d.session_no = k.session_no AND d.workflow = k.workflow
    LEFT JOIN bag_meta bm
      ON bm.park_id = k.park_id AND bm.shed_id = k.shed_id AND bm.partition_key = k.partition_key
     AND bm.session_no = k.session_no AND bm.workflow = k.workflow
    LEFT JOIN locations lp ON lp.tenant_id = $1 AND lp.location_id = k.park_id
    LEFT JOIN locations ls ON ls.tenant_id = $1 AND ls.location_id = k.shed_id
    LEFT JOIN workforce_members wv ON wv.tenant_id = $1 AND wv.user_id = d.verified_by
)
SELECT park_id::text, park_label, shed_id::text, shed_label, partition_label, partition_key,
       session_no, session_label, workflow, raw_status, verified_at, verified_by_name,
       feed_item_key, feed_item_label,
       COALESCE(planned_kg::text, ''),
       COALESCE(entered_kg::text, ''),
       CASE WHEN planned_kg IS NOT NULL AND entered_kg IS NOT NULL
            THEN (entered_kg - planned_kg)::text ELSE '' END,
       variance_acknowledged,
       COALESCE((SUM(planned_kg) OVER (PARTITION BY park_id, shed_id, partition_key, session_no, workflow))::text, ''),
       COALESCE((SUM(entered_kg) OVER (PARTITION BY park_id, shed_id, partition_key, session_no, workflow))::text, ''),
       COALESCE((SUM(planned_kg) OVER ())::text, ''),
       COALESCE((SUM(entered_kg) OVER ())::text, '')
FROM joined
-- Labels first for reading order, then the identity columns, so one bag's rows are always
-- contiguous even where two pens share a label -- the grouping loop below relies on it.
ORDER BY park_label, park_id, shed_label, shed_id, partition_label, partition_key, session_no, workflow,
         item_seq NULLS LAST, feed_item_label, feed_item_key`

// PackingVerificationLog serves the FEED VERIFICATION panel: one feed day's packed bags, each feed
// item's plan beside the verifier's reading (plan only once her verdict stands). See
// packingVerificationLogSQL.
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
		Bags:       []domain.PackingLogBag{},
	}

	bound := sqlbind.MustBind(packingVerificationLogSQL, tenantID, parkArg, day)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.PackingVerificationLog{}, fmt.Errorf("feed packing verification log: %w", err)
	}
	defer rows.Close()

	type bagKey struct {
		park, shed, partition string
		session               int
		workflow              string
	}
	var current *bagKey
	for rows.Next() {
		var (
			parkID, parkLabel, shedID, shedLabel, partitionLabel, partitionKey string
			sessionNo                                                          int
			sessionLabel, workflow, rawStatus                                  string
			verifiedAt                                                         *time.Time
			verifiedByName, itemKey, itemLabel                                 string
			planned, entered, difference                                       string
			varianceAck                                                        bool
			bagPlanned, bagEntered, dayPlanned, dayEntered                     string
		)
		if err := rows.Scan(
			&parkID, &parkLabel, &shedID, &shedLabel, &partitionLabel, &partitionKey,
			&sessionNo, &sessionLabel, &workflow, &rawStatus, &verifiedAt, &verifiedByName,
			&itemKey, &itemLabel, &planned, &entered, &difference, &varianceAck,
			&bagPlanned, &bagEntered, &dayPlanned, &dayEntered,
		); err != nil {
			return domain.PackingVerificationLog{}, fmt.Errorf("feed packing verification log scan: %w", err)
		}
		out.Totals.PlannedKg, out.Totals.EnteredKg = dayPlanned, dayEntered

		key := bagKey{parkID, shedID, partitionKey, sessionNo, workflow}
		if current == nil || *current != key {
			status := domain.PackingLogStatus(rawStatus)
			bag := domain.PackingLogBag{
				ParkID: parkID, ParkLabel: parkLabel,
				ShedID: shedID, ShedLabel: shedLabel, PartitionLabel: partitionLabel,
				// Canonical composition, never hand-rolled (operational-location rule).
				OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedLabel, PartitionLabel: partitionLabel}.Display(),
				SessionNo:                  sessionNo, SessionLabel: sessionLabel, Workflow: workflow,
				Status: status, VerifiedByName: verifiedByName,
				PlannedTotalKg: bagPlanned, EnteredTotalKg: bagEntered,
				Items: []domain.PackingLogItem{},
			}
			if verifiedAt != nil {
				bag.VerifiedAt = verifiedAt.UTC().Format(time.RFC3339)
			}
			out.Bags = append(out.Bags, bag)
			k := key
			current = &k
		}
		if itemKey == "" {
			continue // a packed bag the current sheet no longer lists: the bag, with no items
		}
		bag := &out.Bags[len(out.Bags)-1]
		if !domain.PackingLogPlanVisible(bag.Status) {
			// The SQL already withholds these; this is the second lock on the same door.
			planned, difference = "", ""
		}
		bag.Items = append(bag.Items, domain.PackingLogItem{
			FeedItemKey: itemKey, FeedItemLabel: itemLabel,
			PlannedKg: planned, EnteredKg: entered, DifferenceKg: difference,
			VarianceAcknowledged: varianceAck,
		})
	}
	if err := rows.Err(); err != nil {
		return domain.PackingVerificationLog{}, fmt.Errorf("feed packing verification log rows: %w", err)
	}
	out.Totals = domain.CountPackingLogBags(out.Bags, out.Totals.PlannedKg, out.Totals.EnteredKg)
	return out, nil
}
