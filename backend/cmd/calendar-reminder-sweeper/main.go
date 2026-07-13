package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/taskqueue"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("calendar-reminder-sweeper", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	limit := fs.Int("limit", envInt("GOATOS_REMINDER_LIMIT", 200), "max cadence candidates scanned per tick")
	timeout := fs.Duration("timeout", 30*time.Second, "sweeper timeout")

	// Reminder cadence ladder config (vaccination-notification-rules.md §3), mirroring how
	// cmd/calendar-escalation-sweeper takes Level1..4 offsets. Every offset/slot is a rule
	// parameter; the defaults are the documented "T-7 advance notice, T-6..T-1 daily 2x, T-0 2x"
	// cadence.
	advanceOffsetDays := fs.Int("advance-offset-days", envInt("GOATOS_REMINDER_ADVANCE_OFFSET_DAYS", 7), "days before due date the one-time advance_notice fires")
	advanceSlots := fs.String("advance-slots", envString("GOATOS_REMINDER_ADVANCE_SLOTS", "09:00"), "comma-separated IST HH:MM slot(s) for the advance_notice rung")
	dailyFromOffsetDays := fs.Int("daily-from-offset-days", envInt("GOATOS_REMINDER_DAILY_FROM_OFFSET_DAYS", 6), "days-before-due the daily reminder rung STARTS (inclusive)")
	dailyToOffsetDays := fs.Int("daily-to-offset-days", envInt("GOATOS_REMINDER_DAILY_TO_OFFSET_DAYS", 1), "days-before-due the daily reminder rung ENDS (inclusive)")
	dailySlots := fs.String("daily-slots", envString("GOATOS_REMINDER_DAILY_SLOTS", "08:00,17:00"), "comma-separated IST HH:MM slot(s)/day for the daily reminder rung")
	dueTodaySlots := fs.String("due-today-slots", envString("GOATOS_REMINDER_DUE_TODAY_SLOTS", "08:00,12:00"), "comma-separated IST HH:MM slot(s) for the due_today rung")
	quietHoursStart := fs.String("quiet-hours-start", envString("GOATOS_REMINDER_QUIET_HOURS_START", ""), "IST HH:MM quiet-hours window start (default 21:00)")
	quietHoursEnd := fs.String("quiet-hours-end", envString("GOATOS_REMINDER_QUIET_HOURS_END", ""), "IST HH:MM quiet-hours window end (default 07:00)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	ladder, err := buildReminderLadder(*advanceOffsetDays, *advanceSlots, *dailyFromOffsetDays, *dailyToOffsetDays, *dailySlots, *dueTodaySlots)
	if err != nil {
		return fmt.Errorf("calendar-reminder-sweeper: invalid ladder config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "calendar-reminder-sweeper"})
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

	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, pgCfg.QueryTimeout))
	workforceRepo := workforcepg.NewRepository(pool, pgCfg.QueryTimeout)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)

	count, err := sweepReminderCadence(ctx, calendarService, rosterService, reminderCadenceTick{
		TenantID:           *tenantID,
		Now:                time.Now().In(biztime.DefaultLocation()),
		Ladder:             ladder,
		QuietHoursStartIST: *quietHoursStart,
		QuietHoursEndIST:   *quietHoursEnd,
		Limit:              *limit,
	})
	if err != nil {
		return err
	}
	if count > 0 {
		if err := enqueueNotificationDispatcher(ctx, *tenantID, "calendar-reminder-sweeper"); err != nil {
			return err
		}
	}
	fmt.Printf("calendar reminder cadence sweep queued=%d tenant=%s\n", count, *tenantID)
	return nil
}

func buildReminderLadder(advanceOffsetDays int, advanceSlots string, dailyFromOffsetDays, dailyToOffsetDays int, dailySlots, dueTodaySlots string) ([]calendardomain.ReminderLadderStep, error) {
	advance, err := splitSlots(advanceSlots)
	if err != nil {
		return nil, fmt.Errorf("advance-slots: %w", err)
	}
	daily, err := splitSlots(dailySlots)
	if err != nil {
		return nil, fmt.Errorf("daily-slots: %w", err)
	}
	dueToday, err := splitSlots(dueTodaySlots)
	if err != nil {
		return nil, fmt.Errorf("due-today-slots: %w", err)
	}
	if advanceOffsetDays < 0 {
		return nil, errors.New("advance-offset-days must be >= 0")
	}
	if dailyFromOffsetDays < dailyToOffsetDays {
		return nil, errors.New("daily-from-offset-days must be >= daily-to-offset-days")
	}
	if dailyToOffsetDays < 0 {
		return nil, errors.New("daily-to-offset-days must be >= 0")
	}
	return []calendardomain.ReminderLadderStep{
		{
			OffsetDaysFrom: -advanceOffsetDays, OffsetDaysTo: -advanceOffsetDays,
			Slots: advance, Type: "advance_notice", Priority: "normal",
		},
		{
			OffsetDaysFrom: -dailyFromOffsetDays, OffsetDaysTo: -dailyToOffsetDays,
			Slots: daily, Type: "reminder", Priority: "normal",
		},
		{
			OffsetDaysFrom: 0, OffsetDaysTo: 0,
			Slots: dueToday, Type: "due_today", Priority: "high",
		},
	}, nil
}

func splitSlots(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		slot := strings.TrimSpace(part)
		if slot == "" {
			continue
		}
		out = append(out, slot)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one HH:MM slot is required, got %q", raw)
	}
	return out, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envString(key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return fallback
	}
	return n
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
