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
//
// It deliberately does NOT wire the PACKING store, so the scheduled 14:00 correction amends the
// sheet but reopens no packed bag -- exactly as origin/main (what STG runs) behaves. AGENTS.md
// "AFTERNOON FEED CORRECTION" describes the reopen, but turning it on in the scheduled paths
// changes daily operations on deploy without any SOP edit, so it is a PENDING MAINTAINER DECISION
// (2026-09-17 parity revert). Pinned by TestFeedDirectionLifecycleServicePinsTheCardAndKeepsMainsCorrection
// and the feed-direction-issue job's TestIssueServiceDoesNotReopenPackingOnCorrection.
func NewFeedDirectionLifecycleService(deps Deps, generatedBy string) *feeddirectionapp.Service {
	repo := feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	countsService := countsapp.NewService(countspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return feeddirectionapp.NewService(repo, feeddirectioncounts.NewReader(countsService)).
		WithIssueStore(repo).
		WithScheduleReader(repo).
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
