package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// projection-review: membership=toxin_test_tasks latest round per feed_purchase_id; group_key=(tenant_id, feed_purchase_id); join_cardinality=1:1 (round_uq makes DISTINCT ON exact; workforce_members LEFT JOIN is unique per tenant user_id); pagination=keyset on (purchase_date DESC, feed_purchase_id DESC), summaries are whole-window aggregates computed before paging; scope=tenant_id on every read, 30-day window on the analytics, unwindowed on the loads table.
//
// The /feed/toxin report.
//
// GRAIN. Producer unique key: toxin_test_tasks_round_uq = (tenant_id, feed_purchase_id,
// round_no). Consumer match/group key: (tenant_id, feed_purchase_id). The `latest` CTE
// collapses the many side with DISTINCT ON (feed_purchase_id) ORDER BY round_no DESC,
// which is an EXACT selection rather than a ranking guess: round_no is unique per
// (tenant, purchase) by that constraint, so exactly one row survives per load and no tie
// is possible. Every figure on the page — rows, KPIs, mix, weekly series, supplier
// rollup — ranges over that same one-row-per-load set, so the numerator and denominator
// of every ratio share a key set.
//
// MULTIPLICITY. workforce_members joins ON (tenant_id, user_id); user_id is unique per
// tenant there, and the join is LEFT, so it cannot add or drop a load row. No other table
// is joined. There is no fan-out anywhere in this file.
//
// WINDOW. The KPI/series/vendor reads are bounded to WindowDays. The LOADS TABLE is
// deliberately NOT windowed — a load waiting since before the window is precisely the row
// a reader needs, and windowing it away would make the screening look finished.
//
// SCALE. Bounded by the tenant's feed-purchase count (~46 loads/month observed, so low
// hundreds per year). DISTINCT ON walks toxin_test_tasks_report_idx
// (tenant_id, feed_purchase_id, round_no DESC) as an index skip-scan; the four reads are
// fixed in number and never issued inside a loop.

// latestRoundCTE is the ONE definition of "this load's current state". Every read below
// starts from it, so a chip count and the rows under that chip cannot disagree.
const latestRoundCTE = `
WITH latest AS (
    SELECT DISTINCT ON (t.feed_purchase_id)
           t.feed_purchase_id, t.task_id, t.round_no, t.origin,
           t.farm_label, t.feed_item_key, t.feed_item_label, t.vendor, t.batch_no,
           t.purchase_date, t.quantity_kg, t.status, COALESCE(t.outcome, '') AS outcome,
           t.submitted_by, t.submitted_at, t.created_at
      FROM public.toxin_test_tasks t
     WHERE t.tenant_id = $1
     ORDER BY t.feed_purchase_id, t.round_no DESC
)`

// reportBucketSQL mirrors domain.ReportBucketFor. The two are kept in lockstep by
// TestReportBucketSQLMatchesTheDomainPartition, which drives every status/outcome pair
// through both and fails on the first disagreement — a SQL chip that disagrees with the
// domain would badge a count the page cannot list.
func reportBucketSQL(alias string) string {
	return fmt.Sprintf(`CASE
        WHEN %[1]s.status = '%[2]s' THEN '%[6]s'
        WHEN %[1]s.status = '%[3]s' THEN '%[7]s'
        WHEN %[1]s.status = '%[4]s' AND %[1]s.outcome = '%[9]s' THEN '%[8]s'
        ELSE '%[10]s'
    END`, alias,
		domain.StatusInProgress, domain.StatusPendingReview, domain.StatusAccepted, domain.StatusCancelled,
		domain.ReportFilterWaiting, domain.ReportFilterReview, domain.ReportFilterCleared,
		domain.OutcomeNegative, domain.ReportFilterFlagged)
}

// LoadReport serves the whole /feed/toxin screen in four bounded reads.
func (r *Repository) LoadReport(ctx context.Context, p ports.ReportParams) (ports.ReportPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	filter := domain.ReportFilterKeyOrDefault(p.Filter)
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	window := p.WindowDays
	if window <= 0 {
		window = 30
	}

	page := ports.ReportPage{FilterCounts: map[string]int{}}
	var err error
	if page.Loads, page.NextCursor, err = r.reportLoads(ctx, p.TenantID, filter, limit, p.Cursor); err != nil {
		return ports.ReportPage{}, err
	}
	if page.Summary, page.Mix, page.FilterCounts, err = r.reportSummary(ctx, p.TenantID, window); err != nil {
		return ports.ReportPage{}, err
	}
	if page.Weeks, err = r.reportWeeks(ctx, p.TenantID, window); err != nil {
		return ports.ReportPage{}, err
	}
	if page.Vendors, err = r.reportVendors(ctx, p.TenantID, window); err != nil {
		return ports.ReportPage{}, err
	}
	return page, nil
}

func (r *Repository) reportLoads(ctx context.Context, tenantID, filter string, limit int, cursor string) ([]ports.ReportLoad, string, error) {
	args := []any{tenantID}
	where := "TRUE"
	if filter != domain.ReportFilterAll {
		args = append(args, filter)
		where = fmt.Sprintf("%s = $%d", reportBucketSQL("l"), len(args))
	}
	if cursor != "" {
		purchaseDate, purchaseID, err := decodeReportCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, purchaseDate, purchaseID)
		where += fmt.Sprintf(" AND (l.purchase_date, l.feed_purchase_id) < ($%d::date, $%d::uuid)", len(args)-1, len(args))
	}

	query := fmt.Sprintf(`%s
SELECT l.feed_purchase_id::text, l.task_id::text, l.round_no, l.origin,
       l.farm_label, l.feed_item_label, l.vendor, l.batch_no,
       to_char(l.purchase_date, 'YYYY-MM-DD'), l.quantity_kg, l.status, l.outcome,
       COALESCE(NULLIF(BTRIM(COALESCE(m.display_name,
           BTRIM(COALESCE(m.first_name, '') || ' ' || COALESCE(m.last_name, '')))), ''), '') AS tested_by_name,
       COALESCE(to_char(l.submitted_at AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD"T"HH24:MI:SS'), ''),
       COALESCE(GREATEST(0, EXTRACT(EPOCH FROM (l.submitted_at - l.created_at))::bigint / 60), 0) AS turnaround_minutes,
       CASE WHEN l.submitted_at IS NULL
            THEN GREATEST(0, (now() AT TIME ZONE 'Asia/Kolkata')::date - l.purchase_date)
            ELSE 0 END AS waiting_days
  FROM latest l
  LEFT JOIN public.workforce_members m
         ON m.tenant_id = $1 AND m.user_id = l.submitted_by
 WHERE %s
 ORDER BY l.purchase_date DESC, l.feed_purchase_id DESC
 LIMIT %d`, latestRoundCTE, where, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("toxin: report loads: %w", err)
	}
	defer rows.Close()

	loads := make([]ports.ReportLoad, 0, limit)
	for rows.Next() {
		var l ports.ReportLoad
		var turnaround, waiting int64
		if err := rows.Scan(&l.FeedPurchaseID, &l.TaskID, &l.RoundNo, &l.Origin,
			&l.FarmLabel, &l.FeedItemLabel, &l.Vendor, &l.BatchNo,
			&l.PurchaseDate, &l.QuantityKg, &l.Status, &l.Outcome,
			&l.TestedByName, &l.SubmittedAt, &turnaround, &waiting); err != nil {
			return nil, "", fmt.Errorf("toxin: report loads scan: %w", err)
		}
		l.TurnaroundMinutes = int(turnaround)
		l.WaitingDays = int(waiting)
		loads = append(loads, l)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("toxin: report loads rows: %w", err)
	}

	next := ""
	if len(loads) > limit {
		last := loads[limit-1]
		next = encodeReportCursor(last.PurchaseDate, last.FeedPurchaseID)
		loads = loads[:limit]
	}
	return loads, next, nil
}

// projection-review: membership=toxin_test_tasks latest round per feed_purchase_id; group_key=(tenant_id, feed_purchase_id); join_cardinality=1:1 via DISTINCT ON over the round_uq key, no fan-out; pagination=whole-window aggregate, computed before and independently of row paging; scope=tenant_id bound on every read.
// reportSummary returns the KPI strip, the outcome mix and the whole-tenant chip counts in
// ONE pass. The chip counts are deliberately NOT windowed (a chip badge must match the
// rows its chip lists, and the table is unwindowed), while the KPIs and mix are.
func (r *Repository) reportSummary(ctx context.Context, tenantID string, window int) (ports.ReportSummary, ports.ReportOutcomeMix, map[string]int, error) {
	query := fmt.Sprintf(`%s, windowed AS (
    SELECT * FROM latest
     WHERE purchase_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date - ($2::int - 1)
)
SELECT
    (SELECT count(*) FROM windowed),
    (SELECT count(*) FROM windowed WHERE submitted_at IS NOT NULL),
    (SELECT count(*) FROM windowed WHERE %[11]s = '%[3]s'),
    (SELECT count(*) FROM windowed WHERE status = '%[4]s'),
    (SELECT COALESCE(max((now() AT TIME ZONE 'Asia/Kolkata')::date - purchase_date), 0)
       FROM latest WHERE submitted_at IS NULL),
    (SELECT COALESCE(feed_item_label || ' · ' || farm_label, '') FROM latest
      WHERE submitted_at IS NULL ORDER BY purchase_date ASC, feed_purchase_id ASC LIMIT 1),
    (SELECT count(DISTINCT feed_item_key) FROM windowed),
    (SELECT count(DISTINCT farm_label) FROM windowed),
    (SELECT count(*) FROM windowed WHERE outcome = '%[5]s'),
    (SELECT count(*) FROM windowed WHERE outcome = '%[6]s'),
    (SELECT count(*) FROM windowed WHERE outcome = '%[7]s'),
    (SELECT count(*) FROM windowed WHERE outcome = ''),
    (SELECT count(*) FROM latest),
    (SELECT count(*) FROM latest WHERE %[2]s = '%[8]s'),
    (SELECT count(*) FROM latest WHERE %[2]s = '%[9]s'),
    (SELECT count(*) FROM latest WHERE %[2]s = '%[10]s'),
    (SELECT count(*) FROM latest WHERE %[2]s = '%[3]s')`,
		latestRoundCTE, reportBucketSQL("latest"),
		domain.ReportFilterFlagged, domain.StatusInProgress,
		domain.OutcomeNegative, domain.OutcomePositive, domain.OutcomeInvalid,
		domain.ReportFilterWaiting, domain.ReportFilterReview, domain.ReportFilterCleared,
		// The bucket CASE must be aliased to the table each sub-select actually reads:
		// `windowed` inside the 30-day sub-selects, `latest` for the whole-tenant chip
		// counts. Using one alias for both makes Postgres refuse the query outright
		// ("missing FROM-clause entry for table latest"), which is how this was caught.
		reportBucketSQL("windowed"))

	var s ports.ReportSummary
	var mix ports.ReportOutcomeMix
	counts := map[string]int{}
	var all, waiting, review, cleared, flagged int
	if err := r.pool.QueryRow(ctx, query, tenantID, window).Scan(
		&s.LoadsReceived, &s.LoadsTested, &s.NeedsAttention, &s.Waiting,
		&s.OldestWaitingDays, &s.OldestWaitingLabel, &s.FeedTypes, &s.Parks,
		&mix.Negative, &mix.Positive, &mix.Invalid, &mix.Untested,
		&all, &waiting, &review, &cleared, &flagged,
	); err != nil {
		return ports.ReportSummary{}, ports.ReportOutcomeMix{}, nil, fmt.Errorf("toxin: report summary: %w", err)
	}
	counts[domain.ReportFilterAll] = all
	counts[domain.ReportFilterWaiting] = waiting
	counts[domain.ReportFilterReview] = review
	counts[domain.ReportFilterCleared] = cleared
	counts[domain.ReportFilterFlagged] = flagged
	return s, mix, counts, nil
}

// projection-review: membership=toxin_test_tasks latest round per feed_purchase_id; group_key=(tenant_id, feed_purchase_id); join_cardinality=1:1 via DISTINCT ON over the round_uq key, no fan-out; pagination=whole-window aggregate, one row per ISO week, never page-local; scope=tenant_id bound on every read.
func (r *Repository) reportWeeks(ctx context.Context, tenantID string, window int) ([]ports.ReportWeek, error) {
	query := fmt.Sprintf(`%s
SELECT to_char(date_trunc('week', purchase_date)::date, 'YYYY-MM-DD') AS week_start,
       count(*) AS received,
       count(*) FILTER (WHERE submitted_at IS NOT NULL) AS tested
  FROM latest
 WHERE purchase_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date - ($2::int - 1)
 GROUP BY 1
 ORDER BY 1`, latestRoundCTE)

	rows, err := r.pool.Query(ctx, query, tenantID, window)
	if err != nil {
		return nil, fmt.Errorf("toxin: report weeks: %w", err)
	}
	defer rows.Close()
	weeks := []ports.ReportWeek{}
	for rows.Next() {
		var w ports.ReportWeek
		if err := rows.Scan(&w.WeekStart, &w.Received, &w.Tested); err != nil {
			return nil, fmt.Errorf("toxin: report weeks scan: %w", err)
		}
		weeks = append(weeks, w)
	}
	return weeks, rows.Err()
}

// projection-review: membership=toxin_test_tasks latest round per feed_purchase_id; group_key=(tenant_id, feed_purchase_id); join_cardinality=1:1 via DISTINCT ON over the round_uq key, no fan-out; pagination=whole-window aggregate, ranked and capped at 5, never page-local; scope=tenant_id bound on every read.
// reportVendors ranks suppliers by the share of their loads that came back unusable —
// positive, or a void strip. A supplier with a single load is excluded from the ORDER: one
// bad load out of one is 100% and would top the list on no evidence.
func (r *Repository) reportVendors(ctx context.Context, tenantID string, window int) ([]ports.ReportVendor, error) {
	query := fmt.Sprintf(`%s
SELECT vendor,
       count(*) AS loads,
       count(*) FILTER (WHERE %s = '%s') AS flagged
  FROM latest
 WHERE purchase_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date - ($2::int - 1)
   AND BTRIM(vendor) <> ''
 GROUP BY vendor
 HAVING count(*) >= 2
 ORDER BY (count(*) FILTER (WHERE %s = '%s'))::numeric / count(*) DESC, count(*) DESC, vendor
 LIMIT 5`, latestRoundCTE,
		reportBucketSQL("latest"), domain.ReportFilterFlagged,
		reportBucketSQL("latest"), domain.ReportFilterFlagged)

	rows, err := r.pool.Query(ctx, query, tenantID, window)
	if err != nil {
		return nil, fmt.Errorf("toxin: report vendors: %w", err)
	}
	defer rows.Close()
	vendors := []ports.ReportVendor{}
	for rows.Next() {
		var v ports.ReportVendor
		if err := rows.Scan(&v.Vendor, &v.Loads, &v.Flagged); err != nil {
			return nil, fmt.Errorf("toxin: report vendors scan: %w", err)
		}
		vendors = append(vendors, v)
	}
	return vendors, rows.Err()
}

func encodeReportCursor(purchaseDate, purchaseID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(purchaseDate + "|" + purchaseID))
}

func decodeReportCursor(cursor string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("toxin: report cursor: %w", err)
	}
	date, id, ok := strings.Cut(string(raw), "|")
	if !ok || date == "" || id == "" {
		return "", "", fmt.Errorf("toxin: report cursor is malformed")
	}
	return date, id, nil
}
