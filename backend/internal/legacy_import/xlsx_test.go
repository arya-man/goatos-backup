package legacy_import

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseXLSXSheetSelectsNamedNonFirstSheet(t *testing.T) {
	data := testWorkbook(t, []testSheet{
		{
			Name: "Operational",
			Rows: [][]string{
				{"Task", "Owner", "Done"},
				{"Vaccination", "Field team", "No"},
			},
		},
		{
			Name: "Combined",
			Rows: [][]string{
				requiredWorkbookColumns,
				{"CBE", "1900", "CBE", longRFID, "Adult", "Female", "Synthetic Boer", "", "S1", "P1"},
			},
		},
	})

	rows, err := ParseXLSXSheet(data, "Combined")
	if err != nil {
		t.Fatalf("ParseXLSXSheet Combined: %v", err)
	}
	if len(rows) != 1 || rows[0].Raw["RFID"] != longRFID || rows[0].Raw["Old ID"] != "1900" {
		t.Fatalf("named sheet parse mismatch: %#v", rows)
	}
}

func TestParseXLSXSheetMissingSheetReturnsClearError(t *testing.T) {
	data := testWorkbook(t, []testSheet{{Name: "Combined", Rows: [][]string{requiredWorkbookColumns}}})
	_, err := ParseXLSXSheet(data, "Missing")
	if err == nil || !strings.Contains(err.Error(), `sheet "Missing" not found`) {
		t.Fatalf("missing sheet error=%v", err)
	}
}

func TestParseXLSXSheetMatchesTrimmedWorkbookTabName(t *testing.T) {
	data := testWorkbook(t, []testSheet{{
		Name: "  RFID DB",
		Rows: [][]string{shape1WorkbookColumns},
	}})

	got, err := DiscoverXLSXSource(data, "RFID DB")
	if err != nil {
		t.Fatalf("DiscoverXLSXSource: %v", err)
	}
	if got.Classification != SourceClassificationRecognizedShape1NeedsMap {
		t.Fatalf("classification=%s, want %s", got.Classification, SourceClassificationRecognizedShape1NeedsMap)
	}
}

func TestSourceDiscoveryClassifiesWorkbooks(t *testing.T) {
	tests := []struct {
		name           string
		headers        []string
		want           string
		wantImportable bool
	}{
		{
			name:           "shape 2 importable",
			headers:        requiredWorkbookColumns,
			want:           SourceClassificationImportableShape2,
			wantImportable: true,
		},
		{
			name:           "shape 1 recognized blocked",
			headers:        shape1WorkbookColumns,
			want:           SourceClassificationRecognizedShape1NeedsMap,
			wantImportable: false,
		},
		{
			name:           "operational unknown",
			headers:        []string{"Task", "Owner", "Due Date", "Status"},
			want:           SourceClassificationOperationalOrUnknown,
			wantImportable: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := testWorkbook(t, []testSheet{{Name: "Combined", Rows: [][]string{tc.headers}}})
			got, err := DiscoverXLSXSource(data, "Combined")
			if err != nil {
				t.Fatalf("DiscoverXLSXSource: %v", err)
			}
			if got.Classification != tc.want || got.Importable != tc.wantImportable {
				t.Fatalf("classification=%s importable=%t, want %s/%t", got.Classification, got.Importable, tc.want, tc.wantImportable)
			}
		})
	}
}

type testSheet struct {
	Name string
	Rows [][]string
}

func testWorkbook(t *testing.T, sheets []testSheet) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeTestZipFile(t, zw, "[Content_Types].xml", contentTypesXML(len(sheets)))
	writeTestZipFile(t, zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`)
	writeTestZipFile(t, zw, "xl/workbook.xml", workbookXML(sheets))
	writeTestZipFile(t, zw, "xl/_rels/workbook.xml.rels", workbookRelsXML(len(sheets)))
	for idx, sheet := range sheets {
		writeTestZipFile(t, zw, fmt.Sprintf("xl/worksheets/sheet%d.xml", idx+1), worksheetXMLTest(sheet.Rows))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func contentTypesXML(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for idx := 1; idx <= sheetCount; idx++ {
		b.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, idx))
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func workbookXML(sheets []testSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for idx, sheet := range sheets {
		b.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscapeTest(sheet.Name), idx+1, idx+1))
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func workbookRelsXML(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for idx := 1; idx <= sheetCount; idx++ {
		b.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, idx, idx))
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func worksheetXMLTest(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIdx, row := range rows {
		b.WriteString(fmt.Sprintf(`<row r="%d">`, rowIdx+1))
		for colIdx, value := range row {
			ref := columnName(colIdx+1) + fmt.Sprint(rowIdx+1)
			b.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEscapeTest(value)))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func columnName(idx int) string {
	var out []byte
	for idx > 0 {
		idx--
		out = append([]byte{byte('A' + idx%26)}, out...)
		idx /= 26
	}
	return string(out)
}

func xmlEscapeTest(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func writeTestZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}
