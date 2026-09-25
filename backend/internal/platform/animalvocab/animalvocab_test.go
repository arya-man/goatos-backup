package animalvocab

import "testing"

func TestHasAndResolveReadTheConfiguredListNotAConstantPair(t *testing.T) {
	species := []Entry{{Code: "goat", Name: "Goat"}, {Code: "sheep", Name: "Sheep"}, {Code: "alpaca", Name: "Alpaca"}}
	if !Has(species, "alpaca") {
		t.Fatal("a third species from Configuration must be accepted")
	}
	if Has(species, "camel") {
		t.Fatal("a code no active row carries must be refused")
	}
	if code, ok := Resolve(species, " Alpaca "); !ok || code != "alpaca" {
		t.Fatalf("Resolve by name = %q, %v; want alpaca, true", code, ok)
	}
	if code, ok := Resolve(species, "SHEEP"); !ok || code != "sheep" {
		t.Fatalf("Resolve by code = %q, %v; want sheep, true", code, ok)
	}
	if _, ok := Resolve(species, ""); ok {
		t.Fatal("a blank value must not resolve")
	}
	if got := Label(species, "alpaca"); got != "Alpaca" {
		t.Fatalf("Label = %q", got)
	}
	if got := Label(species, "old_breed_line"); got != "old breed line" {
		t.Fatalf("Label of an archived code = %q", got)
	}
}

func TestBuiltinsAreTheFourPreConfigurationCodes(t *testing.T) {
	b := Builtins()
	if got := Codes(b.Species); len(got) != 2 || got[0] != "goat" || got[1] != "sheep" {
		t.Fatalf("species = %v", got)
	}
	if got := Codes(b.Sexes); len(got) != 2 || got[0] != "female" || got[1] != "male" {
		t.Fatalf("sexes = %v", got)
	}
}

func TestValidCodeShape(t *testing.T) {
	for _, ok := range []string{"goat", "water_buffalo", "x1"} {
		if !ValidCodeShape(ok) {
			t.Fatalf("%q must be a valid shape", ok)
		}
	}
	for _, bad := range []string{"", "Goat", "1goat", "goat sheep"} {
		if ValidCodeShape(bad) {
			t.Fatalf("%q must not be a valid shape", bad)
		}
	}
}
