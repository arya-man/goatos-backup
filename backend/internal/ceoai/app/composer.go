package app

import (
	"errors"
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

// ErrForeignTenantFacts is returned by validateFactTenants (and everything that
// calls it: composeFor, buildChart, the cache gate) when a fact set carries a
// TenantID other than the acting tenant, more than one TenantID, or no TenantID
// at all. The orchestrator turns it into a refusal: a foreign or unstamped row
// must never be composed, charted, or cached (plan v3 D0 "Chart / facts").
var ErrForeignTenantFacts = errors.New("ceoai: fact set is not scoped to the acting tenant")

// validateFactTenants is the typed tenant gate over grounding facts. Every Fact
// must carry TenantID == actor.TenantID (set by the executor/reader from the
// session, never from a result row). Errored results are skipped — they carry
// no facts the composer will render. An empty fact set is fine.
func validateFactTenants(actor domain.Actor, results []domain.ToolResult) error {
	want := strings.TrimSpace(actor.TenantID)
	if want == "" {
		return fmt.Errorf("%w: actor has no tenant", ErrForeignTenantFacts)
	}
	seen := map[string]bool{}
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		for _, f := range r.Facts {
			got := strings.TrimSpace(f.TenantID)
			seen[got] = true
			if got == "" {
				return fmt.Errorf("%w: fact %q from %s has no TenantID (executor/reader must stamp it from the actor)", ErrForeignTenantFacts, f.Label, surfaceOrRoute(r))
			}
			if got != want {
				return fmt.Errorf("%w: fact %q from %s belongs to another tenant", ErrForeignTenantFacts, f.Label, surfaceOrRoute(r))
			}
		}
	}
	if len(seen) > 1 {
		return fmt.Errorf("%w: %d distinct tenant ids in one fact set", ErrForeignTenantFacts, len(seen))
	}
	return nil
}

// composeFor is the tenant-gated entry point the orchestrator uses: it rejects
// a fact set that is not wholly the actor's before any figure is rendered.
func (c composer) composeFor(actor domain.Actor, results []domain.ToolResult) (body string, citations []domain.Citation, groundValues []string, err error) {
	if err := validateFactTenants(actor, results); err != nil {
		return "", nil, nil, err
	}
	body, citations, groundValues = c.compose(results)
	return body, citations, groundValues, nil
}

// compose builds the user-facing answer body and citations from tool results.
// Callers on the live path go through composeFor so the tenant gate runs first.
func (composer) compose(results []domain.ToolResult) (body string, citations []domain.Citation, groundValues []string) {
	var sections []string
	seenSurface := map[string]bool{}

	for _, r := range results {
		if r.Err != nil {
			// Never surface the raw internal error (route/tool wiring, SQL, etc.)
			// to the leadership user — that stays in the admin trace + audit. Show
			// a clean, honest "not available yet" line instead.
			sections = append(sections, fmt.Sprintf("%s isn't available to the assistant yet.", surfaceOrRoute(r)))
			continue
		}
		if len(r.Facts) == 0 && strings.TrimSpace(r.Summary) == "" {
			sections = append(sections, fmt.Sprintf("%s: no records found for the requested scope.", surfaceOrRoute(r)))
		} else {
			sections = append(sections, renderAnswerBlock(r))
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

func renderAnswerBlock(r domain.ToolResult) string {
	block := ""
	if block := renderSalesAnswer(r); block != "" {
		return appendMetricStatus(block, r)
	}
	if block := renderOperationalAnswer(r); block != "" {
		return appendMetricStatus(block, r)
	}
	if block := renderSingleMetricBreakdown(r); block != "" {
		return appendMetricStatus(block, r)
	}
	block = renderFacts(r)
	return block
}

func appendMetricStatus(block string, r domain.ToolResult) string {
	if r.MetricStatus == domain.MetricDraft {
		return block + "\n(draft metric — pending business sign-off)"
	}
	return block
}

func renderSalesAnswer(r domain.ToolResult) string {
	var soldTotal, soldGoats, soldSheep, revenue, month string
	thisMonth := false
	for _, f := range r.Facts {
		label := strings.ToLower(strings.TrimSpace(f.Label))
		value := strings.TrimSpace(f.Value)
		if value == "" {
			continue
		}
		if strings.Contains(label, "this month") {
			thisMonth = true
		}
		if strings.Contains(label, "sold animals") {
			soldTotal = value
			month = monthFromLabel(f.Label)
		} else if strings.Contains(label, "sold goats") {
			soldGoats = value
			if month == "" {
				month = monthFromLabel(f.Label)
			}
		} else if strings.Contains(label, "sold sheep") {
			soldSheep = value
			if month == "" {
				month = monthFromLabel(f.Label)
			}
		} else if strings.Contains(label, "sales revenue") {
			revenue = value
			if month == "" {
				month = monthFromLabel(f.Label)
			}
		}
	}
	if soldTotal == "" && soldGoats == "" && soldSheep == "" && revenue == "" {
		return ""
	}

	var b strings.Builder
	if month != "" {
		b.WriteString("For " + month + ", ")
	} else if thisMonth {
		b.WriteString("This month, ")
	} else {
		b.WriteString("For the selected period, ")
	}
	switch {
	case soldTotal != "" && soldGoats != "" && soldSheep != "" && revenue != "":
		b.WriteString(fmt.Sprintf("%s animals were sold: %s goats and %s sheep. Sales revenue was %s.", soldTotal, soldGoats, soldSheep, revenue))
	case soldTotal != "" && soldGoats != "" && soldSheep != "":
		b.WriteString(fmt.Sprintf("%s animals were sold: %s goats and %s sheep.", soldTotal, soldGoats, soldSheep))
	case soldTotal != "" && revenue != "":
		b.WriteString(fmt.Sprintf("%s animals were sold. Sales revenue was %s.", soldTotal, revenue))
	case soldTotal != "":
		b.WriteString(fmt.Sprintf("%s animals were sold.", soldTotal))
	case revenue != "":
		b.WriteString(fmt.Sprintf("sales revenue was %s.", revenue))
	default:
		b.WriteString(strings.TrimSuffix(renderFacts(r), ".") + ".")
	}
	return b.String()
}

func renderOperationalAnswer(r domain.ToolResult) string {
	if len(r.Facts) == 0 {
		return ""
	}
	if r.ToolName == "feed_weight_band_summary" {
		return renderFeedWeightBandSummary(r.Facts)
	}
	label := strings.TrimSpace(r.Facts[0].Label)
	for _, f := range r.Facts {
		if strings.TrimSpace(f.Label) != label {
			return ""
		}
		if _, ok := parseNumber(f.Value); !ok {
			return ""
		}
	}
	switch label {
	case "Farm-born animals":
		return renderScopedCountSentence(r.Facts, "Farm-born active animals")
	case "Lump-sum animals weighed":
		return renderScopedCountSentence(r.Facts, "Lump-sum animals weighed")
	case "Per-animal animals weighed":
		return renderScopedCountSentence(r.Facts, "Per-animal animals weighed")
	case "Animals weighed":
		return renderScopedCountSentence(r.Facts, "Animals weighed")
	case "Matched animals":
		return renderScopedCountSentence(r.Facts, "Matched animals")
	case "Kid deaths":
		return renderScopedCountSentence(r.Facts, "Kid deaths")
	case "Adult deaths":
		return renderScopedCountSentence(r.Facts, "Adult deaths")
	case "Deaths":
		return renderScopedCountSentence(r.Facts, "Deaths")
	case "Cause established deaths":
		return renderScopedCountSentence(r.Facts, "Deaths with an established cause")
	case "Died within 7 days of birth":
		return renderScopedCountSentence(r.Facts, "Deaths within 7 days of birth")
	case "Mortality rate pct":
		return renderScopedCountSentence(r.Facts, "Mortality rate")
	default:
		return ""
	}
}

func renderFeedWeightBandSummary(facts []domain.Fact) string {
	values := map[string]domain.Fact{}
	for _, f := range facts {
		values[strings.TrimSpace(f.Label)] = f
	}
	pick := func(label string) string {
		return strings.TrimSpace(values[label].Value)
	}
	if pick("Matched animals") == "" {
		return ""
	}
	var parts []string
	parts = append(parts, "Feed by weight band has "+pick("Matched animals")+" matched animals on farm")
	if v := pick("Matched animals including exited"); v != "" {
		parts = append(parts, v+" when exited animals are included")
	}
	if v := pick("Animals weighed in period"); v != "" {
		parts = append(parts, v+" animals weighed in the General tab for the same period")
	}
	if v := pick("Not shown feed rows"); v != "" {
		parts = append(parts, v+" feed rows not shown because they have no qualifying weighing")
	}
	if v := pick("Exited in period"); v != "" {
		parts = append(parts, v+" exited in the period")
	}
	if v := pick("Feed sheet"); v != "" {
		parts = append(parts, "feed sheet "+v)
	}
	return strings.Join(parts, "; ") + "."
}

func renderScopedCountSentence(facts []domain.Fact, title string) string {
	var parts []string
	for _, f := range facts {
		value := strings.TrimSpace(f.Value)
		scope := strings.TrimSpace(f.Scope)
		if scope == "" {
			parts = append(parts, value)
		} else {
			parts = append(parts, scope+" "+value)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return title + ": " + parts[0] + "."
	}
	return title + ": " + strings.Join(parts, ", ") + "."
}

func monthFromLabel(label string) string {
	fields := strings.Fields(label)
	for _, field := range fields {
		field = strings.Trim(field, ".,;:()[]{}")
		if len(field) == len("2006-01") && field[4] == '-' {
			if _, err := strconv.Atoi(field[:4]); err == nil {
				if _, err := strconv.Atoi(field[5:]); err == nil {
					return field
				}
			}
		}
	}
	return ""
}

func renderSingleMetricBreakdown(r domain.ToolResult) string {
	if len(r.Facts) == 0 {
		return ""
	}
	label := ""
	var parts []string
	for _, f := range r.Facts {
		if _, ok := parseNumber(f.Value); !ok {
			return ""
		}
		fl := strings.TrimSpace(f.Label)
		if fl == "" {
			return ""
		}
		if label == "" {
			label = fl
		} else if fl != label {
			return ""
		}
		value := strings.TrimSpace(f.Value)
		if scope := strings.TrimSpace(f.Scope); scope != "" {
			parts = append(parts, scope+" "+value)
		} else {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 || len(parts) > 8 {
		return ""
	}
	if len(parts) == 1 {
		return label + ": " + parts[0] + "."
	}
	return label + ": " + strings.Join(parts, ", ") + "."
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
				lines = append(lines, fmt.Sprintf("%s in %s: %s.", f.Label, f.Scope, sentenceValue(f.Value)))
			} else {
				lines = append(lines, fmt.Sprintf("%s: %s.", f.Label, sentenceValue(f.Value)))
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

func sentenceValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	return strings.TrimRight(v, ".")
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
// fabricated. A plain single-value count question yields no chart. It is
// tenant-gated like the composer: a fact set that is not wholly the actor's
// returns ErrForeignTenantFacts and no chart.
func buildChart(actor domain.Actor, questionText string, results []domain.ToolResult) (*domain.Chart, error) {
	if err := validateFactTenants(actor, results); err != nil {
		return nil, err
	}
	series, labels := dimensionedSeries(results)
	if series == nil || len(labels) < 2 {
		return nil, nil
	}

	if !plotRequested(questionText) && !isDimensioned(labels) {
		return nil, nil
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
	}, nil
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
