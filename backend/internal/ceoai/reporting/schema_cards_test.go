package reporting

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// TestSchemaCardsMatchInformationSchema is the Postgres-gated card <=> catalog
// diff: for every ceo_ai VIEW in the migrated database, a card must exist, and
// its column list (name + short type, in order) must equal
// information_schema.columns for that view — in both directions. It skips
// cleanly when Postgres tests are not enabled (pgtest opt-in).
func TestSchemaCardsMatchInformationSchema(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	// Views only: ceo_ai also holds helper TABLES (e.g. vaccine_label_map) and
	// functions that are not read surfaces for the assistant.
	rows, err := pool.Query(ctx, `SELECT table_name FROM information_schema.views WHERE table_schema = 'ceo_ai' ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list views: %v", err)
	}
	var dbViews []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		dbViews = append(dbViews, n)
	}
	rows.Close()
	if len(dbViews) == 0 {
		t.Fatal("no ceo_ai views in the migrated database; migrations did not apply")
	}

	cardsByName := map[string]SchemaCard{}
	for _, c := range Cards() {
		cardsByName[c.Name] = c
	}

	// Direction 1: every DB view has a card.
	for _, v := range dbViews {
		if _, ok := cardsByName[v]; !ok {
			t.Errorf("ceo_ai.%s exists in the database but has no schema card (add one in schema_cards.go)", v)
		}
	}
	// Direction 2: every card names a real view.
	dbSet := map[string]bool{}
	for _, v := range dbViews {
		dbSet[v] = true
	}
	for name := range cardsByName {
		if !dbSet[name] {
			t.Errorf("schema card %q names a view that does not exist in the migrated database", name)
		}
	}

	// Column-level diff, both directions, per view.
	for _, v := range dbViews {
		card, ok := cardsByName[v]
		if !ok {
			continue
		}
		crows, err := pool.Query(ctx,
			`SELECT column_name, data_type, udt_name FROM information_schema.columns
			 WHERE table_schema = 'ceo_ai' AND table_name = $1 ORDER BY ordinal_position`, v)
		if err != nil {
			t.Fatalf("columns for %s: %v", v, err)
		}
		var dbCols []string
		for crows.Next() {
			var cn, dt, udt string
			if err := crows.Scan(&cn, &dt, &udt); err != nil {
				t.Fatalf("scan: %v", err)
			}
			dbCols = append(dbCols, cn+":"+PGTypeShort(dt, udt))
		}
		crows.Close()

		var cardCols []string
		for _, c := range card.Columns {
			cardCols = append(cardCols, c.Name+":"+c.Type)
		}
		if strings.Join(dbCols, ",") != strings.Join(cardCols, ",") {
			t.Errorf("ceo_ai.%s column drift\n  database: %s\n  card:     %s", v, strings.Join(dbCols, ", "), strings.Join(cardCols, ", "))
		}
	}
}

// TestSchemaCardsNoBannedKeywordColumns guards the prompt/guard contract: a
// column spelled like a sqlguard banned keyword (SET, CLOSE, LOAD, START, …)
// could never be selected through the guard, so a card must not advertise one.
func TestSchemaCardsNoBannedKeywordColumns(t *testing.T) {
	banned := map[string]bool{}
	for _, k := range sqlguard.BannedKeywords() {
		banned[strings.ToUpper(k)] = true
	}
	if len(banned) == 0 {
		t.Fatal("sqlguard.BannedKeywords returned nothing")
	}
	for _, c := range Cards() {
		names := append([]string{}, c.ColumnNames()...)
		names = append(names, c.GroupByColumns...)
		names = append(names, c.NeverAverage...)
		if c.DateColumn != "" {
			names = append(names, c.DateColumn)
		}
		if c.ParkColumn != "" {
			names = append(names, c.ParkColumn)
		}
		for _, n := range names {
			if banned[strings.ToUpper(n)] {
				t.Errorf("card %s: column/alias %q equals a sqlguard banned keyword", c.Name, n)
			}
		}
	}
}

// TestEveryCardHasTenantIDColumn: the executor binds tenant_id = '<session>' on
// every read, so a view without tenant_id can never be read through the guard.
func TestEveryCardHasTenantIDColumn(t *testing.T) {
	for _, c := range Cards() {
		if !c.HasColumn("tenant_id") {
			t.Errorf("card %s has no tenant_id column", c.Name)
		}
		found := false
		for _, s := range c.TenantScopedColumns {
			if s == "tenant_id" {
				found = true
			}
		}
		if !found {
			t.Errorf("card %s does not list tenant_id in TenantScopedColumns", c.Name)
		}
	}
}

// TestSchemaCardsInternalConsistency: every referenced column (date, park,
// group-by, never-average) exists in Columns; names are unique; routes are
// absolute admin-web hrefs; the compact render mentions the name and purpose.
func TestSchemaCardsInternalConsistency(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Cards() {
		if seen[c.Name] {
			t.Errorf("duplicate card %s", c.Name)
		}
		seen[c.Name] = true
		if strings.TrimSpace(c.Purpose) == "" || strings.TrimSpace(c.Grain) == "" {
			t.Errorf("card %s: purpose and grain are required", c.Name)
		}
		if !strings.HasPrefix(c.Route, "/") {
			t.Errorf("card %s: route %q must be an absolute admin-web href", c.Name, c.Route)
		}
		colSeen := map[string]bool{}
		for _, col := range c.Columns {
			if colSeen[col.Name] {
				t.Errorf("card %s: duplicate column %s", c.Name, col.Name)
			}
			colSeen[col.Name] = true
			if col.Type == "" {
				t.Errorf("card %s: column %s has no type", c.Name, col.Name)
			}
		}
		check := func(kind, name string) {
			if name != "" && !c.HasColumn(name) {
				t.Errorf("card %s: %s column %q is not in Columns", c.Name, kind, name)
			}
		}
		check("date", c.DateColumn)
		check("park", c.ParkColumn)
		for _, g := range c.GroupByColumns {
			check("group-by", g)
		}
		for _, n := range c.NeverAverage {
			check("never-average", n)
		}
		if c.DateColumn != "" {
			for _, col := range c.Columns {
				if col.Name == c.DateColumn && col.Type != "date" {
					t.Errorf("card %s: DateColumn %s must be a business-day DATE column, got %s", c.Name, col.Name, col.Type)
				}
			}
		}
		r := c.RenderCompact()
		if !strings.Contains(r, "ceo_ai."+c.Name) || !strings.Contains(r, c.Purpose) {
			t.Errorf("card %s: compact render missing name/purpose: %s", c.Name, r)
		}
	}
	// Cards() must be name-sorted for a stable prompt.
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	got := make([]string, 0, len(names))
	for _, c := range Cards() {
		got = append(got, c.Name)
	}
	if strings.Join(names, ",") != strings.Join(got, ",") {
		t.Fatalf("Cards() is not name-sorted: %v", got)
	}
}

// TestSchemaCardsCoverEveryMigrationView is the in-process twin of the mjs
// guard: every `CREATE OR REPLACE VIEW ceo_ai.<name>` in the migrations
// directory has a card, so a missing card is caught by `go test` as well as by
// `make ceo-ai-schema-card-guard`.
func TestSchemaCardsCoverEveryMigrationView(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "migrations", "postgres")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migrations dir not readable: %v", err)
	}
	re := regexp.MustCompile(`(?i)CREATE\s+OR\s+REPLACE\s+VIEW\s+ceo_ai\.([a-z0-9_]+)`)
	views := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			views[strings.ToLower(m[1])] = e.Name()
		}
	}
	if len(views) == 0 {
		t.Fatal("no CREATE OR REPLACE VIEW ceo_ai.* found in migrations")
	}
	for v, file := range views {
		if _, ok := CardByName(v); !ok {
			t.Errorf("ceo_ai.%s (from %s) has no schema card", v, file)
		}
	}
}

func TestCardForSQLAndViewName(t *testing.T) {
	cases := map[string]string{
		"SELECT count(*) FROM ceo_ai.animal_current_scope WHERE tenant_id = 'x' LIMIT 1":       "animal_current_scope",
		"select 1 from CEO_AI.Mortality_Base m where m.tenant_id = 'x' limit 1":                "mortality_base",
		"SELECT 'ceo_ai.fake' AS label FROM ceo_ai.feed_adherence WHERE tenant_id='x' LIMIT 1": "fake", // first reference wins; a literal is the model's problem, Validate has the tenant
		"SELECT 1":   "",
		"":           "",
		"x_ceo_ai.y": "",
	}
	for sql, want := range cases {
		if got := ViewNameFromSQL(sql); got != want {
			t.Errorf("ViewNameFromSQL(%q) = %q want %q", sql, got, want)
		}
	}
	if c, ok := CardForSQL("SELECT count(*) FROM ceo_ai.mortality_base WHERE tenant_id = 'x' LIMIT 1"); !ok || c.DateColumn != "event_date" {
		t.Fatalf("CardForSQL: ok=%v card=%+v", ok, c)
	}
	if _, ok := CardForSQL("SELECT count(*) FROM ceo_ai.not_a_view WHERE tenant_id = 'x' LIMIT 1"); ok {
		t.Fatal("unknown view must not resolve to a card")
	}
	if c, ok := CardByName("ceo_ai.Animal_Current_Scope"); !ok || c.Name != "animal_current_scope" {
		t.Fatalf("CardByName with prefix/case: ok=%v %+v", ok, c)
	}
}

func TestRenderCardBlockIsBounded(t *testing.T) {
	block := RenderCardBlock()
	n := len(Cards())
	if got := strings.Count(block, "\n"); got != n { // legend line + n cards
		t.Fatalf("expected %d card lines after the legend, got %d", n, got)
	}
	// ~30-50 tokens per view target: bound the whole block so the planner
	// prompt cannot silently balloon (chars/4 is the same rough estimate the
	// budget fallback uses).
	if est := len(block) / 4; est > n*100 {
		t.Fatalf("card block too large: ~%d tokens for %d views (%s)", est, n, fmt.Sprintf("%d chars", len(block)))
	}
}
