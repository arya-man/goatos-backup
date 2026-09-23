package app

// sqlrepair.go holds the plan v3 D1.2/D1.3 pieces the orchestrator threads
// around the SQL fallback tier:
//
//   - TokenUsage: the real Vertex token accounting (usageMetadata) the budget
//     records instead of len/4 (D1.1);
//   - the optional planner capabilities the orchestrator type-asserts for
//     (usagePlanner, sqlRepairer) so ports.AIProvider stays unchanged;
//   - prepareSQLWindows: a period question routed to a current-state view is
//     answered "as of now" explicitly (from/to stripped, window_as_of set);
//   - repairSQLResults: on a sqlguard reject or Postgres error of a
//     model-drafted sql_fallback, re-prompt the model ONCE with the reason and
//     the referenced view's schema card, re-run through the same guard; a
//     second failure leaves the existing honest-partial path untouched;
//   - windowResult: the one synthetic "Window: …" fact the composer prints so
//     the answer states the period it was computed over (D4 citation rule).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TokenUsage is one model call's billed token accounting as the provider
// reports it (Vertex usageMetadata). Zero means "not reported": the budget
// then falls back to the len/4 estimate.
type TokenUsage struct {
	PromptTokens int
	OutputTokens int
}

func (u TokenUsage) add(o TokenUsage) TokenUsage {
	return TokenUsage{PromptTokens: u.PromptTokens + o.PromptTokens, OutputTokens: u.OutputTokens + o.OutputTokens}
}

func (u TokenUsage) reported() bool { return u.PromptTokens > 0 || u.OutputTokens > 0 }

// usagePlanner is the optional capability a provider implements to report
// real token usage with its plan (adapters/vertex does).
type usagePlanner interface {
	PlanWithUsage(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, TokenUsage, error)
}

// sqlRepairer is the optional capability a provider implements to repair one
// rejected SQL draft (adapters/vertex does). The deterministic fallback
// planner does not, so a keyword-planned request never enters the loop.
type sqlRepairer interface {
	RepairSQL(ctx context.Context, q domain.Question, failedSQL, reason, cardText, windowText string) (string, TokenUsage, error)
}

// sqlTelemetry is the optional telemetry capability for the D1.3 counters
// (adapters/observability.Metrics implements it); ports.Telemetry is unchanged.
type sqlTelemetry interface {
	SQLReject(ctx context.Context, reason string)
	SQLPGError(ctx context.Context, code string)
}

// Param keys the window threading writes. from/to are ISO business dates
// (inclusive); window_label is the phrase; compare_from/compare_to carry a
// two-window comparison; window_as_of is set (to the as-of date) when a
// period was asked but the routed SQL view is current-state.
const (
	paramFrom        = "from"
	paramTo          = "to"
	paramWindowLabel = "window_label"
	paramCompareFrom = "compare_from"
	paramCompareTo   = "compare_to"
	paramWindowAsOf  = "window_as_of"
	// paramWindowAltCols lists the ALTERNATE date columns this question is
	// allowed to bind its period on, comma-separated, computed by the SERVER
	// from the question's own words. A card's other date columns are NOT
	// interchangeable: binding "last 90 days" to verified_at and then labelling
	// the answer with the requested period reports a figure read over a date
	// nobody asked about, and the reader cannot see the swap.
	paramWindowAltCols = "window_alt_columns"
)

// isModelSQL reports whether a sub-question is a model-drafted SQL fallback
// (the only shape the window guard and the repair loop apply to). Trusted
// server-authored SQL and the deterministic regex pre-planner's SQL
// (natural_sql.go, marked by the natural_sql param) are never repaired or
// window-checked here: they are backend code, bind their own dates, and still
// pass through sqlguard.Validate in the executor.
func isModelSQL(sub domain.SubQuestion) bool {
	if sub.Route != domain.RouteSQL || sub.TrustedSQL {
		return false
	}
	if v, ok := sub.Params["natural_sql"].(string); ok && v != "" {
		return false
	}
	return true
}

// isModelSQLParams is isModelSQL for the registry, which sees only the params
// of a non-trusted RouteSQL sub-question.
func isModelSQLParams(params map[string]any) bool {
	v, ok := params["natural_sql"].(string)
	return !(ok && v != "")
}

// prepareSQLWindows applies the current-state rule before execution: for a
// model-drafted SQL sub-question whose referenced view has no date column,
// the from/to params are removed (so sqlguard.ValidateWindow does not fire)
// and window_as_of records that the answer is as of the business date. The
// composer then prints "Window: as of <date>" instead of a period the view
// cannot honour. Returns true when at least one sub-question was converted.
func prepareSQLWindows(subs []domain.SubQuestion, w Window, asOf time.Time) bool {
	if w.IsZero() {
		return false
	}
	converted := false
	for i := range subs {
		if !isModelSQL(subs[i]) {
			continue
		}
		sql, _ := subs[i].Params["sql"].(string)
		card, ok := reporting.CardForSQL(sql)
		if !ok || card.DateColumn != "" {
			continue
		}
		delete(subs[i].Params, paramFrom)
		delete(subs[i].Params, paramTo)
		delete(subs[i].Params, paramCompareFrom)
		delete(subs[i].Params, paramCompareTo)
		subs[i].Params[paramWindowAsOf] = asOf.In(biztime.DefaultLocation()).Format("2006-01-02")
		converted = true
	}
	return converted
}

// sqlWindowFromParams rebuilds the guard window from a sub-question's
// from/to params. ok=false when no window was threaded.
func sqlWindowFromParams(params map[string]any) (sqlguard.Window, bool) {
	return windowFromParamKeys(params, paramFrom, paramTo)
}

// sqlCompareWindowFromParams rebuilds the comparison window from a
// sub-question's compare_from/compare_to params. ok=false when the question
// was not a two-window comparison.
func sqlCompareWindowFromParams(params map[string]any) (sqlguard.Window, bool) {
	return windowFromParamKeys(params, paramCompareFrom, paramCompareTo)
}

func windowFromParamKeys(params map[string]any, fromKey, toKey string) (sqlguard.Window, bool) {
	from, _ := params[fromKey].(string)
	to, _ := params[toKey].(string)
	if from == "" || to == "" {
		return sqlguard.Window{}, false
	}
	return sqlguard.WindowFromDates(from, to, biztime.DefaultLocation())
}

// validateModelSQL is the guard sequence the registry runs on a model-drafted
// statement before the executor: sqlguard.Validate, then (when a window was
// threaded) sqlguard.ValidateWindow against the referenced view's card. The
// window comes ONLY from the server-injected from/to params (injectWindow
// overwrites or strips whatever the plan carried). For a two-window
// comparison the statement may bind EITHER the primary window or the
// comparison window (compare_from/compare_to): the planner drafts one
// sub-question per window and each arm must be answerable. The executor
// re-runs Validate itself — never trust a caller — this pass exists so the
// window contract is enforced and the reject reason is attributable.
func validateModelSQL(sql string, params map[string]any) error {
	if err := sqlguard.Validate(sql); err != nil {
		return err
	}
	if !isModelSQLParams(params) {
		return nil
	}
	w, ok := sqlWindowFromParams(params)
	if !ok {
		return nil
	}
	var card sqlguard.SchemaCardLike
	if c, found := reporting.CardForSQL(sql); found {
		// The card's alternate date columns are narrowed to the ones the
		// QUESTION named, so a period can only ride a second date column when
		// the leader asked about that date.
		card = narrowAlternates(c, allowedAltColumns(params))
	}
	primaryErr := sqlguard.ValidateWindow(sql, card, w)
	if primaryErr == nil {
		return nil
	}
	cw, hasCompare := sqlCompareWindowFromParams(params)
	if !hasCompare || errors.Is(primaryErr, sqlguard.ErrWindowOnCurrentStateView) {
		return primaryErr
	}
	if compareErr := sqlguard.ValidateWindow(sql, card, cw); compareErr == nil {
		return nil
	}
	return fmt.Errorf("%w (or, for the comparison arm, %s >= '%s' AND %s < '%s')",
		primaryErr, dateColumnOf(card), cw.FromLiteral(), dateColumnOf(card), cw.ToExclusiveLiteral())
}

// dateColumnOf is the card's date column for messages ("<date_col>" when the
// card is unknown).
func dateColumnOf(card sqlguard.SchemaCardLike) string {
	if card == nil || strings.TrimSpace(card.CardDateColumn()) == "" {
		return "<date_col>"
	}
	return card.CardDateColumn()
}

// rejectClass maps a guard/executor error to the bounded reason label the
// ceoai_sql_reject_total metric is tagged with. Free-form validator text stays
// in the trace step; only the class becomes a label.
func rejectClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, sqlguard.ErrWindowOnCurrentStateView) {
		return "window_current_state"
	}
	if errors.Is(err, sqlguard.ErrTenantBinding) {
		return "tenant_binding"
	}
	ve, ok := sqlguard.AsValidationError(err)
	if !ok {
		return ""
	}
	r := strings.ToLower(ve.Reason)
	switch {
	case strings.Contains(r, "window"):
		return "window"
	case strings.Contains(r, "banned keyword"):
		return "banned_keyword"
	case strings.Contains(r, "banned function"), strings.Contains(r, "banned identifier"):
		return "banned_function"
	case strings.Contains(r, "join"):
		return "join"
	case strings.Contains(r, "subquer"), strings.Contains(r, "single flat select"):
		return "subquery"
	case strings.Contains(r, "limit"):
		return "limit"
	case strings.Contains(r, "tenant"), strings.Contains(r, "where"):
		return "tenant_scope"
	case strings.Contains(r, "schema"), strings.Contains(r, "source"):
		return "schema"
	case strings.Contains(r, "comment"), strings.Contains(r, "separator"), strings.Contains(r, "quoted"), strings.Contains(r, "literal"):
		return "syntax"
	default:
		return "other"
	}
}

// pgErrorCode extracts the SQLSTATE of a Postgres failure, or "" when the
// error is not a Postgres error.
func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// sqlFailureKind classifies a failed sql_fallback result: "reject" for a
// guard rejection, "pg" for a Postgres error, "" for anything else (nil,
// wiring, context) which the repair loop leaves alone.
func sqlFailureKind(err error) string {
	if err == nil {
		return ""
	}
	if rejectClass(err) != "" {
		return "reject"
	}
	if pgErrorCode(err) != "" {
		return "pg"
	}
	return ""
}

// repairSQLResults is the D1.3 one-shot repair loop. For every model-drafted
// sql_fallback result that failed with a guard rejection or a Postgres error,
// it records the failure (metric + trace step with the validator reason),
// asks the provider for ONE corrected draft with the reason and the referenced
// view's schema card, runs the corrected draft through the same registry path
// (Validate -> ValidateWindow -> executor), and replaces the result on
// success. A second failure is recorded and left for the existing
// honest-partial path. It returns the token usage of the repair calls.
func (a *Assistant) repairSQLResults(ctx context.Context, q domain.Question, subs []domain.SubQuestion, results []domain.ToolResult, traces *[]domain.StepTrace, budget *askBudget) TokenUsage {
	var usage TokenUsage
	repairer, canRepair := a.provider.(sqlRepairer)
	// A model that answered with nothing has nothing to say about the next
	// failed read either, so the first empty candidate ends repair for this
	// ask rather than spending a round trip per failed read on it.
	repairSilent := false
	for i := range results {
		if i >= len(subs) || !isModelSQL(subs[i]) {
			continue
		}
		err := results[i].Err
		kind := sqlFailureKind(err)
		if kind == "" {
			continue
		}
		a.recordSQLFailure(ctx, err)
		if !canRepair {
			continue
		}
		failedSQL, _ := subs[i].Params["sql"].(string)
		cardText := ""
		if card, ok := reporting.CardForSQL(failedSQL); ok {
			cardText = card.RenderCompact()
		}
		windowText := ""
		if w, ok := sqlWindowFromParams(subs[i].Params); ok {
			col := "<date_col>"
			if card, ok := reporting.CardForSQL(failedSQL); ok && card.DateColumn != "" {
				col = card.DateColumn
			}
			windowText = col + " >= '" + w.FromLiteral() + "' AND " + col + " < '" + w.ToExclusiveLiteral() + "'"
			if cw, ok := sqlCompareWindowFromParams(subs[i].Params); ok {
				windowText += " (primary window) or " + col + " >= '" + cw.FromLiteral() + "' AND " + col + " < '" + cw.ToExclusiveLiteral() + "' (comparison window)"
			}
		}
		// A repair is a model round trip and a re-execution: it is charged to the
		// ASK's allowance, and an exhausted ask keeps the honest failed read
		// rather than starting work whose answer arrives after the reply.
		if repairSilent || !budget.take() {
			continue
		}
		repairCtx, cancelRepair := budget.withDeadline(ctx)
		start := a.now()
		fixed, u, rerr := repairer.RepairSQL(repairCtx, q, failedSQL, err.Error(), cardText, windowText)
		cancelRepair()
		if modelAnsweredNothing(rerr) {
			repairSilent = true
		}
		usage = usage.add(u)
		if rerr != nil {
			a.log.WarnContext(ctx, "ceoai sql repair call failed", "error", rerr)
			*traces = append(*traces, domain.StepTrace{
				SubQuestionID: subs[i].ID, Route: domain.RouteSQL, ToolName: "sql_repair",
				StartedAt: start, DurationMS: a.now().Sub(start).Milliseconds(),
				Err: "repair call: " + rerr.Error(),
			})
			continue
		}
		retry := subs[i]
		retry.Params = make(map[string]any, len(subs[i].Params)+1)
		for k, v := range subs[i].Params {
			retry.Params[k] = v
		}
		retry.Params["sql"] = fixed
		retry.Params["_repaired_from"] = kind
		res, execErr := a.registry.Execute(ctx, q.Actor, retry)
		if execErr == nil && res.Err != nil {
			execErr = res.Err
		}
		trace := domain.StepTrace{
			SubQuestionID: subs[i].ID, Route: domain.RouteSQL, ToolName: "sql_repair",
			StartedAt: start, DurationMS: a.now().Sub(start).Milliseconds(),
			RowCount: len(res.Facts), Err: errString(execErr),
		}
		*traces = append(*traces, trace)
		if execErr != nil {
			// Second failure: record it and fall through to the honest partial.
			a.recordSQLFailure(ctx, execErr)
			continue
		}
		res.SubQuestionID = subs[i].ID
		if res.Route == "" {
			res.Route = domain.RouteSQL
		}
		if res.ToolName == "" {
			res.ToolName = subs[i].ToolName
		}
		results[i] = res
		subs[i].Params["sql"] = fixed
	}
	return usage
}

// recordSQLFailure emits the D1.3 counters for one failed model-drafted read.
func (a *Assistant) recordSQLFailure(ctx context.Context, err error) {
	st, ok := a.telemetry.(sqlTelemetry)
	if !ok || err == nil {
		return
	}
	if cls := rejectClass(err); cls != "" {
		st.SQLReject(ctx, cls)
		return
	}
	if code := pgErrorCode(err); code != "" {
		st.SQLPGError(ctx, code)
	}
}

// windowResult builds the one synthetic result the composer renders as
// "Window: …" so the answer states its period exactly once (D4). It is
// emitted only when a model-drafted SQL read grounded the answer: a resolved
// window renders the period; a current-state conversion renders "as of
// <date>". The single fact is stamped with the actor's tenant so the tenant
// gate accepts it, and reuses the SQL read's Surface so it adds no citation.
// nil when nothing applies.
//
// The line describes what actually RAN, not what the question resolved (PR
// #318 R2-2): for a two-window comparison each successful model-SQL sub is
// checked for which window its statement bound (boundWindow, the same check
// validateModelSQL accepted it on). "A vs B" is printed only when at least
// one arm bound A AND one arm bound B; when every executed arm bound the same
// window (both arms drafted for August, or only one arm ran) the line names
// that single window and says the comparison could not be answered, so a
// reader never sees two identical numbers labelled month-on-month.
func windowResult(actor domain.Actor, subs []domain.SubQuestion, results []domain.ToolResult, w Window) *domain.ToolResult {
	asOf := ""
	windowed := false
	surface := ""
	boundPrimary, boundCompare := false, false
	for i, r := range results {
		if i >= len(subs) || !isModelSQL(subs[i]) || r.Err != nil {
			continue
		}
		if surface == "" {
			surface = r.Surface
		}
		if v, ok := subs[i].Params[paramWindowAsOf].(string); ok && v != "" {
			asOf = v
			continue
		}
		if _, ok := sqlWindowFromParams(subs[i].Params); ok {
			windowed = true
			switch boundWindow(subs[i].Params) {
			case boundToPrimary:
				boundPrimary = true
			case boundToCompare:
				boundCompare = true
			}
		}
	}
	var value string
	switch {
	case windowed && !w.IsZero():
		value = describeExecutedWindows(w, boundPrimary, boundCompare)
	case asOf != "":
		value = "as of " + biztime.FarmDateFromBusinessDate(asOf)
		// The question named a period but the source that answered it records a
		// CURRENT STATE, so the read bound a single day. Saying only "as of
		// <today>" silently re-labels the answer's period; name both.
		if !w.IsZero() {
			value += " (current state — the source does not record the " + describeExecutedWindows(w, true, false) + " period you asked about)"
		}
	default:
		return nil
	}
	// Same Surface as the SQL read it annotates: the composer de-duplicates
	// citations by Surface, and sourceLabel would otherwise add a bare "sql".
	return &domain.ToolResult{
		Route:     domain.RouteSQL,
		ToolName:  "window",
		Surface:   surface,
		Synthetic: true,
		Facts:     []domain.Fact{{TenantID: actor.TenantID, Label: "Window", Value: value}},
	}
}

// windowBinding names which of a comparison's two windows a model-SQL
// statement bound.
type windowBinding int

const (
	boundToNeither windowBinding = iota
	boundToPrimary
	boundToCompare
)

// boundWindow re-runs the window contract on the sub-question's (possibly
// repaired) statement to report which window it bound: the primary from/to
// or the comparison compare_from/compare_to. It mirrors validateModelSQL's
// acceptance order, so a statement that executed always reports one of the
// two when a window was threaded; boundToNeither only when the params carry
// no window or the view is current-state.
func boundWindow(params map[string]any) windowBinding {
	sql, _ := params["sql"].(string)
	w, ok := sqlWindowFromParams(params)
	if !ok {
		return boundToNeither
	}
	var card sqlguard.SchemaCardLike
	if c, found := reporting.CardForSQL(sql); found {
		card = c
	}
	if sqlguard.ValidateWindow(sql, card, w) == nil {
		return boundToPrimary
	}
	if cw, has := sqlCompareWindowFromParams(params); has && sqlguard.ValidateWindow(sql, card, cw) == nil {
		return boundToCompare
	}
	return boundToNeither
}

// describeExecutedWindows renders the Window line from the set of windows
// the executed arms actually bound. A non-comparison window renders as
// before. A comparison renders "A vs B" only when both arms ran; otherwise
// the single executed window plus an honest note naming the other.
func describeExecutedWindows(w Window, boundPrimary, boundCompare bool) string {
	if w.Compare == nil || w.Compare.IsZero() {
		return w.Describe()
	}
	switch {
	case boundPrimary && boundCompare:
		return w.Describe()
	case boundCompare && !boundPrimary:
		return describeOne(*w.Compare) + " (comparison with " + describeOne(w) + " could not be answered)"
	default:
		// Primary only, or (defensively) neither reported: never claim a
		// comparison that did not run.
		return describeOne(w) + " (comparison with " + describeOne(*w.Compare) + " could not be answered)"
	}
}

// allowedAltColumns reads the server-computed alternate-date allowance off the
// sub-question's params. Absent means NONE: an alternate date column is opt-in,
// per question.
func allowedAltColumns(params map[string]any) []string {
	raw, _ := params[paramWindowAltCols].(string)
	var out []string
	for _, c := range strings.Split(raw, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// narrowedCard is a schema card whose alternate date columns are restricted to
// the ones the question named. The card's OWN business-day column is never
// narrowed — that is the default period column and needs no permission.
type narrowedCard struct {
	reporting.SchemaCard
	allowed []string
}

// AlternateDateColumns implements sqlguard.AlternateDateColumnsCard.
func (n narrowedCard) AlternateDateColumns() []string {
	if len(n.allowed) == 0 {
		return nil
	}
	declared := n.SchemaCard.AlternateDateColumns()
	var out []string
	for _, col := range declared {
		for _, want := range n.allowed {
			if strings.EqualFold(col, want) {
				out = append(out, col)
				break
			}
		}
	}
	return out
}

// narrowAlternates wraps a card with the per-question allowance.
func narrowAlternates(card reporting.SchemaCard, allowed []string) sqlguard.SchemaCardLike {
	return narrowedCard{SchemaCard: card, allowed: allowed}
}

// modelAnsweredNothing reports the provider returning no candidate at all --
// not a rejected repair, but the model saying nothing. Asking it again inside
// the same ask spends a round trip for the same silence.
func modelAnsweredNothing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "empty candidate")
}
