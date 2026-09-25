package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeededFeedPurchaseFormAsksEveryTypedColumn is the day-one claim: the seeded document asks
// for everything the hard-coded drawer asked for, so a form-driven client records no less than
// the screens did before this.
func TestSeededFeedPurchaseFormAsksEveryTypedColumn(t *testing.T) {
	dsl := SeededFeedPurchaseFormDSL()
	if problems := ValidateFeedPurchaseForm(dsl); len(problems) > 0 {
		t.Fatalf("seeded feed purchase form invalid: %v", problems)
	}
	asked := map[string]bool{}
	for _, p := range dsl.Pages {
		for _, q := range p.Questions {
			asked[q.ID] = true
		}
	}
	for _, id := range []string{
		"purchase_date", "farm_label", "feed_item_label", "quantity_kg", "vendor",
		"feed_cost", "transport_cost", "loading_cost", "unloading_cost", "total_cost",
		"payment_released", "payment_status", "reached_on", "reached_weight_kg",
	} {
		if !asked[id] {
			t.Fatalf("the seeded form does not ask for %q, which the ledger stores", id)
		}
	}
}

// TestFeedPurchaseFormRefusesWhatTheLedgerCannotRun: the locked ids are the columns every
// downstream read is keyed on or divided by, so their kind and catalog are not the author's.
func TestFeedPurchaseFormRefusesWhatTheLedgerCannotRun(t *testing.T) {
	cases := map[string]struct {
		mutate func(*VendorFormDSL)
		want   string
	}{
		"typed kind changed":     {func(d *VendorFormDSL) { d.Pages[0].Questions[3].Kind = VendorQuestionText }, `"quantity_kg" is fixed to "number"`},
		"typed catalog changed":  {func(d *VendorFormDSL) { d.Pages[0].Questions[2].Catalog = CatalogKindFarm }, `takes its choices from the "feed_item" catalog`},
		"identity made optional": {func(d *VendorFormDSL) { d.Pages[0].Questions[0].Required = false }, "must stay compulsory"},
		"identity dropped":       {func(d *VendorFormDSL) { d.Pages[0].Questions = d.Pages[0].Questions[1:] }, `question "purchase_date" must be present`},
		"foreign catalog":        {func(d *VendorFormDSL) { d.Pages[0].Questions[1].Catalog = CatalogKindBreed }, "not a catalog this form can read"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dsl := SeededFeedPurchaseFormDSL()
			tc.mutate(&dsl)
			problems := ValidateFeedPurchaseForm(dsl)
			found := false
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %v, want one containing %q", problems, tc.want)
			}
		})
	}
}

// TestApplyFeedPurchaseAnswersFillsTheColumnsAndKeepsTheRest proves the two halves: a typed
// answer lands in the ledger's own column, an authored one is kept as an extra, and a BLANK
// answer leaves the column alone so an older client sending typed fields only still works.
func TestApplyFeedPurchaseAnswersFillsTheColumnsAndKeepsTheRest(t *testing.T) {
	base := FeedPurchaseWrite{PurchaseDate: "2026-09-01", FarmLabel: "CBE", FeedItemLabel: "Maize", QuantityKg: 1000, Vendor: "Agri Traders"}
	got, extras := ApplyFeedPurchaseAnswers(base, map[string]string{
		"quantity_kg":    "1500",
		"transport_cost": "4200.50",
		"payment_status": "Pending",
		"reached_on":     "",
		"lorry_number":   "KA 05 AB 1234",
		"driver_phone":   "  ",
	})
	if got.QuantityKg != 1500 {
		t.Fatalf("quantity = %v", got.QuantityKg)
	}
	if got.TransportCost == nil || *got.TransportCost != 4200.50 {
		t.Fatalf("transport = %v", got.TransportCost)
	}
	if got.PaymentStatus != "Pending" {
		t.Fatalf("payment status = %q", got.PaymentStatus)
	}
	if got.ReachedOn != "" {
		t.Fatalf("a blank answer must leave the column alone, got %q", got.ReachedOn)
	}
	if extras["lorry_number"] != "KA 05 AB 1234" {
		t.Fatalf("extras = %v", extras)
	}
	if _, ok := extras["driver_phone"]; ok {
		t.Fatal("a blank authored answer must not be stored as an empty extra")
	}
	if _, ok := extras["quantity_kg"]; ok {
		t.Fatal("a typed answer must not also be stored as an extra")
	}
}

// TestFeedPurchaseFormCatalogComesFromTheLedgersOwnVocabularies: the choices are the farms it buys
// for, the ACTIVE feed catalog and the two payment states -- never a list typed into a document.
func TestFeedPurchaseFormCatalogComesFromTheLedgersOwnVocabularies(t *testing.T) {
	catalog := FeedPurchaseFormCatalog(testFeedFarms, []string{"Maize", "Soya"}, FeedPaymentStatuses)
	form := CompileVendorForm(SeededFeedPurchaseFormDSL(), 1, catalog)
	byID := map[string][]VendorQuestionOpt{}
	for _, q := range form.Questions() {
		byID[q.ID] = q.Options
	}
	if len(byID["farm_label"]) != len(testFeedFarms) {
		t.Fatalf("farm choices = %v", byID["farm_label"])
	}
	if len(byID["feed_item_label"]) != 2 {
		t.Fatalf("feed choices = %v", byID["feed_item_label"])
	}
	if len(byID["payment_status"]) != len(FeedPaymentStatuses) {
		t.Fatalf("payment choices = %v", byID["payment_status"])
	}
}

// TestMigrationEmbedsTheSeededFeedPurchaseForm pins migration 000376 to the seed file byte for
// byte, so the document the tests reason about is the document the database is seeded from.
func TestMigrationEmbedsTheSeededFeedPurchaseForm(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000376_feed_purchase_form_sop.sql"))
	if err != nil {
		t.Fatal(err)
	}
	seed := strings.TrimSpace(string(SeededFeedPurchaseFormJSON()))
	if !strings.Contains(string(raw), "$seed$"+seed+"$seed$") {
		t.Fatal("migration 000376 does not embed feedformseed/feed_purchase.json verbatim; regenerate the SQL")
	}
	// The columns that let a load keep the answers and the version it was recorded on are part of
	// the same migration: without them the form could ask a question nothing could store.
	for _, col := range []string{"sop_answers", "questionnaire_version"} {
		if !strings.Contains(string(raw), col) {
			t.Fatalf("migration 000376 does not add %s", col)
		}
	}
}
