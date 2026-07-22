package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
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
var fallbackAliases = map[string]fallbackAlias{
	"counts_breakdown":     {cube: "active_animals", api: "counts_breakdown", toolbox: "mesha_count_by_scope"},
	"feed_direction_today": {api: "feed_direction_today", toolbox: "mesha_feed_direction_summary"},
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
				Params:      subs[i].Params,
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
