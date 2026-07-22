package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Registry is the in-process tool catalog. It composes, in Cube-first order:
//  1. Cube governed metrics (via MetricService)
//  2. Mesha read-service tools (registered ToolExecutors — in-process, NOT
//     HTTP self-calls)
//  3. MCP Toolbox curated tools
//  4. sqlguard-validated SQL fallback
//
// The orchestrator asks the Registry for the model-facing catalog and for the
// executor that resolves a routed sub-question.
type Registry struct {
	mu        sync.RWMutex
	metrics   ports.MetricService
	executors map[string]ports.ToolExecutor // read-service + toolbox tools by name
	toolbox   ports.Toolbox
	sqlFB     ports.SQLFallback
}

// NewRegistry builds a Registry. Any port may be nil; the catalog and routing
// degrade to the tiers that are wired.
func NewRegistry(metrics ports.MetricService, toolbox ports.Toolbox, sqlFB ports.SQLFallback) *Registry {
	return &Registry{
		metrics:   metrics,
		executors: map[string]ports.ToolExecutor{},
		toolbox:   toolbox,
		sqlFB:     sqlFB,
	}
}

// Register adds an in-process read-service ToolExecutor (tier 2 / api).
func (r *Registry) Register(execs ...ports.ToolExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range execs {
		r.executors[e.Spec().Name] = e
	}
}

// Catalog returns the full model-facing tool catalog in Cube-first order.
func (r *Registry) Catalog(ctx context.Context) []ports.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []ports.ToolSpec
	// Tier 1: Cube governed metrics first.
	if r.metrics != nil {
		if ms, err := r.metrics.Metrics(ctx); err == nil {
			for _, m := range ms {
				out = append(out, ports.ToolSpec{
					Name:        m.Name,
					Route:       domain.RouteCube,
					Description: m.Title,
					Params:      m.Dimensions,
				})
			}
		}
	}
	// Tier 2: in-process read-service tools.
	var apiSpecs []ports.ToolSpec
	for _, e := range r.executors {
		apiSpecs = append(apiSpecs, e.Spec())
	}
	sort.Slice(apiSpecs, func(i, j int) bool { return apiSpecs[i].Name < apiSpecs[j].Name })
	out = append(out, apiSpecs...)
	// Tier 3: toolbox tools.
	if r.toolbox != nil {
		if ts, err := r.toolbox.Tools(ctx); err == nil {
			out = append(out, ts...)
		}
	}
	// Tier 4: SQL fallback is not advertised as a named tool; it is the last
	// resort chosen only when no metric/api/toolbox tool matched.
	return out
}

// Execute runs a routed sub-question through the correct tier. Tenant scope is
// injected from actor by each tier; params never carry tenant.
func (r *Registry) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch sub.Route {
	case domain.RouteCube:
		if r.metrics == nil {
			return domain.ToolResult{}, fmt.Errorf("cube metric service not wired for %q", sub.ToolName)
		}
		dims, tr, filters := cubeParams(sub.Params)
		return r.metrics.Query(ctx, actor, ports.MetricQuery{
			Metric: sub.ToolName, Dimensions: dims, TimeRange: tr, Filters: filters,
		})
	case domain.RouteAPI:
		e, ok := r.executors[sub.ToolName]
		if !ok {
			return domain.ToolResult{}, fmt.Errorf("no read-service executor %q", sub.ToolName)
		}
		return e.Execute(ctx, actor, sub)
	case domain.RouteToolbox:
		if r.toolbox == nil {
			return domain.ToolResult{}, fmt.Errorf("toolbox not wired for %q", sub.ToolName)
		}
		return r.toolbox.Call(ctx, actor, sub.ToolName, sub.Params)
	case domain.RouteSQL:
		if r.sqlFB == nil {
			return domain.ToolResult{}, fmt.Errorf("sql fallback not wired")
		}
		sql, _ := sub.Params["sql"].(string)
		args, _ := sub.Params["args"].([]any)
		return r.sqlFB.Execute(ctx, actor, sql, args)
	default:
		return domain.ToolResult{}, fmt.Errorf("unroutable sub-question %q (route=%q)", sub.ID, sub.Route)
	}
}

// cubeParams normalizes a plan's flat params into a Cube metric request. Both
// the Vertex and keyword planners emit params as a flat map with string values
// (e.g. {"park_label":"Coimbatore","species":"goat","group_by":"species"}); this
// derives group-by dimensions, the time range, and the non-tenant equality
// filters the MetricService applies. Reserved keys never become filters.
func cubeParams(params map[string]any) (dims []string, timeRange string, filters map[string]string) {
	filters = map[string]string{}
	if params == nil {
		return nil, "", filters
	}
	// Explicit group-by dimensions: "dimensions" ([]string or CSV) or "group_by".
	switch d := params["dimensions"].(type) {
	case []string:
		dims = append(dims, d...)
	case string:
		for _, p := range splitCSV(d) {
			dims = append(dims, p)
		}
	}
	if gb, ok := params["group_by"].(string); ok && gb != "" {
		dims = append(dims, gb)
	}
	if tr, ok := params["time_range"].(string); ok {
		timeRange = tr
	}
	for k, v := range params {
		switch k {
		case "dimensions", "group_by", "time_range", "sql", "args", "tenant_id":
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			filters[k] = s
		}
	}
	return dims, timeRange, filters
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// HasExecutor reports whether an in-process read-service tool exists by name.
func (r *Registry) HasExecutor(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.executors[name]
	return ok
}
