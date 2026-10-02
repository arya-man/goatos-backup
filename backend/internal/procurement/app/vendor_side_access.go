package app

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// VendorSideAccess is which halves of the ONE vendor register a caller may read and write
// (People / HRMS fixes, 2026-10-02). The suppliers are the procurement side (VendorRead /
// VendorWrite), the buyers the sales side (VendorSalesRead / VendorSalesWrite). The route table
// admits a caller holding EITHER half; this is what stops a buyers-only person reading or editing
// a supplier -- by asking for `?side=procurement`, by asking for no side at all, or by fetching a
// supplier's id directly.
//
// A vendor's side is its record type's catalog register_side (migration 000256); an uncatalogued
// type is on the procurement side, the same complementary rule the register's two pages use.
type VendorSideAccess struct {
	ReadSales, ReadSupply   bool
	WriteSales, WriteSupply bool
}

// ErrVendorSideForbidden refuses a request for a register half the caller does not hold.
var ErrVendorSideForbidden = errors.New("procurement: vendor register side not granted")

type vendorSideAccessKey struct{}

// WithVendorSideAccess attaches the caller's side access. The HTTP handler sets it on EVERY
// request; a context without it (an importer, a CLI, a service test) is not narrowed.
func WithVendorSideAccess(ctx context.Context, access VendorSideAccess) context.Context {
	return context.WithValue(ctx, vendorSideAccessKey{}, access)
}

func vendorSideAccessFrom(ctx context.Context) (VendorSideAccess, bool) {
	access, ok := ctx.Value(vendorSideAccessKey{}).(VendorSideAccess)
	return access, ok
}

func (a VendorSideAccess) reads(side string) bool {
	if side == domain.VendorSideSales {
		return a.ReadSales
	}
	return a.ReadSupply
}

func (a VendorSideAccess) writes(side string) bool {
	if side == domain.VendorSideSales {
		return a.WriteSales
	}
	return a.WriteSupply
}

// resolveReadSide turns the side a list/catalog/form request named into the side it may see. A
// named side the caller does not hold is refused. NO side means the whole register only for a
// caller holding both halves; a caller holding one half gets that half -- never the whole
// register, which is how a buyers-only person used to read every supplier.
func resolveReadSide(ctx context.Context, requested string) (string, error) {
	side, ok := domain.NormalizeVendorSide(requested)
	if !ok {
		return "", ErrVendorSideUnknown
	}
	access, narrowed := vendorSideAccessFrom(ctx)
	if !narrowed {
		return side, nil
	}
	if side != "" {
		if !access.reads(side) {
			return "", ErrVendorSideForbidden
		}
		return side, nil
	}
	switch {
	case access.ReadSales && access.ReadSupply:
		return "", nil
	case access.ReadSales:
		return domain.VendorSideSales, nil
	case access.ReadSupply:
		return domain.VendorSideProcurement, nil
	default:
		return "", ErrVendorSideForbidden
	}
}

// vendorSide is the register half a record type belongs to, from the catalog itself.
func (s *VendorService) vendorSide(ctx context.Context, tenantID, recordType string) (string, error) {
	catalog, err := s.repo.ListVendorCatalog(ctx, tenantID, false)
	if err != nil {
		return "", err
	}
	if side := domain.VendorSideForRecordType(catalog, strings.TrimSpace(recordType)); side != "" {
		return side, nil
	}
	return domain.VendorSideProcurement, nil
}

// requireVendorRead hides a vendor on a half the caller does not hold: NOT FOUND, never a
// "forbidden" that would confirm the id exists.
func (s *VendorService) requireVendorRead(ctx context.Context, tenantID string, v domain.Vendor) error {
	access, narrowed := vendorSideAccessFrom(ctx)
	if !narrowed {
		return nil
	}
	side, err := s.vendorSide(ctx, tenantID, v.RecordType)
	if err != nil {
		return err
	}
	if !access.reads(side) {
		return ports.ErrVendorNotFound
	}
	return nil
}

// requireVendorWrite refuses a write onto a half the caller may not write.
func (s *VendorService) requireVendorWrite(ctx context.Context, tenantID, recordType string) error {
	access, narrowed := vendorSideAccessFrom(ctx)
	if !narrowed {
		return nil
	}
	side, err := s.vendorSide(ctx, tenantID, recordType)
	if err != nil {
		return err
	}
	if !access.writes(side) {
		return ErrVendorSideForbidden
	}
	return nil
}
