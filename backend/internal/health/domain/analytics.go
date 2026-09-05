package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Health Analytics is the Health vertical's leadership read: what the herd is
// sick with, whether the prescribed treatment is actually being carried out,
// and what it is dying of.
//
// THE THIRD QUESTION HAS A HOLE IN IT, AND THIS FILE IS WHERE THAT IS RECORDED.
// Nothing in Goat OS stores a CODED cause of death. The death workflow captures
// a written account ("what happened", 3-500 characters), a death video and a
// post-mortem video, and then exits the animal as dead/died -- `exit_reason` is
// the MANNER of exit (sold, died, culled, transferred, lost), never a diagnosis.
// The post-mortem is filmed and never read back into a field.
//
// So mortality is attributed the only honest way the schema allows: an animal
// that died while a health case was OPEN has that case held at
// 'held_death_review' and closed at 'closed_dead', which names the disease it
// was being treated for. An animal that died with no open case is reported as
// UNATTRIBUTED and is never assigned a disease. That unattributed share is not
// missing data to be tidied away -- it measures how much of the herd's mortality
// the health system never saw coming, and it is a headline figure here.
//
// TIME GRAIN IS THE INDIA BUSINESS DAY, and the flow series buckets by
// `Asia/Kolkata` calendar MONTH. Never a UTC instant, never a rolling 30-day
// slice: a figure the farm reads as "August" is the month the farm worked.
//
// The page derives NO number of its own. Every figure below is computed
// server-side over the WHOLE filtered window; a client re-summing the months to
// get a total would be the banned read-time rollup.

// ---------------------------------------------------------------------------
// Payload
// ---------------------------------------------------------------------------

// HealthAnalyticsTotals are whole-window rollups plus the current open-case
// census. They are computed over the full window server-side and must never be
// re-derived from the returned months.
type HealthAnalyticsTotals struct {
	// OpenCases, OpenAdults and OpenKids are a census AS OF NOW, not a window
	// figure -- the page's own copy says so. OpenAdults + OpenKids always equals
	// OpenCases exactly, because `health_cases.age_band` is NOT NULL and
	// constrained to the two values.
	OpenCases   int64 `json:"open_cases"`
	OpenAdults  int64 `json:"open_adults"`
	OpenKids    int64 `json:"open_kids"`
	NewCases    int64 `json:"new_cases"`
	ClosedCases int64 `json:"closed_cases"`
	Recovered   int64 `json:"recovered"`

	// Deaths is every animal that exited as died inside the window.
	//
	// DeathsAttributed and DeathsUnattributed are DISJOINT and sum to Deaths:
	// an animal either had a case open when it died or it did not.
	// DeathsNeverDiagnosed is a SUBSET of DeathsUnattributed (an animal that has
	// never had a case at all, as against one whose last case closed before it
	// died) and is deliberately reported separately rather than as a third
	// bucket, because adding it to the other two would double-count.
	Deaths               int64 `json:"deaths"`
	DeathsAttributed     int64 `json:"deaths_attributed"`
	DeathsUnattributed   int64 `json:"deaths_unattributed"`
	DeathsNeverDiagnosed int64 `json:"deaths_never_diagnosed"`
}

// HealthAnalyticsMonth is one India-calendar month of clinical flow.
type HealthAnalyticsMonth struct {
	// Month is the IST calendar month key, "2026-08".
	Month string `json:"month"`
	// Label is the farm-readable month, "Aug 2026".
	Label    string `json:"label"`
	NewCases int64  `json:"new_cases"`
	// Deaths always equals DeathsAttributed + DeathsUnattributed.
	Deaths             int64 `json:"deaths"`
	DeathsAttributed   int64 `json:"deaths_attributed"`
	DeathsUnattributed int64 `json:"deaths_unattributed"`
}

// HealthAnalyticsDisease is one row of the disease board.
//
// GRAIN IS THE CASE, never the animal and never the treatment session. One
// animal treated twice for the same illness is two cases: counting animals
// hides a relapse, and counting sessions multiplies every disease by the length
// of its course.
//
// It is keyed on the REGISTER RULE (`health_cases.register_rule_id`), not on
// `disease_key`. Those are different facts and the schema keeps them apart on
// purpose: disease_key names the treatment CARD and the mapping is many-to-one
// (PPR, POX and UNDIFFERENTIATED all route to the 'supportive' card), so a card
// key cannot say which illness was named. A case opened through the pre-engine
// direct-pick route carries no rule id; those fall back to the disease key so
// they are still counted, and Key says which of the two it is.
type HealthAnalyticsDisease struct {
	// Key is the register rule id ("MASTITIS") when the case came from the
	// diagnosis engine, else the disease key of the treatment card it was opened
	// against.
	Key string `json:"key"`
	// KeyKind is "register_rule" or "disease_key" -- the client renders no
	// arithmetic on it, but a reader comparing two rows is owed the difference.
	KeyKind string `json:"key_kind"`
	// Label is the farm-readable disease name recorded on the case.
	Label string `json:"label"`
	// AgeBands is "adult", "kid" or "both", from the cases actually counted.
	AgeBands  string `json:"age_bands"`
	NewCases  int64  `json:"new_cases"`
	OpenCases int64  `json:"open_cases"`
	Recovered int64  `json:"recovered"`
	Died      int64  `json:"died"`
	// CaseFatalityPct is Died over NewCases for THIS disease, one decimal place.
	// It is only ever computed per disease: a herd-wide "case fatality" that
	// mixes flystrike with milk fever is a number about nothing.
	CaseFatalityPct float64 `json:"case_fatality_pct"`
}

// HealthAnalyticsAdherence is the treatment execution split.
//
// GRAIN IS THE SESSION: one day-and-session of one animal's course.
//
// OnTime, Late, Rework and NotDone are DISJOINT and sum to SessionsDue. "Late"
// is kept apart from "not done" deliberately -- they are different failures,
// and collapsing them into one number hides a crew that is working but working
// late.
//
// SessionsDue counts sessions whose business date falls inside the window AND is
// not in the future: a session due tomorrow has not been missed, and counting it
// as not-done would report every forward-dated course as a failure.
//
// EXECUTION AND VERIFICATION ARE SEPARATE AXES and are not mixed into one set of
// buckets. AwaitingVerification is therefore a SUBSET of OnTime + Late (the
// operator did the work; the verifier has not yet watched the video), reported
// beside them rather than as a fifth bucket -- as a bucket it would steal from
// OnTime and make an on-time crew read as late.
type HealthAnalyticsAdherence struct {
	SessionsDue int64 `json:"sessions_due"`
	// OnTime is completed on or before the session's own business date.
	OnTime int64 `json:"on_time"`
	// Late is completed, but after that date.
	Late int64 `json:"late"`
	// Rework is a completion the verifier sent back for a re-shoot. It is not
	// currently complete, which is why it is its own bucket and not a subset.
	Rework int64 `json:"rework"`
	// NotDone is a due session with no completion at all. Sessions cancelled by a
	// clinical closure, cancelled by the animal's death, or held by the death
	// review are excluded from every bucket AND from SessionsDue -- work the farm
	// was told to stop doing is not work it failed to do.
	NotDone int64 `json:"not_done"`
	// AwaitingVerification is a SUBSET of OnTime + Late, never added to them.
	AwaitingVerification int64 `json:"awaiting_verification"`
	// OnTimePct is OnTime over SessionsDue, one decimal place.
	OnTimePct float64 `json:"on_time_pct"`
}

// HealthAnalyticsMedicine is one medicine actually administered, off the
// operator's own completed step -- never off the authored protocol, which says
// what SHOULD have been given.
type HealthAnalyticsMedicine struct {
	Name  string `json:"name"`
	Route string `json:"route"`
	Doses int64  `json:"doses"`
	// Animals is the distinct animals that received it, which is a different
	// number from Doses whenever a course runs more than one day.
	Animals int64 `json:"animals"`
}

// HealthAnalyticsEngineRule is one diagnosis rule the engine proposed.
//
// PROPOSED vs OPENED, never "declined". A run's status says whether the whole
// RUN was confirmed or declined; there is no per-problem decline stored
// anywhere, so a per-rule "override rate" would be invented. What IS canonical
// is whether a case carrying that rule id was opened from that run, so the
// gap between the two is reported as work the director did not take up, which
// is the same signal without the fabrication.
type HealthAnalyticsEngineRule struct {
	Key string `json:"key"`
	// Proposed counts RUNS naming this rule in their proposal, never entries in
	// the proposal array: a rule listed twice in one proposal is still one
	// observation of one animal.
	Proposed int64 `json:"proposed"`
	// Opened counts cases actually opened from those runs carrying this rule id.
	Opened int64 `json:"opened"`
	// NotTakenUpPct is (Proposed-Opened) over Proposed, one decimal place.
	NotTakenUpPct float64 `json:"not_taken_up_pct"`
}

// HealthAnalyticsEngine is the diagnosis engine's own quality read -- the
// improvement loop `health_diagnosis_runs` was built for, which stores the
// ticked form, the context it was judged against and the proposal verbatim so
// an override can be read back later.
type HealthAnalyticsEngine struct {
	Observations int64 `json:"observations"`
	Confirmed    int64 `json:"confirmed"`
	Declined     int64 `json:"declined"`
	// Pending is a run nobody has decided yet.
	Pending int64 `json:"pending"`
	// Superseded is a run replaced by a later observation of the same animal. It
	// is EXCLUDED from ConfirmedPct's denominator: a second look is not a
	// director disagreeing with the engine.
	Superseded int64 `json:"superseded"`
	// Invalid is a run the engine itself refused (`valid=false`) -- an
	// observation that did not satisfy any rule. It is counted so the total
	// reconciles rather than leaving a silent remainder.
	Invalid      int64   `json:"invalid"`
	ConfirmedPct float64 `json:"confirmed_pct"`
	// MedianHoursToConfirm is observed -> confirmed, in whole hours. Nil when no
	// run in the window was confirmed; a zero would read as instant.
	MedianHoursToConfirm *int64                      `json:"median_hours_to_confirm"`
	Rules                []HealthAnalyticsEngineRule `json:"rules"`
}

// HealthAnalyticsDeath is one animal that died inside the window.
//
// The list is BOUNDED to HealthAnalyticsDeathListLimit rows, most recent first,
// and the page says so. It is an evidence trail beside the counts, not a
// register: the counts above it are whole-window aggregates and do not change
// with this bound.
type HealthAnalyticsDeath struct {
	GoatID    string `json:"goat_id"`
	DisplayID string `json:"display_id"`
	// Tag is the animal's RFID/eartag as the farm reads it. Empty when the
	// animal carries no identifier, which is a data gap and is rendered as one.
	Tag string `json:"tag"`
	// OperationalLocationDisplay is the composed pen name, from the canonical
	// oploc resolver -- never a bare shed name and never a raw partition key.
	OperationalLocationDisplay string `json:"operational_location_display"`
	ParkLabel                  string `json:"park_label"`
	// BusinessDate is the IST date the animal exited.
	BusinessDate string `json:"business_date"`
	AgeBand      string `json:"age_band"`
	// Attribution is "attributed" or "unattributed".
	Attribution string `json:"attribution"`
	// DiseaseLabel is the disease it was under treatment for, and is empty for
	// an unattributed death. The client renders its own contract copy for the
	// empty case rather than composing a sentence here.
	DiseaseLabel string `json:"disease_label"`
	// NeverDiagnosed marks an animal that never had a case at all, as against
	// one whose case had already closed.
	NeverDiagnosed bool `json:"never_diagnosed"`
	// CauseRecorded distinguishes a disease the operator NAMED on the death form from one
	// merely INFERRED because a case happened to be open when the animal died. Both read as
	// attributed, and they are not the same claim: the first is causation as the farm
	// recorded it, the second is co-incidence and is all that was available before causes
	// existed. A reader must never be shown a guess and a recorded fact as if they were
	// alike, so the row carries which one it is and the screen says so.
	CauseRecorded bool `json:"cause_recorded"`
	// DaysUnderTreatment is the case start to the death date, in whole days.
	// Nil for an unattributed death.
	DaysUnderTreatment *int64 `json:"days_under_treatment"`
}

// HealthAnalytics is the whole page payload, already scoped to the requested
// park and window.
type HealthAnalytics struct {
	// WindowFrom and WindowTo are inclusive IST business dates.
	WindowFrom  string                    `json:"window_from"`
	WindowTo    string                    `json:"window_to"`
	Totals      HealthAnalyticsTotals     `json:"totals"`
	Months      []HealthAnalyticsMonth    `json:"months"`
	Diseases    []HealthAnalyticsDisease  `json:"diseases"`
	Adherence   HealthAnalyticsAdherence  `json:"adherence"`
	Medicines   []HealthAnalyticsMedicine `json:"medicines"`
	Engine      HealthAnalyticsEngine     `json:"engine"`
	Deaths      []HealthAnalyticsDeath    `json:"deaths"`
	GeneratedAt time.Time                 `json:"generated_at"`
}

// HealthAnalyticsQuery scopes the read. Park comes from the top-bar scope, not
// from a control on the page (Scope Chrome Rule); the page body owns only the
// window.
type HealthAnalyticsQuery struct {
	TenantID string
	ParkID   *string
	// FromDate and ToDate are inclusive "YYYY-MM-DD" bounds.
	FromDate string
	ToDate   string
}

// ---------------------------------------------------------------------------
// Window rules
//
// Pure calendar rules with no I/O, so they live here rather than in either
// adapter: the HTTP layer validates a caller's window with the SAME functions
// the repository resolves it with, and two hand-rolled copies cannot drift on
// what counts as a day.
// ---------------------------------------------------------------------------

// HealthAnalyticsDateLayout is the wire format of a window bound.
const HealthAnalyticsDateLayout = "2006-01-02"

const (
	// HealthAnalyticsDefaultMonths is the default window: six months back from
	// the first of the current month, through today. Six rather than the twelve
	// Herd Analytics opens on, because a clinical picture goes stale faster than
	// a herd-size one -- what the farm was treating a year ago says little about
	// what is in the pens this week.
	HealthAnalyticsDefaultMonths = 6
	// HealthAnalyticsMaxDays is the widest window the read serves. A longer one
	// is REJECTED rather than silently trimmed: a director who asked for three
	// years must not be shown two and told nothing.
	HealthAnalyticsMaxDays = 1150
	// HealthAnalyticsFloorDate is the earliest day the DEFAULT window opens on.
	// The Health module's own history in Goat OS starts here, so a default
	// reaching further back pads the charts with empty months that read as a
	// herd with nothing wrong with it. The floor binds the DEFAULT only -- a
	// director who names an earlier window gets exactly that window, and the
	// honest empty months with it.
	HealthAnalyticsFloorDate = "2026-08-01"
	// HealthAnalyticsDeathListLimit bounds the per-animal death list. The counts
	// above it are whole-window aggregates and are unaffected by this bound.
	HealthAnalyticsDeathListLimit = 50
	// HealthAnalyticsDiseaseLimit bounds the disease board. The registers carry
	// 34 adult rules and 27 per kid class; a window in which more than this many
	// distinct diseases were diagnosed is reported down to the busiest, which
	// the page's own copy states.
	HealthAnalyticsDiseaseLimit = 25
	// HealthAnalyticsMedicineLimit bounds the medicine table, most-given first.
	HealthAnalyticsMedicineLimit = 15
	// HealthAnalyticsEngineRuleLimit bounds the per-rule engine table, most-
	// proposed first.
	HealthAnalyticsEngineRuleLimit = 12
)

// ParseHealthAnalyticsDate parses an inclusive "YYYY-MM-DD" bound into the first
// instant of that IST day.
func ParseHealthAnalyticsDate(raw string) (time.Time, error) {
	parsed, err := time.ParseInLocation(HealthAnalyticsDateLayout, strings.TrimSpace(raw), biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("health: %q is not a YYYY-MM-DD date", raw)
	}
	return parsed, nil
}

// HealthAnalyticsDefaultWindow returns the default inclusive bounds: the first
// day of the month five back, through today -- never earlier than
// HealthAnalyticsFloorDate.
//
// It starts on a month BOUNDARY because the flow chart buckets by business
// month: an arbitrary start day would open the default view with a half-empty
// first column that reads as a collapse in cases rather than a window edge.
func HealthAnalyticsDefaultWindow(now time.Time) (from string, to string) {
	ist := biztime.DefaultLocation()
	today := now.In(ist)
	firstOfMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, ist)
	from = firstOfMonth.AddDate(0, -(HealthAnalyticsDefaultMonths - 1), 0).Format(HealthAnalyticsDateLayout)
	// "YYYY-MM-DD" orders lexicographically, so the clamp is a string compare.
	if from < HealthAnalyticsFloorDate {
		from = HealthAnalyticsFloorDate
	}
	return from, today.Format(HealthAnalyticsDateLayout)
}

// ResolveHealthAnalyticsWindow validates and resolves the inclusive window.
//
// BOTH ABSENT means the default window. Anything else is validated and REJECTED
// on failure -- a malformed date, only one of the pair, a reversed range, or a
// span wider than HealthAnalyticsMaxDays. None of those is quietly rewritten: a
// director who names a window must see that window or an error, never a
// different window under the label they chose.
func ResolveHealthAnalyticsWindow(rawFrom, rawTo string, now time.Time) (string, string, error) {
	from := strings.TrimSpace(rawFrom)
	to := strings.TrimSpace(rawTo)
	if from == "" && to == "" {
		defaultFrom, defaultTo := HealthAnalyticsDefaultWindow(now)
		return defaultFrom, defaultTo, nil
	}
	if from == "" || to == "" {
		return "", "", fmt.Errorf("from and to must be given together as YYYY-MM-DD dates")
	}
	fromDate, err := ParseHealthAnalyticsDate(from)
	if err != nil {
		return "", "", fmt.Errorf("from must be a YYYY-MM-DD date")
	}
	toDate, err := ParseHealthAnalyticsDate(to)
	if err != nil {
		return "", "", fmt.Errorf("to must be a YYYY-MM-DD date")
	}
	if toDate.Before(fromDate) {
		return "", "", fmt.Errorf("to must not be earlier than from")
	}
	if days := int(toDate.Sub(fromDate).Hours()/24) + 1; days > HealthAnalyticsMaxDays {
		return "", "", fmt.Errorf("the window must not be longer than %d days", HealthAnalyticsMaxDays)
	}
	return fromDate.Format(HealthAnalyticsDateLayout), toDate.Format(HealthAnalyticsDateLayout), nil
}

// ---------------------------------------------------------------------------
// Derived figures
//
// Every rate on this page is computed HERE, once, so the HTTP layer, the
// repository and any future consumer cannot each round it their own way.
// ---------------------------------------------------------------------------

// HealthAnalyticsPct is share of whole as a percentage to one decimal place.
// A zero denominator returns 0 rather than NaN: no cases means no rate, and a
// NaN would serialize to null and render as a broken cell.
func HealthAnalyticsPct(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	// Rounded to one decimal by construction so the wire value and the rendered
	// value are the same number -- a client rounding a long float would show a
	// figure that does not match a CSV export of the same field.
	return float64(int64(float64(part)/float64(whole)*1000+0.5)) / 10
}

// AgeBandSummary folds the age bands a set of cases actually carried into the
// one word the board shows. It takes the two counts rather than a slice so a
// caller cannot pass a partially-filtered set by accident.
func AgeBandSummary(adults, kids int64) string {
	switch {
	case adults > 0 && kids > 0:
		return "both"
	case kids > 0:
		return "kid"
	case adults > 0:
		return "adult"
	default:
		return ""
	}
}
