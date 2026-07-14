package kernelstages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
)

// SweeperConfig captures the obligation-sweeper stage tunables, resolved from
// the same env the obligation-sweeper one-shot uses.
type SweeperConfig struct {
	TenantID  string
	VersionID string // empty sweeps all published vaccination versions

	// ActorID is REQUIRED. Without it the sweeper's SOP TaskCreator is nil and
	// SOP-task creation for new batches silently no-ops — obligations get batched
	// but no operator task is ever created. SweeperConfigFromEnv rejects an empty
	// ActorID so a misconfigured deployment fails fast at startup rather than
	// silently dropping SOP tasks forever.
	ActorID       string
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int
	MarkMissed    bool
	MissedGrace   time.Duration

	SweepReminders bool
	ReminderLimit  int

	SweepEscalations bool
	EscalationLimit  int
	Level1After      time.Duration
	Level2After      time.Duration
	Level3After      time.Duration
	Level4After      time.Duration
}

// SweeperConfigFromEnv resolves the sweeper config from env with the same
// defaults the one-shot uses, and enforces the two hard preconditions: a tenant
// and an audited actor id. The actor-id check is the guard for the nil
// TaskCreator footgun (see SweeperConfig.ActorID).
func SweeperConfigFromEnv() (SweeperConfig, error) {
	cfg := SweeperConfig{
		TenantID:         getenv("GOATOS_TENANT_ID"),
		VersionID:        getenv("GOATOS_SWEEPER_VERSION_ID"),
		ActorID:          getenv("GOATOS_SWEEPER_ACTOR_ID"),
		SOPVersionID:     getenv("GOATOS_SWEEPER_SOP_VERSION_ID"),
		VaccineItemID:    getenv("GOATOS_SWEEPER_VACCINE_ITEM_ID"),
		DosesPerGoat:     intEnv("GOATOS_SWEEPER_DOSES_PER_GOAT", 1),
		MarkMissed:       boolEnv("GOATOS_SWEEPER_MARK_MISSED", true),
		MissedGrace:      durationEnv("GOATOS_SWEEPER_MISSED_GRACE", 24*time.Hour),
		SweepReminders:   boolEnv("GOATOS_SWEEPER_SWEEP_REMINDERS", true),
		ReminderLimit:    intEnv("GOATOS_SWEEPER_REMINDER_LIMIT", 100),
		SweepEscalations: boolEnv("GOATOS_SWEEPER_SWEEP_ESCALATIONS", true),
		EscalationLimit:  intEnv("GOATOS_SWEEPER_ESCALATION_LIMIT", 100),
		Level1After:      durationEnv("GOATOS_ESCALATION_LEVEL1_AFTER", 0),
		Level2After:      durationEnv("GOATOS_ESCALATION_LEVEL2_AFTER", 4*time.Hour),
		Level3After:      durationEnv("GOATOS_ESCALATION_LEVEL3_AFTER", 24*time.Hour),
		Level4After:      durationEnv("GOATOS_ESCALATION_LEVEL4_AFTER", 48*time.Hour),
	}
	if cfg.DosesPerGoat < 1 {
		cfg.DosesPerGoat = 1
	}
	if cfg.ReminderLimit <= 0 {
		cfg.ReminderLimit = 100
	}
	if cfg.EscalationLimit < 1 || cfg.EscalationLimit > 500 {
		return SweeperConfig{}, errors.New("GOATOS_SWEEPER_ESCALATION_LIMIT must be between 1 and 500")
	}
	if cfg.MissedGrace < 0 {
		return SweeperConfig{}, errors.New("GOATOS_SWEEPER_MISSED_GRACE must be non-negative")
	}
	if cfg.Level1After < 0 || cfg.Level2After < cfg.Level1After || cfg.Level3After < cfg.Level2After || cfg.Level4After < cfg.Level3After {
		return SweeperConfig{}, errors.New("escalation SLA thresholds must be non-negative and increasing")
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return SweeperConfig{}, errors.New("GOATOS_TENANT_ID is required for the obligation-sweeper stage")
	}
	if strings.TrimSpace(cfg.ActorID) == "" {
		return SweeperConfig{}, errors.New("GOATOS_SWEEPER_ACTOR_ID is required for the obligation-sweeper stage: without it the SOP TaskCreator is nil and batch SOP-task creation silently no-ops")
	}
	return cfg, nil
}

// buildSweeperTaskCreator returns the SOP TaskCreator the sweeper uses to spawn
// batch tasks. It returns nil when actorID is empty — which is precisely the
// silent-no-op footgun the ActorID precondition guards against — so callers must
// validate actorID before wiring the sweeper.
func buildSweeperTaskCreator(deps Deps, actorID string) obligationapp.TaskCreator {
	if strings.TrimSpace(actorID) == "" {
		return nil
	}
	sopService := sopapp.NewService(soppg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return sopbridge.New(&sopServiceAdapter{service: sopService}, actorID)
}

// sopServiceAdapter adapts the SOP service to sopbridge.SOPTaskCreator.
type sopServiceAdapter struct {
	service *sopapp.Service
}

func (a *sopServiceAdapter) CreateTask(ctx context.Context, cmd sopports.CreateTaskCommand) (sopdomain.TaskSummary, error) {
	resp, err := a.service.CreateTask(ctx, cmd, "obligation-sweeper")
	if err != nil {
		return sopdomain.TaskSummary{}, err
	}
	return resp.Task, nil
}

func (a *sopServiceAdapter) CreateTasksForBatches(ctx context.Context, tenantID, sopVersionID, actorID string, tasks []sopdomain.BatchTaskRequest) (map[string]string, error) {
	return a.service.CreateTasksForBatches(ctx, tenantID, sopVersionID, actorID, tasks)
}

// ObligationSweeperStage materializes due obligations into batches + SOP tasks,
// reconciles inventory reservations via the sweeper, marks missed work, and
// queues due reminders and SLA escalations. It reuses
// obligationapp.SweeperService and calendarapp.Service — the same code paths as
// the obligation-sweeper one-shot — MINUS every projection-refresh call (the
// 5k-50k envelope serves screens from canonical tables; the projection calls in
// the one-shot are repair-only). Operational (15m) cadence.
type ObligationSweeperStage struct {
	deps         Deps
	cfg          SweeperConfig
	protocolRepo *protocolpg.Repository
	sweeper      *obligationapp.SweeperService
	calendar     *calendarapp.Service
	logger       *slog.Logger
}

// NewObligationSweeperStage builds the sweeper stage. It requires cfg.ActorID to
// be set (SweeperConfigFromEnv enforces this); the TaskCreator is built here and
// must be non-nil.
func NewObligationSweeperStage(deps Deps, cfg SweeperConfig) *ObligationSweeperStage {
	protocolRepo := protocolpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	var reserver obligationapp.StockReserver
	if strings.TrimSpace(cfg.VaccineItemID) != "" {
		reserver = inventoryapp.NewService(inventorypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	}
	creator := buildSweeperTaskCreator(deps, cfg.ActorID)
	sweeper := obligationapp.NewSweeperService(obligationRepo, creator, reserver)
	calendar := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return &ObligationSweeperStage{
		deps:         deps,
		cfg:          cfg,
		protocolRepo: protocolRepo,
		sweeper:      sweeper,
		calendar:     calendar,
		logger:       deps.Logger,
	}
}

// Name implements worker.StageRunner.
func (s *ObligationSweeperStage) Name() string { return "obligation-sweep" }

// Run performs one operational sweep pass.
func (s *ObligationSweeperStage) Run(ctx context.Context) error {
	cfg := s.cfg
	if strings.TrimSpace(cfg.TenantID) == "" {
		return errors.New("obligation sweeper: tenant id is required")
	}
	now := time.Now().In(biztime.DefaultLocation())
	dueBefore := now
	missedBefore := now.Add(-cfg.MissedGrace)

	versionIDs := []string{cfg.VersionID}
	if cfg.VersionID == "" {
		var err error
		versionIDs, err = s.protocolRepo.ListPublishedVaccinationVersions(ctx, cfg.TenantID)
		if err != nil {
			return fmt.Errorf("list published vaccination versions: %w", err)
		}
	}
	for _, versionID := range versionIDs {
		if strings.TrimSpace(versionID) == "" {
			continue
		}
		sweepCfg, err := s.buildSweepConfig(ctx, cfg, versionID)
		if err != nil {
			return fmt.Errorf("sweep config version %s: %w", versionID, err)
		}
		result, err := s.sweeper.SweepVersion(ctx, cfg.TenantID, versionID, sweepCfg, dueBefore)
		if err != nil {
			return fmt.Errorf("sweep version %s: %w", versionID, err)
		}
		if s.logger != nil {
			s.logger.Info("obligation_sweep_stage_version",
				"version_id", versionID,
				"batches", result.Batches,
				"obligations", result.Obligations,
				"park_batches", result.ParkBatches,
				"park_obligations", result.ParkObligations,
			)
		}
	}
	if len(versionIDs) > 0 {
		if _, err := s.sweeper.AlignComboDrives(ctx, cfg.TenantID, obligationdomain.DefaultDrivePlannerSettings().ComboAlignWindowDays, dueBefore); err != nil {
			return fmt.Errorf("align combo drives: %w", err)
		}
	}
	if cfg.MarkMissed {
		if _, err := s.sweeper.MarkMissed(ctx, cfg.TenantID, missedBefore); err != nil {
			return fmt.Errorf("mark missed obligations: %w", err)
		}
	}
	if cfg.SweepReminders {
		queued, err := s.calendar.SweepDueReminders(ctx, cfg.TenantID, cfg.ReminderLimit)
		if err != nil {
			return fmt.Errorf("sweep calendar reminders: %w", err)
		}
		if s.logger != nil {
			s.logger.Info("obligation_sweep_stage_reminders", "queued", queued)
		}
	}
	if cfg.SweepEscalations {
		queued, err := s.calendar.SweepEscalations(ctx, calendarports.SweepEscalations{
			TenantID:    cfg.TenantID,
			Limit:       cfg.EscalationLimit,
			Now:         now,
			Level1After: cfg.Level1After,
			Level2After: cfg.Level2After,
			Level3After: cfg.Level3After,
			Level4After: cfg.Level4After,
		})
		if err != nil {
			return fmt.Errorf("sweep calendar escalations: %w", err)
		}
		if s.logger != nil {
			s.logger.Info("obligation_sweep_stage_escalations", "queued", queued)
		}
	}
	return nil
}

func (s *ObligationSweeperStage) buildSweepConfig(ctx context.Context, cfg SweeperConfig, versionID string) (obligationapp.SweepConfig, error) {
	version, err := s.protocolRepo.GetVersion(ctx, cfg.TenantID, versionID)
	if err != nil {
		return obligationapp.SweepConfig{}, err
	}
	versionSOP := strings.TrimSpace(version.SopVersionID)
	if versionSOP == "" {
		versionSOP = strings.TrimSpace(cfg.SOPVersionID)
	}
	vaccineItemID := strings.TrimSpace(cfg.VaccineItemID)
	if vaccineItemID == "" {
		vaccineItemID = stockItemIDFromRuleDSL(version.RuleDsl)
	}
	vaccineCode, drivePlanner := obligationapp.DrivePlannerFromRuleDSL(version.RuleDsl)
	out := obligationapp.SweepConfig{
		SOPVersionID:      versionSOP,
		VaccineItemID:     vaccineItemID,
		VaccineCode:       vaccineCode,
		DosesPerGoat:      int32(cfg.DosesPerGoat),
		ParkConsolidation: obligationdomain.DefaultParkConsolidationSettings(),
		DrivePlanner:      drivePlanner,
	}
	rules, err := s.protocolRepo.ListRules(ctx, cfg.TenantID, versionID)
	if err != nil {
		return obligationapp.SweepConfig{}, err
	}
	for _, rule := range rules {
		ruleSOP := strings.TrimSpace(rule.SopVersionID)
		if ruleSOP == "" || ruleSOP == versionSOP {
			continue
		}
		if out.RuleConfigs == nil {
			out.RuleConfigs = map[string]obligationapp.SweepRuleConfig{}
		}
		out.RuleConfigs[rule.RuleID] = obligationapp.SweepRuleConfig{
			SOPVersionID:  ruleSOP,
			VaccineItemID: out.VaccineItemID,
			DosesPerGoat:  out.DosesPerGoat,
		}
	}
	return out, nil
}

func stockItemIDFromRuleDSL(raw []byte) string {
	var dsl struct {
		StockPolicy struct {
			ItemID        string `json:"item_id"`
			VaccineItemID string `json:"vaccine_item_id"`
		} `json:"stock_policy"`
	}
	if len(raw) == 0 {
		return ""
	}
	if err := json.Unmarshal(raw, &dsl); err != nil {
		return ""
	}
	if itemID := strings.TrimSpace(dsl.StockPolicy.VaccineItemID); itemID != "" {
		return itemID
	}
	return strings.TrimSpace(dsl.StockPolicy.ItemID)
}
