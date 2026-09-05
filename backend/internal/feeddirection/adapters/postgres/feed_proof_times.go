package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// feedProofTimesSQL reads one park's whole feed day: every pen-session the live sheet planned, with
// the upload instant of each of the three captures it owes.
//
// projection-review:
//
//	membership   = feed_direction_issue_rows for the LIVE issue of (tenant, park, feed_day), which is
//	               the authored expected set. NOT the completions table -- a pen nobody fed has no
//	               completion row and must still appear, as a red line.
//	producer key = feed_direction_issue_rows is unique on (tenant_id, feed_direction_issue_id,
//	               shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key) --
//	               MANY rows per pen-session (one per feed ITEM). The DISTINCT in `planned` collapses
//	               it to exactly (shed_id, partition_key, session_no, workflow, partition_label,
//	               session_label), which IS the pen-session grain this report is at.
//	consumer key = feed_distribution_completions is unique on (tenant_id, park_id, shed_id,
//	               partition_key, session_no, target_date, workflow) --
//	               feed_distribution_completions_natural_uq, migration 000137. The join names
//	               shed_id, partition_key, session_no, workflow and pins target_date = the feed day,
//	               so every column of that unique key is bound.
//	multiplicity = 1:0..1 on the completion (its natural key is fully bound above); 1:0..1 on each
//	               of the three proof_artifacts joins (proof_id is that table's PRIMARY KEY, and the
//	               refs are compared to it one at a time); 1:0..1 on locations (location_id is its
//	               key). No join can fan a pen-session into two rows, so no COUNT or time can be
//	               duplicated.
//	group_by     = none. This read has no aggregate at all: it emits one row per pen-session and
//	               the footer count is computed in Go over the same rows the table prints, so the
//	               count and the table can never disagree.
//	pagination   = none. Bounded by the park's own pen catalog x sessions (physical infrastructure,
//	               ~40 rows), never by herd size, and a report of "who is missing" must never be
//	               clipped by a page window.
//	scope        = tenant_id bound on the issue, which every other table is reached through. Parks
//	               are NOT filtered: the report is posted per park, and reading every park's live
//	               issue in one bounded pass is one round trip instead of one per park.
//
// THE PARK IS NAMED FROM `locations`, NOT from the sheet's park_label. The sheet stores the farm
// CODE ("CBE"), because that is what the legacy feed workbook keys on; a director reading Slack is
// owed "Coimbatore". park_label remains the fallback for a park whose location row is missing a
// name, so the message degrades to the code rather than to a blank heading.
//
// The session_label / partition_label carried out of `planned` are picked with min() so
// the grouping cannot split one pen-session into two rows over a cosmetic difference between two of
// its feed-item cells; the grain columns are the group, and these are descriptive passengers of it.
//
// THE PEN JOIN IS partition_key ON BOTH SIDES, which is the same GENERATED expression on both tables
// (migrations 000135 and 000137 declare it character for character: NULL/blank/whitespace to 'whole',
// otherwise lower(btrim(label))). Matching on the raw label instead would let "Part 3" and "part 3"
// read as different pens and file a fed pen as missing.
//
// scale-guard:ignore: bounded read of ONE park-day's planned pen-sessions, entered through
// feed_direction_issues_live_uidx (tenant_id, park_id, feed_day, workflow) and
// feed_direction_issue_rows_serve_idx, then joined to the completion on its own natural unique key.
// Bounded by the park's pen catalog x sessions, never by herd size; binds are cast and indexed
// columns stay bare.
const feedProofTimesSQL = `
WITH live_issue AS (
  SELECT feed_direction_issue_id, park_id, workflow
  FROM feed_direction_issues
  WHERE tenant_id = $1::uuid
    AND feed_day = $2::date
    AND state IN ('issued', 'amended', 'locked')
),
planned AS (
  SELECT
    i.park_id,
    r.shed_id,
    r.partition_key,
    r.session_no,
    i.workflow,
    min(r.park_label)                    AS park_label,
    min(coalesce(r.partition_label, '')) AS partition_label,
    min(coalesce(r.session_label, ''))   AS session_label
  FROM feed_direction_issue_rows r
  JOIN live_issue i ON i.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid
  GROUP BY i.park_id, r.shed_id, r.partition_key, r.session_no, i.workflow
)
SELECT
  p.park_id::text,
  coalesce(nullif(btrim(park.name), ''), p.park_label),
  p.shed_id::text,
  coalesce(l.name, ''),
  p.partition_label,
  p.session_no,
  p.session_label,
  p.workflow,
  coalesce(c.status, ''),
  weight.uploaded_at,
  distribution.uploaded_at,
  water.uploaded_at
FROM planned p
LEFT JOIN locations park
  ON park.tenant_id = $1::uuid AND park.location_id = p.park_id
LEFT JOIN locations l
  ON l.tenant_id = $1::uuid AND l.location_id = p.shed_id
LEFT JOIN feed_distribution_completions c
  ON c.tenant_id = $1::uuid
 AND c.park_id = p.park_id
 AND c.target_date = $2::date
 AND c.shed_id = p.shed_id
 AND c.partition_key = p.partition_key
 AND c.session_no = p.session_no
 AND c.workflow = p.workflow
LEFT JOIN proof_artifacts weight
  ON weight.tenant_id = $1::uuid
 AND weight.proof_id::text = c.feed_weight_proof_ref
 AND weight.upload_state = 'completed'
LEFT JOIN proof_artifacts distribution
  ON distribution.tenant_id = $1::uuid
 AND distribution.proof_id::text = c.distribution_proof_ref
 AND distribution.upload_state = 'completed'
LEFT JOIN proof_artifacts water
  ON water.tenant_id = $1::uuid
 AND water.proof_id::text = c.water_proof_ref
 AND water.upload_state = 'completed'`

// FeedProofTimesByPark returns one report per park that has a live feed sheet for feedDay, each
// carrying every pen-session that sheet planned and the upload instant of its three captures.
//
// A capture that never arrived, was cleared for a re-shoot, or whose upload has not finished landing
// comes back as a zero time -- the report renders all three the same way, as a missing capture,
// because from the farm's side they are the same fact: there is nothing to watch. An upload still in
// flight is deliberately NOT reported as done; upload_state='completed' is the moment the artifact
// exists, and it is the moment the maintainer named as the time.
//
// Parks come back in a stable name order so two consecutive posts of the same day read the same way.
func (r *Repository) FeedProofTimesByPark(ctx context.Context, tenantID string, feedDay time.Time) ([]domain.FeedProofTimesReport, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	businessDate := feedDay.Format("2006-01-02")
	rows, err := r.pool.Query(ctx, feedProofTimesSQL, tenantID, businessDate)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: feed proof times: %w", err)
	}
	defer rows.Close()

	byPark := map[string]*domain.FeedProofTimesReport{}
	for rows.Next() {
		var parkID, parkName string
		var row domain.PenSessionProofTimes
		var weightAt, distributionAt, waterAt *time.Time
		if err := rows.Scan(
			&parkID,
			&parkName,
			&row.ShedID,
			&row.ShedName,
			&row.PartitionLabel,
			&row.SessionNo,
			&row.SessionLabel,
			&row.Workflow,
			&row.Status,
			&weightAt,
			&distributionAt,
			&waterAt,
		); err != nil {
			return nil, fmt.Errorf("feeddirection: scan feed proof times: %w", err)
		}
		if weightAt != nil {
			row.Weight = domain.FeedProofCapture{UploadedAt: *weightAt}
		}
		if distributionAt != nil {
			row.Distribution = domain.FeedProofCapture{UploadedAt: *distributionAt}
		}
		if waterAt != nil {
			row.Water = domain.FeedProofCapture{UploadedAt: *waterAt}
		}
		report, ok := byPark[parkID]
		if !ok {
			report = &domain.FeedProofTimesReport{ParkID: parkID, ParkName: parkName, FeedDay: businessDate}
			byPark[parkID] = report
		}
		report.Rows = append(report.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feeddirection: feed proof times rows: %w", err)
	}

	out := make([]domain.FeedProofTimesReport, 0, len(byPark))
	for _, report := range byPark {
		out = append(out, *report)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ParkName != out[j].ParkName {
			return out[i].ParkName < out[j].ParkName
		}
		return out[i].ParkID < out[j].ParkID
	})
	return out, nil
}
