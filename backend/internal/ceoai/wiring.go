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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ceohttp "github.com/vgoats/goatos/backend/internal/ceoai/adapters/http"
	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/vertex"
	"github.com/vgoats/goatos/backend/internal/ceoai/cubeclient"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/persistence"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
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
		Model:    envOr("MESHA_VERTEX_MODEL", "gemini-2.5-flash"),
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
}

// cubeDimensionMembers maps a plain dimension/filter key to its view member
// suffix. park_label/shed_label/species exist on every leadership view.
var cubeDimensionMembers = map[string]string{
	"species":    "species",
	"park_label": "park_label",
	"shed_label": "shed_label",
	"park":       "park_label",
	"shed":       "shed_label",
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
			Dimensions: []string{"species", "park_label", "shed_label"},
			TimeGrains: grains,
			Title:      b.title,
		})
	}
	return out, nil
}

func (s *cubeMetricService) Query(ctx context.Context, actor domain.Actor, req ports.MetricQuery) (domain.ToolResult, error) {
	b, ok := cubeMetricBindings[req.Metric]
	if !ok {
		return domain.ToolResult{}, fmt.Errorf("cube: unknown governed metric %q", req.Metric)
	}
	member := b.view + "." + b.measure

	q := cubeclient.Query{Measures: []string{member}}
	// Dimensions: group-by members (e.g. species split).
	for _, d := range req.Dimensions {
		if suf, ok := cubeDimensionMembers[strings.ToLower(strings.TrimSpace(d))]; ok {
			q.Dimensions = append(q.Dimensions, b.view+"."+suf)
		}
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
	for k, v := range req.Filters {
		suf, ok := cubeDimensionMembers[strings.ToLower(strings.TrimSpace(k))]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		q.Filters = append(q.Filters, cubeclient.Filter{
			Member:   b.view + "." + suf,
			Operator: "equals",
			Values:   []string{v},
		})
	}

	res, err := s.client.Load(ctx, actor.TenantID, q)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("cube: load %s: %w", member, err)
	}

	tr := domain.ToolResult{
		SubQuestionID: "",
		Route:         domain.RouteCube,
		ToolName:      req.Metric,
		Surface:       "Cube · " + req.Metric,
		MetricStatus:  b.status,
		AsOf:          time.Now(),
	}
	for _, row := range res.Rows {
		val := scalarString(row[member])
		scope := cubeRowScope(row, b.view, q.Dimensions)
		tr.Facts = append(tr.Facts, domain.Fact{
			Label: b.title,
			Value: val,
			Scope: scope,
		})
	}
	if len(tr.Facts) == 0 {
		tr.Facts = append(tr.Facts, domain.Fact{Label: b.title, Value: "0"})
	}
	return tr, nil
}

func cubeRowScope(row map[string]any, view string, dims []string) string {
	var parts []string
	for _, d := range dims {
		if v, ok := row[d]; ok {
			if s := scalarString(v); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " / ")
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
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
	tr := domain.ToolResult{
		Route:    domain.RouteSQL,
		ToolName: "sql_fallback",
		Surface:  "Mesha read-only SQL fallback",
		AsOf:     time.Now(),
	}
	for _, row := range rows {
		for k, v := range row {
			tr.Facts = append(tr.Facts, domain.Fact{Label: k, Value: scalarString(v)})
		}
	}
	return tr, nil
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

// NewFeedbackHTTPStore exposes the Postgres feedback store to the HTTP feedback
// route. Returns a nil interface when pool is nil.
func NewFeedbackHTTPStore(pool *pgxpool.Pool, timeout time.Duration) ceohttp.FeedbackStore {
	if pool == nil {
		return nil
	}
	return persistence.NewPostgresFeedbackStore(pool, timeout)
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
		Surface:  "Mesha MCP Toolbox · " + tool,
		AsOf:     time.Now(),
	}
	for _, row := range rows {
		for k, v := range row {
			tr.Facts = append(tr.Facts, domain.Fact{Label: k, Value: scalarString(v)})
		}
	}
	return tr, nil
}
