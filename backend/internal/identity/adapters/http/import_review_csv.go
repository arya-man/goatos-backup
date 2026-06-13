package identityhttp

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/csvutil"
)

const importRunRowsCSVLimit = 500

var messyImportRunRowStates = []string{"needs_review", "error"}

type importRunRowsCSVSpec struct {
	ProcessingState *string
	ReasonCode      *string
}

func (h *Handler) ExportImportRunRowsCSV(w http.ResponseWriter, r *http.Request) {
	importRunID := r.PathValue("import_run_id")
	specs := importRunRowsCSVSpecs(r)
	firstPage, err := h.fetchImportRunRowsCSVPage(r, importRunID, specs[0], nil)
	if err != nil {
		h.respond(w, r, nil, err)
		return
	}

	scope := "messy"
	if strings.EqualFold(r.URL.Query().Get("scope"), "current") {
		scope = "current"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mesha-import-review-%s-%s.csv"`, scope, safeCSVFilename(importRunID)))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	writer := csv.NewWriter(w)
	if err := csvutil.WriteSafeRow(writer, importRunRowsCSVHeader()); err != nil {
		h.logCSVWriteError(r, err)
		return
	}
	if !h.writeImportRunCSVPage(r, writer, importRunID, firstPage.Items) {
		return
	}
	if !h.writeImportRunCSVSpecFromCursor(r, writer, importRunID, specs[0], firstPage.NextCursor) {
		return
	}
	for _, spec := range specs[1:] {
		if !h.writeImportRunCSVSpec(r, writer, importRunID, spec) {
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		h.logCSVWriteError(r, err)
	}
}

func importRunRowsCSVSpecs(r *http.Request) []importRunRowsCSVSpec {
	q := r.URL.Query()
	reasonCode := optionalQuery(q.Get("reason_code"))
	if strings.EqualFold(q.Get("scope"), "current") {
		return []importRunRowsCSVSpec{{
			ProcessingState: optionalQuery(q.Get("processing_state")),
			ReasonCode:      reasonCode,
		}}
	}
	specs := make([]importRunRowsCSVSpec, 0, len(messyImportRunRowStates))
	for _, state := range messyImportRunRowStates {
		value := state
		specs = append(specs, importRunRowsCSVSpec{
			ProcessingState: &value,
			ReasonCode:      reasonCode,
		})
	}
	return specs
}

func (h *Handler) writeImportRunCSVSpec(r *http.Request, writer *csv.Writer, importRunID string, spec importRunRowsCSVSpec) bool {
	page, err := h.fetchImportRunRowsCSVPage(r, importRunID, spec, nil)
	if err != nil {
		h.logCSVWriteError(r, err)
		return false
	}
	if !h.writeImportRunCSVPage(r, writer, importRunID, page.Items) {
		return false
	}
	return h.writeImportRunCSVSpecFromCursor(r, writer, importRunID, spec, page.NextCursor)
}

func (h *Handler) writeImportRunCSVSpecFromCursor(r *http.Request, writer *csv.Writer, importRunID string, spec importRunRowsCSVSpec, cursor *string) bool {
	for cursor != nil {
		page, err := h.fetchImportRunRowsCSVPage(r, importRunID, spec, cursor)
		if err != nil {
			h.logCSVWriteError(r, err)
			return false
		}
		if !h.writeImportRunCSVPage(r, writer, importRunID, page.Items) {
			return false
		}
		cursor = page.NextCursor
	}
	return true
}

func (h *Handler) fetchImportRunRowsCSVPage(r *http.Request, importRunID string, spec importRunRowsCSVSpec, cursor *string) (*domain.ImportRunRowsResponse, error) {
	return h.service.ListImportRunRows(r.Context(), ports.ListImportRunRowsParams{
		TenantID:        tenantID(r),
		ImportRunID:     importRunID,
		Limit:           importRunRowsCSVLimit,
		Cursor:          cursor,
		ProcessingState: spec.ProcessingState,
		ReasonCode:      spec.ReasonCode,
	}, traceID(r))
}

func (h *Handler) writeImportRunCSVPage(r *http.Request, writer *csv.Writer, importRunID string, rows []domain.ImportRunRow) bool {
	for _, row := range rows {
		if err := csvutil.WriteSafeRow(writer, importRunRowCSVFields(importRunID, row)); err != nil {
			h.logCSVWriteError(r, err)
			return false
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		h.logCSVWriteError(r, err)
		return false
	}
	return true
}

func importRunRowsCSVHeader() []string {
	return []string{
		"import_run_id",
		"import_row_id",
		"row_number",
		"row_state",
		"review_reasons",
		"rfid",
		"old_tag",
		"breed",
		"gender",
		"farm",
		"shed",
		"partition",
		"source_record_id",
		"source_row_key_ref",
		"matched_goat_id",
		"error_reason",
	}
}

func importRunRowCSVFields(importRunID string, row domain.ImportRunRow) []string {
	return []string{
		importRunID,
		row.ImportRowID,
		fmt.Sprint(row.RowNumber),
		row.RowState,
		strings.Join(row.ReviewReasons, "|"),
		ptrValue(row.RFID),
		ptrValue(row.OldTag),
		ptrValue(row.Breed),
		ptrValue(row.Gender),
		ptrValue(row.Farm),
		ptrValue(row.Shed),
		ptrValue(row.Partition),
		ptrValue(row.SourceRecordID),
		ptrValue(row.SourceRowKeyRef),
		ptrValue(row.MatchedGoatID),
		ptrValue(row.ErrorReason),
	}
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func safeCSVFilename(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "export"
	}
	return out
}

func (h *Handler) logCSVWriteError(r *http.Request, err error) {
	if err == nil {
		return
	}
	h.log.LogAttrs(r.Context(), slog.LevelError, "import_review_csv_export_failed",
		slog.String("trace_id", traceID(r)),
		slog.String("tenant_id", tenantID(r)),
		slog.String("import_run_id", r.PathValue("import_run_id")),
		slog.String("error", err.Error()),
	)
}
