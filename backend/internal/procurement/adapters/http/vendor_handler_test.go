package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

type vendorOptionsRepo struct {
	ports.VendorRepository
	sawSide string
}

func (r *vendorOptionsRepo) ListVendorOptions(_ context.Context, _, side string) (domain.VendorOptions, error) {
	r.sawSide = side
	return domain.VendorOptions{Vendors: []domain.VendorOption{{VendorID: "buyer", BusinessName: "Buyer", RecordType: "Butcher"}}}, nil
}

func TestVendorOptionsSalesReadResolvesToBuyerSideOnly(t *testing.T) {
	repo := &vendorOptionsRepo{}
	svc := app.NewVendorService(repo)
	req := httptest.NewRequest(http.MethodGet, "/procurement/vendor-options", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), "11111111-1111-4111-8111-111111111111")
	ctx = httpmiddleware.WithPersonPermissions(ctx, []string{permissions.SalesRead})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	NewVendorHandler(svc).ListVendorOptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.sawSide != domain.VendorSideSales {
		t.Fatalf("options side = %q, want sales side for SalesRead picker", repo.sawSide)
	}
	var body vendorOptionsPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Vendors) != 1 || body.Vendors[0].VendorID != "buyer" {
		t.Fatalf("response vendors = %+v", body.Vendors)
	}
}
