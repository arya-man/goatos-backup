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
	reserver := inventoryapp.NewService(inventorypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
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
	// Every version swept in this pass shares ONE SweepSession so MaxShotsPerAnimalPerDrive
	// spans vaccines/versions instead of resetting per version, and versions are ordered by
	// resolved vaccine priority (ascending) so higher-priority vaccines claim an
	// over-subscribed animal's slots first, deterministically. See
	// cmd/obligation-sweeper/main.go for the same pattern applied to the one-shot binary.
	plans := make([]obligationapp.SweepVersionPriority, 0, len(versionIDs))
	for _, versionID := range versionIDs {
		if strings.TrimSpace(versionID) == "" {
			continue
		}
		sweepCfg, err := s.buildSweepConfig(ctx, cfg, versionID)
		if err != nil {
			return fmt.Errorf("sweep config version %s: %w", versionID, err)
		}
		plans = append(plans, obligationapp.SweepVersionPriority{VersionID: versionID, Config: sweepCfg})
	}
	plans = obligationapp.SortSweepVersionsByPriority(plans)

	// RV-03: hold ONE per-tenant advisory lock across the whole batch-writing sweep so it is the
	// single priority-ordered writer for the tenant. Without it, a second sweeper process (a kernel
	// replica, or cmd/obligation-sweeper run alongside this stage) can commit a lower-priority shot
	// into an animal's last cap slot while this run refreshes only persisted counts -- inverting the
	// medical plan by lock-acquisition order rather than vaccine priority. If another sweeper already
	// owns the tenant, skip the batch sweep entirely (it will do this work); MarkMissed / reminder /
	// escalation sweeps below are idempotent and non-shot-cap, so they still run.
	acquired, releaseSweep, err := s.sweeper.LockTenantSweep(ctx, cfg.TenantID)
	if err != nil {
		return fmt.Errorf("acquire tenant sweep lock: %w", err)
	}
	defer func() { _ = releaseSweep(context.Background()) }()

	if acquired {
		// RV-05: capture an insertion HWM, then have preflight return the exact candidate-ID snapshot.
		// The HWM excludes later inserts; the snapshot also excludes an older deferred/rescheduled row
		// that becomes eligible only after preflight. Candidate membership therefore cannot grow while
		// earlier plans are being committed; newly eligible work waits for the next cycle.
		createdAtHWM, err := s.sweeper.CaptureSweepHighWaterMark(ctx)
		if err != nil {
			return fmt.Errorf("capture sweep high-water mark: %w", err)
		}

		// VAX-REV-04: detect a cross-version shot-cap priority tie BEFORE sweeping a single plan for
		// real. Without this, a later plan's tie aborted the loop below while earlier plans' batches/
		// SOP tasks/stock reservations were already committed -- a silent, arbitrary partial commit.
		// See obligationapp.PreflightVisitShotCapTies.
		snapshot, err := s.sweeper.PreflightVisitShotCapTiesWithSnapshotAsOf(ctx, cfg.TenantID, plans, now, dueBefore, createdAtHWM)
		if err != nil {
			return fmt.Errorf("preflight shot-cap ties: %w", err)
		}

		session := obligationapp.NewSweepSession()
		// BUG #6 / R2-04: use the snapshot/no-finalize sweep so AlignComboDrives can run BEFORE
		// any stock is reserved or SOP task is created -- reserving/tasking a combo batch on its
		// pre-alignment planned_date would permanently exclude it from the alignment candidate query
		// (sop_task_id IS NULL AND NOT context ? 'stock_reservation'). The HWM variant (RV-05) bounds
		// every real-sweep read to the successful preflight's exact candidate membership.
		for _, plan := range plans {
			result, err := s.sweeper.SweepVersionWithSessionNoFinalizeSnapshotAsOf(ctx, cfg.TenantID, plan.VersionID, plan.Config, now, dueBefore, session, createdAtHWM, snapshot)
			if err != nil {
				return fmt.Errorf("sweep version %s: %w", plan.VersionID, err)
			}
			if s.logger != nil {
				s.logger.Info("obligation_sweep_stage_version",
					"version_id", plan.VersionID,
					"batches", result.Batches,
					"obligations", result.Obligations,
					"park_batches", result.ParkBatches,
					"park_obligations", result.ParkObligations,
				)
			}
		}
		// VAX-REV-04 (BUG #6 / R2-04): align combo drives BEFORE any finalization, so AlignComboDrives can
		// find and move every still-planned combo batch (fresh or retry). After alignment, finalize BOTH
		// stock reservation and SOP-task creation for all swept versions in one pass, against each
		// batch's FINAL (aligned) planned_date.
		if len(plans) > 0 {
			alignWindowDays, maxShotsPerAnimalPerDrive := obligationapp.ComboAlignmentSettingsForPlans(plans)
			if _, err := s.sweeper.AlignComboDrivesAsOf(ctx, cfg.TenantID, alignWindowDays, now, dueBefore, maxShotsPerAnimalPerDrive, session); err != nil {
				return fmt.Errorf("align combo drives: %w", err)
			}
			for _, plan := range plans {
				if err := s.sweeper.FinalizePlannedBatches(ctx, cfg.TenantID, plan.VersionID, plan.Config); err != nil {
					return fmt.Errorf("finalize batches version %s: %w", plan.VersionID, err)
				}
			}
		}
	} else if s.logger != nil {
		s.logger.Info("obligation_sweep_stage_skipped_tenant_locked", "tenant_id", cfg.TenantID)
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
	// BUG #1 / R2-05(a) fix: populate per-rule vaccine identity from rule eligibility_json (populated
	// at publish time from matrix_rows). Used to thread vaccine code/priority through cap/tie
	// detection. Only a COMPLETE identity (non-empty VaccineCode) is cached: a rule whose
	// eligibility_json carries no vaccine (legacy non-matrix rule) or whose extraction fails must be
	// left OUT of the map entirely, so SweepConfig.getRuleVaccineIdentity's cache-miss fallback
	// resolves it to the version-level identity instead of comparing/tie-checking as a blank-code,
	// zero-priority vaccine.
	out.RuleVaccineIDs = make(map[string]obligationapp.RuleVaccineIdentity, len(rules))
	for _, rule := range rules {
		// Extract vaccine identity from rule's eligibility_json (contains matrix row vaccine metadata).
		ruleVaccineID := obligationapp.ExtractRuleVaccineIdentity(rule.EligibilityJSON)
		if ruleVaccineID.VaccineCode != "" {
			out.RuleVaccineIDs[rule.RuleID] = ruleVaccineID
		}

		ruleSOP := strings.TrimSpace(rule.SopVersionID)
		ruleVaccineItemID := strings.TrimSpace(ruleVaccineID.VaccineItemID)
		if (ruleSOP == "" || ruleSOP == versionSOP) && ruleVaccineItemID == "" {
			continue
		}
		if out.RuleConfigs == nil {
			out.RuleConfigs = map[string]obligationapp.SweepRuleConfig{}
		}
		out.RuleConfigs[rule.RuleID] = obligationapp.SweepRuleConfig{
			SOPVersionID:  ruleSOP,
			VaccineItemID: ruleVaccineItemID,
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
