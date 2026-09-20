package app

import (
	"context"
	"strings"
	"testing"
)

// TestSupplierFormEditorSaysSupplierWhileTheBuyerRegisterKeepsItsOwnWords pins the 2026-09-20
// split on the shared pages-of-questions editor.
//
// ONE EDITOR, TWO REGISTERS. `procurement.vendor` asks about a SUPPLIER and `sales.vendor` about a
// BUYER, and both are authored through the same component -- so the copy is the only thing that can
// tell the buying desk which register it is changing. Before this, /procurement/sops greeted it
// with "Vendor form -- what is asked when a vendor or buyer is added", which names the other side's
// job on a page whose own breadcrumb says SUPPLIER FORM.
//
// Mutation-tested when written: removing either page override turns this red, and so does copying
// the supplier wording onto sales-sops.
func TestSupplierFormEditorSaysSupplierWhileTheBuyerRegisterKeepsItsOwnWords(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	buying := pageByRouteID(t, resp.Pages, "procurement-sops")
	selling := pageByRouteID(t, resp.Pages, "sales-sops")

	if got := buying.Copy["vendor_form.title"]; got != "Supplier form" {
		t.Fatalf("buying desk editor title = %q, want %q", got, "Supplier form")
	}
	if got := buying.Copy["vendor_form.subtitle"]; !strings.Contains(got, "supplier") || strings.Contains(got, "buyer") {
		t.Fatalf("buying desk editor subtitle must name a supplier and not a buyer, got %q", got)
	}
	if got := buying.Copy["vendor_form.notice.capture_kept"]; !strings.Contains(got, "supply register") {
		t.Fatalf("buying desk locked-column notice must name the supply register, got %q", got)
	}
	if got := selling.Copy["vendor_form.title"]; got == "Supplier form" {
		t.Fatalf("the buyer register must keep its own title, got %q", got)
	}

	// An entry form is not an INSPECTION: nobody inspects a supplier or a feed load, so each
	// profile names the reader it actually has. All three keys must exist on the page that hosts
	// all three editors, because a fixed contract key that is absent renders a hard failure.
	for _, key := range []string{"inspection.question.title", "vendor_form.question.title", "feed_form.question.title"} {
		if strings.TrimSpace(buying.Copy[key]) == "" {
			t.Fatalf("procurement-sops is missing the question label %q", key)
		}
	}
	for _, key := range []string{"vendor_form.question.title", "feed_form.question.title"} {
		if strings.Contains(buying.Copy[key], "inspector") {
			t.Fatalf("%s must not name an inspector, got %q", key, buying.Copy[key])
		}
	}

	// A page holding one question read "1 questions": both forms are backend-owned.
	if buying.Copy["label.inspection_question"] == "" || buying.Copy["label.inspection_page"] == "" {
		t.Fatal("procurement-sops must carry the singular question/page labels")
	}
	if buying.Copy["label.inspection_question"] == buying.Copy["label.inspection_questions"] {
		t.Fatal("the singular and plural question labels must differ")
	}
}
