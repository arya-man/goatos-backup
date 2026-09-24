package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	countsboard "github.com/vgoats/goatos/backend/internal/counts/adapters/boardsource"
	feedboard "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/boardsource"
	healthboard "github.com/vgoats/goatos/backend/internal/health/adapters/boardsource"
	pccareboard "github.com/vgoats/goatos/backend/internal/pccare/adapters/boardsource"
	penroutinesboard "github.com/vgoats/goatos/backend/internal/penroutines/adapters/boardsource"
	penvisitsboard "github.com/vgoats/goatos/backend/internal/penvisits/adapters/boardsource"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	verificationboard "github.com/vgoats/goatos/backend/internal/verification/adapters/boardsource"
	weighingboard "github.com/vgoats/goatos/backend/internal/weighing/adapters/boardsource"
	workboardpg "github.com/vgoats/goatos/backend/internal/workboard/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/workboard/app"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// P10: the /work-board/page summary used to send one count statement per source, each its own
// round trip (8 SQL sources + feed's batch = 9). Every batchable source's count now rides ONE
// pgx.Batch, so the summary is 2 round trips (that batch + feed's own batch).
func TestWorkBoardSummaryBatchesSourceCounts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	counted, trips := pgtest.CountingPool(t, ctx, pool)
	const timeout = 15 * time.Second
	sources := []ports.Source{
		weighingboard.New(counted, timeout),
		feedboard.New(counted, timeout),
		verificationboard.New(counted, timeout),
		countsboard.NewApprovals(counted, timeout),
		countsboard.NewMilkFeeding(counted, timeout),
		healthboard.New(counted, timeout),
		pccareboard.New(counted, timeout),
		penvisitsboard.New(counted, timeout),
		penroutinesboard.New(counted, timeout),
	}
	q := domain.Query{TenantID: "00000000-0000-0000-0000-000000000001", ParkID: "00000000-0000-0000-0000-000000003001", BusinessDate: "2026-09-24"}
	measure := func(svc *app.Service) (int, domain.Summary) {
		t.Helper()
		if _, err := svc.Summary(ports.WithRequestReadMemo(ctx), q); err != nil { // warm statement cache
			t.Fatal(err)
		}
		trips.Reset()
		sum, err := svc.Summary(ports.WithRequestReadMemo(ctx), q)
		if err != nil {
			t.Fatal(err)
		}
		return trips.Trips(), sum
	}
	before, sumBefore := measure(app.NewService(sources...))
	after, sumAfter := measure(app.NewService(sources...).WithStatementBatch(workboardpg.NewStatementBatch(counted, timeout)))
	if before < 9 {
		t.Fatalf("baseline summary took %d round trips, expected one per source (>=9)", before)
	}
	if after > 2 {
		t.Fatalf("batched summary took %d round trips, want <=2:\n%s", after, strings.Join(trips.SQL(), "\n---\n"))
	}
	if fmt.Sprint(sumBefore) != fmt.Sprint(sumAfter) {
		t.Fatalf("batched summary differs:\nbefore=%+v\nafter=%+v", sumBefore, sumAfter)
	}
	t.Logf("summary round trips: per-source %d -> batched %d", before, after)
}
