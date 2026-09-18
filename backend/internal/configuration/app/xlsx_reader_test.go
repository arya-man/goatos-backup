package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
)

// TestXLSXReaderReadsWhatASpreadsheetSaves round-trips a workbook written the way a person's
// spreadsheet writes it: shared strings, plain numbers, a 15-digit tag, a booleans cell, a real
// DATE cell under a date style (must come back as YYYY-MM-DD, never the serial), an empty column
// in the middle, and a blank trailing row.
func TestXLSXReaderReadsWhatASpreadsheetSaves(t *testing.T) {
	f := excelize.NewFile()
	sheet := "Animals"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(f.SetSheetRow(sheet, "A1", &[]any{"animal_identifier_1", "capacity", "", "dob", "has_icu", "name"}))
	must(f.SetCellValue(sheet, "A2", 901007000504274))
	must(f.SetCellValue(sheet, "B2", 40))
	must(f.SetCellValue(sheet, "D2", time.Date(2024, 7, 21, 0, 0, 0, 0, time.UTC)))
	dateStyle, err := f.NewStyle(&excelize.Style{NumFmt: 14})
	must(err)
	must(f.SetCellStyle(sheet, "D2", "D2", dateStyle))
	must(f.SetCellValue(sheet, "E2", true))
	must(f.SetCellValue(sheet, "F2", "Castro & Sons"))
	must(f.SetCellValue(sheet, "A3", "BLR-699"))
	must(f.SetCellValue(sheet, "B3", 12.5))
	var buf bytes.Buffer
	must(f.Write(&buf))
	must(f.Close())

	reader, err := OpenSheet(domain.FormatXLSX, bytes.NewReader(buf.Bytes()))
	must(err)
	var rows [][]string
	for {
		row, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		must(err)
		rows = append(rows, row)
	}
	want := []string{
		"animal_identifier_1,capacity,,dob,has_icu,name",
		"901007000504274,40,,2024-07-21,true,Castro & Sons",
		"BLR-699,12.5",
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d %q", len(rows), rows)
	}
	for i := range want {
		if got := strings.Join(rows[i], ","); got != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestXLSXReaderRefusesGarbageAndBadIndexes(t *testing.T) {
	if _, err := OpenSheet(domain.FormatXLSX, strings.NewReader("not a zip")); err == nil {
		t.Fatalf("garbage must be refused")
	}
	wb := &xlsxWorkbook{shared: []string{"a"}}
	if got := wb.cellValue("s", "", "-1"); got != "" {
		t.Fatalf("a negative shared-string index must read as blank, got %q", got)
	}
	if got := wb.cellValue("s", "", "7"); got != "" {
		t.Fatalf("an out-of-range shared-string index must read as blank, got %q", got)
	}
	if got := xlsxSerialDate(45494); got != "2024-07-21" {
		t.Fatalf("serial 45494 = %s", got)
	}
	if xlsxColumnIndex("AB7") != 27 || xlsxColumnIndex("A1") != 0 {
		t.Fatalf("column index")
	}
}

func TestXLSXReaderRefusesMalformedWorkbookRelationships(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Rows" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>name</t></is></c></row></sheetData></worksheet>`,
		"[Content_Types].xml":        `<Types/>`,
		"_rels/.rels":                `<Relationships/>`,
		"docProps/core.xml":          `<coreProperties/>`,
		"docProps/app.xml":           `<Properties/>`,
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if _, err := OpenSheet(domain.FormatXLSX, bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatalf("malformed workbook rels must be refused")
	}
}

func TestXLSXWriterFallsBackForFarmAuthoredSheetNames(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewSheetWriter(domain.FormatXLSX, "Milk/Meat: Bands", &buf)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if err := w.Row([]string{"name"}); err != nil {
		t.Fatalf("row: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("open written workbook: %v", err)
	}
	defer func() { _ = f.Close() }()
	if got := f.GetSheetName(0); got != "Sheet1" {
		t.Fatalf("sheet name = %q, want Sheet1 fallback", got)
	}
}
