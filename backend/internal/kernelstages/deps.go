// Package kernelstages provides thin StageRunner wrappers that reuse the
// existing module services and constructors so the kernel worker can run the
// work previously owned by independently scheduled one-shot Cloud Run jobs.
//
// Each stage is a faithful, reusable wrapper around the same module service the
// matching backend/cmd/* one-shot command invokes. The stages do NOT reimplement
// module SQL; they build the module service via its existing constructor and call
// the same method. The supervisor (backend/internal/platform/worker) orchestrates
// cadence, advisory locking, panic isolation, and per-stage timeouts. The one-shot
// commands are retained for manual repair; this package is the scheduled runtime.
package kernelstages

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// Deps carries the shared process-level dependencies every stage needs. It is
// built once in the kernel worker's main and injected into each stage
// constructor so the stages stay thin and reuse the same pool/logger.
type Deps struct {
	Pool   *pgxpool.Pool
	PgCfg  platformpg.Config
	Logger *slog.Logger
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := getenv(key); value != "" {
			return value
		}
	}
	return ""
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
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envTruthy(key string) bool {
	switch strings.ToLower(getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
