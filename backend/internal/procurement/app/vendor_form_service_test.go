package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// formVendorRepo records the write the service hands it; every other method is unused here.
type formVendorRepo struct {
	ports.VendorRepository
	created domain.VendorWrite
	updated domain.VendorWrite
}

func (r *formVendorRepo) CreateVendor(_ context.Context, _ string, w domain.VendorWrite, _ string) (domain.Vendor, error) {
	r.created = w
	return domain.Vendor{VendorID: "v1", BusinessName: w.BusinessName, SOPAnswers: w.SOPAnswers}, nil
}

func (r *formVendorRepo) UpdateVendor(_ context.Context, _, _ string, w domain.VendorWrite, _ int64, _, _ string, _ bool) (domain.Vendor, error) {
	r.updated = w
	return domain.Vendor{VendorID: "v1", BusinessName: w.BusinessName}, nil
}

func (r *formVendorRepo) ListVendorCatalog(context.Context, string, bool) ([]domain.VendorCatalogEntry, error) {
	return []domain.VendorCatalogEntry{
		{Kind: domain.CatalogKindRecordType, Value: "Agent", Label: "Agent", IsActive: true, RegisterSide: "sales"},
		{Kind: domain.CatalogKindRecordType, Value: "Feed Agent", Label: "Feed Agent", IsActive: true, RegisterSide: "procurement"},
		{Kind: domain.CatalogKindState, Value: "Karnataka", Label: "Karnataka", IsActive: true},
		{Kind: domain.CatalogKindStatus, Value: "active", Label: "Active", IsActive: true},
	}, nil
}

// formSource serves the seeded document plus one extra page as version 2, and only that version.
type formSource struct{}

func v2DSL() domain.VendorFormDSL {
	dsl := domain.SeededVendorFormDSL()
	dsl.Pages = append(dsl.Pages, domain.VendorFormPage{Key: "extra", Title: "Extra", Questions: []domain.VendorQuestion{
		{ID: "transport", Kind: domain.VendorQuestionChoice, Title: "Own transport?", Required: true,
			Options: []domain.VendorQuestionOpt{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
	}})
	return dsl
}

func (formSource) PublishedVendorForm(_ context.Context, _ string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return domain.CompileVendorForm(v2DSL(), 2, catalog), nil
}

func (formSource) VendorFormVersion(_ context.Context, _ string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	if version != 2 {
		return domain.VendorForm{}, ports.ErrVendorFormVersionUnknown
	}
	return domain.CompileVendorForm(v2DSL(), 2, catalog), nil
}

func formAnswers() map[string]string {
	return map[string]string{
		"business_name": "Bhopal Goat", "record_type": "Agent", "contact_person_name": "Sammer",
		"phone_number": "9999999999", "state": "Karnataka", "city": "Bhopal", "status": "active",
		"transport": "yes",
	}
}

// TestCreateVendorFromFormAnswersMapsTypedAndStoresExtras: a form-driven create lands the typed
// answers on the register's columns and keeps only the extra ones as sop_answers, stamped with
// the version they were answered on.
func TestCreateVendorFromFormAnswersMapsTypedAndStoresExtras(t *testing.T) {
	repo := &formVendorRepo{}
	svc := NewVendorService(repo).WithVendorFormSource(formSource{})
	_, err := svc.CreateVendor(context.Background(), "t", domain.VendorWrite{SOPAnswers: formAnswers(), QuestionnaireVersion: 2}, "actor", true)
	if err != nil {
		t.Fatal(err)
	}
	if repo.created.BusinessName != "Bhopal Goat" || repo.created.RecordType != "Agent" || repo.created.City != "Bhopal" {
		t.Fatalf("typed answers not on the write: %+v", repo.created)
	}
	if len(repo.created.SOPAnswers) != 1 || repo.created.SOPAnswers["transport"] != "yes" || repo.created.QuestionnaireVersion != 2 {
		t.Fatalf("extras = %v v%d", repo.created.SOPAnswers, repo.created.QuestionnaireVersion)
	}
}

// TestCreateVendorFromFormRefusesAMissingRequiredExtra: the form's own compulsory question is
// enforced on the write, not only the register's columns.
func TestCreateVendorFromFormRefusesAMissingRequiredExtra(t *testing.T) {
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(formSource{})
	a := formAnswers()
	delete(a, "transport")
	_, err := svc.CreateVendor(context.Background(), "t", domain.VendorWrite{SOPAnswers: a, QuestionnaireVersion: 2}, "actor", true)
	var refused domain.ErrVendorAnswer
	if !errors.As(err, &refused) || refused.QuestionID != "transport" {
		t.Fatalf("err = %v, want a refused transport answer", err)
	}
	if VendorHTTPError(err).Field != "transport" {
		t.Fatalf("transport error does not name its field: %+v", VendorHTTPError(err))
	}
}

// TestCreateVendorFromFormRefusesAVersionNoLongerServed: answers on a form version the library
// does not carry are refused as a conflict (reopen the form), never accepted against another.
func TestCreateVendorFromFormRefusesAVersionNoLongerServed(t *testing.T) {
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(formSource{})
	_, err := svc.CreateVendor(context.Background(), "t", domain.VendorWrite{SOPAnswers: formAnswers(), QuestionnaireVersion: 1}, "actor", true)
	if !errors.Is(err, ports.ErrVendorFormVersionUnknown) {
		t.Fatalf("err = %v", err)
	}
	if got := VendorHTTPError(err); got.Code != "vendor_form_changed" || got.HTTPStatus != 409 {
		t.Fatalf("http = %+v", got)
	}
	_, err = svc.CreateVendor(context.Background(), "t", domain.VendorWrite{SOPAnswers: formAnswers()}, "actor", true)
	if !errors.Is(err, ErrVendorFormVersionRequired) {
		t.Fatalf("answers without a version accepted: %v", err)
	}
}

// TestUpdateVendorWithoutAnswersPreservesStoredExtras: a typed-only client (older phone, the
// importer) never rendered the form, so its replace carries nil answers and the repository is
// told to keep what is stored.
func TestUpdateVendorWithoutAnswersPreservesStoredExtras(t *testing.T) {
	repo := &formVendorRepo{}
	svc := NewVendorService(repo).WithVendorFormSource(formSource{})
	w := domain.VendorWrite{BusinessName: "Bhopal Goat", RecordType: "Agent", State: "Karnataka", Status: "active"}
	if _, err := svc.UpdateVendor(context.Background(), "t", "v1", w, 3, "actor", "key", true); err != nil {
		t.Fatal(err)
	}
	if repo.updated.SOPAnswers != nil {
		t.Fatalf("typed-only update must carry nil answers so the repository preserves them, got %v", repo.updated.SOPAnswers)
	}
}

// TestVendorFormNarrowsRecordTypesToTheSide: the sales register's form offers sales buyer types
// only, the same narrowing the catalog read applies.
func TestVendorFormNarrowsRecordTypesToTheSide(t *testing.T) {
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(formSource{})
	form, err := svc.VendorForm(context.Background(), "t", "sales")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range form.Questions() {
		if q.ID == "record_type" {
			if len(q.Options) != 1 || q.Options[0].Value != "Agent" {
				t.Fatalf("sales record types = %+v", q.Options)
			}
		}
	}
	if _, err := svc.VendorForm(context.Background(), "t", "nonsense"); !errors.Is(err, ErrVendorSideUnknown) {
		t.Fatalf("unknown side accepted: %v", err)
	}
}
