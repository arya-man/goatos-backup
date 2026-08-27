package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// VendorService serves the procurement vendor register.
//
// It is deliberately thin. The register is a contact book: there is no state machine, no clock, no
// obligation and no proof, so there is nothing here for a service layer to orchestrate beyond
// validating a write and deciding whether the caller may see payment instruments. Inventing
// workflow around it would be inventing rules the business does not have.
type VendorService struct {
	repo ports.VendorRepository
}

func NewVendorService(repo ports.VendorRepository) *VendorService {
	return &VendorService{repo: repo}
}

// VendorListQuery is one page request against the register.
type VendorListQuery struct {
	Filter domain.VendorFilter
	Limit  int
	Offset int
	// IncludeFinance reflects the CALLER's VendorFinanceRead permission, resolved by the HTTP layer
	// from the request's grants. It is never a client-supplied parameter -- a caller must not be
	// able to ask for payment instruments they do not hold the permission for.
	IncludeFinance bool
}

// ListVendors returns one page plus the whole-filter total.
func (s *VendorService) ListVendors(ctx context.Context, tenantID string, q VendorListQuery) (ports.VendorPage, error) {
	if q.Offset < 0 || q.Offset > domain.MaxVendorOffset {
		// REJECTED rather than clamped. Clamping would serve page 1's rows under page 400's number,
		// which is a lie the operator cannot detect from the screen.
		return ports.VendorPage{}, ErrVendorOffsetOutOfRange
	}
	return s.repo.ListVendors(ctx, tenantID, q.Filter, q.Limit, q.Offset, q.IncludeFinance)
}

// GetVendor returns one vendor.
func (s *VendorService) GetVendor(ctx context.Context, tenantID, vendorID string, includeFinance bool) (domain.Vendor, error) {
	return s.repo.GetVendor(ctx, tenantID, vendorID, includeFinance)
}

// CreateVendor validates and inserts a vendor.
//
// Normalize runs BEFORE Validate so the rules apply to the values that will actually be stored: a
// business name of "   " must fail the required check, not pass it because it was non-empty before
// trimming.
func (s *VendorService) CreateVendor(ctx context.Context, tenantID string, write domain.VendorWrite, actorID string, includeFinance bool) (domain.Vendor, error) {
	normalized := write.Normalize()
	// Create is held to the stricter bar: contact person, phone and city too, matching the Slack
	// intake questionnaire. Update is not -- see ValidateForCreate for why.
	if err := normalized.ValidateForCreate(); err != nil {
		return domain.Vendor{}, err
	}
	created, err := s.repo.CreateVendor(ctx, tenantID, normalized, actorID)
	if err != nil {
		return domain.Vendor{}, err
	}
	// The write path accepted finance values from a caller who holds VendorWrite; the READ back is
	// still governed by VendorFinanceRead. A caller who may add a bank account but not read one
	// gets the redacted row, which keeps one rule in one place instead of two that can disagree.
	if !includeFinance {
		created = created.RedactFinance()
	}
	return created, nil
}

// UpdateVendor validates and replaces a vendor's fields, fenced on rowVersion.
func (s *VendorService) UpdateVendor(ctx context.Context, tenantID, vendorID string, write domain.VendorWrite, rowVersion int64, actorID string, includeFinance bool) (domain.Vendor, error) {
	if rowVersion <= 0 {
		// A missing or zero row_version means the client never read the row it is trying to replace,
		// so the optimistic fence cannot protect anyone. Refuse rather than defaulting to "overwrite
		// whatever is there".
		return domain.Vendor{}, ErrVendorRowVersionRequired
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.Vendor{}, err
	}
	// preserveFinance is the INVERSE of includeFinance. A caller who cannot READ the payment
	// instruments was never shown them, so their form submits blanks -- and a replace would delete a
	// bank account they had no way to know existed. You cannot clear what you cannot see.
	updated, err := s.repo.UpdateVendor(ctx, tenantID, vendorID, normalized, rowVersion, actorID, !includeFinance)
	if err != nil {
		return domain.Vendor{}, err
	}
	if !includeFinance {
		updated = updated.RedactFinance()
	}
	return updated, nil
}

// UpdateVendorStatus flips only the trading status.
//
// Separate from UpdateVendor because the update is a REPLACE: routing a status change through it
// would make the caller resend every other field, and anything their screen did not render would be
// cleared as a side effect.
func (s *VendorService) UpdateVendorStatus(ctx context.Context, tenantID, vendorID, status string, rowVersion int64, actorID string) (domain.Vendor, error) {
	if rowVersion <= 0 {
		return domain.Vendor{}, ErrVendorRowVersionRequired
	}
	normalized, ok := domain.NormalizeStatus(status)
	if !ok {
		return domain.Vendor{}, domain.ErrVendorValidation{Field: "status", Reason: "must be one of active, inactive, negotiating, banned"}
	}
	return s.repo.UpdateVendorStatus(ctx, tenantID, vendorID, normalized, rowVersion, actorID)
}

// ListVendorCatalog returns the dropdown vocabularies.
//
// activeOnly is false here on purpose: the screen needs retired entries too, because an existing
// vendor may still carry one and the edit form must be able to render (and re-save) the value it
// already has. The client marks inactive entries so they are shown but not offered for new rows.
func (s *VendorService) ListVendorCatalog(ctx context.Context, tenantID string) ([]domain.VendorCatalogEntry, error) {
	return s.repo.ListVendorCatalog(ctx, tenantID, false)
}

// ListVendorOptions returns the ACTIVE register as a bounded picklist for a counterparty dropdown.
//
// Read by the Sales record-sale drawer, which must map every deal to a vendor (maintainer decision
// 2026-08-27). It stays a procurement read served under VendorRead: the SALES module reads no
// procurement table (the 000173 lock), so the vendor is chosen in admin-web and only the chosen id
// travels onto the sale.
func (s *VendorService) ListVendorOptions(ctx context.Context, tenantID string) (domain.VendorOptions, error) {
	return s.repo.ListVendorOptions(ctx, tenantID)
}
