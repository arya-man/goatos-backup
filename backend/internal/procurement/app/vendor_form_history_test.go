package app

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"testing"
)

func TestLegacySupplierHistoryKeepsSalesDocument(t *testing.T) {
	src := &recordingFormSource{}
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(src)
	version := 7
	svc.VendorAnswerRows(context.Background(), "t1", domain.Vendor{RecordType: "Feed Agent", QuestionnaireVersion: &version, SOPAnswers: map[string]string{"legacy_question": "yes"}})
	if len(src.versioned) != 1 || src.versioned[0] != domain.SOPCodeVendor {
		t.Fatalf("historical supplier resolved %v; want sales.vendor", src.versioned)
	}
}

func TestVendorWritePersistsRenderedDocument(t *testing.T) {
	for _, code := range []string{"", domain.SOPCodeVendor, domain.SOPCodeProcurementVendor} {
		repo := &formVendorRepo{}
		svc := NewVendorService(repo).WithVendorFormSource(formSource{})
		answers := formAnswers()
		answers["record_type"] = "Feed Agent"
		_, err := svc.CreateVendor(context.Background(), "t1", domain.VendorWrite{SOPAnswers: answers, QuestionnaireVersion: 2, QuestionnaireSOPCode: code}, "", true)
		if err != nil {
			t.Fatal(err)
		}
		want := code
		if want == "" {
			want = domain.SOPCodeVendor
		}
		if repo.created.QuestionnaireSOPCode != want {
			t.Fatalf("code=%q got %q want %q", code, repo.created.QuestionnaireSOPCode, want)
		}
	}
}

// The two documents may both carry version 2 with different question titles.
// Record type changes must never relabel an answer against the other document.
type historicalVendorSource struct{ formSource }

func (historicalVendorSource) VendorFormVersion(_ context.Context, _ string, code string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	dsl := v2DSL()
	dsl.Pages[len(dsl.Pages)-1].Questions[0].Title = code + " transport"
	return domain.CompileVendorForm(dsl, version, catalog), nil
}
func TestVendorHistoryKeepsLabelsWhenRecordTypeChanges(t *testing.T) {
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(historicalVendorSource{})
	version := 2
	for _, code := range []string{domain.SOPCodeVendor, domain.SOPCodeProcurementVendor} {
		for _, recordType := range []string{"Agent", "Feed Agent"} {
			rows := svc.VendorAnswerRows(context.Background(), "t1", domain.Vendor{RecordType: recordType, QuestionnaireSOPCode: code, QuestionnaireVersion: &version, SOPAnswers: map[string]string{"transport": "yes"}})
			if len(rows) != 1 || rows[0].Label != code+" transport" {
				t.Fatalf("%s %s labels=%+v", code, recordType, rows)
			}
		}
	}
}
