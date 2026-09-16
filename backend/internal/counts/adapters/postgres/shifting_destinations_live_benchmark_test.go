//go:build liveoci

package postgres

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveOCIShiftingDestinationCatalogLatency(t *testing.T) {
	if os.Getenv("GOATOS_LIVE_OCI_BENCH") != "1" {
		t.Skip("set GOATOS_LIVE_OCI_BENCH=1 to run against the configured OCI DATABASE_URL")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required")
	}
	tenantID := os.Getenv("GOATOS_TENANT_ID")
	if tenantID == "" {
		tenantID = countsTenant
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool, 30*time.Second)
	for i := 0; i < 5; i++ {
		if _, err := repo.ShiftingDestinationCatalog(ctx, tenantID); err != nil {
			t.Fatalf("warmup %d: %v", i+1, err)
		}
	}

	samples := make([]float64, 0, 25)
	var parks, sheds int
	for i := 0; i < 25; i++ {
		start := time.Now()
		catalog, err := repo.ShiftingDestinationCatalog(ctx, tenantID)
		if err != nil {
			t.Fatalf("sample %d: %v", i+1, err)
		}
		samples = append(samples, float64(time.Since(start).Microseconds())/1000.0)
		parks = len(catalog.Parks)
		sheds = 0
		for _, park := range catalog.Parks {
			sheds += len(park.Sheds)
		}
	}
	sort.Float64s(samples)
	sum := 0.0
	for _, sample := range samples {
		sum += sample
	}
	percentile := func(q float64) float64 {
		idx := int(q*float64(len(samples)-1) + 0.5)
		return samples[idx]
	}
	t.Logf("live_oci_shifting_destinations tenant=%s parks=%d sheds=%d n=%d min=%.2f p50=%.2f p90=%.2f p95=%.2f max=%.2f avg=%.2f",
		tenantID,
		parks,
		sheds,
		len(samples),
		samples[0],
		percentile(0.50),
		percentile(0.90),
		percentile(0.95),
		samples[len(samples)-1],
		sum/float64(len(samples)),
	)
	if parks == 0 || sheds == 0 {
		t.Fatalf("empty catalog: parks=%d sheds=%d", parks, sheds)
	}
}
