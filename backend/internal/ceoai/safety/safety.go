// Package safety is the CEO-AI assistant's safety, abuse, and cost layer.
//
// It is the server-side boundary that every leadership-assistant request must
// pass through before any model call, tool call, or SQL executes. It provides,
// as small composable ports:
//
//   - Prompt-injection defense (injection.go): user text is DATA, never
//     instructions; tenant/role/tool-allowlist can never be changed by user
//     text.
//   - Content moderation + Vertex safety settings (moderation.go): screens
//     input and output for unsafe/abusive/off-topic content.
//   - Rate limiting (ratelimit.go): per-tenant and per-actor token buckets.
//   - Cost/token budgets (budget.go): per-request and per-actor/day caps.
//   - Circuit breaker (breaker.go): deterministic degraded mode when Vertex or
//     a dependency is failing/slow.
//   - Concurrency/backpressure (concurrency.go): bounded in-flight work, no
//     unbounded goroutines.
//   - Data retention (retention.go): purges expired stored conversations.
//
// # Design rules (from the CEO-AI build contract)
//
//   - The assistant is READ-ONLY over business data.
//   - Tenant and role scope come ONLY from the server-side session, encoded in
//     Identity. Nothing in this package reads scope from user text.
//   - Errors are never swallowed into an empty answer: every guard returns a
//     typed decision the caller must render honestly.
//   - No secrets are logged. Actor identity is sensitive; goat RFID/tags are
//     not PII.
//
// The Layer facade (layer.go) wires these together for the orchestrator.
package safety

import (
	"errors"
	"strings"
	"time"
)

// Identity is the server-session-derived caller scope. It is the ONLY source of
// tenant and role for the assistant. It is constructed from the authenticated
// session, never from user-supplied text.
type Identity struct {
	TenantID string
	ActorID  string
	Role     string
}

// Valid reports whether the identity carries the minimum server-side scope
// required to serve a leadership request.
func (i Identity) Valid() bool {
	return strings.TrimSpace(i.TenantID) != "" && strings.TrimSpace(i.ActorID) != ""
}

// key is the composite bucket key for rate-limit and budget accounting. It is
// never derived from user text.
func (i Identity) key() string {
	return i.TenantID + ":" + i.ActorID
}

// Decision is the outcome of a safety check the caller must honor.
type Decision int

const (
	// DecisionAllow permits the request to proceed to the next stage.
	DecisionAllow Decision = iota
	// DecisionRefuse means the request was rejected for a safety/policy reason
	// and the caller must return a scoped refusal answer (never an empty one).
	DecisionRefuse
	// DecisionThrottle means the caller exceeded a rate or budget limit and
	// should back off; RetryAfter carries the hint.
	DecisionThrottle
	// DecisionDegrade means a dependency is unavailable and the caller must
	// serve an honest degraded-mode answer.
	DecisionDegrade
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionRefuse:
		return "refuse"
	case DecisionThrottle:
		return "throttle"
	case DecisionDegrade:
		return "degrade"
	default:
		return "unknown"
	}
}

// Verdict is a structured safety result. Reason is a stable, non-sensitive slug
// suitable for logging and metrics; UserMessage is a leadership-appropriate
// refusal/throttle string safe to surface in the chat answer.
type Verdict struct {
	Decision    Decision
	Reason      string
	UserMessage string
	// RetryAfter is set for DecisionThrottle.
	RetryAfter time.Duration
}

// Allowed reports whether the verdict permits proceeding.
func (v Verdict) Allowed() bool { return v.Decision == DecisionAllow }

func allow() Verdict { return Verdict{Decision: DecisionAllow, Reason: "ok"} }

// Sentinel errors for programmatic handling by the orchestrator.
var (
	// ErrInvalidIdentity is returned when a request arrives without a valid
	// server-side tenant/actor scope.
	ErrInvalidIdentity = errors.New("safety: invalid or missing session identity")
	// ErrRateLimited is returned when a per-tenant/per-actor rate bucket is
	// exhausted.
	ErrRateLimited = errors.New("safety: rate limit exceeded")
	// ErrOverBudget is returned when a token/cost cap is exceeded.
	ErrOverBudget = errors.New("safety: token/cost budget exceeded")
	// ErrBreakerOpen is returned when a dependency circuit breaker is open.
	ErrBreakerOpen = errors.New("safety: circuit breaker open")
	// ErrOverloaded is returned when concurrency backpressure sheds the request.
	ErrOverloaded = errors.New("safety: server busy")
	// ErrUnsafeContent is returned when moderation blocks input or output.
	ErrUnsafeContent = errors.New("safety: content blocked by moderation")
	// ErrInjection is returned when a prompt-injection override attempt is
	// detected in a context that must reject it.
	ErrInjection = errors.New("safety: prompt-injection attempt detected")
)

// Clock is an injectable time source so every timing-dependent guard is
// deterministically testable.
type Clock func() time.Time

func (c Clock) now() time.Time {
	if c == nil {
		return time.Now()
	}
	return c()
}
