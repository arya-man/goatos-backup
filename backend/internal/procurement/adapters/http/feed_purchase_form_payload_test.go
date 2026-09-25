package http

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// TestEachFormMarksItsOwnTypedColumns pins the defect the 2026-09-20 E2E found by SERVING the
// form: the feed purchase form was composed with the VENDOR register's typed predicate, so every
// one of the ledger's own columns came back `typed: false`.
//
// That flag is not decoration. The web drawer keeps purpose-built inputs for the typed columns
// and renders everything else from the document, so a ledger column marked untyped renders
// TWICE -- once as the drawer's own input and once as an "extra".
func TestEachFormMarksItsOwnTypedColumns(t *testing.T) {
	catalog := domain.FeedPurchaseFormCatalog([]string{"CBE", "CPT"}, []string{"Maize"}, domain.FeedPaymentStatuses)
	feed := toFeedPurchaseFormPayload(domain.CompileVendorForm(domain.SeededFeedPurchaseFormDSL(), 1, catalog))
	for _, page := range feed.Pages {
		for _, q := range page.Questions {
			if !q.Typed {
				t.Fatalf("feed purchase question %q is one of the ledger's own columns but came back typed=false", q.ID)
			}
		}
	}

	vendor := toVendorFormPayload(domain.CompileVendorForm(domain.SeededVendorFormDSL(), 1, nil))
	for _, page := range vendor.Pages {
		for _, q := range page.Questions {
			if !q.Typed {
				t.Fatalf("vendor question %q is one of the register's own columns but came back typed=false", q.ID)
			}
		}
	}

	// And the predicates are NOT interchangeable: a feed column is not a vendor column.
	if domain.IsTypedVendorQuestion("quantity_kg") {
		t.Fatal("quantity_kg is a feed ledger column, not a vendor register column")
	}
	if domain.IsTypedFeedPurchaseQuestion("business_name") {
		t.Fatal("business_name is a vendor register column, not a feed ledger column")
	}
}
