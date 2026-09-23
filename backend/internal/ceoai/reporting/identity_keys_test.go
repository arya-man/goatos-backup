package reporting

import "testing"

// TestIdentityKeyColumnsNarrowsByTypeAndScope covers the two narrowings that no
// SHIPPED card exercises today and that a future card would otherwise discover
// the hard way: a uuid is not an identity KEY even when it is named like one,
// and a tenant-scoped column is never one at all. The filter path
// (app.normalizeIdentityFilters) rewrites the comparison on every column this
// returns, so a uuid or a tenant column here would rewrite a join predicate, or
// the predicate the guard binds the session tenant on.
func TestIdentityKeyColumnsNarrowsByTypeAndScope(t *testing.T) {
	card := SchemaCard{
		Name:                "synthetic",
		TenantScopedColumns: []string{"tenant_id", "owner_key"},
		Columns: []Column{
			{Name: "tenant_id", Type: uuidT},
			{Name: "animal_key", Type: textT},  // folded identity key
			{Name: "scanned_tag", Type: textT}, // folded identity key
			{Name: "owner_key", Type: textT},   // named like one, but tenant-scoped
			{Name: "campaign_key", Type: uuidT},
			{Name: "shed_id", Type: uuidT},
			{Name: "weight_kg", Type: numT},
			{Name: "park_label", Type: textT},
		},
	}
	got := card.IdentityKeyColumns()
	want := []string{"animal_key", "scanned_tag"}
	if len(got) != len(want) {
		t.Fatalf("IdentityKeyColumns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IdentityKeyColumns() = %v, want %v", got, want)
		}
	}
}
