package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// naturalKeyConstraint is the unique index a duplicate write violates. Matching on the constraint
// NAME rather than on SQLSTATE 23505 alone matters: the table carries several unique/check
// constraints, and reporting "vendor already exists" for an unrelated violation would send an
// operator hunting for a duplicate that does not exist.
const naturalKeyConstraint = "procurement_vendors_natural_uq"

// vendorColumns is the single projection every vendor read uses.
//
// search_text is deliberately NOT selected: it is a generated denormalization for the trigram
// index, never a field the API returns.
const vendorColumns = `
	v.vendor_id, v.tenant_id, v.record_type, v.business_name, v.contact_person_name, v.phone_number,
	v.breed, v.feed, v.status, v.filtered_stock, v.price_per_goat, v.ready_to_filtered,
	v.eta_after_order_days, v.details, v.state, v.city,
	v.bank_name, v.account_no, v.ifsc_code, v.upi_id, v.pan_number,
	v.comments, v.party_id, v.source_row,
	v.created_at, v.updated_at, v.row_version`

// scanVendor reads one row of vendorColumns, in that exact order.
//
// Column order here and in vendorColumns must move together. pgx fails loudly on a count mismatch
// ("number of field descriptions must equal number of destinations") but silently mis-assigns two
// same-typed columns that are swapped, so any edit to one must be mirrored in the other.
func scanVendor(row pgx.Row) (domain.Vendor, error) {
	var (
		v          domain.Vendor
		createdAt  time.Time
		updatedAt  time.Time
		partyID    *string
		priceStr   *string
		stock      *int32
		etaDays    *int32
		sourceRow  *int32
		rowVersion int64
	)
	err := row.Scan(
		&v.VendorID, &v.TenantID, &v.RecordType, &v.BusinessName, &v.ContactPersonName, &v.PhoneNumber,
		&v.Breed, &v.Feed, &v.Status, &stock, &priceStr, &v.ReadyToFiltered,
		&etaDays, &v.Details, &v.State, &v.City,
		&v.BankName, &v.AccountNo, &v.IFSCCode, &v.UPIID, &v.PANNumber,
		&v.Comments, &partyID, &sourceRow,
		&createdAt, &updatedAt, &rowVersion,
	)
	if err != nil {
		return domain.Vendor{}, err
	}
	v.PartyID = partyID
	v.PricePerGoat = priceStr
	if stock != nil {
		n := int(*stock)
		v.FilteredStock = &n
	}
	if etaDays != nil {
		n := int(*etaDays)
		v.ETAAfterOrderDays = &n
	}
	if sourceRow != nil {
		n := int(*sourceRow)
		v.SourceRow = &n
	}
	// RFC3339 in UTC. The register carries no business-day semantics -- these are record-keeping
	// timestamps ("when was this vendor added"), not a farm clock, so the Asia/Kolkata business-date
	// rule that governs scheduling does not apply here. The client renders them in local time.
	v.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	v.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	v.RowVersion = rowVersion
	return v, nil
}

// buildVendorFilter renders the shared WHERE clause for both the page read and its total.
//
// The page and the total MUST range over the identical predicate set. Building them from one
// function is what guarantees that: a hand-written second copy is how a screen ends up reporting
// "307 vendors" over a list that is filtered to 89. The cursor is the ONLY thing the total omits,
// because a total is a whole-filter aggregate and a cursor is a page position.
func buildVendorFilter(tenantID string, f domain.VendorFilter) (string, []any) {
	args := []any{tenantID}
	clauses := []string{"v.tenant_id = $1"}

	add := func(sql string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(sql, len(args)))
	}
	if f.Search != "" {
		// Infix match against the generated, lower-cased search_text, served by the pg_trgm GIN index
		// (procurement_vendors_search_trgm_idx). The wildcards are bound as DATA, never concatenated
		// into the SQL text.
		add("v.search_text LIKE $%d", "%"+escapeLikePattern(f.Search)+"%")
	}
	if f.RecordType != "" {
		add("v.record_type = $%d", f.RecordType)
	}
	if f.Status != "" {
		add("v.status = $%d", f.Status)
	}
	if f.State != "" {
		add("v.state = $%d", f.State)
	}
	if f.City != "" {
		add("v.city = $%d", f.City)
	}
	if f.Breed != "" {
		add("v.breed = $%d", f.Breed)
	}
	return strings.Join(clauses, " AND "), args
}

// escapeLikePattern neutralizes the LIKE metacharacters so a search for "100%" looks for the
// literal text rather than matching everything. The backslash is the default escape character in
// Postgres LIKE, so no ESCAPE clause is needed.
func escapeLikePattern(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(v)
}

// ListVendors returns one page plus the whole-filter total.
func (r *Repository) ListVendors(ctx context.Context, tenantID string, filter domain.VendorFilter, limit, offset int, includeFinance bool) (ports.VendorPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	filter = filter.Normalize()
	limit = domain.ClampVendorPageSize(limit)
	if offset < 0 {
		offset = 0
	}

	where, args := buildVendorFilter(tenantID, filter)

	// scale-guard:ignore: bounded LIMIT/OFFSET over an authored contact book, not a herd-sized table.
	// The register is ~306 rows today and grows with the number of SUPPLIERS the farm deals with, never
	// with animal count, so the skipped-row cost cannot grow with the business the way an
	// obligation/goat table would. The service rejects offset > domain.MaxVendorOffset, so the offset is
	// bounded by construction. Offset rather than keyset is a deliberate product requirement: the
	// operator asked to page BACKWARDS and to see a page number, and a forward-only keyset cursor can
	// express neither. Same reasoning and shape as feedconfig's authored-grid reads.
	query := fmt.Sprintf(`SELECT %s FROM public.procurement_vendors v WHERE %s ORDER BY v.business_name, v.vendor_id LIMIT %d OFFSET %d`, // scale-guard:ignore: bounded authored contact book pagination; see note above
		vendorColumns, where, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ports.VendorPage{}, fmt.Errorf("list vendors: %w", err)
	}
	defer rows.Close()

	vendors := make([]domain.Vendor, 0, limit)
	for rows.Next() {
		v, err := scanVendor(rows)
		if err != nil {
			return ports.VendorPage{}, fmt.Errorf("list vendors scan: %w", err)
		}
		if !includeFinance {
			v = v.RedactFinance()
		}
		vendors = append(vendors, v)
	}
	if err := rows.Err(); err != nil {
		return ports.VendorPage{}, fmt.Errorf("list vendors rows: %w", err)
	}

	page := ports.VendorPage{Vendors: vendors}

	// Whole-filter total, over the SAME predicates. Built from the same buildVendorFilter call so the
	// two can never drift -- a hand-written second copy is how a screen reports "306 vendors" over a
	// list filtered to 89.
	countQuery := fmt.Sprintf(`SELECT count(*) FROM public.procurement_vendors v WHERE %s`, where)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&page.Total); err != nil {
		return ports.VendorPage{}, fmt.Errorf("count vendors: %w", err)
	}
	return page, nil
}

// GetVendor returns one vendor scoped to the caller's tenant.
func (r *Repository) GetVendor(ctx context.Context, tenantID, vendorID string, includeFinance bool) (domain.Vendor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM public.procurement_vendors v WHERE v.tenant_id = $1 AND v.vendor_id = $2`, vendorColumns)
	v, err := scanVendor(r.pool.QueryRow(ctx, query, tenantID, vendorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Vendor{}, ports.ErrVendorNotFound
	}
	if err != nil {
		return domain.Vendor{}, fmt.Errorf("get vendor: %w", err)
	}
	if !includeFinance {
		v = v.RedactFinance()
	}
	return v, nil
}

// CreateVendor inserts a vendor and returns the stored row.
//
// The caller is expected to have normalized and validated the write already; this re-normalizes
// defensively because the natural key is computed from these exact values and a bypassed
// normalization would produce a row that collides with nothing and duplicates something.
func (r *Repository) CreateVendor(ctx context.Context, tenantID string, write domain.VendorWrite, actorID string) (domain.Vendor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	w := write.Normalize()
	query := fmt.Sprintf(`
		INSERT INTO public.procurement_vendors AS v (
			tenant_id, record_type, business_name, contact_person_name, phone_number,
			breed, feed, status, filtered_stock, price_per_goat, ready_to_filtered,
			eta_after_order_days, details, state, city,
			bank_name, account_no, ifsc_code, upi_id, pan_number,
			comments, created_by, updated_by
		) VALUES (
			$1, $2, $3, %s, %s,
			%s, %s, $6, $7, $8, %s,
			$10, %s, $11, %s,
			%s, %s, %s, %s, %s,
			%s, $18, $18
		)
		RETURNING %s`,
		nullIf("$4"), nullIf("$5"),
		nullIf("$19"), nullIf("$20"), nullIf("$9"),
		nullIf("$21"), nullIf("$12"),
		nullIf("$13"), nullIf("$14"), nullIf("$15"), nullIf("$16"), nullIf("$17"),
		nullIf("$22"),
		vendorColumns)

	v, err := scanVendor(r.pool.QueryRow(ctx, query,
		tenantID, w.RecordType, w.BusinessName, w.ContactPersonName, w.PhoneNumber,
		w.Status, w.FilteredStock, w.PricePerGoat, w.ReadyToFiltered,
		w.ETAAfterOrderDays, w.State, w.City,
		w.BankName, w.AccountNo, w.IFSCCode, w.UPIID, w.PANNumber,
		nullableActor(actorID),
		w.Breed, w.Feed, w.Details, w.Comments,
	))
	if err != nil {
		if isNaturalKeyViolation(err) {
			return domain.Vendor{}, ports.ErrVendorDuplicate
		}
		return domain.Vendor{}, fmt.Errorf("create vendor: %w", err)
	}
	return v, nil
}

// UpdateVendor replaces a vendor's fields, fenced on row_version.
//
// The fence is in the WHERE clause rather than a read-then-write check, so two concurrent saves
// cannot both pass a check and then both write. The loser gets zero rows back, and the follow-up
// read distinguishes "gone" from "changed underneath you" -- two different things to tell an
// operator who has an edit drawer open.
func (r *Repository) UpdateVendor(ctx context.Context, tenantID, vendorID string, write domain.VendorWrite, rowVersion int64, actorID string, preserveFinance bool) (domain.Vendor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	w := write.Normalize()
	query := fmt.Sprintf(`
		UPDATE public.procurement_vendors v SET
			record_type = $3,
			business_name = $4,
			contact_person_name = %s,
			phone_number = %s,
			breed = %s,
			feed = %s,
			status = $9,
			filtered_stock = $10,
			price_per_goat = $11,
			ready_to_filtered = %s,
			eta_after_order_days = $13,
			details = %s,
			state = $15,
			city = %s,
			bank_name = CASE WHEN $25 THEN v.bank_name ELSE %s END,
			account_no = CASE WHEN $25 THEN v.account_no ELSE %s END,
			ifsc_code = CASE WHEN $25 THEN v.ifsc_code ELSE %s END,
			upi_id = CASE WHEN $25 THEN v.upi_id ELSE %s END,
			pan_number = CASE WHEN $25 THEN v.pan_number ELSE %s END,
			comments = %s,
			updated_by = $23,
			updated_at = now(),
			row_version = v.row_version + 1
		WHERE v.tenant_id = $1 AND v.vendor_id = $2 AND v.row_version = $24
		RETURNING %s`,
		nullIf("$5"), nullIf("$6"), nullIf("$7"), nullIf("$8"),
		nullIf("$12"), nullIf("$14"), nullIf("$16"),
		nullIf("$17"), nullIf("$18"), nullIf("$19"), nullIf("$20"), nullIf("$21"),
		nullIf("$22"),
		vendorColumns)

	v, err := scanVendor(r.pool.QueryRow(ctx, query,
		tenantID, vendorID, w.RecordType, w.BusinessName,
		w.ContactPersonName, w.PhoneNumber, w.Breed, w.Feed,
		w.Status, w.FilteredStock, w.PricePerGoat, w.ReadyToFiltered,
		w.ETAAfterOrderDays, w.Details, w.State, w.City,
		w.BankName, w.AccountNo, w.IFSCCode, w.UPIID, w.PANNumber,
		w.Comments, nullableActor(actorID), rowVersion, preserveFinance,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// Zero rows means the tenant+id+version triple did not match. Re-read without the version to
		// tell the two cases apart, because "someone else saved first" and "this vendor is gone" need
		// different words on screen.
		if _, getErr := r.GetVendor(ctx, tenantID, vendorID, false); errors.Is(getErr, ports.ErrVendorNotFound) {
			return domain.Vendor{}, ports.ErrVendorNotFound
		}
		return domain.Vendor{}, ports.ErrVendorStaleWrite
	}
	if err != nil {
		if isNaturalKeyViolation(err) {
			return domain.Vendor{}, ports.ErrVendorDuplicate
		}
		return domain.Vendor{}, fmt.Errorf("update vendor: %w", err)
	}
	return v, nil
}

// ListVendorCatalog returns the business-managed dropdown vocabularies.
func (r *Repository) ListVendorCatalog(ctx context.Context, tenantID string, activeOnly bool) ([]domain.VendorCatalogEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// CITY is derived LIVE from the cities vendors actually carry, not from the stored vocabulary.
	//
	// City is free text on the entry form, so a frozen catalog would go stale the moment someone
	// records a supplier in a town the sheet never listed: the vendor saves fine and then cannot be
	// found by its own city filter. Deriving the facet from the data keeps "what you can filter by"
	// equal to "what is actually in the register", which is what a facet means.
	//
	// Every other kind still comes from the authored catalog, because those ARE closed vocabularies
	// the business governs.
	query := `
		SELECT kind, value, label, sort_order, is_active
		FROM public.procurement_vendor_catalog
		WHERE tenant_id = $1 AND kind <> 'city' AND ($2::boolean IS NOT TRUE OR is_active)
		UNION ALL
		SELECT 'city', city, city, 0, true
		FROM (
			SELECT DISTINCT btrim(city) AS city
			FROM public.procurement_vendors
			WHERE tenant_id = $1 AND btrim(coalesce(city, '')) <> ''
		) c
		ORDER BY kind, sort_order, value`
	rows, err := r.pool.Query(ctx, query, tenantID, activeOnly)
	if err != nil {
		return nil, fmt.Errorf("list vendor catalog: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.VendorCatalogEntry, 0, 128)
	for rows.Next() {
		var e domain.VendorCatalogEntry
		if err := rows.Scan(&e.Kind, &e.Value, &e.Label, &e.SortOrder, &e.IsActive); err != nil {
			return nil, fmt.Errorf("list vendor catalog scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list vendor catalog rows: %w", err)
	}
	return entries, nil
}

// UpdateVendorStatus changes only the trading status.
func (r *Repository) UpdateVendorStatus(ctx context.Context, tenantID, vendorID, status string, rowVersion int64, actorID string) (domain.Vendor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	query := fmt.Sprintf(`
		UPDATE public.procurement_vendors v SET
			status = $3,
			updated_by = %s,
			updated_at = now(),
			row_version = v.row_version + 1
		WHERE v.tenant_id = $1 AND v.vendor_id = $2 AND v.row_version = $5
		RETURNING %s`, "$4", vendorColumns)

	v, err := scanVendor(r.pool.QueryRow(ctx, query, tenantID, vendorID, status, nullableActor(actorID), rowVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		// Same two-case split as UpdateVendor: "gone" and "someone saved first" need different words.
		if _, getErr := r.GetVendor(ctx, tenantID, vendorID, false); errors.Is(getErr, ports.ErrVendorNotFound) {
			return domain.Vendor{}, ports.ErrVendorNotFound
		}
		return domain.Vendor{}, ports.ErrVendorStaleWrite
	}
	if err != nil {
		return domain.Vendor{}, fmt.Errorf("update vendor status: %w", err)
	}
	// The status change never returns finance to the caller: this path exists for a quick flip from
	// the list, and the row is re-read by the page afterwards under the caller's own permission.
	return v.RedactFinance(), nil
}

// isNaturalKeyViolation reports a unique violation on the register's natural key SPECIFICALLY.
//
// Checking the constraint name rather than bare SQLSTATE 23505 keeps an unrelated unique violation
// from being reported to an operator as "this vendor already exists".
func isNaturalKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == naturalKeyConstraint
}

// nullIf renders a placeholder that stores NULL for an empty string.
//
// Every optional text column goes through this so "" and NULL cannot both end up in the table
// meaning the same thing. That matters most for phone_number, which the natural key coalesces:
// storing "" for some rows and NULL for others would give two spellings of "no phone".
func nullIf(placeholder string) string {
	return fmt.Sprintf("nullif(btrim(%s), '')", placeholder)
}

// nullableActor maps an absent actor to nil so created_by/updated_by stores NULL rather than
// failing the uuid cast on an empty string.
func nullableActor(actorID string) any {
	if strings.TrimSpace(actorID) == "" {
		return nil
	}
	return actorID
}
