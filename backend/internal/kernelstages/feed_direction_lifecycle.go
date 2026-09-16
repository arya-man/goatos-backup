package kernelstages

import (
	"context"
	"fmt"
	feedsoppg "github.com/vgoats/goatos/backend/internal/feedsop/adapters/postgres"
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
	service := NewFeedDirectionLifecycleService(deps, "kernel-worker:feed-direction-lifecycle")
	return &FeedDirectionLifecycleStage{service: service, tenantID: tenantID, now: time.Now}
}

// NewFeedDirectionLifecycleService is the ONE composition of the scheduled sheet lifecycle (issue,
// the 14:00 correction, lock), shared by this stage and the feed-direction-issue Cloud Run job so the
// two cannot drift again. It wires:
//   - the issue store and dispatch clock the three operations run on;
//   - the published feed SOP cards, so an issued sheet pins the card in force (FEED SOP,
//     2026-09-16) -- the same source the API's freeze-on-read uses;
//   - the PACKING store, through which the correction reopens every packed session of a pen whose
//     head count moved (maintainer decision 2026-08-10). Without it AmendDirection skips the reopen
//     silently; both scheduled roots shipped that way until 2026-09-17.
func NewFeedDirectionLifecycleService(deps Deps, generatedBy string) *feeddirectionapp.Service {
	repo := feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	countsService := countsapp.NewService(countspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return feeddirectionapp.NewService(repo, feeddirectioncounts.NewReader(countsService)).
		WithIssueStore(repo).
		WithScheduleReader(repo).
		WithPackingStore(repo).
		WithSOPRules(feedsoppg.NewRulesSource(deps.Pool, deps.PgCfg.QueryTimeout)).
		WithGeneratedBy(generatedBy)
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
