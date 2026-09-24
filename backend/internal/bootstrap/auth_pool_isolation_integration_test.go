package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
// tiny audit INSERT) waited 6-10s for a connection. This test reproduces that on a
// throwaway Postgres: saturate the MAIN pool with slow queries, then drive the
// session-events handler exactly as newAPI wires it, and require it to stay fast.
//
// Run with GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_AUTH_POOL_TEST_DATABASE_URL=<throwaway,
// migrated DB>. Never point it at the shared OCI goatos DB or staging.
const authPoolTestTenantID = "20000000-0000-4000-8000-000000000001"

type authPoolStaticVerifier struct{ claims platformauth.Claims }

func (v authPoolStaticVerifier) Verify(string) (platformauth.Claims, error) { return v.claims, nil }

func TestAuthSessionEventsNotStarvedBySaturatedMainPool(t *testing.T) {
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("set GOATOS_RUN_POSTGRES_TESTS=1 and GOATOS_AUTH_POOL_TEST_DATABASE_URL to run")
	}
	dsn := os.Getenv("GOATOS_AUTH_POOL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_AUTH_POOL_TEST_DATABASE_URL is required (throwaway, migrated DB)")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Production defaults: GOATOS_PG_MAX_CONNS unset -> 10; stg query timeout 15s.
	cfg := platformpg.ConfigFromEnv()
	cfg.DatabaseURL = dsn
	cfg.MaxConns = 10
	cfg.QueryTimeout = 15 * time.Second

	mainPool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect main pool: %v", err)
	}
	defer mainPool.Close()
	authPool, err := connectAuthPool(ctx, cfg, mainPool)
	if err != nil {
		t.Fatalf("connect auth pool: %v", err)
	}
	if authPool != mainPool {
		defer authPool.Close()
	}

	if _, err := mainPool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'auth pool isolation test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, authPoolTestTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	emailVerified := true
	verifier := authPoolStaticVerifier{claims: platformauth.Claims{
		Subject:       "10000000-0000-4000-8000-0000000000aa",
		Issuer:        "goatos-test",
		Audience:      "goatos-test",
		Email:         "pool-isolation@mesha.sg",
		EmailVerified: &emailVerified,
		Expires:       time.Now().Add(time.Hour),
	}}
	authMux := http.NewServeMux()
	authaudit.Register(authMux, newAuthSessionHandler(authPool, cfg, verifier, []authaudit.Option{
		authaudit.WithAllowedEmails([]string{"pool-isolation@mesha.sg"}),
	}, log))
	handler := httpmiddleware.RequestContext(log)(authMux)

	// Saturate the main pool: 2x MaxConns concurrent slow dashboard-style reads.
	satCtx, stopSat := context.WithCancel(ctx)
	var satWG sync.WaitGroup
	for i := 0; i < int(cfg.MaxConns)*2; i++ {
		satWG.Add(1)
		go func() {
			defer satWG.Done()
			for satCtx.Err() == nil {
				_, _ = mainPool.Exec(satCtx, "select pg_sleep(4)")
			}
		}()
	}
	defer func() { stopSat(); satWG.Wait() }()
	waitForSaturation(t, mainPool, cfg.MaxConns)

	const requests = 40
	const concurrency = 4
	latencies := make([]time.Duration, requests)
	codes := make([]int, requests)
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
			handler.ServeHTTP(rec, req)
			latencies[i] = time.Since(start)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusNoContent {
			t.Errorf("request %d status=%d want 204", i, code)
		}
	}
	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	p50 := sorted[len(sorted)/2]
	p95 := sorted[(len(sorted)*95)/100-1]
	maxLat := sorted[len(sorted)-1]
	t.Logf("session-events under main-pool saturation: n=%d p50=%s p95=%s max=%s main_pool_acquired=%d/%d",
		requests, p50.Round(time.Millisecond), p95.Round(time.Millisecond), maxLat.Round(time.Millisecond),
		mainPool.Stat().AcquiredConns(), mainPool.Stat().MaxConns())

	const bound = time.Second
	if p95 > bound || maxLat > bound {
		t.Fatalf("session-events starved by saturated main pool: p95=%s max=%s (bound %s)", p95, maxLat, bound)
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
	t.Fatalf("main pool never saturated: acquired=%d max=%d", pool.Stat().AcquiredConns(), maxConns)
}
