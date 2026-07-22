// Package ports declares the interfaces the ceoai app orchestrates. Adapters
// (Vertex, Cube/cubeclient, MCP toolboxclient, sqlguard, persistence, safety,
// in-process read services) implement these. Defining the ports here keeps the
// core independent of any concrete adapter (hexagonal dependency inversion).
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// Planner turns a scoped Question into a Plan (decompose + Cube-first route +
// param extraction). It NEVER executes SQL, holds DB creds, or decides
// permissions. Implementations: adapters/vertex (Gemini) and
// adapters/keywordplanner (deterministic fallback).
type Planner interface {
	Plan(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ToolSpec) (domain.Plan, error)
}

// Reviewer is the optional Gemini critic gated by MESHA_AI_REVIEW. It judges
// groundedness of a drafted answer against the tool facts. A nil/absent
// Reviewer means the deterministic reviewer in app is authoritative.
type Reviewer interface {
	Critique(ctx context.Context, answer string, facts []domain.Fact) (grounded bool, reason string, err error)
}

// AIProvider bundles the model-backed capabilities so the app can swap Vertex
// for a fallback or a future Agent Engine runtime behind one seam.
type AIProvider interface {
	Planner
	// PlannedByModel reports whether this provider is the real model planner
	// (true) or the deterministic fallback (false). Drives Answer.Mode +
	// Citation.PlannedByModel.
	PlannedByModel() bool
}

// ToolSpec is the model-facing description of a callable tool/metric.
type ToolSpec struct {
	Name        string
	Route       domain.Route
	Description string
	// Params documents accepted, server-validated argument names.
	Params []string
}

// ToolExecutor runs one resolved sub-question against its tier and returns
// groundable facts. Tenant scope is injected from the Actor, never from params.
type ToolExecutor interface {
	Spec() ToolSpec
	Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error)
}

// MetricService is the Cube governed-metric port (adapter = cubeclient). The
// planner picks metric+dimensions+timeRange; MetricService returns the official
// number plus its governance status. Browser never calls Cube directly.
type MetricService interface {
	// Metrics lists the governed metric catalog (name, dims, status) so the
	// planner can prefer Cube for any official KPI.
	Metrics(ctx context.Context) ([]MetricSpec, error)
	Query(ctx context.Context, actor domain.Actor, req MetricQuery) (domain.ToolResult, error)
}

// MetricSpec is one governed Cube metric.
type MetricSpec struct {
	Name       string
	Status     domain.MetricStatus
	Dimensions []string
	TimeGrains []string
	Title      string
}

// MetricQuery is a governed metric request (NOT SQL).
type MetricQuery struct {
	Metric     string
	Dimensions []string
	TimeRange  string
	Filters    map[string]string
}

// SQLFallback is the sqlguard-validated read-only executor (tier 4). It rejects
// non-SELECT/multi-statement/DDL and enforces the ceo_ai.* allowlist + a
// mandatory tenant predicate before touching mesha_ceo_readonly.
type SQLFallback interface {
	Execute(ctx context.Context, actor domain.Actor, sql string, args []any) (domain.ToolResult, error)
}

// Toolbox is the MCP Toolbox curated-tool port (adapter = toolboxclient).
type Toolbox interface {
	Tools(ctx context.Context) ([]ToolSpec, error)
	Call(ctx context.Context, actor domain.Actor, tool string, params map[string]any) (domain.ToolResult, error)
}

// Moderator is the safety port (adapter = safety): pre-check the inbound
// question and post-check the outbound answer for off-domain / unsafe content.
type Moderator interface {
	CheckInbound(ctx context.Context, text string) (allow bool, refusal string)
	CheckOutbound(ctx context.Context, text string) (allow bool, refusal string)
}

// ConversationStore is durable thread persistence (adapter = persistence).
type ConversationStore interface {
	EnsureConversation(ctx context.Context, actor domain.Actor, conversationID, firstQuestion string) (id string, title string, err error)
	AppendTurn(ctx context.Context, actor domain.Actor, conversationID string, turn domain.Turn) error
	RecentTurns(ctx context.Context, actor domain.Actor, conversationID string, n int) ([]domain.Turn, error)
}

// MemoryStore is bounded session memory of resolved entities for follow-ups.
type MemoryStore interface {
	Recall(ctx context.Context, actor domain.Actor, conversationID string) ([]domain.ResolvedEntities, error)
	Remember(ctx context.Context, actor domain.Actor, conversationID string, ent domain.ResolvedEntities) error
}

// Cache stores plans/answers keyed by (tenant, normalized question, as_of
// bucket). Never cache across tenants.
type Cache interface {
	Get(key string) (domain.Answer, bool)
	Set(key string, ans domain.Answer)
}

// RateLimiter enforces per (tenant,user) request rate.
type RateLimiter interface {
	Allow(tenantID, userID string) bool
}

// Budget enforces per-tenant/day and per-user/day token+cost caps.
type Budget interface {
	// Reserve reports whether the request may proceed and, if not, a
	// friendly over-budget message (never a silent empty answer).
	Reserve(ctx context.Context, actor domain.Actor) (ok bool, overBudgetMsg string)
	Record(ctx context.Context, actor domain.Actor, inputTokens, outputTokens int)
}

// AuditSink persists one tamper-evident audit row per request + the internal
// step trace and review verdict. Actor identity is sensitive; never leak it to
// the user answer. Goat RFID/tags are not PII.
type AuditSink interface {
	Record(ctx context.Context, rec AuditRecord) error
}

// Telemetry is the observability metric-emission port. The orchestrator calls
// it at pipeline boundaries so the assistant_* OTel instruments fire on a live
// request (adapter = adapters/observability.Metrics). Labels are bounded,
// low-cardinality enums only — never tenant_id, actor, request_id, or question
// text. A nil Telemetry is a valid no-op so an unwired boot never nil-panics.
type Telemetry interface {
	// RecordRequest counts one terminal request and records its latency. tool
	// is the resolved route/tier label; status is the terminal outcome
	// (ok|rejected|error|degraded).
	RecordRequest(ctx context.Context, tool, status string, latencyMS float64)
	// RecordToolRows records how many rows the grounding read tool returned.
	RecordToolRows(ctx context.Context, tool string, rows int)
	// VertexFailover counts one planner failover to the keyword planner.
	VertexFailover(ctx context.Context)
	// RateLimitTrip counts one rate-limiter rejection.
	RateLimitTrip(ctx context.Context)
	// BudgetRejection counts one budget-cap rejection.
	BudgetRejection(ctx context.Context)
	// CacheLookup counts one response-cache lookup (hit or miss).
	CacheLookup(ctx context.Context, hit bool)
	// ReviewCorrection counts one answer the runtime review downgraded.
	ReviewCorrection(ctx context.Context)
	// InjectionBlocked counts one blocked scope-override / injection attempt.
	InjectionBlocked(ctx context.Context)
}

// AuditRecord is the internal-only per-request audit payload.
type AuditRecord struct {
	RequestID      string
	TenantID       string
	ActorID        string
	ConversationID string
	QuestionHash   string
	Mode           domain.Mode
	Routes         []domain.Route
	ToolsCalled    []string
	RowCount       int
	LatencyMS      int64
	Steps          []domain.StepTrace
	Review         domain.ReviewVerdict
	ModelVersion   string
	PromptVersion  string
}
