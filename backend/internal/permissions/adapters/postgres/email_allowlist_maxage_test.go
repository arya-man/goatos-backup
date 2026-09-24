package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
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
// requests fail closed immediately instead of re-querying the saturated pool,
// and the backoff error still reads as "database unavailable" (503, not 403).
func TestAllowedEmailSourceBacksOffAfterFailedReloadPastMaxAge(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	loads := 0
	fail := false
	outage := fmt.Errorf("pool: %w", context.DeadlineExceeded)
	src := NewAllowedEmailSourceWithLoader(func(context.Context, string) (map[string]struct{}, error) {
		loads++
		if fail {
			return nil, outage
		}
		return map[string]struct{}{"a@mesha.sg": {}}, nil
	}, func() time.Time { return now }, nil)
	ctx := context.Background()
	if ok, err := src.EmailAllowedErr(ctx, maxAgeTestTenant, "a@mesha.sg"); !ok || err != nil {
		t.Fatalf("warm: ok=%v err=%v", ok, err)
	}
	fail = true
	now = now.Add(allowlistMaxStaleAge + time.Second)
	if ok, err := src.EmailAllowedErr(ctx, maxAgeTestTenant, "a@mesha.sg"); ok || err == nil {
		t.Fatalf("first past max age: ok=%v err=%v; want refusal", ok, err)
	}
	if loads != 2 {
		t.Fatalf("loads=%d; want 2", loads)
	}
	now = now.Add(allowlistFailureBackoff / 2)
	ok, err := src.EmailAllowedErr(ctx, maxAgeTestTenant, "a@mesha.sg")
	if ok || err == nil {
		t.Fatalf("backoff: ok=%v err=%v; want fail-closed error", ok, err)
	}
	if loads != 2 {
		t.Fatalf("within backoff the loader ran again (loads=%d)", loads)
	}
	if !errors.Is(err, outage) || !httpmiddleware.DatabaseUnavailable(ctx, err) {
		t.Fatalf("backoff err %v must wrap the last reload error and read as DB-unavailable", err)
	}
	now = now.Add(allowlistFailureBackoff)
	_, _ = src.EmailAllowedErr(ctx, maxAgeTestTenant, "a@mesha.sg")
	if loads != 3 {
		t.Fatalf("after backoff the source must retry the load (loads=%d)", loads)
	}
}
