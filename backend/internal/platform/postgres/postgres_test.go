package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConnectDisablesJITForLatencySensitivePool(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://user:password@localhost:5432/goatos?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	configureOLTPRuntime(cfg)
	if got := cfg.ConnConfig.RuntimeParams["jit"]; got != "off" {
		t.Fatalf("jit runtime parameter = %q, want off", got)
	}
}

func TestConfigFromEnvIncludesBoundedMinConns(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/goatos?sslmode=disable")
	t.Setenv("GOATOS_PG_MAX_CONNS", "4")
	t.Setenv("GOATOS_PG_MIN_CONNS", "9")
	cfg := normalizedConfig(ConfigFromEnv())
	if got := cfg.MaxConns; got != 4 {
		t.Fatalf("MaxConns=%d want 4", got)
	}
	if got := cfg.MinConns; got != 4 {
		t.Fatalf("MinConns=%d want capped at MaxConns 4", got)
	}

	t.Setenv("GOATOS_PG_MIN_CONNS", "-2")
	cfg = normalizedConfig(ConfigFromEnv())
	if got := cfg.MinConns; got != 0 {
		t.Fatalf("negative MinConns=%d want 0", got)
	}
}

func TestApplyApplicationNameIsOptIn(t *testing.T) {
	cases := []struct{ dsn, name, want string }{
		{"postgres://u@h/db", "", ""},
		{"postgres://u@h/db?application_name=psql", "", "psql"},
		{"postgres://u@h/db", ServiceApplicationName("api"), "goatos-api"},
	}
	for _, tc := range cases {
		cfg, err := pgxpool.ParseConfig(tc.dsn)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.dsn, err)
		}
		configureOLTPRuntime(cfg)
		applyApplicationName(cfg, tc.name)
		if got := cfg.ConnConfig.RuntimeParams["application_name"]; got != tc.want {
			t.Fatalf("%q/%q: application_name = %q, want %q", tc.dsn, tc.name, got, tc.want)
		}
	}
}
