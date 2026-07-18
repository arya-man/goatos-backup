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
