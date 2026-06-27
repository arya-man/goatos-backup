package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
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
	if cfg.SOPVersionID != "" && cfg.ActorID != "" {
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
			result, err := sweeper.SweepVersion(ctx, cfg.TenantID, versionID, obligationapp.SweepConfig{
				SOPVersionID:  cfg.SOPVersionID,
				VaccineItemID: cfg.VaccineItemID,
				DosesPerGoat:  int32(cfg.DosesPerGoat),
			}, cfg.DueBefore)
			if err != nil {
				return fmt.Errorf("sweep version %s: %w", versionID, err)
			}
			fmt.Printf("swept version=%s batches=%d obligations=%d\n", versionID, result.Batches, result.Obligations)
		}
	}
	if cfg.MarkMissed {
		missed, err := sweeper.MarkMissed(ctx, cfg.TenantID, cfg.MissedBefore)
		if err != nil {
			return fmt.Errorf("mark missed obligations: %w", err)
		}
		fmt.Printf("marked missed obligations=%d before=%s\n", missed, cfg.MissedBefore.Format(time.RFC3339))
	}
	return nil
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
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	now := time.Now().UTC()
	cfg.DueBefore = now
	if strings.TrimSpace(*dueBeforeRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*dueBeforeRaw))
		if err != nil {
			return config{}, errors.New("due-before must be RFC3339")
		}
		cfg.DueBefore = parsed.UTC()
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
		cfg.MissedBefore = parsed.UTC()
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.DosesPerGoat < 1 {
		cfg.DosesPerGoat = 1
	}
	return cfg, nil
}

type sopTaskCreator struct {
	service *sopapp.Service
	actorID string
}

func (c sopTaskCreator) CreateTaskForBatch(ctx context.Context, tenantID, sopVersionID, taskType, title, scopeType, scopeID string) (string, error) {
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
			Context:      map[string]any{"created_by": "obligation-sweeper"},
		},
	}, "obligation-sweeper")
	if err != nil {
		return "", err
	}
	return resp.Task.TaskID, nil
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
