package bootstrap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type countingGrants struct {
	calls atomic.Int32
	fail  atomic.Bool
}

func (c *countingGrants) ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	c.calls.Add(1)
	return []string{"ceo_internal"}, nil
}

func (c *countingGrants) ActiveTenantGrants(ctx context.Context, userID, tenantID string) ([]permissions.ActiveGrant, error) {
	c.calls.Add(1)
	time.Sleep(20 * time.Millisecond)
	if c.fail.Load() {
		return nil, errors.New("db down")
	}
	return []permissions.ActiveGrant{{Role: "ceo_internal", ScopeType: "tenant", ScopeID: tenantID}}, nil
}

type countingPerson struct{ calls atomic.Int32 }

func (c *countingPerson) ResolvePermissions(ctx context.Context, tenantID, userID string) ([]string, bool, error) {
	c.calls.Add(1)
	return []string{"weighing.monitor"}, true, nil
}
func (c *countingPerson) ResolveParkScope(ctx context.Context, tenantID, userID string) (string, []string, bool, error) {
	c.calls.Add(1)
	return "parks", []string{"p1"}, true, nil
}
func (c *countingPerson) ResolveAccessSnapshot(ctx context.Context, tenantID, userID string) (httpmiddleware.PersonAccessSnapshot, bool, error) {
	c.calls.Add(1)
	time.Sleep(20 * time.Millisecond)
	return httpmiddleware.PersonAccessSnapshot{Permissions: []string{"weighing.monitor"}, ScopeMode: "parks", ParkIDs: []string{"p1"}}, true, nil
}

// A page load fires many requests for one person at once: the grant and person-access lookups
// run once per burst, not once per request.
func TestAuthLookupsCoalesceABurstToOneStatementEach(t *testing.T) {
	g, p := &countingGrants{}, &countingPerson{}
	cache := newAuthReadCache()
	cg, cp := cachedGrantSource{inner: g, cache: cache}, cachedPersonAccess{inner: p, cache: cache}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cg.ActiveTenantGrants(context.Background(), "u1", "t1"); err != nil {
				t.Error(err)
			}
			if _, _, err := cp.ResolveAccessSnapshot(context.Background(), "t1", "u1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if g.calls.Load() != 1 || p.calls.Load() != 1 {
		t.Fatalf("20 concurrent requests ran %d grant and %d person lookups, want 1 each", g.calls.Load(), p.calls.Load())
	}
}

// Revocation bound: an answer is never served past authAccessTTL, other users/tenants never share
// it, errors are never cached, and a caller mutating its slice cannot corrupt the cache.
func TestAuthLookupsExpireAreScopedAndNeverCacheErrors(t *testing.T) {
	g := &countingGrants{}
	cg := cachedGrantSource{inner: g, cache: newAuthReadCache()}
	ctx := context.Background()
	got, _ := cg.ActiveTenantGrants(ctx, "u1", "t1")
	got[0].Role = "mutated"
	again, _ := cg.ActiveTenantGrants(ctx, "u1", "t1")
	if again[0].Role != "ceo_internal" || g.calls.Load() != 1 {
		t.Fatalf("second read = %+v after %d calls; a caller's mutation leaked or the burst was not coalesced", again, g.calls.Load())
	}
	cg.ActiveTenantGrants(ctx, "u2", "t1")
	cg.ActiveTenantGrants(ctx, "u1", "t2")
	if g.calls.Load() != 3 {
		t.Fatalf("another user/tenant shared the cached grants (%d calls)", g.calls.Load())
	}
	time.Sleep(authAccessTTL + 100*time.Millisecond)
	cg.ActiveTenantGrants(ctx, "u1", "t1")
	if g.calls.Load() != 4 {
		t.Fatal("grants were served past authAccessTTL")
	}
	g.fail.Store(true)
	cg2 := cachedGrantSource{inner: g, cache: newAuthReadCache()}
	if _, err := cg2.ActiveTenantGrants(ctx, "u9", "t1"); err == nil {
		t.Fatal("expected the lookup error")
	}
	g.fail.Store(false)
	if _, err := cg2.ActiveTenantGrants(ctx, "u9", "t1"); err != nil {
		t.Fatalf("an error was cached: %v", err)
	}
}
