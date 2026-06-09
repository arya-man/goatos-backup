package legacy_import

import (
	"context"
	"errors"
	"strings"
)

type Applier struct {
	repo Repository
}

func NewApplier(repo Repository) *Applier {
	return &Applier{repo: repo}
}

func (a *Applier) ApplyRFIDRows(ctx context.Context, cmd ApplyCommand) (*ApplyResult, error) {
	if a == nil || a.repo == nil {
		return nil, errors.New("legacy import repository is required")
	}
	if strings.TrimSpace(cmd.TenantID) == "" {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(cmd.ImportRunID) == "" {
		return nil, errors.New("import_run_id is required")
	}
	if cmd.BatchSize <= 0 {
		cmd.BatchSize = DefaultBatchSize
	}
	return a.repo.ApplyRFIDRows(ctx, cmd)
}
