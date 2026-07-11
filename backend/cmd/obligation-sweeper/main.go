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

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obligationdomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/taskqueue"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
)

type config struct {
	TenantID      string
	VersionID     string
	DueBefore     time.Time
	Timeout       time.Duration
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int
	ActorID       string
	MarkMissed    bool
	MissedBefore  time.Time

	ProjectCalendar  bool
	CalendarDateFrom time.Time
	CalendarDateTo   time.Time
	CalendarLimit    int

	SweepReminders bool
	ReminderLimit  int

	SweepEscalations bool
	EscalationLimit  int
	Level1After      time.Duration
	Level2After      time.Duration
	Level3After      time.Duration
	Level4After      time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	var reserver obligationapp.StockReserver
	if cfg.VaccineItemID != "" {
		reserver = inventoryapp.NewService(inventorypg.NewRepository(pool, pgCfg.QueryTimeout))
	}
	var creator obligationapp.TaskCreator
	if cfg.ActorID != "" {
		creator = sopTaskCreator{
			service: sopapp.NewService(soppg.NewRepository(pool, pgCfg.QueryTimeout)),
			actorID: cfg.ActorID,
		}
	}
	sweeper := obligationapp.NewSweeperService(obligationRepo, creator, reserver)
	versionIDs := []string{cfg.VersionID}
	if cfg.VersionID == "" {
		versionIDs, err = protocolRepo.ListPublishedVaccinationVersions(ctx, cfg.TenantID)
		if err != nil {
			return err
		}
	}
	if len(versionIDs) == 0 {
		fmt.Println("no published vaccination protocol versions to sweep")
	} else {
		for _, versionID := range versionIDs {
			sweepCfg, err := buildSweepConfig(ctx, protocolRepo, cfg, versionID)
			if err != nil {
				return fmt.Errorf("sweep config version %s: %w", versionID, err)
			}
			result, err := sweeper.SweepVersion(ctx, cfg.TenantID, versionID, sweepCfg, cfg.DueBefore)
			if err != nil {
				return fmt.Errorf("sweep version %s: %w", versionID, err)
			}
			fmt.Printf("swept version=%s batches=%d obligations=%d park_batches=%d park_obligations=%d\n",
				versionID, result.Batches, result.Obligations, result.ParkBatches, result.ParkObligations)
		}
		aligned, err := sweeper.AlignComboDrives(ctx, cfg.TenantID, obligationdomain.DefaultDrivePlannerSettings().ComboAlignWindowDays, cfg.DueBefore)
		if err != nil {
			return fmt.Errorf("align combo drives: %w", err)
		}
		if aligned > 0 {
			fmt.Printf("combo drive dates aligned=%d\n", aligned)
		}
	}
	if cfg.MarkMissed {
		missed, err := sweeper.MarkMissed(ctx, cfg.TenantID, cfg.MissedBefore)
		if err != nil {
			return fmt.Errorf("mark missed obligations: %w", err)
		}
		fmt.Printf("marked missed obligations=%d before=%s\n", missed, cfg.MissedBefore.Format(time.RFC3339))
	}
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, pgCfg.QueryTimeout))
	if cfg.ProjectCalendar {
		refreshed, err := calendarService.RefreshVaccinationProjection(ctx, calendarports.RefreshVaccinationProjection{
			TenantID: cfg.TenantID,
			DateFrom: cfg.CalendarDateFrom,
			DateTo:   cfg.CalendarDateTo,
			Limit:    cfg.CalendarLimit,
		})
		if err != nil {
			return fmt.Errorf("refresh calendar vaccination projection: %w", err)
		}
		fmt.Printf("calendar projection refreshed=%d from=%s to=%s\n", refreshed, cfg.CalendarDateFrom.Format(time.RFC3339), cfg.CalendarDateTo.Format(time.RFC3339))
	}
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

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("obligation-sweeper", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.VersionID, "version-id", "", "protocol version id; empty sweeps all published vaccination versions")
	fs.StringVar(&cfg.SOPVersionID, "sop-version-id", getenv("GOATOS_SWEEPER_SOP_VERSION_ID"), "SOP version id used when creating batch tasks")
	fs.StringVar(&cfg.VaccineItemID, "vaccine-item-id", getenv("GOATOS_SWEEPER_VACCINE_ITEM_ID"), "vaccine inventory item id used for FEFO reserve")
	fs.StringVar(&cfg.ActorID, "actor-id", getenv("GOATOS_SWEEPER_ACTOR_ID"), "actor id for SOP task creation")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_SWEEPER_TIMEOUT", 60*time.Second), "sweeper timeout")
	dueBeforeRaw := fs.String("due-before", getenv("GOATOS_SWEEPER_DUE_BEFORE"), "RFC3339 due-before cutoff; default now")
	missedBeforeRaw := fs.String("missed-before", getenv("GOATOS_SWEEPER_MISSED_BEFORE"), "RFC3339 missed cutoff; default now minus missed-grace")
	missedGrace := fs.Duration("missed-grace", durationEnv("GOATOS_SWEEPER_MISSED_GRACE", 24*time.Hour), "grace period before due/window-crossed obligations become missed")
	fs.BoolVar(&cfg.MarkMissed, "mark-missed", boolEnv("GOATOS_SWEEPER_MARK_MISSED", true), "materialize canonical missed status for overdue open obligations")
	fs.IntVar(&cfg.DosesPerGoat, "doses-per-goat", intEnv("GOATOS_SWEEPER_DOSES_PER_GOAT", 1), "doses reserved per goat")
	calendarDateFromRaw := fs.String("calendar-date-from", getenv("GOATOS_SWEEPER_CALENDAR_DATE_FROM"), "RFC3339 calendar projection lower bound; default now minus 24h")
	calendarDateToRaw := fs.String("calendar-date-to", getenv("GOATOS_SWEEPER_CALENDAR_DATE_TO"), "RFC3339 calendar projection upper bound; default now plus 45d")
	fs.BoolVar(&cfg.ProjectCalendar, "project-calendar", boolEnv("GOATOS_SWEEPER_PROJECT_CALENDAR", true), "refresh Calendar vaccination projection after obligation sweep")
	fs.IntVar(&cfg.CalendarLimit, "calendar-limit", intEnv("GOATOS_SWEEPER_CALENDAR_LIMIT", 1000), "max Calendar projection rows to upsert")
	fs.BoolVar(&cfg.SweepReminders, "sweep-reminders", boolEnv("GOATOS_SWEEPER_SWEEP_REMINDERS", true), "queue due Calendar reminders after projection refresh")
	fs.IntVar(&cfg.ReminderLimit, "reminder-limit", intEnv("GOATOS_SWEEPER_REMINDER_LIMIT", 100), "max reminders to queue")
	fs.BoolVar(&cfg.SweepEscalations, "sweep-escalations", boolEnv("GOATOS_SWEEPER_SWEEP_ESCALATIONS", true), "queue SLA escalations after projection refresh")
	fs.IntVar(&cfg.EscalationLimit, "escalation-limit", intEnv("GOATOS_SWEEPER_ESCALATION_LIMIT", 100), "max escalations to queue")
	fs.DurationVar(&cfg.Level1After, "level1-after", durationEnv("GOATOS_ESCALATION_LEVEL1_AFTER", 0), "level 1 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level2After, "level2-after", durationEnv("GOATOS_ESCALATION_LEVEL2_AFTER", 4*time.Hour), "level 2 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level3After, "level3-after", durationEnv("GOATOS_ESCALATION_LEVEL3_AFTER", 24*time.Hour), "level 3 SLA threshold after due_at")
	fs.DurationVar(&cfg.Level4After, "level4-after", durationEnv("GOATOS_ESCALATION_LEVEL4_AFTER", 48*time.Hour), "level 4 SLA threshold after due_at")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	now := time.Now().In(biztime.DefaultLocation())
	cfg.DueBefore = now
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
	cfg.CalendarDateFrom = now.Add(-24 * time.Hour)
	if strings.TrimSpace(*calendarDateFromRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*calendarDateFromRaw))
		if err != nil {
			return config{}, errors.New("calendar-date-from must be RFC3339")
		}
		cfg.CalendarDateFrom = parsed.In(biztime.DefaultLocation())
	}
	cfg.CalendarDateTo = now.Add(45 * 24 * time.Hour)
	if strings.TrimSpace(*calendarDateToRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*calendarDateToRaw))
		if err != nil {
			return config{}, errors.New("calendar-date-to must be RFC3339")
		}
		cfg.CalendarDateTo = parsed.In(biztime.DefaultLocation())
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.DosesPerGoat < 1 {
		cfg.DosesPerGoat = 1
	}
	if !cfg.CalendarDateFrom.Before(cfg.CalendarDateTo) {
		return config{}, errors.New("calendar-date-from must be before calendar-date-to")
	}
	if cfg.CalendarLimit <= 0 {
		cfg.CalendarLimit = 1000
	}
	if cfg.ReminderLimit <= 0 {
		cfg.ReminderLimit = 100
	}
	if cfg.EscalationLimit < 1 || cfg.EscalationLimit > 500 {
		return config{}, errors.New("escalation-limit must be between 1 and 500")
	}
	if cfg.Level1After < 0 || cfg.Level2After < cfg.Level1After || cfg.Level3After < cfg.Level2After || cfg.Level4After < cfg.Level3After {
		return config{}, errors.New("SLA thresholds must be non-negative and increasing")
	}
	return cfg, nil
}

type sopTaskCreator struct {
	service *sopapp.Service
	actorID string
}

func (c sopTaskCreator) CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (string, error) {
	resp, err := c.service.CreateTask(ctx, sopports.CreateTaskCommand{
		TenantID: tenantID,
		ActorID:  c.actorID,
		Body: sopdomain.CreateTaskRequest{
			SOPVersionID: &sopVersionID,
			TaskType:     taskType,
			Title:        title,
			ScopeType:    scopeType,
			ScopeID:      scopeID,
			Priority:     "normal",
			Context:      map[string]any{"created_by": "obligation-sweeper", "obligation_batch_id": batchID},
		},
	}, "obligation-sweeper")
	if err != nil {
		return "", err
	}
	return resp.Task.TaskID, nil
}

func (c sopTaskCreator) CreateTasksForBatches(ctx context.Context, tenantID string, batches []obligationapp.BatchTaskCreate) (map[string]string, error) {
	out := make(map[string]string, len(batches))
	for _, batch := range batches {
		taskID, err := c.CreateTaskForBatch(ctx, tenantID, batch.BatchID, batch.SOPVersionID, batch.TaskType, batch.Title, batch.ScopeType, batch.ScopeID)
		if err != nil {
			return nil, err
		}
		out[batch.BatchID] = taskID
	}
	return out, nil
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
