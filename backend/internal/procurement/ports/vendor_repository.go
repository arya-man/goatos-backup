package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

var (
	// ErrVendorNotFound is returned when a vendor id does not resolve inside the caller's tenant.
	// It is deliberately indistinguishable from "exists in another tenant", so a caller cannot probe
	// for the existence of another tenant's vendors.
	ErrVendorNotFound = errors.New("procurement: vendor not found")
	// ErrVendorDuplicate is returned when a write would violate procurement_vendors_natural_uq --
	// the same person at the same business in the same state. Surfaced as a 409 with an operator
	// readable message rather than a raw constraint 500.
	ErrVendorDuplicate = errors.New("procurement: vendor already exists")
	// ErrVendorStaleWrite is returned when an update carries a row_version that no longer matches.
	// Two people had the edit drawer open; the second one's save is refused rather than silently
	// overwriting the first one's.
	ErrVendorStaleWrite = errors.New("procurement: vendor changed since it was read")
)

// VendorPage is one page of the register plus the whole-filter total.
//
// Total is the count across the ENTIRE filter, not the page -- per the operational read-model
// contract, a summary is a whole-filter aggregate and pagination changes rows only. It is what the
// screen renders as "306 vendors", and it is also what makes a page COUNT possible: the register
// pages by bounded offset precisely so an operator can go back and see "Page 2 of 13".
type VendorPage struct {
	Vendors []domain.Vendor
	Total   int
}

// VendorRepository is the register's persistence boundary.
type VendorRepository interface {
	// ListVendors returns one page matching the filter, ordered by (business_name, vendor_id).
	// includeFinance selects whether payment instruments are populated; when false the returned rows
	// are already redacted, so a caller cannot forget to redact them.
	ListVendors(ctx context.Context, tenantID string, filter domain.VendorFilter, limit, offset int, includeFinance bool) (VendorPage, error)

	// GetVendor returns a single vendor, redacted the same way as the list read.
	GetVendor(ctx context.Context, tenantID, vendorID string, includeFinance bool) (domain.Vendor, error)

	// CreateVendor inserts a vendor and returns the stored row. Returns ErrVendorDuplicate on a
	// natural-key collision.
	CreateVendor(ctx context.Context, tenantID string, write domain.VendorWrite, actorID string) (domain.Vendor, error)

	// UpdateVendor replaces a vendor's fields, fenced on rowVersion. Returns ErrVendorStaleWrite when
	// the fence fails and ErrVendorDuplicate when the edit collides with another row's natural key.
	//
	// preserveFinance keeps the stored payment instruments UNTOUCHED instead of taking them from the
	// write. It is set for a caller without VendorFinanceRead, whose form never rendered those fields
	// and would therefore submit blanks -- silently deleting a bank account they were not allowed to
	// see. You cannot clear what you cannot read.
	UpdateVendor(ctx context.Context, tenantID, vendorID string, write domain.VendorWrite, rowVersion int64, actorID string, preserveFinance bool) (domain.Vendor, error)

	// UpdateVendorStatus changes ONLY the trading status, fenced on rowVersion.
	//
	// It is a separate method rather than a full update with one field changed, because the update is
	// a REPLACE: driving a status flip through it would require the caller to resend every other
	// field, and any field their screen did not render would be cleared as a side effect. "Stop
	// buying from this vendor" is also a distinct business act from "correct this vendor's details",
	// and reads better in an audit trail as its own operation.
	UpdateVendorStatus(ctx context.Context, tenantID, vendorID, status string, rowVersion int64, actorID string) (domain.Vendor, error)

	// ListVendorCatalog returns the business-managed dropdown vocabularies. activeOnly excludes
	// retired entries, which is what a write form wants; a read filter wants everything so an
	// existing vendor's retired value still renders.
	ListVendorCatalog(ctx context.Context, tenantID string, activeOnly bool) ([]domain.VendorCatalogEntry, error)

	// ListVendorOptions returns the ACTIVE register as a bounded picklist for a "who is this for"
	// dropdown -- id, name and location only, never the payment instruments VendorFinanceRead
	// guards. One query capped at domain.MaxVendorOptions, so this is never a paged full walk.
	ListVendorOptions(ctx context.Context, tenantID string) (domain.VendorOptions, error)
}
