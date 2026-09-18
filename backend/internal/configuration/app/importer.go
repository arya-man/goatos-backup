package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// BULK UPLOAD (maintainer instruction 2026-09-18: "one lakh animals; it should work"). Three
// phases, each bounded and resumable:
//
//	STAGE     the request streams the file line by line into configuration_import_rows in
//	          chunks; it never holds the sheet. Then the job is kicked.
//	VALIDATE  a processor claims the job and walks the staged rows in chunks after a row_no
//	          cursor: header-matched fields, refs resolved (a label becomes its id), the same
//	          ValidateWrite the drawer runs, in-sheet duplicates. Each row is marked valid or
//	          invalid with its messages; the job becomes 'previewed'.
//	APPLY     on the person's say-so, the processor walks the VALID rows the same way and
//	          writes each through the ordinary service (Create or Update), under an idempotency
//	          key of (job, row_no). A worker that dies mid-chunk resumes after the last row it
//	          finished; a row it had written but not marked is replayed by the key and comes back
//	          as the original result, never written twice.
//
// Animals are the one register whose rows are not written here: each chunk is handed to the
// herd register's own bulk pipeline (identity's preview + commit), so every herd rule stays in
// the one place that owns it.
//
// Where it runs: the API kicks a bounded goroutine on upload and on apply so a small sheet
// previews in seconds; the kernel worker's ConfigurationImportStage sweeps for jobs whose claim
// lapsed (the API restarted mid-file) and finishes them. Both go through Process, both claim
// first, so a job is never worked twice at once.

// AnimalBulk is the slice of identity's admin service the animals register imports through.
type AnimalBulk interface {
	PreviewAdminGoatBulkImport(ctx context.Context, input identityapp.PreviewAdminGoatBulkInput) (*identitydomain.AdminGoatBulkResponse, error)
	CommitAdminGoatBulkImport(ctx context.Context, input identityapp.CommitAdminGoatBulkInput) (*identitydomain.AdminGoatBulkResponse, error)
}

const (
	// stageChunk is how many lines one COPY carries while staging.
	stageChunk = 1000
	// processChunk is how many rows a validate/apply step reads at a time.
	processChunk = 500
	// animalChunk is identity's own bulk ceiling (maxBulkRows), one preview/commit per chunk.
	animalChunk = 500
	// DefaultMaxImportRows bounds one sheet.
	DefaultMaxImportRows = 200000
	// claimLease is how long a processor's claim is honoured before a sweep may take the job.
	claimLease = 5 * time.Minute
	// jobTimeout bounds one in-process run; a job longer than this is finished by later runs.
	jobTimeout = 20 * time.Minute
)

// Importer stages, validates and applies bulk sheets.
type Importer struct {
	svc     *Service
	jobs    ports.ImportRepository
	animals AnimalBulk
	log     *slog.Logger
	worker  string
	maxRows int
	// sem bounds the in-process kicks so an upload storm cannot fan out unbounded goroutines.
	sem chan struct{}
	wg  sync.WaitGroup
	now func() time.Time
}

// NewImporter wires the importer. animals may be nil on a process with no identity service; an
// animals sheet then fails closed with a clear job error.
func NewImporter(svc *Service, jobs ports.ImportRepository, animals AnimalBulk, worker string, log *slog.Logger) *Importer {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(worker) == "" {
		worker = "importer-" + randomID()[:8]
	}
	return &Importer{svc: svc, jobs: jobs, animals: animals, log: log, worker: worker, maxRows: DefaultMaxImportRows, sem: make(chan struct{}, 2), now: time.Now}
}

// Stage reads the uploaded sheet into a new job and returns it (status validating).
func (i *Importer) Stage(ctx context.Context, w ports.WriteParams, register, fileName string, src io.Reader) (domain.ImportJob, error) {
	reg, err := i.svc.Register(ctx, w.TenantID, register)
	if err != nil {
		return domain.ImportJob{}, err
	}
	if !reg.Importable {
		return domain.ImportJob{}, domain.ErrReadOnlyRegister
	}
	format, err := FormatFromFileName(fileName)
	if err != nil {
		return domain.ImportJob{}, err
	}
	reader, err := OpenSheet(format, src)
	if err != nil {
		return domain.ImportJob{}, err
	}
	header, err := reader.Next()
	if err != nil {
		return domain.ImportJob{}, BadRequest("invalid_file", "The file needs a header row naming the columns.")
	}
	keys, unknown, missing := domain.MatchHeader(reg, header)
	if len(missing) > 0 {
		return domain.ImportJob{}, &Error{Code: "missing_columns", HTTPStatus: 422,
			Message: "The header is missing required columns: " + strings.Join(missing, ", ") + ". Download the template to see the expected columns."}
	}
	if len(unknown) > 0 && len(unknown) == len(header) {
		return domain.ImportJob{}, BadRequest("invalid_file", "None of the header cells match this list's columns. Download the template to see the expected columns.")
	}
	job := domain.ImportJob{ID: randomID(), Register: register, FileName: strings.TrimSpace(fileName), Format: format, Status: domain.ImportValidating, CreatedBy: w.ActorID}
	if err := i.jobs.CreateImportJob(ctx, job, w.TenantID); err != nil {
		return domain.ImportJob{}, err
	}
	chunk := make([]domain.ImportRow, 0, stageChunk)
	rowNo := 1 // the header
	total := 0
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := i.jobs.StageImportRows(ctx, w.TenantID, job.ID, chunk); err != nil {
			return err
		}
		total += len(chunk)
		chunk = chunk[:0]
		return nil
	}
	for {
		record, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			i.fail(ctx, w.TenantID, job.ID, "The file could not be read past row "+strconv.Itoa(rowNo)+": "+err.Error())
			return domain.ImportJob{}, BadRequest("invalid_file", "The file could not be read past row "+strconv.Itoa(rowNo)+".")
		}
		rowNo++
		if domain.IsBlankRecord(record) {
			continue
		}
		if total+len(chunk) >= i.maxRows {
			i.fail(ctx, w.TenantID, job.ID, fmt.Sprintf("More than %d rows; split the file.", i.maxRows))
			return domain.ImportJob{}, &Error{Code: "sheet_too_large", HTTPStatus: 422, Message: fmt.Sprintf("A sheet can carry up to %d rows. Split the file and upload the parts.", i.maxRows)}
		}
		chunk = append(chunk, domain.ImportRow{RowNo: rowNo, Fields: domain.SheetRow(keys, record)})
		if len(chunk) == stageChunk {
			if err := flush(); err != nil {
				return domain.ImportJob{}, err
			}
		}
	}
	if err := flush(); err != nil {
		return domain.ImportJob{}, err
	}
	if total == 0 {
		i.fail(ctx, w.TenantID, job.ID, "The file has a header but no rows.")
		return domain.ImportJob{}, BadRequest("invalid_file", "The file has a header but no rows.")
	}
	staged, err := i.jobs.GetImportJob(ctx, w.TenantID, job.ID)
	if err != nil {
		return domain.ImportJob{}, err
	}
	i.Kick(w.TenantID, job.ID)
	return staged, nil
}

func (i *Importer) fail(ctx context.Context, tenantID, jobID, msg string) {
	status := domain.ImportFailed
	if _, err := i.jobs.PatchImportJob(ctx, tenantID, jobID, ports.ImportJobPatch{Status: &status, Error: &msg, Finished: true, Release: true}); err != nil {
		i.log.Warn("configuration import: mark failed", "job_id", jobID, "error", err)
	}
}

// Kick runs Process in the background, bounded by the semaphore; a kick that finds it full
// returns at once and the kernel sweep finishes the job.
func (i *Importer) Kick(tenantID, jobID string) {
	select {
	case i.sem <- struct{}{}:
	default:
		i.log.Info("configuration import: kick deferred to the worker sweep", "job_id", jobID)
		return
	}
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		defer func() { <-i.sem }()
		defer func() {
			if r := recover(); r != nil {
				i.log.Error("configuration import: panic", "job_id", jobID, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		if err := i.Process(ctx, tenantID, jobID); err != nil {
			i.log.Warn("configuration import: process", "job_id", jobID, "error", err)
		}
	}()
}

// Wait blocks until in-flight kicks finish (shutdown).
func (i *Importer) Wait() { i.wg.Wait() }

// ProcessDue finishes jobs whose claim lapsed: the kernel worker's sweep.
func (i *Importer) ProcessDue(ctx context.Context, tenantID string, limit int) (int, error) {
	ids, err := i.jobs.DueImportJobIDs(ctx, tenantID, claimLease, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if err := i.Process(ctx, tenantID, id); err != nil {
			i.log.Warn("configuration import: sweep", "job_id", id, "error", err)
			continue
		}
		done++
	}
	return done, nil
}

// Process claims the job and runs its current phase to the end (or until ctx ends, leaving the
// cursor where it is).
func (i *Importer) Process(ctx context.Context, tenantID, jobID string) error {
	job, ok, err := i.jobs.ClaimImportJob(ctx, tenantID, jobID, i.worker, claimLease)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	reg, err := i.svc.Register(ctx, tenantID, job.Register)
	if err != nil {
		i.fail(ctx, tenantID, jobID, "This list no longer exists.")
		return err
	}
	switch job.Status {
	case domain.ImportValidating:
		err = i.validate(ctx, tenantID, job, reg)
	case domain.ImportApplying, domain.ImportCancelled:
		err = i.apply(ctx, tenantID, job, reg)
	default:
		return nil
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		i.fail(ctx, tenantID, jobID, "The import stopped: "+HTTPError(err).Message)
	}
	return err
}

// rowValidator validates one staged row of a register into the fields apply will write.
type rowValidator func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError)

func (i *Importer) validate(ctx context.Context, tenantID string, job domain.ImportJob, reg domain.Register) error {
	var validateChunk func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error)
	if reg.Key == domain.RegAnimals {
		v, err := i.animalValidator(ctx, tenantID, false, job)
		if err != nil {
			return err
		}
		validateChunk = v
	} else {
		v, err := i.registerValidator(ctx, tenantID, reg)
		if err != nil {
			return err
		}
		validateChunk = perRow(v)
	}
	validating := domain.ImportValidating
	after := job.ProgressRowNo
	for {
		rows, err := i.jobs.ImportRowsAfter(ctx, tenantID, job.ID, []string{domain.ImportRowStaged}, after, processChunk)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		updates, err := validateChunk(ctx, rows)
		if err != nil {
			return err
		}
		patch := ports.ImportJobPatch{}
		for _, u := range updates {
			if u.State == domain.ImportRowValid {
				patch.AddValid++
			} else {
				patch.AddInvalid++
			}
		}
		moved, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowStaged, updates)
		if err != nil {
			return err
		}
		if moved == 0 {
			return nil
		}
		after = rows[len(rows)-1].RowNo
		patch.ProgressRowNo = &after
		patch.FromStatus = &validating
		if _, err := i.jobs.PatchImportJob(ctx, tenantID, job.ID, patch); err != nil {
			return phaseEnded(err)
		}
		// Renew the claim; a job cancelled meanwhile is no longer workable and the loop stops.
		if _, ok, err := i.jobs.ClaimImportJob(ctx, tenantID, job.ID, i.worker, claimLease); err != nil || !ok {
			return err
		}
	}
	status := domain.ImportPreviewed
	zero := 0
	_, err := i.jobs.PatchImportJob(ctx, tenantID, job.ID, ports.ImportJobPatch{FromStatus: &validating, Status: &status, ProgressRowNo: &zero, Release: true})
	return phaseEnded(err)
}

func perRow(v rowValidator) func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error) {
	return func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error) {
		out := make([]ports.ImportRowUpdate, 0, len(rows))
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fields, errs := v(ctx, row)
			u := ports.ImportRowUpdate{RowNo: row.RowNo, State: domain.ImportRowValid, Errors: errs, Fields: fields}
			if len(errs) > 0 {
				u.State = domain.ImportRowInvalid
				u.Fields = nil
			}
			out = append(out, u)
		}
		return out, nil
	}
}

// registerValidator builds the per-row validator of an ordinary register: header keys are
// already column keys; refs resolve through the target's active options, loaded once.
func (i *Importer) registerValidator(ctx context.Context, tenantID string, reg domain.Register) (rowValidator, error) {
	refs := map[string]domain.RefIndex{}
	for _, c := range reg.Columns {
		if c.Type != domain.TypeRef {
			continue
		}
		idx, err := i.refIndex(ctx, tenantID, c.Ref)
		if err != nil {
			return nil, err
		}
		refs[c.Key] = idx
	}
	nameKey := ""
	for _, key := range []string{"name", "label"} {
		if _, ok := reg.Column(key); ok {
			nameKey = key
			break
		}
	}
	seenNames := map[string]int{}
	seenCodes := map[string]int{}
	seenIDs := map[string]int{}
	return func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError) {
		raw := make(map[string]any, len(row.Fields))
		for k, v := range row.Fields {
			raw[k] = v
		}
		id := strings.TrimSpace(fmt.Sprint(raw[domain.SheetColumnID]))
		if raw[domain.SheetColumnID] == nil {
			id = ""
		}
		delete(raw, domain.SheetColumnID)
		delete(raw, domain.SheetColumnStatus)
		for _, c := range reg.Columns {
			if c.Derived {
				delete(raw, c.Key)
			}
		}
		var errs []domain.FieldError
		for key, idx := range refs {
			v, ok := raw[key]
			if !ok || v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
				continue
			}
			resolved, msg, ok := idx.Resolve(fmt.Sprint(v))
			if !ok {
				col, _ := reg.Column(key)
				errs = append(errs, domain.FieldError{Field: key, Code: "unknown", Message: col.Label + ": " + msg})
				continue
			}
			raw[key] = resolved
		}
		if len(errs) > 0 {
			return nil, errs
		}
		var existing *domain.Row
		if id != "" {
			if reg.ImportCreateOnly {
				return nil, []domain.FieldError{{Field: domain.SheetColumnID, Code: "invalid", Message: "This list takes new rows only; leave id blank."}}
			}
			if prev, dup := seenIDs[id]; dup {
				return nil, []domain.FieldError{{Field: domain.SheetColumnID, Code: "duplicate", Message: "The same id is on row " + strconv.Itoa(prev) + "."}}
			}
			seenIDs[id] = row.RowNo
			got, err := i.svc.Get(ctx, tenantID, reg.Key, id)
			if err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return nil, []domain.FieldError{{Field: domain.SheetColumnID, Code: "unknown", Message: "No row of this list has this id. Leave id blank to add a new row."}}
				}
				return nil, []domain.FieldError{{Field: "row", Code: "error", Message: HTTPError(err).Message}}
			}
			existing = &got
		}
		kind, err := i.svc.kindFor(ctx, tenantID, reg, raw, existing)
		if err != nil {
			var vErr *domain.ValidationError
			if errors.As(err, &vErr) {
				return nil, vErr.Fields
			}
			return nil, []domain.FieldError{{Field: "row", Code: "error", Message: HTTPError(err).Message}}
		}
		clean, err := domain.ValidateWrite(reg, raw, existing, kind)
		if err != nil {
			var vErr *domain.ValidationError
			if errors.As(err, &vErr) {
				return nil, vErr.Fields
			}
			return nil, []domain.FieldError{{Field: "row", Code: "error", Message: err.Error()}}
		}
		if existing == nil {
			if nameKey != "" {
				name := strings.ToLower(strings.TrimSpace(domain.FieldString(clean, nameKey)))
				if prev, dup := seenNames[name]; dup && name != "" {
					col, _ := reg.Column(nameKey)
					return nil, []domain.FieldError{{Field: nameKey, Code: "duplicate", Message: col.Label + " is repeated on row " + strconv.Itoa(prev) + "."}}
				}
				seenNames[name] = row.RowNo
			}
			if code := strings.TrimSpace(domain.FieldString(clean, "code")); code != "" {
				if prev, dup := seenCodes[code]; dup {
					return nil, []domain.FieldError{{Field: "code", Code: "duplicate", Message: "Code is repeated on row " + strconv.Itoa(prev) + "."}}
				}
				seenCodes[code] = row.RowNo
			}
		}
		if id != "" {
			clean[domain.SheetColumnID] = id
		}
		return clean, nil
	}, nil
}

func (i *Importer) apply(ctx context.Context, tenantID string, job domain.ImportJob, reg domain.Register) error {
	var applyChunk func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error)
	if reg.Key == domain.RegAnimals {
		v, err := i.animalValidator(ctx, tenantID, true, job)
		if err != nil {
			return err
		}
		applyChunk = v
	} else {
		applyChunk = perRow(func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError) {
			fields := make(map[string]any, len(row.Fields))
			for k, v := range row.Fields {
				fields[k] = v
			}
			id := ""
			if v, ok := fields[domain.SheetColumnID]; ok && v != nil {
				id = fmt.Sprint(v)
			}
			delete(fields, domain.SheetColumnID)
			w := ports.WriteParams{TenantID: tenantID, ActorID: job.CreatedBy, IdempotencyKey: importRowKey(job.ID, row.RowNo), TraceID: job.ID}
			var (
				written domain.Row
				err     error
			)
			if id != "" {
				written, err = i.svc.Update(ctx, w, reg.Key, id, fields, 0)
			} else {
				written, err = i.svc.Create(ctx, w, reg.Key, fields)
			}
			if err != nil {
				appErr := HTTPError(err)
				if len(appErr.Fields) > 0 {
					return nil, appErr.Fields
				}
				return nil, []domain.FieldError{{Field: "row", Code: appErr.Code, Message: appErr.Message}}
			}
			return map[string]any{"__result_id": written.ID}, nil
		})
	}
	applying := domain.ImportApplying
	after := job.ProgressRowNo
	for {
		states := []string{domain.ImportRowApplying, domain.ImportRowValid}
		if job.Status == domain.ImportCancelled {
			states = []string{domain.ImportRowApplying}
		}
		rows, err := i.jobs.ImportRowsAfter(ctx, tenantID, job.ID, states, after, processChunk)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if _, ok, err := i.jobs.ClaimImportJob(ctx, tenantID, job.ID, i.worker, claimLease); err != nil || !ok {
				return err
			}
			if row.State == domain.ImportRowValid {
				claimed := []ports.ImportRowUpdate{{RowNo: row.RowNo, State: domain.ImportRowApplying, Fields: row.Fields}}
				moved, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowValid, claimed)
				if err != nil {
					return err
				}
				if moved == 0 {
					return nil
				}
			}
			updates, err := applyChunk(ctx, []domain.ImportRow{row})
			if err != nil {
				return err
			}
			if len(updates) != 1 {
				return fmt.Errorf("configuration import: apply row %d returned %d updates", row.RowNo, len(updates))
			}
			patch := ports.ImportJobPatch{}
			if updates[0].State == domain.ImportRowValid {
				updates[0].State = domain.ImportRowApplied
				if updates[0].Fields != nil {
					updates[0].ResultID = domain.FieldString(updates[0].Fields, "__result_id")
					updates[0].Fields = nil
				}
				patch.AddApplied = 1
			} else {
				updates[0].State = domain.ImportRowFailed
				patch.AddFailed = 1
			}
			moved, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowApplying, updates)
			if err != nil {
				return err
			}
			if moved == 0 {
				return nil
			}
			after = row.RowNo
			patch.ProgressRowNo = &after
			if _, err := i.jobs.PatchImportJob(ctx, tenantID, job.ID, patch); err != nil {
				return phaseEnded(err)
			}
		}
	}
	patch := ports.ImportJobPatch{Finished: true, Release: true}
	if job.Status == domain.ImportApplying {
		status := domain.ImportApplied
		patch.FromStatus = &applying
		patch.Status = &status
	}
	_, err := i.jobs.PatchImportJob(ctx, tenantID, job.ID, patch)
	return phaseEnded(err)
}

// phaseEnded reads a fenced patch miss as "the job left this phase under us" (a cancel), which
// is not a failure of the import.
func phaseEnded(err error) error {
	if errors.Is(err, ports.ErrVersionConflict) {
		return nil
	}
	return err
}

func importRowKey(jobID string, rowNo int) string {
	return "cfgimport:" + jobID + ":" + strconv.Itoa(rowNo)
}

// animalSheetColumns is the header the animals chunk CSV carries into identity's bulk pipeline
// (its own header vocabulary: park_id / shed_id resolved here, the rest passed through).
var animalSheetColumns = []string{"animal_identifier_1", "animal_identifier_2", "species", "breed", "sex", "park_id", "shed_id", "partition_label", "management_stage", "dob", "entry_date", "origin", "reproductive_status", "weight_kg"}

// animalValidator builds the chunk function for the animals register: resolve park and pen to
// ids from the places registers (by code or name), then hand the chunk to identity's preview
// (validate) or preview + commit (apply). Identity reports per row; rows map back by line.
func (i *Importer) animalValidator(ctx context.Context, tenantID string, commit bool, job domain.ImportJob) (func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error), error) {
	if i.animals == nil {
		return nil, BadRequest("animals_unavailable", "Animal uploads are not available on this server.")
	}
	parks, err := i.placeIndex(ctx, tenantID, domain.RegParks, "code", "")
	if err != nil {
		return nil, err
	}
	pens, err := i.placeIndex(ctx, tenantID, domain.RegPens, "", "park_id")
	if err != nil {
		return nil, err
	}
	// Species and gender are refs on the animals register: a sheet may say "Sheep" or "sheep".
	species, err := i.refIndex(ctx, tenantID, domain.RegSpecies)
	if err != nil {
		return nil, err
	}
	sexes, err := i.refIndex(ctx, tenantID, domain.RegSexes)
	if err != nil {
		return nil, err
	}
	codes := map[string]domain.RefIndex{"species": species, "sex": sexes}
	return func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error) {
		out := make([]ports.ImportRowUpdate, 0, len(rows))
		for start := 0; start < len(rows); start += animalChunk {
			end := start + animalChunk
			if end > len(rows) {
				end = len(rows)
			}
			chunk := rows[start:end]
			// Line k of the chunk CSV (header = line 1) is chunk[k-2].
			var buf strings.Builder
			cw := csv.NewWriter(&buf)
			_ = cw.Write(animalSheetColumns)
			pre := make(map[int][]domain.FieldError, len(chunk))
			for k, row := range chunk {
				cells, errs := animalCells(row.Fields, parks, pens, codes)
				if len(errs) > 0 {
					pre[k] = errs
					cells = make([]string, len(animalSheetColumns)) // keeps the line count aligned
				}
				_ = cw.Write(cells)
			}
			cw.Flush()
			body, err := json.Marshal(identitydomain.AdminGoatBulkPreviewRequest{CSV: buf.String(), FileHash: fileHash(buf.String())})
			if err != nil {
				return nil, err
			}
			preview, err := i.animals.PreviewAdminGoatBulkImport(ctx, identityapp.PreviewAdminGoatBulkInput{TenantID: tenantID, TraceID: job.ID, RawBody: body})
			if err != nil {
				return nil, fmt.Errorf("animals preview: %w", err)
			}
			results := map[int]identitydomain.AdminGoatBulkRowResult{}
			for _, r := range preview.Rows {
				results[r.RowNumber-2] = r
			}
			if commit {
				commitRows := make([]identitydomain.AdminGoatBulkCommitRow, 0, len(preview.Rows))
				for _, r := range preview.Rows {
					if (r.Decision == "create" || r.Decision == "update_reproductive") && r.Normalized != nil && pre[r.RowNumber-2] == nil {
						commitRows = append(commitRows, identitydomain.AdminGoatBulkCommitRow{RowNumber: r.RowNumber, Normalized: r.Normalized})
					}
				}
				if len(commitRows) > 0 {
					cbody, err := json.Marshal(identitydomain.AdminGoatBulkCommitRequest{Rows: commitRows, FileHash: fileHash(buf.String()), PreviewToken: preview.PreviewToken})
					if err != nil {
						return nil, err
					}
					committed, err := i.animals.CommitAdminGoatBulkImport(ctx, identityapp.CommitAdminGoatBulkInput{
						TenantID: tenantID, ActorID: job.CreatedBy, IdempotencyKey: importRowKey(job.ID, chunk[0].RowNo), TraceID: job.ID, RawBody: cbody})
					if err != nil {
						return nil, fmt.Errorf("animals commit: %w", err)
					}
					for _, r := range committed.Rows {
						results[r.RowNumber-2] = r
					}
				}
			}
			for k, row := range chunk {
				u := ports.ImportRowUpdate{RowNo: row.RowNo, State: domain.ImportRowValid}
				if errs := pre[k]; len(errs) > 0 {
					u.State, u.Errors = domain.ImportRowInvalid, errs
				} else if r, ok := results[k]; ok {
					if len(r.Errors) > 0 || (r.Decision != "create" && r.Decision != "update_reproductive") {
						u.State = domain.ImportRowInvalid
						u.Errors = animalErrors(r)
					} else if commit {
						if r.Result != nil {
							u.Fields = map[string]any{"__result_id": r.Result.Goat.GoatID}
						}
					}
				} else {
					u.State = domain.ImportRowInvalid
					u.Errors = []domain.FieldError{{Field: "row", Code: "error", Message: "The herd register did not answer for this row."}}
				}
				out = append(out, u)
			}
		}
		return out, nil
	}, nil
}

func animalErrors(r identitydomain.AdminGoatBulkRowResult) []domain.FieldError {
	out := make([]domain.FieldError, 0, len(r.Errors)+1)
	for _, e := range r.Errors {
		field := e.Field
		switch field {
		case "shed_id", "shed_code":
			field = "pen_name"
		case "park_id", "park_code":
			field = "park"
		case "origin_type":
			field = "origin"
		}
		out = append(out, domain.FieldError{Field: field, Code: e.Code, Message: e.Message})
	}
	if len(out) == 0 {
		out = append(out, domain.FieldError{Field: "row", Code: r.Decision, Message: "The herd register did not accept this row (" + r.Decision + ")."})
	}
	return out
}

// refIndex loads a ref target's ACTIVE rows into a sheet lookup: the display label and, when
// the target carries one, its code -- so a sheet may say "CBE" as well as "Coimbatore". Targets
// are bounded catalogs, paged at the service ceiling.
func (i *Importer) refIndex(ctx context.Context, tenantID, register string) (domain.RefIndex, error) {
	var ids, labels []string
	cursor := ""
	for {
		// scale-guard:ignore: keyset page walk over a bounded reference catalog; cursor advances each iteration and the loop returns on an empty NextCursor
		page, err := i.svc.List(ctx, tenantID, register, ports.ListParams{Status: domain.StatusActive, Cursor: cursor, Limit: MaxPageSize})
		if err != nil {
			return domain.RefIndex{}, err
		}
		for _, row := range page.Rows {
			ids, labels = append(ids, row.ID), append(labels, row.Display)
			if code := strings.TrimSpace(domain.FieldString(row.Fields, "code")); code != "" && !strings.EqualFold(code, row.Display) {
				ids, labels = append(ids, row.ID), append(labels, code)
			}
		}
		if page.NextCursor == "" {
			return domain.NewRefIndex(ids, labels), nil
		}
		cursor = page.NextCursor
	}
}

// placeIndex loads one places register into a lookup by name and by code (the Code column when
// the register has one), optionally scoped by a parent field, so a sheet may say "CBE" or
// "Coimbatore" for the park and "Castro" for the pen.
type placeLookup struct {
	// key: parent id ("" when unscoped) + "\x1f" + lower(name or code) -> ids
	byKey map[string][]string
	byID  map[string]bool
}

func (i *Importer) placeIndex(ctx context.Context, tenantID, register, codeKey, parentKey string) (placeLookup, error) {
	idx := placeLookup{byKey: map[string][]string{}, byID: map[string]bool{}}
	cursor := ""
	for {
		// scale-guard:ignore: keyset page walk over the parks / pens catalog, loaded once per job; cursor advances each iteration and the loop returns on an empty NextCursor
		page, err := i.svc.List(ctx, tenantID, register, ports.ListParams{Status: domain.StatusActive, Cursor: cursor, Limit: MaxPageSize})
		if err != nil {
			return idx, err
		}
		for _, row := range page.Rows {
			parent := ""
			if parentKey != "" {
				parent = domain.FieldString(row.Fields, parentKey)
			}
			idx.byID[row.ID] = true
			for _, k := range []string{row.Display, domain.FieldString(row.Fields, "name")} {
				if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
					idx.add(parent, k, row.ID)
				}
			}
			if codeKey != "" {
				if k := strings.ToLower(strings.TrimSpace(domain.FieldString(row.Fields, codeKey))); k != "" {
					idx.add(parent, k, row.ID)
				}
			}
		}
		if page.NextCursor == "" {
			return idx, nil
		}
		cursor = page.NextCursor
	}
}

func (p placeLookup) add(parent, key, id string) {
	k := parent + "\x1f" + key
	for _, have := range p.byKey[k] {
		if have == id {
			return
		}
	}
	p.byKey[k] = append(p.byKey[k], id)
}

func (p placeLookup) resolve(parent, cell string) (string, bool, bool) {
	cell = strings.TrimSpace(cell)
	if p.byID[cell] {
		return cell, true, false
	}
	ids := p.byKey[parent+"\x1f"+strings.ToLower(cell)]
	switch len(ids) {
	case 1:
		return ids[0], true, false
	case 0:
		return "", false, false
	}
	return "", false, true
}

func animalCells(fields map[string]any, parks, pens placeLookup, codes map[string]domain.RefIndex) ([]string, []domain.FieldError) {
	get := func(k string) string {
		v := strings.TrimSpace(cellText(fields[k]))
		// A species / gender given by name resolves to its code; an unknown value is passed
		// through so the herd register's own message names it.
		if idx, ok := codes[k]; ok && v != "" {
			if id, _, ok := idx.Resolve(v); ok {
				return id
			}
		}
		return v
	}
	var errs []domain.FieldError
	parkID, ok, ambiguous := parks.resolve("", get("park"))
	switch {
	case get("park") == "":
		errs = append(errs, domain.FieldError{Field: "park", Code: "required", Message: "Park is required."})
	case ambiguous:
		errs = append(errs, domain.FieldError{Field: "park", Code: "ambiguous", Message: "More than one park is called " + strconv.Quote(get("park")) + "; use its code."})
	case !ok:
		errs = append(errs, domain.FieldError{Field: "park", Code: "unknown", Message: "No active park is called " + strconv.Quote(get("park")) + "."})
	}
	penID := ""
	if parkID != "" {
		var pok, pamb bool
		penID, pok, pamb = pens.resolve(parkID, get("pen_name"))
		switch {
		case get("pen_name") == "":
			errs = append(errs, domain.FieldError{Field: "pen_name", Code: "required", Message: "Pen is required."})
		case pamb:
			errs = append(errs, domain.FieldError{Field: "pen_name", Code: "ambiguous", Message: "More than one pen in that park is called " + strconv.Quote(get("pen_name")) + "."})
		case !pok:
			errs = append(errs, domain.FieldError{Field: "pen_name", Code: "unknown", Message: "No active pen in that park is called " + strconv.Quote(get("pen_name")) + "."})
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return []string{get("animal_identifier_1"), get("animal_identifier_2"), get("species"), get("breed"), get("sex"), parkID, penID, get("partition_label"),
		get("management_stage"), get("dob"), get("entry_date"), get("origin"), get("reproductive_status"), get("weight_kg")}, nil
}

func fileHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
