// Command vaccination-projection-worker is the BOUNDED INCREMENTAL projector worker for the
// vaccination-shed read model (P0-B, context/execution/api-projection-performance-handoff-2026-07-13.md).
// One invocation:
//  1. reclaims any expired dirty-scope leases (a crashed/killed prior worker instance),
//  2. claims up to -limit pending, ready dirty scopes with `FOR UPDATE SKIP LOCKED`
//     (backend/internal/vaccinationexecution/adapters/postgres/dirty_scopes.go ClaimDirtyScopes,
//     mirroring the outbox ClaimPending lease pattern),
//  3. calls RebuildShedShard (incremental_shed_projection.go) for exactly each claimed shed --
//     never a tenant-wide rebuild, never a copy of any other shed's row,
//  4. marks each scope done or retries/dead-letters it on failure,
//  5. runs the time-driven pass (EnqueueDueTransitions) so scheduled->due->overdue recomputation
//     keeps happening for sheds with no new writes.
//
// This is a SEPARATE, additive worker: it does not replace RecomputeShedProjection
// (cmd/vaccination-shed-projection-recompute), which remains the explicit bootstrap/repair path
// for a tenant's first build or a full-tenant repair.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type config struct {
	TenantID              string
	Owner                 string
	ClaimLimit            int
	EnqueueDueTransitions bool
	DueTransitionsLimit   int
	Timeout               time.Duration
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

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "vaccination-projection-worker"})
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
	now := time.Now().In(biztime.DefaultLocation())

	reclaimed, err := repo.ReclaimExpiredDirtyScopeLeases(ctx, now)
	if err != nil {
		return fmt.Errorf("reclaim expired dirty scope leases: %w", err)
	}
	if reclaimed > 0 {
		fmt.Printf("reclaimed expired leases=%d\n", reclaimed)
	}

	claimed, err := repo.ClaimDirtyScopes(ctx, cfg.Owner, cfg.ClaimLimit, now)
	if err != nil {
		return fmt.Errorf("claim dirty scopes: %w", err)
	}
	var rebuilt, deferred, failed int
	// This loop is the intended per-scope worker shape, not an accidental N+1: each claimed dirty
	// scope names a DIFFERENT shed, and RebuildShedShard's whole purpose is to recompute exactly
	// that one shed with its own shed-scoped query (incremental_shed_projection.go) -- there is no
	// single batched SQL statement that could "rebuild sheds X and Y at once" without collapsing
	// back into the rejected tenant-wide-copy design the handoff doc forbids. The loop itself is
	// bounded by len(claimed) <= cfg.ClaimLimit (hard-capped at 1000 in ClaimDirtyScopes), the same
	// claimed-batch shape the outbox relay and obligation-sweeper's per-version loop already use for
	// the identical "one durable-queue claim, one operation per claimed item" problem.
	for _, scope := range claimed {
		if cfg.TenantID != "" && scope.TenantID != cfg.TenantID {
			// Defensive: ClaimDirtyScopes is tenant-agnostic (claims across all tenants in one
			// pass); a configured tenant-id filter is a narrowing debug/ops knob, not the normal
			// path. Put it back for another worker/tenant run rather than silently dropping it.
			// scale-guard:ignore: bounded by len(claimed) <= cfg.ClaimLimit (see loop comment above); a defensive re-queue for the rare tenant-filter-mismatch ops-knob path.
			if err := repo.MarkDirtyScopeFailed(ctx, scope.DirtyScopeID, "tenant_filter_mismatch", now); err != nil {
				return fmt.Errorf("re-queue out-of-scope dirty scope %d: %w", scope.DirtyScopeID, err)
			}
			continue
		}
		// scale-guard:ignore: bounded by len(claimed) <= cfg.ClaimLimit (see loop comment above); each claimed scope names a different shed and must be rebuilt individually -- that IS the bounded-incremental-projector design (P0-B), never a tenant-wide batch rebuild.
		result, err := repo.RebuildShedShard(ctx, vaccexecdomain.RebuildShedShardRequest{
			TenantID: scope.TenantID,
			ShedID:   scope.ShedID,
			AsOf:     now,
		})
		if err != nil {
			failed++
			// scale-guard:ignore: bounded by len(claimed) <= cfg.ClaimLimit (see loop comment above); records this one scope's own rebuild failure for its own retry/backoff.
			if markErr := repo.MarkDirtyScopeFailed(ctx, scope.DirtyScopeID, err.Error(), now); markErr != nil {
				return fmt.Errorf("mark dirty scope %d failed: %w (rebuild error: %v)", scope.DirtyScopeID, markErr, err)
			}
			fmt.Printf("rebuild failed tenant=%s shed=%s attempt=%d error=%v\n", scope.TenantID, scope.ShedID, scope.AttemptCount+1, err)
			continue
		}
		if result.Deferred {
			deferred++
			// scale-guard:ignore: bounded by len(claimed) <= cfg.ClaimLimit (see loop comment above); re-queues this one scope until a bootstrap RecomputeShedProjection creates a serving version.
			if markErr := repo.MarkDirtyScopeFailed(ctx, scope.DirtyScopeID, "no_serving_projection_version_yet", now); markErr != nil {
				return fmt.Errorf("mark deferred dirty scope %d: %w", scope.DirtyScopeID, markErr)
			}
			continue
		}
		// scale-guard:ignore: bounded by len(claimed) <= cfg.ClaimLimit (see loop comment above); marks this one successfully rebuilt scope done (deletes its queue row).
		if err := repo.MarkDirtyScopeDone(ctx, scope.DirtyScopeID); err != nil {
			return fmt.Errorf("mark dirty scope %d done: %w", scope.DirtyScopeID, err)
		}
		rebuilt++
	}
	fmt.Printf("claimed=%d rebuilt=%d deferred=%d failed=%d\n", len(claimed), rebuilt, deferred, failed)

	if cfg.EnqueueDueTransitions {
		if strings.TrimSpace(cfg.TenantID) == "" {
			// The dirty-scope queue is tenant-scoped work by design; an empty tenant-id here is a
			// caller error, not a silent no-op across every tenant in one query.
			return errors.New("tenant-id is required when -enqueue-due-transitions is set")
		}
		enqueued, err := repo.EnqueueDueTransitions(ctx, cfg.TenantID, cfg.DueTransitionsLimit)
		if err != nil {
			return fmt.Errorf("enqueue due transitions tenant=%s: %w", cfg.TenantID, err)
		}
		fmt.Printf("time-driven enqueue tenant=%s sheds=%d\n", cfg.TenantID, enqueued)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("vaccination-projection-worker", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", strings.TrimSpace(os.Getenv("GOATOS_TENANT_ID")), "tenant id; when set, out-of-tenant claimed scopes are re-queued rather than processed, and required by -enqueue-due-transitions")
	hostname, _ := os.Hostname()
	fs.StringVar(&cfg.Owner, "owner", firstNonEmpty(os.Getenv("GOATOS_PROJECTION_WORKER_OWNER"), hostname, "vaccination-projection-worker"), "lease owner identity recorded on claimed dirty scopes")
	fs.IntVar(&cfg.ClaimLimit, "limit", intEnv("GOATOS_PROJECTION_WORKER_CLAIM_LIMIT", 200), "max dirty scopes to claim and rebuild in this run")
	fs.BoolVar(&cfg.EnqueueDueTransitions, "enqueue-due-transitions", boolEnv("GOATOS_PROJECTION_WORKER_ENQUEUE_DUE_TRANSITIONS", true), "also run the time-driven pass (EnqueueDueTransitions) so scheduled->due->overdue recomputes without writes")
	fs.IntVar(&cfg.DueTransitionsLimit, "due-transitions-limit", intEnv("GOATOS_PROJECTION_WORKER_DUE_TRANSITIONS_LIMIT", 200), "max sheds to enqueue per time-driven pass")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_PROJECTION_WORKER_TIMEOUT", 90*time.Second), "run timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.ClaimLimit <= 0 {
		return config{}, errors.New("limit must be positive")
	}
	if cfg.DueTransitionsLimit <= 0 {
		return config{}, errors.New("due-transitions-limit must be positive")
	}
	return cfg, nil
}

func validateDatabaseTarget(databaseURL string) error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("vaccination-projection-worker", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("vaccination-projection-worker", env, databaseURL, "local", "dev")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func intEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
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
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
