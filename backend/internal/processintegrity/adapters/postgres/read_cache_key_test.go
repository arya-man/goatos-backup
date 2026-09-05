package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// The read cache was dead: the handler builds AsOf from time.Now() and
// DueBefore from AsOf+30d, and the key formatted both with RFC3339Nano, so two
// requests a millisecond apart never shared a key. This is the regression that
// fails on the un-bucketed key and passes on the bucketed one.
func TestReadCacheKeyIsStableAcrossRequestsWithinABucket(t *testing.T) {
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	q := func(now time.Time) domain.Query {
		return domain.Query{
			TenantID:  "00000000-0000-4000-8000-000000000001",
			AsOf:      now,
			DueBefore: now.Add(30 * 24 * time.Hour),
			Limit:     100,
		}
	}

	first := processIntegrityReadCacheKey("rows", q(base))
	// A second request 900ms later — a realistic gap between two clicks, and the
	// exact case the 60s TTL exists to serve.
	second := processIntegrityReadCacheKey("rows", q(base.Add(900*time.Millisecond)))
	if first != second {
		t.Fatalf("cache key must be stable within a bucket, so the 60s read cache can hit:\n first=%s\nsecond=%s", first, second)
	}
}

// The bucket must not defeat the TTL: two requests far enough apart still get
// distinct keys, so a stale entry cannot be served indefinitely.
func TestReadCacheKeySeparatesDistantRequests(t *testing.T) {
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	q := func(now time.Time) domain.Query {
		return domain.Query{TenantID: "t", AsOf: now, DueBefore: now.Add(30 * 24 * time.Hour), Limit: 100}
	}
	if processIntegrityReadCacheKey("rows", q(base)) == processIntegrityReadCacheKey("rows", q(base.Add(2*processIntegrityReadCacheBucket))) {
		t.Fatal("keys two buckets apart must differ")
	}
}

// Bucketing the clock must not collapse queries that differ in any other way.
func TestReadCacheKeyStillSeparatesDistinctQueries(t *testing.T) {
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	a := domain.Query{TenantID: "t1", AsOf: base, DueBefore: base.Add(time.Hour), Limit: 100}
	b := domain.Query{TenantID: "t2", AsOf: base, DueBefore: base.Add(time.Hour), Limit: 100}
	if processIntegrityReadCacheKey("rows", a) == processIntegrityReadCacheKey("rows", b) {
		t.Fatal("different tenants must not share a cache key")
	}
	c := a
	c.Limit = 50
	if processIntegrityReadCacheKey("rows", a) == processIntegrityReadCacheKey("rows", c) {
		t.Fatal("different limits must not share a cache key")
	}
}
