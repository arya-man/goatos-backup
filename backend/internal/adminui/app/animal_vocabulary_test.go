package app

import (
	"context"
	"testing"
)

type vocabularyFamilies struct{ fakeFamilies }

func (vocabularyFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	f, err := fakeFamilies{}.LoadContractFamilies(ctx, tenantID)
	f.AllBreeds = []ReferenceOption{{Key: "Sojat", Label: "Sojat"}, {Key: "Nellore", Label: "Nellore"}}
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
// literal: a breed added to the register is offered at once.
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
