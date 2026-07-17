// Command vaccination-schedule-projection-recompute rebuilds the month-window Full Schedule read model.
//
// The request path for GET /vaccination/schedule never runs the canonical operations query; it only
// serves materialized windows. Run this command after seed/import and as a periodic/off-request worker
// so cold windows are populated and dirty windows are refreshed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type config struct {
	TenantID    string
	ParkID      string
	Timeout     time.Duration
	DirtyLimit  int
	MonthsBack  int
	MonthsAhead int
	DirtyOnly   bool
	GeneratedBy string
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "vaccination-schedule-projection-recompute"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateDatabaseTarget(pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := vaccexecpg.NewRepository(pool, pgCfg.QueryTimeout)
	dirty, err := repo.RebuildDirtyVaccinationScheduleWindows(ctx, cfg.TenantID, cfg.DirtyLimit, cfg.GeneratedBy)
	if err != nil {
		return fmt.Errorf("rebuild dirty vaccination schedule windows: %w", err)
	}
	fmt.Printf("rebuilt dirty vaccination schedule windows tenant=%s windows=%d rows=%d\n", cfg.TenantID, dirty.Windows, dirty.Rows)

	if cfg.DirtyOnly {
		return nil
	}

	parkIDs := []string{cfg.ParkID}
	if cfg.ParkID == "" {
		parkIDs, err = activeParkIDs(ctx, pool, cfg.TenantID)
		if err != nil {
			return fmt.Errorf("list active parks for vaccination schedule horizon: %w", err)
		}
		parkIDs = append([]string{""}, parkIDs...)
	}
	monthStart := firstOfMonth(time.Now().In(biztime.DefaultLocation())).AddDate(0, -cfg.MonthsBack, 0)
	totalWindows := 0
	totalRows := 0
	for _, configuredParkID := range parkIDs {
		for i := 0; i <= cfg.MonthsBack+cfg.MonthsAhead; i++ {
			q := vaccexecd.ScheduleQuery{TenantID: cfg.TenantID, MonthStart: monthStart.AddDate(0, i, 0)}
			if configuredParkID != "" {
				parkID := configuredParkID
				q.ParkID = &parkID
			}
			// scale-guard:ignore: off-request horizon ensure; bounded to (tenant + active parks) x configured month window and keeps GET /vaccination/schedule read-only.
			state, err := repo.RebuildVaccinationScheduleWindow(ctx, q, cfg.GeneratedBy)
			if err != nil {
				return fmt.Errorf("rebuild vaccination schedule window park=%s month=%s: %w", configuredParkID, q.MonthStart.Format("2006-01"), err)
			}
			totalWindows++
			totalRows += state.RowCount
		}
	}
	fmt.Printf("ensured vaccination schedule horizon tenant=%s park=%s windows=%d rows=%d months_back=%d months_ahead=%d\n",
		cfg.TenantID, cfg.ParkID, totalWindows, totalRows, cfg.MonthsBack, cfg.MonthsAhead)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("vaccination-schedule-projection-recompute", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.StringVar(&cfg.ParkID, "park-id", "", "optional park id for horizon ensure")
	fs.DurationVar(&cfg.Timeout, "timeout", 10*time.Minute, "recompute timeout")
	fs.IntVar(&cfg.DirtyLimit, "dirty-limit", 200, "maximum dirty windows to rebuild before horizon ensure")
	fs.IntVar(&cfg.MonthsBack, "months-back", 1, "number of prior month windows to ensure")
	fs.IntVar(&cfg.MonthsAhead, "months-ahead", 13, "number of future month windows to ensure")
	fs.BoolVar(&cfg.DirtyOnly, "dirty-only", false, "only process dirty windows; do not prebuild horizon")
	fs.StringVar(&cfg.GeneratedBy, "generated-by", "vaccination-schedule-projection-recompute", "projection generator label")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ParkID = strings.TrimSpace(cfg.ParkID)
	cfg.GeneratedBy = strings.TrimSpace(cfg.GeneratedBy)
	if cfg.TenantID == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.ParkID != "" && !uuidutil.IsUUIDString(cfg.ParkID) {
		return config{}, errors.New("park-id must be a UUID")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.DirtyLimit < 0 {
		return config{}, errors.New("dirty-limit must be non-negative")
	}
	if cfg.MonthsBack < 0 || cfg.MonthsAhead < 0 {
		return config{}, errors.New("months-back/months-ahead must be non-negative")
	}
	if cfg.GeneratedBy == "" {
		return config{}, errors.New("generated-by is required")
	}
	return cfg, nil
}

func firstOfMonth(t time.Time) time.Time {
	local := t.In(biztime.DefaultLocation())
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, biztime.DefaultLocation())
}

func activeParkIDs(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
SELECT location_id::text
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
ORDER BY name, location_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func validateDatabaseTarget(databaseURL string) error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-schedule-projection-recompute", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("vaccination-schedule-projection-recompute", env, databaseURL, "local", "dev")
}
