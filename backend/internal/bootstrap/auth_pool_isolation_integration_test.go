package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// Incident goatos-stg 2026-09-24 01:56 UTC: a burst of heavy admin-web reads held
// every connection of the API's single pgx pool, and POST /auth/session-events (a
// tiny audit INSERT) waited 6-10s for a connection. These tests reproduce that on
// a throwaway Postgres and drive the session-events handler exactly as newAPI
// wires it (auth pool, shared request deadline, DB-backed email allowlist).
//
// Run with GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_AUTH_POOL_TEST_DATABASE_URL=<throwaway,
// migrated DB>. Never point it at the shared OCI goatos DB or staging.
const (
	authPoolTestTenantID = "20000000-0000-4000-8000-000000000001"
	authPoolTestEmail    = "pool-isolation@mesha.sg"
)

type authPoolStaticVerifier struct{ claims platformauth.Claims }

func (v authPoolStaticVerifier) Verify(string) (platformauth.Claims, error) { return v.claims, nil }

type authPoolHarness struct {
	cfg      platformpg.Config
	mainPool *pgxpool.Pool
	authPool *pgxpool.Pool
	handler  http.Handler
}

func newAuthPoolHarness(t *testing.T) *authPoolHarness {
	t.Helper()
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("set GOATOS_RUN_POSTGRES_TESTS=1 and GOATOS_AUTH_POOL_TEST_DATABASE_URL to run")
	}
	dsn := os.Getenv("GOATOS_AUTH_POOL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_AUTH_POOL_TEST_DATABASE_URL is required (throwaway, migrated DB)")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Production shape: main pool 10 (stg default), stg query timeout 15s;
	// auth pool at its defaults (2 conns, 3s shared request deadline).
	cfg := platformpg.ConfigFromEnv()
	cfg.DatabaseURL = dsn
	cfg.MaxConns = 10
	cfg.QueryTimeout = 15 * time.Second

	mainPool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect main pool: %v", err)
	}
	t.Cleanup(mainPool.Close)
	authPool, err := connectAuthPool(ctx, cfg, log)
	if err != nil {
		t.Fatalf("connect auth pool: %v", err)
	}
	t.Cleanup(authPool.Close)

	if _, err := mainPool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'auth pool isolation test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, authPoolTestTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	// The signing-in email is allowed ONLY through the DB-backed allowlist, so
	// every request exercises the dynamic allowlist read on the auth pool.
	if _, err := mainPool.Exec(ctx, `
INSERT INTO auth_allowed_emails (tenant_id, email, normalized_email, status, source)
SELECT $1, $2, $2, 'active', 'auth_pool_isolation_test'
WHERE NOT EXISTS (SELECT 1 FROM auth_allowed_emails WHERE tenant_id = $1 AND normalized_email = $2)`,
		authPoolTestTenantID, authPoolTestEmail); err != nil {
		t.Fatalf("seed allowed email: %v", err)
	}

	return &authPoolHarness{cfg: cfg, mainPool: mainPool, authPool: authPool, handler: sessionHandlerOn(authPool, cfg, log)}
}

// sessionHandlerOn serves POST /auth/session-events exactly as newAPI wires it.
func sessionHandlerOn(authPool *pgxpool.Pool, cfg platformpg.Config, log *slog.Logger) http.Handler {
	emailVerified := true
	verifier := authPoolStaticVerifier{claims: platformauth.Claims{
		Subject:       "10000000-0000-4000-8000-0000000000aa",
		Issuer:        "goatos-test",
		Audience:      "goatos-test",
		Email:         authPoolTestEmail,
		EmailVerified: &emailVerified,
		Expires:       time.Now().Add(time.Hour),
	}}
	mux := http.NewServeMux()
	authaudit.Register(mux, newAuthSessionHandler(authPool, cfg, verifier, []authaudit.Option{
		// A non-empty static list that does NOT contain the email forces the
		// dynamic (DB) allowlist lookup, as for a person added on /people.
		authaudit.WithAllowedEmails([]string{"someone-else@mesha.sg"}),
	}, log))
	return httpmiddleware.RequestContext(log)(mux)
}

// Real Postgres answering 53300 (too many connections) to the auth pool -- the
// cold-instance-at-max_connections case -- must be a 503, not a 403 for an
// allowlisted person. A role with CONNECTION LIMIT 0 makes the server refuse
// every auth-pool connection with exactly that SQLSTATE.
func TestAuthSessionEventsTooManyConnectionsIs503(t *testing.T) {
	h := newAuthPoolHarness(t)
	ctx := context.Background()
	if _, err := h.mainPool.Exec(ctx, `DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authpool_limited') THEN
    CREATE ROLE authpool_limited LOGIN PASSWORD 'limited' CONNECTION LIMIT 0;
  END IF; END $$`); err != nil {
		t.Fatalf("create limited role: %v", err)
	}
	u, err := url.Parse(h.cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = url.UserPassword("authpool_limited", "limited")
	cfg := h.cfg
	cfg.DatabaseURL = u.String()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	authPool, err := connectAuthPool(ctx, cfg, log)
	if err != nil {
		t.Fatalf("auth pool must not fail boot on 53300: %v", err)
	}
	defer authPool.Close()
	h.handler = sessionHandlerOn(authPool, cfg, log)

	for i, r := range h.signIns(3, 3) {
		t.Logf("53300 auth pool: request %d status=%d retry-after=%q latency=%s", i, r.code, r.retryAfter, r.latency.Round(time.Millisecond))
		if r.code != http.StatusServiceUnavailable || r.retryAfter == "" {
			t.Errorf("request %d status=%d, want 503 + Retry-After", i, r.code)
		}
	}
}

// holdConns keeps n goroutines looping pg_sleep on pool until the test ends.
func holdConns(t *testing.T, pool *pgxpool.Pool, n int, sleep string) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_, _ = pool.Exec(ctx, "select pg_sleep("+sleep+")")
			}
		}()
	}
	t.Cleanup(func() { stop(); wg.Wait() })
	waitForSaturation(t, pool, pool.Config().MaxConns)
}

type signInResult struct {
	latency    time.Duration
	code       int
	retryAfter string
}

func (h *authPoolHarness) signIns(requests, concurrency int) []signInResult {
	out := make([]signInResult, requests)
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in","source":"admin-web"}`))
			req.Header.Set("Authorization", "Bearer test")
			req.Header.Set(httpmiddleware.TenantContextHeader, authPoolTestTenantID)
			rec := httptest.NewRecorder()
			start := time.Now()
			h.handler.ServeHTTP(rec, req)
			out[i] = signInResult{latency: time.Since(start), code: rec.Code, retryAfter: rec.Header().Get("Retry-After")}
		}(i)
	}
	wg.Wait()
	return out
}

func percentiles(results []signInResult) (p50, p95, maxLat time.Duration) {
	sorted := make([]time.Duration, len(results))
	for i, r := range results {
		sorted[i] = r.latency
	}
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	p95i := (len(sorted)*95)/100 - 1
	if p95i < 0 {
		p95i = 0
	}
	return sorted[len(sorted)/2], sorted[p95i], sorted[len(sorted)-1]
}

func requireAllNoContent(t *testing.T, results []signInResult) {
	t.Helper()
	for i, r := range results {
		if r.code != http.StatusNoContent {
			t.Errorf("request %d status=%d want 204", i, r.code)
		}
	}
}

// The incident: the main pool is full of slow dashboard reads.
func TestAuthSessionEventsNotStarvedBySaturatedMainPool(t *testing.T) {
	h := newAuthPoolHarness(t)
	holdConns(t, h.mainPool, int(h.cfg.MaxConns)*2, "4")

	results := h.signIns(40, 4)
	requireAllNoContent(t, results)
	p50, p95, maxLat := percentiles(results)
	t.Logf("session-events under main-pool saturation: n=40 conc=4 p50=%s p95=%s max=%s main_pool_acquired=%d/%d",
		p50.Round(time.Millisecond), p95.Round(time.Millisecond), maxLat.Round(time.Millisecond),
		h.mainPool.Stat().AcquiredConns(), h.mainPool.Stat().MaxConns())
	const bound = time.Second
	if p95 > bound || maxLat > bound {
		t.Fatalf("session-events starved by saturated main pool: p95=%s max=%s (bound %s)", p95, maxLat, bound)
	}
}

// More concurrent sign-ins than the auth pool has connections (2): they queue on
// the auth pool, but every one must succeed well inside the shared deadline.
func TestAuthSessionEventsQueueOnSmallAuthPoolWithoutFailing(t *testing.T) {
	h := newAuthPoolHarness(t)
	holdConns(t, h.mainPool, int(h.cfg.MaxConns)*2, "4")

	results := h.signIns(24, 8)
	requireAllNoContent(t, results)
	p50, p95, maxLat := percentiles(results)
	st := h.authPool.Stat()
	t.Logf("session-events, 8 concurrent on a %d-conn auth pool: n=24 p50=%s p95=%s max=%s auth_empty_acquires=%d",
		st.MaxConns(), p50.Round(time.Millisecond), p95.Round(time.Millisecond), maxLat.Round(time.Millisecond), st.EmptyAcquireCount())
	deadline := platformpg.AuthPoolConfig(h.cfg).QueryTimeout
	if maxLat >= deadline {
		t.Fatalf("concurrent sign-ins hit the %s auth deadline: max=%s", deadline, maxLat)
	}
	if st.EmptyAcquireCount() == 0 {
		t.Fatalf("test did not actually saturate the auth pool (no empty acquires)")
	}
}

// When the auth pool itself is exhausted, sign-in fails FAST with a retryable
// 503 + Retry-After at the shared deadline -- never a 500, never 15s.
func TestAuthSessionEventsFailFastWith503WhenAuthPoolExhausted(t *testing.T) {
	h := newAuthPoolHarness(t)
	holdConns(t, h.authPool, int(h.authPool.Config().MaxConns), "6")

	results := h.signIns(3, 3)
	deadline := platformpg.AuthPoolConfig(h.cfg).QueryTimeout
	for i, r := range results {
		t.Logf("exhausted auth pool: request %d status=%d retry-after=%q latency=%s", i, r.code, r.retryAfter, r.latency.Round(time.Millisecond))
		if r.code != http.StatusServiceUnavailable || r.retryAfter == "" {
			t.Errorf("request %d status=%d retry-after=%q, want 503 with Retry-After", i, r.code, r.retryAfter)
		}
		if r.latency > deadline+time.Second {
			t.Errorf("request %d took %s, want about the %s shared deadline", i, r.latency, deadline)
		}
	}
}

func waitForSaturation(t *testing.T, pool *pgxpool.Pool, maxConns int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pool.Stat().AcquiredConns() >= maxConns {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pool never saturated: acquired=%d max=%d", pool.Stat().AcquiredConns(), maxConns)
}
