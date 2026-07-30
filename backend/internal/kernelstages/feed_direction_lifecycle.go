package kernelstages

import (
	"context"
	"fmt"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
)

type scheduledFeedLifecycle interface {
	AdvanceScheduledLifecycle(context.Context, string, time.Time) ([]feeddirectionapp.LifecycleReport, error)
}

// FeedDirectionLifecycleStage freezes the same generated rows served by Feed
// Distribution and Feed Packing, then advances their authored correction and
// transport cutoffs. Both screens therefore retain the identical historical
// sheet after their business date passes.
type FeedDirectionLifecycleStage struct {
	service  scheduledFeedLifecycle
	tenantID string
	now      func() time.Time
}

func NewFeedDirectionLifecycleStage(deps Deps, tenantID string) *FeedDirectionLifecycleStage {
	repo := feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	countsService := countsapp.NewService(countspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	service := feeddirectionapp.NewService(repo, feeddirectioncounts.NewReader(countsService)).
		WithIssueStore(repo).
		WithScheduleReader(repo).
		WithGeneratedBy("kernel-worker:feed-direction-lifecycle")
	return &FeedDirectionLifecycleStage{service: service, tenantID: tenantID, now: time.Now}
}

func (s *FeedDirectionLifecycleStage) Name() string { return "feed-direction-lifecycle" }

func (s *FeedDirectionLifecycleStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("feed direction lifecycle: tenant id is required")
	}
	_, err := s.service.AdvanceScheduledLifecycle(ctx, s.tenantID, s.now())
	return err
}

func (s *FeedDirectionLifecycleStage) withClock(now func() time.Time) *FeedDirectionLifecycleStage {
	s.now = now
	return s
}
