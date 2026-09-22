package app

// Answer fit (relevance / grounding of SHAPE, not just of numbers).
//
// The composer already guarantees every number traces to a fact. That is not
// enough: a grounded answer of the WRONG question — per-pen feed variance for
// "total directed feed kg per day by park for the last 14 days" — is still a
// wrong answer, and it used to ship labelled as the answer. This file is the
// generic check that the answer's grouping, time grain, period and unit
// correspond to what was asked. It is deliberately topic-free: it parses the
// SHAPE of the question ("by X", "per day", "last N days", "kg", "%") and
// compares it with the shape the plan declared and, where it is inspectable
// (model-drafted SQL, Cube group-by), with the read that actually ran.
//
// Two consumers:
//   - planned answers (model plan): a mismatch is FLAGGED — the answer carries
//     an explicit note, the mode is downgraded to partial and it is not cached;
//   - deterministic fallback (model unavailable): a template or keyword plan
//     whose shape does not cover the question is REFUSED with an honest "can't
//     answer that precisely right now" instead of answering a different metric.

import (
	"regexp"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// RequestedShape is what the question asks for, parsed generically.
type RequestedShape struct {
	// Dimensions are canonical grouping dimensions ("park", "pen", "day", …).
	Dimensions []string
	// Units are canonical units the measure must be expressed in ("kg",
	// "money", "percent").
	Units []string
	// Window is the server-resolved period (zero when the question names none).
	Window Window
}

// canonical dimension vocabulary: question noun -> canonical dimension.
var dimensionNouns = map[string]string{
	"park": "park", "parks": "park", "farm": "park", "farms": "park", "site": "park", "sites": "park",
	"pen": "pen", "pens": "pen", "shed": "pen", "sheds": "pen", "partition": "pen", "partitions": "pen", "location": "pen", "locations": "pen",
	"species": "species",
	"breed":   "breed", "breeds": "breed",
	"operator": "operator", "operators": "operator", "vaccinator": "operator", "vaccinators": "operator",
	"person": "operator", "people": "operator", "staff": "operator", "employee": "operator", "employees": "operator", "worker": "operator", "workers": "operator",
	"vaccine": "vaccine", "vaccines": "vaccine",
	"item": "feed_item", "items": "feed_item", "ingredient": "feed_item", "ingredients": "feed_item",
	"stage": "stage", "stages": "stage",
	"sex": "sex", "gender": "sex",
	"load": "load", "loads": "load", "batch": "load", "batches": "load",
	"buyer": "buyer", "buyers": "buyer", "customer": "customer", "customers": "customer",
	"vendor": "vendor", "vendors": "vendor", "supplier": "vendor", "suppliers": "vendor", "source": "vendor", "sources": "vendor",
	"category": "category", "categories": "category", "type": "category", "types": "category", "kind": "category",
	"status": "status", "state": "status",
	"session": "session", "sessions": "session",
	"disease": "disease", "diseases": "disease", "cause": "disease", "causes": "disease",
	"day": "day", "days": "day", "date": "day", "dates": "day",
	"week": "week", "weeks": "week",
	"month": "month", "months": "month",
	"year": "year", "years": "year",
	"quarter": "quarter", "quarters": "quarter",
}

// dimensionEvidence: canonical dimension -> identifier fragments that prove a
// read is grouped/sliced by it (matched against the SELECT list + GROUP BY of
// a SQL read, or a Cube group_by/dimensions param).
var dimensionEvidence = map[string][]string{
	"park":      {"park", "farm"},
	"pen":       {"shed", "pen", "partition", "location"},
	"species":   {"species"},
	"breed":     {"breed"},
	"operator":  {"operator", "vaccinator", "member", "staff", "person", "worker", "assignee", "owner"},
	"vaccine":   {"vaccine", "rule", "protocol"},
	"feed_item": {"feed_item", "item", "feed_name", "ingredient"},
	"stage":     {"stage", "tag"},
	"sex":       {"sex", "gender"},
	"load":      {"load", "batch"},
	"buyer":     {"buyer", "customer"},
	"customer":  {"buyer", "customer"},
	"vendor":    {"vendor", "supplier", "source"},
	"category":  {"category", "type", "kind", "class"},
	"status":    {"status", "state", "outcome"},
	"session":   {"session"},
	"disease":   {"disease", "cause", "diagnosis", "condition"},
	"day":       {"date", "day"},
	"week":      {"week"},
	"month":     {"month"},
	"year":      {"year"},
	"quarter":   {"quarter"},
}

var coarserThanDay = []string{"week", "month", "year", "quarter"}

var (
	byDimensionRe = regexp.MustCompile(`\b(?:by|per|each|every)\s+(?:the\s+|each\s+|every\s+|individual\s+|one\s+)?([a-z]+)`)
	wiseRe        = regexp.MustCompile(`\b([a-z]+)[- ]?wise\b`)
	periodicRe    = regexp.MustCompile(`\b(daily|weekly|monthly|yearly|annually|quarterly|day[- ]by[- ]day|week[- ]by[- ]week|month[- ]by[- ]month|day[- ]wise|date[- ]wise|month[- ]wise|week[- ]wise)\b`)
	kgUnitRe      = regexp.MustCompile(`\b(kg|kgs|kilo|kilos|kilograms?|tonnes?|tons?)\b`)
	moneyUnitRe   = regexp.MustCompile(`(₹|\b(rs|inr|rupees?|lakh|lakhs|crore|cost|costs|spend|spent|revenue|price|prices)\b)`)
	percentUnitRe = regexp.MustCompile(`(%|\b(percent|percentage|pct)\b)`)
	// A "per X" that is a rate denominator, not a grouping ("kg per animal",
	// "average weight per head").
	rateDenominators = map[string]bool{"animal": true, "animals": true, "head": true, "heads": true, "kid": true, "kids": true, "goat": true, "goats": true, "sheep": true, "kg": true, "unit": true, "capita": true}
)

// ParseRequestedShape extracts the grouping, unit and period a question asks
// for. It is shape grammar only ("by X", "X-wise", "daily", "kg", "%"), never a
// per-topic phrase list.
func ParseRequestedShape(text string, window Window) RequestedShape {
	low := " " + strings.ToLower(text) + " "
	set := map[string]bool{}
	for _, m := range byDimensionRe.FindAllStringSubmatchIndex(low, -1) {
		word := low[m[2]:m[3]]
		next := ""
		if rest := strings.Fields(low[m[1]:]); len(rest) > 0 {
			next = strings.Trim(rest[0], "?.,!;:")
		}
		prefix := strings.TrimSpace(low[:m[0]])
		isPer := strings.HasPrefix(strings.TrimSpace(low[m[0]:m[1]]), "per")
		// "feed item"/"feed type" two-word nouns.
		if word == "feed" && (next == "item" || next == "items" || next == "type" || next == "types") {
			set["feed_item"] = true
			continue
		}
		if isPer && rateDenominators[word] {
			continue
		}
		// "average/mean ... per day" and "... per day on average" are rates
		// (average daily figure), not a day-by-day series.
		if isPer && (word == "day" || word == "week" || word == "month") && (strings.HasSuffix(prefix, "average") || strings.Contains(lastWords(prefix, 5), "average") || strings.Contains(lastWords(prefix, 5), "avg") || strings.Contains(lastWords(prefix, 5), "mean")) {
			continue
		}
		// "by today", "by now", "by the end": deadlines, not groupings.
		if d, ok := dimensionNouns[word]; ok {
			set[d] = true
		}
	}
	for _, m := range wiseRe.FindAllStringSubmatch(low, -1) {
		if d, ok := dimensionNouns[m[1]]; ok {
			set[d] = true
		}
	}
	for _, m := range periodicRe.FindAllStringSubmatch(low, -1) {
		w := m[1]
		switch {
		case strings.HasPrefix(w, "day") || strings.HasPrefix(w, "date") || w == "daily":
			// "average daily gain" / "daily gain" is a metric name, not a series.
			if w == "daily" && regexp.MustCompile(`daily\s+(gain|weight gain|growth)|average\s+daily`).MatchString(low) {
				continue
			}
			set["day"] = true
		case strings.HasPrefix(w, "week"):
			set["week"] = true
		case strings.HasPrefix(w, "month"):
			set["month"] = true
		case w == "yearly" || w == "annually":
			set["year"] = true
		case w == "quarterly":
			set["quarter"] = true
		}
	}
	var units []string
	if kgUnitRe.MatchString(low) {
		units = append(units, "kg")
	}
	if moneyUnitRe.MatchString(low) {
		units = append(units, "money")
	}
	if percentUnitRe.MatchString(low) {
		units = append(units, "percent")
	}
	dims := make([]string, 0, len(set))
	for d := range set {
		dims = append(dims, d)
	}
	sort.Strings(dims)
	return RequestedShape{Dimensions: dims, Units: units, Window: window}
}

func lastWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[len(f)-n:]
	}
	return strings.Join(f, " ")
}

var unitEvidence = map[string][]string{
	"kg":      {"kg", "weight", "qty", "quantity", "kilo", "tonne"},
	"money":   {"cost", "price", "amount", "value", "revenue", "inr", "rupee", "₹", "spend", "paid", "payment", "rs"},
	"percent": {"pct", "percent", "rate", "ratio", "%", "share", "compliance", "adherence"},
}

// readEvidence is the inspectable shape of what one sub-question actually read.
type readEvidence struct {
	// groupText is the SELECT list + GROUP BY text (lowercased) of a SQL read,
	// or the group_by/dimensions params of a Cube read. Empty = not inspectable.
	groupText string
	// measureText is everything that names the measure: SELECT list, labels,
	// fact labels/units, the declared measure, the tool/surface names.
	measureText string
	inspectable bool
}

func evidenceFor(sub domain.SubQuestion, res *domain.ToolResult) readEvidence {
	var ev readEvidence
	var measure []string
	if sql, _ := sub.Params["sql"].(string); strings.TrimSpace(sql) != "" && sub.Route == domain.RouteSQL {
		sel, group := splitSelectAndGroup(sql)
		ev.groupText = sel + " " + group
		ev.inspectable = true
		measure = append(measure, sel)
	} else if sub.Route == domain.RouteCube {
		var parts []string
		if gb, ok := sub.Params["group_by"].(string); ok {
			parts = append(parts, gb)
		}
		switch d := sub.Params["dimensions"].(type) {
		case string:
			parts = append(parts, d)
		case []string:
			parts = append(parts, d...)
		}
		ev.groupText = strings.ToLower(strings.Join(parts, " "))
		ev.inspectable = true
	}
	measure = append(measure, sub.ToolName, sub.IntentClass, sub.Declared.Measure)
	if res != nil {
		measure = append(measure, res.Surface, res.ToolName)
		for i, f := range res.Facts {
			if i >= 20 {
				break
			}
			measure = append(measure, f.Label, f.Unit)
		}
	}
	ev.measureText = strings.ToLower(strings.Join(measure, " "))
	return ev
}

// splitSelectAndGroup returns the lowercased SELECT list and GROUP BY list of
// a flat single-view SELECT (the only shape sqlguard admits).
func splitSelectAndGroup(sql string) (string, string) {
	low := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	sel := low
	if i := strings.Index(low, " from "); i >= 0 {
		sel = low[:i]
	}
	sel = strings.TrimPrefix(sel, "select ")
	group := ""
	if i := strings.Index(low, " group by "); i >= 0 {
		group = low[i+len(" group by "):]
		for _, stop := range []string{" having ", " order by ", " limit "} {
			if j := strings.Index(group, stop); j >= 0 {
				group = group[:j]
			}
		}
	}
	return sel, group
}

func dimensionSatisfied(dim, text string) bool {
	if text == "" {
		return false
	}
	frags := dimensionEvidence[dim]
	hit := false
	for _, f := range frags {
		if strings.Contains(text, f) {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	if dim == "day" {
		// A date column truncated to a coarser grain is not a daily series.
		for _, c := range coarserThanDay {
			if strings.Contains(text, "'"+c+"'") || strings.Contains(text, "date_trunc('"+c) {
				return strings.Contains(text, "'day'") || strings.Contains(text, "::date")
			}
		}
	}
	return true
}

func declaredHas(spec domain.AnswerSpec, dim string) bool {
	for _, d := range spec.Dimensions {
		if CanonicalDimension(d) == dim {
			return true
		}
	}
	return false
}

// CanonicalDimension maps a declared dimension name ("park_label", "shed",
// "feed_day", "Day") to the canonical vocabulary used by RequestedShape.
func CanonicalDimension(name string) string {
	low := strings.ToLower(strings.TrimSpace(name))
	if d, ok := dimensionNouns[low]; ok {
		return d
	}
	for dim := range dimensionEvidence {
		if low == dim {
			return dim
		}
	}
	// Longest-evidence match on an identifier ("park_label" -> park).
	best, bestLen := "", 0
	for dim, frags := range dimensionEvidence {
		for _, f := range frags {
			if strings.Contains(low, f) && len(f) > bestLen {
				best, bestLen = dim, len(f)
			}
		}
	}
	if best == "day" {
		for _, c := range coarserThanDay {
			if strings.Contains(low, c) {
				return c
			}
		}
	}
	return best
}

// FitIssue is one way an answer does not correspond to the question.
type FitIssue struct {
	Kind   string // "dimension" | "unit" | "window" | "measure"
	Detail string // user-safe, digit-free phrase (e.g. "per day")
}

// checkAnswerFit compares the requested shape with ONE sub-question's declared
// and executed shape. It returns nothing it cannot verify: a read whose shape
// is neither declared nor inspectable is not flagged (no false alarms on an
// API tool that already returns the requested grain natively).
func checkAnswerFit(req RequestedShape, sub domain.SubQuestion, res *domain.ToolResult) []FitIssue {
	ev := evidenceFor(sub, res)
	var issues []FitIssue
	for _, dim := range req.Dimensions {
		switch {
		case ev.inspectable:
			if !dimensionSatisfied(dim, ev.groupText) {
				issues = append(issues, FitIssue{Kind: "dimension", Detail: dimensionPhrase(dim)})
			}
		case len(sub.Declared.Dimensions) > 0:
			if !declaredHas(sub.Declared, dim) {
				issues = append(issues, FitIssue{Kind: "dimension", Detail: dimensionPhrase(dim)})
			}
		}
	}
	// Units are only checked where the measure is actually named: a SQL read
	// (its SELECT list) or a sub-question whose plan declared its measure.
	if ev.measureText != "" && (sub.Route == domain.RouteSQL || strings.TrimSpace(sub.Declared.Measure) != "") {
		for _, u := range req.Units {
			ok := false
			for _, f := range unitEvidence[u] {
				if strings.Contains(ev.measureText, f) {
					ok = true
					break
				}
			}
			if !ok {
				issues = append(issues, FitIssue{Kind: "unit", Detail: unitPhrase(u)})
			}
		}
	}
	return issues
}

// planFitIssues folds checkAnswerFit over a whole plan: a requested dimension
// or unit is satisfied when ANY sub-question covers it (a decomposed answer may
// split the grouping across sub-questions).
func planFitIssues(req RequestedShape, subs []domain.SubQuestion, results []domain.ToolResult) []FitIssue {
	if len(subs) == 0 || (len(req.Dimensions) == 0 && len(req.Units) == 0) {
		return nil
	}
	missing := map[string]FitIssue{}
	covered := map[string]bool{}
	for i, s := range subs {
		var res *domain.ToolResult
		if i < len(results) {
			res = &results[i]
			if res.Err != nil {
				continue
			}
		}
		got := checkAnswerFit(req, s, res)
		bad := map[string]bool{}
		for _, is := range got {
			key := is.Kind + ":" + is.Detail
			bad[key] = true
			missing[key] = is
		}
		for _, d := range req.Dimensions {
			key := "dimension:" + dimensionPhrase(d)
			if !bad[key] {
				covered[key] = true
			}
		}
		for _, u := range req.Units {
			key := "unit:" + unitPhrase(u)
			if !bad[key] {
				covered[key] = true
			}
		}
	}
	var out []FitIssue
	for key, is := range missing {
		if !covered[key] {
			out = append(out, is)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind+out[i].Detail < out[j].Kind+out[j].Detail })
	return out
}

func dimensionPhrase(dim string) string {
	switch dim {
	case "day", "week", "month", "year", "quarter":
		return "per " + dim
	case "feed_item":
		return "by feed item"
	case "pen":
		return "by pen"
	default:
		return "by " + dim
	}
}

func unitPhrase(u string) string {
	switch u {
	case "kg":
		return "in kilograms"
	case "money":
		return "in rupees"
	case "percent":
		return "as a percentage"
	}
	return u
}

// fitNote renders flagged issues as one honest, digit-free sentence appended
// to a planned answer (digit-free so the grounding reviewer never mistakes the
// note for an ungrounded figure).
func fitNote(issues []FitIssue) string {
	if len(issues) == 0 {
		return ""
	}
	var parts []string
	for _, is := range issues {
		parts = append(parts, is.Detail)
	}
	return "Note: this answer may not match the question exactly — I could not produce it " + joinHuman(parts) + " as asked. Please rephrase or ask for that breakdown explicitly."
}

func joinHuman(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " or " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}

// fallbackFit decides whether a deterministic (model-free) plan may answer the
// question. It is strict where checkAnswerFit is lenient: in fallback nothing
// else stands between the question and the answer, so an unverifiable shape is
// a refusal, not a pass. It returns the unmet requirements (empty = answer).
func fallbackFit(q domain.Question, req RequestedShape, subs []domain.SubQuestion) []FitIssue {
	var issues []FitIssue
	low := strings.ToLower(q.Text)
	for _, s := range subs {
		spec := s.Declared
		// Measure: a template answers only a question that asks for the
		// measure it computes.
		if spec.Template && len(spec.MeasureTerms) > 0 {
			hit := false
			for _, t := range spec.MeasureTerms {
				if strings.Contains(low, t) {
					hit = true
					break
				}
			}
			if !hit {
				issues = append(issues, FitIssue{Kind: "measure", Detail: "the measure you asked for"})
			}
		}
		// Grouping: every requested dimension must be in what the read groups by.
		ev := evidenceFor(s, nil)
		for _, dim := range req.Dimensions {
			if !dimensionSatisfied(dim, ev.groupText) {
				issues = append(issues, FitIssue{Kind: "dimension", Detail: dimensionPhrase(dim)})
			}
		}
		// Units: the read must express the requested unit.
		for _, u := range req.Units {
			ok := false
			for _, f := range unitEvidence[u] {
				if strings.Contains(ev.measureText+" "+strings.ToLower(spec.Description), f) {
					ok = true
					break
				}
			}
			if !ok {
				issues = append(issues, FitIssue{Kind: "unit", Detail: unitPhrase(u)})
			}
		}
		// Period: a named period must be bound exactly by the read.
		if !req.Window.IsZero() && !isAsOfDay(req.Window, q) {
			if spec.WindowFrom != req.Window.FromDate() || spec.WindowTo != req.Window.ToDate() || req.Window.Compare != nil {
				issues = append(issues, FitIssue{Kind: "window", Detail: "the period you asked about"})
			}
		}
	}
	return dedupeIssues(issues)
}

// isAsOfDay reports a window that is exactly the question's own business day
// ("today"): every deterministic read is an as-of-now read of that day, so it
// honours it. Any other period must be bound explicitly.
func isAsOfDay(w Window, q domain.Question) bool {
	if w.Compare != nil || q.AsOf.IsZero() {
		return false
	}
	day := q.AsOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	return w.FromDate() == day && w.ToDate() == day
}

func dedupeIssues(in []FitIssue) []FitIssue {
	seen := map[string]bool{}
	var out []FitIssue
	for _, is := range in {
		k := is.Kind + ":" + is.Detail
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, is)
	}
	return out
}

// fallbackCannotAnswer is the honest reply when the model planner is
// unavailable and no deterministic path answers the question as asked.
func fallbackCannotAnswer(subs []domain.SubQuestion, issues []FitIssue) string {
	desc := ""
	for _, s := range subs {
		if s.Declared.Description != "" {
			desc = s.Declared.Description
			break
		}
	}
	var parts []string
	for _, is := range issues {
		parts = append(parts, is.Detail)
	}
	msg := "I can't answer that precisely right now: the AI planner is unavailable, and the built-in fallback can't match what you asked for"
	if len(parts) > 0 {
		msg += " (" + strings.Join(parts, "; ") + ")"
	}
	msg += "."
	if desc != "" {
		msg += " Without the planner I can only report " + desc + ", which is a different figure."
	}
	return msg + " Please try again in a few minutes."
}
