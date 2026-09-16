package app

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
	"io"
	"strconv"
	"testing"
	"time"
)

type exportWindowRepo struct {
	fakeRepo
	from, to time.Time
	calls    int
	err      error
}

func (r *exportWindowRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return []domain.WeighingPark{{ParkID: "park"}}, nil
}
func (r *exportWindowRepo) ExportCSV(_ context.Context, _ string, _ []string, _ []string, from, to time.Time, _, _, _ string, w io.Writer) error {
	r.from, r.to = from, to
	r.calls++
	return r.err
}
func TestPublishedWeightsWindowIsExportable(t *testing.T) {
	actor := domain.Actor{PermissionsResolved: true, Permissions: []string{permissions.WeighingMonitor}}
	for _, days := range []int{1, 366, 730, 3650} {
		t.Run(strconv.Itoa(days), func(t *testing.T) {
			rules := domain.SeededRules()
			rules.WeightsPages.DefaultFromMode = domain.WeightsFromRollingDays
			rules.WeightsPages.DefaultFromDays = days
			rules.WeightsPages.EarliestDate = "2000-01-01"
			if p := domain.ValidateWeighingSOP(rules.WeighingSOP); len(p) > 0 {
				t.Fatal(p)
			}
			to := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
			from := to.AddDate(0, 0, -(days - 1))
			repo := &exportWindowRepo{}
			err := NewService(repo).ExportCSV(context.Background(), actor, from.Format("2006-01-02"), to.Format("2006-01-02"), "", nil, "", "", "", io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if repo.calls != 1 || repo.from.Format("2006-01-02") != from.Format("2006-01-02") || repo.to.Format("2006-01-02") != "2026-09-17" {
				t.Fatalf("range changed: %v..%v (%d calls)", repo.from, repo.to, repo.calls)
			}
		})
	}
	repo := &exportWindowRepo{err: context.DeadlineExceeded}
	err := NewService(repo).ExportCSV(context.Background(), actor, "2024-09-17", "2026-09-16", "", nil, "", "", "", io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout hidden: %v", err)
	}
	repo.calls = 0
	err = NewService(repo).ExportCSV(context.Background(), actor, "2026-09-17", "2026-09-16", "", nil, "", "", "", io.Discard)
	if !errors.Is(err, ports.ErrInvalidArgument) || repo.calls != 0 {
		t.Fatalf("inverted range reached export: %v", err)
	}
}
