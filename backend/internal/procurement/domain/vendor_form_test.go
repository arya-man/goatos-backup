package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

// TestMigrationEmbedsTheSeededVendorForm pins migration 000364 to vendorformseed/vendor.json byte
// for byte, and proves the seeded document is one the register can run.
func TestMigrationEmbedsTheSeededVendorForm(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000364_vendor_form_sop.sql"))
	if err != nil {
		t.Fatal(err)
	}
	seed := strings.TrimSpace(string(SeededVendorFormJSON()))
	if !strings.Contains(string(raw), "$seed$"+seed+"$seed$") {
		t.Fatal("migration 000364 does not embed vendorformseed/vendor.json verbatim; regenerate the SQL")
	}
	dsl := SeededVendorFormDSL()
	if problems := ValidateVendorForm(dsl); len(problems) > 0 {
		t.Fatalf("seeded vendor form invalid: %v", problems)
	}
	if len(dsl.Pages) != 3 {
		t.Fatalf("seeded form pages = %d, want 3 (the phone's three steps)", len(dsl.Pages))
	}
	// Every typed column the current write carries is asked somewhere, so a form-driven client
	// records no less than the hard-coded wizard did.
	asked := map[string]bool{}
	for _, q := range dsl.Pages[0].Questions {
		asked[q.ID] = true
	}
	for _, id := range []string{"business_name", "record_type", "contact_person_name", "phone_number", "state", "city", "status"} {
		if !asked[id] {
			t.Fatalf("seeded page 1 does not ask %q", id)
		}
	}
}

// TestValidateVendorFormRefusesWhatTheRegisterCannotRun covers the locked-id rules: a typed id
// with the wrong kind or catalog, a compulsory identity question made optional, and a missing one.
func TestValidateVendorFormRefusesWhatTheRegisterCannotRun(t *testing.T) {
	base := SeededVendorFormDSL()
	cases := map[string]struct {
		mutate func(*VendorFormDSL)
		want   string
	}{
		"typed kind changed":     {func(d *VendorFormDSL) { d.Pages[0].Questions[0].Kind = VendorQuestionNumber }, `"business_name" is fixed to "text"`},
		"typed catalog changed":  {func(d *VendorFormDSL) { d.Pages[0].Questions[1].Catalog = CatalogKindBreed }, `takes its choices from the "record_type" catalog`},
		"identity made optional": {func(d *VendorFormDSL) { d.Pages[0].Questions[0].Required = false }, "must stay compulsory"},
		"identity dropped":       {func(d *VendorFormDSL) { d.Pages[0].Questions = d.Pages[0].Questions[1:] }, `question "business_name" must be present`},
		"duplicate id": {func(d *VendorFormDSL) {
			d.Pages[2].Questions = append(d.Pages[2].Questions, VendorQuestion{ID: "comments", Kind: VendorQuestionText, Title: "Again"})
		}, `"comments" is used twice`},
		"only_if forward": {func(d *VendorFormDSL) {
			d.Pages[0].Questions[0].OnlyIf = &VendorQuestionOnlyIf{QuestionID: "record_type", Value: "agent"}
		}, "must be an EARLIER question"},
		"choice without options": {func(d *VendorFormDSL) {
			d.Pages[2].Questions = append(d.Pages[2].Questions, VendorQuestion{ID: "transport", Kind: VendorQuestionChoice, Title: "Transport"})
		}, "needs at least one choice"},
		"bad kind": {func(d *VendorFormDSL) {
			d.Pages[2].Questions = append(d.Pages[2].Questions, VendorQuestion{ID: "pic", Kind: "photo", Title: "Photo"})
		}, "is not a vendor-form question kind"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := clone(base)
			tc.mutate(&d)
			problems := ValidateVendorForm(d)
			if len(problems) == 0 {
				t.Fatal("expected a problem")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("problems %v do not mention %q", problems, tc.want)
			}
		})
	}
	// An added optional question of every allowed kind is fine.
	d := clone(base)
	d.Pages = append(d.Pages, VendorFormPage{Key: "extra", Title: "Extra", Questions: []VendorQuestion{
		{ID: "transport", Kind: VendorQuestionChoice, Title: "Own transport?", Options: []VendorQuestionOpt{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
		{ID: "vehicle_count", Kind: VendorQuestionNumber, Title: "Vehicles", Min: fp(0), OnlyIf: &VendorQuestionOnlyIf{QuestionID: "transport", Value: "yes"}},
		{ID: "languages", Kind: VendorQuestionMulti, Title: "Languages", Options: []VendorQuestionOpt{{Value: "kn", Label: "Kannada"}, {Value: "ta", Label: "Tamil"}}},
		{ID: "landmark", Kind: VendorQuestionText, Title: "Landmark"},
	}})
	if problems := ValidateVendorForm(d); len(problems) > 0 {
		t.Fatalf("added questions refused: %v", problems)
	}
}

func clone(d VendorFormDSL) VendorFormDSL {
	raw, _ := json.Marshal(d)
	var out VendorFormDSL
	_ = json.Unmarshal(raw, &out)
	return out
}

func compiledForm() VendorForm {
	d := clone(SeededVendorFormDSL())
	d.Pages = append(d.Pages, VendorFormPage{Key: "extra", Title: "Extra", Questions: []VendorQuestion{
		{ID: "transport", Kind: VendorQuestionChoice, Title: "Own transport?", Required: true, Options: []VendorQuestionOpt{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}, {Value: "other", Label: "Other"}}, AllowOther: true},
		{ID: "vehicle_count", Kind: VendorQuestionNumber, Title: "Vehicles", Required: true, Min: fp(1), Max: fp(50), Unit: "vehicles", OnlyIf: &VendorQuestionOnlyIf{QuestionID: "transport", Value: "yes"}},
		{ID: "languages", Kind: VendorQuestionMulti, Title: "Languages", Options: []VendorQuestionOpt{{Value: "kn", Label: "Kannada"}, {Value: "ta", Label: "Tamil"}}},
	}})
	return CompileVendorForm(d, 2, []VendorCatalogEntry{
		{Kind: CatalogKindRecordType, Value: "agent", Label: "Agent", IsActive: true},
		{Kind: CatalogKindRecordType, Value: "old", Label: "Old", IsActive: false},
		{Kind: CatalogKindState, Value: "karnataka", Label: "Karnataka", IsActive: true},
		{Kind: CatalogKindStatus, Value: "active", Label: "Active", IsActive: true},
	})
}

func okAnswers() map[string]string {
	return map[string]string{
		"business_name": "Bhopal Goat", "record_type": "agent", "contact_person_name": "Sammer",
		"phone_number": "9999999999", "state": "karnataka", "city": "Bhopal", "status": "active",
		"transport": "yes", "vehicle_count": "3", "languages": "kn|ta", "price_per_goat": "8500",
	}
}

// TestCompileVendorFormFillsCatalogChoicesActiveOnly: a catalog-backed question carries the live
// active entries; a retired one is not offered.
func TestCompileVendorFormFillsCatalogChoicesActiveOnly(t *testing.T) {
	form := compiledForm()
	var rt VendorQuestion
	for _, q := range form.Questions() {
		if q.ID == "record_type" {
			rt = q
		}
	}
	if len(rt.Options) != 1 || rt.Options[0].Value != "agent" {
		t.Fatalf("record_type options = %+v, want the one active entry", rt.Options)
	}
}

// TestValidateVendorAnswersRefusesEachBadShape covers required, hidden-by-only_if, choice set,
// other text, multi set, number range and an unknown question.
func TestValidateVendorAnswersRefusesEachBadShape(t *testing.T) {
	form := compiledForm()
	if err := ValidateVendorAnswers(form, okAnswers()); err != nil {
		t.Fatalf("good answers refused: %v", err)
	}
	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"required blank":       {func(a map[string]string) { a["business_name"] = "  " }, "business_name: required"},
		"choice not offered":   {func(a map[string]string) { a["record_type"] = "old" }, "record_type: must be one of"},
		"other without text":   {func(a map[string]string) { a["transport"] = "other"; delete(a, "vehicle_count") }, "transport: say what"},
		"multi not offered":    {func(a map[string]string) { a["languages"] = "kn|hi" }, "languages: must be among"},
		"number not numeric":   {func(a map[string]string) { a["vehicle_count"] = "three" }, "vehicle_count: must be a number"},
		"number over max":      {func(a map[string]string) { a["vehicle_count"] = "51" }, "vehicle_count: must be at most 50"},
		"unknown question":     {func(a map[string]string) { a["gst_number"] = "x" }, "gst_number: is not a question"},
		"required extra blank": {func(a map[string]string) { delete(a, "transport"); delete(a, "vehicle_count") }, "transport: required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := okAnswers()
			tc.mutate(a)
			err := ValidateVendorAnswers(form, a)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	// A question hidden by only_if is not owed even when required.
	a := okAnswers()
	a["transport"] = "no"
	delete(a, "vehicle_count")
	if err := ValidateVendorAnswers(form, a); err != nil {
		t.Fatalf("hidden required question demanded: %v", err)
	}
}

// TestApplyVendorAnswersSplitsTypedFromExtras: typed answers land on the register's columns,
// everything else becomes sop_answers, and answer rows label the extras by the form.
func TestApplyVendorAnswersSplitsTypedFromExtras(t *testing.T) {
	form := compiledForm()
	w, extras := ApplyVendorAnswers(VendorWrite{}, okAnswers())
	if w.BusinessName != "Bhopal Goat" || w.RecordType != "agent" || w.State != "karnataka" || w.City != "Bhopal" || w.Status != "active" {
		t.Fatalf("typed answers not applied: %+v", w)
	}
	if w.PricePerGoat == nil || *w.PricePerGoat != "8500" {
		t.Fatalf("price_per_goat not applied: %v", w.PricePerGoat)
	}
	if len(extras) != 3 || extras["transport"] != "yes" || extras["vehicle_count"] != "3" || extras["languages"] != "kn|ta" {
		t.Fatalf("extras = %v", extras)
	}
	rows := VendorAnswerRows(form, extras)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Label != "Own transport?" || rows[0].Value != "Yes" {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].Value != "3 vehicles" {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	if rows[2].Value != "Kannada, Tamil" {
		t.Fatalf("row 2 = %+v", rows[2])
	}
	// An answer whose question the form no longer carries is listed under its id, not dropped.
	rows = VendorAnswerRows(form, map[string]string{"gst_number": "29ABC"})
	if len(rows) != 1 || rows[0].Label != "gst_number" || rows[0].Value != "29ABC" {
		t.Fatalf("orphan row = %+v", rows)
	}
}
