package ceoai

// wiring.go holds the seam bridges that adapt the sibling-owned concrete
// adapters (Cube cubeclient, MCP toolboxclient, safety moderator, persistence
// conversation store, Vertex planner) to the app ports. Keeping the bridges in
// package ceoai lets bootstrap construct the whole assistant from env with a
// single call and keeps the cross-package concrete dependencies out of the
// bootstrap package. Tenant scope always comes from the Actor (server session),
// never from user text or params.

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	ceohttp "github.com/vgoats/goatos/backend/internal/ceoai/adapters/http"
	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/vertex"
	"github.com/vgoats/goatos/backend/internal/ceoai/cubeclient"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/persistence"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/ceoai/safety"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	"github.com/vgoats/goatos/backend/internal/ceoai/toolboxclient"
)

// ---------------------------------------------------------------------------
// Vertex AIProvider + Reviewer
// ---------------------------------------------------------------------------

// NewVertexProvider builds the Gemini planner from env (MESHA_VERTEX_*). It
// returns nil when the provider is not configured or ADC is unavailable so the
// caller degrades to the deterministic keyword planner (mode=fallback) instead
// of failing boot. The returned *vertex.Planner satisfies BOTH ports.AIProvider
// and ports.Reviewer.
func NewVertexProvider(ctx context.Context, log *slog.Logger) *vertex.Planner {
	if !strings.EqualFold(os.Getenv("MESHA_AI_PROVIDER"), "vertex") {
		return nil
	}
	p, err := vertex.New(ctx, vertex.Config{
		Project:  os.Getenv("MESHA_VERTEX_PROJECT"),
		Location: os.Getenv("MESHA_VERTEX_LOCATION"),
		Model:    envOr("MESHA_VERTEX_MODEL", "gemini-3.8-flash"),
		// The classifying call (the grounding critic) runs on a smaller, faster
		// model. Unset falls back to the planner model, so nothing changes for a
		// deployment that does not configure one.
		JudgeModel: os.Getenv("MESHA_VERTEX_JUDGE_MODEL"),
	})
	if err != nil {
		if log != nil {
			log.Warn("ceoai: vertex provider unavailable, using keyword fallback", "err", err.Error())
		}
		return nil
	}
	return p
}

// ---------------------------------------------------------------------------
// Cube MetricService bridge
// ---------------------------------------------------------------------------

// metricBinding maps a planner-facing metric/tool name to its governed Cube
// view member and governance status. The FORMULAS live in the Cube model; this
// table is only the name→member routing the planner catalog advertises.
type metricBinding struct {
	view    string // Cube view, e.g. "kpi_animals"
	measure string // measure member, e.g. "active_animal_count"
	timeDim string // business-day time member on view, e.g. "due_business_day"
	status  domain.MetricStatus
	title   string
}

// cubeMetricBindings is the governed catalog the assistant exposes as tier-1.
// Names match the keyword/Vertex planner tool names. Only active/total census
// counts are approved; every vaccination + rate metric is draft (see METRICS.md).
// The timeDim is the view's IST business-day member (analytics/cube/model/views
// /leadership.yml). It is the member a time-scoped/trend request buckets on:
// mortality anchors on exit_business_day; vaccination on due_business_day.
//
// Census counts (active_animals/total_animals) have NO time member and are
// deliberately all-time. active_animal_count filters lifecycle_status='alive'
// and exit_business_day is NULL for every living animal, so binding a
// timeDimension on exit_business_day would window OUT the entire living herd and
// return a near-zero census mislabelled as the scoped metric. A census question
// ("active animals", even "active animals last month") is answered as the
// current all-time living count; empty timeDim makes timeDimensionFor return
// ok=false so Query() never appends a window, and Metrics() advertises no
// TimeGrains for census so the planner cannot try to time-scope it.
var cubeMetricBindings = map[string]metricBinding{
	"active_animals":         {"kpi_animals", "active_animal_count", "", domain.MetricApproved, "Active animals"},
	"total_animals":          {"kpi_animals", "total_animal_count", "", domain.MetricApproved, "Total animals"},
	"mortality_rate":         {"kpi_animals", "mortality_rate", "exit_business_day", domain.MetricDraft, "Mortality rate"},
	"vaccination_due":        {"kpi_vaccination", "vaccination_due", "due_business_day", domain.MetricDraft, "Vaccinations due"},
	"vaccination_due_today":  {"kpi_vaccination", "due_today", "due_business_day", domain.MetricDraft, "Vaccinations due today"},
	"vaccination_overdue":    {"kpi_vaccination", "vaccination_overdue", "due_business_day", domain.MetricDraft, "Vaccinations overdue"},
	"vaccination_completed":  {"kpi_vaccination", "vaccination_completed", "due_business_day", domain.MetricDraft, "Vaccinations completed"},
	"vaccination_compliance": {"kpi_vaccination", "vaccination_compliance", "due_business_day", domain.MetricDraft, "Vaccination compliance"},
	// Operator-grain vaccination drive KPIs (operator-based drive model). These
	// answer the operator questions the shed-grain metrics above cannot: which
	// operators are behind, who is overloaded, operator drive load, assigned
	// animals per operator. Source: kpi_vaccination_operator view over
	// ceo_ai.vaccination_operator_status (migration 000026).
	"operator_vaccination_load":        {"kpi_vaccination_operator", "operator_assigned_animals", "planned_business_day", domain.MetricDraft, "Animals assigned to operators"},
	"operator_vaccination_overdue":     {"kpi_vaccination_operator", "operator_overdue", "planned_business_day", domain.MetricDraft, "Operator vaccination overdue"},
	"operator_vaccination_capacity":    {"kpi_vaccination_operator", "operator_capacity", "planned_business_day", domain.MetricDraft, "Operator daily capacity"},
	"operator_vaccination_utilization": {"kpi_vaccination_operator", "operator_utilization", "planned_business_day", domain.MetricDraft, "Operator utilization"},
}

// cubeDimensionMembers maps a plain dimension/filter key to its view member
// suffix. park_label/shed_label/species exist on every leadership view.
var cubeDimensionMembers = map[string]string{
	"species":         "species",
	"park_label":      "park_label",
	"shed_label":      "shed_label",
	"partition_label": "partition_label",
	"park":            "park_label",
	"shed":            "shed_label",
	"partition":       "partition_label",
	"operator_label":  "operator_label",
	"operator":        "operator_label",
}

type cubeMetricService struct {
	client *cubeclient.Client
	log    *slog.Logger
}

// NewCubeMetricService builds the Cube governed-metric port from env
// (MESHA_CUBE_URL + MESHA_CUBE_API_SECRET). Returns nil when unconfigured so the
// registry degrades (Cube-routed questions then honestly report the tier is not
// wired instead of fabricating a number).
func NewCubeMetricService(log *slog.Logger) ports.MetricService {
	c, err := cubeclient.New(cubeclient.Config{
		BaseURL:   os.Getenv("MESHA_CUBE_URL"),
		APISecret: os.Getenv("MESHA_CUBE_API_SECRET"),
	})
	if err != nil {
		if log != nil {
			log.Warn("ceoai: cube metric service unavailable", "err", err.Error())
		}
		return nil
	}
	return &cubeMetricService{client: c, log: log}
}

func (s *cubeMetricService) Metrics(_ context.Context) ([]ports.MetricSpec, error) {
	out := make([]ports.MetricSpec, 0, len(cubeMetricBindings))
	for name, b := range cubeMetricBindings {
		// Only metrics bound to a real business-day member advertise TimeGrains.
		// Census counts (no timeDim) are all-time and must NOT invite the planner
		// to time-scope them onto exit_business_day (which is NULL for the living
		// herd and would zero out the answer).
		var grains []string
		if b.timeDim != "" {
			grains = []string{"day", "week", "month"}
		}
		out = append(out, ports.MetricSpec{
			Name:       name,
			Status:     b.status,
			Dimensions: metricDimensions(b),
			TimeGrains: grains,
			Title:      b.title,
		})
	}
	return out, nil
}

// metricDimensions is the group-by dimension menu a metric advertises to the
// planner. Operator-grain drive metrics live on kpi_vaccination_operator, whose
// canonical breakdown axis is the OPERATOR (operator_label), not species — so
// "which operator is behind / who is overloaded / at capacity" can only be
// answered per operator when operator_label is an advertised, groupable
// dimension. Every other governed metric keeps the species/park/shed menu.
func metricDimensions(b metricBinding) []string {
	if b.view == "kpi_vaccination_operator" {
		return []string{"operator_label", "park_label", "shed_label", "partition_label"}
	}
	return []string{"species", "park_label", "shed_label", "partition_label"}
}

func (s *cubeMetricService) Query(ctx context.Context, actor domain.Actor, req ports.MetricQuery) (domain.ToolResult, error) {
	b, ok := cubeMetricBindings[req.Metric]
	if !ok {
		return domain.ToolResult{}, fmt.Errorf("cube: unknown governed metric %q", req.Metric)
	}
	member := b.view + "." + b.measure

	q := cubeclient.Query{Measures: []string{member}}
	// Dimensions: group-by members (e.g. species split, operator breakdown).
	for _, d := range req.Dimensions {
		if suf, ok := cubeDimensionMembers[strings.ToLower(strings.TrimSpace(d))]; ok {
			q.Dimensions = append(q.Dimensions, b.view+"."+suf)
		}
	}
	// A grouped metric ("overdue by operator", "overdue by shed") is asked so the
	// answer can NAME and LEAD with the worst contributors. Order the rows by the
	// measure descending so the composer's grounded lead reads "led by <worst> …"
	// rather than an arbitrary Cube row order. This never changes any figure — only
	// the row sequence — so it stays inside the grounding contract.
	if len(q.Dimensions) > 0 {
		q.Order = map[string]string{member: "desc"}
	}
	// TimeRange: a time-scoped or trend leadership question ("mortality this
	// month", "vaccinations due this week", "mortality trend by month") MUST
	// carry a Cube timeDimension, or Cube returns the all-time aggregate and the
	// assistant answers an unscoped number labelled as the scoped metric. Parse
	// the planner's time_range into an IST business-day [from,to] window (and a
	// granularity for trends) on the view's business-day member. An unparseable
	// value is dropped (no timeDimension) rather than silently mislabelled — the
	// answer is then the all-time value, which the caller surfaces as such; we
	// never fabricate a window we cannot ground.
	if td, ok := timeDimensionFor(b, req.TimeRange, time.Now()); ok {
		q.TimeDimensions = append(q.TimeDimensions, td)
	}
	// Filters: business (non-tenant) equality filters. Tenant is injected by
	// Cube queryRewrite from the signed session context, never here.
	//
	// filterScope captures the equality filter VALUES (e.g. "goat", "Coimbatore")
	// so the resulting single-value fact is self-labelled. Root cause it fixes:
	// "goats vs sheep" is frequently planned as two species-FILTERED sub-queries
	// (species=goat / species=sheep) rather than one group_by; each returned a
	// bare "Active animals: 972" with no scope, so the two rows were
	// indistinguishable and the answer read as an ambiguous raw dump (and the
	// grounding reviewer could not tell them apart). Threading the filter value
	// into Fact.Scope labels each figure with the dimension it was filtered on.
	var filterScopeParts []string
	// Deterministic filter ordering so the scope label is stable across requests.
	filterKeys := make([]string, 0, len(req.Filters))
	for k := range req.Filters {
		filterKeys = append(filterKeys, k)
	}
	sort.Strings(filterKeys)
	for _, k := range filterKeys {
		v := req.Filters[k]
		suf, ok := cubeDimensionMembers[strings.ToLower(strings.TrimSpace(k))]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		q.Filters = append(q.Filters, cubeclient.Filter{
			Member:   b.view + "." + suf,
			Operator: "equals",
			Values:   []string{v},
		})
		filterScopeParts = append(filterScopeParts, strings.TrimSpace(v))
	}
	filterScope := strings.Join(filterScopeParts, " / ")

	res, err := s.client.Load(ctx, actor.TenantID, q)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("cube: load %s: %w", member, err)
	}

	tr := domain.ToolResult{
		SubQuestionID: "",
		Route:         domain.RouteCube,
		ToolName:      req.Metric,
		// Surface is USER-FACING (it becomes the citation chip + source label). It
		// must read as a clean business label — never the raw snake_case metric id
		// or the internal "Cube ·" plumbing tag. The Cube route + raw metric id stay
		// in the internal step trace / audit (ToolName + Route above), not here.
		Surface:      b.title,
		MetricStatus: b.status,
		AsOf:         time.Now(),
	}
	for _, row := range res.Rows {
		val := readableFigure(formatMetricValue(req.Metric, scalarString(row[member])), "", b.title)
		scope := joinScope(filterScope, cubeRowScope(row, b.view, q.Dimensions))
		tr.Facts = append(tr.Facts, domain.Fact{
			TenantID: actor.TenantID, // from the session actor, never from the Cube row
			Label:    b.title,
			Value:    val,
			Scope:    scope,
		})
	}
	if len(tr.Facts) == 0 {
		tr.Facts = append(tr.Facts, domain.Fact{TenantID: actor.TenantID, Label: b.title, Value: "0", Scope: filterScope})
	}
	return tr, nil
}

// formatMetricValue renders a raw Cube measure value as the user-facing figure.
// Operator utilization comes back as a raw ratio (1.55, 0.40); leadership reads
// it as a percent of capacity, so it is rendered "155%" / "40%". Rounding to a
// whole percent also kills the "1.5500000000000000" decimal-noise leak. The
// percent string keeps the figure groundable: the reviewer's number regex reads
// 155 from "155%", and the composer frames "over capacity at 155%". Every other
// metric passes through unchanged.
func formatMetricValue(metric, raw string) string {
	if metric != "operator_vaccination_utilization" {
		return raw
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return raw
	}
	return strconv.FormatInt(int64(math.Round(f*100)), 10) + "%"
}

// joinScope combines the static filter scope (e.g. "goat") with the per-row
// group-by scope (e.g. "Coimbatore") into one label, skipping empty parts, so a
// filtered AND grouped query still reads cleanly.
func joinScope(filterScope, rowScope string) string {
	switch {
	case filterScope == "":
		return rowScope
	case rowScope == "":
		return filterScope
	default:
		return filterScope + " / " + rowScope
	}
}

func cubeRowScope(row map[string]any, view string, dims []string) string {
	var parts []string
	shedMember := view + ".shed_label"
	partitionMember := view + ".partition_label"
	for _, d := range dims {
		if d == shedMember {
			shed := scalarString(row[shedMember])
			partition := scalarString(row[partitionMember])
			if shed != "" && partition != "" && !strings.EqualFold(partition, "whole") {
				// Use space separator for bare numerals, dash for worded labels (e.g., "Part 3").
				separator := " - "
				if isBarNumeric(partition) {
					separator = " "
				}
				parts = append(parts, shed+separator+partition)
				continue
			}
		}
		if d == partitionMember {
			continue
		}
		if v, ok := row[d]; ok {
			if s := scalarString(v); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " / ")
}

// scalarString renders ONE database value as the text a reader will see. It is
// the single rendering boundary between what pgx decodes out of a ceo_ai read
// and what the composer puts in an answer, so a value it cannot render properly
// is printed to a CEO verbatim.
//
// It used to end in a %v of whatever arrived, and pgx v5 decodes a `numeric`
// column into a pgtype.Numeric STRUCT -- so the most basic feed question
// answered "Total fed kg: Channapatna {1494 -1 false finite true}" instead of
// "149.4". A %v of a struct is never a figure; the cases below name every
// database scalar the fallback can return, and the default path asks the value
// ITSELF for its SQL text form before it can fall through to a struct dump.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return trimDecimalZeros(strconv.FormatFloat(t, 'f', -1, 64))
	case float32:
		return trimDecimalZeros(strconv.FormatFloat(float64(t), 'f', -1, 32))
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return trimDecimalZeros(t.String())
	case time.Time:
		return t.Format(time.RFC3339)
	case []byte:
		return string(t)
	case pgtype.Numeric:
		return numericString(t)
	case *pgtype.Numeric:
		if t == nil {
			return ""
		}
		return numericString(*t)
	default:
		return scalarStringSlow(v)
	}
}

// numericString renders a `numeric` column. pgx decodes one into a
// pgtype.Numeric, whose own text form carries the column's full SCALE -- which
// is how an oracle-exact price per kg reached a reader as
// 596.71800000000000000000. Only the scale's trailing zeros are dropped; the
// figure itself is never rounded.
func numericString(n pgtype.Numeric) string {
	v, err := n.Value()
	if err != nil || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return scalarString(v)
	}
	return trimDecimalZeros(s)
}

// scalarStringSlow renders the wrapper types pgx decodes into (pgtype.Numeric,
// pgtype.Date, pgtype.UUID, ...). Each of them carries its own SQL text form,
// and driver.Valuer IS that form, so it is asked for first. A NULL-ish wrapper
// reports (nil, nil) and renders empty, exactly like an untyped nil.
func scalarStringSlow(v any) string {
	if val, ok := v.(driver.Valuer); ok {
		dv, err := val.Value()
		if err == nil {
			if dv == nil {
				return ""
			}
			// dv is a driver.Value (string / int64 / float64 / bool /
			// time.Time / []byte) -- never another wrapper, so this cannot
			// recurse indefinitely.
			return scalarString(dv)
		}
	}
	if str, ok := v.(fmt.Stringer); ok {
		return str.String()
	}
	return fmt.Sprintf("%v", v)
}

// decimalText matches a plain decimal figure and nothing else -- no exponent,
// no unit, no thousands separator, no leading plus.
var decimalText = regexp.MustCompile(`^-?\d+\.\d+$`)

// factValueString renders the fact contract's `value` column, which is the one
// column of a ceo_ai read that is always a FIGURE.
//
// It exists because a model-drafted read may hand the same numeric over in
// either of two shapes: as a `numeric`, which pgx decodes into a
// pgtype.Numeric, or as text, when the draft writes `CAST(weight_kg AS text)`
// -- and it really does, on some routes and not others. The same animal's
// weight then read `22.9` or `22.900` depending on which plan the model
// happened to plan, which is the scale showing through again by another door.
//
// Only a string that is ENTIRELY a decimal figure is trimmed, and only in this
// column. A label, a scope and a unit are left exactly as the database spelled
// them: a pen called `Castro 1.10` is a name, not a number.
func factValueString(v any) string {
	s := scalarString(v)
	if _, isText := v.(string); isText && decimalText.MatchString(s) {
		return trimDecimalZeros(s)
	}
	return s
}

// earTagText matches a token shaped like the ear tag printed on an animal: a
// short letter prefix, a hyphen, and at least four digits ("MG-100001"). The
// digit floor is what keeps an operational location out of it -- sheds and
// partitions are spelled "Castro 1", "Godel 2 - Part 1", never "AB-1234" -- so
// no display identity is touched by the fold below. A bare date cannot match
// either: "2026-09-17" has no letter prefix.
var earTagText = regexp.MustCompile(`\b[A-Za-z]{2,4}-[0-9]{4,}\b`)

// canonicalTagText prints an ear tag the way it is printed ON THE EAR, which
// is also how goat_identifiers.normalized_value stores it: upper case.
//
// The ceo_ai views key the same tag lower case (animal_key is
// lower(btrim(...))), and whether a reader sees the stored spelling or the one
// their own question used depends on which plan the model happened to draft.
// The reviewer caught one answer echoing BOTH -- "MG-100001" in four lines and
// "mg-100001" in the fifth -- which reads as two animals.
//
// It folds tag-shaped TOKENS INSIDE the string ("Animal mg-100001" is a real
// label the weighing route writes), and nothing else: the surrounding words,
// every other label, every scope and every figure survive byte-identical.
func canonicalTagText(s string) string {
	if !strings.Contains(s, "-") || uuidText.MatchString(s) {
		return s
	}
	return earTagText.ReplaceAllStringFunc(s, strings.ToUpper)
}

// uuidText is the one hyphenated shape that can contain a tag-shaped run by
// coincidence (`...-abcd-1234...`). An id is not a name a reader reads, so a
// value carrying one is left byte-identical rather than half-upper-cased.
var uuidText = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// trimDecimalZeros drops the trailing zeros Postgres carries in a numeric's
// scale, so a price that divides out to 596.71800000000000000000 is shown as
// 596.718. It only ever removes zeros AFTER a decimal point: no value is
// rounded, and an integer-valued string is returned untouched.
func trimDecimalZeros(s string) string {
	if !strings.Contains(s, ".") || strings.ContainsAny(s, "eE") {
		return s
	}
	trimmed := strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	if trimmed == "" || trimmed == "-" {
		return "0"
	}
	return trimmed
}

// isBarNumeric reports whether a partition label is a bare ordinal (e.g., "1", "42").
// Used by partition display logic: bare numerics join with space, worded labels with dash.
func isBarNumeric(label string) bool {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// SQL fallback bridge (tier 4) — server-bound tenant
// ---------------------------------------------------------------------------

type sqlFallbackAdapter struct {
	exec *sqlguard.Executor
}

// NewSQLFallback adapts the sqlguard executor to ports.SQLFallback. Tenant is
// bound from the Actor (server session): the adapter calls
// ExecuteReadOnlyForTenant, which validates the draft AND rejects any draft whose
// tenant_id literal is not the session tenant, so the planner (influenced by user
// text) can never widen scope. Returns nil when exec is nil so an unconfigured
// boot leaves tier 4 absent (Cube/API/toolbox still answer; SQL-only questions
// honestly refuse) rather than nil-panicking.
func NewSQLFallback(exec *sqlguard.Executor) ports.SQLFallback {
	if exec == nil {
		return nil
	}
	return &sqlFallbackAdapter{exec: exec}
}

func (a *sqlFallbackAdapter) Execute(ctx context.Context, actor domain.Actor, sql string, _ []any) (domain.ToolResult, error) {
	rows, err := a.exec.ExecuteReadOnlyForTenant(ctx, actor.TenantID, sql)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("sqlguard fallback: %w", err)
	}
	return rowsToToolResult(actor.TenantID, rows, sql), nil
}

// ExecuteTrusted runs server-authored SQL with the session tenant ALWAYS bound
// as $1 by the executor; args (park scope etc.) bind as $2.. (D0 trusted-SQL API).
func (a *sqlFallbackAdapter) ExecuteTrusted(ctx context.Context, actor domain.Actor, sql string, args []any) (domain.ToolResult, error) {
	rows, err := a.exec.ExecuteTrustedReadOnlyForTenant(ctx, actor.TenantID, sql, args...)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("trusted sql fallback: %w", err)
	}
	return rowsToToolResult(actor.TenantID, rows, sql), nil
}

// seriesColumnPrefix is the SQL fact contract's multi-metric column prefix:
// `label, scope, value[, series_<name>…]` — every numeric `series_<name>`
// column lands in Fact.Values[name] so a grouped/stacked chart can draw more
// than one series per row (plan v3 D4). Non-numeric series columns are
// ignored rather than guessed.
const seriesColumnPrefix = "series_"

// rowsToToolResult maps sqlguard rows to grounding facts. tenantID is the
// SESSION tenant the executor already bound the query to; it is stamped onto
// every fact here and is never read from a row column (a `tenant_id` column in
// the result is just another scalar and is not trusted for scoping).
func rowsToToolResult(tenantID string, rows []sqlguard.Row, sql string) domain.ToolResult {
	tr := domain.ToolResult{
		Route:    domain.RouteSQL,
		ToolName: "sql_fallback",
		// Which ceo_ai view answered, taken from the executed statement. The
		// composer titles the block with it so a model-authored row label can
		// never stand alone as the only claim about what the figure counts.
		SourceView: reporting.ViewNameFromSQL(sql),
		// User-facing source label: a clean business phrase. The read-only SQL route
		// stays in the audit (Route above), never in the leadership chip.
		Surface: "Mesha operational data",
		AsOf:    time.Now(),
	}
	for _, row := range rows {
		label, hasLabel := row["label"]
		value, hasValue := row["value"]
		// A contract row that carries value (+ scope) but no label is still ONE
		// fact per row. Exploding it column-by-column below split "Channapatna"
		// and "242" into two unrelated facts, in map order.
		if !hasLabel && hasValue {
			label, hasLabel = "Result", true
		}
		if hasLabel {
			if hasValue {
				// The unit is read FIRST because the figure's precision is
				// derived from it: readableFigure needs the row's own
				// description of what it measures before it can decide how
				// much of a 16-decimal avg() a leader is owed.
				var unitText string
				if unit, hasUnit := row["unit"]; hasUnit {
					unitText = scalarString(unit)
				}
				labelText := canonicalTagText(scalarString(label))
				fact := domain.Fact{
					TenantID: tenantID,
					Label:    labelText,
					Unit:     unitText,
					Value:    readableFigure(canonicalTagText(factValueString(value)), unitText, labelText),
				}
				if scope, hasScope := row["scope"]; hasScope {
					fact.Scope = canonicalTagText(scalarString(scope))
				}
				fact.Values = seriesValues(row)
				tr.Facts = append(tr.Facts, fact)
				continue
			}
		}
		for k, v := range row {
			tr.Facts = append(tr.Facts, domain.Fact{
				TenantID: tenantID,
				Label:    k,
				Value:    readableFigure(canonicalTagText(scalarString(v)), "", k),
			})
		}
	}
	return tr
}

// seriesValues collects every numeric `series_<name>` column of a contract row
// into a name -> float64 map (nil when the row has none).
func seriesValues(row sqlguard.Row) map[string]float64 {
	var values map[string]float64
	for k, v := range row {
		if !strings.HasPrefix(k, seriesColumnPrefix) || len(k) == len(seriesColumnPrefix) {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(scalarString(v)), 64)
		if err != nil {
			continue
		}
		if values == nil {
			values = map[string]float64{}
		}
		values[k[len(seriesColumnPrefix):]] = f
	}
	return values
}

// ---------------------------------------------------------------------------
// Safety moderator bridge
// ---------------------------------------------------------------------------

type moderatorAdapter struct{ m safety.Moderator }

// NewModerator adapts the heuristic safety moderator to ports.Moderator.
func NewModerator() ports.Moderator {
	return &moderatorAdapter{m: safety.NewHeuristicModerator()}
}

func (a *moderatorAdapter) CheckInbound(ctx context.Context, text string) (bool, string) {
	v := a.m.Moderate(ctx, safety.StageInput, text)
	if v.Decision == safety.DecisionAllow {
		return true, ""
	}
	return false, moderatorMessage(v)
}

func (a *moderatorAdapter) CheckOutbound(ctx context.Context, text string) (bool, string) {
	v := a.m.Moderate(ctx, safety.StageOutput, text)
	if v.Decision == safety.DecisionAllow {
		return true, ""
	}
	return false, moderatorMessage(v)
}

func moderatorMessage(v safety.Verdict) string {
	if strings.TrimSpace(v.UserMessage) != "" {
		return v.UserMessage
	}
	return "I can only answer questions about your Mesha operations."
}

// ---------------------------------------------------------------------------
// Conversation store bridge
// ---------------------------------------------------------------------------

type conversationStoreAdapter struct {
	store *persistence.PostgresConversationStore
}

// NewConversationStore adapts the Postgres conversation store to
// ports.ConversationStore. Returns nil when pool is nil.
func NewConversationStore(pool *pgxpool.Pool, timeout time.Duration) ports.ConversationStore {
	if pool == nil {
		return nil
	}
	return &conversationStoreAdapter{store: persistence.NewPostgresConversationStore(pool, timeout)}
}

// NewConversationHTTPStore exposes the full Postgres conversation store to the
// HTTP thread surface (list/create/rename/delete/messages). Returns a nil
// interface when pool is nil so the routes degrade to 503 rather than panic.
func NewConversationHTTPStore(pool *pgxpool.Pool, timeout time.Duration) ceohttp.ConvStore {
	if pool == nil {
		return nil
	}
	return persistence.NewPostgresConversationStore(pool, timeout)
}

func (a *conversationStoreAdapter) EnsureConversation(ctx context.Context, actor domain.Actor, conversationID, firstQuestion string) (string, string, error) {
	if strings.TrimSpace(conversationID) != "" {
		c, err := a.store.Get(ctx, actor.TenantID, actor.UserID, conversationID)
		if err == nil {
			return c.ID, c.Title, nil
		}
		// fall through to create when not found / expired
	}
	title := clampTitle(firstQuestion)
	c, _, err := a.store.Create(ctx, persistence.NewConversation{
		TenantID: actor.TenantID,
		ActorID:  actor.UserID,
		Title:    title,
	})
	if err != nil {
		return "", "", err
	}
	return c.ID, c.Title, nil
}

func (a *conversationStoreAdapter) AppendTurn(ctx context.Context, actor domain.Actor, conversationID string, turn domain.Turn) error {
	role := persistence.RoleUser
	if turn.Role == "assistant" {
		role = persistence.RoleAssistant
	}
	_, err := a.store.AppendMessage(ctx, persistence.NewMessage{
		ConversationID: conversationID,
		TenantID:       actor.TenantID,
		ActorID:        actor.UserID,
		Role:           role,
		Content:        turn.Content,
		Source:         turn.Source,
		Mode:           string(turn.Mode),
		RequestID:      turn.RequestID,
	})
	return err
}

func (a *conversationStoreAdapter) RecentTurns(ctx context.Context, actor domain.Actor, conversationID string, n int) ([]domain.Turn, error) {
	if n <= 0 {
		n = 10
	}
	page, err := a.store.ListMessages(ctx, persistence.ListMessagesQuery{
		ConversationID: conversationID,
		TenantID:       actor.TenantID,
		ActorID:        actor.UserID,
		PageSize:       n,
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Turn, 0, len(page.Items))
	for _, m := range page.Items {
		out = append(out, domain.Turn{
			Role:      string(m.Role),
			Content:   m.Content,
			Source:    m.Source,
			Mode:      domain.Mode(m.Mode),
			RequestID: m.RequestID,
			CreatedAt: m.CreatedAt,
		})
	}
	return out, nil
}

func clampTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "New conversation"
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

// ---------------------------------------------------------------------------
// MCP Toolbox bridge
// ---------------------------------------------------------------------------

type toolboxAdapter struct {
	client *toolboxclient.Client
}

// NewToolbox adapts the MCP toolbox client to ports.Toolbox from env
// (MESHA_MCP_TOOLBOX_URL + MESHA_MCP_TOOLSET). Returns nil when unconfigured.
func NewToolbox(log *slog.Logger) ports.Toolbox {
	base := os.Getenv("MESHA_MCP_TOOLBOX_URL")
	toolset := envOr("MESHA_MCP_TOOLSET", "mesha_ceo_toolset")
	if strings.TrimSpace(base) == "" {
		return nil
	}
	c, err := toolboxclient.New(toolboxclient.Config{
		BaseURL:   base,
		Toolset:   toolset,
		AuthToken: os.Getenv("MESHA_MCP_TOOLBOX_AUTH_TOKEN"),
	})
	if err != nil {
		if log != nil {
			log.Warn("ceoai: toolbox unavailable", "err", err.Error())
		}
		return nil
	}
	return &toolboxAdapter{client: c}
}

func (a *toolboxAdapter) Tools(ctx context.Context) ([]ports.ToolSpec, error) {
	ts, err := a.client.LoadToolset(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ports.ToolSpec, 0, len(ts.Tools))
	for name, t := range ts.Tools {
		params := make([]string, 0, len(t.Parameters))
		for _, p := range t.Parameters {
			params = append(params, p.Name)
		}
		out = append(out, ports.ToolSpec{
			Name:        name,
			Route:       domain.RouteToolbox,
			Description: t.Description,
			Params:      params,
		})
	}
	return out, nil
}

func (a *toolboxAdapter) Call(ctx context.Context, actor domain.Actor, tool string, params map[string]any) (domain.ToolResult, error) {
	// Inject the session-bound tenant; never trust a tenant from params/user text.
	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		if strings.EqualFold(k, "tenant_id") {
			continue
		}
		merged[k] = v
	}
	merged["tenant_id"] = actor.TenantID

	raw, err := a.client.Invoke(ctx, tool, merged)
	if err != nil {
		return domain.ToolResult{}, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return domain.ToolResult{}, fmt.Errorf("toolbox: decode rows for %q: %w", tool, err)
	}
	tr := domain.ToolResult{
		Route:    domain.RouteToolbox,
		ToolName: tool,
		// User-facing source label: a clean business phrase, not the "MCP Toolbox ·
		// <raw tool id>" plumbing. The tool id + toolbox route stay in the audit
		// (ToolName + Route above).
		Surface: "Mesha operational data",
		AsOf:    time.Now(),
	}
	for _, row := range rows {
		for k, v := range row {
			// On this route the label IS the source column name, which is the
			// most precise description of the measure anything downstream
			// gets: `weight_kg`, `price_per_kg`, `coverage_ratio`.
			tr.Facts = append(tr.Facts, domain.Fact{
				TenantID: actor.TenantID,
				Label:    k,
				Value:    readableFigure(canonicalTagText(scalarString(v)), "", k),
			})
		}
	}
	return tr, nil
}
