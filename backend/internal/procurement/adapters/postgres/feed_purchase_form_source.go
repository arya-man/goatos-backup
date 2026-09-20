package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FeedPurchaseFormSource reads the authored feed-purchase entry form from the SOP library (THE
// FEED PURCHASE FORM IS AUTHORED, 2026-09-20): `form_dsl.feed_purchase_form` of the
// `procurement.feed_purchase_form` sop_versions row. It shares the vendor form's reader, because
// the two documents share an engine; only the SOP code and the profile differ.
type FeedPurchaseFormSource struct{ inner *VendorFormSource }

// NewFeedPurchaseFormSource wires the reader over the shared pool.
func NewFeedPurchaseFormSource(pool *pgxpool.Pool) *FeedPurchaseFormSource {
	return &FeedPurchaseFormSource{inner: NewVendorFormSource(pool)}
}

var _ ports.FeedPurchaseFormSource = (*FeedPurchaseFormSource)(nil)

// PublishedFeedPurchaseForm is the form a screen opening now renders.
func (s *FeedPurchaseFormSource) PublishedFeedPurchaseForm(ctx context.Context, tenantID string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return s.inner.publishedForm(ctx, tenantID, domain.SOPCodeFeedPurchaseForm, domain.FeedPurchaseFormProfile(), domain.SeededFeedPurchaseFormDSL, catalog)
}

// FeedPurchaseFormVersion is the exact form a load was recorded on.
func (s *FeedPurchaseFormSource) FeedPurchaseFormVersion(ctx context.Context, tenantID string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return s.inner.formVersion(ctx, tenantID, domain.SOPCodeFeedPurchaseForm, domain.FeedPurchaseFormProfile(), domain.SeededFeedPurchaseFormDSL, version, catalog)
}
