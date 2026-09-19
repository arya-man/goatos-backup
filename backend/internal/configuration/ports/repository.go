// Package ports is the seam between the configuration service and its stores.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
)

var (
	// ErrNotFound is a row the register does not carry.
	ErrNotFound = errors.New("configuration: row not found")
	// ErrVersionConflict is a write fenced on a row_version the row has moved past.
	ErrVersionConflict = errors.New("configuration: row changed since it was read")
	// ErrDuplicate is a name or code the register already carries.
	ErrDuplicate = errors.New("configuration: duplicate")
	// ErrInUse is an archive or delete refused because other rows still name this one.
	ErrInUse = errors.New("configuration: row is in use")
	// ErrUnknownRef is a ref field naming a row that does not exist or is archived.
	ErrUnknownRef = errors.New("configuration: unknown reference")
	// ErrIdempotencyConflict is a replay under the same key with a different payload.
	ErrIdempotencyConflict = errors.New("configuration: idempotency key reused with a different request")
)

// RefError names the field whose reference failed, so the drawer can mark it.
type RefError struct {
	Field string
	Label string
}

func (e *RefError) Error() string {
	return "configuration: " + e.Field + " names an unknown " + e.Label
}
func (e *RefError) Unwrap() error { return ErrUnknownRef }

// DuplicateError names the field the duplicate was found on.
type DuplicateError struct {
	Field   string
	Message string
}

func (e *DuplicateError) Error() string { return "configuration: duplicate " + e.Field }
func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// InUseError carries the usage that blocked an archive or delete.
type InUseError struct {
	Usage domain.Usage
}

func (e *InUseError) Error() string { return "configuration: in use: " + e.Usage.Sentence() }
func (e *InUseError) Unwrap() error { return ErrInUse }

// WriteParams is who is writing, under which key.
type WriteParams struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
}

// ListParams scopes a register read. Filters are column key -> value (ref ids or enum values).
type ListParams struct {
	Status  string // active (default) | archived | all
	Query   string
	Filters map[string]string
	// FilterJSON is a store-composed containment filter merged with Filters (an items
	// category filter becomes {"category_ids": [id]} so the whole subtree matches).
	FilterJSON map[string]any
	Cursor     string
	Limit      int
}

// Page is one keyset page of rows.
type Page struct {
	Rows       []domain.Row
	NextCursor string
	// Total is the whole-filter count, never the page's.
	Total int
}

// RefOption is one choice a ref column offers: the target row and, where the target itself
// belongs to a parent (a pen to a park), that parent's id so the drawer can narrow.
type RefOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	ParentID string `json:"parent_id,omitempty"`
	// Kind carries the item kind for a category option, so the item drawer shows the right fields.
	Kind string `json:"kind,omitempty"`
}

// Repository is what the service needs from a store.
type Repository interface {
	// ReferenceLists is the tenant's own vocabularies, in sort order, active and archived.
	ReferenceLists(ctx context.Context, tenantID string) ([]domain.ReferenceList, error)
	Counts(ctx context.Context, tenantID string) (map[string]int, error)
	List(ctx context.Context, tenantID, register string, p ListParams) (Page, error)
	Get(ctx context.Context, tenantID, register, id string) (domain.Row, error)
	// CategoryKind is the effective item kind of a category (its root's), or ErrNotFound. It is
	// the cheap read an item write needs: Get(categories) composes the tree with its per-list
	// item COUNTS, which is a full count of that list's items on every item written -- fine for
	// one drawer save, quadratic for a lakh-row sheet.
	CategoryKind(ctx context.Context, tenantID, id string) (string, error)
	Options(ctx context.Context, tenantID, register string) ([]RefOption, error)
	Usage(ctx context.Context, tenantID, register, id string) (domain.Usage, error)
	Create(ctx context.Context, w WriteParams, register string, fields map[string]any) (domain.Row, error)
	Update(ctx context.Context, w WriteParams, register, id string, fields map[string]any, rowVersion int) (domain.Row, error)
	SetStatus(ctx context.Context, w WriteParams, register, id, status string, rowVersion int) (domain.Row, error)
	Delete(ctx context.Context, w WriteParams, register, id string, rowVersion int) error
}

// ImportRowUpdate is what validation or apply decided about one staged row.
type ImportRowUpdate struct {
	RowNo    int
	State    string
	Errors   []domain.FieldError
	ResultID string
	// Fields, when non-nil, replaces the staged fields (validation writes the resolved values
	// back -- a ref label becomes its id -- so apply never resolves twice).
	Fields map[string]any
}

// ImportJobPatch is the bounded set of job columns a phase step may move.
type ImportJobPatch struct {
	// FromStatus, when set, fences the patch on the job still being in that status (a phase
	// end must not overwrite a cancel that landed mid-chunk); a miss is ErrVersionConflict.
	FromStatus    *string
	Status        *string
	ProgressRowNo *int
	Error         *string
	// Counters are ADDED, never set, so two chunks cannot lose each other's rows.
	AddValid, AddInvalid, AddApplied, AddFailed int
	Finished                                    bool
	// Release drops the worker's claim (the phase is over or the job is parked).
	Release bool
}

// ImportRowsParams pages a job's rows for the preview (state-filtered, keyset on row_no).
type ImportRowsParams struct {
	State      string // "" = every row
	AfterRowNo int
	Limit      int
}

// ImportBundlePatch is the bounded set of bundle columns the orchestrator may move.
type ImportBundlePatch struct {
	// FromStatus fences the patch on the bundle still being in that status; a miss is
	// ErrVersionConflict.
	FromStatus *string
	Status     *string
	Error      *string
	Finished   bool
}

// ImportRowResult is what apply-time reference resolution needs of a sibling tab's row.
type ImportRowResult struct {
	RowNo    int
	State    string
	ResultID string
}

// ImportRepository is the durable side of a bulk upload: the job row and its staged lines.
type ImportRepository interface {
	// --- workbooks (bundles) ---
	// CreateImportBundle inserts the bundle row (its jobs are created one by one).
	CreateImportBundle(ctx context.Context, bundle domain.ImportBundle, tenantID string) error
	// GetImportBundle reads a bundle with its jobs in bundle order.
	GetImportBundle(ctx context.Context, tenantID, bundleID string) (domain.ImportBundle, error)
	// ListImportBundles is the tenant's recent workbooks, newest first, each with its jobs.
	ListImportBundles(ctx context.Context, tenantID string, limit int) ([]domain.ImportBundle, error)
	// DueImportBundleIDs is every bundle still validating or applying, for the recovery sweep.
	DueImportBundleIDs(ctx context.Context, tenantID string, limit int) ([]string, error)
	PatchImportBundle(ctx context.Context, tenantID, bundleID string, patch ImportBundlePatch) (domain.ImportBundle, error)
	// RequestImportBundleApply moves a previewed bundle to applying, fenced on the status.
	RequestImportBundleApply(ctx context.Context, tenantID, bundleID, actorID string) (domain.ImportBundle, bool, error)
	// CancelImportBundle parks the bundle and every tab that has not finished.
	CancelImportBundle(ctx context.Context, tenantID, bundleID string) (domain.ImportBundle, bool, error)
	// PromoteImportJob moves a job from one status to another, fenced on the first (a queued
	// tab to validating when its turn comes); ok false when it is not in that status.
	PromoteImportJob(ctx context.Context, tenantID, jobID, from, to string) (domain.ImportJob, bool, error)
	// --- rows ---
	// ClaimImportRows moves the named valid rows to applying and returns the row numbers that
	// actually moved (a cancel that marked some skipped meanwhile keeps them).
	ClaimImportRows(ctx context.Context, tenantID, jobID string, rowNos []int) ([]int, error)
	// ImportRowResults reads the state and result id of the named rows (a sibling tab's rows a
	// reference token names).
	ImportRowResults(ctx context.Context, tenantID, jobID string, rowNos []int) (map[int]ImportRowResult, error)

	CreateImportJob(ctx context.Context, job domain.ImportJob, tenantID string) error
	// StageImportRows appends one chunk of lines; row_no is the sheet line, unique per job.
	StageImportRows(ctx context.Context, tenantID, jobID string, rows []domain.ImportRow) error
	GetImportJob(ctx context.Context, tenantID, jobID string) (domain.ImportJob, error)
	ListImportJobs(ctx context.Context, tenantID, register string, limit int) ([]domain.ImportJob, error)
	// ClaimImportJob takes the job for one worker for the lease; ok is false when another
	// worker holds a live claim or the job is not in a workable status.
	ClaimImportJob(ctx context.Context, tenantID, jobID, worker string, lease time.Duration) (domain.ImportJob, bool, error)
	// DueImportJobIDs is every job in a workable status whose claim is absent or expired, for
	// the recovery sweep.
	DueImportJobIDs(ctx context.Context, tenantID string, lease time.Duration, limit int) ([]string, error)
	// ImportRowsAfter is the next chunk of a phase: rows in the given states after row_no.
	ImportRowsAfter(ctx context.Context, tenantID, jobID string, states []string, afterRowNo, limit int) ([]domain.ImportRow, error)
	ImportRows(ctx context.Context, tenantID, jobID string, p ImportRowsParams) ([]domain.ImportRow, error)
	// UpdateImportRows writes a chunk's outcomes; only rows still in fromState move (a cancel
	// that marked them skipped meanwhile wins). The return value is the number of rows that
	// actually transitioned.
	UpdateImportRows(ctx context.Context, tenantID, jobID, fromState string, updates []ImportRowUpdate) (int, error)
	PatchImportJob(ctx context.Context, tenantID, jobID string, patch ImportJobPatch) (domain.ImportJob, error)
	// RequestImportApply moves a previewed job to applying, fenced on the status; ok false when
	// it is not previewed.
	RequestImportApply(ctx context.Context, tenantID, jobID, actorID string) (domain.ImportJob, bool, error)
	CancelImportJob(ctx context.Context, tenantID, jobID string) (domain.ImportJob, bool, error)
}
