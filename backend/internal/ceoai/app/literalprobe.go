package app

// literalprobe.go: the generic "wrong filter value" repair.
//
// A model-drafted SQL read that filters a dimension column on a value the
// data does not use — sex = 'Female' when the column holds 'female',
// park_label = 'Coimbatore' when it holds 'CBE' — is VALID SQL: it passes the
// guard, runs, and returns nothing (or a zero count). The answer then reports
// a confident 0. The schema cards cannot list every value vocabulary, so this
// probes the actual values of each filtered dimension column (a bounded,
// guard-validated, tenant-scoped read) when a model read comes back empty or
// all-zero, and — only when a filter literal is not among those values —
// hands the model ONE repair with the real values. It is topic-free: it knows
// nothing about sexes or parks, only about "this literal is not a value of
// this column".

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// maxProbedValues bounds the values read per probed column.
const maxProbedValues = 40

var (
	eqLiteralRe = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s*(?:=|ilike|like)\s*'((?:[^']|'')*)'`)
	inListRe    = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s+in\s*\(([^)]*)\)`)
	quotedRe    = regexp.MustCompile(`'((?:[^']|'')*)'`)
)

// filterLiterals returns column -> literals used as equality/IN/LIKE filters.
func filterLiterals(sql string) map[string][]string {
	out := map[string][]string{}
	for _, m := range eqLiteralRe.FindAllStringSubmatch(sql, -1) {
		col := strings.ToLower(m[1])
		out[col] = append(out[col], strings.ReplaceAll(m[2], "''", "'"))
	}
	for _, m := range inListRe.FindAllStringSubmatch(sql, -1) {
		col := strings.ToLower(m[1])
		for _, q := range quotedRe.FindAllStringSubmatch(m[2], -1) {
			out[col] = append(out[col], strings.ReplaceAll(q[1], "''", "'"))
		}
	}
	return out
}

// emptyOrAllZero reports a read that found nothing to report.
func emptyOrAllZero(r domain.ToolResult) bool {
	if r.Err != nil {
		return false
	}
	if len(r.Facts) == 0 {
		return true
	}
	for _, f := range r.Facts {
		v, err := strconv.ParseFloat(strings.TrimSpace(f.Value), 64)
		if err != nil || v != 0 {
			return false
		}
	}
	return true
}

// likeMatches reports whether a LIKE/ILIKE-style literal (with % wildcards)
// or a plain literal matches one of the actual values (case-insensitively for
// patterns, exactly for plain equality).
func literalMatchesAny(lit string, values []string) bool {
	if strings.ContainsAny(lit, "%_") {
		pat := "^" + strings.ReplaceAll(strings.ReplaceAll(regexp.QuoteMeta(strings.ToLower(lit)), "%", ".*"), "_", ".") + "$"
		re, err := regexp.Compile(pat)
		if err != nil {
			return true
		}
		for _, v := range values {
			if re.MatchString(strings.ToLower(v)) {
				return true
			}
		}
		return false
	}
	for _, v := range values {
		if v == lit {
			return true
		}
	}
	return false
}

// probeFilterValues checks the filter literals of an empty/all-zero model SQL
// read against the real values of those dimension columns and returns a
// repair reason naming every literal that is not a value (empty = nothing to
// repair: the read is genuinely empty).
func (a *Assistant) probeFilterValues(ctx context.Context, actor domain.Actor, sql string) string {
	card, ok := reporting.CardForSQL(sql)
	if !ok {
		return ""
	}
	dims := map[string]bool{}
	for _, c := range card.GroupByColumns {
		dims[strings.ToLower(c)] = true
	}
	var problems []string
	lits := filterLiterals(sql)
	cols := make([]string, 0, len(lits))
	for c := range lits {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	for _, col := range cols {
		if !dims[col] || col == strings.ToLower(card.DateColumn) || strings.HasSuffix(col, "_id") || col == "tenant_id" {
			continue
		}
		values, err := a.distinctValues(ctx, actor, card.Name, col)
		if err != nil {
			// The probe is best-effort — it only explains an empty read — but a
			// silent skip made a repeatedly failing probe indistinguishable from a
			// column with no stored values, so it is logged rather than swallowed.
			a.log.WarnContext(ctx, "ceoai filter-value probe failed",
				"error", err, "view", card.Name, "column", col, "tenant_id", actor.TenantID)
			continue
		}
		if len(values) == 0 {
			continue
		}
		for _, lit := range lits[col] {
			if !literalMatchesAny(lit, values) {
				problems = append(problems, fmt.Sprintf("column %s has no value %q; its actual values are: %s", col, lit, strings.Join(values, ", ")))
			}
		}
	}
	if len(problems) == 0 {
		return ""
	}
	return "the query ran but matched nothing because a filter value is not used in the data — " + strings.Join(problems, "; ") + ". Use the exact stored values."
}

// distinctValues reads the distinct values of one dimension column of one
// ceo_ai view for the actor's tenant, through the same guarded executor.
func (a *Assistant) distinctValues(ctx context.Context, actor domain.Actor, view, col string) ([]string, error) {
	probe := fmt.Sprintf("SELECT 'value' AS label, CAST(%s AS text) AS value FROM ceo_ai.%s WHERE tenant_id = %s AND %s IS NOT NULL GROUP BY %s LIMIT %d",
		col, view, sqlStringLiteral(actor.TenantID), col, col, maxProbedValues)
	res, err := a.registry.Execute(ctx, actor, domain.SubQuestion{
		ID: "value_probe", Route: domain.RouteSQL, ToolName: "sql_fallback",
		// Marked server-authored (not a model draft) so no period contract
		// applies; it still runs through sqlguard + the tenant-bound executor.
		Params: map[string]any{"sql": probe, "natural_sql": "value_probe"},
	})
	if err != nil {
		return nil, err
	}
	if res.Err != nil {
		return nil, res.Err
	}
	out := make([]string, 0, len(res.Facts))
	for _, f := range res.Facts {
		if v := strings.TrimSpace(f.Value); v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

// repairEmptyFilterReads is the generic wrong-filter-value repair: for each
// model-drafted SQL read that succeeded but is empty/all-zero, probe its
// filter literals and, when one is not a stored value, ask the model ONCE for
// a corrected draft with the real values, re-run it through the same guard,
// and keep it when it returns data.
func (a *Assistant) repairEmptyFilterReads(ctx context.Context, q domain.Question, subs []domain.SubQuestion, results []domain.ToolResult, traces *[]domain.StepTrace) TokenUsage {
	var usage TokenUsage
	repairer, canRepair := a.provider.(sqlRepairer)
	if !canRepair {
		return usage
	}
	for i := range results {
		if i >= len(subs) || !isModelSQL(subs[i]) || !emptyOrAllZero(results[i]) {
			continue
		}
		sql, _ := subs[i].Params["sql"].(string)
		reason := a.probeFilterValues(ctx, q.Actor, sql)
		if reason == "" {
			continue
		}
		cardText := ""
		if card, ok := reporting.CardForSQL(sql); ok {
			cardText = card.RenderCompact()
		}
		windowText := ""
		if w, ok := sqlWindowFromParams(subs[i].Params); ok {
			col := "<date_col>"
			if card, ok := reporting.CardForSQL(sql); ok && card.DateColumn != "" {
				col = card.DateColumn
			}
			windowText = col + " >= '" + w.FromLiteral() + "' AND " + col + " < '" + w.ToExclusiveLiteral() + "'"
		}
		start := a.now()
		fixed, u, err := repairer.RepairSQL(ctx, q, sql, reason, cardText, windowText)
		usage = usage.add(u)
		if err != nil {
			continue
		}
		retry := subs[i]
		retry.Params = make(map[string]any, len(subs[i].Params)+1)
		for k, v := range subs[i].Params {
			retry.Params[k] = v
		}
		retry.Params["sql"] = fixed
		retry.Params["_repaired_from"] = "filter_value"
		res, execErr := a.registry.Execute(ctx, q.Actor, retry)
		if execErr == nil && res.Err != nil {
			execErr = res.Err
		}
		*traces = append(*traces, domain.StepTrace{
			SubQuestionID: subs[i].ID, Route: domain.RouteSQL, ToolName: "sql_filter_value_repair",
			StartedAt: start, DurationMS: a.now().Sub(start).Milliseconds(),
			RowCount: len(res.Facts), Err: errString(execErr),
		})
		if execErr != nil || emptyOrAllZero(res) {
			continue
		}
		res.SubQuestionID = subs[i].ID
		if res.Route == "" {
			res.Route = domain.RouteSQL
		}
		if res.ToolName == "" {
			res.ToolName = subs[i].ToolName
		}
		results[i] = res
		subs[i].Params["sql"] = fixed
	}
	return usage
}
