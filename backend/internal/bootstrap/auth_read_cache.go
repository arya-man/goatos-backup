package bootstrap

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// AUTH LOOKUP COALESCING (stg latency program, P10/P13).
//
// Every authenticated request resolved the caller's grants and person-access snapshot from
// Postgres -- two statements per request, and a dashboard page fires 5-10 requests at once, all
// for the same person. These decorators put both lookups through a tiny dedicated read cache:
// single flight collapses a burst to one statement each, and an answer lives authAccessTTL.
//
// The TTL is the revocation bound: a grant or tick removed in HRMS stops authorizing requests on
// every instance within authAccessTTL (no stale serving, errors never cached). Keep it short --
// this is burst coalescing, not a session cache.
const authAccessTTL = 2 * time.Second

func newAuthReadCache() *readcache.Cache {
	return readcache.New(readcache.Options{
		Name:        "auth",
		MaxEntries:  4096,
		MaxBytes:    8 << 20,
		FreshTTL:    authAccessTTL,
		DegradedTTL: authAccessTTL,
		StaleGrace:  0,
	})
}

func authKey(tenantID, userID, what string) readcache.Key {
	return readcache.Key{Tenant: tenantID, Params: "auth:" + what + ":" + userID}
}

type cachedGrantSource struct {
	inner permissions.GrantSource
	cache *readcache.Cache
}

func (g cachedGrantSource) ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	roles, err := readcache.Load(ctx, g.cache, authKey(tenantID, userID, "roles"), func(ctx context.Context) ([]string, error) {
		return g.inner.ActiveTenantRoles(ctx, userID, tenantID)
	})
	return append([]string(nil), roles...), err
}

func (g cachedGrantSource) ActiveTenantGrants(ctx context.Context, userID, tenantID string) ([]permissions.ActiveGrant, error) {
	grants, err := readcache.Load(ctx, g.cache, authKey(tenantID, userID, "grants"), func(ctx context.Context) ([]permissions.ActiveGrant, error) {
		return g.inner.ActiveTenantGrants(ctx, userID, tenantID)
	})
	return append([]permissions.ActiveGrant(nil), grants...), err
}

// personAccessSnapshotSource is what the auth middleware consumes from the workforce access
// repository: the per-person resolver plus its one-statement snapshot.
type personAccessSnapshotSource interface {
	httpmiddleware.PersonAccessSource
	httpmiddleware.PersonAccessSnapshotSource
}

type cachedPersonAccess struct {
	inner personAccessSnapshotSource
	cache *readcache.Cache
}

type permsResult struct {
	perms       []string
	provisioned bool
}

type scopeResult struct {
	mode        string
	parks       []string
	provisioned bool
}

type snapshotResult struct {
	snap        httpmiddleware.PersonAccessSnapshot
	provisioned bool
}

func (p cachedPersonAccess) ResolvePermissions(ctx context.Context, tenantID, userID string) ([]string, bool, error) {
	r, err := readcache.Load(ctx, p.cache, authKey(tenantID, userID, "perms"), func(ctx context.Context) (permsResult, error) {
		perms, ok, err := p.inner.ResolvePermissions(ctx, tenantID, userID)
		return permsResult{perms, ok}, err
	})
	return append([]string(nil), r.perms...), r.provisioned, err
}

func (p cachedPersonAccess) ResolveParkScope(ctx context.Context, tenantID, userID string) (string, []string, bool, error) {
	r, err := readcache.Load(ctx, p.cache, authKey(tenantID, userID, "scope"), func(ctx context.Context) (scopeResult, error) {
		mode, parks, ok, err := p.inner.ResolveParkScope(ctx, tenantID, userID)
		return scopeResult{mode, parks, ok}, err
	})
	return r.mode, append([]string(nil), r.parks...), r.provisioned, err
}

func (p cachedPersonAccess) ResolveAccessSnapshot(ctx context.Context, tenantID, userID string) (httpmiddleware.PersonAccessSnapshot, bool, error) {
	r, err := readcache.Load(ctx, p.cache, authKey(tenantID, userID, "snapshot"), func(ctx context.Context) (snapshotResult, error) {
		snap, ok, err := p.inner.ResolveAccessSnapshot(ctx, tenantID, userID)
		return snapshotResult{snap, ok}, err
	})
	snap := r.snap
	snap.Permissions = append([]string(nil), snap.Permissions...)
	snap.ParkIDs = append([]string(nil), snap.ParkIDs...)
	return snap, r.provisioned, err
}
