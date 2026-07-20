// Command feed-direction-issue drives the issued-sheet lifecycle from the dispatch clock.
//
// It is scheduler-facing and tenant/park/workflow/feed-day scoped. Feed for day D is produced on
// D-1, so on the business day it runs (as-of, Asia/Kolkata) it acts on feed_day = as-of + 1:
//
//	-action issue   at direction_time  -- generate the day's sheet once and freeze it.
//	-action amend   at correction_time -- recompute, diff, persist an amendment for the changed sheds.
//	-action lock    at transport_time  -- lock the sheet; later changes roll to the next feed day.
//
// It resolves feed_schedule_config (the clock migration 000004 added and nothing read until now),
// so it only issues/amends/locks workflows a park actually runs. It is idempotent and safe to
// re-run: an exact re-issue is a no-op, an unchanged amend records that it ran, a second lock is a
// no-op.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	actionIssue        = "issue"
	actionAmend        = "amend"
	actionLock         = "lock"
	workflowNormal     = "normal"
	workflowExperiment = "experiment"
	workflowBoth       = "both"
	defaultGeneratedBy = "feed-direction-issue"
)

type config struct {
	TenantID    string
	ParkID      string
	Action      string
	Workflow    string
	AsOf        time.Time
	GeneratedBy string
	Timeout     time.Duration
}

func main() {
	if err := run(os.Args[1:], time.Now); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, now func() time.Time) error {
	cfg, err := parseFlags(args, now)
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

	repo := feeddirectionpg.NewRepository(pool, pgCfg.QueryTimeout)
	countsService := countsapp.NewService(countspg.NewRepository(pool, pgCfg.QueryTimeout))
	service := feeddirectionapp.NewService(repo, feeddirectioncounts.NewReader(countsService)).
		WithIssueStore(repo).
		WithScheduleReader(repo).
		WithGeneratedBy(cfg.GeneratedBy).
		WithClock(func() time.Time { return cfg.AsOf })

	parks := []string{cfg.ParkID}
	if cfg.ParkID == "" {
		parks, err = repo.ListScheduledParks(ctx, cfg.TenantID, cfg.AsOf)
		if err != nil {
			return err
		}
		if len(parks) == 0 {
			fmt.Println("feed-direction-issue: no parks with a dispatch clock; nothing to do")
			return nil
		}
	}

	for _, parkID := range parks {
		workflows, err := resolveWorkflows(ctx, repo, cfg, parkID)
		if err != nil {
			return err
		}
		for _, workflow := range workflows {
			if err := dispatch(ctx, service, cfg, parkID, workflow); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveWorkflows narrows the requested workflow set to the ones the park actually runs, read from
// feed_schedule_config. A single explicitly-named workflow that the park does not run is an error;
// 'both' silently issues only the configured ones.
func resolveWorkflows(ctx context.Context, repo *feeddirectionpg.Repository, cfg config, parkID string) ([]string, error) {
	clocks, err := repo.ListScheduleClocks(ctx, cfg.TenantID, parkID, cfg.AsOf)
	if err != nil {
		return nil, err
	}
	configured := map[string]bool{}
	for _, c := range clocks {
		configured[c.Workflow] = true
	}

	requested := []string{cfg.Workflow}
	if cfg.Workflow == workflowBoth {
		requested = []string{workflowNormal, workflowExperiment}
	}
	out := make([]string, 0, len(requested))
	for _, w := range requested {
		if configured[w] {
			out = append(out, w)
			continue
		}
		if cfg.Workflow != workflowBoth {
			return nil, fmt.Errorf("park %s has no %s dispatch clock", parkID, w)
		}
	}
	return out, nil
}

func dispatch(ctx context.Context, service *feeddirectionapp.Service, cfg config, parkID, workflow string) error {
	req := feeddirectionapp.IssueRequest{TenantID: cfg.TenantID, ParkID: parkID, Workflow: workflow, AsOf: cfg.AsOf}
	switch cfg.Action {
	case actionIssue:
		report, err := service.IssueDirection(ctx, req)
		if err != nil {
			return wrap(cfg.Action, parkID, workflow, err)
		}
		printReport(cfg.Action, report)
	case actionAmend:
		report, err := service.AmendDirection(ctx, req)
		if err != nil {
			return wrap(cfg.Action, parkID, workflow, err)
		}
		printReport(cfg.Action, report)
	case actionLock:
		report, err := service.LockDirection(ctx, req)
		if err != nil {
			return wrap(cfg.Action, parkID, workflow, err)
		}
		printReport(cfg.Action, report)
	default:
		return fmt.Errorf("unknown action %q", cfg.Action)
	}
	return nil
}

func wrap(action, parkID, workflow string, err error) error {
	// A worker asked to amend/lock a day that was never issued for that workflow is a benign
	// scheduling gap (e.g. the issue action failed earlier), not a crash-worthy fault. Report and
	// continue rather than aborting the whole run.
	if errors.Is(err, feeddirectionports.ErrIssueNotFound) {
		fmt.Printf("feed-direction-issue %s park=%s workflow=%s skipped: %v\n", action, parkID, workflow, err)
		return nil
	}
	return fmt.Errorf("%s park=%s workflow=%s: %w", action, parkID, workflow, err)
}

func printReport(action string, report feeddirectionapp.LifecycleReport) {
	affected := ""
	if len(report.AffectedShedIDs) > 0 {
		affected = fmt.Sprintf(" affected_sheds=%d", len(report.AffectedShedIDs))
	}
	fmt.Printf("feed-direction-issue %s park=%s workflow=%s feed_day=%s outcome=%s state=%s%s\n",
		action, report.Header.ParkID, report.Workflow, report.FeedDay, report.Outcome, report.Header.State, affected)
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("feed-direction-issue", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.ParkID, "park-id", getenv("GOATOS_FEED_DIRECTION_PARK_ID"), "park/location id; empty means every park with a dispatch clock")
	fs.StringVar(&cfg.Action, "action", actionIssue, "issue | amend | lock")
	fs.StringVar(&cfg.Workflow, "workflow", workflowBoth, "normal | experiment | both")
	fs.StringVar(&cfg.GeneratedBy, "generated-by", getenvDefault("GOATOS_FEED_DIRECTION_GENERATED_BY", defaultGeneratedBy), "generated_by stamp on issued sheets")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_FEED_DIRECTION_TIMEOUT", 120*time.Second), "worker timeout")
	asOfRaw := fs.String("as-of", getenv("GOATOS_FEED_DIRECTION_AS_OF"), "as-of instant as RFC3339 or YYYY-MM-DD; default now (Asia/Kolkata)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ParkID = strings.TrimSpace(cfg.ParkID)
	cfg.Action = strings.TrimSpace(cfg.Action)
	cfg.Workflow = strings.TrimSpace(cfg.Workflow)
	cfg.GeneratedBy = strings.TrimSpace(cfg.GeneratedBy)

	if cfg.TenantID == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.Action != actionIssue && cfg.Action != actionAmend && cfg.Action != actionLock {
		return config{}, errors.New("action must be issue, amend, or lock")
	}
	if cfg.Workflow != workflowNormal && cfg.Workflow != workflowExperiment && cfg.Workflow != workflowBoth {
		return config{}, errors.New("workflow must be normal, experiment, or both")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.GeneratedBy == "" {
		return config{}, errors.New("generated-by is required")
	}

	if now == nil {
		now = time.Now
	}
	cfg.AsOf = now().In(biztime.DefaultLocation())
	if raw := strings.TrimSpace(*asOfRaw); raw != "" {
		parsed, err := parseAsOf(raw)
		if err != nil {
			return config{}, err
		}
		cfg.AsOf = parsed
	}
	return cfg, nil
}

// parseAsOf accepts an instant (RFC3339) or a business date (YYYY-MM-DD, taken as that day's start
// in Asia/Kolkata), always returned in the business calendar.
func parseAsOf(raw string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation()); err == nil {
		return biztime.BusinessDayStart(t), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("as-of must be RFC3339 or YYYY-MM-DD")
	}
	return t.In(biztime.DefaultLocation()), nil
}

func getenv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func getenvDefault(key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
