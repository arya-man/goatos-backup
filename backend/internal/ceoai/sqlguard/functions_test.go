package sqlguard

import (
	"sort"
	"strings"
	"testing"
)

const fnTenant = `tenant_id = '11111111-1111-1111-1111-111111111111'`

// TestAllowedFunctionsOnly (PR #318 R2-1): every function on the allow-list
// is callable in a well-formed read, and every function-argument read shape
// the judge proved live — plus the whole family it belongs to — is rejected
// because it is NOT on the list, with the reject reason naming the function.
func TestAllowedFunctionsOnly(t *testing.T) {
	positive := map[string]string{
		"count":           `SELECT count(*) AS value`,
		"sum":             `SELECT sum(deaths) AS value`,
		"avg":             `SELECT avg(body_weight) AS value`,
		"min":             `SELECT min(event_date) AS value`,
		"max":             `SELECT max(event_date) AS value`,
		"coalesce":        `SELECT coalesce(shed_label, 'none') AS value`,
		"nullif":          `SELECT nullif(deaths, 0) AS value`,
		"round":           `SELECT round(avg(body_weight), 1) AS value`,
		"abs":             `SELECT abs(delta) AS value`,
		"floor":           `SELECT floor(body_weight) AS value`,
		"ceil":            `SELECT ceil(body_weight) AS value`,
		"greatest":        `SELECT greatest(a, b) AS value`,
		"least":           `SELECT least(a, b) AS value`,
		"percentile_cont": `SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY seconds) AS value`,
		"percentile_disc": `SELECT percentile_disc(0.5) WITHIN GROUP (ORDER BY seconds) AS value`,
		"bool_and":        `SELECT bool_and(overdue > 0) AS value`,
		"bool_or":         `SELECT bool_or(overdue > 0) AS value`,
		"date_trunc":      `SELECT date_trunc('month', event_date) AS value`,
		"date_part":       `SELECT date_part('year', event_date) AS value`,
		"extract":         `SELECT extract(epoch, event_date) AS value`, // the `FROM` form is rejected by the single-FROM rule (see below)
		"to_char":         `SELECT to_char(event_date, 'DD/MM/YYYY') AS value`,
		"to_date":         `SELECT to_date('01/08/2026', 'DD/MM/YYYY') AS value`,
		"now":             `SELECT now() AS value`,
		"current_date":    `SELECT current_date() AS value`,
		"make_date":       `SELECT make_date(2026, 8, 1) AS value`,
		"age":             `SELECT age(event_date) AS value`,
		"interval":        `SELECT interval('1 month') AS value`,
		"lower":           `SELECT lower(shed_label) AS value`,
		"upper":           `SELECT upper(shed_label) AS value`,
		"trim":            `SELECT trim(shed_label) AS value`,
		"length":          `SELECT length(shed_label) AS value`,
		"substring":       `SELECT substring(shed_label, 1, 3) AS value`,
		"left":            `SELECT left(shed_label, 3) AS value`,
		"right":           `SELECT right(shed_label, 3) AS value`,
		"concat":          `SELECT concat(park_label, shed_label) AS value`,
		"concat_ws":       `SELECT concat_ws(' - ', park_label, shed_label) AS value`,
		"replace":         `SELECT replace(shed_label, 'a', 'b') AS value`,
		"initcap":         `SELECT initcap(shed_label) AS value`,
		"cast":            `SELECT cast(sum(deaths) AS text) AS value`,
	}
	allowed := AllowedFunctions()
	if !sort.StringsAreSorted(allowed) {
		t.Fatalf("AllowedFunctions must be sorted: %v", allowed)
	}
	if len(allowed) != len(positive) {
		t.Fatalf("every allowed function needs a positive case: allowed=%d cases=%d", len(allowed), len(positive))
	}
	for _, fn := range allowed {
		proj, ok := positive[fn]
		if !ok {
			t.Errorf("allowed function %q has no positive case", fn)
			continue
		}
		sql := proj + ` FROM ceo_ai.mortality_base WHERE ` + fnTenant + ` LIMIT 10`
		if err := Validate(sql); err != nil {
			t.Errorf("allowed function %q must validate: %v\n  %s", fn, err, sql)
		}
		// Case-insensitive: the model may upper-case the call.
		if err := Validate(strings.ToUpper(proj) + ` FROM ceo_ai.mortality_base WHERE ` + fnTenant + ` LIMIT 10`); err != nil {
			t.Errorf("allowed function %q must validate upper-cased: %v", fn, err)
		}
	}
	// Keyword-before-paren shapes and both casting forms are not function calls.
	for _, ok := range []string{
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND (stage = 'a' OR stage = 'b') LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND stage IN ('a', 'b') LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND NOT (stage = 'a') LIMIT 10`,
		`SELECT count(*) FILTER (WHERE stage = 'a') FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 10`,
		`SELECT count(*) OVER () FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 10`,
		`SELECT CASE WHEN (deaths > 0) THEN (1) ELSE (0) END FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 10`,
		`SELECT sum(deaths)::text, CAST(avg(deaths) AS numeric) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND event_date >= now() - interval '30 days' LIMIT 10`,
	} {
		if err := Validate(ok); err != nil {
			t.Errorf("keyword/cast shape must validate: %v\n  %s", err, ok)
		}
	}

	// Negative: not on the list => rejected, and the reason names the function.
	negative := []struct{ fn, proj string }{
		{"table_to_xml", `SELECT table_to_xml('ceo_ai.x', true, false, '')::text AS value`},
		{"schema_to_xml", `SELECT schema_to_xml('ceo_ai', true, false, '')::text AS value`},
		{"query_to_xml_and_xmlschema", `SELECT query_to_xml_and_xmlschema('select 1', true, false, '')::text AS value`},
		{"ts_stat", `SELECT (ts_stat('select to_tsvector(a) from ceo_ai.x')).word AS value`},
		{"xpath", `SELECT xpath('/a', b)::text AS value`},
		{"pg_get_viewdef", `SELECT pg_get_viewdef('ceo_ai.x') AS value`},
		{"pg_sleep", `SELECT pg_sleep(1) AS value`},
		{"regexp_replace", `SELECT regexp_replace(a, 'x', 'y') AS value`},
		{"regexp_matches", `SELECT regexp_matches(a, 'x') AS value`},
		{"string_agg", `SELECT string_agg(a, ',') AS value`},
		{"array_agg", `SELECT array_agg(a) AS value`},
		{"jsonb_agg", `SELECT jsonb_agg(a) AS value`},
		{"row_to_json", `SELECT row_to_json(a) AS value`},
		{"generate_series", `SELECT generate_series(1, 10) AS value`},
		{"to_tsvector", `SELECT to_tsvector(a) AS value`},
		{"numeric", `SELECT CAST(a AS numeric(10, 2)) AS value`},
		{"version", `SELECT version() AS value`},
		{"current_user", `SELECT current_user() AS value`},
	}
	for _, tc := range negative {
		sql := tc.proj + ` FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 10`
		err := Validate(sql)
		if err == nil {
			t.Errorf("function %q must be rejected: %s", tc.fn, sql)
			continue
		}
		if !strings.Contains(err.Error(), `"`+tc.fn+`"`) {
			t.Errorf("reject reason must name %q, got %v", tc.fn, err)
		}
	}
	// A FROM-based source function is rejected by the source rule before the
	// function rule; either way it never runs.
	for _, bad := range []string{
		`SELECT c.a FROM crosstab('select 1') AS c(a text) WHERE ` + fnTenant + ` LIMIT 1`,
		`SELECT d.a FROM dblink('x', 'select 1') AS d(a text) WHERE ` + fnTenant + ` LIMIT 1`,
		`SELECT x.a FROM ceo_ai.x, xmltable('/r' PASSING b COLUMNS a text) x WHERE x.` + fnTenant + ` LIMIT 1`,
		`SELECT extract(year FROM event_date) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
	} {
		if err := Validate(bad); err == nil {
			t.Errorf("must be rejected: %s", bad)
		}
	}
}

// TestFunctionNameCannotBeQualifiedOrQuoted: an allowed builtin reached through
// a schema prefix or a double-quoted identifier is rejected outright — that is
// how an extension or a ceo_ai/public function would be called.
func TestFunctionNameCannotBeQualifiedOrQuoted(t *testing.T) {
	cases := map[string]string{
		"schema-qualified allowed name": `SELECT pg_catalog.lower(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
		"ceo_ai-qualified function":     `SELECT ceo_ai.lower(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
		"other schema function":         `SELECT reports.total(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
		"double-quoted allowed name":    `SELECT "lower"(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
		"double-quoted disallowed name": `SELECT "table_to_xml"('ceo_ai.x', true, false, '') FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
		"quoted-schema qualified":       `SELECT "ceo_ai".lower(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`,
	}
	for name, sql := range cases {
		if err := Validate(sql); err == nil {
			t.Errorf("%s must be rejected: %s", name, sql)
		}
	}
	err := Validate(`SELECT ceo_ai.lower(a) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`)
	if err == nil || !strings.Contains(err.Error(), "schema-qualified function") {
		t.Fatalf("qualified call must be rejected as such, got %v", err)
	}
}

// TestQueryToXMLStaysExplicitlyBanned is the belt-and-braces twin: even if
// the allow-list were loosened, query_to_xml is still rejected as an explicit
// ban, as a call and as a bare identifier.
func TestQueryToXMLStaysExplicitlyBanned(t *testing.T) {
	call := `SELECT query_to_xml('select 1', true, false, '') FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`
	err := Validate(call)
	if err == nil || !strings.Contains(err.Error(), `banned function "query_to_xml"`) {
		t.Fatalf("query_to_xml() must be an explicit ban, got %v", err)
	}
	bare := `SELECT query_to_xml FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT 1`
	err = Validate(bare)
	if err == nil || !strings.Contains(err.Error(), `banned identifier "query_to_xml"`) {
		t.Fatalf("bare query_to_xml must be an explicit ban, got %v", err)
	}
	if _, banned := bannedFunctions["query_to_xml"]; !banned {
		t.Fatal("query_to_xml must stay in the explicit ban set")
	}
	if _, allowed := allowedFunctions["query_to_xml"]; allowed {
		t.Fatal("query_to_xml must never be allow-listed")
	}
}

// TestBetweenNotSplitAsConjunct (PR #318 R2-3): the AND inside
// `x BETWEEN a AND b` is the range connective, not a conjunction, so a
// string-literal BETWEEN beside the tenant conjunct validates, while a
// BETWEEN on tenant_id itself is still rejected.
func TestBetweenNotSplitAsConjunct(t *testing.T) {
	for _, ok := range []string{
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND load_label BETWEEN 'A' AND 'Z' LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE load_label BETWEEN 'A' AND 'Z' AND ` + fnTenant + ` LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND deaths BETWEEN 1 AND 5 AND stage = 'a' LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND deaths NOT BETWEEN 1 AND 5 LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND (deaths BETWEEN 1 AND 5) LIMIT 10`,
	} {
		if err := Validate(ok); err != nil {
			t.Errorf("BETWEEN beside the tenant conjunct must validate: %v\n  %s", err, ok)
		}
	}
	for _, bad := range []string{
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id BETWEEN '1' AND '2' LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND load_label BETWEEN 'A' AND 'Z' OR true LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` AND load_label BETWEEN 'A' AND 'Z' AND LIMIT 10`,
	} {
		if err := Validate(bad); err == nil {
			t.Errorf("must be rejected: %s", bad)
		}
	}
}

// TestLimitMustBePlainInteger (PR #318 NIT): the bound is a decimal integer
// <= MaxRowLimit and nothing else — no exponent, hex, expression or suffix.
func TestLimitMustBePlainInteger(t *testing.T) {
	head := `SELECT count(*) FROM ceo_ai.x WHERE ` + fnTenant + ` LIMIT `
	for _, ok := range []string{"1", "10", "100"} {
		if err := Validate(head + ok); err != nil {
			t.Errorf("LIMIT %s must validate: %v", ok, err)
		}
	}
	for _, bad := range []string{"1e9", "1E2", "0x10", "1.5", "10 5", "1+1", "(1)", "100::int", "10 rows", "1e", "9e0"} {
		if err := Validate(head + bad); err == nil {
			t.Errorf("LIMIT %s must be rejected", bad)
		}
	}
}
