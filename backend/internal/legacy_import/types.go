package legacy_import

import (
	"context"
	"encoding/json"
)

const (
	DefaultPolicyVersion = "phase1-rfid-db-import-v1"
	DefaultSourceName    = "RFID workbook import"
	DefaultBatchSize     = 500

	StatePending     = "pending"
	StateNeedsReview = "needs_review"
	StateError       = "error"
	StateCreatedGoat = "created_goat"
)

type ImportCommand struct {
	InputPath     string
	TenantID      string
	DryRun        bool
	BatchSize     int
	StartedBy     *string
	SourceName    string
	PolicyVersion string
}

type ImportResult struct {
	ImportRunID   string
	TenantID      string
	PolicyVersion string
	SourceSystem  string
	SourceDataset string
	DryRun        bool
	RowCount      int
	ErrorCount    int
	RowsInserted  int
	StateCounts   map[string]int
}

type ApplyCommand struct {
	TenantID      string
	ImportRunID   string
	DryRun        bool
	BatchSize     int
	ActorID       *string
	PolicyVersion string
}

type ApplyResult struct {
	ImportRunID    string
	TenantID       string
	PolicyVersion  string
	SourceSystem   string
	SourceDataset  string
	DryRun         bool
	PendingScanned int
	AppliedCount   int
	ReviewCount    int
	ReplayCount    int
	SkippedCount   int
	ReviewReasons  map[string]int
	CreatedGoatIDs []string
}

type SourceKeyRecipe struct {
	Strategy        string   `json:"strategy"`
	Fields          []string `json:"fields"`
	ForbiddenFields []string `json:"forbidden_fields"`
}

type HashRecipe struct {
	Strategy      string   `json:"strategy"`
	IncludeFields []string `json:"include_fields"`
	ExcludeFields []string `json:"exclude_fields"`
}

type Policy struct {
	PolicyVersion           string
	SourceSystem            string
	SourceDataset           string
	IdentifierPolicyVersion string
	SourceKeyRecipe         SourceKeyRecipe
	SourceKeyRecipeVersion  string
	HashRecipe              HashRecipe
	HashRecipeVersion       string
	NormalizerVersion       string
	RawSourceKeyRecipe      json.RawMessage
	RawHashRecipe           json.RawMessage
}

type WorkbookRow struct {
	RowNumber int
	Raw       map[string]string
}

type StagedRow struct {
	TenantID             string
	ImportRunID          string
	RowNumber            int
	SourceSystem         string
	SourceDataset        string
	SourceRecordID       *string
	SourceRowKey         string
	SourceKeyRecipeVer   string
	SourceRowVersionHash string
	HashRecipeVersion    string
	RawPayload           json.RawMessage
	NormalizedPayload    json.RawMessage
	ProcessingState      string
	ErrorReason          *string
}

type CreateRunParams struct {
	TenantID       string
	SourceName     string
	SourceSystem   string
	SourceDataset  string
	SourceFileRef  *string
	SourceFileHash string
	PolicyVersion  string
	DryRun         bool
	Status         string
	RowCount       int
	ErrorCount     int
	StartedBy      *string
}

type CompleteRunParams struct {
	TenantID    string
	ImportRunID string
	RowCount    int
	ErrorCount  int
}

type SourceRowChangeParams struct {
	TenantID             string
	SourceSystem         string
	SourceDataset        string
	SourceRowKey         string
	SourceRowVersionHash string
}

type Repository interface {
	LoadApprovedPolicy(ctx context.Context, policyVersion string) (Policy, error)
	CreateImportRun(ctx context.Context, params CreateRunParams) (string, error)
	CompleteImportRun(ctx context.Context, params CompleteRunParams) error
	FailImportRun(ctx context.Context, params CompleteRunParams) error
	HasDifferentSourceRowVersion(ctx context.Context, params SourceRowChangeParams) (bool, error)
	InsertRows(ctx context.Context, rows []StagedRow, batchSize int) (int, error)
	ApplyRFIDRows(ctx context.Context, cmd ApplyCommand) (*ApplyResult, error)
}
