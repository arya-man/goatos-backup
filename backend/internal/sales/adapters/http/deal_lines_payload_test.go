package http

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// The record-sale body accepts `lines` (the 2026-09-12 shape) AND the legacy single-product
// fields, and the response always carries the lines back -- an installed phone that still sends
// the old body must keep working while the new form sends lines.
func TestDealWriteBodyDecodesLinesAndLegacyShape(t *testing.T) {
	var withLines dealWritePayload
	if err := json.Unmarshal([]byte(`{
		"sale_date":"2026-09-12","farm":"CPT","buyer_name":"Tanveer","buyer_vendor_id":"3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		"lines":[
			{"product_type":"Sheep","breed":"Anantapur","animal_count":10,"total_weight_kg":300,"sales_value":120000},
			{"product_type":"Goat","breed":"Sirohi","animal_count":4,"total_weight_kg":100,"sales_value":45000}
		]}`), &withLines); err != nil {
		t.Fatal(err)
	}
	w := withLines.toDomain().Normalize(builtinCatalog())
	if err := w.Validate(builtinCatalog()); err != nil {
		t.Fatalf("lines body rejected: %v", err)
	}
	if len(w.Lines) != 2 || w.ProductType != domain.ProductMixed || w.SalesValue != 165000 {
		t.Fatalf("lines body -> %+v", w)
	}

	var legacy dealWritePayload
	if err := json.Unmarshal([]byte(`{
		"sale_date":"2026-09-12","farm":"CPT","buyer_name":"Tanveer","buyer_vendor_id":"3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		"product_type":"Goat","breed":"Sirohi","animal_count":4,"sales_value":45000}`), &legacy); err != nil {
		t.Fatal(err)
	}
	l := legacy.toDomain().Normalize(builtinCatalog())
	if err := l.Validate(builtinCatalog()); err != nil {
		t.Fatalf("legacy body rejected: %v", err)
	}
	if len(l.Lines) != 1 || l.Lines[0].Breed != "Sirohi" || l.ProductType != domain.ProductGoat {
		t.Fatalf("legacy body -> %+v", l)
	}
}

func TestDealPayloadCarriesLinesNeverNull(t *testing.T) {
	raw, err := json.Marshal(toDealPayload(domain.Deal{DealID: "d1"}))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if string(out["lines"]) != "[]" {
		t.Fatalf("lines = %s, want [] (a null list is a render crash on the phone)", out["lines"])
	}
	v := 1.0
	raw, _ = json.Marshal(toDealPayload(domain.Deal{DealID: "d1", Lines: []domain.DealLine{{LineID: "l1", LineNo: 1, ProductType: "Sheep", Breed: "Nipani", AnimalCount: &v, SalesValue: 5}}}))
	var decoded struct {
		Lines []dealLinePayload `json:"lines"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Lines) != 1 || decoded.Lines[0].Breed != "Nipani" || decoded.Lines[0].LineNo != 1 {
		t.Fatalf("lines round trip = %+v", decoded.Lines)
	}
}

// builtinCatalog is the registry every tenant starts with (migration 000422): the three products
// that used to be constants in the sales domain.
func builtinCatalog() domain.ProductCatalog {
	return domain.NewProductCatalog([]domain.Product{
		{Code: domain.ProductCodeSheep, Name: domain.ProductSheep, Kind: domain.KindAnimal, Unit: domain.UnitNumber, SpeciesCode: "sheep", SortOrder: 10},
		{Code: domain.ProductCodeGoat, Name: domain.ProductGoat, Kind: domain.KindAnimal, Unit: domain.UnitNumber, SpeciesCode: "goat", SortOrder: 20},
		{Code: domain.ProductCodeManure, Name: domain.ProductManure, Kind: domain.KindOther, Unit: "kg", SortOrder: 30},
	})
}
