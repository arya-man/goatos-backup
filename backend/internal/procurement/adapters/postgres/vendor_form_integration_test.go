package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// TestVendorFormAnswersRoundTripAndSurviveATypedOnlyUpdate proves the two halves of the vendor
// form's storage on a real Postgres: a form-driven write stores its extra answers and version and
// reads them back, and a typed-only replace (nil answers -- an older client) PRESERVES them while
// a form-driven replace REPLACES them. It also proves the seeded sales.vendor v1 is served by the
// form source and that a version the library does not carry is refused.
func TestVendorFormAnswersRoundTripAndSurviveATypedOnlyUpdate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	created, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
		RecordType: "Agent", BusinessName: "Form Buyer", ContactPersonName: "Asha", PhoneNumber: "9000000001",
		Status: "active", State: "Karnataka", City: "Mysuru",
		SOPAnswers: map[string]string{"transport": "yes", "vehicle_count": "3"}, QuestionnaireVersion: 2, QuestionnaireSOPCode: domain.SOPCodeVendor,
	}.Normalize(), "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.QuestionnaireSOPCode != domain.SOPCodeVendor || created.QuestionnaireVersion == nil || *created.QuestionnaireVersion != 2 || created.SOPAnswers["transport"] != "yes" || created.SOPAnswers["vehicle_count"] != "3" {
		t.Fatalf("answers did not round-trip: %v v%v", created.SOPAnswers, created.QuestionnaireVersion)
	}

	typedOnly := domain.VendorWrite{RecordType: "Agent", BusinessName: "Form Buyer", ContactPersonName: "Asha", PhoneNumber: "9000000001",
		Status: "active", State: "Karnataka", City: "Bengaluru"}.Normalize()
	after, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, typedOnly, created.RowVersion, "", "idem-typed-1", false)
	if err != nil {
		t.Fatalf("typed-only update: %v", err)
	}
	if after.City == nil || *after.City != "Bengaluru" {
		t.Fatalf("typed change lost: %+v", after.City)
	}
	if after.QuestionnaireSOPCode != domain.SOPCodeVendor || after.QuestionnaireVersion == nil || *after.QuestionnaireVersion != 2 || after.SOPAnswers["transport"] != "yes" {
		t.Fatalf("typed-only update cleared the answers: %v v%v", after.SOPAnswers, after.QuestionnaireVersion)
	}

	formDriven := typedOnly
	formDriven.SOPAnswers = map[string]string{"transport": "no"}
	formDriven.QuestionnaireVersion = 3
	formDriven.QuestionnaireSOPCode = domain.SOPCodeProcurementVendor
	replaced, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, formDriven, after.RowVersion, "", "idem-form-1", false)
	if err != nil {
		t.Fatalf("form-driven update: %v", err)
	}
	if replaced.QuestionnaireSOPCode != domain.SOPCodeProcurementVendor || len(replaced.SOPAnswers) != 1 || replaced.SOPAnswers["transport"] != "no" || *replaced.QuestionnaireVersion != 3 {
		t.Fatalf("form-driven update did not replace the answers: %v v%v", replaced.SOPAnswers, replaced.QuestionnaireVersion)
	}

	src := NewVendorFormSource(pool)
	catalog, err := repo.ListVendorCatalog(ctx, testTenant, false)
	if err != nil {
		t.Fatal(err)
	}
	form, err := src.PublishedVendorForm(ctx, testTenant, domain.SOPCodeVendor, catalog)
	if err != nil {
		t.Fatalf("published form: %v", err)
	}
	if form.Version != 1 || len(form.Pages) != 3 {
		t.Fatalf("published form = v%d with %d pages, want the seeded v1 with 3 pages", form.Version, len(form.Pages))
	}
	if _, err := src.VendorFormVersion(ctx, testTenant, domain.SOPCodeVendor, 9, catalog); err == nil {
		t.Fatal("unknown form version served")
	}
}
