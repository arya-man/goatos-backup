package safety

import (
	"testing"
	"time"
)

func TestLimiterPerActorTrips(t *testing.T) {
	l := NewLimiter(LimiterConfig{PerActorPerWindow: 3, PerTenantPerWindow: 100, Window: time.Minute})
	id := Identity{TenantID: "t1", ActorID: "a1"}
	for i := 0; i < 3; i++ {
		if v := l.Check(id); !v.Allowed() {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	v := l.Check(id)
	if v.Decision != DecisionThrottle {
		t.Fatalf("4th request should throttle, got %s", v.Decision)
	}
	if v.Reason != "rate_limit:actor" {
		t.Fatalf("expected actor reason, got %s", v.Reason)
	}
	if v.RetryAfter <= 0 {
		t.Fatal("throttle must carry a RetryAfter hint")
	}
}

func TestLimiterPerTenantTrips(t *testing.T) {
	// Generous per-actor, tight per-tenant; two actors share the tenant bucket.
	l := NewLimiter(LimiterConfig{PerActorPerWindow: 100, PerTenantPerWindow: 4, Window: time.Minute})
	a1 := Identity{TenantID: "t1", ActorID: "a1"}
	a2 := Identity{TenantID: "t1", ActorID: "a2"}
	seq := []Identity{a1, a2, a1, a2}
	for i, id := range seq {
		if v := l.Check(id); !v.Allowed() {
			t.Fatalf("tenant request %d should be allowed", i)
		}
	}
	if v := l.Check(a1); v.Reason != "rate_limit:tenant" {
		t.Fatalf("5th tenant request should throttle on tenant, got %s/%s", v.Decision, v.Reason)
	}
}

func TestLimiterIsolatesActors(t *testing.T) {
	l := NewLimiter(LimiterConfig{PerActorPerWindow: 1, PerTenantPerWindow: 100, Window: time.Minute})
	a1 := Identity{TenantID: "t1", ActorID: "a1"}
	a2 := Identity{TenantID: "t1", ActorID: "a2"}
	if v := l.Check(a1); !v.Allowed() {
		t.Fatal("a1 first should pass")
	}
	if v := l.Check(a1); v.Allowed() {
		t.Fatal("a1 second should throttle")
	}
	if v := l.Check(a2); !v.Allowed() {
		t.Fatal("a2 must not be affected by a1's limit")
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	if got := RetryAfterSeconds(Verdict{RetryAfter: 90 * time.Second}); got != 90 {
		t.Fatalf("want 90, got %d", got)
	}
	if got := RetryAfterSeconds(Verdict{RetryAfter: 0}); got != 1 {
		t.Fatalf("zero should floor to 1, got %d", got)
	}
	if got := RetryAfterSeconds(Verdict{RetryAfter: 200 * time.Millisecond}); got != 1 {
		t.Fatalf("sub-second should floor to 1, got %d", got)
	}
}
