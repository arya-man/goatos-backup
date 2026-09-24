package postgres

import (
	"testing"
	"time"
)

func TestAuthPoolConfigIsSmallDedicatedAndFailsFast(t *testing.T) {
	main := Config{DatabaseURL: "postgres://x", MaxConns: 10, MinConns: 3, QueryTimeout: 15 * time.Second, ApplicationName: "goatos-api"}
	auth := AuthPoolConfig(main)
	if auth.MaxConns != DefaultAuthMaxConns || auth.MinConns != 0 {
		t.Fatalf("auth pool sizing = %d/%d, want %d/0", auth.MaxConns, auth.MinConns, DefaultAuthMaxConns)
	}
	if auth.QueryTimeout != DefaultAuthQueryTimeout {
		t.Fatalf("auth timeout = %s, must not inherit the main pool's %s", auth.QueryTimeout, main.QueryTimeout)
	}
	if auth.ApplicationName != "goatos-api-auth" || auth.DatabaseURL != main.DatabaseURL {
		t.Fatalf("auth cfg = %+v", auth)
	}
	main.AuthMaxConns, main.AuthQueryTimeout = 4, time.Second
	if got := AuthPoolConfig(main); got.MaxConns != 4 || got.QueryTimeout != time.Second {
		t.Fatalf("explicit auth config ignored: %+v", got)
	}
}

func TestAuthPoolEnvDefaults(t *testing.T) {
	t.Setenv("GOATOS_PG_AUTH_MAX_CONNS", "")
	t.Setenv("GOATOS_PG_AUTH_QUERY_TIMEOUT", "")
	cfg := ConfigFromEnv()
	if cfg.AuthMaxConns != 2 || cfg.AuthQueryTimeout != 3*time.Second {
		t.Fatalf("defaults = %d %s", cfg.AuthMaxConns, cfg.AuthQueryTimeout)
	}
	t.Setenv("GOATOS_PG_AUTH_MAX_CONNS", "3")
	t.Setenv("GOATOS_PG_AUTH_QUERY_TIMEOUT", "2s")
	cfg = ConfigFromEnv()
	if cfg.AuthMaxConns != 3 || cfg.AuthQueryTimeout != 2*time.Second {
		t.Fatalf("env = %d %s", cfg.AuthMaxConns, cfg.AuthQueryTimeout)
	}
}

func TestPoolPressureDelta(t *testing.T) {
	d := poolPressure(poolSample{acquires: 10, empty: 1, wait: time.Second}, poolSample{acquires: 14, empty: 4, canceled: 1, wait: 9 * time.Second})
	if d.Acquires != 4 || d.EmptyAcquires != 3 || d.Canceled != 1 || d.WaitTotal != 8*time.Second {
		t.Fatalf("delta = %+v", d)
	}
}
