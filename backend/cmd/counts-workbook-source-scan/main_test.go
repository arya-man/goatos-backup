package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestSourceManifestFromColumnMapFixtureCoversSourceWorkbooks(t *testing.T) {
	f, err := os.Open("../../testdata/counts/source-workbook-column-map.json")
	if err != nil {
		t.Fatalf("open source workbook column map fixture: %v", err)
	}
	defer f.Close()
	mapping, err := parseWorkbookMappingFile(f)
	if err != nil {
		t.Fatalf("parseWorkbookMappingFile fixture: %v", err)
	}
	manifest := sourceManifestFromMapping(mapping)
	if len(manifest.Sheets) < 12 {
		t.Fatalf("sheets=%d, want broad source sheet coverage", len(manifest.Sheets))
	}
	if !manifestHasSheet(manifest, "counting_db", "Counting DB - values only.xlsx", "DB", "Shed Tag") {
		t.Fatalf("manifest missing Counting DB DB/Shed Tag")
	}
	if !manifestHasSheet(manifest, "feed_automation_workbook", "Feed Directions Automation DB.xlsx", "CBE Validation", "Wastage Factor") {
		t.Fatalf("manifest missing feed automation validation/wastage factor")
	}
	if !manifestHasSheet(manifest, "sheds_db", "Sheds DB.xlsx", "DB", "CBE Capacity") {
		t.Fatalf("manifest missing Sheds DB/CBE Capacity")
	}
}

func TestWorkbookSourceScanPassesTempXLSXHeaders(t *testing.T) {
	root := t.TempDir()
	if err := writeTestXLSX(filepath.Join(root, "Counting DB - values only.xlsx"), map[string][][]string{
		"DB": {
			{"not", "the", "header"},
			{"Date", "Farm", "Shed", "Shed Tag", "Breed", "Age", "Count"},
		},
	}); err != nil {
		t.Fatalf("write counting db fixture: %v", err)
	}
	if err := writeTestXLSX(filepath.Join(root, "Sheds DB.xlsx"), map[string][][]string{
		"DB": {
			{"", "CBE", "CBE", "CPT"},
			{"Shed", "Tags", "Capacity", "Tags"},
		},
	}); err != nil {
		t.Fatalf("write sheds db fixture: %v", err)
	}
	manifest := workbookSourceManifest{SourceRef: "source.md", Sheets: []requiredSheet{
		{SourceSystem: "counting_db", Workbook: "Counting DB - values only.xlsx", Sheet: "DB", Headers: []string{"Date", "Farm", "Shed", "Shed Tag", "Breed", "Age", "Count"}},
		{SourceSystem: "sheds_db", Workbook: "Sheds DB.xlsx", Sheet: "DB", Headers: []string{"Shed", "CBE Tags", "CBE Capacity", "CPT Tags"}},
	}}
	result := checkWorkbookSources(manifest, root, nil, 5)
	if len(result.Missing) != 0 {
		t.Fatalf("missing=%v, want source headers present", result.Missing)
	}
	status, blocker := sourceScanStatus(manifest.SourceRef, result)
	if status != "pending" {
		t.Fatalf("status=%s blocker=%q, want pending", status, blocker)
	}
	if !strings.Contains(blocker, "full row parity") || !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("blocker=%q, want remaining closure caveats", blocker)
	}
}

func TestWorkbookSourceScanReportsMissingSheetAndHeader(t *testing.T) {
	root := t.TempDir()
	if err := writeTestXLSX(filepath.Join(root, "book.xlsx"), map[string][][]string{
		"DB": {{"Date", "Farm"}},
	}); err != nil {
		t.Fatalf("write workbook fixture: %v", err)
	}
	manifest := workbookSourceManifest{SourceRef: "source.md", Sheets: []requiredSheet{
		{SourceSystem: "counting_db", Workbook: "book.xlsx", Sheet: "DB", Headers: []string{"Date", "Farm", "Count"}},
		{SourceSystem: "counting_db", Workbook: "book.xlsx", Sheet: "Projected-DB", Headers: []string{"Date"}},
	}}
	result := checkWorkbookSources(manifest, root, nil, 5)
	got := strings.Join(result.Missing, "\n")
	if !strings.Contains(got, "missing header Count") || !strings.Contains(got, "Projected-DB: missing sheet") {
		t.Fatalf("missing=%v, want missing header and missing sheet", result.Missing)
	}
	status, blocker := sourceScanStatus(manifest.SourceRef, result)
	if status != "blocked" || !strings.Contains(blocker, "missing=") {
		t.Fatalf("status=%s blocker=%q, want blocked missing status", status, blocker)
	}
}

func TestUpsertCSG10SourceScanReadinessWritesCommandEvidence(t *testing.T) {
	db := &fakeReadinessDB{}
	err := upsertCSG10SourceScanReadiness(context.Background(), db, "tenant-1", "source.md", "pending", "still pending")
	if err != nil {
		t.Fatalf("upsertCSG10SourceScanReadiness: %v", err)
	}
	if !strings.Contains(db.query, "counts-workbook-source-scan") {
		t.Fatalf("query=%q, want command implementation ref", db.query)
	}
	if len(db.args) != 4 {
		t.Fatalf("args=%+v, want tenant/status/evidence/blocker", db.args)
	}
	if db.args[1] != "pending" {
		t.Fatalf("status arg=%v", db.args[1])
	}
	evidence, _ := db.args[2].(string)
	if !strings.HasPrefix(evidence, "counts-workbook-source-scan:source.md:") {
		t.Fatalf("evidence=%q, want command evidence ref", evidence)
	}
}

func TestWorkbookSourceScanReadinessWritesCSG10OnPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	status, blocker := sourceScanStatus("source.md", workbookSourceScanResult{Workbooks: 2, Sheets: 3, RequiredHeaders: 12})
	if status != "pending" || !strings.Contains(blocker, "full row parity") {
		t.Fatalf("status=%s blocker=%q, want pending remaining-work caveat", status, blocker)
	}
	const tenantID = "00000000-0000-4000-8000-000000000001"
	if err := upsertCSG10SourceScanReadiness(ctx, pool, tenantID, "source.md", status, blocker); err != nil {
		t.Fatalf("upsertCSG10SourceScanReadiness: %v", err)
	}

	var gotStatus, gotEvidence, gotBlocker string
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref, blocker_reason
FROM counts_shifting_readiness_subgates
WHERE tenant_id=$1::uuid AND subgate_id='CSG10'`, tenantID).Scan(&gotStatus, &gotEvidence, &gotBlocker); err != nil {
		t.Fatalf("load CSG10 readiness: %v", err)
	}
	if gotStatus != "pending" || !strings.Contains(gotEvidence, "counts-workbook-source-scan") ||
		!strings.Contains(gotBlocker, "owner-approved mapping review") {
		t.Fatalf("CSG10 status=%s evidence=%s blocker=%q, want pending workbook source scan evidence", gotStatus, gotEvidence, gotBlocker)
	}
}

func manifestHasSheet(manifest workbookSourceManifest, sourceSystem, workbook, sheet, header string) bool {
	for _, got := range manifest.Sheets {
		if got.SourceSystem != sourceSystem || got.Workbook != workbook || got.Sheet != sheet {
			continue
		}
		for _, gotHeader := range got.Headers {
			if gotHeader == header {
				return true
			}
		}
	}
	return false
}

func writeTestXLSX(path string, sheets map[string][][]string) error {
	var names []string
	for name := range sheets {
		names = append(names, name)
	}
	sort.Strings(names)

	shared := map[string]int{}
	var sharedValues []string
	indexShared := func(value string) int {
		if idx, ok := shared[value]; ok {
			return idx
		}
		idx := len(sharedValues)
		shared[value] = idx
		sharedValues = append(sharedValues, value)
		return idx
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipEntry(zw, "xl/workbook.xml", testWorkbookXML(names)); err != nil {
		return err
	}
	if err := writeZipEntry(zw, "xl/_rels/workbook.xml.rels", testWorkbookRelsXML(names)); err != nil {
		return err
	}
	for i, name := range names {
		if err := writeZipEntry(zw, fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), testSheetXML(sheets[name], indexShared)); err != nil {
			return err
		}
	}
	if err := writeZipEntry(zw, "xl/sharedStrings.xml", testSharedStringsXML(sharedValues)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func testWorkbookXML(names []string) string {
	var b strings.Builder
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, name := range names {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func testWorkbookRelsXML(names []string) string {
	var b strings.Builder
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range names {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func testSheetXML(rows [][]string, indexShared func(string) int) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, value := range row {
			if value == "" {
				continue
			}
			fmt.Fprintf(&b, `<c r="%s%d" t="s"><v>%d</v></c>`, columnName(c), r+1, indexShared(value))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func testSharedStringsXML(values []string) string {
	var b strings.Builder
	b.WriteString(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, value := range values {
		fmt.Fprintf(&b, `<si><t>%s</t></si>`, xmlEscape(value))
	}
	b.WriteString(`</sst>`)
	return b.String()
}

func writeZipEntry(zw *zip.Writer, name, body string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, body)
	return err
}

func columnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

func xmlEscape(value string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(value)); err != nil {
		panic(err)
	}
	return b.String()
}

type fakeReadinessDB struct {
	query string
	args  []any
	err   error
}

func (db *fakeReadinessDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.query = query
	db.args = args
	return pgconn.CommandTag{}, db.err
}
