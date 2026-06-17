package authaudit

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimitPerWindow(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(2, time.Minute, 16)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("actor|tenant|ip") {
		t.Fatal("first request denied")
	}
	if !limiter.Allow("actor|tenant|ip") {
		t.Fatal("second request denied")
	}
	if limiter.Allow("actor|tenant|ip") {
		t.Fatal("third request allowed")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow("actor|tenant|ip") {
		t.Fatal("request after window reset denied")
	}
}

func TestRateLimiterBoundsKeyCount(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(1, time.Hour, 2)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("a") || !limiter.Allow("b") || !limiter.Allow("c") {
		t.Fatal("new keys should be allowed")
	}
	if len(limiter.buckets) > 2 {
		t.Fatalf("bucket count=%d want <= 2", len(limiter.buckets))
	}
}
