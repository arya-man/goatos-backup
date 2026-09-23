package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

const identityTenant = "00000000-0000-4000-8000-000000000001"

// TestTheReadersSpellingOfAnEarTagFindsTheAnimal is the bug the held-out set
// caught: `MG-100001` is how goat_identifiers stores the tag and how it is
// printed on the ear, `animal_key` is lower(btrim(...)) of it, and the equality
// missed -- reported to the reader as "No records found" rather than as a
// lookup that did not match.
func TestTheReadersSpellingOfAnEarTagFindsTheAnimal(t *testing.T) {
	in := "SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.growth_adg_pairs " +
		"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-100001' LIMIT 10"
	got := normalizeIdentityFilters(in)
	if got == in {
		t.Fatalf("the upper-case tag filter was left as it was:\n%s", got)
	}
	if !strings.Contains(got, "upper(trim(animal_key)) = upper(trim('MG-100001'))") {
		t.Fatalf("comparison not folded to the identity normal form:\n%s", got)
	}
	// The tenant conjunct the guard binds on must come through byte-identical.
	if !strings.Contains(got, "tenant_id = '"+identityTenant+"'") {
		t.Fatalf("tenant conjunct was disturbed:\n%s", got)
	}
	assertGuardStillAccepts(t, got)
}

// TestFoldingCoversTheOtherShapesAModelDrafts: a listed IN, a LIKE prefix and a
// buyer name -- `sales_buyer_summary.buyer_key` is folded the same way and the
// reader types "Ravi Traders".
func TestFoldingCoversTheOtherShapesAModelDrafts(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{
			"in list",
			"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = '" + identityTenant + "' AND animal_key IN ('MG-100001', 'MG-100002') LIMIT 10",
			"upper(trim(animal_key)) IN (upper(trim('MG-100001')), upper(trim('MG-100002')))",
		},
		{
			"like prefix",
			"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = '" + identityTenant + "' AND animal_key LIKE 'MG-1000%' LIMIT 10",
			"upper(trim(animal_key)) LIKE upper(trim('MG-1000%'))",
		},
		{
			// A doubled-quote escape EARLIER in the statement. The rewrite has
			// to know where a string literal begins and ends to find the real
			// comparison after it; a scan that treats a quote as an ordinary
			// byte loses the boundary and silently folds nothing.
			"an escaped quote earlier in the statement",
			"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = '" + identityTenant + "' AND park_label = 'Ravi''s Park' AND animal_key = 'MG-100001' LIMIT 10",
			"upper(trim(animal_key)) = upper(trim('MG-100001'))",
		},
		{
			"buyer name",
			"SELECT buyer_label AS label, revenue_rupees AS value FROM ceo_ai.sales_buyer_summary WHERE tenant_id = '" + identityTenant + "' AND buyer_key = 'Ravi Traders' LIMIT 10",
			"upper(trim(buyer_key)) = upper(trim('Ravi Traders'))",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeIdentityFilters(c.sql)
			if !strings.Contains(got, c.want) {
				t.Fatalf("want %q in:\n%s", c.want, got)
			}
			assertGuardStillAccepts(t, got)
		})
	}
}

// TestFoldingLeavesEverythingElseAlone is the narrowing half, and it is the one
// worth keeping: a fold that reached the tenant literal, a measure, a join, an
// already-case-insensitive ILIKE or the inside of a string would each be a
// defect of its own.
func TestFoldingLeavesEverythingElseAlone(t *testing.T) {
	unchanged := []struct {
		name string
		sql  string
	}{
		{
			"tenant literal is never folded",
			"SELECT park_label AS label, count(*) AS value FROM ceo_ai.growth_adg_pairs WHERE tenant_id = '" + identityTenant + "' GROUP BY park_label LIMIT 10",
		},
		{
			"a dimension that is not an identity key",
			"SELECT shed_label AS label, count(*) AS value FROM ceo_ai.growth_adg_pairs WHERE tenant_id = '" + identityTenant + "' AND park_label = 'Channapatna' GROUP BY shed_label LIMIT 10",
		},
		{
			"ILIKE is already case-insensitive",
			"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.growth_adg_pairs WHERE tenant_id = '" + identityTenant + "' AND animal_key ILIKE 'mg-1000%' LIMIT 10",
		},
		// A draft that folded the column itself is left alone for a structural
		// reason, not a name one: the token after the column is `)`, not a
		// comparison operator, so there is no comparison here to rewrite. The
		// same property is what makes the rewrite idempotent below.
		{
			"a draft that folded the column itself",
			"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.growth_adg_pairs WHERE tenant_id = '" + identityTenant + "' AND lower(animal_key) = 'mg-100001' LIMIT 10",
		},
		{
			// A whole comparison spelled INSIDE a literal, doubled quotes and
			// all. Only the lexer's literal handling keeps the rewrite off it;
			// a scan that did not know where a string starts would fold the
			// text of the label a reader is shown.
			"the column name inside a string literal",
			"SELECT 'animal_key = ''MG-100001''' AS label, count(*) AS value FROM ceo_ai.growth_adg_pairs WHERE tenant_id = '" + identityTenant + "' LIMIT 10",
		},
		{
			"a view with no identity key",
			"SELECT park_label AS label, fed_kg AS value FROM ceo_ai.feed_adherence WHERE tenant_id = '" + identityTenant + "' AND park_label = 'Channapatna' LIMIT 10",
		},
	}
	for _, c := range unchanged {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeIdentityFilters(c.sql); got != c.sql {
				t.Fatalf("statement was rewritten when it should not have been:\ngot  %s\nwant %s", got, c.sql)
			}
		})
	}
}

// TestFoldingIsIdempotent: the rewrite runs on every model SQL execution, and
// a repaired or re-planned draft can arrive already folded. A second pass must
// not wrap the comparison again.
func TestFoldingIsIdempotent(t *testing.T) {
	in := "SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.growth_adg_pairs " +
		"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-100001' LIMIT 10"
	once := normalizeIdentityFilters(in)
	if twice := normalizeIdentityFilters(once); twice != once {
		t.Fatalf("second pass changed the statement:\nonce  %s\ntwice %s", once, twice)
	}
}

// TestIdentityKeyColumnsAreTheFoldedOnesOnly pins the card-owned list the
// rewrite is driven from, so a new view carrying a folded key is covered by
// naming convention and a measure never is.
func TestIdentityKeyColumnsAreTheFoldedOnesOnly(t *testing.T) {
	want := map[string][]string{
		"growth_adg_pairs":                  {"animal_key"},
		"weighing_latest_individual_weight": {"animal_key"},
		"sales_buyer_summary":               {"buyer_key"},
		"sales_deal_lines_closed":           {"buyer_key"},
		"feed_adherence":                    nil,
	}
	for name, expect := range want {
		card, ok := reporting.CardByName(name)
		if !ok {
			t.Fatalf("no schema card %q", name)
		}
		got := card.IdentityKeyColumns()
		if len(got) != len(expect) {
			t.Fatalf("%s identity keys = %v, want %v", name, got, expect)
		}
		for i := range got {
			if got[i] != expect[i] {
				t.Fatalf("%s identity keys = %v, want %v", name, got, expect)
			}
		}
	}
}

// assertGuardStillAccepts proves the fold did not cost the statement its
// guard clearance: sqlguard must still validate it AND still read the session
// tenant out of the one conjunct that binds it.
func assertGuardStillAccepts(t *testing.T, sql string) {
	t.Helper()
	if err := sqlguard.Validate(sql); err != nil {
		t.Fatalf("folded statement no longer passes the guard: %v\n%s", err, sql)
	}
	lits, err := sqlguard.ExtractAllTenantPredicates(sql)
	if err != nil {
		t.Fatalf("tenant predicate no longer extractable: %v\n%s", err, sql)
	}
	for _, l := range lits {
		if l != identityTenant {
			t.Fatalf("tenant literal changed to %q", l)
		}
	}
}
