package postgres

// Pipeline and evidence writes: buyer leads, FPO leads, market quotes, sold-tag lists and weight
// checks -- the datasets the retired Sales DB sheet used to carry, now recorded in the app.
//
// Every write follows CreateDeal's one-transaction contract: idempotency reservation, insert(s),
// and audit commit together; an exact replay re-reads and returns the original result with zero
// new side effects.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

const (
	idemScopeBuyerLeadCreate = "sales.buyer_lead.create"
	idemScopeBuyerLeadStatus = "sales.buyer_lead.status"
	idemScopeFPOLeadCreate   = "sales.fpo_lead.create"
	idemScopeFPOLeadStatus   = "sales.fpo_lead.status"
	idemScopeBenchmarkCreate = "sales.market_quote.create"
	idemScopeSoldTagsCreate  = "sales.sold_tags.create"
	idemScopeWeightCheck     = "sales.weight_check.create"
)

// ---------------------------------------------------------------------------
// Buyer leads
// ---------------------------------------------------------------------------

const buyerLeadColumns = `
	l.id, l.recorded_date, l.farm, l.buyer_name, l.buyer_place,
	l.animal_type, l.breed, l.call_status, l.created_at`

func scanBuyerLead(row pgx.Row) (domain.BuyerLead, error) {
	var (
		l        domain.BuyerLead
		recorded *time.Time
		created  time.Time
	)
	err := row.Scan(&l.LeadID, &recorded, &l.Farm, &l.BuyerName, &l.BuyerPlace,
		&l.AnimalType, &l.Breed, &l.CallStatus, &created)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	if recorded != nil {
		d := recorded.Format("2006-01-02")
		l.RecordedDate = &d
	}
	l.CreatedAt = created.UTC().Format(time.RFC3339)
	return l, nil
}

// ListBuyerLeads returns one pipeline page (newest first), the whole-filter total, and the
// tenant's existing call-status vocabulary.
func (r *Repository) ListBuyerLeads(ctx context.Context, tenantID string, limit, offset int) (ports.BuyerLeadPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit = domain.ClampLeadPageSize(limit)
	if offset < 0 {
		offset = 0
	}
	window := limit + offset

	query := fmt.Sprintf(`SELECT %s FROM public.sales_buyer_leads l WHERE l.tenant_id = $1
		ORDER BY l.created_at DESC, l.id LIMIT %d`, buyerLeadColumns, window)
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return ports.BuyerLeadPage{}, fmt.Errorf("list buyer leads: %w", err)
	}
	defer rows.Close()

	page := ports.BuyerLeadPage{Leads: make([]domain.BuyerLead, 0, limit)}
	seen := 0
	for rows.Next() {
		l, err := scanBuyerLead(rows)
		if err != nil {
			return ports.BuyerLeadPage{}, fmt.Errorf("list buyer leads scan: %w", err)
		}
		if seen < offset {
			seen++
			continue
		}
		seen++
		page.Leads = append(page.Leads, l)
	}
	if err := rows.Err(); err != nil {
		return ports.BuyerLeadPage{}, fmt.Errorf("list buyer leads rows: %w", err)
	}

	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM public.sales_buyer_leads l WHERE l.tenant_id = $1`, tenantID,
	).Scan(&page.Total); err != nil {
		return ports.BuyerLeadPage{}, fmt.Errorf("count buyer leads: %w", err)
	}

	options, err := r.distinctStatuses(ctx, "sales_buyer_leads", tenantID)
	if err != nil {
		return ports.BuyerLeadPage{}, err
	}
	page.StatusOptions = options
	return page, nil
}

// distinctStatuses returns the tenant's stored call-status vocabulary for one pipeline table.
// The table name is one of two compile-time constants, never caller input.
func (r *Repository) distinctStatuses(ctx context.Context, table, tenantID string) ([]string, error) {
	query := fmt.Sprintf(`SELECT DISTINCT l.call_status FROM public.%s l
		WHERE l.tenant_id = $1 AND l.call_status IS NOT NULL AND btrim(l.call_status) <> ''
		ORDER BY l.call_status`, table)
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list %s statuses: %w", table, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan %s status: %w", table, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) getBuyerLead(ctx context.Context, tenantID, leadID string) (domain.BuyerLead, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.sales_buyer_leads l WHERE l.tenant_id = $1 AND l.id = $2`, buyerLeadColumns)
	l, err := scanBuyerLead(r.pool.QueryRow(ctx, query, tenantID, leadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BuyerLead{}, ports.ErrLeadNotFound
	}
	if err != nil {
		return domain.BuyerLead{}, fmt.Errorf("get buyer lead: %w", err)
	}
	return l, nil
}

// CreateBuyerLead records a buyer lead: reservation, insert, audit in one transaction.
func (r *Repository) CreateBuyerLead(ctx context.Context, tenantID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: begin create buyer lead: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("buyer_lead",
		write.RecordedDate, write.Farm, write.BuyerName, write.BuyerPlace,
		write.AnimalType, write.Breed, write.CallStatus)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeBuyerLeadCreate, idempotencyKey, fingerprint)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.BuyerLead{}, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return r.getBuyerLead(ctx, tenantID, reservation.resultID)
	}

	var leadID string
	err = tx.QueryRow(ctx, `
		INSERT INTO public.sales_buyer_leads (
			tenant_id, recorded_date, farm, buyer_name, buyer_place, animal_type, breed, call_status
		) VALUES (
			$1, nullif($2, '')::date, nullif($3, ''), $4, nullif($5, ''),
			nullif($6, ''), nullif($7, ''), nullif($8, '')
		)
		RETURNING id::text`,
		tenantID, write.RecordedDate, write.Farm, write.BuyerName, write.BuyerPlace,
		write.AnimalType, write.Breed, write.CallStatus,
	).Scan(&leadID)
	if err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: create buyer lead: %w", err)
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.buyer_lead.record", "sales_buyer_lead", leadID, idempotencyKey, map[string]any{
		"buyer_name":  write.BuyerName,
		"call_status": write.CallStatus,
	}); err != nil {
		return domain.BuyerLead{}, err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeBuyerLeadCreate, idempotencyKey, "sales_buyer_lead", leadID); err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: complete buyer lead idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: commit create buyer lead: %w", err)
	}
	return r.getBuyerLead(ctx, tenantID, leadID)
}

// SetBuyerLeadStatus updates one lead's call status: reservation, update, audit in one transaction.
func (r *Repository) SetBuyerLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.BuyerLead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: begin buyer lead status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("buyer_lead_status", leadID, write.CallStatus)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeBuyerLeadStatus, idempotencyKey, fingerprint)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.BuyerLead{}, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return r.getBuyerLead(ctx, tenantID, leadID)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE public.sales_buyer_leads SET call_status = nullif($3, ''), updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, leadID, write.CallStatus)
	if err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: set buyer lead status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.BuyerLead{}, ports.ErrLeadNotFound
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.buyer_lead.status", "sales_buyer_lead", leadID, idempotencyKey, map[string]any{
		"call_status": write.CallStatus,
	}); err != nil {
		return domain.BuyerLead{}, err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeBuyerLeadStatus, idempotencyKey, "sales_buyer_lead", leadID); err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: complete buyer lead status idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.BuyerLead{}, fmt.Errorf("sales: commit buyer lead status: %w", err)
	}
	return r.getBuyerLead(ctx, tenantID, leadID)
}

// ---------------------------------------------------------------------------
// FPO leads
// ---------------------------------------------------------------------------

const fpoLeadColumns = `
	l.id, l.fpo_name, l.crops, l.district, l.taluk, l.state, l.call_status, l.created_at`

func scanFPOLead(row pgx.Row) (domain.FPOLead, error) {
	var (
		l       domain.FPOLead
		created time.Time
	)
	err := row.Scan(&l.LeadID, &l.FPOName, &l.Crops, &l.District, &l.Taluk, &l.State, &l.CallStatus, &created)
	if err != nil {
		return domain.FPOLead{}, err
	}
	l.CreatedAt = created.UTC().Format(time.RFC3339)
	return l, nil
}

// ListFPOLeads mirrors ListBuyerLeads for the farmer-group pipeline.
func (r *Repository) ListFPOLeads(ctx context.Context, tenantID string, limit, offset int) (ports.FPOLeadPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit = domain.ClampLeadPageSize(limit)
	if offset < 0 {
		offset = 0
	}
	window := limit + offset

	query := fmt.Sprintf(`SELECT %s FROM public.sales_fpo_leads l WHERE l.tenant_id = $1
		ORDER BY l.created_at DESC, l.id LIMIT %d`, fpoLeadColumns, window)
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return ports.FPOLeadPage{}, fmt.Errorf("list fpo leads: %w", err)
	}
	defer rows.Close()

	page := ports.FPOLeadPage{Leads: make([]domain.FPOLead, 0, limit)}
	seen := 0
	for rows.Next() {
		l, err := scanFPOLead(rows)
		if err != nil {
			return ports.FPOLeadPage{}, fmt.Errorf("list fpo leads scan: %w", err)
		}
		if seen < offset {
			seen++
			continue
		}
		seen++
		page.Leads = append(page.Leads, l)
	}
	if err := rows.Err(); err != nil {
		return ports.FPOLeadPage{}, fmt.Errorf("list fpo leads rows: %w", err)
	}

	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM public.sales_fpo_leads l WHERE l.tenant_id = $1`, tenantID,
	).Scan(&page.Total); err != nil {
		return ports.FPOLeadPage{}, fmt.Errorf("count fpo leads: %w", err)
	}

	options, err := r.distinctStatuses(ctx, "sales_fpo_leads", tenantID)
	if err != nil {
		return ports.FPOLeadPage{}, err
	}
	page.StatusOptions = options
	return page, nil
}

func (r *Repository) getFPOLead(ctx context.Context, tenantID, leadID string) (domain.FPOLead, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.sales_fpo_leads l WHERE l.tenant_id = $1 AND l.id = $2`, fpoLeadColumns)
	l, err := scanFPOLead(r.pool.QueryRow(ctx, query, tenantID, leadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FPOLead{}, ports.ErrLeadNotFound
	}
	if err != nil {
		return domain.FPOLead{}, fmt.Errorf("get fpo lead: %w", err)
	}
	return l, nil
}

// CreateFPOLead records a farmer-group lead.
func (r *Repository) CreateFPOLead(ctx context.Context, tenantID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: begin create fpo lead: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("fpo_lead",
		write.FPOName, write.Crops, write.District, write.Taluk, write.State, write.CallStatus)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeFPOLeadCreate, idempotencyKey, fingerprint)
	if err != nil {
		return domain.FPOLead{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.FPOLead{}, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return r.getFPOLead(ctx, tenantID, reservation.resultID)
	}

	var leadID string
	err = tx.QueryRow(ctx, `
		INSERT INTO public.sales_fpo_leads (
			tenant_id, fpo_name, crops, district, taluk, state, call_status
		) VALUES (
			$1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, '')
		)
		RETURNING id::text`,
		tenantID, write.FPOName, write.Crops, write.District, write.Taluk, write.State, write.CallStatus,
	).Scan(&leadID)
	if err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: create fpo lead: %w", err)
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.fpo_lead.record", "sales_fpo_lead", leadID, idempotencyKey, map[string]any{
		"fpo_name":    write.FPOName,
		"call_status": write.CallStatus,
	}); err != nil {
		return domain.FPOLead{}, err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeFPOLeadCreate, idempotencyKey, "sales_fpo_lead", leadID); err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: complete fpo lead idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: commit create fpo lead: %w", err)
	}
	return r.getFPOLead(ctx, tenantID, leadID)
}

// SetFPOLeadStatus updates one farmer-group lead's call status.
func (r *Repository) SetFPOLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.FPOLead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: begin fpo lead status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("fpo_lead_status", leadID, write.CallStatus)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeFPOLeadStatus, idempotencyKey, fingerprint)
	if err != nil {
		return domain.FPOLead{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.FPOLead{}, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return r.getFPOLead(ctx, tenantID, leadID)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE public.sales_fpo_leads SET call_status = nullif($3, ''), updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, leadID, write.CallStatus)
	if err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: set fpo lead status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.FPOLead{}, ports.ErrLeadNotFound
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.fpo_lead.status", "sales_fpo_lead", leadID, idempotencyKey, map[string]any{
		"call_status": write.CallStatus,
	}); err != nil {
		return domain.FPOLead{}, err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeFPOLeadStatus, idempotencyKey, "sales_fpo_lead", leadID); err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: complete fpo lead status idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FPOLead{}, fmt.Errorf("sales: commit fpo lead status: %w", err)
	}
	return r.getFPOLead(ctx, tenantID, leadID)
}

// ---------------------------------------------------------------------------
// Market quotes, sold-tag lists, weight checks
// ---------------------------------------------------------------------------

// CreateBenchmark records one market quote.
func (r *Repository) CreateBenchmark(ctx context.Context, tenantID string, write domain.BenchmarkWrite, actorID, idempotencyKey string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sales: begin create benchmark: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("benchmark",
		write.Market, write.Category, write.Breed, write.Source,
		write.ExFarmRate, write.TransportRate, fpFloat(write.LandingCostPerKg), fpFloat(write.MarketPricePerKg))
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeBenchmarkCreate, idempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		return tx.Commit(ctx)
	}

	var quoteID string
	err = tx.QueryRow(ctx, `
		INSERT INTO public.sales_market_benchmarks (
			tenant_id, market, category, breed, source, ex_farm_rate, transport_rate,
			landing_cost_per_kg, market_price_per_kg
		) VALUES (
			$1, nullif($2, ''), nullif($3, ''), $4, nullif($5, ''), nullif($6, ''), nullif($7, ''), $8, $9
		)
		RETURNING id::text`,
		tenantID, write.Market, write.Category, write.Breed, write.Source,
		write.ExFarmRate, write.TransportRate, write.LandingCostPerKg, write.MarketPricePerKg,
	).Scan(&quoteID)
	if err != nil {
		return fmt.Errorf("sales: create benchmark: %w", err)
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.market_quote.record", "sales_market_benchmark", quoteID, idempotencyKey, map[string]any{
		"breed":  write.Breed,
		"market": write.Market,
	}); err != nil {
		return err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeBenchmarkCreate, idempotencyKey, "sales_market_benchmark", quoteID); err != nil {
		return fmt.Errorf("sales: complete benchmark idempotency: %w", err)
	}
	return tx.Commit(ctx)
}

// CreateSoldTags records a handed-over tag list in ONE set-based insert (never per-row round
// trips). The whole batch commits or none of it does.
func (r *Repository) CreateSoldTags(ctx context.Context, tenantID string, write domain.SoldTagsWrite, actorID, idempotencyKey string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("sales: begin create sold tags: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	parts := []string{"sold_tags", write.Farm}
	labels := make([]string, 0, len(write.Rows))
	tags := make([]*string, 0, len(write.Rows))
	weights := make([]*float64, 0, len(write.Rows))
	for _, row := range write.Rows {
		parts = append(parts, row.AnimalLabel, row.TagNumber, fpFloat(row.WeightKg))
		labels = append(labels, row.AnimalLabel)
		tag := row.TagNumber
		if tag == "" {
			tags = append(tags, nil)
		} else {
			tags = append(tags, &tag)
		}
		weights = append(weights, row.WeightKg)
	}
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeSoldTagsCreate, idempotencyKey, requestFingerprint(parts...))
	if err != nil {
		return 0, err
	}
	if !reservation.proceed {
		// Exact replay: the fingerprint proves the batch is identical, so its size is the answer.
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return len(write.Rows), nil
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO public.sales_sold_animal_tags (tenant_id, farm, animal_label, tag_number, weight_kg)
		SELECT $1, nullif($2, ''), t.label, t.tag, t.weight
		FROM unnest($3::text[], $4::text[], $5::numeric[]) AS t(label, tag, weight)`,
		tenantID, write.Farm, labels, tags, weights)
	if err != nil {
		return 0, fmt.Errorf("sales: create sold tags: %w", err)
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.sold_tags.record", "sales_sold_animal_tags", "", idempotencyKey, map[string]any{
		"farm":    write.Farm,
		"animals": len(write.Rows),
	}); err != nil {
		return 0, err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeSoldTagsCreate, idempotencyKey, "sales_sold_animal_tags", ""); err != nil {
		return 0, fmt.Errorf("sales: complete sold tags idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("sales: commit create sold tags: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// CreateWeightCheck records one video-vs-book weight audit row.
func (r *Repository) CreateWeightCheck(ctx context.Context, tenantID string, write domain.WeightCheckWrite, actorID, idempotencyKey string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sales: begin create weight check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint("weight_check",
		write.TagNumber, fmt.Sprintf("%.4f", write.BookWeightKg), fmt.Sprintf("%.4f", write.VideoWeightKg),
		fmt.Sprintf("%t", write.FarmBorn))
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeWeightCheck, idempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		return tx.Commit(ctx)
	}

	var checkID string
	err = tx.QueryRow(ctx, `
		INSERT INTO public.sales_weight_audit (tenant_id, tag_number, book_weight_kg, video_weight_kg, farm_born)
		VALUES ($1, nullif($2, ''), $3, $4, $5)
		RETURNING id::text`,
		tenantID, write.TagNumber, write.BookWeightKg, write.VideoWeightKg, write.FarmBorn,
	).Scan(&checkID)
	if err != nil {
		return fmt.Errorf("sales: create weight check: %w", err)
	}

	if err := r.recordAudit(ctx, tx, tenantID, actorID, "sales.weight_check.record", "sales_weight_audit", checkID, idempotencyKey, map[string]any{
		"tag_number": write.TagNumber,
	}); err != nil {
		return err
	}
	if err := completeIdempotency(ctx, tx, tenantID, idemScopeWeightCheck, idempotencyKey, "sales_weight_audit", checkID); err != nil {
		return fmt.Errorf("sales: complete weight check idempotency: %w", err)
	}
	return tx.Commit(ctx)
}

// recordAudit writes the standard sales audit event inside the write transaction.
func (r *Repository) recordAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceType, resourceID, idempotencyKey string, extra map[string]any) error {
	metadata := map[string]any{
		"domain":          "sales",
		"module":          "sales",
		"category":        "pipeline",
		"idempotency_key": idempotencyKey,
		"operation_id":    idempotencyKey,
	}
	for k, v := range extra {
		metadata[k] = v
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Metadata:     metadata,
	}); err != nil {
		return fmt.Errorf("sales: audit %s: %w", action, err)
	}
	return nil
}
