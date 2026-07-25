package safety

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
)

// Limiter enforces per-tenant AND per-actor request rate limits for the
// assistant. Both dimensions are checked: a single tenant cannot swamp the
// service through many actors, and a single actor cannot swamp it alone.
//
// It wraps the platform token-bucket (authaudit.RateLimiter) rather than
// re-implementing bucketing. Keys are derived ONLY from the server-session
// Identity, never from user text.
type Limiter struct {
	perActor  *authaudit.RateLimiter
	perTenant *authaudit.RateLimiter
	window    time.Duration
}

// LimiterConfig configures the two rate dimensions.
type LimiterConfig struct {
	// PerActorPerWindow is the max requests one actor may make per Window.
	PerActorPerWindow int
	// PerTenantPerWindow is the max requests one tenant may make per Window
	// across all its actors.
	PerTenantPerWindow int
	// Window is the rolling window both limits apply over.
	Window time.Duration
	// MaxKeys bounds the in-memory bucket map (LRU-drop beyond it).
	MaxKeys int
}

// DefaultLimiterConfig returns conservative leadership-assistant defaults:
// 20 questions/actor/minute, 120/tenant/minute.
func DefaultLimiterConfig() LimiterConfig {
	return LimiterConfig{
		PerActorPerWindow:  20,
		PerTenantPerWindow: 120,
		Window:             time.Minute,
		MaxKeys:            8192,
	}
}

// NewLimiter builds a Limiter. A nil-returning sub-limiter (misconfigured) is
// treated as "no limit" by the underlying bucket's nil-safe Allow.
func NewLimiter(cfg LimiterConfig) *Limiter {
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	return &Limiter{
		perActor:  authaudit.NewRateLimiter(cfg.PerActorPerWindow, cfg.Window, cfg.MaxKeys),
		perTenant: authaudit.NewRateLimiter(cfg.PerTenantPerWindow, cfg.Window, cfg.MaxKeys),
		window:    cfg.Window,
	}
}

// Check consumes one unit against both the actor and tenant buckets. It returns
// an allow verdict, or a DecisionThrottle verdict carrying RetryAfter.
//
// The actor bucket is checked first (cheap, most specific); if the actor is over
// limit we short-circuit without touching the tenant bucket, so a rejected actor
// request does not burn tenant budget.
func (l *Limiter) Check(id Identity) Verdict {
	if l == nil {
		return allow()
	}
	if !l.perActor.Allow("actor:" + id.key()) {
		return l.throttle("rate_limit:actor")
	}
	if !l.perTenant.Allow("tenant:" + id.TenantID) {
		return l.throttle("rate_limit:tenant")
	}
	return allow()
}

func (l *Limiter) throttle(reason string) Verdict {
	return Verdict{
		Decision:    DecisionThrottle,
		Reason:      reason,
		RetryAfter:  l.window,
		UserMessage: "You're sending questions faster than I can safely handle. Please wait a moment and try again.",
	}
}

// RetryAfterSeconds is a helper for the HTTP layer's Retry-After header.
func RetryAfterSeconds(v Verdict) int {
	if v.RetryAfter <= 0 {
		return 1
	}
	secs := int(v.RetryAfter / time.Second)
	if secs < 1 {
		return 1
	}
	return secs
}
