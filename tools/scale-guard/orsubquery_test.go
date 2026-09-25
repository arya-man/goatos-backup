package main

import "testing"

// Fixtures for or-subquery-membership (PP-22). The FAILING shapes are the processintegrity
// incident (IN (SELECT ..) inside an OR over obligation_instances) and its siblings; the
// PASSING shapes are the accepted fixes and the look-alikes that are not this class.

func orHits(t *testing.T, src string) int {
	t.Helper()
	repo, p := writeGoAt(t, adapterPath, src)
	return rules(scanFile(repo, p))["or-subquery-membership"]
}

func TestORSubqueryMembershipFlagsTheIncidentShapes(t *testing.T) {
	for name, sql := range map[string]string{
		"in-select (incident)": `SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.tenant_id = $1
  AND (oi.due_at <= $2 OR oi.batch_id IN (SELECT batch_id FROM due_window_batches))`,
		"exists": `SELECT g.goat_id FROM goats g WHERE g.tenant_id = $1
  AND (g.shed_id = $2 OR EXISTS (SELECT 1 FROM pen_moves m WHERE m.goat_id = g.goat_id))`,
		"any-select": `SELECT vi.item_id FROM verification_items vi WHERE vi.status = 'open' OR vi.item_id = ANY(SELECT item_id FROM overrides)`,
		"any-array-select": `SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.tenant_id = $1
  AND (oi.due_at <= $2 OR oi.batch_id = ANY(ARRAY(SELECT batch_id FROM due_window_batches)))`,
		"deep in a CTE, AND binds tighter": `WITH w AS (SELECT o.id FROM weighing_observations o
  WHERE o.tenant_id = $1 AND ((o.accepted_at > $2 AND o.pen_id = $3) OR o.goat_id IN (SELECT goat_id FROM moved)))
SELECT id FROM w`,
		"join ON": `SELECT a.id FROM audit_log a JOIN actors x ON x.id = a.actor_id OR a.actor_id IN (SELECT id FROM aliases)`,
	} {
		src := "package postgres\n\nconst q = `" + sql + "`\n"
		if got := orHits(t, src); got != 1 {
			t.Errorf("%s: or-subquery-membership = %d, want 1", name, got)
		}
	}
}

func TestORSubqueryMembershipAcceptsFixesAndLookAlikes(t *testing.T) {
	for name, sql := range map[string]string{
		"union all per branch (the fix)": `SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.tenant_id = $1 AND oi.due_at <= $2
UNION ALL
SELECT oi.obligation_id FROM obligation_instances oi JOIN due_window_batches b ON b.batch_id = oi.batch_id WHERE oi.tenant_id = $1 AND oi.due_at > $2`,
		"membership ANDed, OR elsewhere":       `SELECT oi.obligation_id FROM obligation_instances oi WHERE (oi.status = 'due' OR oi.status = 'missed') AND oi.batch_id IN (SELECT batch_id FROM b)`,
		"optional-filter switch":               `SELECT o.id FROM weighing_observations o WHERE o.tenant_id = $1 AND (NOT $5::bool OR EXISTS (SELECT 1 FROM flags f WHERE f.id = o.id))`,
		"membership over binds only":           `SELECT o.id FROM weighing_observations o WHERE o.pen_id = $2 OR EXISTS (SELECT 1 FROM unnest($6::uuid[]) AS b(loc) WHERE b.loc = o.pen_id)`,
		"large table only inside the subquery": `SELECT l.location_id FROM locations l WHERE l.kind = 'shed' OR EXISTS (SELECT 1 FROM goats g WHERE g.shed_id = l.location_id)`,
		"small table":                          `SELECT p.id FROM parks p WHERE p.id = $1 OR p.id IN (SELECT park_id FROM grants)`,
		"OR inside the subquery":               `SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.batch_id IN (SELECT batch_id FROM b WHERE b.a = 1 OR b.c = 2)`,
	} {
		src := "package postgres\n\nconst q = `" + sql + "`\n"
		if got := orHits(t, src); got != 0 {
			t.Errorf("%s: or-subquery-membership = %d, want 0", name, got)
		}
	}
}

func TestORSubqueryMembershipIgnoreMustNameAnAtScalePlanTest(t *testing.T) {
	sql := "SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.due_at <= $2 OR oi.batch_id IN (SELECT batch_id FROM b)"
	bare := "package postgres\n\n// scale-guard:ignore: fast on STG\nconst q = `" + sql + "`\n"
	if got := orHits(t, bare); got != 1 {
		t.Fatalf("an ignore without a plan test must not suppress, got %d", got)
	}
	proved := "package postgres\n\n// scale-guard:ignore: BitmapOr holds at 500k; plan: TestExampleQueryPlanUsesIndexesAtScale\nconst q = `" + sql + "`\n"
	if got := orHits(t, proved); got != 0 {
		t.Fatalf("an ignore naming a Test*AtScale plan test must suppress, got %d", got)
	}
	sqlc := "package postgres\n\n// scale-guard:ignore: initplan array; plan: validate-sqlc-plans ObligationDueWindow\nconst q = `" + sql + "`\n"
	if got := orHits(t, sqlc); got != 0 {
		t.Fatalf("an ignore naming a validate-sqlc-plans entry must suppress, got %d", got)
	}
}

func TestORSubqueryMembershipReadsAssembledStatementsOnce(t *testing.T) {
	// The OR lives in a shared fragment embedded by two statements: one finding, not three.
	src := "package postgres\n\nconst frag = `WITH x AS (SELECT oi.obligation_id FROM obligation_instances oi WHERE oi.due_at <= $2 OR EXISTS (SELECT 1 FROM m WHERE m.id = oi.obligation_id))`\n" +
		"const a = frag + ` SELECT * FROM x`\nconst b = frag + ` SELECT count(*) FROM x`\n"
	if got := orHits(t, src); got != 1 {
		t.Fatalf("shared fragment counted %d times, want 1", got)
	}
	// Split across fragments: only the assembled text holds both the table and the OR.
	split := "package postgres\n\nconst head = `SELECT oi.obligation_id FROM obligation_instances oi WHERE `\n" +
		"const q = head + `oi.due_at <= $2 OR oi.batch_id IN (SELECT batch_id FROM b)`\n"
	if got := orHits(t, split); got != 1 {
		t.Fatalf("assembled statement: got %d, want 1", got)
	}
}
