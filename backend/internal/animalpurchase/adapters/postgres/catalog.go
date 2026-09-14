package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
)

// CatalogSource reads the inspection questionnaire from the SOP library (PROCUREMENT SOP,
// maintainer decision 2026-09-14): the `inspection` section of a `procurement.animal_purchase`
// sop_versions row, compiled the same way for the phone, the write and the review. A published
// version that fails to parse fails CLOSED with the field named -- and cannot be published in
// the first place (sop/app validates it through the same functions).
type CatalogSource struct {
	pool *pgxpool.Pool
}

func NewCatalogSource(pool *pgxpool.Pool) *CatalogSource { return &CatalogSource{pool: pool} }

var _ ports.CatalogSource = (*CatalogSource)(nil)

const sqlPublishedInspection = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlInspectionVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedCatalog is what a phone opening the form now renders. No authored version (a tenant
// created before the migration ran, or a test fixture) means the seeded document, version 1.
func (c *CatalogSource) PublishedCatalog(ctx context.Context, tenantID string) (domain.Catalog, error) {
	cat, found, err := c.read(ctx, sqlPublishedInspection, tenantID, domain.SOPCodeAnimalPurchase)
	if err != nil {
		return domain.Catalog{}, err
	}
	if !found {
		return domain.SeededCatalog(), nil
	}
	return cat, nil
}

// CatalogVersion is the exact document a recorded animal was answered on.
func (c *CatalogSource) CatalogVersion(ctx context.Context, tenantID string, version int) (domain.Catalog, error) {
	cat, found, err := c.read(ctx, sqlInspectionVersion, tenantID, domain.SOPCodeAnimalPurchase, version)
	if err != nil {
		return domain.Catalog{}, err
	}
	if !found {
		seeded := domain.SeededCatalog()
		if version == seeded.Version {
			return seeded, nil
		}
		return domain.Catalog{}, ports.ErrCatalogVersionUnknown
	}
	return cat, nil
}

func (c *CatalogSource) read(ctx context.Context, sql string, args ...any) (domain.Catalog, bool, error) {
	var version int
	var raw []byte
	err := c.pool.QueryRow(ctx, sql, args...).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Catalog{}, false, nil
	}
	if err != nil {
		return domain.Catalog{}, false, fmt.Errorf("animal purchase: read inspection sop: %w", err)
	}
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.Catalog{}, false, fmt.Errorf("animal purchase: inspection sop v%d form_dsl: %w", version, err)
	}
	dsl, err := domain.ParseInspection(formDSL)
	if err != nil {
		return domain.Catalog{}, false, fmt.Errorf("animal purchase: inspection sop v%d: %w", version, err)
	}
	if problems := domain.ValidateInspection(dsl); len(problems) > 0 {
		return domain.Catalog{}, false, fmt.Errorf("animal purchase: inspection sop v%d invalid: %s", version, problems[0])
	}
	return domain.Catalog{Version: version, Questions: domain.CompileInspection(dsl)}, true, nil
}
