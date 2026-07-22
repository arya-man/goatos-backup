// Package keywordplanner is the deterministic, model-free planner used when
// Vertex is unavailable (mode=fallback) and as the always-safe baseline in
// tests. It implements ports.AIProvider. It classifies the question into one of
// the GENAI query classes and emits a Cube-first-routed Plan. It NEVER invents
// SQL: unknown/out-of-scope intents refuse rather than fabricate.
package keywordplanner

import (
	"context"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Planner is the deterministic keyword planner.
type Planner struct{}

// New builds a Planner.
func New() *Planner { return &Planner{} }

// PlannedByModel is false: this is the fallback, not a model.
func (*Planner) PlannedByModel() bool { return false }

type rule struct {
	re     *regexp.Regexp
	intent string
	tool   string
	route  domain.Route
	// groupBy, when set, is attached to the sub-question params as the Cube
	// group-by dimension. Operator-grain drive questions MUST group by
	// operator_label, or the operator metric returns one ungrounded tenant-wide
	// total the answer can never attribute to a named operator.
	groupBy string
}

// Ordered rules: governed KPIs (Cube) are matched with priority over the
// operational read-API tools. Order matters — first match wins.
var rules = []rule{
	// Operator-based vaccination drive questions (operator grain). These MUST be
	// matched before the shed-grain overdue/capacity rules below, or "which
	// operators are behind" would collapse to the shed-grain vaccination_overdue.
	// Source metric: kpi_vaccination_operator over ceo_ai.vaccination_operator_status.
	{regexp.MustCompile(`(?i)(operator|vaccinator).{0,40}(overload|over.?capacit|overstretch|too many|stretched)|(overload|over.?capacit|overstretch|stretched).{0,40}(operator|vaccinator)|who.{0,20}(is )?(overload|over.?capacit|at capacity|stretched|maxed)`), "operator_vaccination_overloaded", "operator_vaccination_utilization", domain.RouteCube, "operator_label"},
	{regexp.MustCompile(`(?i)(operator|vaccinator).{0,40}(behind|overdue|late|lagging)|(behind|overdue|late|lagging).{0,40}(operator|vaccinator)`), "operator_vaccination_behind", "operator_vaccination_overdue", domain.RouteCube, "operator_label"},
	{regexp.MustCompile(`(?i)(operator|vaccinator).{0,40}(capacity|utilization|utili[sz]ation)|(capacity|utilization|utili[sz]ation).{0,40}(operator|vaccinator)`), "operator_vaccination_capacity", "operator_vaccination_capacity", domain.RouteCube, "operator_label"},
	{regexp.MustCompile(`(?i)operator|vaccinator|drive assignment|assigned animals|animals assigned`), "operator_vaccination_load", "operator_vaccination_load", domain.RouteCube, "operator_label"},
	{regexp.MustCompile(`(?i)overdue|behind|late`), "vaccination_overdue_sheds", "vaccination_overdue", domain.RouteCube, ""},
	{regexp.MustCompile(`(?i)adherence|complian`), "vaccination_adherence", "vaccination_compliance", domain.RouteCube, ""},
	{regexp.MustCompile(`(?i)mortality|death`), "counts_movement_daily", "mortality_rate", domain.RouteCube, ""},
	{regexp.MustCompile(`(?i)vaccinat|shot|dose|due`), "vaccination_due_today", "vaccination_due", domain.RouteCube, ""},
	{regexp.MustCompile(`(?i)how many (goat|sheep|animal)|headcount|census|herd size|total animal`), "total_animal_census", "active_animals", domain.RouteCube, ""},
	{regexp.MustCompile(`(?i)feed|ration|packing`), "feed_direction_today", "feed_direction_preview", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)procure|intake|source.?entry|load`), "procurement_open_loads", "procurement_source_entry_loads", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)coverage|backup|staff|roster|who owns`), "workforce_coverage", "admin_roster_coverage", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)verif|proof`), "verification_queue", "verification_queue", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)action center|needs action|queue`), "action_center_queue", "action_center_obligations", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)exception|risk|broken|integrity|dlq|kernel health`), "ops_exceptions_risk", "operations_kernel_health", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)audit|what happened|activity`), "audit_activity", "operations_audit_summary", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)capacity|over.?capacity|overstock`), "shed_capacity_variance", "admin_location_usage", domain.RouteAPI, ""},
	{regexp.MustCompile(`(?i)count|breakdown|by (park|shed|breed|sex|stage)|how many`), "count_by_scope_dimension", "counts_breakdown", domain.RouteAPI, ""},
}

// writeIntent matches mutation/scope requests that must be refused.
var writeIntent = regexp.MustCompile(`(?i)\b(mark done|approve|reschedule|verify|cancel|update|delete|reassign|replay|run import|create|set capacity)\b`)

// Plan classifies the question into one or more sub-questions.
func (p *Planner) Plan(_ context.Context, q domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	text := q.Text
	if writeIntent.MatchString(text) {
		return domain.Plan{Refusal: "I'm read-only and can't change records. That action lives in its owning workflow — I can report status, but not perform writes."}, nil
	}

	// Diagnostic decomposition: "why are we behind on vaccination" is not a single
	// number — it is a request to break the overdue headline into its top
	// contributors. Decompose into overdue grouped by park AND operator overdue
	// grouped by operator, so the composer can synthesise "168 overdue, led by
	// Coimbatore 108 / Channapatna 60; operator X most behind".
	if subs := diagnoseBehind(text); subs != nil {
		return domain.Plan{SubQuestions: subs}, nil
	}

	// Multi-intent: split on "and" and classify each clause; dedupe tools.
	clauses := splitClauses(text)
	seen := map[string]bool{}
	var subs []domain.SubQuestion
	for i, clause := range clauses {
		if r, ok := classify(clause); ok && !seen[r.tool] {
			seen[r.tool] = true
			subs = append(subs, domain.SubQuestion{
				ID:          intToID(i),
				Text:        clause,
				IntentClass: r.intent,
				Route:       r.route,
				ToolName:    r.tool,
				Params:      withGroupBy(extractParams(clause), r.groupBy),
			})
		}
	}
	if len(subs) == 0 {
		// Try the whole text once before refusing.
		if r, ok := classify(text); ok {
			subs = append(subs, domain.SubQuestion{
				ID: "0", Text: text, IntentClass: r.intent, Route: r.route,
				ToolName: r.tool, Params: withGroupBy(extractParams(text), r.groupBy),
			})
		}
	}
	if len(subs) == 0 {
		return domain.Plan{Refusal: "I can answer questions about your Mesha operations — counts, vaccination, feed, procurement, workforce, and exceptions. Could you rephrase toward one of those?"}, nil
	}
	return domain.Plan{SubQuestions: subs}, nil
}

// withGroupBy attaches a Cube group-by dimension to the sub-question params when
// the matched rule declares one. Operator drive questions carry
// group_by=operator_label so the metric returns one row per operator (named,
// groundable) instead of a single tenant-wide total.
func withGroupBy(params map[string]any, groupBy string) map[string]any {
	if groupBy == "" {
		return params
	}
	if params == nil {
		params = map[string]any{}
	}
	params["group_by"] = groupBy
	return params
}

// behindDiagnostic matches "why are we behind / what's driving the overdue"
// style questions that call for a contributor breakdown, not a single figure.
var behindDiagnostic = regexp.MustCompile(`(?i)\b(why|what.?s? (driving|causing)|reason|root cause)\b.{0,40}(behind|overdue|late|lag)`)

// diagnoseBehind returns the decomposed grouped sub-questions for a "why are we
// behind on vaccination" diagnostic, or nil when the question is not that shape.
// Contributors: shed-grain overdue grouped by park, plus operator-grain overdue
// grouped by operator — every figure grounded, worst-first (Cube orders desc).
func diagnoseBehind(text string) []domain.SubQuestion {
	if !behindDiagnostic.MatchString(text) {
		return nil
	}
	low := strings.ToLower(text)
	if !strings.Contains(low, "vaccinat") && !strings.Contains(low, "overdue") &&
		!strings.Contains(low, "behind") && !strings.Contains(low, "late") {
		return nil
	}
	return []domain.SubQuestion{
		{
			ID: "0", Text: "vaccination overdue by park", IntentClass: "vaccination_overdue_by_park",
			Route: domain.RouteCube, ToolName: "vaccination_overdue",
			Params: map[string]any{"group_by": "park_label"},
		},
		{
			ID: "1", Text: "operator vaccination overdue by operator", IntentClass: "operator_vaccination_behind",
			Route: domain.RouteCube, ToolName: "operator_vaccination_overdue",
			Params: map[string]any{"group_by": "operator_label"},
		},
	}
}

func classify(text string) (rule, bool) {
	for _, r := range rules {
		if r.re.MatchString(text) {
			return r, true
		}
	}
	return rule{}, false
}

func splitClauses(text string) []string {
	parts := regexp.MustCompile(`(?i)\s+and\s+|;`).Split(text, -1)
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

var knownParks = []string{"Castro 1", "Castro 2", "Channapatna", "Gandhi 1", "Gandhi 2"}

func extractParams(text string) map[string]any {
	params := map[string]any{}
	low := strings.ToLower(text)
	for _, park := range knownParks {
		if strings.Contains(low, strings.ToLower(park)) {
			params["park_label"] = park
			break
		}
	}
	if strings.Contains(low, "goat") && !strings.Contains(low, "sheep") {
		params["species"] = "goat"
	} else if strings.Contains(low, "sheep") {
		params["species"] = "sheep"
	}
	return params
}

func intToID(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}
