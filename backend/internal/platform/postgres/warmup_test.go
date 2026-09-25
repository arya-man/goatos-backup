package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConnWarmerInstallKeepsHooksAndCapsIdleConns(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://u@localhost/db")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 5
	var afterCalled, closeCalled bool
	cfg.AfterConnect = func(context.Context, *pgx.Conn) error { afterCalled = true; return nil }
	cfg.BeforeClose = func(*pgx.Conn) { closeCalled = true }
	w := NewConnWarmer(nil)
	w.install(cfg, 9)
	if cfg.MinIdleConns != 4 {
		t.Fatalf("MinIdleConns = %d, want MaxConns-1 = 4", cfg.MinIdleConns)
	}
	conn := &pgx.Conn{}
	if err := cfg.AfterConnect(context.Background(), conn); err != nil || !afterCalled {
		t.Fatalf("AfterConnect must chain the previous hook: err=%v called=%v", err, afterCalled)
	}
	if !w.isCold(conn) {
		t.Fatal("a new connection must be marked cold")
	}
	if !cfg.AfterRelease(conn) {
		t.Fatal("AfterRelease must keep the connection")
	}
	cfg.BeforeClose(conn)
	if !closeCalled || w.isCold(conn) {
		t.Fatalf("BeforeClose must chain and forget the connection: called=%v cold=%v", closeCalled, w.isCold(conn))
	}

	one, _ := pgxpool.ParseConfig("postgres://u@localhost/db")
	one.MaxConns = 1
	NewConnWarmer(nil).install(one, 1)
	if one.MinIdleConns != 0 {
		t.Fatalf("a single-connection pool must not keep a spare idle connection, got %d", one.MinIdleConns)
	}
}

func TestWarmupParamCount(t *testing.T) {
	n, err := warmupParamCount("SELECT $1::text, $3::int, '$9' WHERE $2::bool")
	if err != nil || n != 3 {
		t.Fatalf("warmupParamCount = %d, %v; want 3", n, err)
	}
}

// GOATOS_PLATFORM_PG_TEST_DATABASE_URL: any Postgres >= 14 (read-only is enough).
func TestConnWarmerWarmsIdleConnectionsInTheirPlanMode(t *testing.T) {
	dsn := os.Getenv("GOATOS_PLATFORM_PG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_PLATFORM_PG_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		genericSQL = "SELECT count(*) FROM pg_class WHERE relname = $1::text AND $2::int IS NULL /*warm generic*/"
		cachedSQL  = "SELECT count(*) FROM pg_class WHERE relname = $1::text /*warm cached*/"
		customSQL  = "SELECT count(*) FROM pg_class WHERE relname = $1::text /*warm custom*/"
		brokenSQL  = "SELECT * FROM warmup_table_that_does_not_exist WHERE id = $1"
	)
	w := NewConnWarmer(nil)
	w.Register(
		ConnWarmup{Name: "broken", SQL: brokenSQL, Mode: WarmCachedStatement},
		ConnWarmup{Name: "generic", SQL: genericSQL, Mode: WarmGenericPlan, Args: TenantFirst()},
		ConnWarmup{Name: "cached", SQL: cachedSQL, Mode: WarmCachedStatement},
		ConnWarmup{Name: "custom", SQL: customSQL, Mode: WarmCustomPlan},
	)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	w.install(cfg, 1)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil { // dials the (only) connection, which is now cold
		t.Fatal(err)
	}
	w.Start(ctx, pool)
	for {
		w.mu.Lock()
		n := len(w.cold)
		w.mu.Unlock()
		if n == 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("connection was never warmed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let the warmer release the connection

	type prepared struct{ generic, custom int64 }
	got := map[string]prepared{}
	var mode string
	if err := func() error {
		c, err := pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer c.Release()
		if err := c.QueryRow(ctx, "SELECT current_setting('plan_cache_mode')").Scan(&mode); err != nil {
			return err
		}
		rows, err := c.Query(ctx, "SELECT statement, generic_plans, custom_plans FROM pg_prepared_statements")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var stmt string
			var p prepared
			if err := rows.Scan(&stmt, &p.generic, &p.custom); err != nil {
				return err
			}
			got[stmt] = p
		}
		return rows.Err()
	}(); err != nil {
		t.Fatal(err)
	}
	if mode == "force_generic_plan" {
		t.Fatal("warm-up leaked plan_cache_mode onto the pooled connection")
	}
	if p, ok := got[genericSQL]; !ok || p.generic != 1 || p.custom != 0 {
		t.Fatalf("generic warm-up must cache exactly one generic plan, got %+v (present=%v)", p, ok)
	}
	if p, ok := got[cachedSQL]; !ok || p.generic != 0 || p.custom != 1 {
		t.Fatalf("cached warm-up must prepare the statement and run one custom (auto) plan, got %+v (present=%v)", p, ok)
	}
	if _, ok := got[customSQL]; ok {
		t.Fatal("custom-plan warm-up must not leave a prepared statement (the call site pins a custom plan)")
	}
}
