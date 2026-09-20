package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// VendorFormSource reads a vendor form from the SOP library (VENDOR FORM IS AUTHORED,
// maintainer instruction 2026-09-19): `form_dsl.vendor_form` of the sop_versions row for the SOP
// CODE the caller names (`sales.vendor` for the buyer register, `procurement.vendor` for the
// supply one), compiled with the live vendor catalog so a catalog-backed question carries its
// choices.
// The same document is served to the phone, the web drawer and the write check. A version that
// fails to parse fails CLOSED with the field named -- and cannot be published in the first place,
// because sop/app validates it through the same functions.
type VendorFormSource struct {
	pool *pgxpool.Pool
}

func NewVendorFormSource(pool *pgxpool.Pool) *VendorFormSource {
	return &VendorFormSource{pool: pool}
}

var _ ports.VendorFormSource = (*VendorFormSource)(nil)

const sqlPublishedVendorForm = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlVendorFormVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedVendorForm is the form a screen opening now renders. No authored version (a tenant
// created before the migration ran, or a test fixture) means the seeded document, version 1.
func (s *VendorFormSource) PublishedVendorForm(ctx context.Context, tenantID, sopCode string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	dsl, version, found, err := s.read(ctx, sqlPublishedVendorForm, tenantID, sopCode)
	if err != nil {
		return domain.VendorForm{}, err
	}
	if !found {
		dsl, version = domain.SeededVendorFormDSL(), 1
	}
	return domain.CompileVendorForm(dsl, version, catalog), nil
}

// VendorFormVersion is the exact form a vendor was answered on.
func (s *VendorFormSource) VendorFormVersion(ctx context.Context, tenantID, sopCode string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	dsl, got, found, err := s.read(ctx, sqlVendorFormVersion, tenantID, sopCode, version)
	if err != nil {
		return domain.VendorForm{}, err
	}
	if !found {
		if version == 1 {
			return domain.CompileVendorForm(domain.SeededVendorFormDSL(), 1, catalog), nil
		}
		return domain.VendorForm{}, ports.ErrVendorFormVersionUnknown
	}
	return domain.CompileVendorForm(dsl, got, catalog), nil
}

func (s *VendorFormSource) read(ctx context.Context, sql string, args ...any) (domain.VendorFormDSL, int, bool, error) {
	var version int
	var raw []byte
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.VendorFormDSL{}, 0, false, nil
	}
	if err != nil {
		return domain.VendorFormDSL{}, 0, false, fmt.Errorf("procurement: read vendor form: %w", err)
	}
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.VendorFormDSL{}, 0, false, fmt.Errorf("procurement: vendor form v%d form_dsl: %w", version, err)
	}
	dsl, err := domain.ParseVendorForm(formDSL)
	if err != nil {
		return domain.VendorFormDSL{}, 0, false, fmt.Errorf("procurement: vendor form v%d: %w", version, err)
	}
	if problems := domain.ValidateVendorForm(dsl); len(problems) > 0 {
		return domain.VendorFormDSL{}, 0, false, fmt.Errorf("procurement: vendor form v%d: %s", version, problems[0])
	}
	return dsl, version, true, nil
}
