package readtools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// scopedReader reads real data for a tool, honoring the sub-question's
// advertised scope params (park_label, species, shed_id, dimension, …) and the
// as-of business date (params["as_of"], injected by the orchestrator from
// Question.AsOf). Passing params through — instead of dropping everything but
// tenantID — is what lets "how many goats in Castro 1" and "counts as of
// yesterday" actually scope/back-date the read (P1-4).
type scopedReader func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error)

// stampTenant sets Fact.TenantID on every fact from the session actor. It is
// the ONLY place the API-tier executors assign a tenant to a fact: whatever a
// reader put there (or left empty) is overwritten, so a tenant id can never be
// sourced from a result row. The composer/cache/chart gate
// (app.validateFactTenants) then rejects any fact whose TenantID is not the
// actor's — every executor's success path must go through this helper
// (TestReadersRequireTenant / TestFactTenantSetFromActorNotResult).
func stampTenant(actor domain.Actor, facts []domain.Fact) []domain.Fact {
	out := make([]domain.Fact, len(facts))
	for i, f := range facts {
		f.TenantID = actor.TenantID
		out[i] = f
	}
	return out
}

// countsBreakdownExecutor provides animal counts broken down by dimensions.
// It calls the real counts service to return actual data from the database.
type countsBreakdownExecutor struct {
	// countsBySpeciesReader provides counts broken down by species/park/shed/
	// partition/breed/sex/stage. In the bootstrap wiring, this is set to a
	// closure that calls the counts service (ceo_ai.animal_current_scope,
	// fixed by migration 000110 to carry partition_label per goat).
	countsBySpeciesReader scopedReader
}

func (e *countsBreakdownExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:  "counts_breakdown",
		Route: domain.RouteAPI,
		// The catalog must advertise only what buildCountsReader honours
		// (fix/ceo-ai-planner-first-routing): it used to promise a breakdown
		// "by park, pen, breed, sex, stage, or other dimensions" and to take
		// `dimension` and `species`, which the reader ignored, so the planner
		// sent "how many sheep in Channapatna" here and got the whole park's
		// total plus a fixed per-pen table.
		Description: "Live herd total, kids/adults age bands and a fixed top-rows per-pen breakdown by stage, breed and sex. Filters narrow it (park_label, shed_id, partition_label, stage, breed, sex); group_by accepts only \"species\" (goat vs sheep totals). Not a general grouping tool.",
		// partition_label is advertised and honored: buildCountsReader
		// (backend/internal/bootstrap/ceoai_readers.go) filters returned rows
		// to the named partition and renders every scope through
		// oploc.OperationalLocation.Display(), so "at Castro 1" answers with
		// Castro's partition-1 count instead of the whole-shed total. Fixed
		// 2026-08-05 (migration 000110); previously partition scope silently
		// collapsed into the parent shed here.
		Params: []string{"park_label", "shed_id", "partition_label", "stage", "breed", "sex", "group_by"},
	}
}

func (e *countsBreakdownExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	// Call the real counts reader to get actual data.
	if e.countsBySpeciesReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("counts data reader not wired"),
		}, nil
	}

	facts, err := e.countsBySpeciesReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// vaccinationShedSummaryExecutor provides vaccination status by shed.
type vaccinationShedSummaryExecutor struct {
	vaccinationDataReader scopedReader
}

func (e *vaccinationShedSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_shed_summary",
		Route:       domain.RouteAPI,
		Description: "Vaccination status summary by pen (due, completed, overdue)",
		Params:      []string{"park_label", "shed_id"},
	}
}

func (e *vaccinationShedSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.vaccinationDataReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("vaccination data reader not wired"),
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// vaccinationExecutionExecutor provides vaccination execution details.
type vaccinationExecutionExecutor struct {
	vaccinationDataReader scopedReader
}

func (e *vaccinationExecutionExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_execution",
		Route:       domain.RouteAPI,
		Description: "Vaccination execution status and drive details",
		Params:      []string{"park_label", "drive_id"},
	}
}

func (e *vaccinationExecutionExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.vaccinationDataReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("vaccination data reader not wired"),
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// feedDirectionTodayExecutor provides today's feed direction. Its Spec().Name
// ("feed_direction_today") MUST match the tool name the planner routes feed
// questions to (keywordplanner rule) — a mismatch here is what P1-1 fixed:
// registry.Execute looks executors up strictly by name, so a planner tool name
// that doesn't match this Spec silently dead-ends on "no read-service executor".
type feedDirectionTodayExecutor struct {
	feedDataReader scopedReader
}

func (e *feedDirectionTodayExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "feed_direction_today",
		Route:       domain.RouteAPI,
		Description: "Feed direction for today by pen",
		Params:      []string{"park_label", "shed_id", "as_of"},
	}
}

func (e *feedDirectionTodayExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.feedDataReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("feed data reader not wired"),
		}, nil
	}

	facts, err := e.feedDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// feedWeightBandSummaryExecutor provides the Growth Director "Feed by weight
// band" reconciliation. It must stay an API read so Ask Mesha answers the same
// matched/on-farm/include-exited counts as the dashboard, instead of guessing
// from generic feed or weighing SQL.
type feedWeightBandSummaryExecutor struct {
	feedWeightBandReader scopedReader
}

func (e *feedWeightBandSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "feed_weight_band_summary",
		Route:       domain.RouteAPI,
		Description: "Feed by weight band reconciliation: matched feed rollups, not shown, include-exited count, total weighed in period, feed-sheet day",
		Params:      []string{"park_label", "from", "to", "sex", "origin", "weighing_category"},
	}
}

func (e *feedWeightBandSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.feedWeightBandReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("feed weight band reader not wired"),
		}, nil
	}

	facts, err := e.feedWeightBandReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API · Feed by weight band",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// procurementExecutor provides procurement loads (source entry) information.
type procurementExecutor struct {
	procurementDataReader scopedReader
}

func (e *procurementExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "procurement_source_entry_loads",
		Route:       domain.RouteAPI,
		Description: "Procurement source entry loads and their status",
		// park_label is intentionally NOT advertised: procurementdomain.LoadQuery
		// has no park-scoping field at all (not even a park_id), so the closure
		// has nothing to map it onto. Advertising it would silently promise
		// scoping the pipeline cannot honor. Revisit if/when the source-entry
		// load read model gains a park column.
		Params: []string{"status"},
	}
}

// salesOverviewExecutor provides the same closed-deal summary the Sales page
// reads from GET /sales/overview. It is intentionally an API-tier read, not SQL
// fallback, so Ask Mesha reuses the governed sales module contract.
type salesOverviewExecutor struct {
	salesDataReader scopedReader
}

func (e *salesOverviewExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "sales_overview",
		Route:       domain.RouteAPI,
		Description: "Sales overview for closed deals: sold animals, sheep/goat split, revenue, deals, monthly sales",
		// Only what the reader honours (buildSalesOverviewReader reads farm and a
		// calendar month). It used to also advertise time_range and group_by,
		// which the reader ignored: the planner then trusted it for "last 30
		// days" / "per park" questions and got the all-time summary back.
		Params: []string{"farm", "month"},
	}
}

func (e *salesOverviewExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.salesDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("sales overview reader not wired"),
		}, nil
	}

	params := sub.Params
	// A MONTH THE READER CANNOT BIND IS WORSE THAN NO MONTH AT ALL, because
	// the read reports hard zeros for it and the sentence then loses the
	// period and calls them "all recorded sales". Measured live on this
	// branch: "how much revenue did we make in august" planned
	// {month:"August", from:"2026-08-01", to:"2026-08-31"} and answered
	// "Across all recorded sales, 0 animals were sold ... Sales revenue was
	// 0" against an August that made 291,600. The month arrived in ENGLISH
	// and the reader matches its rows on "2006-01", so nothing matched; the
	// bound window said 2026-08 in the very same params and was never read,
	// because the window was only consulted when `month` was ABSENT.
	//
	// The window the server resolved outranks the word the model typed: it is
	// the period the rest of the answer is stated from. So the month is
	// normalised first, and an unbindable one is REPLACED by the bound window
	// rather than passed through.
	bound := monthFromBoundWindow(params)
	raw, present := params["month"].(string)
	switch {
	case !present || strings.TrimSpace(raw) == "":
		if bound != "" {
			params = cloneParams(params)
			params["month"] = bound
		} else if mentionsAMonth(sub.Text) {
			// No resolved window to read: fall back to the calendar month the
			// question is being asked in. This is the ONLY branch that may
			// answer "current", and it is reached only when the server
			// resolved no period at all.
			params = cloneParams(params)
			params["month"] = "current"
		}
	default:
		if normalized := normalizeMonthParam(raw, bound, params); normalized != raw {
			params = cloneParams(params)
			params["month"] = normalized
		}
	}
	facts, err := e.salesDataReader(ctx, actor.TenantID, params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API · Sales overview",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// monthFromBoundWindow reads the calendar month the SERVER already resolved
// for this question, from the from/to business dates the orchestrator threads
// into every sub-question (app.injectWindow). It answers only when both dates
// fall inside ONE calendar month and span it — which is exactly the shape a
// month question resolves to.
//
// It exists because this executor used to bind the month from the question's
// WORDS: any text containing "month" got month="current", so "how much money
// did we make last month" asked for THIS month's row. The window the server
// resolved is the same one the SQL route binds and the same one the answer's
// period is stated from, so reading it here is what makes the two routes
// agree about which month they are talking about.
func monthFromBoundWindow(params map[string]any) string {
	from, _ := params["from"].(string)
	to, _ := params["to"].(string)
	if len(from) != len("2006-01-02") || len(to) != len("2006-01-02") {
		return ""
	}
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return ""
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return ""
	}
	if start.Day() != 1 || end.Year() != start.Year() || end.Month() != start.Month() {
		return ""
	}
	if end.Day() != start.AddDate(0, 1, -1).Day() {
		return ""
	}
	return start.Format("2006-01")
}

// normalizeMonthParam turns whatever the planner put in `month` into the
// "2006-01" the sales read matches its rows on, or into a sentinel the read
// already understands.
//
// Order matters and is the point of the function: the window the SERVER
// resolved wins over the model's word whenever the two can disagree, because
// the window is what every other part of the answer is stated from. An English
// month name is only resolved on its own when there is no bound window to
// prefer, and it takes its year from the request rather than from a guess.
//
// A value it cannot bind at all is returned unchanged, so the read refuses it
// rather than this function inventing a period for it.
func normalizeMonthParam(raw, bound string, params map[string]any) string {
	trimmed := strings.TrimSpace(raw)
	if isMonthToken(trimmed) {
		return trimmed
	}
	if bound != "" {
		return bound
	}
	lower := strings.ToLower(trimmed)
	if lower == "current" || lower == "this_month" || lower == "this month" {
		return trimmed
	}
	if month := monthFromEnglish(trimmed, params); month != "" {
		return month
	}
	return raw
}

// isMonthToken reports the "2006-01" shape the sales read matches on.
func isMonthToken(s string) bool {
	if len(s) != len("2006-01") || s[4] != '-' {
		return false
	}
	if _, err := time.Parse("2006-01", s); err != nil {
		return false
	}
	return true
}

// monthFromEnglish resolves "August", "aug", "August 2026" and "aug-2026" to
// "2006-01". The year comes from the text when it names one, otherwise from
// the as-of date the server threaded in -- never from time.Now(), so the
// binding is reproducible from the request alone.
func monthFromEnglish(text string, params map[string]any) string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '/' || r == ',' || r == '.'
	})
	month := 0
	year := 0
	for _, f := range fields {
		if m, ok := englishMonths[f]; ok && month == 0 {
			month = m
			continue
		}
		if len(f) == 4 {
			if n, err := strconv.Atoi(f); err == nil && n >= 1000 {
				year = n
			}
		}
	}
	if month == 0 {
		return ""
	}
	if year == 0 {
		asOf, _ := params["as_of"].(string)
		if len(asOf) >= 4 {
			if n, err := strconv.Atoi(asOf[:4]); err == nil && n >= 1000 {
				year = n
			}
		}
	}
	if year == 0 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", year, month)
}

// englishMonths carries both the full name and the common abbreviations, which
// is every spelling the planner has been measured producing for this
// parameter.
var englishMonths = map[string]int{
	"january": 1, "jan": 1, "february": 2, "feb": 2, "march": 3, "mar": 3,
	"april": 4, "apr": 4, "may": 5, "june": 6, "jun": 6, "july": 7, "jul": 7,
	"august": 8, "aug": 8, "september": 9, "sep": 9, "sept": 9,
	"october": 10, "oct": 10, "november": 11, "nov": 11, "december": 12, "dec": 12,
}

// mentionsAMonth reports that the question is asked in months at all.
func mentionsAMonth(text string) bool {
	return strings.Contains(strings.ToLower(text), "month")
}

func cloneParams(params map[string]any) map[string]any {
	next := make(map[string]any, len(params)+1)
	for k, v := range params {
		next[k] = v
	}
	return next
}

func (e *procurementExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.procurementDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("procurement data reader not wired"),
		}, nil
	}

	facts, err := e.procurementDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// workforceExecutor provides roster coverage information.
type workforceExecutor struct {
	workforceDataReader scopedReader
}

func (e *workforceExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "admin_roster_coverage",
		Route:       domain.RouteAPI,
		Description: "Roster coverage by scope (position/pen)",
		// position_id/start_date/end_date were previously advertised but
		// workforceports.ListCoverageParams has none of those fields -- the
		// pipeline could never honor them. scope_type/scope_id ARE real
		// ListCoverageParams fields and the closure maps them; that is the set
		// actually wired end-to-end today. park_label is NOT advertised: roster
		// coverage scope is shed/position-based (scope_type="shed"), not
		// park-based, and there is no park-label resolution that fits this
		// scope model.
		Params: []string{"scope_type", "scope_id"},
	}
}

func (e *workforceExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.workforceDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("workforce data reader not wired"),
		}, nil
	}

	facts, err := e.workforceDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// verificationExecutor provides verification queue information.
type verificationExecutor struct {
	verificationDataReader scopedReader
}

func (e *verificationExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "verification_queue",
		Route:       domain.RouteAPI,
		Description: "Verification queue items pending review",
		Params:      []string{"status", "category", "vertical", "module"},
	}
}

func (e *verificationExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.verificationDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("verification data reader not wired"),
		}, nil
	}

	facts, err := e.verificationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// actionCenterExecutor provides action center obligations.
type actionCenterExecutor struct {
	actionCenterDataReader scopedReader
}

func (e *actionCenterExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "action_center_obligations",
		Route:       domain.RouteAPI,
		Description: "Action center obligations requiring attention",
		// work_state and shed_id map directly onto processintegritydomain.Query
		// fields. park_label is resolved to Query.ParkID via the park resolver
		// wired in bootstrap (see api.go / ceoai_readers.go) -- this is the P1
		// fix: previously advertised but silently dropped. shed_id is a plain
		// shed-location ID; there is no sub-shed "partition" concept on this
		// Query (partition_label lives only on vaccination_drive_assignments /
		// goat_shed_partitions, which this tool does not read), so partition is
		// intentionally NOT advertised.
		Params: []string{"work_state", "park_label", "shed_id"},
	}
}

func (e *actionCenterExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.actionCenterDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("action center data reader not wired"),
		}, nil
	}

	facts, err := e.actionCenterDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// opsKernelHealthExecutor provides the process-integrity control-tower view
// (open exceptions / broken-or-at-risk obligations) for kernel-health questions.
type opsKernelHealthExecutor struct {
	opsKernelHealthDataReader scopedReader
}

func (e *opsKernelHealthExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "operations_kernel_health",
		Route:       domain.RouteAPI,
		Description: "Open exceptions / broken-or-at-risk obligations (kernel health)",
		// severity, work_state and shed_id are real processintegritydomain.Query
		// fields the closure maps directly (shed_id is a plain shed-location ID,
		// same field ParkID's sibling ShedID -- see action_center). park_label is
		// NOT advertised for the same reason as action_center's ParkID -- see the
		// api.go wiring comment; unlike action_center this tool has no park
		// resolver wired yet. There is no sub-shed "partition" concept on this
		// Query (partition_label lives only on vaccination_drive_assignments /
		// goat_shed_partitions, which this tool does not read), so partition is
		// NOT advertised either.
		Params: []string{"severity", "work_state", "shed_id"},
	}
}

func (e *opsKernelHealthExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.opsKernelHealthDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("operations kernel health data reader not wired"),
		}, nil
	}

	facts, err := e.opsKernelHealthDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// opsAuditSummaryExecutor provides the operations/business audit summary.
type opsAuditSummaryExecutor struct {
	opsAuditSummaryDataReader scopedReader
}

func (e *opsAuditSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "operations_audit_summary",
		Route:       domain.RouteAPI,
		Description: "Operations/business audit activity summary",
		// category/module/status are real operationsaudit domain.Query fields
		// the closure maps directly. No park field exists on that Query, so
		// park_label is not advertised.
		Params: []string{"category", "module", "status"},
	}
}

func (e *opsAuditSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.opsAuditSummaryDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("operations audit summary data reader not wired"),
		}, nil
	}

	facts, err := e.opsAuditSummaryDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    stampTenant(actor, facts),
	}, nil
}

// NewToolExecutors returns a set of in-process read service tool executors
// for tier-2 (API) routing. These are registered with the leadership assistant
// registry to handle RouteAPI sub-questions.
// The executors are wired with data readers in the bootstrap layer.
func NewToolExecutors() []ports.ToolExecutor {
	return []ports.ToolExecutor{
		&countsBreakdownExecutor{},
		&vaccinationShedSummaryExecutor{},
		&vaccinationExecutionExecutor{},
		&feedDirectionTodayExecutor{},
		&feedWeightBandSummaryExecutor{},
		&procurementExecutor{},
		&salesOverviewExecutor{},
		&workforceExecutor{},
		&verificationExecutor{},
		&actionCenterExecutor{},
		&opsKernelHealthExecutor{},
		&opsAuditSummaryExecutor{},
	}
}

// SetSalesDataReader wires the sales overview reader into the sales executor.
func SetSalesDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*salesOverviewExecutor); ok {
		e.salesDataReader = reader
	}
}

// SetCountsDataReader wires the counts data reader into the counts executor.
func SetCountsDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*countsBreakdownExecutor); ok {
		e.countsBySpeciesReader = reader
	}
}

// SetVaccinationDataReader wires the vaccination data reader into the vaccination executors.
func SetVaccinationDataReader(execs []ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	for _, exec := range execs {
		switch e := exec.(type) {
		case *vaccinationShedSummaryExecutor:
			e.vaccinationDataReader = reader
		case *vaccinationExecutionExecutor:
			e.vaccinationDataReader = reader
		}
	}
}

// SetFeedDataReader wires the feed data reader into the feed executor.
func SetFeedDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*feedDirectionTodayExecutor); ok {
		e.feedDataReader = reader
	}
}

// SetFeedWeightBandReader wires the Growth Director feed-band reader.
func SetFeedWeightBandReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*feedWeightBandSummaryExecutor); ok {
		e.feedWeightBandReader = reader
	}
}

// SetProcurementDataReader wires the procurement data reader into the procurement executor.
func SetProcurementDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*procurementExecutor); ok {
		e.procurementDataReader = reader
	}
}

// SetWorkforceDataReader wires the workforce data reader into the workforce executor.
func SetWorkforceDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*workforceExecutor); ok {
		e.workforceDataReader = reader
	}
}

// SetVerificationDataReader wires the verification data reader into the verification executor.
func SetVerificationDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*verificationExecutor); ok {
		e.verificationDataReader = reader
	}
}

// SetActionCenterDataReader wires the action center data reader into the action center executor.
func SetActionCenterDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*actionCenterExecutor); ok {
		e.actionCenterDataReader = reader
	}
}

// SetOpsKernelHealthDataReader wires the data reader into the operations kernel health executor.
func SetOpsKernelHealthDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*opsKernelHealthExecutor); ok {
		e.opsKernelHealthDataReader = reader
	}
}

// SetOpsAuditSummaryDataReader wires the data reader into the operations audit summary executor.
func SetOpsAuditSummaryDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*opsAuditSummaryExecutor); ok {
		e.opsAuditSummaryDataReader = reader
	}
}
