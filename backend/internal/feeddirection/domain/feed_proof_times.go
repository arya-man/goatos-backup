package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// The 17:30 feed-proof-times report (maintainer decision 2026-09-05).
//
// Every feed day, every pen owes THREE captures per session: the feed WEIGHT photo (taken on the
// scale, before anything is given out), the feed DISTRIBUTION video, and the WATER video. They are
// the three refs on one feed_distribution_completions row -- one record per pen per session -- so
// the report is a read of that row plus the upload instants of the artifacts it points at.
//
// THE TIME IS THE UPLOAD TIME, and the maintainer chose it knowing what it means. proof_artifacts
// stamps uploaded_at when the object actually lands and the backend marks the upload completed, so
// a phone that captured at 09:10 and only found signal at 11:40 reports 11:40. That is the instant
// the farm can prove; a client-claimed capture time is an unverified number the phone supplies.
// The consequence to carry: a late upload reads as late work, and the honest reading of this
// report is "when the evidence reached us", never "when the animals ate".
//
// LATEST UPLOAD WINS. A verifier rejection sends a pen back for a re-shoot, and the replacement
// capture becomes the row's ref. The report follows the ref, so it always shows the capture that
// currently STANDS as the proof -- never a superseded one whose video no longer exists as evidence.
//
// THE EXPECTED SET IS THE ISSUED SHEET, not the completions table. Reading completions alone would
// make a pen nobody fed simply absent, and a report that goes quiet exactly when work was skipped
// is the opposite of what 17:30 is for. Every pen-session on the day's live feed sheet appears,
// with "--" where a capture never arrived.

// FeedProofCapture is one of the three captures a pen-session owes.
type FeedProofCapture struct {
	// UploadedAt is the instant the artifact finished landing in storage. Zero when the capture is
	// absent -- either never taken, or cleared for a re-shoot after a rejection.
	UploadedAt time.Time
}

// Recorded reports whether the capture has actually arrived.
func (c FeedProofCapture) Recorded() bool { return !c.UploadedAt.IsZero() }

// PenSessionProofTimes is one row of the report: one pen, one session, one feed day.
type PenSessionProofTimes struct {
	ShedID         string
	ShedName       string
	PartitionLabel string
	SessionNo      int
	SessionLabel   string
	Workflow       string

	// Status is the feed_distribution_completions status ('pending_verification', 'completed',
	// 'rework'), or empty when the operator never submitted the session at all. It is carried for
	// the footer count, not to change which captures are shown.
	Status string

	Weight       FeedProofCapture
	Distribution FeedProofCapture
	Water        FeedProofCapture
}

// PenDisplay renders the pen the way the farm names it, through the one canonical helper.
func (r PenSessionProofTimes) PenDisplay() string {
	return oploc.OperationalLocation{ShedName: r.ShedName, PartitionLabel: r.PartitionLabel}.Display()
}

// Complete reports whether all three captures have landed.
func (r PenSessionProofTimes) Complete() bool {
	return r.Weight.Recorded() && r.Distribution.Recorded() && r.Water.Recorded()
}

// FeedProofTimesReport is one park's whole feed day.
type FeedProofTimesReport struct {
	ParkID   string
	ParkName string
	// FeedDay is the business date the sheet was issued for, as YYYY-MM-DD.
	FeedDay string
	Rows    []PenSessionProofTimes
}

// MissingCount is the number of pen-sessions still short of at least one capture.
func (r FeedProofTimesReport) MissingCount() int {
	missing := 0
	for _, row := range r.Rows {
		if !row.Complete() {
			missing++
		}
	}
	return missing
}

// feedProofTimeMissing is what a capture that never arrived renders as. Two characters wide so the
// columns stay aligned under a monospace font.
const feedProofTimeMissing = "--"

// FeedProofTimesTitle is the message headline.
func FeedProofTimesTitle(report FeedProofTimesReport) string {
	park := strings.TrimSpace(report.ParkName)
	if park == "" {
		park = "Feed"
	}
	return fmt.Sprintf("%s · feed proof times · %s", park, biztime.FarmDateFromBusinessDate(report.FeedDay))
}

// PenProofTimes is one PEN's whole feed day: its sessions, keyed by session number.
//
// The report is READ per pen and stored per pen-session, because a pen's morning and evening are
// separately captured, separately verified facts. Grouping happens here, at the edge, so the two
// grains never get confused upstream.
type PenProofTimes struct {
	Pen      string
	Sessions map[int]PenSessionProofTimes
}

// Complete reports whether every session this pen owes has all three captures. A session the pen
// owes but never submitted is absent from the map and therefore incomplete.
func (p PenProofTimes) Complete(sessionNos []int) bool {
	for _, sessionNo := range sessionNos {
		row, ok := p.Sessions[sessionNo]
		if !ok || !row.Complete() {
			return false
		}
	}
	return true
}

// SessionNumbers is every session the park ran that day, ascending. It is derived from the sheet
// rather than assumed to be {1, 2}: a park that adds a third feeding gets a third column instead of
// silently losing it.
func (r FeedProofTimesReport) SessionNumbers() []int {
	seen := map[int]bool{}
	for _, row := range r.Rows {
		seen[row.SessionNo] = true
	}
	out := make([]int, 0, len(seen))
	for sessionNo := range seen {
		out = append(out, sessionNo)
	}
	sort.Ints(out)
	return out
}

// Pens groups the report's rows by pen, in the order the farm reads them.
func (r FeedProofTimesReport) Pens() []PenProofTimes {
	byPen := map[string]*PenProofTimes{}
	for _, row := range r.Rows {
		pen := row.PenDisplay()
		if _, ok := byPen[pen]; !ok {
			byPen[pen] = &PenProofTimes{Pen: pen, Sessions: map[int]PenSessionProofTimes{}}
		}
		byPen[pen].Sessions[row.SessionNo] = row
	}
	out := make([]PenProofTimes, 0, len(byPen))
	for _, pen := range byPen {
		out = append(out, *pen)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pen < out[j].Pen })
	return out
}

// MissingPenCount is the number of PENS short of at least one capture anywhere in their day.
func (r FeedProofTimesReport) MissingPenCount() int {
	sessions := r.SessionNumbers()
	missing := 0
	for _, pen := range r.Pens() {
		if !pen.Complete(sessions) {
			missing++
		}
	}
	return missing
}

// sessionColumnWidth is "HH:MM HH:MM HH:MM" -- three 5-character times, two single spaces.
const sessionColumnWidth = 17

// FeedProofTimesSlackBody renders the report as Slack message text.
//
// ONE ROW PER PEN, with each session's three captures side by side under its own heading
// (maintainer decision, 2026-09-05, after seeing the pen-session layout on real data). A park runs
// ~60 pens twice a day, so a row per pen-session is a 122-line wall in which the pen name is
// repeated throughout; this halves it and lets the eye run down one pen's whole day.
//
// THE TABLE IS A ```diff BLOCK, and that is the only way Slack renders actual RED. Slack has no
// inline text colour: a message can bold, italicise or code-span, but it cannot colour a word. A
// fenced block tagged `diff` is syntax-highlighted, and in that grammar a line beginning with '-'
// is a deletion and renders red. So a pen missing any capture is written as a '-' line and arrives
// red; a complete one is written with a leading space and stays plain. The monospace font of the
// block is what keeps the columns aligned, which a Block Kit layout cannot promise.
//
// Every complete row therefore MUST start with a space, and every incomplete row MUST start with
// '-'. Nothing else may begin a line -- a '+' would render green and a '#' would render as a
// comment, either of which would silently say something the farm did not mean.
//
// THE BODY DOES NOT REPEAT THE TITLE. The gateway posts `Title + "\n" + Body` for every Slack
// request (sendSlack), so a body that opened with its own heading would show it twice.
func FeedProofTimesSlackBody(report FeedProofTimesReport, loc *time.Location) string {
	if loc == nil {
		loc = biztime.DefaultLocation()
	}
	pens := report.Pens()

	var b strings.Builder
	b.WriteString("Times are when each capture reached the backend (IST).\n")
	if len(pens) == 0 {
		// No sheet, no pens: say so plainly rather than posting an empty table, which reads as a
		// broken report rather than as "nothing was planned".
		b.WriteString("\nNo feed sheet was issued for this park today, so no pen owes captures.")
		return b.String()
	}

	sessions := report.SessionNumbers()
	penWidth := len("PEN")
	for _, pen := range pens {
		if n := len(pen.Pen); n > penWidth {
			penWidth = n
		}
	}

	b.WriteString("```diff\n")
	// Two header lines: the session names above, the capture names below, so "WT FEED WATER" is not
	// repeated with no indication of which feeding it belongs to.
	sessionHeader := make([]string, 0, len(sessions))
	captureHeader := make([]string, 0, len(sessions))
	rule := make([]string, 0, len(sessions))
	for _, sessionNo := range sessions {
		sessionHeader = append(sessionHeader, pad(strings.ToUpper(sessionHeadingFor(report, sessionNo))))
		captureHeader = append(captureHeader, pad("WT    FEED  WATER"))
		rule = append(rule, strings.Repeat("-", sessionColumnWidth))
	}
	writeRow(&b, " ", penWidth, "", sessionHeader)
	writeRow(&b, " ", penWidth, "PEN", captureHeader)
	b.WriteString(fmt.Sprintf("  %s-+-%s\n", strings.Repeat("-", penWidth), strings.Join(rule, "-+-")))

	for _, pen := range pens {
		marker := " "
		if !pen.Complete(sessions) {
			marker = "-"
		}
		cells := make([]string, 0, len(sessions))
		for _, sessionNo := range sessions {
			cells = append(cells, sessionCell(pen.Sessions[sessionNo], loc))
		}
		writeRow(&b, marker, penWidth, pen.Pen, cells)
	}
	b.WriteString("```\n")

	missing := report.MissingPenCount()
	if missing == 0 {
		fmt.Fprintf(&b, "All %s complete.", pluralPens(len(pens)))
	} else {
		fmt.Fprintf(&b, "%d of %s missing a capture.", missing, pluralPens(len(pens)))
	}
	return b.String()
}

// writeRow emits one table line. Trailing blanks are trimmed so a row ending in "--" does not carry
// invisible padding; the leading marker and the inner padding keep the columns aligned.
func writeRow(b *strings.Builder, marker string, penWidth int, pen string, cells []string) {
	line := fmt.Sprintf("%s %-*s | %s", marker, penWidth, pen, strings.Join(cells, " | "))
	fmt.Fprintln(b, strings.TrimRight(line, " "))
}

func pad(s string) string { return fmt.Sprintf("%-*s", sessionColumnWidth, s) }

// sessionCell is one session's three times. A session the pen never submitted renders as three
// gaps -- the same as a submitted session whose captures never arrived, because from the farm's
// side they are the same fact: there is nothing to watch.
func sessionCell(row PenSessionProofTimes, loc *time.Location) string {
	return pad(fmt.Sprintf("%-5s %-5s %-5s",
		captureTime(row.Weight, loc), captureTime(row.Distribution, loc), captureTime(row.Water, loc)))
}

// sessionHeadingFor prefers the sheet's own wording ("Morning") over the number.
func sessionHeadingFor(report FeedProofTimesReport, sessionNo int) string {
	for _, row := range report.Rows {
		if row.SessionNo == sessionNo {
			if label := strings.TrimSpace(row.SessionLabel); label != "" {
				return label
			}
		}
	}
	return fmt.Sprintf("SESSION %d", sessionNo)
}

func captureTime(capture FeedProofCapture, loc *time.Location) string {
	if !capture.Recorded() {
		return feedProofTimeMissing
	}
	return capture.UploadedAt.In(loc).Format("15:04")
}

func pluralPens(n int) string {
	if n == 1 {
		return "1 pen"
	}
	return fmt.Sprintf("%d pens", n)
}
