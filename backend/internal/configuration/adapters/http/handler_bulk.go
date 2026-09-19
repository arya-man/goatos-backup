package http

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/configuration/app"
	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Bulk download / upload routes (maintainer instruction 2026-09-18). Downloads STREAM: the
// register is walked one keyset page at a time straight into the response. Uploads STAGE: the
// file streams into the job's rows and the response is the job, which the screen polls while a
// processor validates it; Apply is a second, explicit call.

// maxUploadBytes bounds one upload (a 500k-row CSV, or a workbook of that many rows, is under it).
const maxUploadBytes = 64 << 20

// Bulk is the slice of the app the bulk routes need beside Service.
type Bulk interface {
	Export(ctx context.Context, tenantID, register, status, format string, out io.Writer) error
	Template(ctx context.Context, tenantID, register, format string, out io.Writer) error
	ErrorSheet(ctx context.Context, jobs ports.ImportRepository, tenantID, jobID, format string, out io.Writer) error
	// The onboarding workbook (2026-09-19): one Excel file, one tab per list.
	WorkbookTemplate(ctx context.Context, tenantID string, out io.Writer) error
	WorkbookExport(ctx context.Context, tenantID, status string, out io.Writer) error
	BundleErrorSheet(ctx context.Context, jobs ports.ImportRepository, tenantID, bundleID, format string, out io.Writer) error
}

// WithBulk mounts the sheet routes on a handler that has an importer.
func (h *Handler) WithBulk(bulk Bulk, importer *app.Importer, jobs ports.ImportRepository) *Handler {
	h.bulk, h.importer, h.jobs = bulk, importer, jobs
	return h
}

// RegisterBulk mounts the sheet routes. Literal segments (`imports`, `export`, `template`,
// `workbook`) are registered beside the {register} / {row_id} patterns; Go's mux prefers the
// literal, and no register is keyed `workbook` (pinned by a test).
func RegisterBulk(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/configuration/workbook/template", h.WorkbookTemplate)
	mux.HandleFunc("GET /admin/configuration/workbook/export", h.WorkbookExport)
	mux.HandleFunc("POST /admin/configuration/workbook/imports", h.UploadWorkbook)
	mux.HandleFunc("GET /admin/configuration/workbook/imports", h.ListWorkbookImports)
	mux.HandleFunc("GET /admin/configuration-import-bundles/{bundle_id}", h.GetBundle)
	mux.HandleFunc("GET /admin/configuration-import-bundles/{bundle_id}/errors", h.BundleErrors)
	mux.HandleFunc("POST /admin/configuration-import-bundles/{bundle_id}/apply", h.ApplyBundle)
	mux.HandleFunc("POST /admin/configuration-import-bundles/{bundle_id}/cancel", h.CancelBundle)
	mux.HandleFunc("GET /admin/configuration/{register}/export", h.Export)
	mux.HandleFunc("GET /admin/configuration/{register}/template", h.Template)
	mux.HandleFunc("POST /admin/configuration/{register}/imports", h.Upload)
	mux.HandleFunc("GET /admin/configuration/{register}/imports", h.ListImports)
	mux.HandleFunc("GET /admin/configuration-imports/{job_id}", h.GetImport)
	mux.HandleFunc("GET /admin/configuration-imports/{job_id}/rows", h.ImportRows)
	mux.HandleFunc("GET /admin/configuration-imports/{job_id}/errors", h.ImportErrors)
	mux.HandleFunc("POST /admin/configuration-imports/{job_id}/apply", h.ApplyImport)
	mux.HandleFunc("POST /admin/configuration-imports/{job_id}/cancel", h.CancelImport)
}

type importJobPayload struct {
	Job     domain.ImportJob `json:"job"`
	TraceID string           `json:"trace_id"`
}

type importJobsPayload struct {
	Jobs    []domain.ImportJob `json:"jobs"`
	TraceID string             `json:"trace_id"`
}

type importBundlePayload struct {
	Bundle  domain.ImportBundle `json:"bundle"`
	TraceID string              `json:"trace_id"`
}

type importBundlesPayload struct {
	Bundles []domain.ImportBundle `json:"bundles"`
	TraceID string                `json:"trace_id"`
}

type importRowsPayload struct {
	Rows []domain.ImportRow `json:"rows"`
	// NextAfterRowNo is the cursor for the next page ("" when this is the last).
	NextAfterRowNo int    `json:"next_after_row_no,omitempty"`
	TraceID        string `json:"trace_id"`
}

func (h *Handler) bulkReady(w http.ResponseWriter, r *http.Request) bool {
	if h.bulk == nil || h.importer == nil || h.jobs == nil {
		writeErr(w, r, h.log, &app.Error{Code: "bulk_unavailable", Message: "Sheets are not available on this server.", HTTPStatus: http.StatusNotImplemented})
		return false
	}
	return true
}

func attachment(w http.ResponseWriter, format, name string) {
	w.Header().Set("Content-Type", app.ContentType(format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + "." + format}))
	w.Header().Set("Cache-Control", "no-store")
}

// Export serves GET /admin/configuration/{register}/export?format=csv|xlsx&status=all|active|archived.
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	format, err := app.NormalizeFormat(r.URL.Query().Get("format"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	register := r.PathValue("register")
	reg, err := h.service.Register(r.Context(), tenantID(r), register)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	attachment(w, format, fileStem(reg.Label))
	if err := h.bulk.Export(r.Context(), tenantID(r), register, strings.TrimSpace(r.URL.Query().Get("status")), format, w); err != nil {
		// Headers may be out; log the cause, the truncated stream is the client's signal.
		h.log.Error("configuration export", "register", register, "error", err)
	}
}

// Template serves GET /admin/configuration/{register}/template?format=.
func (h *Handler) Template(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	format, err := app.NormalizeFormat(r.URL.Query().Get("format"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	register := r.PathValue("register")
	reg, err := h.service.Register(r.Context(), tenantID(r), register)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if !reg.Importable {
		writeErr(w, r, h.log, app.HTTPError(domain.ErrReadOnlyRegister))
		return
	}
	attachment(w, format, fileStem(reg.Label)+"-template")
	if err := h.bulk.Template(r.Context(), tenantID(r), register, format, w); err != nil {
		h.log.Error("configuration template", "register", register, "error", err)
	}
}

// Upload serves POST /admin/configuration/{register}/imports: multipart with one `file` part.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	h.withUploadedFile(w, r, func(name string, part io.Reader) {
		job, err := h.importer.Stage(r.Context(), writeParams(r, ""), r.PathValue("register"), name, part)
		if err != nil {
			h.writeUploadErr(w, r, err)
			return
		}
		httpresponse.WriteJSON(w, http.StatusAccepted, importJobPayload{Job: job, TraceID: traceID(r)})
	})
}

// withUploadedFile streams the `file` part of a multipart upload to fn, bounded by maxUploadBytes.
func (h *Handler) withUploadedFile(w http.ResponseWriter, r *http.Request, fn func(name string, part io.Reader)) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, r, h.log, app.BadRequest("invalid_upload", "Send the sheet as a multipart file upload."))
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			writeErr(w, r, h.log, app.BadRequest("invalid_upload", "Choose a .csv or .xlsx file to upload."))
			return
		}
		if err != nil {
			writeErr(w, r, h.log, app.BadRequest("invalid_upload", "The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		fn(part.FileName(), part)
		return
	}
}

func (h *Handler) writeUploadErr(w http.ResponseWriter, r *http.Request, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeErr(w, r, h.log, &app.Error{Code: "sheet_too_large", Message: "That file is larger than 64 MB. Split it and upload the parts.", HTTPStatus: http.StatusRequestEntityTooLarge})
		return
	}
	writeErr(w, r, h.log, app.HTTPError(err))
}

// --- the onboarding workbook ---

// WorkbookTemplate serves GET /admin/configuration/workbook/template: one Excel file with a tab
// per list, each carrying the upload header. Excel only; a CSV has no tabs.
func (h *Handler) WorkbookTemplate(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	if err := requireWorkbookFormat(r); err != nil {
		writeErr(w, r, h.log, err)
		return
	}
	attachment(w, domain.FormatXLSX, "goatos-setup-template")
	if err := h.bulk.WorkbookTemplate(r.Context(), tenantID(r), w); err != nil {
		h.log.Error("configuration workbook template", "error", err)
	}
}

// WorkbookExport serves GET /admin/configuration/workbook/export?status=: every list as a tab.
func (h *Handler) WorkbookExport(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	if err := requireWorkbookFormat(r); err != nil {
		writeErr(w, r, h.log, err)
		return
	}
	attachment(w, domain.FormatXLSX, "goatos-setup")
	if err := h.bulk.WorkbookExport(r.Context(), tenantID(r), strings.TrimSpace(r.URL.Query().Get("status")), w); err != nil {
		h.log.Error("configuration workbook export", "error", err)
	}
}

func requireWorkbookFormat(r *http.Request) *app.Error {
	format, err := app.NormalizeFormat(r.URL.Query().Get("format"))
	if err != nil {
		return app.HTTPError(err)
	}
	if format != domain.FormatXLSX && strings.TrimSpace(r.URL.Query().Get("format")) != "" {
		return app.BadRequest("invalid_format", "The workbook is an Excel file; choose xlsx.")
	}
	return nil
}

// UploadWorkbook serves POST /admin/configuration/workbook/imports: multipart with one `file`
// part, an .xlsx with one tab per list.
func (h *Handler) UploadWorkbook(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	h.withUploadedFile(w, r, func(name string, part io.Reader) {
		bundle, err := h.importer.StageWorkbook(r.Context(), writeParams(r, ""), name, part)
		if err != nil {
			h.writeUploadErr(w, r, err)
			return
		}
		httpresponse.WriteJSON(w, http.StatusAccepted, importBundlePayload{Bundle: bundle, TraceID: traceID(r)})
	})
}

// ListWorkbookImports serves GET /admin/configuration/workbook/imports: recent workbooks.
func (h *Handler) ListWorkbookImports(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	bundles, err := h.jobs.ListImportBundles(r.Context(), tenantID(r), 10)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importBundlesPayload{Bundles: bundles, TraceID: traceID(r)})
}

// GetBundle serves GET /admin/configuration-import-bundles/{bundle_id}: the workbook the screen polls.
func (h *Handler) GetBundle(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	bundle, err := h.jobs.GetImportBundle(r.Context(), tenantID(r), r.PathValue("bundle_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importBundlePayload{Bundle: bundle, TraceID: traceID(r)})
}

// BundleErrors serves GET /admin/configuration-import-bundles/{bundle_id}/errors?format=: every
// tab's rows to fix (a workbook with one tab per sheet that had problems, or one CSV).
func (h *Handler) BundleErrors(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	format, err := app.NormalizeFormat(r.URL.Query().Get("format"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	bundle, err := h.jobs.GetImportBundle(r.Context(), tenantID(r), r.PathValue("bundle_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	attachment(w, format, "goatos-setup-rows-to-fix")
	if err := h.bulk.BundleErrorSheet(r.Context(), h.jobs, tenantID(r), bundle.ID, format, w); err != nil {
		h.log.Error("configuration bundle error sheet", "bundle_id", bundle.ID, "error", err)
	}
}

// ApplyBundle serves POST /admin/configuration-import-bundles/{bundle_id}/apply.
func (h *Handler) ApplyBundle(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	bundle, ok, err := h.jobs.RequestImportBundleApply(r.Context(), tenantID(r), r.PathValue("bundle_id"), writeParams(r, "").ActorID)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if !ok {
		current, err := h.jobs.GetImportBundle(r.Context(), tenantID(r), r.PathValue("bundle_id"))
		if err != nil {
			writeErr(w, r, h.log, app.HTTPError(err))
			return
		}
		if current.Status == domain.ImportApplying || current.Status == domain.ImportApplied {
			httpresponse.WriteJSON(w, http.StatusOK, importBundlePayload{Bundle: current, TraceID: traceID(r)})
			return
		}
		writeErr(w, r, h.log, &app.Error{Code: "not_previewed", Message: "This workbook is not ready to apply.", HTTPStatus: http.StatusConflict})
		return
	}
	h.importer.KickBundle(tenantID(r), bundle.ID)
	httpresponse.WriteJSON(w, http.StatusAccepted, importBundlePayload{Bundle: bundle, TraceID: traceID(r)})
}

// CancelBundle serves POST /admin/configuration-import-bundles/{bundle_id}/cancel.
func (h *Handler) CancelBundle(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	bundle, ok, err := h.jobs.CancelImportBundle(r.Context(), tenantID(r), r.PathValue("bundle_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if !ok {
		writeErr(w, r, h.log, &app.Error{Code: "not_cancellable", Message: "This workbook has already finished.", HTTPStatus: http.StatusConflict})
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importBundlePayload{Bundle: bundle, TraceID: traceID(r)})
}

// ListImports serves GET /admin/configuration/{register}/imports: recent uploads of the register.
func (h *Handler) ListImports(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	jobs, err := h.jobs.ListImportJobs(r.Context(), tenantID(r), r.PathValue("register"), 20)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importJobsPayload{Jobs: jobs, TraceID: traceID(r)})
}

// GetImport serves GET /admin/configuration-imports/{job_id}: the job the screen polls.
func (h *Handler) GetImport(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	job, err := h.jobs.GetImportJob(r.Context(), tenantID(r), r.PathValue("job_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importJobPayload{Job: job, TraceID: traceID(r)})
}

// ImportRows serves GET /admin/configuration-imports/{job_id}/rows?state=&after=&limit=.
func (h *Handler) ImportRows(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	q := r.URL.Query()
	after, _ := strconv.Atoi(q.Get("after"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > app.MaxPageSize {
		limit = app.DefaultPageSize
	}
	state := strings.TrimSpace(q.Get("state"))
	switch state {
	case "", domain.ImportRowStaged, domain.ImportRowValid, domain.ImportRowApplying, domain.ImportRowInvalid, domain.ImportRowApplied, domain.ImportRowFailed, domain.ImportRowSkipped:
	default:
		writeErr(w, r, h.log, app.BadRequest("invalid_state", "Unknown row state."))
		return
	}
	if _, err := h.jobs.GetImportJob(r.Context(), tenantID(r), r.PathValue("job_id")); err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	rows, err := h.jobs.ImportRows(r.Context(), tenantID(r), r.PathValue("job_id"), ports.ImportRowsParams{State: state, AfterRowNo: after, Limit: limit + 1})
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	payload := importRowsPayload{Rows: rows, TraceID: traceID(r)}
	if len(rows) > limit {
		payload.Rows = rows[:limit]
		payload.NextAfterRowNo = rows[limit-1].RowNo
	}
	httpresponse.WriteJSON(w, http.StatusOK, payload)
}

// ImportErrors serves GET /admin/configuration-imports/{job_id}/errors?format=: the rows to fix.
func (h *Handler) ImportErrors(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	format, err := app.NormalizeFormat(r.URL.Query().Get("format"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	job, err := h.jobs.GetImportJob(r.Context(), tenantID(r), r.PathValue("job_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	attachment(w, format, fileStem(job.Register)+"-rows-to-fix")
	if err := h.bulk.ErrorSheet(r.Context(), h.jobs, tenantID(r), job.ID, format, w); err != nil {
		h.log.Error("configuration error sheet", "job_id", job.ID, "error", err)
	}
}

// ApplyImport serves POST /admin/configuration-imports/{job_id}/apply.
func (h *Handler) ApplyImport(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	job, ok, err := h.jobs.RequestImportApply(r.Context(), tenantID(r), r.PathValue("job_id"), writeParams(r, "").ActorID)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if !ok {
		current, err := h.jobs.GetImportJob(r.Context(), tenantID(r), r.PathValue("job_id"))
		if err != nil {
			writeErr(w, r, h.log, app.HTTPError(err))
			return
		}
		if current.Status == domain.ImportApplying || current.Status == domain.ImportApplied {
			// Already on its way: a double click is the same request.
			httpresponse.WriteJSON(w, http.StatusOK, importJobPayload{Job: current, TraceID: traceID(r)})
			return
		}
		writeErr(w, r, h.log, &app.Error{Code: "not_previewed", Message: "This upload is not ready to apply.", HTTPStatus: http.StatusConflict})
		return
	}
	h.importer.Kick(tenantID(r), job.ID)
	httpresponse.WriteJSON(w, http.StatusAccepted, importJobPayload{Job: job, TraceID: traceID(r)})
}

// CancelImport serves POST /admin/configuration-imports/{job_id}/cancel.
func (h *Handler) CancelImport(w http.ResponseWriter, r *http.Request) {
	if !h.bulkReady(w, r) {
		return
	}
	job, ok, err := h.jobs.CancelImportJob(r.Context(), tenantID(r), r.PathValue("job_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if !ok {
		writeErr(w, r, h.log, &app.Error{Code: "not_cancellable", Message: "This upload has already finished.", HTTPStatus: http.StatusConflict})
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, importJobPayload{Job: job, TraceID: traceID(r)})
}

func fileStem(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_', r == '&', r == ':', r == '/':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
