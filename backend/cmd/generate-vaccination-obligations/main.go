// Command generate-vaccination-obligations runs SM-1 obligation generation for published
// vaccination protocol versions against the existing in-care cohort. The :8080 API only generates
// obligations event-driven (on goat.created); after publishing a new version, the existing cohort
// has no obligations until generation is run. The obligation-sweeper only BATCHES existing
// obligations, so it reports 0 when none have been generated yet. This CLI invokes the real
// production GenerationService.GenerateForVersion (same code path as the goat.created handler),
// so obligation_instances come from the genuine generation logic, not hand-written rows.
//
// Usage:
//
//	DATABASE_URL=... go run ./cmd/generate-vaccination-obligations \
//	  -tenant-id <tenant> [-as-of RFC3339]
//
// The default path resolves the effective protocol per goat, including park/scope precedence. When
// -as-of is omitted, generation uses the current India business-day bucket so replaying a
// calendar-trigger backfill on the same day is idempotent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type config struct {
	TenantID            string
	VersionID           string
	UnsafeVersionRun    bool
	AsOf                time.Time
	Timeout             time.Duration
	RecoveryRepairLimit int
	RecoveryRepairAge   time.Duration
}

var indiaLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.FixedZone("Asia/Kolkata", 5*60*60+30*60)
	}
	return loc
}()

var errRecoveryRepairPartialFailures = errors.New("vaccination recovery repair completed with failed goats")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args, time.Now)
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
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	gen := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)

	if cfg.VersionID == "" {
		repair, repairCandidates, missedBatchRepaired, repairErr := runRecoveryRepair(ctx, obligationRepo, vaccinationRepo, gen, cfg.TenantID, cfg.AsOf, cfg.RecoveryRepairAge, cfg.RecoveryRepairLimit)
		if repairErr != nil && !errors.Is(repairErr, errRecoveryRepairPartialFailures) {
			return fmt.Errorf("vaccination recovery repair: %w", repairErr)
		}
		if cfg.RecoveryRepairLimit > 0 {
			fmt.Printf("recovery-repair candidates=%d missed_batch_repaired=%d generated=%d deferred=%d reopened=%d failed_goats=%d skipped_no_due_date=%d suppressed_trusted=%d\n",
				repairCandidates, missedBatchRepaired, repair.Generated, repair.Deferred, repair.Reopened, repair.FailedGoats, repair.SkippedNoDueDate, repair.SuppressedByTrustedHistory)
		}
		res, err := gen.GenerateEffectiveForAllGoats(ctx, cfg.TenantID, cfg.AsOf)
		if err != nil {
			return fmt.Errorf("generate effective cohort: %w", err)
		}
		stuck, err := vaccinationRepo.CountRecoverableDeferredVaccinationObligations(ctx, cfg.TenantID, cfg.AsOf.Add(-cfg.RecoveryRepairAge))
		if err != nil {
			return fmt.Errorf("count recoverable deferred vaccination obligations: %w", err)
		}
		fmt.Printf("generated effective-cohort generated=%d deferred=%d reopened=%d failed_goats=%d skipped_no_due_date=%d suppressed_trusted=%d stuck_recoverable_deferred=%d\n",
			res.Generated, res.Deferred, res.Reopened, res.FailedGoats, res.SkippedNoDueDate, res.SuppressedByTrustedHistory, stuck)
		if repairErr != nil {
			return fmt.Errorf("vaccination recovery repair: %w", repairErr)
		}
		return nil
	}
	if !cfg.UnsafeVersionRun {
		return errors.New("version-id bypasses effective per-goat protocol resolution; omit -version-id, or pass -unsafe-version-id-bypass-effective-resolution for an intentional repair run")
	}

	versionIDs := []string{cfg.VersionID}
	if len(versionIDs) == 0 {
		fmt.Println("no published vaccination protocol versions to generate")
		return nil
	}

	for _, versionID := range versionIDs {
		run, res, err := gen.GenerateForVersionWithRun(ctx, cfg.TenantID, versionID, cfg.AsOf, "cli", versionID+":"+cfg.AsOf.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("generate version %s: %w", versionID, err)
		}
		fmt.Printf("generated run=%s version=%s generated=%d deferred=%d reopened=%d failed_goats=%d skipped_no_due_date=%d suppressed_trusted=%d\n",
			run.RunID, versionID, res.Generated, res.Deferred, res.Reopened, res.FailedGoats, res.SkippedNoDueDate, res.SuppressedByTrustedHistory)
	}
	return nil
}

func runRecoveryRepair(ctx context.Context, obligationRepo *obligationpg.Repository, repo *vaccinationpg.Repository, gen *vaccinationapp.GenerationService, tenantID string, asOf time.Time, age time.Duration, limit int) (vaccinationdomain.GenerateResult, int, int, error) {
	var out vaccinationdomain.GenerateResult
	if limit <= 0 {
		return out, 0, 0, nil
	}
	if age < 0 {
		age = 0
	}
	olderThan := asOf.Add(-age)
	missedBatchRepaired, err := obligationRepo.RepairStaleMissedVaccinationBatchLinks(ctx, tenantID, olderThan, int32(limit))
	if err != nil {
		return out, 0, 0, err
	}
	goatIDs, err := repo.ListRecoverableDeferredVaccinationGoatIDs(ctx, tenantID, olderThan, int32(limit))
	if err != nil {
		return out, 0, missedBatchRepaired, err
	}
	for _, goatID := range goatIDs {
		res, err := gen.GenerateRecoveryRepairForGoat(ctx, tenantID, goatID, asOf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return out, len(goatIDs), missedBatchRepaired, err
			}
			out.FailedGoats++
			continue
		}
		mergeGenerateResult(&out, res)
	}
	if out.FailedGoats > 0 {
		return out, len(goatIDs), missedBatchRepaired, errRecoveryRepairPartialFailures
	}
	return out, len(goatIDs), missedBatchRepaired, nil
}

func mergeGenerateResult(dst *vaccinationdomain.GenerateResult, src vaccinationdomain.GenerateResult) {
	dst.Generated += src.Generated
	dst.Deferred += src.Deferred
	dst.Reopened += src.Reopened
	dst.FailedGoats += src.FailedGoats
	dst.SkippedNoDueDate += src.SkippedNoDueDate
	dst.SuppressedByTrustedHistory += src.SuppressedByTrustedHistory
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("generate-vaccination-obligations", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id")
	fs.StringVar(&cfg.VersionID, "version-id", "", "unsafe repair-only protocol version id; empty uses effective per-goat protocol resolution")
	fs.BoolVar(&cfg.UnsafeVersionRun, "unsafe-version-id-bypass-effective-resolution", false, "allow version-id to bypass effective per-goat protocol resolution for a targeted repair run")
	fs.DurationVar(&cfg.Timeout, "timeout", 120*time.Second, "generation timeout")
	fs.IntVar(&cfg.RecoveryRepairLimit, "recovery-repair-limit", 1000, "max old deferred/missed vaccination goats to repair before the full effective-cohort scan; 0 disables")
	fs.DurationVar(&cfg.RecoveryRepairAge, "recovery-repair-age", 7*24*time.Hour, "minimum age of deferred/missed vaccination obligations considered stuck/recoverable")
	asOfRaw := fs.String("as-of", "", "RFC3339 as-of instant; default current India business-day bucket")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if now == nil {
		now = time.Now
	}
	cfg.AsOf = startOfIndiaBusinessDay(now())
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.In(indiaLocation)
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.RecoveryRepairLimit < 0 {
		return config{}, errors.New("recovery-repair-limit must be non-negative")
	}
	if cfg.RecoveryRepairAge < 0 {
		return config{}, errors.New("recovery-repair-age must be non-negative")
	}
	return cfg, nil
}

func startOfIndiaBusinessDay(t time.Time) time.Time {
	inIST := t.In(indiaLocation)
	y, m, d := inIST.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, indiaLocation)
}
