package liveload

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	herdpg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestHerdSignalsColdRiskRead times the FIRST risk_state=attention read on a brand-new service
// (empty caches), three times, at LOAD_TAGS tags. When the service persists risk (RecomputeRisk),
// the classification job runs once beforehand, as it would have in production.
func TestHerdSignalsColdRiskRead(t *testing.T) {
	if os.Getenv("GOATOS_HERD_LIVE_LOADTEST") != "1" {
		t.Skip("set GOATOS_HERD_LIVE_LOADTEST=1 to run the live load harness")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	tags := envInt("LOAD_TAGS", 20000)
	seed(t, ctx, pool, tags, tags/100)
	actor := domain.Actor{TenantID: loadTenant, UserID: loadActor}
	risk := "attention"
	if rc, ok := any(herdapp.NewService(herdpg.NewRepository(pool))).(interface {
		RecomputeRisk(context.Context, string) (int, error)
	}); ok {
		start := time.Now()
		n, err := rc.RecomputeRisk(ctx, loadTenant)
		if err != nil {
			t.Fatalf("RecomputeRisk: %v", err)
		}
		fmt.Printf("COLD tags=%d risk_job_rows=%d risk_job=%s\n", tags, n, time.Since(start).Round(time.Millisecond))
	}
	for i := 0; i < 3; i++ {
		svc := herdapp.NewService(herdpg.NewRepository(pool))
		start := time.Now()
		resp, err := svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 25, domain.LiveSort{})
		if err != nil {
			t.Fatalf("cold risk read: %v", err)
		}
		fmt.Printf("COLD tags=%d run=%d cold_risk_read=%s items=%d summary_tags=%d\n", tags, i, time.Since(start).Round(time.Millisecond), len(resp.Items), resp.Summary.TagsSeen)
	}
}
