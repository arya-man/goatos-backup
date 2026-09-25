package bootstrap

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// Every registered warm-up must run cleanly against the migrated schema: a statement that no longer
// prepares (renamed column, changed parameter type) would silently stop being warmed.
func TestAPIConnWarmupsRunOnMigratedSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	warmups := apiConnWarmups()
	if len(warmups) == 0 {
		t.Fatal("no warm-ups registered")
	}
	seen := map[string]bool{}
	for _, wu := range warmups {
		if wu.Name == "" || strings.TrimSpace(wu.SQL) == "" || seen[wu.Name] {
			t.Fatalf("warm-up %q: empty or duplicate", wu.Name)
		}
		seen[wu.Name] = true
	}
	var logs bytes.Buffer
	w := platformpg.NewConnWarmer(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	w.Register(warmups...)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	w.WarmConn(ctx, conn.Conn())
	out := logs.String()
	if strings.Contains(out, "postgres_conn_warmup_statement_failed") || !strings.Contains(out, "failed=0") {
		t.Fatalf("warm-up failures:\n%s", out)
	}
	if got := strings.Count(out, "msg=postgres_conn_warmup_statement "); got != len(warmups) {
		t.Fatalf("warmed %d of %d statements:\n%s", got, len(warmups), out)
	}
}
