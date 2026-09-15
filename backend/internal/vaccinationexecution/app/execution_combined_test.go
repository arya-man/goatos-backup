package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type combinedReadFake struct {
	fakeRepo
	calls, pageCalls, summaryCalls atomic.Int32
	combinedErr                    error
}

func (r *combinedReadFake) ListVaccinationExecutionFirstPageWithSummaries(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, map[string]*domain.ShedCardSummary, error) {
	r.calls.Add(1)
	if r.combinedErr != nil {
		return domain.ExecutionProjectionPage{}, nil, r.combinedErr
	}
	if err := ctx.Err(); err != nil {
		return domain.ExecutionProjectionPage{}, nil, err
	}
	return domain.ExecutionProjectionPage{Rows: []domain.ExecutionProjection{}, TotalCount: 0}, map[string]*domain.ShedCardSummary{"off-page": {TargetCount: 99}}, nil
}
func (r *combinedReadFake) ListVaccinationExecutionPage(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error) {
	r.pageCalls.Add(1)
	return r.fakeRepo.ListVaccinationExecutionPage(ctx, q)
}
func (r *combinedReadFake) VaccinationExecutionCardSummaries(context.Context, domain.ExecutionQuery) (map[string]*domain.ShedCardSummary, error) {
	r.summaryCalls.Add(1)
	return map[string]*domain.ShedCardSummary{}, nil
}
func TestExecutionCombinedReaderSelectionAndErrors(t *testing.T) {
	for _, mode := range []string{"first", "cursor", "no_cards", "error", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			r := &combinedReadFake{}
			ctx := context.Background()
			q := domain.ExecutionQuery{TenantID: "t", Limit: 20}
			sentinel := errors.New("combined failed")
			switch mode {
			case "cursor":
				q.Cursor = &domain.ExecutionCursor{SortRank: 1}
			case "no_cards":
				v := false
				q.IncludeCardSummaries = &v
			case "error":
				r.combinedErr = sentinel
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := NewService(r).VaccinationExecutionPage(ctx, q)
			if mode == "error" || mode == "canceled" {
				if err == nil {
					t.Fatal("lost combined error")
				}
				if mode == "error" && !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "cursor" || mode == "no_cards" {
				if r.calls.Load() != 0 || r.pageCalls.Load() != 1 {
					t.Fatal("combined used outside contract")
				}
			} else {
				if r.calls.Load() != 1 || r.pageCalls.Load() != 0 || r.summaryCalls.Load() != 0 {
					t.Fatal("duplicate canonical reads")
				}
				if mode == "first" && out.CardSummaries["off-page"].TargetCount != 99 {
					t.Fatal("whole-filter summaries lost")
				}
			}
		})
	}
}
