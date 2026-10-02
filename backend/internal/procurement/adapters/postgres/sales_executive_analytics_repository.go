package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// SALES EXECUTIVE ANALYTICS read (maintainer request 2026-10-02,
// docs/decisions/sales-executive-analytics.md).
//
// RECORDED CROSS-MODULE REPORTING READ, the buyer-analytics shape. Procurement owns
// procurement_vendors and joins OUT, read-only, to three facts that already record who did what:
// the register's own created_by/updated_by stamps, the audit_log rows the sales ledger writes
// inside every deal / payment / lead write transaction (and, from 000464, the vendor edit audit
// this package writes), and market_price_entries.recorded_by. Reporting grain only: nothing here
// gates a sale, edits a vendor, or feeds a write.

var _ ports.SalesExecutiveAnalyticsRepository = (*Repository)(nil)

// salesActivitiesSQL returns one row per ACTIVITY (domain.SalesActivityFact), every kind in one
// set-based statement.
//
// projection-review: membership per branch -- vendor_added: procurement_vendors at ROW grain
// (one vendor) with a known adder; vendor_edited: (actor, vendor, IST business day) after the
// audit edit rows and the register's last-edit stamp are UNIONed and GROUPED on exactly that key,
// so the newest save that also carries an audit row is counted once, never twice; market_call:
// (recorded_by, city_id, business_date) GROUPED, so the several prices typed for one city that
// day are one call; lead_call / sale_recorded / payment_recorded / deal_status: audit_log at ROW
// grain (one audited write). group_key=the branch key above on producer and consumer alike.
// join_cardinality: person is pre-aggregated to ONE row per user_id (an active row wins, else an
// agree-or-go-bare name across inactive rows), vendor / lead / deal joins are 1:1 on their PKs,
// and deal animals are pre-aggregated to one row per deal_id before they join -- no branch can
// fan out. pagination=none, whole-window read; the page's totals are computed in the domain over
// this whole set. scope=tenant_id on every branch.
//
// scale-guard:ignore: whole-window read over authored desk activity -- a few hundred vendors, a
// few dozen market entries a day and the audited sales writes -- bounded by the period the page
// offers (at most twice 90 days). The audit branches ride audit_log_tenant_action_idx
// (tenant_id, action, recorded_at); the register and the market entries are small authored
// tables read once per request in one statement, the same reasoning as the buyer read.
const salesActivitiesSQL = `
WITH person AS (
    SELECT wm.user_id,
           CASE WHEN count(*) FILTER (WHERE wm.status = 'active') = 1
                    THEN max(wm.display_name) FILTER (WHERE wm.status = 'active')
                WHEN count(DISTINCT wm.display_name) = 1
                    THEN min(wm.display_name)
           END AS name
    FROM public.workforce_members wm
    WHERE wm.tenant_id = $1 AND wm.user_id IS NOT NULL
    GROUP BY wm.user_id
),
vendor AS (
    SELECT v.vendor_id, v.business_name, v.record_type, v.created_by, v.created_at,
           v.updated_by, v.updated_at
    FROM public.procurement_vendors v
    WHERE v.tenant_id = $1
),
edit_raw AS (
    SELECT a.actor_id AS actor_id, a.resource_id AS vendor_id, a.recorded_at AS at
    FROM public.audit_log a
    WHERE a.tenant_id = $1
      AND a.action IN ('procurement.vendor.update', 'procurement.vendor.status')
      AND a.recorded_at >= $2
      AND a.actor_id IS NOT NULL
    UNION ALL
    -- The register's own last-edit stamp: the only record of an edit made before the vendor
    -- edit audit existed. A creation stamp is not an edit, hence the one-minute floor.
    SELECT v.updated_by, v.vendor_id, v.updated_at
    FROM vendor v
    WHERE v.updated_by IS NOT NULL
      AND v.updated_at >= $2
      AND v.updated_at > v.created_at + interval '1 minute'
),
edit_day AS (
    SELECT e.actor_id, e.vendor_id, (e.at AT TIME ZONE 'Asia/Kolkata')::date AS business_date,
           max(e.at) AS at
    FROM edit_raw e
    GROUP BY e.actor_id, e.vendor_id, (e.at AT TIME ZONE 'Asia/Kolkata')::date
),
market_call AS (
    SELECT m.recorded_by AS actor_id, m.city_id, m.business_date,
           max(m.recorded_at) AS at, max(m.city_name) AS city_name, count(*) AS answers
    FROM public.market_price_entries m
    WHERE m.tenant_id = $1 AND m.business_date >= $3::date AND m.recorded_by IS NOT NULL
    GROUP BY m.recorded_by, m.city_id, m.business_date
),
audited AS (
    SELECT a.action, a.actor_id, a.resource_id, a.recorded_at AS at, a.metadata
    FROM public.audit_log a
    WHERE a.tenant_id = $1
      AND a.action IN ('sales.buyer_lead.record', 'sales.buyer_lead.status',
                       'sales.fpo_lead.record', 'sales.fpo_lead.status',
                       'sales.deal.record', 'sales.deal.payment_record', 'sales.deal.status_set')
      AND a.recorded_at >= $2
      AND a.actor_id IS NOT NULL
),
deal_animals AS (
    SELECT l.deal_id,
           sum(CASE WHEN l.product_kind = 'animal'
                    THEN coalesce(l.animal_count, coalesce(l.male_count, 0) + coalesce(l.female_count, 0))
                    ELSE 0 END) AS animals
    FROM public.sales_deal_lines l
    WHERE l.tenant_id = $1
      AND l.deal_id IN (SELECT resource_id FROM audited WHERE action = 'sales.deal.record')
    GROUP BY l.deal_id
),
activity AS (
    SELECT 'vendor_added' AS kind, v.created_by AS actor_id, v.created_at AS at,
           (v.created_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date,
           v.vendor_id::text AS subject_id, v.business_name AS subject, v.record_type AS category,
           0::float8 AS animals, 0::float8 AS amount
    FROM vendor v
    WHERE v.created_by IS NOT NULL AND v.created_at >= $2

    UNION ALL
    SELECT 'vendor_edited', e.actor_id, e.at, e.business_date,
           e.vendor_id::text, coalesce(v.business_name, ''), coalesce(v.record_type, ''),
           0, 0
    FROM edit_day e
    LEFT JOIN vendor v ON v.vendor_id = e.vendor_id

    UNION ALL
    SELECT 'market_call', m.actor_id, m.at, m.business_date,
           m.city_id::text, m.city_name, '', m.answers::float8, 0
    FROM market_call m

    UNION ALL
    SELECT 'lead_call', a.actor_id, a.at, (a.at AT TIME ZONE 'Asia/Kolkata')::date,
           coalesce(a.resource_id::text, ''),
           coalesce(bl.buyer_name, fl.fpo_name, a.metadata->>'buyer_name', ''),
           coalesce(a.metadata->>'call_status', ''), 0, 0
    FROM audited a
    LEFT JOIN public.sales_buyer_leads bl
           ON a.action LIKE 'sales.buyer_lead.%' AND bl.tenant_id = $1 AND bl.id = a.resource_id
    LEFT JOIN public.sales_fpo_leads fl
           ON a.action LIKE 'sales.fpo_lead.%' AND fl.tenant_id = $1 AND fl.id = a.resource_id
    WHERE a.action IN ('sales.buyer_lead.record', 'sales.buyer_lead.status',
                       'sales.fpo_lead.record', 'sales.fpo_lead.status')

    UNION ALL
    SELECT CASE a.action WHEN 'sales.deal.record' THEN 'sale_recorded'
                         WHEN 'sales.deal.payment_record' THEN 'payment_recorded'
                         ELSE 'deal_status' END,
           a.actor_id, a.at, (a.at AT TIME ZONE 'Asia/Kolkata')::date,
           coalesce(a.resource_id::text, ''), coalesce(d.buyer_name, ''),
           CASE WHEN a.action = 'sales.deal.status_set' THEN coalesce(a.metadata->>'status', '') ELSE '' END,
           CASE WHEN a.action = 'sales.deal.record'
                THEN coalesce(da.animals,
                              CASE WHEN d.product_type IN ('Sheep', 'Goat')
                                   THEN coalesce(d.animal_count, coalesce(d.male_count, 0) + coalesce(d.female_count, 0))
                                   ELSE 0 END)
                ELSE 0 END::float8,
           CASE WHEN a.action = 'sales.deal.record' THEN coalesce(d.sales_value, 0)
                WHEN a.action = 'sales.deal.payment_record'
                     AND (a.metadata->>'amount_rupees') ~ '^-?[0-9]+(\.[0-9]+)?$'
                     THEN (a.metadata->>'amount_rupees')::numeric
                ELSE 0 END::float8
    FROM audited a
    LEFT JOIN public.sales_deals d ON d.tenant_id = $1 AND d.id = a.resource_id
    LEFT JOIN deal_animals da ON da.deal_id = a.resource_id
    WHERE a.action IN ('sales.deal.record', 'sales.deal.payment_record', 'sales.deal.status_set')
)
SELECT act.kind, act.actor_id::text, coalesce(p.name, ''), act.at, act.business_date::text,
       act.subject_id, act.subject, act.category, act.animals, act.amount
FROM activity act
LEFT JOIN person p ON p.user_id = act.actor_id
WHERE act.business_date >= $3::date
ORDER BY act.at, act.kind, act.subject_id`

// SalesActivities implements ports.SalesExecutiveAnalyticsRepository.
func (r *Repository) SalesActivities(ctx context.Context, tenantID string, since time.Time) ([]domain.SalesActivityFact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	bound := sqlbind.MustBind(salesActivitiesSQL, tenantID, since, since.Format("2006-01-02"))
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("procurement: sales executive activities: %w", err)
	}
	defer rows.Close()

	facts := make([]domain.SalesActivityFact, 0, 256)
	for rows.Next() {
		var f domain.SalesActivityFact
		if err := rows.Scan(&f.Kind, &f.ActorID, &f.ActorName, &f.At, &f.BusinessDate,
			&f.SubjectID, &f.Subject, &f.Category, &f.Animals, &f.Amount); err != nil {
			return nil, fmt.Errorf("procurement: sales executive activities scan: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement: sales executive activities rows: %w", err)
	}
	return facts, nil
}

// latestVendorsSQL reads the newest register rows with their adder's name.
//
// projection-review: membership=procurement_vendors at ROW grain, tenant-scoped; the person join
// is pre-aggregated to one row per user_id exactly as in salesActivitiesSQL, so it is 1:1;
// pagination=LIMIT/OFFSET over created_at DESC, vendor_id DESC -- a total order, so page 2
// continues page 1.
//
// scale-guard:ignore: bounded LIMIT/OFFSET over the authored vendor register (a few hundred rows,
// grows with vendors met, never with herd size); the service refuses an offset past
// domain.MaxSalesExecutiveOffset. Offset rather than keyset because the page offers Back as well
// as Next, the same reasoning as the vendor register list.
const latestVendorsSQL = `
WITH person AS (
    SELECT wm.user_id,
           CASE WHEN count(*) FILTER (WHERE wm.status = 'active') = 1
                    THEN max(wm.display_name) FILTER (WHERE wm.status = 'active')
                WHEN count(DISTINCT wm.display_name) = 1
                    THEN min(wm.display_name)
           END AS name
    FROM public.workforce_members wm
    WHERE wm.tenant_id = $1 AND wm.user_id IS NOT NULL
    GROUP BY wm.user_id
)
SELECT v.vendor_id::text, v.business_name, v.record_type,
       coalesce(nullif(btrim(v.city), ''), ''), coalesce(btrim(v.state), ''),
       v.created_by IS NOT NULL, coalesce(p.name, ''), v.created_at
FROM public.procurement_vendors v
LEFT JOIN person p ON p.user_id = v.created_by
WHERE v.tenant_id = $1
ORDER BY v.created_at DESC, v.vendor_id DESC
LIMIT $2 OFFSET $3` // scale-guard:ignore: bounded authored register page; see note above

// LatestVendors implements ports.SalesExecutiveAnalyticsRepository.
func (r *Repository) LatestVendors(ctx context.Context, tenantID string, limit, offset int) ([]domain.LatestVendorFact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	bound := sqlbind.MustBind(latestVendorsSQL, tenantID, limit, offset)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("procurement: latest vendors: %w", err)
	}
	defer rows.Close()

	out := make([]domain.LatestVendorFact, 0, limit)
	for rows.Next() {
		var (
			f           domain.LatestVendorFact
			city, state string
		)
		if err := rows.Scan(&f.VendorID, &f.BusinessName, &f.Category, &city, &state,
			&f.AddedByKnown, &f.AddedByName, &f.AddedAt); err != nil {
			return nil, fmt.Errorf("procurement: latest vendors scan: %w", err)
		}
		f.Place = joinPlace(city, state)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement: latest vendors rows: %w", err)
	}
	return out, nil
}

// VendorRegisterTotals implements ports.SalesExecutiveAnalyticsRepository.
//
// scale-guard:ignore: one aggregate over the authored vendor register (a few hundred rows).
func (r *Repository) VendorRegisterTotals(ctx context.Context, tenantID string) (int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var total, imported int
	if err := r.pool.QueryRow(ctx, `
SELECT count(*), count(*) FILTER (WHERE created_by IS NULL)
FROM public.procurement_vendors
WHERE tenant_id = $1`, tenantID).Scan(&total, &imported); err != nil {
		return 0, 0, fmt.Errorf("procurement: vendor register totals: %w", err)
	}
	return total, imported, nil
}
