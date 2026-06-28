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
	service := calendarapp.NewService(calendarpg.NewRepository(pool, cfg.Timeout))
	count, err := service.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: cfg.TenantID,
		DateFrom: cfg.DateFrom,
		DateTo:   cfg.DateTo,
		Limit:    cfg.Limit,
	})
	if err != nil {
		return err
	}
	pruned := 0
	if cfg.PruneClosed {
		pruned, err = service.PruneClosedVaccinationProjection(ctx, cfg.TenantID, time.Now().UTC().Add(-cfg.ClosedRetention), cfg.PruneLimit)
		if err != nil {
			return err
		}
	}
	fmt.Printf("calendar vaccination projection refreshed=%d pruned_closed=%d tenant=%s from=%s to=%s\n",
		count, pruned, cfg.TenantID, cfg.DateFrom.Format(time.RFC3339), cfg.DateTo.Format(time.RFC3339))
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("calendar-vaccination-projector", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	dateFrom := fs.String("date-from", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_FROM"), "RFC3339 lower bound; default now minus 24h")
	dateTo := fs.String("date-to", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_TO"), "RFC3339 upper bound; default now plus 45d")
	fs.IntVar(&cfg.Limit, "limit", intEnv("GOATOS_CALENDAR_PROJECTOR_LIMIT", 1000), "max projection rows to upsert")
	fs.BoolVar(&cfg.PruneClosed, "prune-closed", boolEnv("GOATOS_CALENDAR_PROJECTOR_PRUNE_CLOSED", true), "prune old closed vaccination projection rows after refresh")
	fs.IntVar(&cfg.PruneLimit, "prune-limit", intEnv("GOATOS_CALENDAR_PROJECTOR_PRUNE_LIMIT", 1000), "max old closed projection rows to prune")
	fs.DurationVar(&cfg.ClosedRetention, "closed-retention", durationEnv("GOATOS_CALENDAR_PROJECTOR_CLOSED_RETENTION", 90*24*time.Hour), "closed projection row retention window")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_CALENDAR_PROJECTOR_TIMEOUT", 60*time.Second), "projector timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	now := time.Now().UTC()
	cfg.DateFrom = now.Add(-24 * time.Hour)
	cfg.DateTo = now.Add(45 * 24 * time.Hour)
	var err error
	if strings.TrimSpace(*dateFrom) != "" {
		cfg.DateFrom, err = time.Parse(time.RFC3339, strings.TrimSpace(*dateFrom))
		if err != nil {
			return config{}, errors.New("date-from must be RFC3339")
		}
		cfg.DateFrom = cfg.DateFrom.UTC()
	}
	if strings.TrimSpace(*dateTo) != "" {
		cfg.DateTo, err = time.Parse(time.RFC3339, strings.TrimSpace(*dateTo))
		if err != nil {
			return config{}, errors.New("date-to must be RFC3339")
		}
		cfg.DateTo = cfg.DateTo.UTC()
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
