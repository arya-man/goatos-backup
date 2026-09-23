package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// THE QUALIFIED COLUMN, WHICH HAD NO TEST AT ALL. `identityfilter_test.go`'s
// 188 lines all name a bare column, so nothing noticed that the rewrite built
// the fold from the bare token while the splice kept the qualifier: an aliased
// read came out as `w.upper(trim(animal_key))` and sqlguard rejected it with
// *schema-qualified function call "w".upper() is not allowed*.
//
// It failed CLOSED -- the statement never executed and the tenant conjunct was
// untouched -- but SILENTLY, and on the exact question this file exists for.
// Aliasing is ordinary drafting: the guard's own tenant-bypass corpus carries
// `FROM ceo_ai.x a WHERE a.tenant_id = ...` shapes.
//
// Mutation: fold `sql[toks[col].start:toks[col].end]` instead of the qualified
// reference, and every case here goes red on the guard.
func TestAQualifiedIdentityColumnIsFoldedWholeAndStillPassesTheGuard(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want string
	}{
		{
			"alias",
			"SELECT w.animal_key AS label, w.weight_kg AS value " +
				"FROM ceo_ai.weighing_latest_individual_weight w " +
				"WHERE w.tenant_id = '" + identityTenant + "' AND w.animal_key = 'MG-100001' LIMIT 10",
			"upper(trim(w.animal_key)) = upper(trim('MG-100001'))",
		},
		{
			"table qualified",
			"SELECT animal_key AS label, weight_kg AS value " +
				"FROM ceo_ai.weighing_latest_individual_weight " +
				"WHERE tenant_id = '" + identityTenant + "' " +
				"AND weighing_latest_individual_weight.animal_key = 'MG-100001' LIMIT 10",
			"upper(trim(weighing_latest_individual_weight.animal_key)) = upper(trim('MG-100001'))",
		},
		{
			"schema and table qualified",
			"SELECT animal_key AS label, weight_kg AS value " +
				"FROM ceo_ai.weighing_latest_individual_weight " +
				"WHERE tenant_id = '" + identityTenant + "' " +
				"AND ceo_ai.weighing_latest_individual_weight.animal_key = 'MG-100001' LIMIT 10",
			"upper(trim(ceo_ai.weighing_latest_individual_weight.animal_key)) = upper(trim('MG-100001'))",
		},
		{
			"alias with an IN list",
			"SELECT b.buyer_key AS label, b.revenue_rupees AS value " +
				"FROM ceo_ai.sales_buyer_summary b " +
				"WHERE b.tenant_id = '" + identityTenant + "' " +
				"AND b.buyer_key IN ('Ravi Traders', 'Kumar') LIMIT 10",
			"upper(trim(b.buyer_key)) IN (upper(trim('Ravi Traders')), upper(trim('Kumar')))",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := sqlguard.Validate(tc.sql); err != nil {
				t.Fatalf("the INPUT a model drafted is already refused, so this case proves nothing: %v", err)
			}
			got := normalizeIdentityFilters(tc.sql)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("qualified column was not folded whole:\nwant substring %s\ngot %s", tc.want, got)
			}
			if strings.Contains(got, ".upper(") {
				t.Fatalf("the qualifier was left OUTSIDE the fold, which the guard rejects:\n%s", got)
			}
			assertGuardStillAccepts(t, got)
		})
	}
}

// A postfix operator on the right-hand side re-associates once the literal
// moves inside the fold, and unlike the qualified-column defect this one PASSES
// the guard and EXECUTES: `= 'MG-' || 'a0001'` becomes
// `upper(trim(animal_key)) = upper(trim('MG-')) || 'a0001'`, comparing an
// upper-cased column against a string whose tail is still lower case. A row
// that matched before does not match after, with no error anywhere.
//
// The statement is returned UNCHANGED rather than folded, which is the
// pre-existing behaviour for every shape this file declines to rewrite.
//
// Mutation: drop the endsComparison call from the `=` branch and this goes red.
func TestAConcatenatedRightHandSideIsLeftAloneRatherThanReassociated(t *testing.T) {
	for _, in := range []string{
		"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight " +
			"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-' || 'a0001' LIMIT 10",
		"SELECT buyer_key AS label, revenue_rupees AS value FROM ceo_ai.sales_buyer_summary " +
			"WHERE tenant_id = '" + identityTenant + "' AND buyer_key IN ('Ravi') || 'x' LIMIT 10",
	} {
		if got := normalizeIdentityFilters(in); got != in {
			t.Errorf("a right-hand side with an operator after the literal was rewritten:\n%s", got)
		}
	}
	// A cast is applied to the literal either way, so it is still folded.
	withCast := "SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight " +
		"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-100001'::text LIMIT 10"
	if got := normalizeIdentityFilters(withCast); !strings.Contains(got, "upper(trim(animal_key)) = upper(trim('MG-100001'))") {
		t.Errorf("a cast right-hand side stopped being folded:\n%s", got)
	}
	// An ordinary terminator is still folded.
	for _, in := range []string{
		"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight " +
			"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-100001' ORDER BY weighed_on LIMIT 10",
		"SELECT animal_key AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight " +
			"WHERE tenant_id = '" + identityTenant + "' AND animal_key = 'MG-100001' AND weight_kg > 20 LIMIT 10",
	} {
		if got := normalizeIdentityFilters(in); !strings.Contains(got, "upper(trim(animal_key)) = upper(trim('MG-100001'))") {
			t.Errorf("an ordinary terminator stopped the fold:\n%s", got)
		}
	}
}

// `sales_buyer_summary.buyer_key` is
// `lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g'))` (000395:256),
// so the stored key has its INTERNAL whitespace collapsed while
// `upper(trim(...))` only strips the ends. A reader typing the doubled space is
// exactly the spelling variance the view's normalisation exists to absorb, and
// it still missed. `regexp_replace` is deliberately not in sqlguard's closed
// function set and this does not add it -- the LITERAL is normalised in Go
// before the statement is validated.
//
// Mutation: make normalizedLiteral the identity function and this goes red.
func TestTheFoldAgreesWithBuyerKeysOwnNormalForm(t *testing.T) {
	in := "SELECT buyer_key AS label, revenue_rupees AS value FROM ceo_ai.sales_buyer_summary " +
		"WHERE tenant_id = '" + identityTenant + "' AND buyer_key = 'Ravi  Traders' LIMIT 10"
	got := normalizeIdentityFilters(in)
	if !strings.Contains(got, "upper(trim('Ravi Traders'))") {
		t.Fatalf("internal whitespace was not collapsed to the view's normal form:\n%s", got)
	}
	if strings.Contains(got, "Ravi  Traders") {
		t.Fatalf("the doubled space survived into the executed statement:\n%s", got)
	}
	assertGuardStillAccepts(t, got)
}

// The property the reviewer proved and that must survive every change here:
// identityfilter cannot reach tenant_id on any path.
func TestTheRewriteStillCannotTouchTheTenantConjunct(t *testing.T) {
	for _, in := range []string{
		"SELECT w.animal_key AS label, w.weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight w " +
			"WHERE w.tenant_id = '" + identityTenant + "' AND w.animal_key = 'MG-100001' LIMIT 10",
		"SELECT buyer_key AS label, revenue_rupees AS value FROM ceo_ai.sales_buyer_summary " +
			"WHERE tenant_id = '" + identityTenant + "' AND buyer_key = 'Ravi  Traders' LIMIT 10",
	} {
		got := normalizeIdentityFilters(in)
		if !strings.Contains(got, "tenant_id = '"+identityTenant+"'") {
			t.Fatalf("the tenant conjunct is no longer byte-identical:\n%s", got)
		}
		if strings.Contains(got, "upper(trim(tenant_id") || strings.Contains(got, "trim(w.tenant_id") {
			t.Fatalf("tenant_id was folded:\n%s", got)
		}
		assertGuardStillAccepts(t, got)
	}
}
