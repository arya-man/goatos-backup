package app

import (
	"context"
	"testing"
)

type vocabularyFamilies struct{ fakeFamilies }

func (vocabularyFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	f, err := fakeFamilies{}.LoadContractFamilies(ctx, tenantID)
	f.AllBreeds = []ReferenceOption{{Key: "Sojat", Label: "Sojat"}, {Key: "Nellore", Label: "Nellore"}}
	f.Species = []ReferenceOption{{Key: "goat", Label: "Goat"}, {Key: "sheep", Label: "Sheep"}, {Key: "alpaca", Label: "Alpaca"}}
	f.Sexes = []ReferenceOption{{Key: "female", Label: "Female"}, {Key: "male", Label: "Male"}, {Key: "castrated", Label: "Castrated male"}}
	return f, err
}

func vocabKeys(t *testing.T, resp map[string][]string, id string) []string {
	t.Helper()
	keys, ok := resp[id]
	if !ok {
		t.Fatalf("no page declares %s", id)
	}
	return keys
}

// The herd filter's breed choices are the breed register (every species), not a seven-breed
// literal: a breed added to the register is offered at once. OPEN UP TO NEW SPECIES (maintainer
// decision 2026-09-25): every species / sex picker is Configuration's list too, so a third species
// and a third gender reach the herd register, the procurement load form, the farm-born filters,
// the counts breakdown and the rule selectors (which lead with "all").
func TestAnimalPickersAreCompiledFromConfiguration(t *testing.T) {
	resp := NewService(vocabularyFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	groups := map[string][]string{}
	for _, page := range resp.Pages {
		for _, g := range page.OptionGroups {
			keys := make([]string, 0, len(g.Options))
			for _, o := range g.Options {
				keys = append(keys, o.Key)
			}
			groups[g.ID] = keys
		}
	}
	if got := vocabKeys(t, groups, "herd_filter_breeds"); len(got) != 2 || got[1] != "Nellore" {
		t.Fatalf("herd_filter_breeds = %v, want the breed register", got)
	}
	join := func(keys []string) string {
		out := ""
		for i, k := range keys {
			if i > 0 {
				out += ","
			}
			out += k
		}
		return out
	}
	for _, id := range []string{"herd_species", "proc_species", "farm_born_species", "assumption_species"} {
		if got := join(vocabKeys(t, groups, id)); got != "goat,sheep,alpaca" {
			t.Fatalf("%s = %s, want Configuration's species", id, got)
		}
	}
	for _, id := range []string{"herd_sex", "herd_filter_sexes", "proc_sex", "farm_born_sexes", "counts_gender", "assumption_sexes"} {
		if got := join(vocabKeys(t, groups, id)); got != "female,male,castrated" {
			t.Fatalf("%s = %s, want Configuration's genders", id, got)
		}
	}
	if got := join(vocabKeys(t, groups, "rule_species")); got != "all,goat,sheep,alpaca" {
		t.Fatalf("rule_species = %s, want all + Configuration's species", got)
	}
	if got := join(vocabKeys(t, groups, "rule_sexes")); got != "all,female,male,castrated" {
		t.Fatalf("rule_sexes = %s, want all + Configuration's genders", got)
	}
}

// A family that did not load keeps the contract's own choices rather than an empty dropdown.
func TestAnimalPickersFallBackWhenAFamilyIsUnavailable(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	for _, page := range resp.Pages {
		for _, g := range page.OptionGroups {
			if g.ID == "herd_filter_breeds" && len(g.Options) != 7 {
				t.Fatalf("herd_filter_breeds fallback = %+v", g.Options)
			}
		}
	}
}

type breedGroupFamilies struct{ fakeFamilies }

func (breedGroupFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	f, err := fakeFamilies{}.LoadContractFamilies(ctx, tenantID)
	f.AllBreeds = []ReferenceOption{{Key: "Beetal", Label: "Beetal", Group: "goat"}, {Key: "Huacaya", Label: "Huacaya", Group: "alpaca"}, {Key: "Kenguri", Label: "Kenguri", Group: "goat", Tone: "warn"}}
	return f, err
}

// BREEDS ARE PER FARM (2026-09-25): Register animal offers the farm's own breeds, and each carries
// its species so the form offers only the chosen species' breeds -- a goat is never registered as
// a Huacaya because the list mixed species. An archived breed (Kenguri) is not offered.
func TestRegisterAnimalBreedsCarryTheirSpecies(t *testing.T) {
	resp := NewService(breedGroupFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	for _, page := range resp.Pages {
		for _, g := range page.OptionGroups {
			if g.ID != "herd_breeds" {
				continue
			}
			if len(g.Options) != 2 || g.Options[0].Group != "goat" || g.Options[1].Key != "Huacaya" || g.Options[1].Group != "alpaca" {
				t.Fatalf("herd_breeds = %+v, want the farm's breeds each with its species", g.Options)
			}
			return
		}
	}
	t.Fatal("no page declares herd_breeds")
}
