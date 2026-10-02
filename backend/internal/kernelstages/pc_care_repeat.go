package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	fwrpg "github.com/vgoats/goatos/backend/internal/feedwaterremoval/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaresoppg "github.com/vgoats/goatos/backend/internal/pccaresop/adapters/postgres"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// PcCareRepeatStage plans the next Preventive Care task of a pen when its SOP card says "repeat
// every N days", and the next pen of a card set to ROTATE through the pens (2026-10-02) (maintainer instruction 2026-09-30, docs/decisions/pc-care-repeat.md): same pens,
// same operators, N days after the last task's planned date, RepeatLeadDays ahead. Idempotent
// from the data -- a task is repeated at most once (unique repeat_of_task_id) and a skip is
// recorded once -- so every tick after the first finds nothing to do for that pen. Registered on
// the operational lane beside pc-care-kernel.
type PcCareRepeatStage struct {
	service  *pccareapp.Service
	store    *pccarepg.Repository
	alerter  pccareapp.RepeatAlerter
	tenantID string
	limit    int
	logger   *slog.Logger
}

// NewPcCareRepeatStage builds the stage over the same repository, published SOP and removal
// cutoff the API's planner uses.
func NewPcCareRepeatStage(deps Deps, tenantID string, logger *slog.Logger) *PcCareRepeatStage {
	limit := intEnv("GOATOS_PC_CARE_REPEAT_LIMIT", 500)
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	repo := pccarepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	service := pccareapp.NewService(repo).
		WithRoundStore(repo).
		WithFeedWaterRemovalCutoff(fwrpg.NewReader(deps.Pool, deps.PgCfg.QueryTimeout)).
		WithSOPRules(pccaresoppg.NewRulesSource(deps.Pool, deps.PgCfg.QueryTimeout), repo)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	return &PcCareRepeatStage{
		service:  service,
		store:    repo,
		alerter:  notificationbridge.NewPCCareRepeatSkippedNotifier(notifyRecipients(deps, rosterService, logger), calendarService, logger),
		tenantID: strings.TrimSpace(tenantID),
		limit:    limit,
		logger:   logger,
	}
}

// Name implements worker.StageRunner.
func (s *PcCareRepeatStage) Name() string { return "pc-care-repeat" }

// Run performs one bounded tick.
func (s *PcCareRepeatStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("pc care repeat: tenant id is required")
	}
	result, err := s.service.RunRepeat(ctx, s.tenantID, s.store, s.alerter, s.limit)
	if err != nil {
		return err
	}
	if result.RoundsCreated > 0 || result.PensSkipped > 0 || result.Conflicts > 0 {
		s.logger.InfoContext(ctx, "pc care repeat tick",
			"rounds_created", result.RoundsCreated, "pens_created", result.PensCreated,
			"pens_skipped", result.PensSkipped, "conflicts", result.Conflicts)
	}
	// The ROTATION half (2026-10-02, docs/decisions/pc-care-rotation.md) rides the same stage:
	// same store, same published SOP, same skip alert. A card is either one or the other, so the
	// two passes never plan the same work.
	rotation, err := s.service.RunRotation(ctx, s.tenantID, s.store, s.alerter, s.limit)
	if err != nil {
		return err
	}
	if rotation.PensCreated > 0 || rotation.PensSkipped > 0 || rotation.Conflicts > 0 {
		s.logger.InfoContext(ctx, "pc care rotation tick",
			"pens_created", rotation.PensCreated, "pens_skipped", rotation.PensSkipped, "conflicts", rotation.Conflicts)
	}
	return nil
}

// WithClock pins the stage's clock (tests and E2E drives); production uses time.Now.
func (s *PcCareRepeatStage) WithClock(now func() time.Time) *PcCareRepeatStage {
	s.service.WithNow(now)
	return s
}
