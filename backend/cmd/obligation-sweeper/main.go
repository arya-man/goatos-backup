package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/taskqueue"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
)

// sopServiceAdapter wraps the SOP service to match sopbridge.SOPTaskCreator interface
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

type config struct {
	TenantID      string
	VersionID     string
	AsOf          time.Time
	DueBefore     time.Time
	Timeout       time.Duration
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int
	ActorID       string
	MarkMissed    bool
	MissedBefore  time.Time

	SweepReminders bool
	ReminderLimit  int

	SweepEscalations bool
	EscalationLimit  int
	Level1After      time.Duration
	Level2After      time.Duration
	Level3After      time.Duration
	Level4After      time.Duration
	DoseCodes        map[string]struct{}
	TargetIDs        map[string]struct{}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlagsAt(args, time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "obligation-sweeper"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	reserver := inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	var creator obligationapp.TaskCreator
	if cfg.ActorID != "" {
		sopService := sopapp.NewService(soppg.NewRepository(pool, pgCfg.QueryTimeout))
		adapter := &sopServiceAdapter{service: sopService}
		creator = sopbridge.New(adapter, cfg.ActorID)
	}
	sweeper := obligationapp.NewSweeperService(obligationRepo, creator, reserver)
	var batchSweepErr error
	batchSweepPhase := "batch"
	versionIDs := []string{cfg.VersionID}
	if cfg.VersionID == "" {
		versionIDs, err = protocolRepo.ListPublishedVaccinationVersions(ctx, cfg.TenantID)
		if err != nil {
			batchSweepPhase = "list_versions"
			batchSweepErr = fmt.Errorf("list published vaccination versions: %w", err)
		}
	}
	if batchSweepErr == nil && len(versionIDs) == 0 {
		fmt.Println("no published vaccination protocol versions to sweep")
	} else {
		// Build every version's sweep config up front, then sort by resolved vaccine priority
		// (ascending -- highest disease priority first) before sweeping. All versions in this
		// run share ONE SweepSession so MaxShotsPerAnimalPerDrive is enforced across
		// vaccines/versions, not reset per version (an animal due 3 vaccines the same day would
		// otherwise be scheduled 3 shots because each version started counting from zero).
		// Sweeping in priority order lets higher-priority vaccines claim an over-subscribed
		// animal's slots first, deterministically, regardless of the arbitrary order
		// ListPublishedVaccinationVersions returned. An unresolved same-priority conflict at a
		// real shared visit is still reported by the sweeper itself
		// (obligationapp.ShotCapPriorityTieError), not silently decided by this ordering.
		plans := make([]obligationapp.SweepVersionPriority, 0, len(versionIDs))
		if batchSweepErr == nil {
			for _, versionID := range versionIDs {
				sweepCfg, err := buildSweepConfig(ctx, protocolRepo, cfg, versionID)
				if err != nil {
					batchSweepPhase = "build_config"
					batchSweepErr = fmt.Errorf("sweep config version %s: %w", versionID, err)
					break
				}
				if len(cfg.DoseCodes) > 0 && len(sweepCfg.AllowedRuleIDs) == 0 {
					continue
				}
				sweepCfg.AllowedTargetIDs = cfg.TargetIDs
				plans = append(plans, obligationapp.SweepVersionPriority{VersionID: versionID, Config: sweepCfg})
			}
		}
		plans = obligationapp.SortSweepVersionsByPriority(plans)

		// RV-03: serialize the whole batch sweep per tenant so this run is the single priority-ordered
		// writer. If another sweeper (the kernel obligation-sweeper stage, or a second replica) already
		// owns the tenant, skip -- otherwise both could commit into an animal's last cap slot and let
		// lock-acquisition order, not vaccine priority, decide the medical plan.
		acquired := false
		var releaseSweep func(context.Context) error
		if batchSweepErr == nil {
			acquired, releaseSweep, err = sweeper.LockTenantSweep(ctx, cfg.TenantID)
			if err != nil {
				batchSweepPhase = "tenant_lock"
				batchSweepErr = fmt.Errorf("acquire tenant sweep lock: %w", err)
			} else {
				defer func() { _ = releaseSweep(context.Background()) }()
			}
		}
		if batchSweepErr == nil && !acquired {
			fmt.Printf("obligation sweep skipped: tenant %s already locked by another sweeper\n", cfg.TenantID)
		} else if batchSweepErr == nil {
			// RV-05: the HWM excludes later inserts and the preflight snapshot below excludes older rows
			// that become eligible only after preflight. Candidate membership cannot grow mid-cycle.
			createdAtHWM, err := sweeper.CaptureSweepHighWaterMark(ctx)
			if err != nil {
				batchSweepPhase = "high_water_mark"
				batchSweepErr = fmt.Errorf("capture sweep high-water mark: %w", err)
			}

			// VAX-REV-04: detect a cross-version shot-cap priority tie BEFORE sweeping a single plan
			// for real. Without this, a later plan's tie aborted the loop below while earlier plans'
			// batches/SOP tasks/stock reservations were already committed -- a silent, arbitrary
			// partial commit. See obligationapp.PreflightVisitShotCapTies.
			var snapshot *obligationapp.SweepCandidateSnapshot
			if batchSweepErr == nil {
				snapshot, err = sweeper.PreflightVisitShotCapTiesWithSnapshotAsOf(ctx, cfg.TenantID, plans, cfg.AsOf, cfg.DueBefore, createdAtHWM)
				if err != nil {
					batchSweepPhase = "preflight"
					batchSweepErr = fmt.Errorf("preflight shot-cap ties: %w", err)
				}
			}

			session := obligationapp.NewSweepSession()
			// R2-04 fix: use the snapshot/no-finalize sweep so AlignComboDrives (below) runs
			// BEFORE any stock is reserved or SOP task is created. Finalizing a combo batch on its
			// pre-alignment planned_date would permanently exclude it from AlignComboDrives' candidate
			// query (sop_task_id IS NULL AND NOT context ? 'stock_reservation'). The HWM variant (RV-05)
			// bounds every real-sweep read to the successful preflight's exact candidate membership.
			if batchSweepErr == nil {
				for _, plan := range plans {
					sweepStart := time.Now()
					result, err := sweeper.SweepVersionWithSessionNoFinalizeSnapshotAsOf(ctx, cfg.TenantID, plan.VersionID, plan.Config, cfg.AsOf, cfg.DueBefore, session, createdAtHWM, snapshot)
					// tasksCreated approximates 1 SOP batch task per obligation batch -
					// obligationapp.SweepResult does not return a distinct tasks-created count, and batches
					// are only task-bearing when a TaskCreator (creator, gated on --actor-id) is configured.
					tasksCreated := 0
					if creator != nil {
						tasksCreated = result.Batches + result.ParkBatches
					}
					kmetrics.RecordSweeperBatch(ctx, "version", time.Since(sweepStart).Seconds(), result.Obligations+result.ParkObligations, tasksCreated)
					if err != nil {
						batchSweepPhase = "version_sweep"
						batchSweepErr = fmt.Errorf("sweep version %s: %w", plan.VersionID, err)
						break
					}
					fmt.Printf("swept version=%s batches=%d obligations=%d park_batches=%d park_obligations=%d\n",
						plan.VersionID, result.Batches, result.Obligations, result.ParkBatches, result.ParkObligations)
				}
			}
			if batchSweepErr == nil {
				alignWindowDays, maxShotsPerAnimalPerDrive, maxDriveCells := obligationapp.ComboAlignmentSettingsForPlans(plans)
				aligned, err := sweeper.AlignComboDrivesAsOf(ctx, cfg.TenantID, alignWindowDays, cfg.AsOf, cfg.DueBefore, maxShotsPerAnimalPerDrive, maxDriveCells, session)
				if err != nil {
					batchSweepPhase = "combo_alignment"
					batchSweepErr = fmt.Errorf("align combo drives: %w", err)
				}
				if aligned > 0 {
					fmt.Printf("combo drive dates aligned=%d\n", aligned)
				}
			}
			if batchSweepErr == nil {
				// R2-04: finalize stock reservation + SOP-task creation for every swept version now that
				// alignment has settled each planned batch's final date.
				for _, plan := range plans {
					if err := sweeper.FinalizePlannedBatches(ctx, cfg.TenantID, plan.VersionID, plan.Config); err != nil {
						batchSweepPhase = "finalize"
						batchSweepErr = fmt.Errorf("finalize batches version %s: %w", plan.VersionID, err)
						break
					}
				}
			}
		}
		if batchSweepErr != nil {
			kmetrics.RecordSweeperFailure(ctx, batchSweepPhase)
			recordSweeperFailure(ctx, pool, pgCfg.QueryTimeout, cfg, batchSweepPhase, batchSweepErr)
			fmt.Fprintf(os.Stderr, "obligation batch sweep failed; continuing to mark missed/reminders: %v\n", batchSweepErr)
		}
		if cfg.MarkMissed {
			missed, err := sweeper.MarkMissed(ctx, cfg.TenantID, cfg.MissedBefore)
			if err != nil {
				if batchSweepErr != nil {
					return errors.Join(batchSweepErr, fmt.Errorf("mark missed obligations: %w", err))
				}
				return fmt.Errorf("mark missed obligations: %w", err)
			}
			fmt.Printf("marked missed obligations=%d before=%s\n", missed, cfg.MissedBefore.Format(time.RFC3339))
		}
		if batchSweepErr != nil {
			return batchSweepErr
		}
	}
	// 5k-50k envelope (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): Calendar has no
	// projection to refresh anymore -- it serves canonical reads directly, which are never stale
	// relative to the canonical write these sweeps just performed.
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, pgCfg.QueryTimeout))
	queuedNotifications := 0
	if cfg.SweepReminders {
		queued, err := calendarService.SweepDueReminders(ctx, cfg.TenantID, cfg.ReminderLimit)
		if err != nil {
			return fmt.Errorf("sweep calendar reminders: %w", err)
		}
		queuedNotifications += queued
		fmt.Printf("calendar reminders queued=%d\n", queued)
	}
	if cfg.SweepEscalations {
		queued, err := calendarService.SweepEscalations(ctx, calendarports.SweepEscalations{
			TenantID:    cfg.TenantID,
			Limit:       cfg.EscalationLimit,
			Now:         time.Now().In(biztime.DefaultLocation()),
			Level1After: cfg.Level1After,
			Level2After: cfg.Level2After,
			Level3After: cfg.Level3After,
			Level4After: cfg.Level4After,
		})
		if err != nil {
			return fmt.Errorf("sweep calendar escalations: %w", err)
		}
		queuedNotifications += queued
		fmt.Printf("calendar escalations queued=%d\n", queued)
	}
	if queuedNotifications > 0 {
		if err := enqueueNotificationDispatcher(ctx, cfg.TenantID, "obligation-sweeper"); err != nil {
			return err
		}
	}
	return nil
}

func buildSweepConfig(ctx context.Context, protocolRepo *protocolpg.Repository, cfg config, versionID string) (obligationapp.SweepConfig, error) {
	version, err := protocolRepo.GetVersion(ctx, cfg.TenantID, versionID)
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
	rules, err := protocolRepo.ListRules(ctx, cfg.TenantID, versionID)
	if err != nil {
		return obligationapp.SweepConfig{}, err
	}
	out.RuleVaccineIDs = make(map[string]obligationapp.RuleVaccineIdentity, len(rules))
	for _, rule := range rules {
		if len(cfg.DoseCodes) > 0 {
			if _, ok := cfg.DoseCodes[strings.ToLower(strings.TrimSpace(rule.DoseCode))]; ok {
				if out.AllowedRuleIDs == nil {
					out.AllowedRuleIDs = map[string]struct{}{}
				}
				out.AllowedRuleIDs[rule.RuleID] = struct{}{}
			}
		}
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

func parseFlags(args []string) (config, error) {
	return parseFlagsAt(args, time.Now().In(biztime.DefaultLocation()))
}

// parseFlagsAt parses CLI flags/env against an explicit clock instant so the
// default -due-before boundary is deterministic and testable (PEND-6): two
// calls made at different instants within the same IST business day must
// resolve to the identical default DueBefore.
func parseFlagsAt(args []string, now time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("obligation-sweeper", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.VersionID, "version-id", "", "protocol version id; empty sweeps all published vaccination versions")
	fs.StringVar(&cfg.SOPVersionID, "sop-version-id", getenv("GOATOS_SWEEPER_SOP_VERSION_ID"), "SOP version id used when creating batch tasks")
	fs.StringVar(&cfg.VaccineItemID, "vaccine-item-id", getenv("GOATOS_SWEEPER_VACCINE_ITEM_ID"), "vaccine inventory item id used for FEFO reserve")
	fs.StringVar(&cfg.ActorID, "actor-id", getenv("GOATOS_SWEEPER_ACTOR_ID"), "actor id for SOP task creation")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_SWEEPER_TIMEOUT", 60*time.Second), "sweeper timeout")
	asOfRaw := fs.String("as-of", getenv("GOATOS_SWEEPER_AS_OF"), "RFC3339 operational sweep day; default now")
	dueBeforeRaw := fs.String("due-before", getenv("GOATOS_SWEEPER_DUE_BEFORE"), "RFC3339 due-before cutoff; default now")
	missedBeforeRaw := fs.String("missed-before", getenv("GOATOS_SWEEPER_MISSED_BEFORE"), "RFC3339 missed cutoff; default now minus missed-grace")
	missedGrace := fs.Duration("missed-grace", durationEnv("GOATOS_SWEEPER_MISSED_GRACE", 24*time.Hour), "grace period before due/window-crossed obligations become missed")
	fs.BoolVar(&cfg.MarkMissed, "mark-missed", boolEnv("GOATOS_SWEEPER_MARK_MISSED", true), "materialize canonical missed status for overdue open obligations")
	fs.IntVar(&cfg.DosesPerGoat, "doses-per-goat", intEnv("GOATOS_SWEEPER_DOSES_PER_GOAT", 1), "doses reserved per goat")
	fs.BoolVar(&cfg.SweepReminders, "sweep-reminders", boolEnv("GOATOS_SWEEPER_SWEEP_REMINDERS", true), "queue due Calendar reminders after projection refresh")
	fs.IntVar(&cfg.ReminderLimit, "reminder-limit", intEnv("GOATOS_SWEEPER_REMINDER_LIMIT", 100), "max reminders to queue")
	fs.BoolVar(&cfg.SweepEscalations, "sweep-escalations", boolEnv("GOATOS_SWEEPER_SWEEP_ESCALATIONS", true), "queue SLA escalations after projection refresh")
	fs.IntVar(&cfg.EscalationLimit, "escalation-limit", intEnv("GOATOS_SWEEPER_ESCALATION_LIMIT", 100), "max escalations to queue")
	fs.DurationVar(&cfg.Level1After, "level1-after", durationEnv("GOATOS_ESCALATION_LEVEL1_AFTER", 0), "level 1 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level2After, "level2-after", durationEnv("GOATOS_ESCALATION_LEVEL2_AFTER", 4*time.Hour), "level 2 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level3After, "level3-after", durationEnv("GOATOS_ESCALATION_LEVEL3_AFTER", 24*time.Hour), "level 3 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level4After, "level4-after", durationEnv("GOATOS_ESCALATION_LEVEL4_AFTER", 48*time.Hour), "level 4 SLA threshold after due_at")
	doseCodesRaw := fs.String("dose-codes", getenv("GOATOS_SWEEPER_DOSE_CODES"), "optional comma-separated protocol dose_code allowlist for campaign-only sweeps")
	targetIDsFile := fs.String("target-ids-file", getenv("GOATOS_SWEEPER_TARGET_IDS_FILE"), "optional newline-delimited target goat UUID allowlist for campaign-only sweeps")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	now = now.In(biztime.DefaultLocation())
	cfg.AsOf = now
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.In(biztime.DefaultLocation())
	}
	// PEND-6: default DueBefore is the END of the current IST business day, not the
	// instant `now`. Sweeping with due-before=now only picks up obligations due at or
	// before this exact second, so a run late in the business day silently skips
	// same-day work still due later that day. cfg.AsOf stays the real instant `now`
	// (correct for hold/backdate decisions); only the default due-before cutoff widens
	// to the full business day.
	cfg.DueBefore = biztime.BusinessDayStart(now).Add(24 * time.Hour)
	if strings.TrimSpace(*dueBeforeRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*dueBeforeRaw))
		if err != nil {
			return config{}, errors.New("due-before must be RFC3339")
		}
		cfg.DueBefore = parsed.In(biztime.DefaultLocation())
	}
	if *missedGrace < 0 {
		return config{}, errors.New("missed-grace must be non-negative")
	}
	cfg.MissedBefore = now.Add(-*missedGrace)
	if strings.TrimSpace(*missedBeforeRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*missedBeforeRaw))
		if err != nil {
			return config{}, errors.New("missed-before must be RFC3339")
		}
		cfg.MissedBefore = parsed.In(biztime.DefaultLocation())
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.DosesPerGoat < 1 {
		cfg.DosesPerGoat = 1
	}
	cfg.DoseCodes = parseDoseCodeSet(*doseCodesRaw)
	targetIDs, err := readIDSetFile(*targetIDsFile)
	if err != nil {
		return config{}, err
	}
	cfg.TargetIDs = targetIDs
	if cfg.ReminderLimit <= 0 {
		cfg.ReminderLimit = 100
	}
	if cfg.EscalationLimit < 1 || cfg.EscalationLimit > 500 {
		return config{}, errors.New("escalation-limit must be between 1 and 500")
	}
	if cfg.Level1After < 0 || cfg.Level2After < cfg.Level1After || cfg.Level3After < cfg.Level2After || cfg.Level4After < cfg.Level3After {
		return config{}, errors.New("SLA thresholds must be non-negative and increasing")
	}
	// Published protocol/rule rows can supply SOP bindings after flag parsing, so
	// checking only the optional CLI overrides is unsafe: the deployed worker can
	// otherwise discover task-bearing rules later while its TaskCreator is nil.
	// Require the audited actor for every sweeper invocation and fail before any
	// obligation/calendar mutation when deployment wiring is incomplete.
	if strings.TrimSpace(cfg.ActorID) == "" {
		return config{}, errors.New("actor-id is required for obligation-sweeper")
	}
	return cfg, nil
}

func readIDSetFile(path string) (map[string]struct{}, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read target ids file: %w", err)
	}
	out := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		id := strings.TrimSpace(line)
		if id == "" || strings.HasPrefix(id, "#") {
			continue
		}
		out[id] = struct{}{}
	}
	if len(out) == 0 {
		return nil, errors.New("target ids file contained no ids")
	}
	return out, nil
}

func parseDoseCodeSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		code := strings.ToLower(strings.TrimSpace(part))
		if code == "" {
			continue
		}
		out[code] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func enqueueNotificationDispatcher(ctx context.Context, tenantID, source string) error {
	cfg, enabled, err := taskqueue.ConfigFromEnv()
	if err != nil || !enabled {
		return err
	}
	enqueuer, err := taskqueue.NewEnqueuer(ctx, cfg)
	if err != nil {
		return err
	}
	defer enqueuer.Close()
	taskID := taskqueue.SafeTaskID(fmt.Sprintf("notification-dispatcher-%s-%s-%s", tenantID, source, time.Now().In(biztime.DefaultLocation()).Format("200601021504")))
	return enqueuer.EnqueueJSONPost(ctx, taskID, map[string]any{}, time.Time{})
}

func recordSweeperFailure(ctx context.Context, pool *pgxpool.Pool, queryTimeout time.Duration, cfg config, phase string, failure error) {
	if pool == nil || failure == nil {
		return
	}
	recorder := audit.NewPostgresRecorder(pool, queryTimeout)
	now := time.Now().In(biztime.DefaultLocation())
	if err := recorder.Record(ctx, audit.Event{
		TenantID:     cfg.TenantID,
		ActorID:      cfg.ActorID,
		ActorType:    "system",
		Action:       "obligation_sweeper.batch_failed_continued_to_missed",
		ResourceType: "obligation_sweeper",
		Metadata: map[string]any{
			"domain":        "preventive_care",
			"module":        "vaccination",
			"category":      "sweeper",
			"phase":         phase,
			"error":         failure.Error(),
			"as_of":         cfg.AsOf.Format(time.RFC3339),
			"due_before":    cfg.DueBefore.Format(time.RFC3339),
			"missed_before": cfg.MissedBefore.Format(time.RFC3339),
			"continued":     true,
			"source":        "cmd/obligation-sweeper",
		},
		TraceID: "obligation-sweeper:" + now.Format("20060102T150405Z0700"),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "obligation batch sweep failure audit failed: %v\n", err)
	}
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func intEnv(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
