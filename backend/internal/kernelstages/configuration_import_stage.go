package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	configurationpg "github.com/vgoats/goatos/backend/internal/configuration/adapters/postgres"
	configurationapp "github.com/vgoats/goatos/backend/internal/configuration/app"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
)

// ConfigurationImportStage finishes bulk sheet uploads (Configuration -> Items and settings,
// maintainer instruction 2026-09-18) whose in-process run did not complete: the API kicks a
// bounded goroutine per upload and per apply, and a job whose claim lapsed -- the API restarted
// mid-file, or its kick found the semaphore full -- is picked up here and run to the end of its
// phase, resuming after the last row it finished. Same claim, same code path, so a job is never
// worked twice at once. Registered on the OPERATIONAL 5-minute lane; a sweep is at most
// GOATOS_CONFIGURATION_IMPORT_SWEEP_JOBS jobs (default 5) per tick.
type ConfigurationImportStage struct {
	importer *configurationapp.Importer
	tenantID string
	limit    int
	logger   *slog.Logger
}

// NewConfigurationImportStage builds the stage. Animals sheets need identity's bulk pipeline,
// which the worker constructs here from the same pool.
func NewConfigurationImportStage(deps Deps, tenantID string, logger *slog.Logger) *ConfigurationImportStage {
	limit := intEnv("GOATOS_CONFIGURATION_IMPORT_SWEEP_JOBS", 5)
	if limit < 1 || limit > 50 {
		limit = 5
	}
	repo := configurationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	service := configurationapp.NewService(repo)
	identityService := identityapp.NewService(identitypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)).
		WithBulkPreviewSigningKey(getenv("GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY"))
	return &ConfigurationImportStage{
		importer: configurationapp.NewImporter(service, repo, identityService, "kernel-worker", logger),
		tenantID: strings.TrimSpace(tenantID),
		limit:    limit,
		logger:   logger,
	}
}

// Name implements worker.StageRunner.
func (s *ConfigurationImportStage) Name() string { return "configuration-import" }

// Run performs one bounded sweep.
func (s *ConfigurationImportStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("configuration import: tenant id is required")
	}
	done, err := s.importer.ProcessDue(ctx, s.tenantID, s.limit)
	if err != nil {
		return err
	}
	if done > 0 && s.logger != nil {
		s.logger.Info("configuration import sweep", "jobs", done)
	}
	return nil
}
