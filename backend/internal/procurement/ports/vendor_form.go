package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// ErrVendorFormVersionUnknown names a questionnaire version the named vendor-form SOP does not
// carry in any published or retired version.
var ErrVendorFormVersionUnknown = errors.New("procurement: vendor form version unknown")

// VendorFormSource reads an authored vendor form (form_dsl.vendor_form of a vendor SOP) -- the
// document the Add/Edit vendor screens render and a write is checked against.
//
// There are TWO such documents since the 2026-09-20 split: `sales.vendor` for the buyer register
// and `procurement.vendor` for the supply register. The CALLER names the code, resolved from the
// register side the screen is on or the side the vendor's record type belongs to, so a supply
// vendor is never checked against the sales desk's questions.
type VendorFormSource interface {
	// PublishedVendorForm is the form a screen opening now renders, its catalog-backed questions
	// filled from the catalog entries given (the service narrows record types to the register
	// side the screen is on, exactly as the catalog read does).
	PublishedVendorForm(ctx context.Context, tenantID, sopCode string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error)
	// VendorFormVersion is the exact form a vendor was answered on, choices filled the same way.
	VendorFormVersion(ctx context.Context, tenantID, sopCode string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error)
}
