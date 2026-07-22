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
	return strings.Join(sections, "\n\n"), citations, groundValues
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
