package main

// projection-review: membership=SQL string literals and assembled package-level SQL consts under backend/; group_key=(rule, file) finding counts compared to baseline.txt; join_cardinality=each statement scanned once, no cross-file join; pagination=none, whole-repo static scan; scope=repository paths only, no tenant or runtime data

// or-subquery-membership (PP-22, 2026-09-26).
//
// Incident: a processintegrity aggregate was "optimised" by rewriting
// `col = ANY(ARRAY(SELECT ...))` to `col IN (SELECT ...)` INSIDE an OR. At STG size
// (86k obligation rows) it was faster. At 500k rows Postgres could no longer build a
// BitmapOr over the branches' indexes and fell back to a Seq Scan of obligation_instances
// (1.2 s); only TestProcessIntegrityCanonicalAggregateQueryPlanUsesIndexesAtScale caught it.
//
// Rule: a statement that reads a large table (largeTables) and has an OR whose branches
// (at any paren depth, AND binds tighter) contain a subquery membership test
// -- IN (SELECT ..), EXISTS (..), = ANY(SELECT ..), = ANY(ARRAY(SELECT ..)) -- is flagged.
//
// Accepted fixes (not flagged): split the OR into a UNION / UNION ALL of per-branch,
// index-driven sets; or keep it with
//   // scale-guard:ignore: <reason>; plan: Test...AtScale (or validate-sqlc-plans <Entry>)
// An ignore for THIS rule without an at-scale plan reference (a Test*AtScale name,
// validate-sqlc-plans or an explain_* entry) does not suppress it.
//
// BLIND SPOTS: SQL text, not plans. A subquery over a provably tiny set is still flagged
// (ignore with the plan test). SQL built at runtime (fmt.Sprintf / strings.Builder) is
// not assembled. A large table reached only through a view or SQL function is not seen.

import (
	"regexp"
	"sort"
	"strings"
)

// largeTables are tables that grow with animals x time; every hot read over them must
// hold its plan at the 500k-obligation-row envelope, not just at STG size.
var largeTables = []string{
	"obligation_instances", "obligation_batches", "verification_items",
	"feed_direction_issue_rows", "goats", "goat_identifiers", "goat_identity_events",
	"weighing_observations", "audit_log", "outbox_messages", "notification_requests",
	"proof_artifacts", "herd_signal_packets", "herd_signal_tag_latest",
	"herd_signal_activity_windows", "herd_signal_pen_medians",
}

var (
	largeTableRe = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INTO)\s+(?:ONLY\s+)?(?:public\.)?(` +
		strings.Join(largeTables, "|") + `)\b`)
	orTokRe          = regexp.MustCompile(`(?i)\bOR\b`)
	subqueryMemberRe = regexp.MustCompile(`(?i)\bIN\s*\(\s*SELECT\b|\bEXISTS\s*\(|=\s*ANY\s*\(\s*(?:ARRAY\s*\(\s*)?SELECT\b`)
	// Clause keywords that end an OR expression at its own paren depth.
	orBoundRe      = regexp.MustCompile(`(?i)\b(?:SELECT|FROM|WHERE|ON|HAVING|WHEN|THEN|ELSE|END|JOIN|GROUP\s+BY|ORDER\s+BY|LIMIT|OFFSET|UNION|INTERSECT|EXCEPT|RETURNING|SET|USING|WINDOW)\b|,|;`)
	planProofRefRe = regexp.MustCompile(`Test\w*AtScale|validate-sqlc-plans|explain_\w+`)
)

// referencedLargeTables returns the large tables a statement reads or writes.
func referencedLargeTables(masked string) []string {
	seen := map[string]bool{}
	for _, m := range largeTableRe.FindAllStringSubmatch(masked, -1) {
		seen[strings.ToLower(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// detectORSubqueryMembership returns the large tables of a statement whose OR predicate
// has a subquery-membership branch, or nil.
func detectORSubqueryMembership(text string) []string {
	tables, _ := detectORSubqueryMembershipExpr(text)
	return tables
}

// detectORSubqueryMembershipExpr also returns the offending OR expression (whitespace-normalised),
// so a fragment shared by several assembled statements is reported once per file.
func detectORSubqueryMembershipExpr(text string) ([]string, string) {
	if !sqlishRe.MatchString(text) || !orTokRe.MatchString(text) || !subqueryMemberRe.MatchString(text) {
		return nil, ""
	}
	masked := maskSQLNoise(text)
	tables := referencedLargeTables(masked)
	if len(tables) == 0 {
		return nil, ""
	}
	depth := make([]int, len(masked)+1)
	d := 0
	for i := 0; i < len(masked); i++ {
		depth[i] = d
		switch masked[i] {
		case '(':
			d++
		case ')':
			d--
		}
	}
	depth[len(masked)] = d
	for _, loc := range orTokRe.FindAllStringIndex(masked, -1) {
		od := depth[loc[0]]
		// Enclosing group: nearest unmatched '(' before, ')' after.
		lo, hi := 0, len(masked)
		for i := loc[0] - 1; i >= 0; i-- {
			if masked[i] == '(' && depth[i] == od-1 {
				lo = i + 1
				break
			}
		}
		for i := loc[1]; i < len(masked); i++ {
			if masked[i] == ')' && depth[i] == od {
				hi = i
				break
			}
		}
		// The OR must filter a large table read at its OWN query level (a large table only
		// reached inside the EXISTS / IN subquery is an index probe per outer row).
		if !queryScopeReadsLargeTable(masked, depth, loc[0], od) {
			continue
		}
		// Narrow to clause keywords at the OR's own depth.
		base := lo
		for _, b := range orBoundRe.FindAllStringIndex(masked[base:loc[0]], -1) {
			if s, e := base+b[0], base+b[1]; depth[s] == od && e > lo {
				lo = e
			}
		}
		for _, b := range orBoundRe.FindAllStringIndex(masked[loc[1]:hi], -1) {
			s := loc[1] + b[0]
			if depth[s] == od {
				hi = s
				break
			}
		}
		if lo < hi && orHasIndexDefeatingBranch(masked, depth, lo, hi, od) {
			return tables, strings.Join(strings.Fields(masked[lo:hi]), " ")
		}
	}
	return nil, ""
}

func orSubqueryMsg(name string, tables []string) string {
	if name != "" {
		name += ": "
	}
	return name + "OR predicate with a subquery-membership branch (IN/EXISTS/= ANY(SELECT)) over large table(s) " +
		strings.Join(tables, ", ") + "; at 500k rows the planner cannot BitmapOr the branch indexes and seq-scans (PP-22). " +
		"Split into UNION/UNION ALL of per-branch index-driven sets, or add `scale-guard:ignore: <reason>; plan: Test...AtScale` naming a plan test run at scale"
}

// ignoreHasPlanProof reports whether the ignore comment on line (1-based) or the line above
// names an at-scale plan test.
func ignoreHasPlanProof(lines []string, line int) bool {
	for _, l := range []int{line, line - 1} {
		if l >= 1 && l <= len(lines) && ignoreRe.MatchString(lines[l-1]) && planProofRefRe.MatchString(lines[l-1]) {
			return true
		}
	}
	return false
}

var (
	// A branch that only tests binds (`$3::text = ''`, `NOT $5::bool`, `$2 IS NULL`) is an
	// optional-filter switch, not an indexable predicate; ORing it with a subquery is a
	// different (generic-plan) concern and is not this rule.
	colRefRe      = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*\b`)
	fmtVerbRe     = regexp.MustCompile(`%(?:\[\d+\])?[a-zA-Z]`)
	bareIdentRe   = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]*\b`)
	bindOnlyWords = map[string]bool{"is": true, "not": true, "null": true, "true": true, "false": true, "and": true,
		"nullif": true, "coalesce": true, "cardinality": true, "array_length": true, "btrim": true, "trim": true, "lower": true,
		"text": true, "bool": true, "boolean": true, "uuid": true, "int": true, "integer": true, "bigint": true, "date": true,
		"timestamptz": true, "numeric": true, "varchar": true, "jsonb": true, "distinct": true, "from": true}
	// Subqueries that only read binds / literals are tiny by construction.
	realTableFromRe = regexp.MustCompile(`(?i)\bFROM\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\b(?:\s*\()?`)
	tinySources     = map[string]bool{"unnest": true, "values": true, "generate_series": true, "jsonb_array_elements": true,
		"jsonb_array_elements_text": true, "jsonb_each": true, "jsonb_each_text": true, "json_array_elements": true, "jsonb_to_recordset": true}
)

func isBindOnlyBranch(b string) bool {
	b = fmtVerbRe.ReplaceAllString(b, "")
	if !strings.Contains(b, "$") || colRefRe.MatchString(b) || subqueryMemberRe.MatchString(b) {
		return false
	}
	for _, w := range bareIdentRe.FindAllString(b, -1) {
		if !bindOnlyWords[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// membershipOverRealTable reports whether a branch holds a subquery membership whose
// subquery reads a real table (not unnest/VALUES of binds).
func membershipOverRealTable(masked string, depth []int, lo, hi int) bool {
	for _, m := range subqueryMemberRe.FindAllStringIndex(masked[lo:hi], -1) {
		start := lo + m[1] // just past the opening "(" or SELECT
		d := depth[start]
		end := hi
		for i := start; i < hi; i++ {
			if masked[i] == ')' && depth[i] < d {
				end = i
				break
			}
		}
		for _, f := range realTableFromRe.FindAllStringSubmatch(masked[start:end], -1) {
			if !tinySources[strings.ToLower(f[1])] {
				return true
			}
		}
	}
	return false
}

// orHasIndexDefeatingBranch splits [lo,hi) at depth-od ORs and reports whether one branch is
// a subquery membership over a real table while another branch is a real (column) predicate.
func orHasIndexDefeatingBranch(masked string, depth []int, lo, hi, od int) bool {
	var branches [][2]int
	start := lo
	for _, o := range orTokRe.FindAllStringIndex(masked[lo:hi], -1) {
		if depth[lo+o[0]] == od {
			branches = append(branches, [2]int{start, lo + o[0]})
			start = lo + o[1]
		}
	}
	branches = append(branches, [2]int{start, hi})
	if len(branches) < 2 {
		return false
	}
	member, columnPred := 0, 0
	for _, b := range branches {
		if membershipOverRealTable(masked, depth, b[0], b[1]) {
			member++
		} else if !isBindOnlyBranch(masked[b[0]:b[1]]) {
			columnPred++
		}
	}
	return member > 0 && member+columnPred >= 2
}

func largeTableAtDepth(masked string, depth []int, lo, hi, od int) bool {
	for _, m := range largeTableRe.FindAllStringIndex(masked[lo:hi], -1) {
		if depth[lo+m[0]] == od {
			return true
		}
	}
	return false
}

var selectRe = regexp.MustCompile(`(?i)\bSELECT\b`)

// enclosingGroup returns the [lo,hi) span of the paren group at depth d containing pos.
func enclosingGroup(masked string, depth []int, pos, d int) (int, int) {
	lo, hi := 0, len(masked)
	for i := pos - 1; i >= 0; i-- {
		if masked[i] == '(' && depth[i] == d-1 {
			lo = i + 1
			break
		}
	}
	for i := pos; i < len(masked); i++ {
		if masked[i] == ')' && depth[i] == d {
			hi = i
			break
		}
	}
	return lo, hi
}

// queryScopeReadsLargeTable walks out from the OR's boolean parens to the SELECT (or
// UPDATE/DELETE) it filters and reports whether that level reads a large table.
func queryScopeReadsLargeTable(masked string, depth []int, pos, od int) bool {
	for d := od; d >= 0; d-- {
		lo, hi := enclosingGroup(masked, depth, pos, d)
		hasSelect := d == 0
		for _, m := range selectRe.FindAllStringIndex(masked[lo:hi], -1) {
			if depth[lo+m[0]] == d {
				hasSelect = true
				break
			}
		}
		if hasSelect {
			return largeTableAtDepth(masked, depth, lo, hi, d)
		}
	}
	return false
}
