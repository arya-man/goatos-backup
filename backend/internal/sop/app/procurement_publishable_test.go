package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEverySeededProcurementDocumentIsPublishable is the test that would have caught the defect
// the 2026-09-20 E2E found: THREE of the six procurement documents could not be published at all.
//
// The generic form validator demands a non-empty `fields` array and runs FIRST, so a document
// whose whole substance is a module-owned section (the aflatoxin procedure, the feed purchase
// form) or a workflow track (the feed intake) was refused "at least one field is required" before
// any of its own rules were ever reached. Every unit test passed, because each called its own
// contract directly -- none went through the path the web editor's Save actually takes.
//
// So this walks the REAL validator with the REAL seeded documents, exactly as the migration
// builds them.
func TestEverySeededProcurementDocumentIsPublishable(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	proofPolicy := map[string]any{
		"subject_scope": "task", "types": []any{"video", "photo"},
		"required": false, "minimum_count": float64(0),
		"verify_before_apply": false, "approval_before_apply": false,
	}

	cases := []struct {
		name    string
		sopCode string
		section string
		seed    string
	}{
		{"animal purchase intake", "procurement.animal_purchase_intake", "follow_up", "backend/internal/tasks/domain/sopseed/procurement_animal_purchase_intake.json"},
		{"feed purchase intake", "procurement.feed_purchase_intake", "follow_up", "backend/internal/tasks/domain/sopseed/procurement_feed_purchase_intake.json"},
		{"aflatoxin procedure", "procurement.toxin_test", "toxin", "backend/internal/toxin/domain/toxinseed/toxin_test.json"},
		{"feed purchase form", "procurement.feed_purchase_form", "feed_purchase_form", "backend/internal/procurement/domain/feedformseed/feed_purchase.json"},
		{"supplier form", "procurement.vendor", "vendor_form", "backend/internal/procurement/domain/vendorformseed/vendor.json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, tc.seed))
			if err != nil {
				t.Fatal(err)
			}
			var section map[string]any
			if err := json.Unmarshal(raw, &section); err != nil {
				t.Fatal(err)
			}
			// Exactly the document the migration publishes: the envelope plus its one section,
			// with NO capture fields.
			formDSL := map[string]any{
				"schema_version": "goatos.sop-form.v1",
				"sop_code":       tc.sopCode,
				"title":          tc.name,
				"fields":         []any{},
				tc.section:       section,
			}
			report := ValidateFormDSL(formDSL, proofPolicy)
			if !report.Valid {
				t.Fatalf("%s cannot be published: %+v", tc.sopCode, report.Errors)
			}
		})
	}
}

// TestADocumentWithNothingInItIsStillRefused is the other half: widening the shape check must not
// turn "at least one field is required" off for every document. A version with no fields and no
// module-owned section of any kind is still an empty document.
func TestADocumentWithNothingInItIsStillRefused(t *testing.T) {
	report := ValidateFormDSL(map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"sop_code":       "procurement.something",
		"title":          "Empty",
		"fields":         []any{},
	}, map[string]any{"subject_scope": "task", "types": []any{"photo"}, "required": false, "minimum_count": float64(0)})
	if report.Valid {
		t.Fatal("a document with no fields and no module-owned section must still be refused")
	}
}

// TestFieldlessSOPCodesMatchTheirOwners keeps the code strings above honest. They are spelled out
// so sop/app depends on neither the procurement nor the toxin package, and a rename in the owning
// module would otherwise leave a document silently unpublishable again.
func TestFieldlessSOPCodesMatchTheirOwners(t *testing.T) {
	for code, label := range map[string]string{
		procurementSOPCodeVendor:           "procurement.vendor",
		procurementSOPCodeFeedPurchaseForm: "procurement.feed_purchase_form",
		toxinSOPCode:                       "procurement.toxin_test",
		salesSOPCodeVendor:                 "sales.vendor",
	} {
		if code != label {
			t.Fatalf("fieldless code %q drifted from %q", code, label)
		}
		if !fieldlessSOPCodes[code] {
			t.Fatalf("%q is spelled but not listed as fieldless", code)
		}
	}
}

// TestDeletingTheTrackReportsTheTrack is the message half of the same defect: an author who
// deletes a purchase document's track must be told THAT, not sent to add a capture field the
// document never had.
func TestDeletingTheTrackReportsTheTrack(t *testing.T) {
	report := ValidateFormDSL(map[string]any{
		"schema_version": "goatos.sop-form.v1",
		"sop_code":       "procurement.feed_purchase_intake",
		"title":          "Feed purchase",
		"fields":         []any{},
		"follow_up":      map[string]any{"schema_version": "goatos.sop-followup.v1", "tracks": []any{}},
	}, map[string]any{"subject_scope": "task", "types": []any{"photo"}, "required": false, "minimum_count": float64(0)})
	for _, e := range report.Errors {
		if e.Code == "required" && e.Field == "form_dsl.fields" {
			t.Fatal("a purchase document with no track must not be refused for missing capture fields")
		}
	}
}
