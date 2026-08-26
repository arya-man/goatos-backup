package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/toxin/app"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// The /feed/toxin report transport. Every visible string on that screen is composed here
// or in domain/report.go and rendered verbatim by admin-web: chip labels, result chips,
// the KPI captions, the alert line, the empty copy. The page derives no business text of
// its own — see the copy-firewall rule in AGENTS.md.

type reportFilterPayload struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Count        int    `json:"count"`
	Selected     bool   `json:"selected"`
	EmptyMessage string `json:"empty_message"`
}

type reportLoadPayload struct {
	FeedPurchaseID string `json:"feed_purchase_id"`
	TaskID         string `json:"task_id"`
	RoundNo        int    `json:"round_no"`
	FeedItemLabel  string `json:"feed_item_label"`
	// BatchLabel is the farm-readable load line under the feed name ("Batch 4 · CPT").
	BatchLabel     string  `json:"batch_label"`
	Vendor         string  `json:"vendor"`
	FarmLabel      string  `json:"farm_label"`
	PurchaseDate   string  `json:"purchase_date"`
	QuantityKg     float64 `json:"quantity_kg"`
	QuantityLabel  string  `json:"quantity_label"`
	TestedByName   string  `json:"tested_by_name"`
	TestedByLabel  string  `json:"tested_by_label"`
	SubmittedAt    string  `json:"submitted_at"`
	ResultLabel    string  `json:"result_label"`
	ResultTone     string  `json:"result_tone"`
	TurnaroundText string  `json:"turnaround_label"`
	Bucket         string  `json:"bucket"`
}

type reportSummaryPayload struct {
	LoadsReceived      int    `json:"loads_received"`
	LoadsReceivedNote  string `json:"loads_received_note"`
	LoadsTested        int    `json:"loads_tested"`
	LoadsTestedNote    string `json:"loads_tested_note"`
	NeedsAttention     int    `json:"needs_attention"`
	NeedsAttentionNote string `json:"needs_attention_note"`
	Waiting            int    `json:"waiting"`
	WaitingNote        string `json:"waiting_note"`
	WindowLabel        string `json:"window_label"`
	// AlertMessage is the red banner, blank when nothing is flagged. Backend-owned because
	// it names a specific load and a specific consequence.
	AlertMessage string `json:"alert_message"`
}

type reportWeekPayload struct {
	WeekStart string `json:"week_start"`
	Label     string `json:"label"`
	Received  int    `json:"received"`
	Tested    int    `json:"tested"`
}

type reportMixSlicePayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Tone  string `json:"tone"`
}

type reportVendorPayload struct {
	Vendor     string `json:"vendor"`
	Loads      int    `json:"loads"`
	Flagged    int    `json:"flagged"`
	ShareLabel string `json:"share_label"`
	Tone       string `json:"tone"`
}

type reportPayload struct {
	Filters    []reportFilterPayload   `json:"filters"`
	Loads      []reportLoadPayload     `json:"loads"`
	NextCursor string                  `json:"next_cursor"`
	Summary    reportSummaryPayload    `json:"summary"`
	Weeks      []reportWeekPayload     `json:"weeks"`
	Mix        []reportMixSlicePayload `json:"outcome_mix"`
	Vendors    []reportVendorPayload   `json:"vendors"`
	// EmptyMessage is the selected chip's own empty line — never a shared one, because
	// "no loads recorded" is a lie under Flagged, where empty is good news.
	EmptyMessage string `json:"empty_message"`
}

// LoadReport serves GET /feed/toxin/reports.
func (h *Handler) LoadReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.ReportFilterKeyOrDefault(strings.TrimSpace(q.Get("filter")))
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}

	page, err := h.service.LoadReport(r.Context(), tenantID(r), filter, limit, strings.TrimSpace(q.Get("cursor")))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toReportPayload(page, filter))
}

func toReportPayload(page ports.ReportPage, selected string) reportPayload {
	out := reportPayload{
		Filters:    make([]reportFilterPayload, 0, 5),
		Loads:      make([]reportLoadPayload, 0, len(page.Loads)),
		NextCursor: page.NextCursor,
		Weeks:      make([]reportWeekPayload, 0, len(page.Weeks)),
		Vendors:    make([]reportVendorPayload, 0, len(page.Vendors)),
	}
	for _, f := range domain.ReportFilters() {
		if f.Key == selected {
			out.EmptyMessage = f.EmptyMessage
		}
		out.Filters = append(out.Filters, reportFilterPayload{
			Key: f.Key, Label: f.Label, Count: page.FilterCounts[f.Key],
			Selected: f.Key == selected, EmptyMessage: f.EmptyMessage,
		})
	}
	for _, l := range page.Loads {
		out.Loads = append(out.Loads, toReportLoadPayload(l))
	}
	for _, w := range page.Weeks {
		out.Weeks = append(out.Weeks, reportWeekPayload{
			WeekStart: w.WeekStart, Label: weekLabel(w.WeekStart), Received: w.Received, Tested: w.Tested,
		})
	}
	for _, v := range page.Vendors {
		out.Vendors = append(out.Vendors, reportVendorPayload{
			Vendor: v.Vendor, Loads: v.Loads, Flagged: v.Flagged,
			ShareLabel: fmt.Sprintf("%d of %d", v.Flagged, v.Loads),
			Tone:       vendorTone(v),
		})
	}
	out.Summary = toReportSummaryPayload(page)
	out.Mix = toReportMixPayload(page.Mix)
	return out
}

func toReportLoadPayload(l ports.ReportLoad) reportLoadPayload {
	p := reportLoadPayload{
		FeedPurchaseID: l.FeedPurchaseID,
		TaskID:         l.TaskID,
		RoundNo:        l.RoundNo,
		FeedItemLabel:  l.FeedItemLabel,
		Vendor:         l.Vendor,
		FarmLabel:      l.FarmLabel,
		PurchaseDate:   l.PurchaseDate,
		QuantityKg:     l.QuantityKg,
		QuantityLabel:  quantityLabel(l.QuantityKg),
		TestedByName:   l.TestedByName,
		SubmittedAt:    l.SubmittedAt,
		ResultLabel:    domain.ReportResultLabel(l.Status, l.Outcome, l.Origin),
		ResultTone:     domain.ReportResultTone(l.Status, l.Outcome),
		Bucket:         domain.ReportBucketFor(l.Status, l.Outcome),
	}
	p.BatchLabel = fmt.Sprintf("Batch %d · %s", l.BatchNo, l.FarmLabel)
	if l.RoundNo > 1 {
		p.BatchLabel += fmt.Sprintf(" · test %d", l.RoundNo)
	}
	// An unresolvable person is NOT printed as a uuid: the row says nobody has tested it
	// yet, which is what the reader can act on.
	if l.TestedByName == "" {
		// "Nobody yet", not "Not tested yet": the Result column one cell over already says
		// that, and two adjacent cells repeating the same sentence reads as a rendering bug.
		// This column answers WHO; that one answers WHAT the strip said.
		p.TestedByLabel = "Nobody yet"
	} else {
		p.TestedByLabel = l.TestedByName
	}
	p.TurnaroundText = turnaroundLabel(l)
	return p
}

// turnaroundLabel answers "how long did this take", or "how long has this been sitting"
// for a load nobody has tested. The two are different questions and must not share copy.
func turnaroundLabel(l ports.ReportLoad) string {
	if l.SubmittedAt == "" {
		switch {
		case l.WaitingDays <= 0:
			return "Arrived today"
		case l.WaitingDays == 1:
			return "Waiting 1 day"
		default:
			return fmt.Sprintf("Waiting %d days", l.WaitingDays)
		}
	}
	switch {
	case l.TurnaroundMinutes < 60:
		return fmt.Sprintf("%d min", l.TurnaroundMinutes)
	case l.TurnaroundMinutes < 24*60:
		return fmt.Sprintf("%dh %02dm", l.TurnaroundMinutes/60, l.TurnaroundMinutes%60)
	default:
		return fmt.Sprintf("%d days", l.TurnaroundMinutes/(24*60))
	}
}

func quantityLabel(kg float64) string {
	return fmt.Sprintf("%s kg", trimFloat(kg))
}

func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func toReportSummaryPayload(page ports.ReportPage) reportSummaryPayload {
	s := page.Summary
	out := reportSummaryPayload{
		LoadsReceived:  s.LoadsReceived,
		LoadsTested:    s.LoadsTested,
		NeedsAttention: s.NeedsAttention,
		Waiting:        s.Waiting,
		WindowLabel:    "Last 30 days",
	}
	out.LoadsReceivedNote = fmt.Sprintf("%s · %s", pluralise(s.FeedTypes, "feed type", "feed types"), pluralise(s.Parks, "park", "parks"))
	if s.LoadsReceived > 0 {
		out.LoadsTestedNote = fmt.Sprintf("%d%% of loads screened", s.LoadsTested*100/s.LoadsReceived)
	} else {
		out.LoadsTestedNote = "No loads yet"
	}
	switch s.NeedsAttention {
	case 0:
		out.NeedsAttentionNote = "Nothing flagged"
	case 1:
		out.NeedsAttentionNote = "1 load to deal with"
	default:
		out.NeedsAttentionNote = fmt.Sprintf("%d loads to deal with", s.NeedsAttention)
	}
	switch {
	case s.Waiting == 0:
		out.WaitingNote = "Everything is tested"
	case s.OldestWaitingDays <= 0:
		out.WaitingNote = "All arrived today"
	default:
		out.WaitingNote = fmt.Sprintf("Oldest waiting %s", pluralise(s.OldestWaitingDays, "day", "days"))
		if s.OldestWaitingLabel != "" {
			out.WaitingNote += " · " + s.OldestWaitingLabel
		}
	}
	if s.NeedsAttention > 0 {
		out.AlertMessage = fmt.Sprintf(
			"%s came back positive or unusable. Send a sample for lab confirmation before the feed is issued.",
			pluralise(s.NeedsAttention, "load", "loads"))
	}
	return out
}

func pluralise(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func toReportMixPayload(m ports.ReportOutcomeMix) []reportMixSlicePayload {
	return []reportMixSlicePayload{
		{Key: domain.OutcomeNegative, Label: "Negative", Count: m.Negative, Tone: "ok"},
		{Key: domain.OutcomePositive, Label: "Positive", Count: m.Positive, Tone: "danger"},
		{Key: domain.OutcomeInvalid, Label: "Strip was void", Count: m.Invalid, Tone: "warn"},
		{Key: "untested", Label: "Not tested yet", Count: m.Untested, Tone: "muted"},
	}
}

// vendorTone keeps the supplier bars honest: a clean record is not painted as a warning
// just because it appears in a list titled "suppliers to watch".
func vendorTone(v ports.ReportVendor) string {
	switch {
	case v.Flagged == 0:
		return "ok"
	case v.Loads > 0 && v.Flagged*100/v.Loads >= 20:
		return "danger"
	default:
		return "warn"
	}
}

var monthNames = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// weekLabel turns 2026-08-24 into "24 Aug" for the bar axis. Dates arrive as business
// DATE strings already resolved in Asia/Kolkata; this only formats, never re-zones.
func weekLabel(businessDate string) string {
	parts := strings.Split(businessDate, "-")
	if len(parts) != 3 {
		return businessDate
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return businessDate
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil {
		return businessDate
	}
	return fmt.Sprintf("%d %s", day, monthNames[month-1])
}
