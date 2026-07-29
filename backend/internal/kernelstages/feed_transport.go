package kernelstages

import (
	"context"
	"fmt"
	"time"

	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

type FeedTransportStage struct {
	repo     *feeddirectionpg.Repository
	tenantID string
	now      func() time.Time
}

func NewFeedTransportStage(deps Deps, tenantID string) *FeedTransportStage {
	return &FeedTransportStage{repo: feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout), tenantID: tenantID, now: time.Now}
}
func (s *FeedTransportStage) Name() string { return "feed-transport-issue" }
func (s *FeedTransportStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("feed transport: tenant id is required")
	}
	_, err := s.repo.MaterializeTransportTasks(ctx, feeddirectionports.MaterializeTransportParams{TenantID: s.tenantID, AsOf: s.now()})
	return err
}
func (s *FeedTransportStage) withClock(now func() time.Time) *FeedTransportStage {
	s.now = now
	return s
}
