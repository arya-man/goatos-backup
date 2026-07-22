package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// composer synthesizes ONE answer from many tool results, aggregate-first, and
// attaches source + freshness (IST) + citations. It never fabricates numbers:
// every emitted figure is drawn verbatim from a ToolResult Fact.
type composer struct{}

// compose builds the user-facing answer body and citations from tool results.
func (composer) compose(results []domain.ToolResult) (body string, citations []domain.Citation, groundValues []string) {
	var sections []string
	seenSurface := map[string]bool{}

	for _, r := range results {
		if r.Err != nil {
			sections = append(sections, fmt.Sprintf("%s: could not be retrieved (%s).", surfaceOrRoute(r), r.Err.Error()))
			continue
		}
		if len(r.Facts) == 0 && strings.TrimSpace(r.Summary) == "" {
			sections = append(sections, fmt.Sprintf("%s: no records found for the requested scope.", surfaceOrRoute(r)))
		} else {
			sections = append(sections, renderFacts(r))
			for _, f := range r.Facts {
				groundValues = append(groundValues, f.Value)
			}
		}
		if !seenSurface[r.Surface] && r.Surface != "" {
			seenSurface[r.Surface] = true
			citations = append(citations, domain.Citation{
				Surface: r.Surface, Route: r.Route, AsOf: r.AsOf, MetricStatus: r.MetricStatus,
			})
		}
	}
	body = strings.Join(sections, "\n\n")
	// Narrative synthesis: a short grounded lead sentence so a leadership answer
	// reads as prose ("In short — goat 972, sheep 336.") instead of a raw metric
	// dump. Every number and scope word in the lead is copied verbatim from a
	// Fact, so it stays inside the grounding contract the reviewer enforces (no
	// computed totals, no invented figures).
	lead := synthesizeOverloadLead(results)
	if lead == "" {
		lead = synthesizeLead(results)
	}
	if lead != "" {
		body = lead + "\n\n" + body
	}
	return body, citations, groundValues
}

// operatorUtilizationLabel is the business title of the operator utilization
// metric (see cubeMetricBindings). Its fact values are rendered as a percent of
// capacity ("155%"), so a value >= 100% means the operator is OVER capacity.
const operatorUtilizationLabel = "Operator utilization"

// synthesizeOverloadLead builds the "who is over capacity" lead from the
// operator-utilization facts. The generic numeric lead only lists ratios; a
// leader asking "who is overloaded / at capacity" needs the answer to say
// explicitly WHICH operators are OVER capacity and by how much. It names each
// operator whose utilization is >= 100% with its percent (grounded verbatim from
// the fact value, e.g. "155%"), worst-first, and returns "" when there are no
// utilization facts so the normal lead handles every other question shape.
func synthesizeOverloadLead(results []domain.ToolResult) string {
	type util struct {
		name    string
		pct     int
		pctText string
	}
	var over []util
	haveUtil := false
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		for _, f := range r.Facts {
			if strings.TrimSpace(f.Label) != operatorUtilizationLabel {
				continue
			}
			haveUtil = true
			pctText := strings.TrimSpace(f.Value)
			n, ok := parseNumber(pctText)
			if !ok {
				continue
			}
			if int(n) >= 100 {
				name := strings.TrimSpace(f.Scope)
				if name == "" {
					name = strings.TrimSpace(f.Label)
				}
				over = append(over, util{name: name, pct: int(n), pctText: pctText})
			}
		}
	}
	if !haveUtil {
		return ""
	}
	if len(over) == 0 {
		return "In short — every operator is within capacity (no operator is over 100% utilization)."
	}
	sort.Slice(over, func(i, j int) bool { return over[i].pct > over[j].pct })
	const maxNamed = 6
	if len(over) > maxNamed {
		over = over[:maxNamed]
	}
	var parts []string
	for _, u := range over {
		parts = append(parts, fmt.Sprintf("%s at %s of capacity", u.name, u.pctText))
	}
	noun := "operator is over capacity"
	if len(parts) > 1 {
		noun = "operators are over capacity"
	}
	return "In short — " + strconv.Itoa(len(parts)) + " " + noun + ": " + strings.Join(parts, ", ") + "."
}

// synthesizeLead builds one grounded lead sentence from the numeric facts. It
// restates existing values (optionally with their scope) as prose and never
// introduces a number that is not already a Fact. When several results share the
// same metric label (the "goats vs sheep" shape, which the planner often emits
// as two species-FILTERED sub-queries), their scoped figures are merged into a
// single breakdown so the lead names every group, not just the first.
func synthesizeLead(results []domain.ToolResult) string {
	label := ""
	var parts []string
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		l, p := numericLeadParts(r)
		if len(p) == 0 {
			continue
		}
		if label == "" || label == "the figure is" {
			label = l
		} else if l != label && l != "the figure is" {
			// Different metrics in one answer: keep the lead to the first metric
			// rather than mixing unlike figures into one misleading sentence.
			break
		}
		parts = append(parts, p...)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return "In short — " + label + " " + parts[0] + "."
	default:
		const maxParts = 6
		if len(parts) > maxParts {
			parts = parts[:maxParts]
		}
		return "In short — " + label + " breaks down as " + strings.Join(parts, ", ") + "."
	}
}

// numericLeadParts extracts the label and the scoped "scope value" (or bare
// value) phrases for the numeric facts of a result, in order.
func numericLeadParts(r domain.ToolResult) (label string, parts []string) {
	for _, f := range r.Facts {
		if _, ok := parseNumber(f.Value); !ok {
			continue
		}
		fl := strings.TrimSpace(f.Label)
		if label == "" {
			label = fl
		} else if fl != label {
			// A differently-labelled numeric fact is a different metric; don't
			// fold it under this label or the lead would misattribute the figure.
			continue
		}
		if s := strings.TrimSpace(f.Scope); s != "" {
			parts = append(parts, s+" "+strings.TrimSpace(f.Value))
		} else {
			parts = append(parts, strings.TrimSpace(f.Value))
		}
	}
	if label == "" {
		label = "the figure is"
	}
	return label, parts
}

func renderFacts(r domain.ToolResult) string {
	var b strings.Builder
	if strings.TrimSpace(r.Summary) != "" {
		b.WriteString(r.Summary)
	}
	const maxRows = 10
	facts := r.Facts
	if len(facts) > maxRows {
		facts = facts[:maxRows]
	}
	if len(facts) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		var lines []string
		for _, f := range facts {
			if f.Scope != "" {
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", f.Label, f.Scope, f.Value))
			} else {
				lines = append(lines, fmt.Sprintf("- %s: %s", f.Label, f.Value))
			}
		}
		b.WriteString(strings.Join(lines, "\n"))
	}
	if r.MetricStatus == domain.MetricDraft {
		b.WriteString("\n(draft metric — pending business sign-off)")
	}
	if len(r.Facts) > maxRows {
		b.WriteString(fmt.Sprintf("\n… %d more not shown (aggregate view).", len(r.Facts)-maxRows))
	}
	return b.String()
}

// chartKeywords trigger a chart when the user explicitly asks to visualize.
var chartKeywords = []string{
	"plot", "graph", "chart", "trend", "compare", "comparison",
	"over time", "over-time", "by park", "by-park", "per park",
	"breakdown", "break down", "distribution", "by shed", "per shed",
}

// lineKeywords bias the chart toward a line (time/trend shape) over bars.
var lineKeywords = []string{"trend", "over time", "over-time", "timeline", "growth", "trajectory"}

const maxChartPoints = 12

// buildChart returns an optional, additive chart for an answer. It emits a
// chart ONLY when (a) the question asks to visualize OR the grounding result is
// a dimensioned metric series, AND (b) at least two real numeric data points
// exist. Every point is parsed verbatim from a Fact value — nothing is
// fabricated. A plain single-value count question yields no chart.
func buildChart(questionText string, results []domain.ToolResult) *domain.Chart {
	series, labels := dimensionedSeries(results)
	if series == nil || len(labels) < 2 {
		return nil
	}

	if !plotRequested(questionText) && !isDimensioned(labels) {
		return nil
	}

	chartType := "bar"
	if matchesAny(questionText, lineKeywords) {
		chartType = "line"
	}

	title := series.name
	if title == "" {
		title = "Result"
	}

	return &domain.Chart{
		Type:  chartType,
		Title: title,
		X:     labels,
		Series: []domain.ChartSeries{
			{Name: series.name, Data: series.data},
		},
	}
}

type numericSeries struct {
	name   string
	data   []float64
	scoped bool // true when x labels came from per-row Scope (a real dimension)
}

// dimensionedSeries picks the tool result with the most numeric data points and
// turns it into a single labelled series. It prefers per-row Scope as the x
// axis (the Cube "by park/shed" shape: same Label, distinct Scope); otherwise
// it falls back to per-fact Label as x (the SQL/toolbox row shape). Non-numeric
// facts are skipped so a mixed result still charts its numeric part.
func dimensionedSeries(results []domain.ToolResult) (*numericSeries, []string) {
	var best *numericSeries
	var bestLabels []string

	for i := range results {
		r := results[i]
		if r.Err != nil {
			continue
		}
		s, labels := seriesFromFacts(r)
		if s == nil {
			continue
		}
		if best == nil || len(s.data) > len(best.data) {
			best = s
			bestLabels = labels
		}
	}
	if best != nil && len(best.data) > maxChartPoints {
		best.data = best.data[:maxChartPoints]
		bestLabels = bestLabels[:maxChartPoints]
	}
	return best, bestLabels
}

func seriesFromFacts(r domain.ToolResult) (*numericSeries, []string) {
	var scopeVals []float64
	var scopeLabels []string
	var labelVals []float64
	var labelLabels []string
	commonLabel := ""
	scopeUsable := true

	for _, f := range r.Facts {
		v, ok := parseNumber(f.Value)
		if !ok {
			continue
		}
		if commonLabel == "" {
			commonLabel = f.Label
		}
		if s := strings.TrimSpace(f.Scope); s != "" {
			scopeVals = append(scopeVals, v)
			scopeLabels = append(scopeLabels, s)
		} else {
			scopeUsable = false
		}
		labelVals = append(labelVals, v)
		labelLabels = append(labelLabels, strings.TrimSpace(f.Label))
	}

	// Prefer the scoped shape (a genuine dimension across rows) when every
	// numeric fact carried a Scope.
	if scopeUsable && len(scopeVals) >= 2 {
		return &numericSeries{name: seriesName(r, commonLabel), data: scopeVals, scoped: true}, scopeLabels
	}
	if len(labelVals) >= 2 && distinctLabels(labelLabels) {
		return &numericSeries{name: seriesName(r, ""), data: labelVals, scoped: false}, labelLabels
	}
	return nil, nil
}

func seriesName(r domain.ToolResult, commonLabel string) string {
	if commonLabel != "" {
		return commonLabel
	}
	if r.Surface != "" {
		return r.Surface
	}
	return string(r.Route)
}

func parseNumber(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	// Tolerate grouped thousands and a trailing percent sign.
	t = strings.ReplaceAll(t, ",", "")
	t = strings.TrimSuffix(t, "%")
	v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func distinctLabels(labels []string) bool {
	seen := map[string]bool{}
	for _, l := range labels {
		if l == "" || seen[l] {
			return false
		}
		seen[l] = true
	}
	return len(labels) >= 2
}

// isDimensioned reports whether the x axis is a real breakdown (distinct
// labels), which alone justifies a chart even without an explicit plot ask.
func isDimensioned(labels []string) bool {
	return distinctLabels(labels)
}

func plotRequested(text string) bool {
	return matchesAny(text, chartKeywords)
}

func matchesAny(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func surfaceOrRoute(r domain.ToolResult) string {
	if r.Surface != "" {
		return r.Surface
	}
	return string(r.Route)
}

// sourceLabel produces the flat Answer.Source string naming the routes used.
func sourceLabel(results []domain.ToolResult) string {
	seen := map[string]bool{}
	var parts []string
	for _, r := range results {
		s := surfaceOrRoute(r)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		parts = append(parts, s)
	}
	sort.Strings(parts)
	return strings.Join(parts, " · ")
}
