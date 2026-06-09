package legacy_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Importer struct {
	repo Repository
}

func NewImporter(repo Repository) *Importer {
	return &Importer{repo: repo}
}

func (i *Importer) ImportRFIDWorkbook(ctx context.Context, cmd ImportCommand) (*ImportResult, error) {
	if i == nil || i.repo == nil {
		return nil, errors.New("legacy import repository is required")
	}
	if strings.TrimSpace(cmd.InputPath) == "" {
		return nil, errors.New("input workbook path is required")
	}
	if filepath.Ext(cmd.InputPath) != ".xlsx" {
		return nil, errors.New("input workbook must be .xlsx")
	}
	if strings.TrimSpace(cmd.TenantID) == "" {
		return nil, errors.New("tenant_id is required")
	}
	if cmd.PolicyVersion == "" {
		cmd.PolicyVersion = DefaultPolicyVersion
	}
	if cmd.SourceName == "" {
		cmd.SourceName = DefaultSourceName
	}
	if cmd.BatchSize <= 0 {
		cmd.BatchSize = DefaultBatchSize
	}

	policy, err := i.repo.LoadApprovedPolicy(ctx, cmd.PolicyVersion)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cmd.InputPath)
	if err != nil {
		return nil, errors.New("read input workbook failed")
	}
	sourceHash := hashBytes(data)
	workbookRows, err := ParseXLSX(data)
	if err != nil {
		return nil, err
	}
	stagedRows, err := NormalizeWorkbookRows(policy, cmd.TenantID, workbookRows)
	if err != nil {
		return nil, err
	}

	changed := map[string]bool{}
	for _, row := range stagedRows {
		hasChanged, err := i.repo.HasDifferentSourceRowVersion(ctx, SourceRowChangeParams{
			TenantID:             cmd.TenantID,
			SourceSystem:         policy.SourceSystem,
			SourceDataset:        policy.SourceDataset,
			SourceRowKey:         row.SourceRowKey,
			SourceRowVersionHash: row.SourceRowVersionHash,
		})
		if err != nil {
			return nil, err
		}
		if hasChanged {
			changed[row.SourceRowKey] = true
		}
	}
	stagedRows = MarkChangedRowsNeedsReview(stagedRows, changed)

	rowCount := len(stagedRows)
	errorCount := ErrorCount(stagedRows)
	status := "running"
	if cmd.DryRun {
		status = "completed"
	}
	runID, err := i.repo.CreateImportRun(ctx, CreateRunParams{
		TenantID:       cmd.TenantID,
		SourceName:     cmd.SourceName,
		SourceSystem:   policy.SourceSystem,
		SourceDataset:  policy.SourceDataset,
		SourceFileRef:  nil,
		SourceFileHash: sourceHash,
		PolicyVersion:  policy.PolicyVersion,
		DryRun:         cmd.DryRun,
		Status:         status,
		RowCount:       rowCount,
		ErrorCount:     errorCount,
		StartedBy:      cmd.StartedBy,
	})
	if err != nil {
		return nil, err
	}

	result := &ImportResult{
		ImportRunID:   runID,
		TenantID:      cmd.TenantID,
		PolicyVersion: policy.PolicyVersion,
		SourceSystem:  policy.SourceSystem,
		SourceDataset: policy.SourceDataset,
		DryRun:        cmd.DryRun,
		RowCount:      rowCount,
		ErrorCount:    errorCount,
		StateCounts:   StateCounts(stagedRows),
	}
	if cmd.DryRun {
		return result, nil
	}

	for idx := range stagedRows {
		stagedRows[idx].ImportRunID = runID
	}
	inserted, err := i.repo.InsertRows(ctx, stagedRows, cmd.BatchSize)
	if err != nil {
		_ = i.repo.FailImportRun(ctx, CompleteRunParams{
			TenantID:    cmd.TenantID,
			ImportRunID: runID,
			RowCount:    rowCount,
			ErrorCount:  errorCount,
		})
		return nil, fmt.Errorf("stage legacy import rows: %w", err)
	}
	result.RowsInserted = inserted
	if err := i.repo.CompleteImportRun(ctx, CompleteRunParams{
		TenantID:    cmd.TenantID,
		ImportRunID: runID,
		RowCount:    rowCount,
		ErrorCount:  errorCount,
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
