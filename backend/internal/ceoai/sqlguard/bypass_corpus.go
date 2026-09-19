package sqlguard

import "strings"

// TenantBypassCorpus is the seed corpus of tenant-bypass SHAPES the model-SQL
// executor path (Validate + checkTenantConjunct + ExtractAllTenantPredicates +
// literal comparison) must reject. `<S>` stands for the session tenant and
// `<V>` for a victim tenant; expand a shape with ExpandBypassShape.
//
// It lives in non-test code so the reporting package's Postgres-gated
// behavioural test (TestModelSQLNoForeignRowsEver) can run EVERY shape against
// real ceo_ai.* views with two seeded tenants and assert the invariant that
// actually matters: as tenant A, no shape ever returns a tenant-B row with a
// nil error. The static twin (TestTenantBypassCorpusAllRejected) asserts every
// shape is rejected before any pool access; FuzzTenantPredicateBypass mutates
// them and asserts that whatever the guard accepts is structurally scoped.
//
// Comments, `$`, quoting tricks and non-ASCII are already banned by Validate;
// the corpus still includes them so a regression there is caught here too.
//
// projection-review: membership=none — these strings are adversarial SQL
// SHAPES the guard must REJECT, never a projection this package executes;
// group_key=n/a (no read model is built here); join_cardinality=n/a — the
// JOIN / comma-join / subquery shapes exist precisely so the guard proves it
// refuses a second relation; pagination=n/a — LIMIT shapes assert the row
// cap, nothing pages; scope=every shape is run as tenant A against two seeded
// tenants in reporting.TestModelSQLNoForeignRowsEver and
// TestModelSQLParkScopeCannotWidenTenant (zero tenant-B rows or an error).
var TenantBypassCorpus = []string{
	// second predicate naming the victim
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' AND tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND (tenant_id = '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND park = 'p' AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = '<S>' AND a.tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id='<V>' LIMIT 10`,
	// the plain foreign literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id='<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE TENANT_ID = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE Tenant_Id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = '<V>' LIMIT 10`,
	"SELECT id FROM ceo_ai.x WHERE tenant_id\t=\t'<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id\n=\n'<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id\r\n= '<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id \v= '<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id \f= '<V>' LIMIT 10",
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>'::uuid LIMIT 10`,
	// non-equality / widening operators
	`SELECT id FROM ceo_ai.x WHERE tenant_id <> '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id != '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id <> '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id > '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id >= '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id LIKE '%' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id ILIKE '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id ~ '.*' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id BETWEEN '<S>' AND '<V>' LIMIT 10`,
	// IN / ANY / lists
	`SELECT id FROM ceo_ai.x WHERE tenant_id IN ('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IN ('<S>', '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id IN ('<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ANY ('{<S>,<V>}') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ANY (ARRAY['<S>','<V>']) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = SOME ('{<V>}') LIMIT 10`,
	// OR / IS / NOT
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR park = 'p' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id = '<S>' OR tenant_id = '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IS NOT NULL LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IS NULL OR tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE NOT tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND NOT tenant_id <> '<V>' LIMIT 10`,
	// column refs / self-equality / expressions
	`SELECT id FROM ceo_ai.x WHERE tenant_id = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = park_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = a.tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' || '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' = true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = lower('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id) = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id::text = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE '<S>' = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE '<V>' = tenant_id AND tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE coalesce(tenant_id, '<S>') = '<S>' LIMIT 10`,
	// params / placeholders
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $1 LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = :tid LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ? LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = $2 LIMIT 10`,
	// tenant_id outside a predicate
	`SELECT tenant_id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' GROUP BY tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' ORDER BY tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE park = 'p' LIMIT 10`,
	`SELECT tenant_id, id FROM ceo_ai.x WHERE park = 'p' LIMIT 10`,
	// unicode lookalikes / non-ASCII outside literals
	"SELECT id FROM ceo_ai.x WHERE tenant_іd = '<S>' LIMIT 10", // Cyrillic і
	"SELECT id FROM ceo_ai.x WHERE тenant_id = '<S>' LIMIT 10", // Cyrillic т
	"SELECT id FROM ceo_ai.x WHERE tenant_id ＝ '<S>' LIMIT 10", // fullwidth =
	"SELECT id FROM ceo_ai.x WHERE tenant_id = ‘<S>’ LIMIT 10", // curly quotes
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10", // NBSP
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>'​ AND tenant_id = '<V>' LIMIT 10",
	// comments / stacking / quoting tricks (banned upstream by Validate)
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' -- AND tenant_id = '<V>'
LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' /* */ AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>'; SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE "tenant_id" = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = E'<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $$<V>$$ LIMIT 10`,
	// scale-guard:ignore: adversarial shape the guard must REJECT (OFFSET is a banned keyword); never executed.
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' AND tenant_id = '<S>' LIMIT 10 OFFSET 0`,
	// multi-relation vectors (JOIN / comma / subquery)
	`SELECT a.id FROM ceo_ai.x a JOIN ceo_ai.y b ON b.tenant_id = '<V>' WHERE a.tenant_id = '<S>' LIMIT 10`,
	`SELECT a.id FROM ceo_ai.x a, ceo_ai.y b WHERE a.tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND id IN (SELECT id FROM ceo_ai.y WHERE tenant_id = '<V>') LIMIT 10`,
	`SELECT (SELECT count(*) FROM ceo_ai.y WHERE tenant_id = '<V>') FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' UNION SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`WITH v AS (SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>') SELECT id FROM v WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM public.goats WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM goats WHERE tenant_id = '<S>' LIMIT 10`,
	// no LIMIT / oversize LIMIT with a victim literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>'`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 1000`,
	// empty / whitespace tenant literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ' <S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S> ' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>''' LIMIT 10`,
	// --- PR #318 judge shapes: the honest session literal is present, yet the
	// predicate's truth value is inverted / disjoined / a second arm is
	// appended, so the scan returns OTHER tenants' rows. Each of these was
	// proven live against real ceo_ai.* views before the structural guard.
	`SELECT id FROM ceo_ai.x WHERE (tenant_id = '<S>' OR health_blockers > 0) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE NOT tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' IS FALSE LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id = '<S>') = false LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE CASE WHEN tenant_id = '<S>' THEN false ELSE true END LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' UNION ALL TABLE ceo_ai.x LIMIT 10`,
	// same class, passed the old guard without being executed live
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' IS NOT TRUE LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' IS UNKNOWN LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' IS DISTINCT FROM true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE coalesce(tenant_id = '<S>', true) IS NOT TRUE LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE false = (tenant_id = '<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE nullif(tenant_id = '<S>', true) IS NULL LIMIT 10`,
	`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '<S>' GROUP BY park HAVING bool_or(tenant_id = '<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' ISNULL LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' INTERSECT TABLE ceo_ai.x LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' EXCEPT SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
	`TABLE ceo_ai.x LIMIT 10`,
	`VALUES ('<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' UNION ALL VALUES ('<V>') LIMIT 10`,
	// --- fixer additions: quoting, escapes, casts, unicode, HAVING, nesting
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $$<S>$$ LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $q$<S>$q$ OR true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = E'<S>' OR true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = e'\x27 OR true --' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id::text = '<S>' OR true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>'::text || '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE "tenant_id" = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE "TENANT_ID" = '<S>' OR true LIMIT 10`,
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR true LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR true LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND　NOT true LIMIT 10",
	`SELECT count(*) FROM ceo_ai.x WHERE park = 'p' GROUP BY park HAVING tenant_id = '<S>' LIMIT 10`,
	`SELECT count(*) FROM ceo_ai.x GROUP BY park HAVING bool_and(tenant_id = '<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE ((tenant_id = '<S>')) OR true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE ((tenant_id = '<S>') OR true) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (((tenant_id = '<S>' AND park = 'p') OR true)) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (park = 'p' OR (tenant_id = '<S>')) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE NOT (tenant_id = '<S>' AND park = 'p') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE NOT (NOT (tenant_id = '<S>')) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id = '<S>') AND (park = 'p' OR true) OR false LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND (park = 'p' OR NOT tenant_id = '<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' = tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND true OR true LIMIT 10`,
	`SELECT id, count(*) FILTER (WHERE tenant_id = '<S>') FROM ceo_ai.x WHERE park = 'p' GROUP BY id LIMIT 10`,
	// scale-guard:ignore: adversarial shape the guard must REJECT (OFFSET is a banned keyword); never executed.
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OFFSET 0 LIMIT 10`,
	`SELECT extract(year FROM purchase_date) FROM ceo_ai.x WHERE tenant_id = '<S>' UNION ALL TABLE ceo_ai.x LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE AND tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' FOR UPDATE LIMIT 10`,
}

// ExpandBypassShape substitutes the session (<S>) and victim (<V>) tenant ids
// into a corpus shape.
func ExpandBypassShape(shape, session, victim string) string {
	return strings.NewReplacer("<S>", session, "<V>", victim).Replace(shape)
}
