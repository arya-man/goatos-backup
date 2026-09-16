package kernelstages

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"testing"
	"time"
)

func TestConsolidatedSweeperRunEmitsActualVersionDuration(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := protopg.NewRepository(pool, 5*time.Second)
	id, err := repo.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: ruleIdentityTenant, Code: "metrics.test", Name: "Metrics", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := repo.CreateVersion(ctx, protodomain.NewVersion{TenantID: ruleIdentityTenant, ProtocolID: id, ScopeType: "tenant", Version: 1, Status: "draft", EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	stage := NewObligationSweeperStage(Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}}, SweeperConfig{TenantID: ruleIdentityTenant, ActorID: "00000000-0000-4000-8000-000000000002", VersionID: version})
	if err = stage.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var data metricdata.ResourceMetrics
	if err = stageMetricReader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "kernel.sweeper.batch.duration" {
				if m.Unit != "s" {
					t.Fatal(m.Unit)
				}
				for _, p := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					if p.Count > 0 {
						return
					}
				}
			}
		}
	}
	t.Fatal("real consolidated Run emitted no sweep duration")
}
