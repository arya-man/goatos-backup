package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
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

func (s csvSheet) Row(cells []string) error { return s.w.Write(cells) }
func (s csvSheet) Close() error             { s.w.Flush(); return s.w.Error() }

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
// register so a person opening the workbook sees what it is.
func NewSheetWriter(format, sheetName string, out io.Writer) (SheetWriter, error) {
	switch format {
	case domain.FormatCSV:
		return csvSheet{w: csv.NewWriter(out)}, nil
	case domain.FormatXLSX:
		f := excelize.NewFile()
		name := sheetName
		if name == "" || len(name) > 31 {
			name = "Sheet1"
		}
		if err := f.SetSheetName("Sheet1", name); err != nil {
			return nil, err
		}
		sw, err := f.NewStreamWriter(name)
		if err != nil {
			return nil, err
		}
		return &xlsxSheet{f: f, sw: sw, out: out}, nil
	}
	return nil, BadRequest("invalid_format", "Choose csv or xlsx.")
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
	cols := domain.SheetColumns(reg)
	w, err := NewSheetWriter(format, reg.Label, out)
	if err != nil {
		return err
	}
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Key
	}
	if err := w.Row(header); err != nil {
		return err
	}
	if status == "" {
		status = "all"
	}
	cursor := ""
	for {
		// scale-guard:ignore: the export IS a keyset page walk -- one page of rows in memory at a time, streamed out; cursor advances each iteration and the loop breaks on an empty NextCursor
		page, err := s.repo.List(ctx, tenantID, register, ports.ListParams{Status: status, Cursor: cursor, Limit: exportPageSize})
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
			break
		}
		cursor = page.NextCursor
	}
	return w.Close()
}

// Template writes the register's header row and one example line, so a person starts from the
// exact columns the upload expects.
func (s *Service) Template(ctx context.Context, tenantID, register, format string, out io.Writer) error {
	reg, err := s.Register(ctx, tenantID, register)
	if err != nil {
		return err
	}
	if !reg.Importable {
		return domain.ErrReadOnlyRegister
	}
	cols := domain.SheetColumns(reg)
	w, err := NewSheetWriter(format, reg.Label, out)
	if err != nil {
		return err
	}
	header := make([]string, 0, len(cols))
	for _, c := range cols {
		// Only what an upload reads: no status, no derived column.
		if c.Derived {
			continue
		}
		header = append(header, c.Key)
	}
	if err := w.Row(header); err != nil {
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

type xlsxReader struct {
	f    *excelize.File
	rows *excelize.Rows
}

func (x *xlsxReader) Next() ([]string, error) {
	if !x.rows.Next() {
		if err := x.rows.Error(); err != nil {
			return nil, err
		}
		_ = x.rows.Close()
		_ = x.f.Close()
		return nil, io.EOF
	}
	return x.rows.Columns()
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
		f, err := excelize.OpenReader(src)
		if err != nil {
			// The cause travels in the transport error's detail; the person sees the farm sentence.
			return nil, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That file could not be read as an Excel workbook.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
		}
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			_ = f.Close()
			return nil, BadRequest("invalid_file", "That workbook has no sheet in it.")
		}
		rows, err := f.Rows(sheets[0])
		if err != nil {
			_ = f.Close()
			return nil, &Error{Code: "invalid_file", HTTPStatus: 400, Message: "That workbook could not be read.", Fields: []domain.FieldError{{Field: "file", Code: "invalid", Message: err.Error()}}}
		}
		return &xlsxReader{f: f, rows: rows}, nil
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
	cols := domain.SheetColumns(reg)
	w, err := NewSheetWriter(format, "Rows to fix", out)
	if err != nil {
		return err
	}
	header := []string{"row", "problem"}
	for _, c := range cols {
		if c.Key == domain.SheetColumnStatus || (c.Derived && c.Key != domain.SheetColumnID) {
			continue
		}
		header = append(header, c.Key)
	}
	if err := w.Row(header); err != nil {
		return err
	}
	for _, state := range []string{domain.ImportRowInvalid, domain.ImportRowFailed} {
		after := 0
		for {
			rows, err := jobs.ImportRows(ctx, tenantID, jobID, ports.ImportRowsParams{State: state, AfterRowNo: after, Limit: exportPageSize})
			if err != nil {
				return err
			}
			for _, row := range rows {
				cells := []string{fmt.Sprint(row.RowNo), problemText(row.Errors)}
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
