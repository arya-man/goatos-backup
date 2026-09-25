package app

import (
	"context"
	"testing"
)

type threeParkFamilies struct{ fakeFamilies }

func (threeParkFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	f, err := fakeFamilies{}.LoadContractFamilies(ctx, tenantID)
	f.Parks = []ReferenceOption{
		{Key: "p-cbe", Label: "CBE", Title: "Coimbatore", Tone: "info", Code: "CBE"},
		{Key: "p-cpt", Label: "CPT", Title: "Channapatna", Tone: "info", Code: "CPT"},
		{Key: "p-hsr", Label: "HSR", Title: "Hosur", Tone: "info", Code: "HSR"},
	}
	return f, err
}

// The sales and feed purchase farm pickers are the tenant's parks, so a park added on
// Configuration > Items & settings is offered on every page that carries them -- never a CBE/CPT
// pair in contract code.
func TestFarmPickersOfferEveryParkFromConfiguration(t *testing.T) {
	resp := NewService(threeParkFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	want := []string{"all", "CBE", "CPT", "HSR"}
	checked := 0
	for _, page := range resp.Pages {
		for _, g := range page.OptionGroups {
			if g.ID != "sales_farms" && g.ID != "feed_purchase_farms" {
				continue
			}
			checked++
			if len(g.Options) != len(want) {
				t.Fatalf("%s %s = %+v, want %v", page.RouteID, g.ID, g.Options, want)
			}
			for i, key := range want {
				if g.Options[i].Key != key {
					t.Fatalf("%s %s option %d = %q, want %q", page.RouteID, g.ID, i, g.Options[i].Key, key)
				}
			}
		}
	}
	if checked < 5 {
		t.Fatalf("expected the farm picker on every sales page and feed purchases, checked %d", checked)
	}
}
