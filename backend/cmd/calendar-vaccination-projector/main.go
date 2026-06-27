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
	TenantID string
	DateFrom time.Time
	DateTo   time.Time
	Limit    int
	Timeout  time.Duration
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
	fmt.Printf("calendar vaccination projection refreshed=%d tenant=%s from=%s to=%s\n",
		count, cfg.TenantID, cfg.DateFrom.Format(time.RFC3339), cfg.DateTo.Format(time.RFC3339))
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("calendar-vaccination-projector", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	dateFrom := fs.String("date-from", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_FROM"), "RFC3339 lower bound; default now minus 24h")
	dateTo := fs.String("date-to", getenv("GOATOS_CALENDAR_PROJECTOR_DATE_TO"), "RFC3339 upper bound; default now plus 45d")
	fs.IntVar(&cfg.Limit, "limit", intEnv("GOATOS_CALENDAR_PROJECTOR_LIMIT", 1000), "max projection rows to upsert")
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
