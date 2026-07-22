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
}

// Ordered rules: governed KPIs (Cube) are matched with priority over the
// operational read-API tools. Order matters — first match wins.
var rules = []rule{
	{regexp.MustCompile(`(?i)overdue|behind|late`), "vaccination_overdue_sheds", "vaccination_overdue", domain.RouteCube},
	{regexp.MustCompile(`(?i)adherence|complian`), "vaccination_adherence", "vaccination_compliance", domain.RouteCube},
	{regexp.MustCompile(`(?i)mortality|death`), "counts_movement_daily", "mortality_rate", domain.RouteCube},
	{regexp.MustCompile(`(?i)vaccinat|shot|dose|due`), "vaccination_due_today", "vaccination_due", domain.RouteCube},
	{regexp.MustCompile(`(?i)how many (goat|sheep|animal)|headcount|census|herd size|total animal`), "total_animal_census", "active_animals", domain.RouteCube},
	{regexp.MustCompile(`(?i)feed|ration|packing`), "feed_direction_today", "feed_direction_preview", domain.RouteAPI},
	{regexp.MustCompile(`(?i)procure|intake|source.?entry|load`), "procurement_open_loads", "procurement_source_entry_loads", domain.RouteAPI},
	{regexp.MustCompile(`(?i)coverage|backup|staff|roster|who owns`), "workforce_coverage", "admin_roster_coverage", domain.RouteAPI},
	{regexp.MustCompile(`(?i)verif|proof`), "verification_queue", "verification_queue", domain.RouteAPI},
	{regexp.MustCompile(`(?i)action center|needs action|queue`), "action_center_queue", "action_center_obligations", domain.RouteAPI},
	{regexp.MustCompile(`(?i)exception|risk|broken|integrity|dlq|kernel health`), "ops_exceptions_risk", "operations_kernel_health", domain.RouteAPI},
	{regexp.MustCompile(`(?i)audit|what happened|activity`), "audit_activity", "operations_audit_summary", domain.RouteAPI},
	{regexp.MustCompile(`(?i)capacity|over.?capacity|overstock`), "shed_capacity_variance", "admin_location_usage", domain.RouteAPI},
	{regexp.MustCompile(`(?i)count|breakdown|by (park|shed|breed|sex|stage)|how many`), "count_by_scope_dimension", "counts_breakdown", domain.RouteAPI},
}

// writeIntent matches mutation/scope requests that must be refused.
var writeIntent = regexp.MustCompile(`(?i)\b(mark done|approve|reschedule|verify|cancel|update|delete|reassign|replay|run import|create|set capacity)\b`)

// Plan classifies the question into one or more sub-questions.
func (p *Planner) Plan(_ context.Context, q domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	text := q.Text
	if writeIntent.MatchString(text) {
		return domain.Plan{Refusal: "I'm read-only and can't change records. That action lives in its owning workflow — I can report status, but not perform writes."}, nil
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
				Params:      extractParams(clause),
			})
		}
	}
	if len(subs) == 0 {
		// Try the whole text once before refusing.
		if r, ok := classify(text); ok {
			subs = append(subs, domain.SubQuestion{
				ID: "0", Text: text, IntentClass: r.intent, Route: r.route,
				ToolName: r.tool, Params: extractParams(text),
			})
		}
	}
	if len(subs) == 0 {
		return domain.Plan{Refusal: "I can answer questions about your Mesha operations — counts, vaccination, feed, procurement, workforce, and exceptions. Could you rephrase toward one of those?"}, nil
	}
	return domain.Plan{SubQuestions: subs}, nil
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
