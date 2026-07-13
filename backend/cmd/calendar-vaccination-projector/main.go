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
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type config struct {
	TenantID        string
	DateFrom        time.Time
	DateTo          time.Time
	Limit           int
	PruneClosed     bool
	PruneLimit      int
	ClosedRetention time.Duration
	Timeout         time.Duration
	ProjectUpcoming bool
	ProjectHistory  bool
	HistoryDateFrom time.Time
	HistoryDateTo   time.Time
}

var nowInBusinessLocation = func() time.Time {
	return time.Now().In(biztime.DefaultLocation())
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "calendar-vaccination-projector"})
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
	repo := calendarpg.NewRepository(pool, cfg.Timeout)
	service := calendarapp.NewService(repo)

	count := 0
	if cfg.ProjectUpcoming {
		var err error
		count, err = service.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
			TenantID: cfg.TenantID,
			DateFrom: cfg.DateFrom,
			DateTo:   cfg.DateTo,
			Limit:    cfg.Limit,
		})
		if err != nil {
			return err
		}
	}

	pruned := 0
	if cfg.PruneClosed {
		var err error
		pruned, err = service.PruneClosedVaccinationProjection(ctx, cfg.TenantID, time.Now().In(biztime.DefaultLocation()).Add(-cfg.ClosedRetention), cfg.PruneLimit)
		if err != nil {
			return err
		}
	}
	fmt.Printf("calendar vaccination projection refreshed=%d pruned_closed=%d tenant=%s from=%s to=%s\n",
		count, pruned, cfg.TenantID, cfg.DateFrom.Format(time.RFC3339), cfg.DateTo.Format(time.RFC3339))

	if cfg.ProjectHistory {
		// RecomputeVaccinationHistoryProjection is called directly on the concrete repository (not
		// through calendarapp.Service/ports.Repository), mirroring how
		// vaccination-shed-projection-recompute calls vaccinationexecution's RecomputeShedProjection
		// directly. Bootstrap/repair remains a full recompute of the requested window; zero-value
		// dates let the projector derive its own now-400d..now+1d default window.
		historyRows, historyErr := repo.RecomputeVaccinationHistoryProjection(ctx, ports.RefreshVaccinationHistoryProjection{
			TenantID: cfg.TenantID,
			DateFrom: cfg.HistoryDateFrom,
			DateTo:   cfg.HistoryDateTo,
			Limit:    cfg.Limit,
		})
		if historyErr != nil {
			return historyErr
		}
		fmt.Printf("calendar history projection refreshed_rows=%d tenant=%s\n", historyRows, cfg.TenantID)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("calendar-vaccination-projector", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	dateFrom := fs.String("date-from", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_FROM"), "RFC3339 lower bound; default covers current UI week/month window")
	dateTo := fs.String("date-to", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_TO"), "RFC3339 exclusive upper bound; default covers current UI month plus max forward query")
	fs.IntVar(&cfg.Limit, "limit", intEnv("GOATOS_CALENDAR_PROJECTOR_LIMIT", 1000), "max projection rows to upsert")
	fs.BoolVar(&cfg.PruneClosed, "prune-closed", boolEnv("GOATOS_CALENDAR_PROJECTOR_PRUNE_CLOSED", true), "prune old closed vaccination projection rows after refresh")
	fs.IntVar(&cfg.PruneLimit, "prune-limit", intEnv("GOATOS_CALENDAR_PROJECTOR_PRUNE_LIMIT", 1000), "max old closed projection rows to prune")
	fs.DurationVar(&cfg.ClosedRetention, "closed-retention", durationEnv("GOATOS_CALENDAR_PROJECTOR_CLOSED_RETENTION", 90*24*time.Hour), "closed projection row retention window")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_CALENDAR_PROJECTOR_TIMEOUT", 60*time.Second), "projector timeout")
	fs.BoolVar(&cfg.ProjectUpcoming, "project-calendar-upcoming", boolEnv("GOATOS_CALENDAR_PROJECTOR_PROJECT_UPCOMING", true), "refresh the upcoming vaccination projection (calendar_vaccination_projection_rows); TEMPORARY MITIGATION: set to false in hourly calendar_history_projector job to avoid redundant concurrent rebuilds")
	fs.BoolVar(&cfg.ProjectHistory, "project-calendar-history", boolEnv("GOATOS_CALENDAR_PROJECTOR_PROJECT_HISTORY", false), "recompute the completed-history + date-marker projection (calendar_history_projection_rows/calendar_history_date_markers); TEMPORARY MITIGATION: lower-frequency full history replay, not incremental maintenance")
	historyDateFrom := fs.String("history-date-from", getenv("GOATOS_CALENDAR_HISTORY_PROJECTOR_DATE_FROM"), "RFC3339 lower bound for the history projection; default now minus 400d")
	historyDateTo := fs.String("history-date-to", getenv("GOATOS_CALENDAR_HISTORY_PROJECTOR_DATE_TO"), "RFC3339 exclusive upper bound for the history projection; default now plus 1d")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	now := nowInBusinessLocation()
	cfg.DateFrom, cfg.DateTo = defaultUpcomingProjectionWindow(now)
	var err error
	if strings.TrimSpace(*dateFrom) != "" {
		cfg.DateFrom, err = time.Parse(time.RFC3339, strings.TrimSpace(*dateFrom))
		if err != nil {
			return config{}, errors.New("date-from must be RFC3339")
		}
		cfg.DateFrom = cfg.DateFrom.In(biztime.DefaultLocation())
	}
	if strings.TrimSpace(*dateTo) != "" {
		cfg.DateTo, err = time.Parse(time.RFC3339, strings.TrimSpace(*dateTo))
		if err != nil {
			return config{}, errors.New("date-to must be RFC3339")
		}
		cfg.DateTo = cfg.DateTo.In(biztime.DefaultLocation())
	}
	if strings.TrimSpace(*historyDateFrom) != "" {
		cfg.HistoryDateFrom, err = time.Parse(time.RFC3339, strings.TrimSpace(*historyDateFrom))
		if err != nil {
			return config{}, errors.New("history-date-from must be RFC3339")
		}
		cfg.HistoryDateFrom = cfg.HistoryDateFrom.In(biztime.DefaultLocation())
	}
	if strings.TrimSpace(*historyDateTo) != "" {
		cfg.HistoryDateTo, err = time.Parse(time.RFC3339, strings.TrimSpace(*historyDateTo))
		if err != nil {
			return config{}, errors.New("history-date-to must be RFC3339")
		}
		cfg.HistoryDateTo = cfg.HistoryDateTo.In(biztime.DefaultLocation())
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 1000
	}
	if cfg.PruneLimit <= 0 {
		cfg.PruneLimit = 1000
	}
	if cfg.ClosedRetention <= 0 {
		return config{}, errors.New("closed-retention must be positive")
	}
	return cfg, nil
}

func defaultUpcomingProjectionWindow(now time.Time) (time.Time, time.Time) {
	dayStart := biztime.BusinessDayStart(now.In(biztime.DefaultLocation()))
	weekStart := dayStart.AddDate(0, 0, -mondayOffset(dayStart))
	monthStart := time.Date(dayStart.Year(), dayStart.Month(), 1, 0, 0, 0, 0, dayStart.Location())
	previousMonthStart := monthStart.AddDate(0, -1, 0)
	dateFrom := earlierTime(weekStart, previousMonthStart)

	// Calendar list reads allow an inclusive 45-day range; the repository
	// coverage gate checks an exclusive upper bound. The month picker supports
	// one adjacent month in either direction, so cover previous/current/next
	// month plus the forward list horizon.
	dateTo := dayStart.AddDate(0, 0, 47)
	nextMonthEndExclusive := monthStart.AddDate(0, 2, 0)
	if nextMonthEndExclusive.After(dateTo) {
		dateTo = nextMonthEndExclusive
	}
	return dateFrom, dateTo
}

func mondayOffset(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

func earlierTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func intEnv(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	switch strings.ToLower(getenv(key)) {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
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
