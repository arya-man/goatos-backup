package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// injectAsOf stamps the resolved IST business instant onto every
// sub-question's Params as "as_of" (YYYY-MM-DD), unless a sub already set one
// explicitly (e.g. a diagnostic decomposition pinning its own window). This is
// the P1-4 thread: Question.AsOf was resolved at the top of the pipeline but
// previously never reached SubQuestion/executor signatures, so a scoped/
// as-of question ("feed direction as of yesterday", "counts as of yesterday")
// could not actually honor it downstream.
func injectAsOf(subs []domain.SubQuestion, asOf time.Time) {
	if asOf.IsZero() {
		return
	}
	day := asOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	for i := range subs {
		if subs[i].Params == nil {
			subs[i].Params = map[string]any{}
		}
		if _, ok := subs[i].Params["as_of"]; !ok {
			subs[i].Params["as_of"] = day
		}
	}
}

// injectWindow threads the server-resolved period (plan v3 D1.2) into every
// sub-question's Params as ISO business dates: from/to (inclusive),
// window_label, and compare_from/compare_to for a two-window comparison.
//
// The window is SERVER truth and is never read back from the plan: whatever
// from/to/compare_* a model-drafted plan seeded is OVERWRITTEN when a window
// resolved from the question text, and STRIPPED when none did (PR #318 M2).
// A model can therefore neither shift the period the guard enforces
// (validateModelSQL binds exactly these params) nor smuggle a period into a
// question that asked for none. The comparison window rides on every
// sub-question too: the planner is asked to draft one sub-question per
// window, and validateModelSQL accepts a model-SQL sub that binds EITHER the
// primary or the comparison window (M1), so the comparison arm is answerable.
// questionNamedDateColumns returns the date columns the QUESTION itself named,
// across every card: a column is named when the question carries the words its
// own name is made of ("purchased" -> purchase_date, "verified" -> verified_at).
// It is derived from the column NAME, so a column added tomorrow needs no list.
func questionNamedDateColumns(questionText string) []string {
	words := questionWords(questionText)
	if len(words) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, card := range reporting.Cards() {
		for _, col := range card.AlternateDateColumns() {
			if seen[strings.ToLower(col)] {
				continue
			}
			// The column's own distinguishing word, minus the date suffix:
			// purchase_date -> "purchase", verified_at -> "verified".
			for _, token := range strings.Split(strings.ToLower(col), "_") {
				if token == "date" || token == "at" || token == "day" || token == "on" || len(token) < 4 {
					continue
				}
				if questionNames(words, token) {
					seen[strings.ToLower(col)] = true
					out = append(out, col)
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// questionNames reports whether any of the question's words is, or starts with,
// the column's token ("purchased" names purchase; "verification" names verified
// only through its shared stem, so both directions are checked).
func questionNames(words map[string]bool, token string) bool {
	for w := range words {
		if w == token || strings.HasPrefix(w, token) || strings.HasPrefix(token, w) {
			return true
		}
	}
	return false
}

func injectWindow(subs []domain.SubQuestion, w Window, questionText string) {
	for i := range subs {
		if subs[i].Params == nil {
			subs[i].Params = map[string]any{}
		}
		for _, k := range []string{paramFrom, paramTo, paramWindowLabel, paramCompareFrom, paramCompareTo, paramWindowAltCols} {
			delete(subs[i].Params, k)
		}
		if w.IsZero() {
			continue
		}
		if alt := questionNamedDateColumns(questionText); len(alt) > 0 {
			subs[i].Params[paramWindowAltCols] = strings.Join(alt, ",")
		}
		subs[i].Params[paramFrom] = w.FromDate()
		subs[i].Params[paramTo] = w.ToDate()
		if w.Label != "" {
			subs[i].Params[paramWindowLabel] = w.Label
		}
		if w.Compare != nil && !w.Compare.IsZero() {
			subs[i].Params[paramCompareFrom] = w.Compare.FromDate()
			subs[i].Params[paramCompareTo] = w.Compare.ToDate()
		}
	}
}

// fallbackTierOrder is the committed Cube -> API -> Toolbox -> SQL hierarchy
// (see registry.go). A retry walks this order, skipping the tier that already
// ran and any tier the alias does not define.
var fallbackTierOrder = []domain.Route{
	domain.RouteCube,
	domain.RouteAPI,
	domain.RouteToolbox,
	domain.RouteSQL,
}

// fallbackAlias names the SAME business question at every tier that can
// actually answer it, so a sub-question that failed (or came back empty) at
// its planned tier can be retried at the next one instead of the pipeline
// reporting "isn't available yet" while a working tier sits right there
// unused (P1-3). Only tools that genuinely serve the same question are
// listed; an empty string means that tier has no equivalent.
type fallbackAlias struct {
	cube    string
	api     string
	toolbox string
	sql     string
}

func (fa fallbackAlias) toolFor(route domain.Route) string {
	switch route {
	case domain.RouteCube:
		return fa.cube
	case domain.RouteAPI:
		return fa.api
	case domain.RouteToolbox:
		return fa.toolbox
	case domain.RouteSQL:
		return fa.sql
	default:
		return ""
	}
}

// fallbackAliases maps a sub-question's resolved tool name (whatever tier it
// was routed to) to its equivalents at the other tiers.
//
//   - counts_breakdown (API): the same census question is also answered by
//     the Cube active_animals metric (species/park/shed grouping) and by the
//     MCP Toolbox mesha_count_by_scope tool (park/shed/species/stage/sex
//     grouping straight off ceo_ai.animal_current_scope).
//   - feed_direction_today (API): no governed Cube feed metric exists
//     (docs/ceo-ai/coverage-matrix.md), so the only real fallback is the MCP
//     Toolbox mesha_feed_direction_summary tool, which reads the same
//     ceo_ai.feed_direction_current view the API executor is meant to read.
//
// Every RouteAPI intent whose in-process reader is not wired MUST have a
// Toolbox (or Cube/SQL) alias here, so no leadership question dead-ends. Each
// mesha_* Toolbox tool reads the same ceo_ai.* view the API executor targets
// (see docs/ceo-ai/mcp-toolbox-tools.yaml). The route-closure guard + the
// keywordplanner catalog_consistency_test enforce that this map covers every
// unwired RouteAPI planner target.
var fallbackAliases = map[string]fallbackAlias{
	"active_animals":                 {cube: "active_animals", api: "counts_breakdown", toolbox: "mesha_count_by_scope"},
	"total_animals":                  {cube: "total_animals", api: "counts_breakdown", toolbox: "mesha_count_by_scope"},
	"counts_breakdown":               {cube: "active_animals", api: "counts_breakdown", toolbox: "mesha_count_by_scope"},
	"vaccination_due":                {cube: "vaccination_due", api: "vaccination_shed_summary", toolbox: "mesha_vaccination_due_summary"},
	"vaccination_due_today":          {cube: "vaccination_due_today", api: "vaccination_shed_summary", toolbox: "mesha_vaccination_due_summary"},
	"vaccination_overdue":            {cube: "vaccination_overdue", api: "vaccination_shed_summary", toolbox: "mesha_vaccination_due_summary"},
	"vaccination_compliance":         {cube: "vaccination_compliance", api: "vaccination_shed_summary", toolbox: "mesha_vaccination_due_summary"},
	"feed_direction_today":           {api: "feed_direction_today", toolbox: "mesha_feed_direction_summary"},
	"procurement_source_entry_loads": {api: "procurement_source_entry_loads", toolbox: "mesha_procurement_summary"},
	"admin_roster_coverage":          {api: "admin_roster_coverage", toolbox: "mesha_workforce_coverage"},
	"verification_queue":             {api: "verification_queue", toolbox: "mesha_verification_queue"},
	"action_center_obligations":      {api: "action_center_obligations", toolbox: "mesha_action_center"},
	"operations_kernel_health":       {api: "operations_kernel_health", toolbox: "mesha_ops_exceptions"},
	"operations_audit_summary":       {api: "operations_audit_summary", toolbox: "mesha_audit_summary"},
	"admin_location_usage":           {api: "admin_location_usage", toolbox: "mesha_capacity_summary"},
}

func fallbackAliasFor(toolName string) (fallbackAlias, bool) {
	fa, ok := fallbackAliases[toolName]
	return fa, ok
}

// resultNeedsFallback reports whether a tool result failed to actually
// resolve the sub-question — either it errored, or it silently came back with
// no grounding facts (which review.go's completeness check would otherwise
// catch too late, after compose).
func resultNeedsFallback(r domain.ToolResult) bool {
	return r.Err != nil || len(r.Facts) == 0
}

// retryFailedResults walks every result that failed/came back empty and, if a
// fallback alias exists for its tool, tries the SAME sub-question at each
// remaining tier in Cube -> API -> Toolbox -> SQL order (skipping the tier
// that already ran and any unwired/unaliased tier) until one returns real
// grounding facts. It mutates results in place and reports whether anything
// changed, so the caller knows whether to recompose/re-review.
//
// This is the runtime fallback P1-3 requires: previously registry.go ran ONLY
// the planner's chosen route, and an errored/empty result went straight to
// compose — an unwired API executor (e.g. feed, counts before this fix) meant
// the user got "isn't available yet" even when Cube or the Toolbox could
// answer the same question.
func (a *Assistant) retryFailedResults(ctx context.Context, actor domain.Actor, subs []domain.SubQuestion, results []domain.ToolResult) bool {
	changed := false
	for i := range results {
		if i >= len(subs) {
			break
		}
		if !resultNeedsFallback(results[i]) {
			continue
		}
		alias, ok := fallbackAliasFor(results[i].ToolName)
		if !ok {
			continue
		}
		triedRoute := results[i].Route
		for _, route := range fallbackTierOrder {
			if route == triedRoute {
				continue
			}
			toolName := alias.toolFor(route)
			if toolName == "" {
				continue
			}
			retrySub := domain.SubQuestion{
				ID:          subs[i].ID,
				Text:        subs[i].Text,
				IntentClass: subs[i].IntentClass,
				Route:       route,
				ToolName:    toolName,
				Params:      fallbackParams(subs[i].Params, results[i].ToolName),
			}
			res, err := a.registry.Execute(ctx, actor, retrySub)
			if err != nil || res.Err != nil || len(res.Facts) == 0 {
				continue
			}
			res.SubQuestionID = subs[i].ID
			if res.Route == "" {
				res.Route = route
			}
			if res.ToolName == "" {
				res.ToolName = toolName
			}
			results[i] = res
			changed = true
			break
		}
	}
	return changed
}

func fallbackParams(params map[string]any, failedTool string) map[string]any {
	next := make(map[string]any, len(params)+1)
	for k, v := range params {
		next[k] = v
	}
	if failedTool != "" {
		next["_fallback_from_tool"] = failedTool
	}
	return next
}
