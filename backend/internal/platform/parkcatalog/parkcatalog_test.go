package parkcatalog

import "testing"

func TestHasAndResolveReadTheCatalogNotAConstantPair(t *testing.T) {
	parks := []Park{{Code: "CBE", Name: "Coimbatore"}, {Code: "CPT", Name: "Channapatna"}, {Code: "HSR", Name: "Hosur"}}
	if !Has(parks, "HSR") {
		t.Fatal("a third park from the catalog must be accepted")
	}
	if Has(parks, "hsr") {
		t.Fatal("Has compares the stored form exactly")
	}
	if code, ok := Resolve(parks, " hsr "); !ok || code != "HSR" {
		t.Fatalf("Resolve(hsr) = %q, %v; want HSR, true", code, ok)
	}
	if _, ok := Resolve(parks, "XYZ"); ok {
		t.Fatal("a code no park carries must not resolve")
	}
	if got := Codes(parks); len(got) != 3 || got[2] != "HSR" {
		t.Fatalf("Codes = %v", got)
	}
}
