package app

// timerange.go is the server-side time-window resolver (plan v3 D1.2). It
// turns the period a leader wrote in a question ("last month", "Aug vs Sep",
// "since 1 July", "Q3 2026", "2026-08-01 to 2026-08-15") into concrete
// inclusive business-day windows in the Goat OS India business calendar
// (Asia/Kolkata). UTC never defines a Goat OS business day.
//
// The resolver is deterministic, bounded and pure: it never guesses. A phrase
// it cannot ground returns ok=false so the pipeline can say so rather than
// silently answering a period question with an all-time aggregate. The
// resolved window is threaded into every sub-question's Params as from/to
// (see injectWindow) and enforced on model-drafted SQL by
// sqlguard.ValidateWindow.
//
// The pure resolver used to live in ceoai/wiring_timerange.go beside the Cube
// timeDimension binding; the binding still lives there and now calls
// ResolveWindow, so Cube, API and SQL tiers share one calendar.

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Window is one resolved inclusive business-day period. From and To are
// day-start instants in the business calendar. Label is the normalized phrase
// that produced it. Compare, when set, is the second window of a two-window
// comparison ("this month vs last month"); the primary window is the FIRST
// phrase in the question.
type Window struct {
	From    time.Time
	To      time.Time
	Label   string
	Compare *Window
}

// IsZero reports whether no window was resolved.
func (w Window) IsZero() bool { return w.From.IsZero() }

// FromDate / ToDate are the ISO business dates (YYYY-MM-DD) of the bounds.
func (w Window) FromDate() string { return w.From.Format("2006-01-02") }
func (w Window) ToDate() string   { return w.To.Format("2006-01-02") }

// Describe renders the window for the answer body in the farm date format
// (DD/MM/YYYY), e.g. "last month (01/08/2026 to 31/08/2026)". A comparison
// renders both halves joined by " vs ".
func (w Window) Describe() string {
	if w.IsZero() {
		return ""
	}
	s := describeOne(w)
	if w.Compare != nil && !w.Compare.IsZero() {
		s += " vs " + describeOne(*w.Compare)
	}
	return s
}

func describeOne(w Window) string {
	from, to := biztime.FarmDate(w.From), biztime.FarmDate(w.To)
	span := from
	if from != to {
		span = from + " to " + to
	}
	if strings.TrimSpace(w.Label) == "" {
		return span
	}
	return w.Label + " (" + span + ")"
}

// ResolveWindow extracts the FIRST groundable period phrase from text (a
// question or a planner time_range param) and resolves it against now in loc
// (nil loc = the Goat OS business calendar). ok=false when no phrase grounds.
// A two-window comparison ("Aug vs Sep", "this month compared to last month",
// "compare July and August") sets Window.Compare.
func ResolveWindow(text string, now time.Time, loc *time.Location) (Window, bool) {
	if loc == nil {
		loc = biztime.DefaultLocation()
	}
	today := biztime.BusinessDayStart(now.In(loc))
	norm := normalizePhrase(text)
	if norm == "" {
		return Window{}, false
	}
	matches := findPhraseMatches(norm)
	if len(matches) == 0 {
		return Window{}, false
	}
	var resolved []phraseWindow
	for _, m := range matches {
		w, ok := m.pattern.resolve(m, today, loc)
		if !ok {
			continue
		}
		w.Label = strings.TrimSpace(norm[m.start:m.end])
		resolved = append(resolved, phraseWindow{win: w, start: m.start, end: m.end})
	}
	if len(resolved) == 0 {
		return Window{}, false
	}
	first := resolved[0]
	if len(resolved) >= 2 {
		second := resolved[1]
		between := strings.TrimSpace(norm[first.end:second.start])
		before := norm[:first.start]
		if isComparisonConnector(between, before) {
			cmp := second.win
			first.win.Compare = &cmp
		}
	}
	return first.win, true
}

type phraseWindow struct {
	win        Window
	start, end int
}

// isComparisonConnector reports whether the text between two period phrases
// (and the text before the first) reads as a comparison.
func isComparisonConnector(between, before string) bool {
	b := strings.TrimSpace(between)
	switch b {
	case "vs", "vs.", "v", "versus", "against", "compared to", "compared with", "compare to", "compare with", "relative to", "over":
		return true
	}
	// "compare X and Y", "compare X to Y", "X and Y compared"
	if (b == "and" || b == "to" || b == "with") && strings.Contains(before, "compar") {
		return true
	}
	return false
}

// normalizePhrase lower-cases, collapses whitespace, and unifies a few
// punctuation variants so the regexes stay small.
func normalizePhrase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("–", "-", "—", "-", "’", "'", " ", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// --- phrase patterns ---------------------------------------------------------

type phraseMatch struct {
	pattern    *phrasePattern
	start, end int
	groups     []string
	text       string // the whole matched phrase
}

type phrasePattern struct {
	re      *regexp.Regexp
	resolve func(m phraseMatch, today time.Time, loc *time.Location) (Window, bool)
}

const monthAlt = `(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)`

var (
	reISORange  = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\s*(?:\.\.|to|-|through|until|till|and)\s*(\d{4}-\d{2}-\d{2})\b`)
	reISOSingle = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
	reDMYRange  = regexp.MustCompile(`\b(\d{1,2}/\d{1,2}/\d{4})\s*(?:\.\.|to|-|through|until|till|and)\s*(\d{1,2}/\d{1,2}/\d{4})\b`)
	reDMYSingle = regexp.MustCompile(`\b(\d{1,2}/\d{1,2}/\d{4})\b`)
	reSince     = regexp.MustCompile(`\b(?:since|from|starting|starting from|after)\s+(\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2}/\d{4}|(?:\d{1,2}(?:st|nd|rd|th)?\s+)?` + monthAlt + `(?:\s+\d{1,2}(?:st|nd|rd|th)?)?(?:,?\s*\d{4})?)\b`)
	reRelDay    = regexp.MustCompile(`\b(day before yesterday|yesterday|today)\b`)
	reToDate    = regexp.MustCompile(`\b(mtd|month to date|month-to-date|ytd|year to date|year-to-date|qtd|quarter to date|quarter-to-date|wtd|week to date|week-to-date)\b`)
	reThisLast  = regexp.MustCompile(`\b(this|current|last|previous|past|prior)\s+(week|month|quarter|year)\b`)
	reLastN     = regexp.MustCompile(`\b(?:last|past|previous|trailing|prior)\s+(\d{1,3}|a|an|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|days|week|weeks|month|months|quarter|quarters|year|years)\b`)
	reQuarter   = regexp.MustCompile(`\b(?:q([1-4])(?:\s*[-' ]?\s*(\d{4}|\d{2}))?|(first|second|third|fourth|1st|2nd|3rd|4th)\s+quarter(?:\s+(?:of\s+)?(\d{4}))?)\b`)
	// A named single day: "18 september 2026", "18th sep", "september 18, 2026".
	// It must win over reMonth, which would otherwise read "on 18 September
	// 2026" as the whole month.
	reNamedDay   = regexp.MustCompile(`\b(?:\d{1,2}(?:st|nd|rd|th)?\s+` + monthAlt + `|` + monthAlt + `\s+\d{1,2}(?:st|nd|rd|th)?)(?:,?\s*\d{4})?\b`)
	reMonth      = regexp.MustCompile(`\b` + monthAlt + `(?:\s*,?\s*'?(\d{4}|\d{2}))?\b`)
	reYear       = regexp.MustCompile(`\b(?:in|for|during|of|year)\s+(20\d{2})\b`)
	reWordNumber = map[string]int{"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12}
)

var monthIndex = map[string]time.Month{
	"jan": time.January, "january": time.January, "feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March, "apr": time.April, "april": time.April, "may": time.May,
	"jun": time.June, "june": time.June, "jul": time.July, "july": time.July, "aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September, "oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November, "dec": time.December, "december": time.December,
}

// phrasePatterns is ordered most-specific first; overlapping later matches are
// dropped by findPhraseMatches.
var phrasePatterns = []*phrasePattern{
	{re: reISORange, resolve: resolveISORange},
	{re: reDMYRange, resolve: resolveDMYRange},
	{re: reSince, resolve: resolveSince},
	{re: reISOSingle, resolve: resolveISOSingle},
	{re: reDMYSingle, resolve: resolveDMYSingle},
	{re: reRelDay, resolve: resolveRelDay},
	{re: reToDate, resolve: resolveToDate},
	{re: reLastN, resolve: resolveLastN},
	{re: reThisLast, resolve: resolveThisLast},
	{re: reQuarter, resolve: resolveQuarter},
	{re: reNamedDay, resolve: resolveNamedDay},
	{re: reMonth, resolve: resolveMonth},
	{re: reYear, resolve: resolveYear},
}

// findPhraseMatches runs every pattern, then keeps non-overlapping matches in
// text order (earlier patterns win on overlap because they are more specific).
func findPhraseMatches(norm string) []phraseMatch {
	var all []phraseMatch
	for _, p := range phrasePatterns {
		for _, loc := range p.re.FindAllStringSubmatchIndex(norm, -1) {
			m := phraseMatch{pattern: p, start: loc[0], end: loc[1], text: norm[loc[0]:loc[1]]}
			for g := 1; g < len(loc)/2; g++ {
				if loc[2*g] >= 0 {
					m.groups = append(m.groups, norm[loc[2*g]:loc[2*g+1]])
				} else {
					m.groups = append(m.groups, "")
				}
			}
			if p.re == reMonth && !monthMatchIsPeriod(norm, m) {
				continue
			}
			if p.re == reNamedDay && !namedDayIsPeriod(norm, m) {
				continue
			}
			overlaps := false
			for _, kept := range all {
				if m.start < kept.end && kept.start < m.end {
					overlaps = true
					break
				}
			}
			if !overlaps {
				all = append(all, m)
			}
		}
	}
	// Stable sort by start position.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].start < all[j-1].start; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	return all
}

// monthMatchIsPeriod rejects the one month name that is also an ordinary
// English word: a bare "may" ("how many may be sick") is a period only when a
// year follows it or a period preposition precedes it.
func monthMatchIsPeriod(norm string, m phraseMatch) bool {
	if group(m, 0) != "may" || group(m, 1) != "" {
		return true
	}
	before := strings.Fields(norm[:m.start])
	if len(before) == 0 {
		return false
	}
	switch before[len(before)-1] {
	case "in", "for", "during", "of", "since", "from", "till", "until", "to", "through", "vs", "vs.", "versus", "and", "compare", "against", "with":
		return true
	}
	return false
}

// namedDayIsPeriod applies the same "may" caution to a named day: "12 may be
// sick" is not the 12th of May unless a year follows or a date preposition
// precedes it.
// Compiled once: namedDayIsPeriod runs per phrase match on every question, and
// regexp.MustCompile inside it recompiled both patterns on each call.
var (
	reBareMay       = regexp.MustCompile(`\bmay\b`)
	reFourDigitYear = regexp.MustCompile(`\d{4}`)
)

func namedDayIsPeriod(norm string, m phraseMatch) bool {
	if !reBareMay.MatchString(m.text) || reFourDigitYear.MatchString(m.text) {
		return true
	}
	before := strings.Fields(norm[:m.start])
	if len(before) == 0 {
		return false
	}
	switch before[len(before)-1] {
	case "on", "since", "from", "till", "until", "to", "by", "for", "of", "before", "after":
		return true
	}
	return false
}

func group(m phraseMatch, i int) string {
	if i < len(m.groups) {
		return m.groups[i]
	}
	return ""
}

// --- resolvers ---------------------------------------------------------------

func parseISO(s string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func parseDMY(s string, loc *time.Location) (time.Time, bool) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	d, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	y, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc)
	if t.Day() != d { // e.g. 31/02
		return time.Time{}, false
	}
	return t, true
}

func orderedRange(f, t time.Time) Window {
	if f.After(t) {
		f, t = t, f
	}
	return Window{From: f, To: t}
}

func resolveISORange(m phraseMatch, _ time.Time, loc *time.Location) (Window, bool) {
	f, ok1 := parseISO(group(m, 0), loc)
	t, ok2 := parseISO(group(m, 1), loc)
	if !ok1 || !ok2 {
		return Window{}, false
	}
	return orderedRange(f, t), true
}

func resolveDMYRange(m phraseMatch, _ time.Time, loc *time.Location) (Window, bool) {
	f, ok1 := parseDMY(group(m, 0), loc)
	t, ok2 := parseDMY(group(m, 1), loc)
	if !ok1 || !ok2 {
		return Window{}, false
	}
	return orderedRange(f, t), true
}

func resolveISOSingle(m phraseMatch, _ time.Time, loc *time.Location) (Window, bool) {
	d, ok := parseISO(group(m, 0), loc)
	if !ok {
		return Window{}, false
	}
	return Window{From: d, To: d}, true
}

func resolveDMYSingle(m phraseMatch, _ time.Time, loc *time.Location) (Window, bool) {
	d, ok := parseDMY(group(m, 0), loc)
	if !ok {
		return Window{}, false
	}
	return Window{From: d, To: d}, true
}

// resolveSince: "since <date>" runs from that date to today. A month-only
// anchor ("since August") starts on the 1st of that month; "since 5 Aug" or
// "since Aug 5" on that day.
func resolveSince(m phraseMatch, today time.Time, loc *time.Location) (Window, bool) {
	anchor := strings.TrimSpace(group(m, 0))
	var from time.Time
	var ok bool
	switch {
	case strings.Contains(anchor, "-"):
		from, ok = parseISO(anchor, loc)
	case strings.Contains(anchor, "/"):
		from, ok = parseDMY(anchor, loc)
	default:
		from, ok = parseMonthDayYear(anchor, today, loc)
	}
	if !ok || from.After(today) {
		return Window{}, false
	}
	return Window{From: from, To: today}, true
}

// resolveNamedDay resolves one named calendar day to a single-day window.
func resolveNamedDay(m phraseMatch, today time.Time, loc *time.Location) (Window, bool) {
	day, ok := parseMonthDayYear(m.text, today, loc)
	if !ok {
		return Window{}, false
	}
	return Window{From: day, To: day}, true
}

var reOrdinal = regexp.MustCompile(`^(\d{1,2})(?:st|nd|rd|th)?$`)

// parseMonthDayYear parses "august", "5 august", "august 5", "august 2026",
// "5 aug 2026", "aug 5, 2026" into a day (1st when no day given). A month
// without a year resolves to the most recent occurrence on or before today.
func parseMonthDayYear(s string, today time.Time, loc *time.Location) (time.Time, bool) {
	fields := strings.Fields(strings.ReplaceAll(s, ",", " "))
	var month time.Month
	day, year := 0, 0
	for _, f := range fields {
		f = strings.TrimPrefix(f, "'")
		if mm, ok := monthIndex[f]; ok && month == 0 {
			month = mm
			continue
		}
		if len(f) == 4 {
			if y, err := strconv.Atoi(f); err == nil && y >= 2000 && y <= 2099 {
				year = y
				continue
			}
		}
		if len(f) == 2 && day != 0 {
			if y, err := strconv.Atoi(f); err == nil {
				year = 2000 + y
				continue
			}
		}
		if g := reOrdinal.FindStringSubmatch(f); g != nil && day == 0 {
			d, _ := strconv.Atoi(g[1])
			if d >= 1 && d <= 31 {
				day = d
				continue
			}
		}
	}
	if month == 0 {
		return time.Time{}, false
	}
	if day == 0 {
		day = 1
	}
	if year == 0 {
		year = today.Year()
		if month > today.Month() {
			year--
		}
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if t.Day() != day {
		return time.Time{}, false
	}
	return t, true
}

func resolveRelDay(m phraseMatch, today time.Time, _ *time.Location) (Window, bool) {
	switch group(m, 0) {
	case "today":
		return Window{From: today, To: today}, true
	case "yesterday":
		y := today.AddDate(0, 0, -1)
		return Window{From: y, To: y}, true
	case "day before yesterday":
		y := today.AddDate(0, 0, -2)
		return Window{From: y, To: y}, true
	}
	return Window{}, false
}

func resolveToDate(m phraseMatch, today time.Time, _ *time.Location) (Window, bool) {
	switch g := group(m, 0); {
	case strings.HasPrefix(g, "mtd"), strings.HasPrefix(g, "month"):
		return Window{From: startOfMonth(today), To: today}, true
	case strings.HasPrefix(g, "ytd"), strings.HasPrefix(g, "year"):
		return Window{From: startOfYear(today), To: today}, true
	case strings.HasPrefix(g, "qtd"), strings.HasPrefix(g, "quarter"):
		return Window{From: startOfQuarter(today), To: today}, true
	case strings.HasPrefix(g, "wtd"), strings.HasPrefix(g, "week"):
		return Window{From: startOfWeek(today), To: today}, true
	}
	return Window{}, false
}

// resolveThisLast: this/last week|month|quarter|year. "this <unit>" is the
// whole unit (a future end is fine — the data simply stops at today), except
// "this year", which is year-to-date to keep the Cube binding's established
// semantics.
func resolveThisLast(m phraseMatch, today time.Time, _ *time.Location) (Window, bool) {
	which, unit := group(m, 0), group(m, 1)
	current := which == "this" || which == "current"
	switch unit {
	case "week":
		start := startOfWeek(today)
		if !current {
			start = start.AddDate(0, 0, -7)
		}
		return Window{From: start, To: start.AddDate(0, 0, 6)}, true
	case "month":
		start := startOfMonth(today)
		if !current {
			start = startOfMonth(start.AddDate(0, 0, -1))
		}
		return Window{From: start, To: endOfMonth(start)}, true
	case "quarter":
		start := startOfQuarter(today)
		if !current {
			start = startOfQuarter(start.AddDate(0, 0, -1))
		}
		return Window{From: start, To: endOfQuarter(start)}, true
	case "year":
		if current {
			return Window{From: startOfYear(today), To: today}, true
		}
		start := startOfYear(today).AddDate(-1, 0, 0)
		return Window{From: start, To: endOfYear(start)}, true
	}
	return Window{}, false
}

// resolveLastN: "last N days|weeks|months|quarters|years", trailing and
// inclusive of today (last 7 days = today and the 6 before it).
func resolveLastN(m phraseMatch, today time.Time, _ *time.Location) (Window, bool) {
	nStr, unit := group(m, 0), group(m, 1)
	n, ok := reWordNumber[nStr]
	if !ok {
		v, err := strconv.Atoi(nStr)
		if err != nil {
			return Window{}, false
		}
		n = v
	}
	if n <= 0 {
		return Window{}, false
	}
	switch {
	case strings.HasPrefix(unit, "day"):
		return Window{From: today.AddDate(0, 0, -(n - 1)), To: today}, true
	case strings.HasPrefix(unit, "week"):
		return Window{From: today.AddDate(0, 0, -(7*n - 1)), To: today}, true
	case strings.HasPrefix(unit, "month"):
		return Window{From: today.AddDate(0, -n, 0).AddDate(0, 0, 1), To: today}, true
	case strings.HasPrefix(unit, "quarter"):
		return Window{From: today.AddDate(0, -3*n, 0).AddDate(0, 0, 1), To: today}, true
	case strings.HasPrefix(unit, "year"):
		return Window{From: today.AddDate(-n, 0, 0).AddDate(0, 0, 1), To: today}, true
	}
	return Window{}, false
}

// resolveQuarter: "q3", "q3 2026", "q3'26", "third quarter of 2026". A quarter
// without a year is the most recent one on or before today's quarter.
func resolveQuarter(m phraseMatch, today time.Time, loc *time.Location) (Window, bool) {
	qStr, yStr := group(m, 0), group(m, 1)
	if qStr == "" {
		switch group(m, 2) {
		case "first", "1st":
			qStr = "1"
		case "second", "2nd":
			qStr = "2"
		case "third", "3rd":
			qStr = "3"
		case "fourth", "4th":
			qStr = "4"
		}
		yStr = group(m, 3)
	}
	q, err := strconv.Atoi(qStr)
	if err != nil || q < 1 || q > 4 {
		return Window{}, false
	}
	year := 0
	if yStr != "" {
		y, err := strconv.Atoi(yStr)
		if err != nil {
			return Window{}, false
		}
		if y < 100 {
			y += 2000
		}
		year = y
	}
	if year == 0 {
		year = today.Year()
		if q > quarterOf(today) {
			year--
		}
	}
	start := time.Date(year, time.Month(3*(q-1)+1), 1, 0, 0, 0, 0, loc)
	return Window{From: start, To: endOfQuarter(start)}, true
}

// resolveMonth: "august", "aug 2026", "august '26". Without a year: the most
// recent occurrence on or before the current month.
func resolveMonth(m phraseMatch, today time.Time, loc *time.Location) (Window, bool) {
	month, ok := monthIndex[group(m, 0)]
	if !ok {
		return Window{}, false
	}
	year := 0
	if y := group(m, 1); y != "" {
		v, err := strconv.Atoi(y)
		if err != nil {
			return Window{}, false
		}
		if v < 100 {
			v += 2000
		}
		year = v
	}
	if year == 0 {
		year = today.Year()
		if month > today.Month() {
			year--
		}
	}
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	return Window{From: start, To: endOfMonth(start)}, true
}

// resolveYear: "in 2025", "for 2026", "during 2024" → the calendar year.
func resolveYear(m phraseMatch, _ time.Time, loc *time.Location) (Window, bool) {
	y, err := strconv.Atoi(group(m, 0))
	if err != nil {
		return Window{}, false
	}
	start := time.Date(y, time.January, 1, 0, 0, 0, 0, loc)
	return Window{From: start, To: endOfYear(start)}, true
}

// --- business-calendar boundary helpers (all inputs are day-start in loc) ---

// startOfWeek returns the Monday of t's week (Goat OS weeks are Monday-start).
func startOfWeek(t time.Time) time.Time {
	wd := int(t.Weekday()) // Sunday=0
	delta := wd - 1
	if wd == 0 {
		delta = 6
	}
	return t.AddDate(0, 0, -delta)
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

func endOfMonth(t time.Time) time.Time {
	return startOfMonth(t).AddDate(0, 1, 0).AddDate(0, 0, -1)
}

func quarterOf(t time.Time) int { return (int(t.Month())-1)/3 + 1 }

func startOfQuarter(t time.Time) time.Time {
	y := t.Year()
	q := quarterOf(t)
	return time.Date(y, time.Month(3*(q-1)+1), 1, 0, 0, 0, 0, t.Location())
}

func endOfQuarter(t time.Time) time.Time {
	return startOfQuarter(t).AddDate(0, 3, 0).AddDate(0, 0, -1)
}

func startOfYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	return time.Date(y, time.January, 1, 0, 0, 0, 0, t.Location())
}

func endOfYear(t time.Time) time.Time {
	return startOfYear(t).AddDate(1, 0, 0).AddDate(0, 0, -1)
}
