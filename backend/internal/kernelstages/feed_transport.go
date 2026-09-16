package kernelstages

import (
	"context"
	"fmt"
	"time"

	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	feedsoppg "github.com/vgoats/goatos/backend/internal/feedsop/adapters/postgres"
)

type FeedTransportStage struct {
	repo     *feeddirectionpg.Repository
	rules    feeddirectionports.SOPRulesSource
	tenantID string
	now      func() time.Time
}

func NewFeedTransportStage(deps Deps, tenantID string) *FeedTransportStage {
	return &FeedTransportStage{
		repo: feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		// FEED SOP (2026-09-16): every task materialized today is pinned to the transport card in
		// force now, so the phone and the submit judge the same slots.
		rules:    feedsoppg.NewRulesSource(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID: tenantID,
		now:      time.Now,
	}
}
func (s *FeedTransportStage) Name() string { return "feed-transport-issue" }
func (s *FeedTransportStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("feed transport: tenant id is required")
	}
	version := 0
	if s.rules != nil {
		rules, err := s.rules.PublishedRules(ctx, s.tenantID, feeddirectiondomain.StageTransport)
		if err != nil {
			return fmt.Errorf("feed transport: read transport card: %w", err)
		}
		version = rules.Version
	}
	_, err := s.repo.MaterializeTransportTasks(ctx, feeddirectionports.MaterializeTransportParams{TenantID: s.tenantID, AsOf: s.now(), SOPVersion: version})
	return err
}
func (s *FeedTransportStage) withClock(now func() time.Time) *FeedTransportStage {
	s.now = now
	return s
}
