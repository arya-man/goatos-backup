package bootstrap

import (
	"context"
	"testing"
	"time"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// A new instance must boot even when Postgres refuses the auth pool (e.g. at
// max_connections): the auth pool connects lazily and retries on first use.
func TestAuthPoolDoesNotBlockBootWhenDatabaseRefuses(t *testing.T) {
	cfg := platformpg.Config{DatabaseURL: "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", ConnectTimeout: time.Second}
	pool, err := connectAuthPool(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("auth pool failed boot on unreachable DB: %v", err)
	}
	if pool == nil {
		t.Fatal("nil auth pool")
	}
	pool.Close()
}
