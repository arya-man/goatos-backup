package kernelstages

import (
	"context"
	"fmt"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"time"
)

type MilkFeedingStage struct {
	repo     *countspg.Repository
	tenantID string
	now      func() time.Time
}

func NewMilkFeedingStage(deps Deps, tenantID string) *MilkFeedingStage {
	return &MilkFeedingStage{repo: countspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout), tenantID: tenantID, now: time.Now}
}
func (s *MilkFeedingStage) Name() string { return "milk-feeding-issue" }
func (s *MilkFeedingStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("milk feeding: tenant id is required")
	}
	_, err := s.repo.MaterializeMilkFeedingTasks(ctx, countsdomain.MilkFeedingMaterializeRequest{TenantID: s.tenantID, FeedingDate: s.now()})
	return err
}
func (s *MilkFeedingStage) withClock(now func() time.Time) *MilkFeedingStage { s.now = now; return s }
