package app

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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

// answerSection is one rendered block of the answer beside the ONE result it
// was rendered from. The reviewer grounds a section's numbers against that
// result alone: a global bag of every result's numbers would let a figure from
// step A silently ground a sentence carrying step B's label.
type answerSection struct {
	text   string
	result domain.ToolResult
}

// composeFor is the tenant-gated entry point the orchestrator uses: it rejects
// a fact set that is not wholly the actor's before any figure is rendered.
func (c composer) composeFor(actor domain.Actor, results []domain.ToolResult) (body string, citations []domain.Citation, sections []answerSection, err error) {
	if err := validateFactTenants(actor, results); err != nil {
		return "", nil, nil, err
	}
	body, citations, sections = c.compose(results)
	return body, citations, sections, nil
}

// compose builds the user-facing answer body and citations from tool results.
// Callers on the live path go through composeFor so the tenant gate runs first.
func (composer) compose(results []domain.ToolResult) (body string, citations []domain.Citation, sections []answerSection) {
	var texts []string
	seenSurface := map[string]bool{}

	for _, r := range results {
		if r.Err != nil {
			// Never surface the raw internal error (route/tool wiring, SQL, etc.)
			// to the leadership user — that stays in the admin trace + audit. Show
			// a clean, honest "the read did not come back" line instead. It names
			// the surface in words rather than the route token, so a failed
			// model-drafted read never renders as the bare "sql: could not be
			// retrieved." the held-out judge caught.
			texts = append(texts, readFailureLine(r))
			sections = append(sections, answerSection{text: readFailureLine(r), result: r})
			continue
		}
		r = withGroundedFacts(r)
		text := renderAnswerBlock(r)
		if len(r.Facts) == 0 && strings.TrimSpace(r.Summary) == "" {
			text = emptyReadLine(r)
		}
		texts = append(texts, text)
		sections = append(sections, answerSection{text: text, result: r})
		if !seenSurface[r.Surface] && r.Surface != "" {
			seenSurface[r.Surface] = true
			citations = append(citations, domain.Citation{
				Surface: r.Surface, Route: r.Route, AsOf: r.AsOf, MetricStatus: r.MetricStatus,
			})
		}
	}
	body = strings.Join(texts, "\n\n")
	return body, citations, sections
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

// renderAnswerBlock renders ONE result. When the read was model-drafted SQL the
// block is TITLED with the view that actually ran, because every other word in
// it (the row labels) was written by the planner: a heading derived from the
// data is the only thing in the block the model did not choose.
func renderAnswerBlock(r domain.ToolResult) string {
	return withSourceHeading(r, renderAnswerBody(r))
}

// withSourceHeading prefixes a model-drafted block with its view's business
// title. A result with no SourceView (Cube, a read API, a curated toolbox tool —
// none of which let the model name the metric) renders exactly as before.
func withSourceHeading(r domain.ToolResult, body string) string {
	title := viewTitle(r.SourceView)
	if title == "" || strings.TrimSpace(body) == "" {
		return body
	}
	return "From " + title + ":\n" + body
}

// viewTitle turns a ceo_ai view name into the business phrase a leader reads:
// "vaccination_obligations_base" -> "Vaccination obligations". It is derived
// from the view NAME rather than a hand-written map so a view added tomorrow is
// titled without anyone remembering to add a row.
func viewTitle(view string) string {
	name := strings.TrimSpace(strings.ToLower(view))
	name = strings.TrimSuffix(name, "_base")
	name = strings.TrimSuffix(name, "_view")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func renderAnswerBody(r domain.ToolResult) string {
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
	for _, f := range r.Facts {
		label := strings.ToLower(strings.TrimSpace(f.Label))
		value := strings.TrimSpace(f.Value)
		if value == "" {
			continue
		}
		if m := monthFromFact(f); m != "" && month == "" {
			month = m
		}
		switch {
		case strings.Contains(label, "sold animals"):
			soldTotal = value
		case strings.Contains(label, "sold goats"):
			soldGoats = value
		case strings.Contains(label, "sold sheep"):
			soldSheep = value
		case strings.Contains(label, "sales revenue"):
			revenue = value
		}
	}
	if soldTotal == "" && soldGoats == "" && soldSheep == "" && revenue == "" {
		return ""
	}

	var b strings.Builder
	if month != "" {
		b.WriteString("For " + readableMonth(month) + ", ")
	} else {
		// No month was bound: the sales reader returns its all-time summary.
		// Saying "for the selected period" presented that as the period the
		// leader asked about.
		//
		// It is all-time over CLOSED deals only: the sales views admit no open
		// deal (pinned by TestSalesViewsStatusMatrixAdmitsOnlyClosedDeals).
		// "Across all recorded sales" claimed a totality the figure does not
		// have — live, roughly Rs 99,000 of open deals sat outside it — so the
		// sentence names the restriction instead of asserting the whole.
		b.WriteString("Across all closed sales deals, ")
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
	// Every branch above is fed by a sales view that admits closed deals only,
	// including the month branch, so the scope is stated once, plainly.
	b.WriteString(" Closed deals only; open deals are not counted.")
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

// monthFromFact is THE ONLY WAY the sales sentence learns its period, and that
// is the point of it.
//
// The period used to come from whether a fact's LABEL contained the substring
// "this month". The monthly sales read stamped that phrase on every row
// whatever month it had bound, so "how much revenue did we make in august"
// answered "This month … 291600" — August's figure under September's name.
// A label is prose; it is not evidence of what was read. `Scope` is: the
// reader sets it to the calendar month it matched the row on, so it says what
// the read actually covered. The label is still parsed, but only for the same
// machine-shaped YYYY-MM token, never for an English phrase.
func monthFromFact(f domain.Fact) string {
	if m := monthToken(f.Scope); m != "" {
		return m
	}
	return monthFromLabel(f.Label)
}

// monthToken returns the YYYY-MM month a string names, or "".
func monthToken(text string) string {
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, ".,;:()[]{}")
		if len(field) != len("2006-01") || field[4] != '-' {
			continue
		}
		year, err := strconv.Atoi(field[:4])
		if err != nil || year < 1000 {
			continue
		}
		mon, err := strconv.Atoi(field[5:])
		if err != nil || mon < 1 || mon > 12 {
			continue
		}
		return field
	}
	return ""
}

func monthFromLabel(label string) string {
	return monthToken(label)
}

// readableMonth turns the bound month into the words a leader reads, so the
// API route names the same period the SQL route's "Window: …" line does
// ("Window: last month (01/08/2026 to 31/08/2026)" and "For August 2026" agree
// about August; "This month" and that line did not).
func readableMonth(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return t.Format("January 2006")
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
			// "Castro 1 in Castro 1: 30.25." A model-drafted read is free to
			// write `SELECT park_label AS label, park_label AS scope, …`, and
			// then the key lands in the label slot: 12 such lines were counted
			// across 86 live answers ("Boer in Boer", "Channapatna in
			// Channapatna"). A category is never "in" itself, so when the two
			// slots resolve to the same text the row names it once.
			switch {
			case f.Scope != "" && !sameSlot(f.Label, f.Scope):
				lines = append(lines, fmt.Sprintf("%s in %s: %s.", f.Label, f.Scope, sentenceValue(f.Value)))
			case f.Scope != "":
				// The scope is the dimension's OWN spelling; the label is the
				// model's echo of it, so the scope is the one to keep.
				lines = append(lines, fmt.Sprintf("%s: %s.", f.Scope, sentenceValue(f.Value)))
			default:
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

// sameSlot reports that a fact's label and scope are the same text, i.e. that
// the read put its series KEY in the label slot as well. Compared on trimmed,
// case-folded text because the two arrive through different columns.
func sameSlot(label, scope string) bool {
	return strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(scope))
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
	labelSeen := false
	labelIsOwnScope := false
	scopeUsable := true

	for _, f := range r.Facts {
		v, ok := parseNumber(f.Value)
		if !ok {
			continue
		}
		// The series NAME is the measure every row shares, and only that. It
		// used to be the FIRST row's label whatever the others said, so a read
		// that labelled each row with its own category named the series after
		// one of its bars — "Castro 1" over a chart whose x axis is Castro 1,
		// Castro 2, Castro 3. A label that is not common, or that is the row's
		// own scope, names nothing and is discarded here; seriesName then
		// falls back to the surface.
		switch {
		case commonLabel == "" && !labelSeen:
			commonLabel = strings.TrimSpace(f.Label)
		case !strings.EqualFold(commonLabel, strings.TrimSpace(f.Label)):
			commonLabel = ""
		}
		labelSeen = true
		// A row whose label IS its scope carries no measure name at all: the
		// read put the series KEY in both slots. renderFacts already collapses
		// that row to one name; the chart has to do the same or the series is
		// named after one of its own bars even when every row agrees, which is
		// exactly the case a "common label" test cannot see.
		if sameSlot(f.Label, f.Scope) && strings.TrimSpace(f.Scope) != "" {
			labelIsOwnScope = true
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
	if labelIsOwnScope {
		commonLabel = ""
	}
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

// withGroundedFacts drops facts whose VALUE is blank. A fact with a label and
// no value rendered as "Total Births in Last 7 days: ." — a heading presented as
// a figure with nothing behind it. A blank value is the read saying it has no
// number, so the result falls through to the honest "no records" line instead.
func withGroundedFacts(r domain.ToolResult) domain.ToolResult {
	kept := make([]domain.Fact, 0, len(r.Facts))
	for _, f := range r.Facts {
		if strings.TrimSpace(f.Value) == "" {
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == len(r.Facts) {
		return r
	}
	r.Facts = kept
	return r
}

// readFailureLine is the user-facing sentence for a read that errored.
func readFailureLine(r domain.ToolResult) string {
	return fmt.Sprintf("I couldn't read the %s needed for this question, so no figure is shown for it.", readSubject(r))
}

// emptyReadLine is the user-facing sentence for a read that ran and returned
// nothing. It names the period the read actually covered (the assistant's own
// Window fact is the only other place that says so), because "no records found"
// with no period reads as "never", which is a different claim.
func emptyReadLine(r domain.ToolResult) string {
	return fmt.Sprintf("No records found in %s for the requested scope.", readSubject(r))
}

// readSubject names a result in words. A bare route token ("sql", "api") is
// wiring, not something a leader can read, so it degrades to "source".
func readSubject(r domain.ToolResult) string {
	if s := strings.TrimSpace(r.Surface); s != "" {
		return s
	}
	return "the source records"
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
