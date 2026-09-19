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
	"sort"
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
//
// ONBOARDING WORKBOOK (maintainer instruction 2026-09-19: one Excel file, one tab per list, so
// a new farm is set up from a single upload). A workbook is a BUNDLE of the jobs above, one per
// tab that matched a register, worked in domain.WorkbookOrder (parks before pens before
// partitions, lists before items, everything before animals). The mechanics per tab are
// unchanged; what the bundle adds is:
//
//	ORDER      tabs wait in 'queued' and are promoted one at a time; the sweep never claims a
//	           queued tab on its own, so a pens tab cannot validate before the parks tab did.
//	CROSS-TAB  a ref resolves against the target's stored rows PLUS the rows the target's own
//	           tab will create: such a value is stored as a BundleRowToken naming that sibling
//	           row, and at apply the token becomes the id that row actually wrote. A child whose
//	           parent row was not written fails with the parent's sheet and row named -- there is
//	           never a pen without its park.
//	PREVIEW    every tab is validated before anything is applied; Apply is one click for the
//	           whole file. A tab that stops for a reason other than a row (the list vanished,
//	           the server errored) stops the bundle before the next tab starts.
//	ANIMALS    park, pen, species, gender and stage may come from sibling tabs. At preview the
//	           herd register cannot see those yet, so the identity checks that depend on them are
//	           set aside for such a row and re-run at apply, when the tabs before it exist.

// AnimalBulk is the slice of identity's admin service the animals register imports through.
type AnimalBulk interface {
	PreviewAdminGoatBulkImport(ctx context.Context, input identityapp.PreviewAdminGoatBulkInput) (*identitydomain.AdminGoatBulkResponse, error)
	CommitAdminGoatBulkImport(ctx context.Context, input identityapp.CommitAdminGoatBulkInput) (*identitydomain.AdminGoatBulkResponse, error)
}

const (
	// stageChunk is how many lines one COPY carries while staging.
	stageChunk = 1000
	// processChunk is how many rows a validate step reads at a time.
	processChunk = 500
	// applyChunk is how many rows an apply step claims and writes at a time: the job claim is
	// renewed and the outcomes written once per chunk, so a hundred-thousand-row apply is not
	// five round trips per row. It is also the most a cancel lets finish once claimed.
	applyChunk = 200
	// bundleKindDepth bounds the walk from a sheet category up to the root that carries its kind.
	bundleKindDepth = 12
	// applyWorkers is how many rows of one chunk are written at once when the rows are
	// independent -- every register but the ones whose rows may name each other (a list under
	// a list) and animals, which the herd register takes as a chunk. One row is one short
	// transaction ending in a commit, so the wall time is the disk's fsync latency times the
	// rows, and a handful of them in flight cuts a lakh-row apply by that handful. Bounded well
	// under the pool (GOATOS_PG_MAX_CONNS, default 10) so a sweep beside it still gets a
	// connection.
	applyWorkers = 6
	// animalChunk is identity's own bulk ceiling (maxBulkRows), one preview/commit per chunk.
	animalChunk = 500
	// DefaultMaxImportRows bounds one sheet (one tab of a workbook). Nothing here holds a sheet,
	// so the bound is the dedupe maps a validation keeps (a name and a code per row) and the
	// time a person is willing to wait, not memory; 500,000 keeps both comfortable.
	DefaultMaxImportRows = 500000
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
	keys, err := matchSheetHeader(reg, header, "")
	if err != nil {
		return domain.ImportJob{}, err
	}
	job := domain.ImportJob{ID: randomID(), Register: register, FileName: strings.TrimSpace(fileName), Format: format, Status: domain.ImportValidating, CreatedBy: w.ActorID}
	if err := i.jobs.CreateImportJob(ctx, job, w.TenantID); err != nil {
		return domain.ImportJob{}, err
	}
	total, err := i.stageRows(ctx, w.TenantID, job.ID, reader, keys, "")
	if err != nil {
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

// matchSheetHeader maps a header onto the register's columns and turns the two header-level
// problems into the upload's errors, prefixed with the tab when the sheet is a workbook tab.
func matchSheetHeader(reg domain.Register, header []string, tab string) ([]string, error) {
	keys, unknown, missing := domain.MatchHeader(reg, header)
	prefix := ""
	if tab != "" {
		prefix = tab + ": "
	}
	if len(missing) > 0 {
		return nil, &Error{Code: "missing_columns", HTTPStatus: 422,
			Message: prefix + "The header is missing required columns: " + strings.Join(missing, ", ") + ". Download the template to see the expected columns."}
	}
	if len(unknown) > 0 && len(unknown) == len(header) {
		return nil, BadRequest("invalid_file", prefix+"None of the header cells match this list's columns. Download the template to see the expected columns.")
	}
	return keys, nil
}

// stageRows streams a reader's data lines into the job in COPY chunks and returns how many it
// staged. A read error or an over-cap sheet fails the job with a message naming the line.
func (i *Importer) stageRows(ctx context.Context, tenantID, jobID string, reader SheetReader, keys []string, tab string) (int, error) {
	prefix := ""
	if tab != "" {
		prefix = tab + ": "
	}
	chunk := make([]domain.ImportRow, 0, stageChunk)
	rowNo := 1 // the header
	total := 0
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := i.jobs.StageImportRows(ctx, tenantID, jobID, chunk); err != nil {
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
			i.fail(ctx, tenantID, jobID, prefix+"The file could not be read past row "+strconv.Itoa(rowNo)+": "+err.Error())
			return 0, BadRequest("invalid_file", prefix+"The file could not be read past row "+strconv.Itoa(rowNo)+".")
		}
		rowNo++
		if domain.IsBlankRecord(record) {
			continue
		}
		if total+len(chunk) >= i.maxRows {
			i.fail(ctx, tenantID, jobID, fmt.Sprintf("%sMore than %d rows; split the file.", prefix, i.maxRows))
			return 0, &Error{Code: "sheet_too_large", HTTPStatus: 422, Message: fmt.Sprintf("%sA sheet can carry up to %d rows. Split the file and upload the parts.", prefix, i.maxRows)}
		}
		chunk = append(chunk, domain.ImportRow{RowNo: rowNo, Fields: domain.SheetRow(keys, record)})
		if len(chunk) == stageChunk {
			if err := flush(); err != nil {
				return 0, err
			}
		}
	}
	if err := flush(); err != nil {
		return 0, err
	}
	return total, nil
}

// workbookTab is one matched worksheet, planned before anything is written.
type workbookTab struct {
	sheet    int
	name     string
	register domain.Register
	keys     []string
}

// StageWorkbook reads an onboarding workbook -- one worksheet per register -- into a bundle of
// jobs, one per tab that carries rows, in domain.WorkbookOrder, and returns it (validating).
// Every tab's header is checked before anything is written, so a bad tab is refused up front
// with its name rather than discovered after nine others were staged. Tabs whose name matches
// no register are reported on the bundle and ignored; tabs with a header and no rows are
// skipped, since the template ships every tab and a farm fills the ones it needs.
func (i *Importer) StageWorkbook(ctx context.Context, w ports.WriteParams, fileName string, src io.Reader) (domain.ImportBundle, error) {
	format, err := FormatFromFileName(fileName)
	if err != nil {
		return domain.ImportBundle{}, err
	}
	if format != domain.FormatXLSX {
		return domain.ImportBundle{}, BadRequest("invalid_format", "Upload the workbook as an .xlsx file with one tab per list; a .csv has no tabs.")
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return domain.ImportBundle{}, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That file could not be read.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
	}
	wb, err := openXLSXFile(data)
	if err != nil {
		return domain.ImportBundle{}, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That file could not be read as an Excel workbook.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
	}
	registers, err := i.svc.WorkbookRegisters(ctx, w.TenantID)
	if err != nil {
		return domain.ImportBundle{}, err
	}
	order := make(map[string]int, len(registers))
	byKey := make(map[string]domain.Register, len(registers))
	for n, reg := range registers {
		order[reg.Key] = n
		byKey[reg.Key] = reg
	}
	var (
		tabs    []workbookTab
		unknown []string
		matched = map[string]string{}
	)
	for sheetNo, name := range wb.Sheets() {
		key := domain.MatchSheetName(name, registers)
		if key == "" {
			unknown = append(unknown, name)
			continue
		}
		if prev, dup := matched[key]; dup {
			return domain.ImportBundle{}, &Error{Code: "duplicate_sheet", HTTPStatus: 422, Message: "Tabs " + strconv.Quote(prev) + " and " + strconv.Quote(name) + " are both the " + byKey[key].Label + " list. Keep one."}
		}
		matched[key] = name
		reader, err := wb.Open(sheetNo)
		if err != nil {
			return domain.ImportBundle{}, &Error{Code: "invalid_file", HTTPStatus: 400, Message: name + ": that tab could not be read."}
		}
		header, hasRows, err := readHeaderAndPeek(reader)
		_ = reader.Close()
		if err != nil {
			return domain.ImportBundle{}, &Error{Code: "invalid_file", HTTPStatus: 400, Message: name + ": that tab could not be read."}
		}
		if len(header) == 0 || !hasRows {
			continue // an empty template tab
		}
		keys, err := matchSheetHeader(byKey[key], header, name)
		if err != nil {
			return domain.ImportBundle{}, err
		}
		tabs = append(tabs, workbookTab{sheet: sheetNo, name: name, register: byKey[key], keys: keys})
	}
	if len(tabs) == 0 {
		return domain.ImportBundle{}, BadRequest("invalid_file", "No tab in the workbook has rows under a header the lists recognise. Download the template to see the expected tabs.")
	}
	sort.SliceStable(tabs, func(a, b int) bool { return order[tabs[a].register.Key] < order[tabs[b].register.Key] })
	bundle := domain.ImportBundle{ID: randomID(), FileName: strings.TrimSpace(fileName), Status: domain.ImportValidating, UnknownSheets: unknown, CreatedBy: w.ActorID}
	if err := i.jobs.CreateImportBundle(ctx, bundle, w.TenantID); err != nil {
		return domain.ImportBundle{}, err
	}
	for n, tab := range tabs {
		status := domain.ImportJobQueued
		if n == 0 {
			status = domain.ImportValidating
		}
		job := domain.ImportJob{ID: randomID(), Register: tab.register.Key, FileName: bundle.FileName, Format: format, Status: status, CreatedBy: w.ActorID,
			BundleID: bundle.ID, BundleOrder: n, SheetName: tab.name}
		if err := i.jobs.CreateImportJob(ctx, job, w.TenantID); err != nil {
			return domain.ImportBundle{}, err
		}
		reader, err := wb.Open(tab.sheet)
		if err != nil {
			return domain.ImportBundle{}, err
		}
		if _, err := reader.Next(); err != nil { // the header, already checked
			_ = reader.Close()
			return domain.ImportBundle{}, BadRequest("invalid_file", tab.name+": that tab could not be read.")
		}
		_, err = i.stageRows(ctx, w.TenantID, job.ID, &xlsxReader{wb: reader}, tab.keys, tab.name)
		if err != nil {
			i.failBundle(ctx, w.TenantID, bundle.ID, HTTPError(err).Message)
			return domain.ImportBundle{}, err
		}
	}
	staged, err := i.jobs.GetImportBundle(ctx, w.TenantID, bundle.ID)
	if err != nil {
		return domain.ImportBundle{}, err
	}
	i.KickBundle(w.TenantID, bundle.ID)
	return staged, nil
}

// readHeaderAndPeek reads a tab's header and reports whether any non-blank line follows it.
func readHeaderAndPeek(reader *xlsxWorkbook) ([]string, bool, error) {
	header, err := reader.Next()
	if errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if domain.IsBlankRecord(header) {
		return nil, false, nil
	}
	for {
		record, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return header, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if !domain.IsBlankRecord(record) {
			return header, true, nil
		}
	}
}

func (i *Importer) fail(ctx context.Context, tenantID, jobID, msg string) {
	status := domain.ImportFailed
	if _, err := i.jobs.PatchImportJob(ctx, tenantID, jobID, ports.ImportJobPatch{Status: &status, Error: &msg, Finished: true, Release: true}); err != nil {
		i.log.Warn("configuration import: mark failed", "job_id", jobID, "error", err)
	}
}

// failBundle stops a workbook: the bundle is marked failed with the reason, and every tab that
// has not finished is cancelled (its unwritten rows skipped) so nothing after the failure runs.
func (i *Importer) failBundle(ctx context.Context, tenantID, bundleID, msg string) {
	if _, ok, err := i.jobs.CancelImportBundle(ctx, tenantID, bundleID); err != nil || !ok {
		if err != nil {
			i.log.Warn("configuration import: cancel bundle tabs", "bundle_id", bundleID, "error", err)
		}
		return
	}
	status := domain.ImportFailed
	if _, err := i.jobs.PatchImportBundle(ctx, tenantID, bundleID, ports.ImportBundlePatch{Status: &status, Error: &msg, Finished: true}); err != nil {
		i.log.Warn("configuration import: mark bundle failed", "bundle_id", bundleID, "error", err)
	}
}

// Kick runs Process in the background, bounded by the semaphore; a kick that finds it full
// returns at once and the kernel sweep finishes the job.
func (i *Importer) Kick(tenantID, jobID string) {
	i.kick(jobID, func(ctx context.Context) error { return i.Process(ctx, tenantID, jobID) })
}

// KickBundle runs ProcessBundle in the background under the same bound.
func (i *Importer) KickBundle(tenantID, bundleID string) {
	i.kick(bundleID, func(ctx context.Context) error { return i.ProcessBundle(ctx, tenantID, bundleID) })
}

func (i *Importer) kick(id string, run func(ctx context.Context) error) {
	select {
	case i.sem <- struct{}{}:
	default:
		i.log.Info("configuration import: kick deferred to the worker sweep", "id", id)
		return
	}
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		defer func() { <-i.sem }()
		defer func() {
			if r := recover(); r != nil {
				i.log.Error("configuration import: panic", "id", id, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		if err := run(ctx); err != nil {
			i.log.Warn("configuration import: process", "id", id, "error", err)
		}
	}()
}

// Wait blocks until in-flight kicks finish (shutdown).
func (i *Importer) Wait() { i.wg.Wait() }

// ProcessDue finishes jobs whose claim lapsed, then every workbook still in flight: the kernel
// worker's sweep. A workbook tab whose claim lapsed is finished by the first pass; the second
// pass then moves the workbook on to its next tab.
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
	bundles, err := i.jobs.DueImportBundleIDs(ctx, tenantID, limit)
	if err != nil {
		return done, err
	}
	for _, id := range bundles {
		if err := i.ProcessBundle(ctx, tenantID, id); err != nil {
			i.log.Warn("configuration import: bundle sweep", "bundle_id", id, "error", err)
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
	var bctx *bundleContext
	if job.BundleID != "" {
		bundle, err := i.jobs.GetImportBundle(ctx, tenantID, job.BundleID)
		if err != nil {
			i.fail(ctx, tenantID, jobID, "The workbook this tab belongs to no longer exists.")
			return err
		}
		bctx = newBundleContext(bundle)
	}
	switch job.Status {
	case domain.ImportValidating:
		err = i.validate(ctx, tenantID, job, reg, bctx)
	case domain.ImportApplying, domain.ImportCancelled:
		err = i.apply(ctx, tenantID, job, reg, bctx)
	default:
		return nil
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		i.fail(ctx, tenantID, jobID, "The import stopped: "+HTTPError(err).Message)
	}
	return err
}

// ProcessBundle moves a workbook along: in order, each tab is promoted to the bundle's phase
// when its turn comes and run to the end of that phase. It is safe to call from anywhere at any
// time -- every step is fenced on the status it expects and every tab is claimed before it is
// worked -- so the API's kick and the worker's sweep may both call it for the same workbook.
func (i *Importer) ProcessBundle(ctx context.Context, tenantID, bundleID string) error {
	bundle, err := i.jobs.GetImportBundle(ctx, tenantID, bundleID)
	if err != nil {
		return err
	}
	var from, to, done string
	switch bundle.Status {
	case domain.ImportValidating:
		from, to, done = domain.ImportJobQueued, domain.ImportValidating, domain.ImportPreviewed
	case domain.ImportApplying:
		from, to, done = domain.ImportPreviewed, domain.ImportApplying, domain.ImportApplied
	default:
		return nil
	}
	for _, job := range bundle.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch job.Status {
		case done:
			continue
		case domain.ImportFailed:
			i.failBundle(ctx, tenantID, bundleID, tabLabel(job)+": "+job.Error)
			return nil
		case domain.ImportCancelled:
			// A tab cancelled on its own takes the workbook with it: what follows may depend on it.
			if _, _, err := i.jobs.CancelImportBundle(ctx, tenantID, bundleID); err != nil {
				return err
			}
			return nil
		case from:
			promoted, ok, err := i.jobs.PromoteImportJob(ctx, tenantID, job.ID, from, to)
			if err != nil {
				return err
			}
			if !ok {
				return nil // someone else moved it; they are driving
			}
			job = promoted
			fallthrough
		case to:
			if err := i.Process(ctx, tenantID, job.ID); err != nil {
				return err
			}
			current, err := i.jobs.GetImportJob(ctx, tenantID, job.ID)
			if err != nil {
				return err
			}
			switch current.Status {
			case done:
				continue
			case domain.ImportFailed:
				i.failBundle(ctx, tenantID, bundleID, tabLabel(current)+": "+current.Error)
				return nil
			default:
				// Still in flight (the run timed out or another worker holds it): a later sweep continues.
				return nil
			}
		default:
			// A tab in a phase the bundle is not in (an apply request racing a preview): leave it.
			return nil
		}
	}
	status := done
	patch := ports.ImportBundlePatch{FromStatus: &bundle.Status, Status: &status, Finished: done == domain.ImportApplied}
	if _, err := i.jobs.PatchImportBundle(ctx, tenantID, bundleID, patch); err != nil {
		return phaseEnded(err)
	}
	return nil
}

// selfReferencing reports a register whose rows may name rows of the same sheet (a list under
// a list): those are written in row order so the parent's id is in hand for the child.
func selfReferencing(reg domain.Register) bool {
	for _, c := range reg.Columns {
		if c.Type == domain.TypeRef && c.Ref == reg.Key {
			return true
		}
	}
	return false
}

func tabLabel(job domain.ImportJob) string {
	if job.SheetName != "" {
		return job.SheetName
	}
	return job.Register
}

// bundleContext is what a tab's validation and apply know about the workbook around it: the
// sibling job per register, so a ref may name a row a sibling tab creates.
type bundleContext struct {
	bundle     domain.ImportBundle
	byRegister map[string]domain.ImportJob
	byID       map[string]domain.ImportJob
}

func newBundleContext(bundle domain.ImportBundle) *bundleContext {
	b := &bundleContext{bundle: bundle, byRegister: map[string]domain.ImportJob{}, byID: map[string]domain.ImportJob{}}
	for _, job := range bundle.Jobs {
		b.byRegister[job.Register] = job
		b.byID[job.ID] = job
	}
	return b
}

// sibling is the workbook's job for a register, if the workbook carries that tab.
func (b *bundleContext) sibling(register string) (domain.ImportJob, bool) {
	if b == nil {
		return domain.ImportJob{}, false
	}
	job, ok := b.byRegister[register]
	return job, ok
}

// walkValidRows visits a job's validated rows (state valid; also applying/applied, which were
// valid) in row order, one chunk at a time.
func (i *Importer) walkValidRows(ctx context.Context, tenantID, jobID string, visit func(domain.ImportRow)) error {
	after := 0
	for {
		// scale-guard:ignore: keyset page walk over one tab's validated rows; after advances each iteration and the loop returns on a short page
		rows, err := i.jobs.ImportRowsAfter(ctx, tenantID, jobID, []string{domain.ImportRowValid, domain.ImportRowApplying, domain.ImportRowApplied}, after, processChunk)
		if err != nil {
			return err
		}
		for _, row := range rows {
			visit(row)
			after = row.RowNo
		}
		if len(rows) < processChunk {
			return nil
		}
	}
}

// rowValidator validates one staged row of a register into the fields apply will write.
type rowValidator func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError)

func (i *Importer) validate(ctx context.Context, tenantID string, job domain.ImportJob, reg domain.Register, bctx *bundleContext) error {
	var validateChunk func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error)
	if reg.Key == domain.RegAnimals {
		v, err := i.animalValidator(ctx, tenantID, false, job, bctx)
		if err != nil {
			return err
		}
		validateChunk = v
	} else {
		v, err := i.registerValidator(ctx, tenantID, reg, job, bctx)
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
		validUpdates, invalidUpdates := splitValidationUpdates(updates)
		validMoved, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowStaged, validUpdates)
		if err != nil {
			return err
		}
		invalidMoved, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowStaged, invalidUpdates)
		if err != nil {
			return err
		}
		moved := validMoved + invalidMoved
		if moved == 0 {
			return nil
		}
		after = rows[len(rows)-1].RowNo
		patch := ports.ImportJobPatch{AddValid: validMoved, AddInvalid: invalidMoved, ProgressRowNo: &after}
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

func splitValidationUpdates(updates []ports.ImportRowUpdate) ([]ports.ImportRowUpdate, []ports.ImportRowUpdate) {
	valid := make([]ports.ImportRowUpdate, 0, len(updates))
	invalid := make([]ports.ImportRowUpdate, 0, len(updates))
	for _, u := range updates {
		if u.State == domain.ImportRowValid {
			valid = append(valid, u)
			continue
		}
		invalid = append(invalid, u)
	}
	return valid, invalid
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

// refTarget is one ref column's resolution: the target's stored rows plus, in a workbook, the
// rows the target's own tab will create, scoped by the parent column both registers share.
type refTarget struct {
	column    domain.Column
	parentKey string // the column of THIS register that scopes the lookup ("" = unscoped)
	index     domain.ScopedRefIndex
	self      bool // the target is this register: rows validated earlier on this sheet join it
}

// registerValidator builds the per-row validator of an ordinary register: header keys are
// already column keys; refs resolve through the target's active options, loaded once, and --
// for a workbook tab -- through the sibling tab's validated rows as tokens.
func (i *Importer) registerValidator(ctx context.Context, tenantID string, reg domain.Register, job domain.ImportJob, bctx *bundleContext) (rowValidator, error) {
	refs := map[string]*refTarget{}
	var refOrder []string // parents before the columns they scope
	for _, c := range reg.Columns {
		if c.Type != domain.TypeRef {
			continue
		}
		target, ok := domain.RegisterByKey(c.Ref)
		if !ok {
			return nil, domain.ErrUnknownRegister
		}
		parentKey := domain.ParentColumn(reg, target, c.Key)
		targetParentKey := ""
		if parentKey != "" {
			targetParentKey = parentKey // the same column key on the target (pens.park_id)
		}
		idx, err := i.scopedRefIndex(ctx, tenantID, c.Ref, targetParentKey)
		if err != nil {
			return nil, err
		}
		rt := &refTarget{column: c, parentKey: parentKey, index: idx, self: c.Ref == reg.Key}
		if sibling, ok := bctx.sibling(c.Ref); ok && sibling.ID != job.ID {
			if err := i.addPendingRefs(ctx, tenantID, sibling, target, targetParentKey, idx); err != nil {
				return nil, err
			}
		}
		if rt.self && job.ProgressRowNo > 0 {
			// Resuming mid-sheet: the rows validated before the restart are pending refs too.
			if err := i.addPendingRefs(ctx, tenantID, job, target, targetParentKey, idx); err != nil {
				return nil, err
			}
		}
		refs[c.Key] = rt
		if parentKey == "" {
			refOrder = append([]string{c.Key}, refOrder...)
		} else {
			refOrder = append(refOrder, c.Key)
		}
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
	kinds := &bundleKinds{cache: map[string]string{}}
	if job.ProgressRowNo > 0 {
		// Resuming mid-sheet: the rows validated before the restart still count for the in-sheet
		// duplicate checks, or a name repeated across the restart boundary would slip through.
		if err := i.walkValidRows(ctx, tenantID, job.ID, func(row domain.ImportRow) {
			if row.RowNo > job.ProgressRowNo {
				return
			}
			if id := domain.FieldString(row.Fields, domain.SheetColumnID); id != "" {
				seenIDs[id] = row.RowNo
				return
			}
			if nameKey != "" {
				name := strings.ToLower(strings.TrimSpace(domain.FieldString(row.Fields, nameKey)))
				seenNames[domain.FieldString(row.Fields, domain.NameScope[reg.Key])+"\x1f"+name] = row.RowNo
			}
			if code := strings.TrimSpace(domain.FieldString(row.Fields, "code")); code != "" {
				seenCodes[code] = row.RowNo
			}
			if reg.Key == domain.RegCategories {
				kinds.remember(domain.BundleRowToken(job.ID, row.RowNo), row.Fields)
			}
		}); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError) {
		raw := make(map[string]any, len(row.Fields))
		for k, v := range row.Fields {
			raw[k] = v
		}
		id := strings.TrimSpace(fmt.Sprint(raw[domain.SheetColumnID]))
		if raw[domain.SheetColumnID] == nil {
			id = ""
		}
		rowVersion := strings.TrimSpace(fmt.Sprint(raw[domain.SheetColumnRowVersion]))
		delete(raw, domain.SheetColumnID)
		delete(raw, domain.SheetColumnRowVersion)
		delete(raw, domain.SheetColumnStatus)
		for _, c := range reg.Columns {
			if c.Derived {
				delete(raw, c.Key)
			}
		}
		var errs []domain.FieldError
		for _, key := range refOrder {
			rt := refs[key]
			v, ok := raw[key]
			if !ok || v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
				continue
			}
			parent := ""
			if rt.parentKey != "" {
				parent = strings.TrimSpace(fmt.Sprint(raw[rt.parentKey]))
				if raw[rt.parentKey] == nil {
					parent = ""
				}
			}
			resolved, msg, ok := rt.index.Resolve(parent, fmt.Sprint(v))
			if !ok {
				errs = append(errs, domain.FieldError{Field: key, Code: "unknown", Message: rt.column.Label + ": " + msg})
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
		kind, err := i.kindFor(ctx, tenantID, reg, raw, existing, bctx, kinds)
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
				// Unique within the scope the store enforces (a pen per park), never across it.
				scoped := domain.FieldString(clean, domain.NameScope[reg.Key]) + "\x1f" + name
				if prev, dup := seenNames[scoped]; dup && name != "" {
					col, _ := reg.Column(nameKey)
					return nil, []domain.FieldError{{Field: nameKey, Code: "duplicate", Message: col.Label + " is repeated on row " + strconv.Itoa(prev) + "."}}
				}
				seenNames[scoped] = row.RowNo
			}
			if code := strings.TrimSpace(domain.FieldString(clean, "code")); code != "" {
				if prev, dup := seenCodes[code]; dup {
					return nil, []domain.FieldError{{Field: "code", Code: "duplicate", Message: "Code is repeated on row " + strconv.Itoa(prev) + "."}}
				}
				seenCodes[code] = row.RowNo
			}
			// A row this sheet will create is a ref the rows below it may name (a list under a
			// list); it also carries its kind for the items that name it.
			token := domain.BundleRowToken(job.ID, row.RowNo)
			for _, rt := range refs {
				if rt.self {
					addPendingRow(rt.index, token, clean, rt.parentKey)
				}
			}
			if reg.Key == domain.RegCategories {
				kinds.remember(token, clean)
			}
		}
		if id != "" {
			clean[domain.SheetColumnID] = id
			if rowVersion == "" {
				return nil, []domain.FieldError{{Field: domain.SheetColumnRowVersion, Code: "required", Message: "Row version is required for updates. Download the latest sheet and try again."}}
			}
			if n, err := strconv.Atoi(rowVersion); err != nil || n < 1 {
				return nil, []domain.FieldError{{Field: domain.SheetColumnRowVersion, Code: "invalid", Message: "Row version must be the positive number from the downloaded sheet."}}
			}
			clean[domain.SheetColumnRowVersion] = rowVersion
		}
		return clean, nil
	}, nil
}

// addPendingRefs indexes a sibling tab's validated rows as tokens under their name/label and
// code, scoped by parentKey when the target has one.
func (i *Importer) addPendingRefs(ctx context.Context, tenantID string, sibling domain.ImportJob, target domain.Register, parentKey string, idx domain.ScopedRefIndex) error {
	return i.walkValidRows(ctx, tenantID, sibling.ID, func(row domain.ImportRow) {
		if domain.FieldString(row.Fields, domain.SheetColumnID) != "" {
			return // an update names a stored row, which the stored index already carries
		}
		addPendingRow(idx, domain.BundleRowToken(sibling.ID, row.RowNo), row.Fields, parentKey)
	})
}

func addPendingRow(idx domain.ScopedRefIndex, token string, fields map[string]any, parentKey string) {
	parent := ""
	if parentKey != "" {
		parent = domain.FieldString(fields, parentKey)
	}
	for _, key := range []string{"name", "label"} {
		if label := strings.TrimSpace(domain.FieldString(fields, key)); label != "" {
			idx.AddPending(token, label, parent)
			break
		}
	}
	if code := strings.TrimSpace(domain.FieldString(fields, "code")); code != "" {
		idx.AddPending(token, code, parent)
	}
}

// bundleKinds resolves the item kind behind a category that only exists on the workbook's
// Lists tab: its own kind, or the kind of the list above it, walked up to the root.
type bundleKinds struct {
	cache map[string]string // token -> kind ("" = unknown)
	rows  map[string]map[string]any
}

func (k *bundleKinds) remember(token string, fields map[string]any) {
	if k.rows == nil {
		k.rows = map[string]map[string]any{}
	}
	k.rows[token] = fields
}

// kindFor is the service's kindFor with the workbook's pending categories in front of it: a
// category token resolves through the Lists tab, a stored id through the store.
func (i *Importer) kindFor(ctx context.Context, tenantID string, reg domain.Register, raw map[string]any, existing *domain.Row, bctx *bundleContext, kinds *bundleKinds) (string, error) {
	if reg.Key != domain.RegItems {
		return "", nil
	}
	categoryID := domain.FieldString(raw, "category_id")
	if _, _, isToken := domain.ParseBundleRowToken(categoryID); !isToken {
		return i.svc.kindFor(ctx, tenantID, reg, raw, existing)
	}
	kind, err := i.pendingKind(ctx, tenantID, categoryID, bctx, kinds, 0)
	if err != nil {
		return "", err
	}
	if kind == "" {
		return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "category_id", Code: "unknown", Message: "The list this item is under has no kind; give its top-level list a kind on the Lists tab."}}}
	}
	return kind, nil
}

func (i *Importer) pendingKind(ctx context.Context, tenantID, ref string, bctx *bundleContext, kinds *bundleKinds, depth int) (string, error) {
	if depth > bundleKindDepth {
		return "", nil
	}
	jobID, rowNo, isToken := domain.ParseBundleRowToken(ref)
	if !isToken {
		// A stored category: its root's kind, through the store.
		cat, err := i.svc.Get(ctx, tenantID, domain.RegCategories, ref)
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				return "", nil
			}
			return "", err
		}
		return domain.FieldString(cat.Fields, "kind"), nil
	}
	if kind, ok := kinds.cache[ref]; ok {
		return kind, nil
	}
	fields, ok := kinds.rows[ref]
	if !ok {
		// A row of the Lists tab validated before this tab (or before a restart): read it back.
		if bctx == nil || bctx.byID[jobID].ID == "" {
			return "", nil
		}
		rows, err := i.jobs.ImportRows(ctx, tenantID, jobID, ports.ImportRowsParams{AfterRowNo: rowNo - 1, Limit: 1})
		if err != nil {
			return "", err
		}
		if len(rows) == 0 || rows[0].RowNo != rowNo {
			return "", nil
		}
		fields = rows[0].Fields
	}
	kind := strings.TrimSpace(domain.FieldString(fields, "kind"))
	if kind == "" {
		if parent := strings.TrimSpace(domain.FieldString(fields, "parent_id")); parent != "" {
			k, err := i.pendingKind(ctx, tenantID, parent, bctx, kinds, depth+1)
			if err != nil {
				return "", err
			}
			kind = k
		}
	}
	kinds.cache[ref] = kind
	return kind, nil
}

func (i *Importer) apply(ctx context.Context, tenantID string, job domain.ImportJob, reg domain.Register, bctx *bundleContext) error {
	var applyRows func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error)
	if reg.Key == domain.RegAnimals {
		v, err := i.animalValidator(ctx, tenantID, true, job, bctx)
		if err != nil {
			return err
		}
		applyRows = v
	} else {
		applyRows = perRow(func(ctx context.Context, row domain.ImportRow) (map[string]any, []domain.FieldError) {
			fields := make(map[string]any, len(row.Fields))
			for k, v := range row.Fields {
				fields[k] = v
			}
			id := ""
			if v, ok := fields[domain.SheetColumnID]; ok && v != nil {
				id = fmt.Sprint(v)
			}
			rowVersion := 0
			if v, ok := fields[domain.SheetColumnRowVersion]; ok && v != nil {
				rowVersion, _ = strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
			}
			delete(fields, domain.SheetColumnID)
			delete(fields, domain.SheetColumnRowVersion)
			w := ports.WriteParams{TenantID: tenantID, ActorID: job.CreatedBy, IdempotencyKey: importRowKey(job.ID, row.RowNo), TraceID: job.ID}
			var (
				written domain.Row
				err     error
			)
			if id != "" {
				written, err = i.svc.Update(ctx, w, reg.Key, id, fields, rowVersion)
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
		rows, err := i.jobs.ImportRowsAfter(ctx, tenantID, job.ID, states, after, applyChunk)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		// One claim renewal per chunk; a job cancelled meanwhile is no longer workable.
		if _, ok, err := i.jobs.ClaimImportJob(ctx, tenantID, job.ID, i.worker, claimLease); err != nil || !ok {
			return err
		}
		// Claim the chunk's valid rows in one statement; a cancel that landed keeps its skipped
		// rows, and the rows that did move are finished before the loop stops.
		var toClaim []int
		for _, row := range rows {
			if row.State == domain.ImportRowValid {
				toClaim = append(toClaim, row.RowNo)
			}
		}
		moved, err := i.jobs.ClaimImportRows(ctx, tenantID, job.ID, toClaim)
		if err != nil {
			return err
		}
		claimed := make(map[int]bool, len(moved))
		for _, n := range moved {
			claimed[n] = true
		}
		work := make([]domain.ImportRow, 0, len(rows))
		for _, row := range rows {
			if row.State == domain.ImportRowApplying || claimed[row.RowNo] {
				work = append(work, row)
			}
		}
		cancelled := len(moved) < len(toClaim)
		if len(work) == 0 {
			return nil
		}
		// A workbook tab: refs that name sibling rows become the ids those rows wrote, or the
		// row fails naming the sheet and row that was not added. A row may also name a row of
		// THIS tab written moments ago (a list under a list), so ordinary registers are written
		// one row at a time with the chunk's own results in hand; the animals tab has no such
		// self reference and goes to the herd register as a chunk.
		updates := make([]ports.ImportRowUpdate, 0, len(work))
		local := map[int]ports.ImportRowResult{}
		finish := func(u ports.ImportRowUpdate) {
			if u.State == domain.ImportRowValid {
				u.State = domain.ImportRowApplied
				if u.Fields != nil {
					u.ResultID = domain.FieldString(u.Fields, "__result_id")
					u.Fields = nil
				}
				local[u.RowNo] = ports.ImportRowResult{RowNo: u.RowNo, State: domain.ImportRowApplied, ResultID: u.ResultID}
			} else {
				u.State = domain.ImportRowFailed
				u.Fields = nil
				local[u.RowNo] = ports.ImportRowResult{RowNo: u.RowNo, State: domain.ImportRowFailed}
			}
			updates = append(updates, u)
		}
		if reg.Key == domain.RegAnimals {
			refFailures, err := i.resolveBundleTokens(ctx, tenantID, reg, job.ID, work, bctx, local)
			if err != nil {
				return err
			}
			runnable := make([]domain.ImportRow, 0, len(work))
			for _, row := range work {
				if errs := refFailures[row.RowNo]; len(errs) > 0 {
					finish(ports.ImportRowUpdate{RowNo: row.RowNo, State: domain.ImportRowFailed, Errors: errs})
					continue
				}
				runnable = append(runnable, row)
			}
			if len(runnable) > 0 {
				applied, err := applyRows(ctx, runnable)
				if err != nil {
					return err
				}
				if len(applied) != len(runnable) {
					return fmt.Errorf("configuration import: apply returned %d updates for %d rows", len(applied), len(runnable))
				}
				for _, u := range applied {
					finish(u)
				}
			}
		} else if selfReferencing(reg) {
			for _, row := range work {
				refFailures, err := i.resolveBundleTokens(ctx, tenantID, reg, job.ID, []domain.ImportRow{row}, bctx, local)
				if err != nil {
					return err
				}
				if errs := refFailures[row.RowNo]; len(errs) > 0 {
					finish(ports.ImportRowUpdate{RowNo: row.RowNo, State: domain.ImportRowFailed, Errors: errs})
					continue
				}
				applied, err := applyRows(ctx, []domain.ImportRow{row})
				if err != nil {
					return err
				}
				if len(applied) != 1 {
					return fmt.Errorf("configuration import: apply returned %d updates for row %d", len(applied), row.RowNo)
				}
				finish(applied[0])
			}
		} else {
			// Independent rows: written applyWorkers at a time, outcomes filed in row order.
			results := make([]ports.ImportRowUpdate, len(work))
			errs := make([]error, len(work))
			sem := make(chan struct{}, applyWorkers)
			var wg sync.WaitGroup
			for n, row := range work {
				sem <- struct{}{}
				wg.Add(1)
				go func(n int, row domain.ImportRow) {
					defer wg.Done()
					defer func() { <-sem }()
					refFailures, err := i.resolveBundleTokens(ctx, tenantID, reg, job.ID, []domain.ImportRow{row}, bctx, map[int]ports.ImportRowResult{})
					if err != nil {
						errs[n] = err
						return
					}
					if fieldErrs := refFailures[row.RowNo]; len(fieldErrs) > 0 {
						results[n] = ports.ImportRowUpdate{RowNo: row.RowNo, State: domain.ImportRowFailed, Errors: fieldErrs}
						return
					}
					applied, err := applyRows(ctx, []domain.ImportRow{row})
					if err != nil {
						errs[n] = err
						return
					}
					if len(applied) != 1 {
						errs[n] = fmt.Errorf("configuration import: apply returned %d updates for row %d", len(applied), row.RowNo)
						return
					}
					results[n] = applied[0]
				}(n, row)
			}
			wg.Wait()
			for n := range work {
				if errs[n] != nil {
					// Record what did finish before stopping; the rows that did not stay applying
					// and replay by key on the next run.
					if _, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowApplying, updates); err != nil {
						return err
					}
					return errs[n]
				}
				finish(results[n])
			}
		}
		patch := ports.ImportJobPatch{}
		for _, u := range updates {
			if u.State == domain.ImportRowApplied {
				patch.AddApplied++
			} else {
				patch.AddFailed++
			}
		}
		if _, err := i.jobs.UpdateImportRows(ctx, tenantID, job.ID, domain.ImportRowApplying, updates); err != nil {
			return err
		}
		after = rows[len(rows)-1].RowNo
		patch.ProgressRowNo = &after
		if _, err := i.jobs.PatchImportJob(ctx, tenantID, job.ID, patch); err != nil {
			return phaseEnded(err)
		}
		if cancelled {
			return nil
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

// resolveBundleTokens replaces, in place, every field that names a sibling tab's row with the
// id that row wrote. A row whose parent was not written (invalid, failed, skipped, or not yet
// applied) is reported per field, naming the sheet and row, so the child is never written.
func (i *Importer) resolveBundleTokens(ctx context.Context, tenantID string, reg domain.Register, ownJobID string, rows []domain.ImportRow, bctx *bundleContext, local map[int]ports.ImportRowResult) (map[int][]domain.FieldError, error) {
	failures := map[int][]domain.FieldError{}
	if bctx == nil {
		return failures, nil
	}
	// Collect the sibling rows named, per sibling job.
	wanted := map[string]map[int]bool{}
	for _, row := range rows {
		for _, v := range row.Fields {
			str, ok := v.(string)
			if !ok {
				continue
			}
			if jobID, rowNo, isToken := domain.ParseBundleRowToken(str); isToken {
				if wanted[jobID] == nil {
					wanted[jobID] = map[int]bool{}
				}
				wanted[jobID][rowNo] = true
			}
		}
	}
	if len(wanted) == 0 {
		return failures, nil
	}
	results := map[string]map[int]ports.ImportRowResult{}
	for sibling, nos := range wanted {
		list := make([]int, 0, len(nos))
		for n := range nos {
			if sibling == ownJobID {
				if res, ok := local[n]; ok {
					if results[sibling] == nil {
						results[sibling] = map[int]ports.ImportRowResult{}
					}
					results[sibling][n] = res
					continue // written in this very run; the store may not carry it yet
				}
			}
			list = append(list, n)
		}
		if len(list) == 0 {
			continue
		}
		got, err := i.jobs.ImportRowResults(ctx, tenantID, sibling, list)
		if err != nil {
			return nil, err
		}
		if results[sibling] == nil {
			results[sibling] = map[int]ports.ImportRowResult{}
		}
		for n, res := range got {
			results[sibling][n] = res
		}
	}
	for _, row := range rows {
		for key, v := range row.Fields {
			str, ok := v.(string)
			if !ok {
				continue
			}
			jobID, rowNo, isToken := domain.ParseBundleRowToken(str)
			if !isToken {
				continue
			}
			res, found := results[jobID][rowNo]
			if found && res.State == domain.ImportRowApplied && res.ResultID != "" {
				row.Fields[key] = res.ResultID
				continue
			}
			label := key
			if col, ok := reg.Column(key); ok {
				label = col.Label
			}
			sheet := jobID
			if sibling, ok := bctx.byID[jobID]; ok {
				sheet = tabLabel(sibling)
			}
			if jobID == ownJobID {
				sheet = "same"
			}
			failures[row.RowNo] = append(failures[row.RowNo], domain.FieldError{Field: key, Code: "parent_not_added",
				Message: label + ": row " + strconv.Itoa(rowNo) + " of the " + sheet + " tab was not added, so this row was not either."})
		}
	}
	return failures, nil
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

// nilUUID stands in for a park or pen that only exists on a sibling tab when the herd register
// previews an animals row: a well-formed id it will not find, whose not-found is set aside.
const nilUUID = "00000000-0000-0000-0000-000000000000"

// animalPending is what a row of the animals tab takes from sibling tabs rather than the store.
type animalPending struct {
	park, pen, species, sex, stage bool
}

func (p animalPending) any() bool { return p.park || p.pen || p.species || p.sex || p.stage }

// setAside reports whether an identity error is about a fact a sibling tab will supply, which
// the preview cannot check yet and the apply checks for real.
func (p animalPending) setAside(field string) bool {
	switch field {
	case "park_id", "park_code", "park":
		return p.park || p.pen
	case "shed_id", "shed_code", "pen_name", "partition_label":
		return p.park || p.pen
	case "species":
		return p.species
	case "sex":
		return p.sex
	case "management_stage":
		return p.stage
	}
	return false
}

// animalValidator builds the chunk function for the animals register: resolve park and pen to
// ids from the places registers (by code or name), then hand the chunk to identity's preview
// (validate) or preview + commit (apply). Identity reports per row; rows map back by line.
//
// In a workbook, park, pen, species, gender and stage may be rows of sibling tabs. At preview
// those resolve to tokens (park, pen) or to the codes the tabs will create (species, gender,
// stage); the herd register is still asked about everything else -- tags, dates, duplicates --
// and its not-found for the pending facts is set aside. At apply the tokens have become ids
// and the codes exist, so the same checks run in full.
func (i *Importer) animalValidator(ctx context.Context, tenantID string, commit bool, job domain.ImportJob, bctx *bundleContext) (func(ctx context.Context, rows []domain.ImportRow) ([]ports.ImportRowUpdate, error), error) {
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
	// The whole-sheet tag index (review finding 2026-09-19): the herd register checks a tag
	// against the register and within the chunk it is handed, so a tag repeated at row 10 and
	// row 70,000 would preview as two valid animals and the second would fail only at apply. The
	// index is one entry per tag on the sheet -- bounded by the tab -- and is rebuilt from the
	// rows already validated when a restart resumes mid-sheet.
	seenTags := map[string]int{}
	if !commit && job.ProgressRowNo > 0 {
		if err := i.walkValidRows(ctx, tenantID, job.ID, func(row domain.ImportRow) {
			if row.RowNo <= job.ProgressRowNo {
				rememberTags(seenTags, row)
			}
		}); err != nil {
			return nil, err
		}
	}
	pending := map[string]bool{} // token or pending code -> true
	if bctx != nil && !commit {
		if sibling, ok := bctx.sibling(domain.RegParks); ok {
			if err := i.walkValidRows(ctx, tenantID, sibling.ID, func(row domain.ImportRow) {
				token := domain.BundleRowToken(sibling.ID, row.RowNo)
				for _, k := range []string{"name", "code"} {
					if v := strings.ToLower(strings.TrimSpace(domain.FieldString(row.Fields, k))); v != "" {
						parks.addPending("", v, token)
					}
				}
				pending[token] = true
			}); err != nil {
				return nil, err
			}
		}
		if sibling, ok := bctx.sibling(domain.RegPens); ok {
			if err := i.walkValidRows(ctx, tenantID, sibling.ID, func(row domain.ImportRow) {
				token := domain.BundleRowToken(sibling.ID, row.RowNo)
				parent := domain.FieldString(row.Fields, "park_id")
				if v := strings.ToLower(strings.TrimSpace(domain.FieldString(row.Fields, "name"))); v != "" {
					pens.addPending(parent, v, token)
				}
				pending[token] = true
			}); err != nil {
				return nil, err
			}
		}
		for _, reg := range []string{domain.RegSpecies, domain.RegSexes, domain.RegStages} {
			sibling, ok := bctx.sibling(reg)
			if !ok {
				continue
			}
			key := map[string]string{domain.RegSpecies: "species", domain.RegSexes: "sex", domain.RegStages: "management_stage"}[reg]
			if err := i.walkValidRows(ctx, tenantID, sibling.ID, func(row domain.ImportRow) {
				code := strings.TrimSpace(domain.FieldString(row.Fields, "code"))
				if code == "" {
					return
				}
				if idx, ok := codes[key]; ok {
					idx.AddPending(code, domain.FieldString(row.Fields, "name"))
					idx.AddPending(code, code)
				}
				pending[key+":"+strings.ToLower(code)] = true
			}); err != nil {
				return nil, err
			}
		}
	}
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
			pend := make(map[int]animalPending, len(chunk))
			resolved := make(map[int]map[string]any, len(chunk))
			for k, row := range chunk {
				cells, fields, p, errs := animalCells(row.Fields, parks, pens, codes, pending)
				if len(errs) == 0 && !commit {
					errs = duplicateTagErrors(seenTags, row)
				}
				if len(errs) > 0 {
					pre[k] = errs
					cells = make([]string, len(animalSheetColumns)) // keeps the line count aligned
				}
				pend[k], resolved[k] = p, fields
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
					errs := r.Errors
					if p := pend[k]; p.any() {
						kept := errs[:0:0]
						for _, e := range errs {
							if !p.setAside(e.Field) {
								kept = append(kept, e)
							}
						}
						errs = kept
					}
					accepted := r.Decision == "create" || r.Decision == "update_reproductive"
					if len(errs) == 0 && !accepted && pend[k].any() && r.Decision == "requires_review" {
						accepted = true // review was owed only to the facts set aside
					}
					if len(errs) > 0 || !accepted {
						u.State = domain.ImportRowInvalid
						u.Errors = animalErrors(identitydomain.AdminGoatBulkRowResult{Decision: r.Decision, Errors: errs})
					} else if commit {
						if r.Result != nil {
							u.Fields = map[string]any{"__result_id": r.Result.Goat.GoatID}
						}
					} else if pend[k].any() {
						// Keep the tokens and codes so the apply resolves them, not the names.
						u.Fields = resolved[k]
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

// animalTagKeys are the sheet columns that carry an animal's tags.
var animalTagKeys = []string{"animal_identifier_1", "animal_identifier_2"}

// rememberTags files a row's tags in the whole-sheet index.
func rememberTags(seen map[string]int, row domain.ImportRow) {
	for _, key := range animalTagKeys {
		if tag := normalizeTag(domain.FieldString(row.Fields, key)); tag != "" {
			if _, dup := seen[tag]; !dup {
				seen[tag] = row.RowNo
			}
		}
	}
}

// duplicateTagErrors reports a tag the sheet already carries on an earlier row and, when the
// row is clean, files its tags for the rows after it.
func duplicateTagErrors(seen map[string]int, row domain.ImportRow) []domain.FieldError {
	var errs []domain.FieldError
	for _, key := range animalTagKeys {
		tag := normalizeTag(domain.FieldString(row.Fields, key))
		if tag == "" {
			continue
		}
		if prev, dup := seen[tag]; dup && prev != row.RowNo {
			errs = append(errs, domain.FieldError{Field: key, Code: "duplicate", Message: "This tag is already on row " + strconv.Itoa(prev) + " of this sheet."})
		}
	}
	if len(errs) == 0 {
		rememberTags(seen, row)
	}
	return errs
}

func normalizeTag(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
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
	scoped, err := i.scopedRefIndex(ctx, tenantID, register, "")
	if err != nil {
		return domain.RefIndex{}, err
	}
	return scoped.Flat(), nil
}

// scopedRefIndex is refIndex with each option filed under its parent (parentKey names the
// target's own ref column, such as pens' park_id) so a lookup can be narrowed to one parent.
func (i *Importer) scopedRefIndex(ctx context.Context, tenantID, register, parentKey string) (domain.ScopedRefIndex, error) {
	idx := domain.NewScopedRefIndex(nil, nil, nil)
	cursor := ""
	for {
		// scale-guard:ignore: keyset page walk over a bounded reference catalog; cursor advances each iteration and the loop returns on an empty NextCursor
		page, err := i.svc.List(ctx, tenantID, register, ports.ListParams{Status: domain.StatusActive, Cursor: cursor, Limit: MaxPageSize})
		if err != nil {
			return idx, err
		}
		for _, row := range page.Rows {
			parent := ""
			if parentKey != "" {
				parent = domain.FieldString(row.Fields, parentKey)
			}
			idx.Add(row.ID, row.Display, parent)
			if code := strings.TrimSpace(domain.FieldString(row.Fields, "code")); code != "" && !strings.EqualFold(code, row.Display) {
				idx.Add(row.ID, code, parent)
			}
		}
		if page.NextCursor == "" {
			return idx, nil
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

// addPending files a sibling tab's would-be place under its name; a name already resolving
// under the same parent is left to the stored row.
func (p placeLookup) addPending(parent, key, id string) {
	k := parent + "\x1f" + key
	if len(p.byKey[k]) > 0 {
		return
	}
	p.byID[id] = true
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

// animalCells resolves one animals row into identity's CSV line. In a workbook a park or pen
// may resolve to a sibling row's token, and a species, gender or stage to a code a sibling tab
// will create: those are reported in animalPending, the token replaced by nilUUID on the line,
// and the resolved fields returned so the row can keep the token for apply.
func animalCells(fields map[string]any, parks, pens placeLookup, codes map[string]domain.RefIndex, pending map[string]bool) ([]string, map[string]any, animalPending, []domain.FieldError) {
	var p animalPending
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
		return nil, nil, p, errs
	}
	speciesCode, sexCode, stageCode := get("species"), get("sex"), get("management_stage")
	p.park, p.pen = pending[parkID], pending[penID]
	p.species = pending["species:"+strings.ToLower(speciesCode)]
	p.sex = pending["sex:"+strings.ToLower(sexCode)]
	p.stage = pending["management_stage:"+strings.ToLower(stageCode)]
	lineParkID, linePenID := parkID, penID
	if p.park {
		lineParkID = nilUUID
	}
	if p.park || p.pen {
		linePenID = nilUUID
	}
	var resolved map[string]any
	if p.any() {
		resolved = make(map[string]any, len(fields))
		for k, v := range fields {
			resolved[k] = v
		}
		resolved["park"], resolved["pen_name"] = parkID, penID
		resolved["species"], resolved["sex"] = speciesCode, sexCode
	}
	return []string{get("animal_identifier_1"), get("animal_identifier_2"), speciesCode, get("breed"), sexCode, lineParkID, linePenID, get("partition_label"),
		stageCode, get("dob"), get("entry_date"), get("origin"), get("reproductive_status"), get("weight_kg")}, resolved, p, nil
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
