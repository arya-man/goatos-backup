package app

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// BULK DOWNLOAD (maintainer instruction 2026-09-18). A register is streamed to the caller one
// keyset page at a time -- the same List read the screen uses, at a bigger page -- and written
// row by row into a CSV or an XLSX stream writer, so a hundred-thousand-animal sheet costs the
// server one page of rows at a time and never a herd-sized array.

// exportPageSize is the keyset page an export walks the register in.
const exportPageSize = 2000

// SheetWriter is the one shape both formats are written through.
type SheetWriter interface {
	Row(cells []string) error
	Close() error
}

type csvSheet struct{ w *csv.Writer }

func (s csvSheet) Row(cells []string) error {
	out := make([]string, len(cells))
	for i, cell := range cells {
		out[i] = csvSafeCell(cell)
	}
	return s.w.Write(out)
}
func (s csvSheet) Close() error { s.w.Flush(); return s.w.Error() }

func csvSafeCell(cell string) string {
	if cell == "" {
		return ""
	}
	trimmed := strings.TrimLeft(cell, " \t\r\n")
	if trimmed == "" {
		return cell
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + cell
	}
	return cell
}

type xlsxSheet struct {
	f   *excelize.File
	sw  *excelize.StreamWriter
	row int
	out io.Writer
}

func (s *xlsxSheet) Row(cells []string) error {
	s.row++
	cell, err := excelize.CoordinatesToCellName(1, s.row)
	if err != nil {
		return err
	}
	values := make([]any, len(cells))
	for i, c := range cells {
		values[i] = c
	}
	return s.sw.SetRow(cell, values)
}

func (s *xlsxSheet) Close() error {
	if err := s.sw.Flush(); err != nil {
		return err
	}
	if _, err := s.f.WriteTo(s.out); err != nil {
		return err
	}
	return s.f.Close()
}

// NewSheetWriter opens a writer of the format onto out. The XLSX sheet is named after the
// register when Excel accepts that name; farm-authored labels can contain characters Excel
// forbids, so the workbook falls back to Sheet1 instead of failing a download.
func NewSheetWriter(format, sheetName string, out io.Writer) (SheetWriter, error) {
	switch format {
	case domain.FormatCSV:
		return csvSheet{w: csv.NewWriter(out)}, nil
	case domain.FormatXLSX:
		f := excelize.NewFile()
		name := safeSheetName(sheetName)
		if name != "Sheet1" {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				name = "Sheet1"
			}
		}
		sw, err := f.NewStreamWriter(name)
		if err != nil {
			return nil, err
		}
		return &xlsxSheet{f: f, sw: sw, out: out}, nil
	}
	return nil, BadRequest("invalid_format", "Choose csv or xlsx.")
}

func safeSheetName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 31 {
		return "Sheet1"
	}
	for _, r := range name {
		switch r {
		case ':', '\\', '/', '?', '*', '[', ']':
			name = "Sheet1"
		}
	}
	return name
}

// WorkbookWriter writes one Excel file with several worksheets, each streamed in turn: one
// tab per register for the onboarding workbook. Tabs are written sequentially -- a tab is
// flushed before the next opens -- so one page of rows is in memory at a time, as with a
// single-sheet download.
type WorkbookWriter struct {
	f    *excelize.File
	out  io.Writer
	sw   *excelize.StreamWriter
	row  int
	used map[string]bool
	err  error
}

// NewWorkbookWriter opens a workbook onto out; call Sheet before the first Row.
func NewWorkbookWriter(out io.Writer) *WorkbookWriter {
	return &WorkbookWriter{f: excelize.NewFile(), out: out, used: map[string]bool{}}
}

// Sheet closes the open tab and starts a new one named after name (made Excel-safe and unique).
func (w *WorkbookWriter) Sheet(name string) error {
	if err := w.flush(); err != nil {
		return err
	}
	name = uniqueSheetName(name, w.used)
	w.used[name] = true
	if len(w.used) == 1 {
		// excelize opens with Sheet1; the first tab takes it over.
		if name != "Sheet1" {
			if err := w.f.SetSheetName("Sheet1", name); err != nil {
				return err
			}
		}
	} else if _, err := w.f.NewSheet(name); err != nil {
		return err
	}
	sw, err := w.f.NewStreamWriter(name)
	if err != nil {
		return err
	}
	w.sw, w.row = sw, 0
	return nil
}

// Row appends one line to the open tab.
func (w *WorkbookWriter) Row(cells []string) error {
	if w.sw == nil {
		return errors.New("workbook: no open sheet")
	}
	w.row++
	cell, err := excelize.CoordinatesToCellName(1, w.row)
	if err != nil {
		return err
	}
	values := make([]any, len(cells))
	for i, c := range cells {
		values[i] = c
	}
	return w.sw.SetRow(cell, values)
}

func (w *WorkbookWriter) flush() error {
	if w.sw == nil {
		return nil
	}
	err := w.sw.Flush()
	w.sw = nil
	return err
}

// Close flushes the last tab and writes the file.
func (w *WorkbookWriter) Close() error {
	if err := w.flush(); err != nil {
		return err
	}
	if _, err := w.f.WriteTo(w.out); err != nil {
		return err
	}
	return w.f.Close()
}

// uniqueSheetName makes a worksheet name Excel accepts (no : \ / ? * [ ], at most 31 runes) and
// distinct from the names already used, so two registers with clashing labels both get a tab.
func uniqueSheetName(name string, used map[string]bool) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch r {
		case ':', '\\', '/', '?', '*', '[', ']':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	base := strings.TrimSpace(b.String())
	if base == "" {
		base = "Sheet"
	}
	if runes := []rune(base); len(runes) > 31 {
		base = strings.TrimSpace(string(runes[:31]))
	}
	candidate := base
	for n := 2; used[candidate]; n++ {
		suffix := " " + strconv.Itoa(n)
		runes := []rune(base)
		if len(runes)+len(suffix) > 31 {
			runes = runes[:31-len(suffix)]
		}
		candidate = strings.TrimSpace(string(runes)) + suffix
	}
	return candidate
}

// WorkbookRegisters is the tabs the onboarding workbook carries, in the order they are worked:
// the static registers in domain.WorkbookOrder, then every reference list the tenant keeps.
func (s *Service) WorkbookRegisters(ctx context.Context, tenantID string) ([]domain.Register, error) {
	all, err := s.Registers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]domain.Register, len(all))
	for _, reg := range all {
		byKey[reg.Key] = reg
	}
	out := make([]domain.Register, 0, len(all))
	for _, key := range domain.WorkbookOrder {
		if reg, ok := byKey[key]; ok && reg.Importable {
			out = append(out, reg)
		}
	}
	for _, reg := range all {
		if domain.IsReferenceRegister(reg.Key) && reg.Importable {
			out = append(out, reg)
		}
	}
	return out, nil
}

// WorkbookTemplate writes one Excel file with a tab per importable register, each carrying
// exactly the header the upload expects, so a new farm is set up from a single file.
func (s *Service) WorkbookTemplate(ctx context.Context, tenantID string, out io.Writer) error {
	regs, err := s.WorkbookRegisters(ctx, tenantID)
	if err != nil {
		return err
	}
	w := NewWorkbookWriter(out)
	for _, reg := range regs {
		if err := w.Sheet(reg.Label); err != nil {
			return err
		}
		if err := w.Row(templateHeader(reg)); err != nil {
			return err
		}
	}
	return w.Close()
}

// WorkbookExport writes every workbook register as a tab with all its rows (status: active,
// archived or all), in the upload's own shape, so the whole setup round-trips as one file.
func (s *Service) WorkbookExport(ctx context.Context, tenantID, status string, out io.Writer) error {
	regs, err := s.WorkbookRegisters(ctx, tenantID)
	if err != nil {
		return err
	}
	if status == "" {
		status = "all"
	}
	w := NewWorkbookWriter(out)
	for _, reg := range regs {
		if err := w.Sheet(reg.Label); err != nil {
			return err
		}
		if err := s.exportRows(ctx, tenantID, reg, status, w); err != nil {
			return err
		}
	}
	return w.Close()
}

// templateHeader is the upload header of a register: every non-derived sheet column.
func templateHeader(reg domain.Register) []string {
	cols := domain.SheetColumns(reg)
	header := make([]string, 0, len(cols))
	for _, c := range cols {
		if c.Derived {
			continue
		}
		header = append(header, c.Key)
	}
	return header
}

// exportRows writes a register's header and rows into an open writer, one keyset page at a time.
func (s *Service) exportRows(ctx context.Context, tenantID string, reg domain.Register, status string, w SheetWriter) error {
	cols := domain.SheetColumns(reg)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Key
	}
	if err := w.Row(header); err != nil {
		return err
	}
	cursor := ""
	for {
		// scale-guard:ignore: the export IS a keyset page walk -- one page of rows in memory at a time, streamed out; cursor advances each iteration and the loop breaks on an empty NextCursor
		page, err := s.repo.List(ctx, tenantID, reg.Key, ports.ListParams{Status: status, Cursor: cursor, Limit: exportPageSize})
		if err != nil {
			return err
		}
		for _, row := range page.Rows {
			cells := make([]string, len(cols))
			for i, c := range cols {
				cells[i] = domain.SheetCell(c, row)
			}
			if err := w.Row(cells); err != nil {
				return err
			}
		}
		if page.NextCursor == "" || len(page.Rows) == 0 {
			return nil
		}
		cursor = page.NextCursor
	}
}

// ContentType is the download's media type per format.
func ContentType(format string) string {
	if format == domain.FormatXLSX {
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return "text/csv; charset=utf-8"
}

// NormalizeFormat accepts csv (default) or xlsx.
func NormalizeFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", domain.FormatCSV:
		return domain.FormatCSV, nil
	case domain.FormatXLSX:
		return domain.FormatXLSX, nil
	}
	return "", BadRequest("invalid_format", "Choose csv or xlsx.")
}

// Export streams the whole register (status: active, archived or all) into the writer.
func (s *Service) Export(ctx context.Context, tenantID, register, status, format string, out io.Writer) error {
	reg, err := s.Register(ctx, tenantID, register)
	if err != nil {
		return err
	}
	w, err := NewSheetWriter(format, reg.Label, out)
	if err != nil {
		return err
	}
	if status == "" {
		status = "all"
	}
	if err := s.exportRows(ctx, tenantID, reg, status, w); err != nil {
		return err
	}
	return w.Close()
}

// Template writes the register's header row, so a person starts from the exact columns the
// upload expects.
func (s *Service) Template(ctx context.Context, tenantID, register, format string, out io.Writer) error {
	reg, err := s.Register(ctx, tenantID, register)
	if err != nil {
		return err
	}
	if !reg.Importable {
		return domain.ErrReadOnlyRegister
	}
	w, err := NewSheetWriter(format, reg.Label, out)
	if err != nil {
		return err
	}
	if err := w.Row(templateHeader(reg)); err != nil {
		return err
	}
	return w.Close()
}

// SheetReader iterates a sheet's lines: the header first, then data records.
type SheetReader interface {
	// Next returns the next record; io.EOF at the end.
	Next() ([]string, error)
}

type csvReader struct{ r *csv.Reader }

func (c csvReader) Next() ([]string, error) { return c.r.Read() }

type xlsxReader struct{ wb *xlsxWorkbook }

func (x *xlsxReader) Next() ([]string, error) {
	row, err := x.wb.Next()
	if errors.Is(err, io.EOF) {
		_ = x.wb.Close()
		return nil, io.EOF
	}
	return row, err
}

// OpenSheet opens an uploaded file of the format for streaming. An XLSX is read from its first
// worksheet; the workbook is held in memory (a hundred-thousand-row sheet is a few megabytes)
// while its rows stream out one at a time.
func OpenSheet(format string, src io.Reader) (SheetReader, error) {
	switch format {
	case domain.FormatCSV:
		r := csv.NewReader(src)
		r.FieldsPerRecord = -1
		r.TrimLeadingSpace = true
		r.LazyQuotes = true
		return csvReader{r: r}, nil
	case domain.FormatXLSX:
		// The upload is bounded by the transport (64 MB); the workbook is held while its first
		// sheet streams out row by row through the stdlib reader.
		data, err := io.ReadAll(src)
		if err != nil {
			return nil, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That file could not be read.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
		}
		wb, err := openXLSX(data)
		if err != nil {
			return nil, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That file could not be read as an Excel workbook.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
		}
		return &xlsxReader{wb: wb}, nil
	}
	return nil, BadRequest("invalid_format", "Upload a .csv or .xlsx file.")
}

// FormatFromFileName reads the format off the upload's extension.
func FormatFromFileName(name string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return domain.FormatCSV, nil
	case strings.HasSuffix(lower, ".xlsx"):
		return domain.FormatXLSX, nil
	}
	return "", BadRequest("invalid_format", "Upload a .csv or .xlsx file.")
}

// ErrorSheet streams the rows that failed validation or apply, with their messages, in the
// same column shape as the upload so a person fixes the file and uploads just those again.
func (s *Service) ErrorSheet(ctx context.Context, jobs ports.ImportRepository, tenantID, jobID, format string, out io.Writer) error {
	job, err := jobs.GetImportJob(ctx, tenantID, jobID)
	if err != nil {
		return err
	}
	reg, err := s.Register(ctx, tenantID, job.Register)
	if err != nil {
		return err
	}
	w, err := NewSheetWriter(format, "Rows to fix", out)
	if err != nil {
		return err
	}
	if err := s.writeErrorRows(ctx, jobs, tenantID, job, reg, w, ""); err != nil {
		return err
	}
	return w.Close()
}

// errorSheetHeader is the rows-to-fix header for a register: row, problem, then the upload's
// own columns.
func errorSheetHeader(reg domain.Register) []string {
	header := []string{"row", "problem"}
	for _, c := range domain.SheetColumns(reg) {
		if c.Key == domain.SheetColumnStatus || (c.Derived && c.Key != domain.SheetColumnID) {
			continue
		}
		header = append(header, c.Key)
	}
	return header
}

// writeErrorRows writes one job's invalid and failed rows (header first). With sheetName set,
// a leading "sheet" column names the tab the row came from (the CSV shape of a workbook's
// rows to fix, which has no tabs of its own).
func (s *Service) writeErrorRows(ctx context.Context, jobs ports.ImportRepository, tenantID string, job domain.ImportJob, reg domain.Register, w SheetWriter, sheetName string) error {
	header := errorSheetHeader(reg)
	line := header
	if sheetName != "" {
		line = append([]string{"sheet"}, header...)
	}
	if err := w.Row(line); err != nil {
		return err
	}
	for _, state := range []string{domain.ImportRowInvalid, domain.ImportRowFailed} {
		after := 0
		for {
			rows, err := jobs.ImportRows(ctx, tenantID, job.ID, ports.ImportRowsParams{State: state, AfterRowNo: after, Limit: exportPageSize})
			if err != nil {
				return err
			}
			for _, row := range rows {
				cells := make([]string, 0, len(line))
				if sheetName != "" {
					cells = append(cells, sheetName)
				}
				cells = append(cells, fmt.Sprint(row.RowNo), problemText(row.Errors))
				for _, key := range header[2:] {
					cells = append(cells, cellText(row.Fields[key]))
				}
				if err := w.Row(cells); err != nil {
					return err
				}
				after = row.RowNo
			}
			if len(rows) < exportPageSize {
				break
			}
		}
	}
	return nil
}

// BundleErrorSheet streams every tab's rows to fix: as an Excel workbook, one tab per sheet that
// had problems, in the same shape as each upload tab; as CSV, one file with a leading "sheet"
// column, since CSV has no tabs.
func (s *Service) BundleErrorSheet(ctx context.Context, jobs ports.ImportRepository, tenantID, bundleID, format string, out io.Writer) error {
	bundle, err := jobs.GetImportBundle(ctx, tenantID, bundleID)
	if err != nil {
		return err
	}
	if format == domain.FormatXLSX {
		wb := NewWorkbookWriter(out)
		wrote := false
		for _, job := range bundle.Jobs {
			if job.InvalidRows == 0 && job.FailedRows == 0 {
				continue
			}
			reg, err := s.Register(ctx, tenantID, job.Register)
			if err != nil {
				return err
			}
			if err := wb.Sheet(job.SheetName); err != nil {
				return err
			}
			if err := s.writeErrorRows(ctx, jobs, tenantID, job, reg, wb, ""); err != nil {
				return err
			}
			wrote = true
		}
		if !wrote {
			if err := wb.Sheet("Rows to fix"); err != nil {
				return err
			}
		}
		return wb.Close()
	}
	w, err := NewSheetWriter(format, "Rows to fix", out)
	if err != nil {
		return err
	}
	for _, job := range bundle.Jobs {
		if job.InvalidRows == 0 && job.FailedRows == 0 {
			continue
		}
		reg, err := s.Register(ctx, tenantID, job.Register)
		if err != nil {
			return err
		}
		if err := s.writeErrorRows(ctx, jobs, tenantID, job, reg, w, job.SheetName); err != nil {
			return err
		}
	}
	return w.Close()
}

func problemText(errs []domain.FieldError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Field != "" && e.Field != "row" {
			parts = append(parts, e.Field+": "+e.Message)
			continue
		}
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, " | ")
}

func cellText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
