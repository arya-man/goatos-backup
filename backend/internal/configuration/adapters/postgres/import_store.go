package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// The durable side of a bulk upload (migration 000349). Every method here is bounded by
// construction: a stage writes one chunk with CopyFrom, a phase reads the next chunk after a
// row_no cursor, and a row update is one UNNEST statement per chunk -- never a statement per
// row, never a job-sized array.

const importJobColumns = `job_id::text, register_key, file_name, file_format, status, total_rows, valid_rows, invalid_rows,
       applied_rows, failed_rows, progress_row_no, error, created_by, created_at, updated_at, finished_at,
       COALESCE(bundle_id::text, ''), bundle_order, sheet_name`

// Every statement is a package-level const so a query-plan test and the scale guard can reach it.
const (
	sqlImportJobInsert = `
INSERT INTO configuration_import_jobs (job_id, tenant_id, register_key, file_name, file_format, status, total_rows, created_by, bundle_id, bundle_order, sheet_name)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid, $10, $11)`
	sqlImportJobClaim = `
UPDATE configuration_import_jobs
SET claimed_at = now(), claimed_by = $3, updated_at = now()
WHERE tenant_id = $1::uuid AND job_id = $2::uuid
  AND (status IN ('validating', 'applying')
       OR (status = 'cancelled' AND EXISTS (
           SELECT 1 FROM configuration_import_rows r
           WHERE r.tenant_id = configuration_import_jobs.tenant_id
             AND r.job_id = configuration_import_jobs.job_id
             AND r.state = 'applying'
       )))
  AND (claimed_at IS NULL OR claimed_by = $3 OR claimed_at < now() - make_interval(secs => $4))
RETURNING ` + importJobColumns
	sqlImportJobsDue = `
SELECT job_id::text FROM configuration_import_jobs
WHERE tenant_id = $1::uuid
  AND (status IN ('validating', 'applying')
       OR (status = 'cancelled' AND EXISTS (
           SELECT 1 FROM configuration_import_rows r
           WHERE r.tenant_id = configuration_import_jobs.tenant_id
             AND r.job_id = configuration_import_jobs.job_id
             AND r.state = 'applying'
       )))
  AND (claimed_at IS NULL OR claimed_at < now() - make_interval(secs => $2))
ORDER BY created_at LIMIT $3`
	sqlImportRowsAfter = `
SELECT row_no, fields, state, errors, result_id FROM configuration_import_rows
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND state = ANY($3::text[]) AND row_no > $4
ORDER BY row_no LIMIT $5`
	sqlImportRowsPage = `
SELECT row_no, fields, state, errors, result_id FROM configuration_import_rows
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND ($3 = '' OR state = $3) AND row_no > $4
ORDER BY row_no LIMIT $5`
	sqlImportRowsUpdate = `
UPDATE configuration_import_rows x
SET state = u.state, errors = u.errors::jsonb, result_id = u.result_id, fields = COALESCE(u.fields::jsonb, x.fields)
FROM unnest($3::int[], $4::text[], $5::text[], $6::text[], $7::text[]) AS u(row_no, state, errors, result_id, fields)
WHERE x.tenant_id = $1::uuid AND x.job_id = $2::uuid AND x.row_no = u.row_no AND x.state = $8`
	sqlImportApplyRequest = `
UPDATE configuration_import_jobs
SET status = 'applying', progress_row_no = 0, apply_requested_at = now(), claimed_at = NULL, claimed_by = '', updated_at = now()
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND status = 'previewed'
RETURNING ` + importJobColumns
	sqlImportCancel = `
UPDATE configuration_import_jobs
SET status = 'cancelled', finished_at = now(), claimed_at = NULL, claimed_by = '', updated_at = now()
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND status IN ('queued', 'validating', 'previewed', 'applying')
RETURNING ` + importJobColumns
	sqlImportRowsClaim = `
UPDATE configuration_import_rows
SET state = 'applying'
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND state = 'valid' AND row_no = ANY($3::int[])
RETURNING row_no`
	sqlImportRowResults = `
SELECT row_no, state, result_id FROM configuration_import_rows
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND row_no = ANY($3::int[])`
	sqlImportJobPromote = `
UPDATE configuration_import_jobs
SET status = $4, progress_row_no = 0, claimed_at = NULL, claimed_by = '', updated_at = now(),
    apply_requested_at = CASE WHEN $4 = 'applying' THEN now() ELSE apply_requested_at END
WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND status = $3
RETURNING ` + importJobColumns
)

// --- workbooks (bundles, migration 000366) ---

const importBundleColumns = `bundle_id::text, file_name, status, unknown_sheets, error, created_by, created_at, updated_at, finished_at`

const (
	sqlImportBundleInsert = `
INSERT INTO configuration_import_bundles (bundle_id, tenant_id, file_name, status, unknown_sheets, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5::jsonb, $6)`
	sqlImportBundleGet = `
SELECT ` + importBundleColumns + ` FROM configuration_import_bundles WHERE tenant_id = $1::uuid AND bundle_id = $2::uuid`
	sqlImportBundleRecent = `
SELECT ` + importBundleColumns + ` FROM configuration_import_bundles WHERE tenant_id = $1::uuid ORDER BY created_at DESC, bundle_id LIMIT $2`
	sqlImportBundleJobs = `
SELECT ` + importJobColumns + ` FROM configuration_import_jobs
WHERE tenant_id = $1::uuid AND bundle_id = ANY($2::uuid[])
ORDER BY bundle_id, bundle_order, job_id`
	sqlImportBundlesDue = `
SELECT bundle_id::text FROM configuration_import_bundles
WHERE tenant_id = $1::uuid AND status IN ('validating', 'applying')
ORDER BY created_at LIMIT $2`
	sqlImportBundleApplyRequest = `
UPDATE configuration_import_bundles
SET status = 'applying', apply_requested_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND bundle_id = $2::uuid AND status = 'previewed'
RETURNING ` + importBundleColumns
	sqlImportBundleCancel = `
UPDATE configuration_import_bundles
SET status = 'cancelled', finished_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND bundle_id = $2::uuid AND status IN ('validating', 'previewed', 'applying')
RETURNING ` + importBundleColumns
	// Every unfinished tab of a cancelled bundle is parked the way a single job is; rows not yet
	// claimed for apply are skipped, a row already applying is finished by the sweep.
	sqlImportBundleCancelJobs = `
UPDATE configuration_import_jobs
SET status = 'cancelled', finished_at = now(), claimed_at = NULL, claimed_by = '', updated_at = now()
WHERE tenant_id = $1::uuid AND bundle_id = $2::uuid AND status IN ('queued', 'validating', 'previewed', 'applying')`
	sqlImportBundleSkipRows = `
UPDATE configuration_import_rows r
SET state = 'skipped'
FROM configuration_import_jobs j
WHERE j.tenant_id = $1::uuid AND j.bundle_id = $2::uuid AND j.status = 'cancelled'
  AND r.tenant_id = j.tenant_id AND r.job_id = j.job_id AND r.state IN ('staged', 'valid')`
)

func scanImportBundle(row pgx.Row) (domain.ImportBundle, error) {
	var (
		b        domain.ImportBundle
		unknown  []byte
		created  time.Time
		updated  time.Time
		finished *time.Time
	)
	if err := row.Scan(&b.ID, &b.FileName, &b.Status, &unknown, &b.Error, &b.CreatedBy, &created, &updated, &finished); err != nil {
		return domain.ImportBundle{}, err
	}
	b.UnknownSheets = []string{}
	if len(unknown) > 0 {
		if err := json.Unmarshal(unknown, &b.UnknownSheets); err != nil {
			return domain.ImportBundle{}, fmt.Errorf("configuration: decode bundle sheets: %w", err)
		}
	}
	b.Jobs = []domain.ImportJob{}
	b.CreatedAt = created.UTC().Format(time.RFC3339)
	b.UpdatedAt = updated.UTC().Format(time.RFC3339)
	if finished != nil {
		b.FinishedAt = finished.UTC().Format(time.RFC3339)
	}
	return b, nil
}

// CreateImportBundle inserts the bundle row.
func (r *Repository) CreateImportBundle(ctx context.Context, bundle domain.ImportBundle, tenantID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	unknown := bundle.UnknownSheets
	if unknown == nil {
		unknown = []string{}
	}
	raw, err := json.Marshal(unknown)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, sqlImportBundleInsert, bundle.ID, tenantID, bundle.FileName, bundle.Status, string(raw), bundle.CreatedBy)
	return err
}

// attachBundleJobs loads the jobs of the given bundles in ONE query and files them in order.
func (r *Repository) attachBundleJobs(ctx context.Context, tenantID string, bundles []domain.ImportBundle) error {
	if len(bundles) == 0 {
		return nil
	}
	ids := make([]string, 0, len(bundles))
	index := make(map[string]int, len(bundles))
	for i, b := range bundles {
		ids = append(ids, b.ID)
		index[b.ID] = i
	}
	rows, err := r.pool.Query(ctx, sqlImportBundleJobs, tenantID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return err
		}
		if i, ok := index[job.BundleID]; ok {
			bundles[i].Jobs = append(bundles[i].Jobs, job)
		}
	}
	return rows.Err()
}

// GetImportBundle reads a bundle with its jobs in bundle order.
func (r *Repository) GetImportBundle(ctx context.Context, tenantID, bundleID string) (domain.ImportBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(bundleID) {
		return domain.ImportBundle{}, ports.ErrNotFound
	}
	bundle, err := scanImportBundle(r.pool.QueryRow(ctx, sqlImportBundleGet, tenantID, bundleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBundle{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ImportBundle{}, err
	}
	bundles := []domain.ImportBundle{bundle}
	if err := r.attachBundleJobs(ctx, tenantID, bundles); err != nil {
		return domain.ImportBundle{}, err
	}
	return bundles[0], nil
}

// ListImportBundles is the tenant's recent workbooks, newest first.
func (r *Repository) ListImportBundles(ctx context.Context, tenantID string, limit int) ([]domain.ImportBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, sqlImportBundleRecent, tenantID, limit)
	if err != nil {
		return nil, err
	}
	out := []domain.ImportBundle{}
	for rows.Next() {
		b, err := scanImportBundle(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachBundleJobs(ctx, tenantID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DueImportBundleIDs is every bundle still in flight, oldest first, for the recovery sweep.
func (r *Repository) DueImportBundleIDs(ctx context.Context, tenantID string, limit int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlImportBundlesDue, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PatchImportBundle moves the bundle's status / error, fenced on FromStatus when set.
func (r *Repository) PatchImportBundle(ctx context.Context, tenantID, bundleID string, p ports.ImportBundlePatch) (domain.ImportBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	set := []string{"updated_at = now()"}
	args := []any{tenantID, bundleID}
	if p.Status != nil {
		args = append(args, *p.Status)
		set = append(set, fmt.Sprintf("status = $%d", len(args)))
	}
	if p.Error != nil {
		args = append(args, *p.Error)
		set = append(set, fmt.Sprintf("error = $%d", len(args)))
	}
	if p.Finished {
		set = append(set, "finished_at = now()")
	}
	where := ` WHERE tenant_id = $1::uuid AND bundle_id = $2::uuid`
	if p.FromStatus != nil {
		args = append(args, *p.FromStatus)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	bundle, err := scanImportBundle(r.pool.QueryRow(ctx, `UPDATE configuration_import_bundles SET `+strings.Join(set, ", ")+where+` RETURNING `+importBundleColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if p.FromStatus != nil {
			return domain.ImportBundle{}, ports.ErrVersionConflict
		}
		return domain.ImportBundle{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ImportBundle{}, err
	}
	bundles := []domain.ImportBundle{bundle}
	if err := r.attachBundleJobs(ctx, tenantID, bundles); err != nil {
		return domain.ImportBundle{}, err
	}
	return bundles[0], nil
}

// RequestImportBundleApply flips previewed -> applying, fenced on the status.
func (r *Repository) RequestImportBundleApply(ctx context.Context, tenantID, bundleID, actorID string) (domain.ImportBundle, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	bundle, err := scanImportBundle(r.pool.QueryRow(ctx, sqlImportBundleApplyRequest, tenantID, bundleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBundle{}, false, nil
	}
	if err != nil {
		return domain.ImportBundle{}, false, err
	}
	bundles := []domain.ImportBundle{bundle}
	if err := r.attachBundleJobs(ctx, tenantID, bundles); err != nil {
		return domain.ImportBundle{}, false, err
	}
	return bundles[0], true, nil
}

// CancelImportBundle parks the bundle and every tab that has not finished. Tabs already applied
// stay applied -- the writes were real.
func (r *Repository) CancelImportBundle(ctx context.Context, tenantID, bundleID string) (domain.ImportBundle, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ImportBundle{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	bundle, err := scanImportBundle(tx.QueryRow(ctx, sqlImportBundleCancel, tenantID, bundleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportBundle{}, false, nil
	}
	if err != nil {
		return domain.ImportBundle{}, false, err
	}
	if _, err := tx.Exec(ctx, sqlImportBundleCancelJobs, tenantID, bundleID); err != nil {
		return domain.ImportBundle{}, false, err
	}
	if _, err := tx.Exec(ctx, sqlImportBundleSkipRows, tenantID, bundleID); err != nil {
		return domain.ImportBundle{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ImportBundle{}, false, err
	}
	bundles := []domain.ImportBundle{bundle}
	if err := r.attachBundleJobs(ctx, tenantID, bundles); err != nil {
		return domain.ImportBundle{}, false, err
	}
	return bundles[0], true, nil
}

// PromoteImportJob moves a job from one status to the next, fenced on the first, and resets the
// phase cursor and claim so the next processor starts the new phase from the top.
func (r *Repository) PromoteImportJob(ctx context.Context, tenantID, jobID, from, to string) (domain.ImportJob, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	job, err := scanImportJob(r.pool.QueryRow(ctx, sqlImportJobPromote, tenantID, jobID, from, to))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportJob{}, false, nil
	}
	return job, err == nil, err
}

// ClaimImportRows moves the named valid rows to applying in ONE statement and returns the rows
// that moved.
func (r *Repository) ClaimImportRows(ctx context.Context, tenantID, jobID string, rowNos []int) ([]int, error) {
	if len(rowNos) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	nos := make([]int32, len(rowNos))
	for i, n := range rowNos {
		nos[i] = int32(n)
	}
	rows, err := r.pool.Query(ctx, sqlImportRowsClaim, tenantID, jobID, nos)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int, 0, len(rowNos))
	for rows.Next() {
		var n int32
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, int(n))
	}
	return out, rows.Err()
}

// ImportRowResults reads the state and result id of the named rows of a job.
func (r *Repository) ImportRowResults(ctx context.Context, tenantID, jobID string, rowNos []int) (map[int]ports.ImportRowResult, error) {
	out := make(map[int]ports.ImportRowResult, len(rowNos))
	if len(rowNos) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	nos := make([]int32, len(rowNos))
	for i, n := range rowNos {
		nos[i] = int32(n)
	}
	rows, err := r.pool.Query(ctx, sqlImportRowResults, tenantID, jobID, nos)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			n   int32
			res ports.ImportRowResult
		)
		if err := rows.Scan(&n, &res.State, &res.ResultID); err != nil {
			return nil, err
		}
		res.RowNo = int(n)
		out[res.RowNo] = res
	}
	return out, rows.Err()
}

func scanImportJob(row pgx.Row) (domain.ImportJob, error) {
	var (
		j        domain.ImportJob
		created  time.Time
		updated  time.Time
		finished *time.Time
	)
	if err := row.Scan(&j.ID, &j.Register, &j.FileName, &j.Format, &j.Status, &j.TotalRows, &j.ValidRows, &j.InvalidRows,
		&j.AppliedRows, &j.FailedRows, &j.ProgressRowNo, &j.Error, &j.CreatedBy, &created, &updated, &finished,
		&j.BundleID, &j.BundleOrder, &j.SheetName); err != nil {
		return domain.ImportJob{}, err
	}
	j.CreatedAt = created.UTC().Format(time.RFC3339)
	j.UpdatedAt = updated.UTC().Format(time.RFC3339)
	if finished != nil {
		j.FinishedAt = finished.UTC().Format(time.RFC3339)
	}
	return j, nil
}

// CreateImportJob inserts the job row in 'validating' with no lines yet.
func (r *Repository) CreateImportJob(ctx context.Context, job domain.ImportJob, tenantID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.pool.Exec(ctx, sqlImportJobInsert,
		job.ID, tenantID, job.Register, job.FileName, job.Format, job.Status, job.TotalRows, job.CreatedBy, job.BundleID, job.BundleOrder, job.SheetName)
	return err
}

// StageImportRows appends one chunk of lines with COPY.
func (r *Repository) StageImportRows(ctx context.Context, tenantID, jobID string, rows []domain.ImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	src := make([][]any, 0, len(rows))
	for _, row := range rows {
		fields, err := json.Marshal(row.Fields)
		if err != nil {
			return fmt.Errorf("configuration: encode staged row %d: %w", row.RowNo, err)
		}
		src = append(src, []any{tenantID, jobID, row.RowNo, fields, domain.ImportRowStaged})
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"configuration_import_rows"},
		[]string{"tenant_id", "job_id", "row_no", "fields", "state"}, pgx.CopyFromRows(src))
	if err != nil {
		return fmt.Errorf("configuration: stage rows: %w", err)
	}
	_, err = r.pool.Exec(ctx, `UPDATE configuration_import_jobs SET total_rows = total_rows + $3, updated_at = now() WHERE tenant_id = $1::uuid AND job_id = $2::uuid`, tenantID, jobID, len(rows))
	return err
}

// GetImportJob reads one job.
func (r *Repository) GetImportJob(ctx context.Context, tenantID, jobID string) (domain.ImportJob, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(jobID) {
		return domain.ImportJob{}, ports.ErrNotFound
	}
	job, err := scanImportJob(r.pool.QueryRow(ctx, `SELECT `+importJobColumns+` FROM configuration_import_jobs WHERE tenant_id = $1::uuid AND job_id = $2::uuid`, tenantID, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportJob{}, ports.ErrNotFound
	}
	return job, err
}

// ListImportJobs is the register's recent uploads, newest first.
func (r *Repository) ListImportJobs(ctx context.Context, tenantID, register string, limit int) ([]domain.ImportJob, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT `+importJobColumns+` FROM configuration_import_jobs WHERE tenant_id = $1::uuid AND register_key = $2 ORDER BY created_at DESC, job_id LIMIT $3`, tenantID, register, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ImportJob{}
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// ClaimImportJob leases a workable job to one worker. A live claim by someone else, or a status
// that is not validating/applying, returns ok=false.
func (r *Repository) ClaimImportJob(ctx context.Context, tenantID, jobID, worker string, lease time.Duration) (domain.ImportJob, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	job, err := scanImportJob(r.pool.QueryRow(ctx, sqlImportJobClaim, tenantID, jobID, worker, lease.Seconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportJob{}, false, nil
	}
	return job, err == nil, err
}

// DueImportJobIDs is the recovery sweep's read: workable jobs nobody holds a live claim on.
func (r *Repository) DueImportJobIDs(ctx context.Context, tenantID string, lease time.Duration, limit int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlImportJobsDue, tenantID, lease.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanImportRows(rows pgx.Rows) ([]domain.ImportRow, error) {
	defer rows.Close()
	out := []domain.ImportRow{}
	for rows.Next() {
		var (
			row       domain.ImportRow
			fieldsRaw []byte
			errorsRaw []byte
		)
		if err := rows.Scan(&row.RowNo, &fieldsRaw, &row.State, &errorsRaw, &row.ResultID); err != nil {
			return nil, err
		}
		row.Fields = map[string]any{}
		if err := json.Unmarshal(fieldsRaw, &row.Fields); err != nil {
			return nil, fmt.Errorf("configuration: decode staged row %d: %w", row.RowNo, err)
		}
		row.Errors = []domain.FieldError{}
		if len(errorsRaw) > 0 {
			if err := json.Unmarshal(errorsRaw, &row.Errors); err != nil {
				return nil, fmt.Errorf("configuration: decode row errors %d: %w", row.RowNo, err)
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ImportRowsAfter is one phase chunk: rows in the given states after the cursor.
func (r *Repository) ImportRowsAfter(ctx context.Context, tenantID, jobID string, states []string, afterRowNo, limit int) ([]domain.ImportRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlImportRowsAfter, tenantID, jobID, states, afterRowNo, limit)
	if err != nil {
		return nil, err
	}
	return scanImportRows(rows)
}

// ImportRows pages a job's rows for the preview screen and the error sheet.
func (r *Repository) ImportRows(ctx context.Context, tenantID, jobID string, p ports.ImportRowsParams) ([]domain.ImportRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if p.Limit < 1 {
		p.Limit = 50
	}
	rows, err := r.pool.Query(ctx, sqlImportRowsPage, tenantID, jobID, p.State, p.AfterRowNo, p.Limit)
	if err != nil {
		return nil, err
	}
	return scanImportRows(rows)
}

// UpdateImportRows writes a chunk's outcomes in ONE statement.
func (r *Repository) UpdateImportRows(ctx context.Context, tenantID, jobID, fromState string, updates []ports.ImportRowUpdate) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rowNos := make([]int32, 0, len(updates))
	states := make([]string, 0, len(updates))
	errs := make([]string, 0, len(updates))
	results := make([]string, 0, len(updates))
	fields := make([]*string, 0, len(updates))
	for _, u := range updates {
		e := u.Errors
		if e == nil {
			e = []domain.FieldError{}
		}
		errRaw, err := json.Marshal(e)
		if err != nil {
			return 0, err
		}
		var f *string
		if u.Fields != nil {
			raw, err := json.Marshal(u.Fields)
			if err != nil {
				return 0, err
			}
			s := string(raw)
			f = &s
		}
		rowNos = append(rowNos, int32(u.RowNo))
		states = append(states, u.State)
		errs = append(errs, string(errRaw))
		results = append(results, u.ResultID)
		fields = append(fields, f)
	}
	tag, err := r.pool.Exec(ctx, sqlImportRowsUpdate,
		tenantID, jobID, rowNos, states, errs, results, fields, fromState)
	return int(tag.RowsAffected()), err
}

// PatchImportJob moves the bounded job columns a phase step may touch.
func (r *Repository) PatchImportJob(ctx context.Context, tenantID, jobID string, p ports.ImportJobPatch) (domain.ImportJob, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	set := []string{"updated_at = now()",
		"valid_rows = valid_rows + $3", "invalid_rows = invalid_rows + $4", "applied_rows = applied_rows + $5", "failed_rows = failed_rows + $6"}
	args := []any{tenantID, jobID, p.AddValid, p.AddInvalid, p.AddApplied, p.AddFailed}
	if p.Status != nil {
		args = append(args, *p.Status)
		set = append(set, fmt.Sprintf("status = $%d", len(args)))
	}
	if p.ProgressRowNo != nil {
		args = append(args, *p.ProgressRowNo)
		set = append(set, fmt.Sprintf("progress_row_no = $%d", len(args)))
	}
	if p.Error != nil {
		args = append(args, *p.Error)
		set = append(set, fmt.Sprintf("error = $%d", len(args)))
	}
	if p.Finished {
		set = append(set, "finished_at = now()")
	}
	if p.Release {
		set = append(set, "claimed_at = NULL", "claimed_by = ''")
	}
	where := ` WHERE tenant_id = $1::uuid AND job_id = $2::uuid`
	if p.FromStatus != nil {
		args = append(args, *p.FromStatus)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	job, err := scanImportJob(r.pool.QueryRow(ctx, `UPDATE configuration_import_jobs SET `+strings.Join(set, ", ")+where+` RETURNING `+importJobColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if p.FromStatus != nil {
			return domain.ImportJob{}, ports.ErrVersionConflict
		}
		return domain.ImportJob{}, ports.ErrNotFound
	}
	return job, err
}

// RequestImportApply flips previewed -> applying and resets the phase cursor, fenced on the
// status so a double click applies once.
func (r *Repository) RequestImportApply(ctx context.Context, tenantID, jobID, actorID string) (domain.ImportJob, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	job, err := scanImportJob(r.pool.QueryRow(ctx, sqlImportApplyRequest, tenantID, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportJob{}, false, nil
	}
	return job, err == nil, err
}

// CancelImportJob parks a job that has not finished. Rows already applied stay applied -- the
// writes were real. Rows not yet claimed for apply are marked skipped; a row already in applying
// is left for the worker sweep to finish through its row idempotency key.
func (r *Repository) CancelImportJob(ctx context.Context, tenantID, jobID string) (domain.ImportJob, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	job, err := scanImportJob(r.pool.QueryRow(ctx, sqlImportCancel, tenantID, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ImportJob{}, false, nil
	}
	if err != nil {
		return domain.ImportJob{}, false, err
	}
	_, err = r.pool.Exec(ctx, `UPDATE configuration_import_rows SET state = 'skipped' WHERE tenant_id = $1::uuid AND job_id = $2::uuid AND state IN ('staged', 'valid')`, tenantID, jobID)
	return job, true, err
}
