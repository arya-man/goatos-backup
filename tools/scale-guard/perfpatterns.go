package main

// projection-review: membership=source files and SQL string literals under the repo root; group_key=(rule, file) finding counts compared to baseline.txt; join_cardinality=each literal is scanned once per rule, no cross-file join; pagination=none, whole-repo static scan; scope=repository paths only, no tenant or runtime data

// Proven-performance-pattern rules (2026-09-25), mined from the perf commits that
// landed on main between 2026-08-15 and 2026-09-25 and from PR #415
// (perf/cold-queries). Each rule names the commit it came from; the canonical
// prose, bad/good snippets and the review-only siblings live in
// docs/decisions/scale-anti-patterns.md -> "Proven performance patterns
// (from main + #415)".
//
// Rules added here:
//
//   count-distinct-sort   COUNT(DISTINCT x) in serving SQL. Postgres implements it with a
//                         SORT of every input row (it cannot hash a DISTINCT aggregate), so a
//                         10k-row obligation set or a 32k-row unread set sorts -- and spills --
//                         before it counts. Collapse to one row per key first (GROUP BY /
//                         SELECT DISTINCT, both hashable) and count those rows.
//                         Evidence: cb0c2d0dc shed-dose matrix 725 -> 276 ms; ba2984573
//                         notification unread count 805 -> 232 ms (27 MB disk sort gone).
//   cte-self-join         a CTE of the same statement joined directly to itself
//                         (`FROM seg a JOIN seg b`). Under a generic plan this became a
//                         nested loop of two CTE scans (2,131 x 2,531, 720 ms of 730 ms). Pair
//                         rows with a window (LAG/LEAD, MIN() OVER ... RANGE) instead.
//                         Evidence: ca7b21a82 growth FCR 800 -> 100 ms.
//   hand-rolled-read-cache a struct named *cache* holding a map guarded by a sync mutex outside
//                         backend/internal/platform/readcache. Every hand-rolled cache on main
//                         re-implemented TTL without single flight, cross-instance eviction or a
//                         bound, and one of them served stale reads after a write
//                         (a056df98a). Use platform/readcache (95b1054c1).
//
// Extended (existing rule): non-sargable-cast now also catches `col::text IN (...)`, the
// shape behind ebe349c37 (proof-kinds subquery seq-scanned proof_artifacts per row,
// 53,487 ms -> 42 ms once it became `= ANY(uuid[])`).
//
// BLIND SPOTS (stated, not hidden):
//   - count-distinct-sort reads SQL text, not plans. A COUNT(DISTINCT) over a set that is
//     provably tiny is still flagged; mark it `// scale-guard:ignore: <why the set is small>`.
//     A DISTINCT hidden inside a SQL function or a view is not seen.
//   - cte-self-join sees only the ADJACENT shape `FROM c [a] JOIN c [b]` in one assembled
//     package-level statement. A self-join through a third relation
//     (`FROM c a JOIN x ON .. JOIN c b`), a correlated EXISTS over a CTE (856bfbbe0) and a
//     self-join of a base table are review-only.
//   - hand-rolled-read-cache is structural by NAME: a map+mutex struct not called *cache*
//     is missed, and an intentionally local memo (per-request) is flagged and must carry an
//     ignore reason.

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	countDistinctRe = regexp.MustCompile(`(?is)\bCOUNT\s*\(\s*DISTINCT\b`)
	castInRe        = regexp.MustCompile(`(?is)\b(?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*\s*::\s*(?:text|varchar)\s+IN\s*\(`)
	cteNameRe       = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s+AS\s+(?:NOT\s+)?(?:MATERIALIZED\s+)?\(`)
	adjSelfJoinRe   = regexp.MustCompile(`(?i)\bFROM\s+([a-z_][a-z0-9_]*)(?:\s+(?:AS\s+)?[a-z_][a-z0-9_]*)?\s+(?:(?:LEFT|RIGHT|INNER|FULL|CROSS)\s+(?:OUTER\s+)?)?JOIN\s+(?:LATERAL\s+)?([a-z_][a-z0-9_]*)\b`)
	sqlKeywordNames = map[string]bool{"select": true, "values": true, "lateral": true, "unnest": true, "generate_series": true}
)

// detectCountDistinct reports whether a SQL literal carries COUNT(DISTINCT ...).
func detectCountDistinct(v string) bool {
	return sqlishRe.MatchString(v) && countDistinctRe.MatchString(v)
}

// detectCastIn reports `col::text IN (` on the column side.
func detectCastIn(v string) bool {
	return sqlishRe.MatchString(v) && castInRe.MatchString(v)
}

// detectCTESelfJoin returns the CTE name joined directly to itself, or "".
func detectCTESelfJoin(text string) string {
	norm := strings.Join(strings.Fields(text), " ")
	ctes := map[string]bool{}
	for _, m := range cteNameRe.FindAllStringSubmatch(norm, -1) {
		n := strings.ToLower(m[1])
		if !sqlKeywordNames[n] {
			ctes[n] = true
		}
	}
	if len(ctes) == 0 {
		return ""
	}
	for _, m := range adjSelfJoinRe.FindAllStringSubmatch(norm, -1) {
		a, b := strings.ToLower(m[1]), strings.ToLower(m[2])
		if a == b && ctes[a] {
			return a
		}
	}
	return ""
}

var cacheTypeNameRe = regexp.MustCompile(`(?i)cache`)

// detectHandRolledReadCache finds struct types named *cache* that hold a map and a
// sync.Mutex/RWMutex, outside the shared platform/readcache package.
func detectHandRolledReadCache(rel string, file *ast.File) []token.Pos {
	if strings.Contains(filepath.ToSlash(rel), "internal/platform/readcache/") {
		return nil
	}
	var out []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !cacheTypeNameRe.MatchString(ts.Name.Name) {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		hasMap, hasMutex := false, false
		for _, f := range st.Fields.List {
			switch t := f.Type.(type) {
			case *ast.MapType:
				hasMap = true
			case *ast.SelectorExpr:
				if id, ok := t.X.(*ast.Ident); ok && id.Name == "sync" && (t.Sel.Name == "Mutex" || t.Sel.Name == "RWMutex") {
					hasMutex = true
				}
			}
		}
		if hasMap && hasMutex {
			out = append(out, ts.Pos())
		}
		return true
	})
	return out
}
