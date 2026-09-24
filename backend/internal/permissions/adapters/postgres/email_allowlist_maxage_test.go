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
