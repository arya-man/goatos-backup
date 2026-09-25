package main

import "testing"

// Adversarial fixtures for the proven-performance-pattern rules (perfpatterns.go).
// Each rule has a FAILING example (the incident's shape) and a PASSING example (the fix).

const adapterPath = "backend/internal/example/adapters/postgres/repository.go"

func TestCountDistinctSortIsFlaggedAndTheHashedRewritePasses(t *testing.T) {
	bad := "package postgres\n\nconst matrixSQL = `SELECT shed_id, COUNT(DISTINCT target_id) FROM obligation_instances WHERE tenant_id = $1 GROUP BY shed_id`\n"
	repo, p := writeGoAt(t, adapterPath, bad)
	if got := rules(scanFile(repo, p))["count-distinct-sort"]; got != 1 {
		t.Fatalf("count-distinct-sort = %d, want 1 (cb0c2d0dc shape)", got)
	}
	good := "package postgres\n\nconst matrixSQL = `WITH per_animal AS (SELECT shed_id, target_id FROM obligation_instances WHERE tenant_id = $1 GROUP BY shed_id, target_id) SELECT shed_id, count(target_id) FROM per_animal GROUP BY shed_id`\n"
	repo, p = writeGoAt(t, adapterPath, good)
	if got := rules(scanFile(repo, p))["count-distinct-sort"]; got != 0 {
		t.Fatalf("count-distinct-sort = %d on the collapse-then-count rewrite, want 0", got)
	}
	ignored := "package postgres\n\n// scale-guard:ignore: at most 7 rows per shed by construction\nconst tinySQL = `SELECT COUNT(DISTINCT kind) FROM proof_kinds WHERE id = $1`\n"
	repo, p = writeGoAt(t, adapterPath, ignored)
	if got := rules(scanFile(repo, p))["count-distinct-sort"]; got != 0 {
		t.Fatalf("an ignore reason must suppress count-distinct-sort, got %d", got)
	}
}

func TestCTESelfJoinIsFlaggedAndTheWindowPairingPasses(t *testing.T) {
	bad := "package postgres\n\nconst fcrSQL = `WITH scan_rounds AS (SELECT pen, round_no, animal FROM rounds WHERE tenant_id = $1)\n" +
		"SELECT cur.animal FROM scan_rounds cur\n  JOIN scan_rounds prev ON prev.pen = cur.pen AND prev.round_no = cur.round_no - 1`\n"
	repo, p := writeGoAt(t, adapterPath, bad)
	if got := rules(scanFile(repo, p))["cte-self-join"]; got != 1 {
		t.Fatalf("cte-self-join = %d, want 1 (ca7b21a82 shape)", got)
	}
	// The same pairing with LAG over the pen's round order: no CTE is joined to itself.
	good := "package postgres\n\nconst fcrSQL = `WITH scan_rounds AS (SELECT pen, round_no, animal FROM rounds WHERE tenant_id = $1)\n" +
		"SELECT animal, LAG(round_no) OVER (PARTITION BY pen, animal ORDER BY round_no) FROM scan_rounds sr JOIN pens p ON p.pen = sr.pen`\n"
	repo, p = writeGoAt(t, adapterPath, good)
	if got := rules(scanFile(repo, p))["cte-self-join"]; got != 0 {
		t.Fatalf("cte-self-join = %d on the LAG rewrite, want 0", got)
	}
	// A base-table self-join is not this rule (review-only) -- no false positive.
	base := "package postgres\n\nconst q = `WITH x AS (SELECT 1) SELECT a.id FROM goats a JOIN goats b ON b.dam_id = a.id`\n"
	repo, p = writeGoAt(t, adapterPath, base)
	if got := rules(scanFile(repo, p))["cte-self-join"]; got != 0 {
		t.Fatalf("cte-self-join fired on a base-table self-join, got %d", got)
	}
	// Outside a postgres adapter the rule does not run.
	repo, p = writeGoAt(t, "backend/internal/example/app/service.go", bad)
	if got := rules(scanFile(repo, p))["cte-self-join"]; got != 0 {
		t.Fatalf("cte-self-join must be scoped to adapters, got %d", got)
	}
}

func TestCastInPredicateIsFlaggedAndTypedArrayPasses(t *testing.T) {
	bad := "package postgres\n\nconst kindsSQL = `SELECT kind FROM proof_artifacts pa WHERE pa.proof_id::text IN (SELECT value FROM jsonb_each_text($1))`\n"
	repo, p := writeGoAt(t, adapterPath, bad)
	if got := rules(scanFile(repo, p))["non-sargable-cast"]; got != 1 {
		t.Fatalf("non-sargable-cast = %d, want 1 (ebe349c37 shape)", got)
	}
	good := "package postgres\n\nconst kindsSQL = `SELECT kind FROM proof_artifacts pa WHERE pa.proof_id = ANY($1::uuid[])`\n"
	repo, p = writeGoAt(t, adapterPath, good)
	if got := rules(scanFile(repo, p))["non-sargable-cast"]; got != 0 {
		t.Fatalf("non-sargable-cast = %d on the typed-array form, want 0", got)
	}
}

func TestHandRolledReadCacheIsFlaggedOutsidePlatformReadcache(t *testing.T) {
	bad := "package app\n\nimport (\n\t\"sync\"\n\t\"time\"\n)\n\ntype growthCache struct {\n\tmu sync.Mutex\n\tentries map[string]time.Time\n}\n"
	repo, p := writeGoAt(t, "backend/internal/weighing/app/growth_cache.go", bad)
	if got := rules(scanFile(repo, p))["hand-rolled-read-cache"]; got != 1 {
		t.Fatalf("hand-rolled-read-cache = %d, want 1 (the pre-95b1054c1 weighing cache shape)", got)
	}
	repo, p = writeGoAt(t, "backend/internal/platform/readcache/cache.go", bad)
	if got := rules(scanFile(repo, p))["hand-rolled-read-cache"]; got != 0 {
		t.Fatalf("the shared readcache package itself must pass, got %d", got)
	}
	// A struct that holds a map but no mutex (e.g. a per-request memo) is not a shared cache.
	memo := "package app\n\ntype labelCache struct {\n\tbyID map[string]string\n}\n"
	repo, p = writeGoAt(t, "backend/internal/weighing/app/memo.go", memo)
	if got := rules(scanFile(repo, p))["hand-rolled-read-cache"]; got != 0 {
		t.Fatalf("a mutex-free memo must pass, got %d", got)
	}
}
