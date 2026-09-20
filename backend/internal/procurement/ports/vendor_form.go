package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// ErrVendorFormVersionUnknown names a questionnaire version no published or retired
// `sales.vendor` SOP carries.
var ErrVendorFormVersionUnknown = errors.New("procurement: vendor form version unknown")

// VendorFormSource reads the authored vendor form (form_dsl.vendor_form of the `sales.vendor`
// SOP) -- the document the Add/Edit vendor screens render and a write is checked against.
type VendorFormSource interface {
	// PublishedVendorForm is the form a screen opening now renders, its catalog-backed questions
	// filled from the catalog entries given (the service narrows record types to the register
	// side the screen is on, exactly as the catalog read does).
	PublishedVendorForm(ctx context.Context, tenantID string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error)
	// VendorFormVersion is the exact form a vendor was answered on, choices filled the same way.
	VendorFormVersion(ctx context.Context, tenantID string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error)
}
