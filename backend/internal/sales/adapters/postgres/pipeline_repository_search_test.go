package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// TestLeadSearchTreatsLikeMetacharactersAsLiteralText pins BOTH directions of the escaping, and the
// second direction is the one that matters.
//
// Asserting only that a search for "%" returns nothing is a FALSE PASS: broken escaping returns
// nothing too. That is exactly how a real defect survived a manual check on 2026-09-05 -- the
// escaper doubled its backslashes (`\\_` in a Go raw string is a literal backslash followed by the
// `_` WILDCARD, not an escaped underscore), so every metacharacter search returned zero and looked
// correct. It was only caught by creating a lead whose NAME contains the metacharacters and finding
// it unsearchable.
//
// So this seeds such a lead and requires that:
//   - a search for the literal "_"/"%" text FINDS it (escaping actually escapes)
//   - a metacharacter does NOT act as a wildcard (it is not passed through raw)
func TestLeadSearchTreatsLikeMetacharactersAsLiteralText(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	for _, name := range []string{"Percent%Buyer", "Under_Score Buyer", "Plain Buyer"} {
		if _, err := repo.CreateBuyerLead(ctx, salesTestTenant, domain.BuyerLeadWrite{BuyerName: name}.Normalize(), "", "k-"+name); err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
	}

	search := func(term string) []string {
		t.Helper()
		page, err := repo.ListBuyerLeads(ctx, salesTestTenant, domain.LeadFilter{Search: term}, 50, 0)
		if err != nil {
			t.Fatalf("search %q: %v", term, err)
		}
		out := make([]string, 0, len(page.Leads))
		for _, l := range page.Leads {
			out = append(out, l.BuyerName)
		}
		if page.Total != len(out) {
			t.Errorf("search %q: total %d but %d rows -- the page and the count must share one predicate", term, page.Total, len(out))
		}
		return out
	}

	// FOUND LITERALLY. This is the half a "returns nothing" assertion cannot see.
	if got := search("under_score"); len(got) != 1 || got[0] != "Under_Score Buyer" {
		t.Errorf(`search "under_score" = %v, want exactly the lead whose name contains that underscore`, got)
	}
	if got := search("percent%buyer"); len(got) != 1 || got[0] != "Percent%Buyer" {
		t.Errorf(`search "percent%%buyer" = %v, want exactly the lead whose name contains that percent`, got)
	}

	// NOT A WILDCARD. Raw metacharacters would make these match every seeded lead.
	if got := search("%"); len(got) != 1 || got[0] != "Percent%Buyer" {
		t.Errorf(`search "%%" = %v, want only the name containing a literal percent, never every lead`, got)
	}
	if got := search("plain_buyer"); len(got) != 0 {
		t.Errorf(`search "plain_buyer" = %v, want nothing: the underscore must not match the space in "Plain Buyer"`, got)
	}
}
