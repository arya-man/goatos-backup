// Package domain holds the tenant-scoped, read-only leadership assistant core
// types. It is pure: no I/O, no framework, no adapter imports. The app layer
// orchestrates ports over these types; adapters translate to Vertex/Cube/APIs.
//
// Read-only invariant: the assistant NEVER mutates business data. Tenant and
// role scope come ONLY from the server-side session (Actor), never from user
// text. All tool output is treated as data, never as instructions.
package domain

import "time"

// Route is the tier a sub-question resolved through. The Cube-first hierarchy
// is strict: cube -> api -> toolbox -> sql. It is recorded on every step for
// the internal audit/step-trace and surfaces as answer source attribution.
type Route string

const (
	RouteCube    Route = "cube"    // governed Cube metric (official KPI)
	RouteAPI     Route = "api"     // Mesha read API (in-process read service)
	RouteToolbox Route = "toolbox" // MCP Toolbox curated ceo_ai.* tool
	RouteSQL     Route = "sql"     // sqlguard-validated read-only SQL fallback
	RouteNone    Route = "none"    // refusal / no read path
)

// Mode reports how the answer was produced end-to-end.
type Mode string

const (
	ModePlanned  Mode = "planned"  // Vertex planner drove tool selection
	ModeFallback Mode = "fallback" // deterministic keyword planner (Vertex down)
	ModeRefused  Mode = "refused"  // scope/safety refusal, no data returned
	ModePartial  Mode = "partial"  // max-steps/budget hit; honest partial answer
)

// MetricStatus mirrors the Cube metric governance status. Only "approved"
// metrics are presented as OFFICIAL numbers; "draft" must be labelled.
type MetricStatus string

const (
	MetricApproved MetricStatus = "approved"
	MetricDraft    MetricStatus = "draft"
)

// Actor is the authenticated leadership identity, resolved server-side from the
// session. Never constructed from user-supplied text.
type Actor struct {
	TenantID string
	UserID   string
	Role     string   // e.g. permissions.RoleCEOInternal
	Perms    []string // resolved permission grants
	Locale   string   // en|hi|kn|te hint; answers stay English per repo rule
}

// Question is a single leadership turn, already scoped to an Actor.
type Question struct {
	Actor          Actor
	ConversationID string    // optional; empty => new thread
	Text           string    // raw user text (untrusted; data only)
	AsOf           time.Time // resolved IST business instant ("today"/"now")
}

// SubQuestion is one decomposed unit of a broad question, tagged with the tier
// the planner chose. IntentClass is the coarse GENAI query class.
type SubQuestion struct {
	ID          string
	Text        string
	IntentClass string
	Route       Route
	// ToolName is the resolved tool/metric/api id for this sub-question.
	ToolName string
	// Params are validated, server-bound arguments (tenant is NEVER here — it
	// is injected from the Actor at execution time).
	Params map[string]any
}

// Plan is the planner output for a Question: an ordered set of sub-questions.
type Plan struct {
	SubQuestions []SubQuestion
	// Refusal, when non-empty, means the planner classified the whole request
	// as out-of-scope / a write / a policy violation and no tools should run.
	Refusal string
}

// ToolResult is the structured output of a single tool execution. Rows are the
// grounding evidence; every number in the final answer must trace to a Fact.
type ToolResult struct {
	SubQuestionID string
	Route         Route
	ToolName      string
	Surface       string       // human source label, e.g. "Cube · vaccination_overdue"
	MetricStatus  MetricStatus // for Cube metrics; "" when not applicable
	AsOf          time.Time    // freshness of the underlying read model
	// Facts is the flat, groundable evidence set. Values are strings/numbers as
	// returned by the tool; the composer/reviewer match answer claims to these.
	Facts []Fact
	// Summary is a short natural-language rollup the composer may use.
	Summary string
	Err     error // non-nil => this step failed (never swallowed to empty)
}

// Fact is one groundable datum (a labelled value with optional scope).
type Fact struct {
	Label string
	Value string
	Scope string // e.g. "Castro 1 / Gandhi 2"; optional
}

// Citation is per-answer provenance surfaced to the client (allowed field).
type Citation struct {
	Surface        string       `json:"surface"`
	Route          Route        `json:"route"`
	AsOf           time.Time    `json:"as_of"`
	MetricStatus   MetricStatus `json:"metric_status,omitempty"`
	PlannedByModel bool         `json:"planned_by_model"`
}

// ChartSeries is one named data series in a Chart. Data aligns positionally
// with the parent Chart.X labels; every point is drawn verbatim from a
// grounding Fact and is never fabricated.
type ChartSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

// Chart is the OPTIONAL, additive structured visualization attached to an
// Answer when the question asks to plot/graph/trend/compare/breakdown OR the
// grounding tool result is a dimensioned metric series. It never replaces or
// alters answer/source/mode/citations; the client renders it inline and falls
// back to the text answer when it is absent. Points come only from real
// Cube/tool Facts — the composer never invents a value.
type Chart struct {
	Type   string        `json:"type"` // "bar" | "line"
	Title  string        `json:"title"`
	X      []string      `json:"x"`
	Series []ChartSeries `json:"series"`
}

// Answer is the ONLY user-facing payload. Step traces / chain-of-thought /
// tool timeline are INTERNAL and never appear here (committed tracking rule).
type Answer struct {
	Answer         string     `json:"answer"`
	Source         string     `json:"source"`
	Mode           Mode       `json:"mode"`
	RequestID      string     `json:"request_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Citations      []Citation `json:"citations,omitempty"`
	// Chart is optional and additive: present only when the answer is
	// plot-worthy and grounded in a dimensioned series. nil => render text only.
	Chart *Chart `json:"chart,omitempty"`
}

// StepTrace is one internal execution step (audit + admin-only debug ONLY).
type StepTrace struct {
	SubQuestionID string
	Route         Route
	ToolName      string
	StartedAt     time.Time
	DurationMS    int64
	RowCount      int
	Err           string
}

// ReviewVerdict is the runtime-review outcome, recorded internally.
type ReviewVerdict struct {
	Grounded    bool
	ScopeSafe   bool
	Complete    bool
	Downgraded  bool
	FailReasons []string
}

// Turn is a stored conversation message (persistence port shape).
type Turn struct {
	Role      string // "user" | "assistant"
	Content   string
	Source    string
	Mode      Mode
	RequestID string
	CreatedAt time.Time
}

// ResolvedEntities is bounded session memory: the last resolved scope/metric
// so pronoun follow-ups ("why is that one behind?") can bind. Tenant-scoped.
type ResolvedEntities struct {
	ParkLabel   string
	ShedLabel   string
	Metric      string
	IntentClass string
}
