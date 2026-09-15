package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func TestCombinedExecutionWholeFilterPaginationDateShiftStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	const otherBatch = "00000000-0000-4000-8000-000000008891"
	const otherGoat = "00000000-0000-4000-8000-000000008893"
	const otherObligation = "00000000-0000-4000-8000-000000008892"
	insertProjectionBatch(t, ctx, pool, otherBatch, "planned")
	insertProjectionGoat(t, ctx, pool, otherGoat, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, otherObligation, otherBatch, otherGoat, "scheduled", "2026-06-24T00:00:00Z", "combined-other-batch")
	repo := NewRepository(pool, 5*time.Second)
	base := domain.ExecutionQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	for _, mode := range []string{"page_boundary", "whole", "empty", "completed", "park", "shifted_date"} {
		t.Run(mode, func(t *testing.T) {
			q := base
			switch mode {
			case "whole":
				q.Limit = 200
			case "shifted_date":
				q.DueBefore = q.DueBefore.AddDate(0, 0, -10)
			case "empty":
				v := "00000000-0000-4000-8000-000000000999"
				q.ParkID = &v
			case "completed":
				v := domain.WorkStateCompleted
				q.WorkState = &v
			case "park":
				v := testPark
				q.ParkID = &v
			}
			page, err := repo.ListVaccinationExecutionPage(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			cards, err := repo.VaccinationExecutionCardSummaries(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			combined, combinedCards, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(page, combined) || !reflect.DeepEqual(cards, combinedCards) {
				t.Fatalf("combined differs: page=%+v combined=%+v cards=%+v combinedCards=%+v", page, combined, cards, combinedCards)
			}
			if mode == "page_boundary" && page.TotalCount <= 1 {
				t.Fatal("fixture must span pages")
			}
			if mode == "empty" && (len(combined.Rows) != 0 || len(combinedCards) != 0) {
				t.Fatal("empty scope invented rows")
			}
		})
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, base); err == nil {
		t.Fatal("cancellation ignored")
	}
	base.Cursor = &domain.ExecutionCursor{}
	if _, _, err := repo.ListVaccinationExecutionFirstPageWithSummaries(context.Background(), base); err == nil {
		t.Fatal("cursor unsupported")
	}
}
