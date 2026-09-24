package postgres

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

const maxAgeTestTenant = "20000000-0000-4000-8000-000000000001"

func seededUnreachableSource(t *testing.T, loadedAgo time.Duration) *AllowedEmailSource {
	t.Helper()
	src := NewAllowedEmailSource(unreachablePool(t, "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"),
		time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	now := time.Now()
	src.now = func() time.Time { return now }
	src.byTenant[maxAgeTestTenant] = tenantEmailCache{
		emails:   map[string]struct{}{"a@mesha.sg": {}},
		loadedAt: now.Add(-loadedAgo),
	}
	return src
}

// A transient reload failure still serves the last-known-good set.
func TestAllowedEmailSourceServesRecentLastKnownGoodOnReloadFailure(t *testing.T) {
	src := seededUnreachableSource(t, time.Minute)
	allowed, err := src.EmailAllowedErr(context.Background(), maxAgeTestTenant, "a@mesha.sg")
	if !allowed || err != nil {
		t.Fatalf("allowed=%v err=%v; want last-known-good allow", allowed, err)
	}
}

// Past the hard max age a failed reload must not keep allowing an email that
// may have been revoked since: it reports the load error (503 path) instead.
func TestAllowedEmailSourceRefusesLastKnownGoodPastMaxAge(t *testing.T) {
	src := seededUnreachableSource(t, allowlistMaxStaleAge+time.Second)
	allowed, err := src.EmailAllowedErr(context.Background(), maxAgeTestTenant, "a@mesha.sg")
	if allowed || err == nil {
		t.Fatalf("allowed=%v err=%v; want refusal with load error past max age", allowed, err)
	}
	if src.EmailAllowed(context.Background(), maxAgeTestTenant, "a@mesha.sg") {
		t.Fatal("EmailAllowed must fail closed past max age")
	}
}

// Past max age during an outage, a failed reload backs off briefly: the next
// requests fail closed immediately instead of re-querying the saturated pool.
func TestAllowedEmailSourceBacksOffAfterFailedReloadPastMaxAge(t *testing.T) {
	src := NewAllowedEmailSource(unreachablePool(t, "postgres://u:p@10.255.255.1:5432/none?sslmode=disable&connect_timeout=30"),
		300*time.Millisecond, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	now := time.Now()
	src.now = func() time.Time { return now }
	src.byTenant[maxAgeTestTenant] = tenantEmailCache{
		emails:   map[string]struct{}{"a@mesha.sg": {}},
		loadedAt: now.Add(-allowlistMaxStaleAge - time.Second),
	}
	if allowed, err := src.EmailAllowedErr(context.Background(), maxAgeTestTenant, "a@mesha.sg"); allowed || err == nil {
		t.Fatalf("first: allowed=%v err=%v; want refusal", allowed, err)
	}
	start := time.Now()
	allowed, err := src.EmailAllowedErr(context.Background(), maxAgeTestTenant, "a@mesha.sg")
	if allowed || err == nil {
		t.Fatalf("backoff: allowed=%v err=%v; want fail-closed error", allowed, err)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("within backoff the pool was queried again (took %s)", took)
	}
	// After the backoff the source tries the database again.
	now = now.Add(allowlistFailureBackoff + time.Millisecond)
	start = time.Now()
	_, _ = src.EmailAllowedErr(context.Background(), maxAgeTestTenant, "a@mesha.sg")
	if took := time.Since(start); took < 200*time.Millisecond {
		t.Fatalf("after backoff the source must retry the load (took %s)", took)
	}
}
