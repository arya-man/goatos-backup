package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// recordingFormSource captures which SOP document the service asked for.
type recordingFormSource struct {
	published []string
	versioned []string
}

func (r *recordingFormSource) PublishedVendorForm(_ context.Context, _ string, sopCode string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	r.published = append(r.published, sopCode)
	return domain.CompileVendorForm(domain.SeededVendorFormDSL(), 1, catalog), nil
}

func (r *recordingFormSource) VendorFormVersion(_ context.Context, _ string, sopCode string, _ int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	r.versioned = append(r.versioned, sopCode)
	return domain.CompileVendorForm(domain.SeededVendorFormDSL(), 1, catalog), nil
}

var _ ports.VendorFormSource = (*recordingFormSource)(nil)

// TestVendorFormRoutesToTheRegisterSidesOwnDocument is the split's proof ON THE SERVICE PATH
// (maintainer decision 2026-09-20): the supply register renders and is judged against
// `procurement.vendor`, the sales one against `sales.vendor`, and a caller that names no side
// keeps the pre-split document. Mutation check: pinning either call back to
// domain.SOPCodeVendor turns this red.
func TestVendorFormRoutesToTheRegisterSidesOwnDocument(t *testing.T) {
	src := &recordingFormSource{}
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(src)
	ctx := context.Background()

	if _, err := svc.VendorForm(ctx, "t1", domain.VendorSideProcurement); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VendorForm(ctx, "t1", domain.VendorSideSales); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VendorForm(ctx, "t1", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{domain.SOPCodeProcurementVendor, domain.SOPCodeVendor, domain.SOPCodeVendor}
	if len(src.published) != len(want) {
		t.Fatalf("published reads = %v, want %v", src.published, want)
	}
	for i := range want {
		if src.published[i] != want[i] {
			t.Fatalf("read %d asked for %q, want %q", i, src.published[i], want[i])
		}
	}

	// A stored vendor's answers are labelled by the document ITS record type belongs to, resolved
	// from the catalog rather than from anything a client sent.
	version := 1
	svc.VendorAnswerRows(ctx, "t1", domain.Vendor{RecordType: "Feed Agent", QuestionnaireVersion: &version, SOPAnswers: map[string]string{"comments": "x"}})
	svc.VendorAnswerRows(ctx, "t1", domain.Vendor{RecordType: "Agent", QuestionnaireVersion: &version, SOPAnswers: map[string]string{"comments": "x"}})
	if len(src.versioned) != 2 || src.versioned[0] != domain.SOPCodeProcurementVendor || src.versioned[1] != domain.SOPCodeVendor {
		t.Fatalf("answer rows read %v, want [procurement.vendor sales.vendor]", src.versioned)
	}
}
