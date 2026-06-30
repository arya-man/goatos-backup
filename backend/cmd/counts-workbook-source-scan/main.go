// Command counts-workbook-source-scan validates that local source XLSX files
// still contain the sanitized sheets and headers Feed Direction G2 relies on.
// It reads workbook structure only: no private rows, formulas, media links, or
// sheet values are imported into GoatOS runtime truth.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTimeout       = 120 * time.Second
	defaultMaxHeaderRows = 15
)

type config struct {
	Manifest      string
	SourceRoot    string
	WorkbookPaths workbookPathFlags
	TenantID      string
	SourceRef     string
	DryRun        bool
	Timeout       time.Duration
	MaxHeaderRows int
}

type workbookPathFlags map[string]string

func (f *workbookPathFlags) String() string {
	if f == nil || len(*f) == 0 {
		return ""
	}
	parts := make([]string, 0, len(*f))
	for workbook, path := range *f {
		parts = append(parts, workbook+"="+path)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (f *workbookPathFlags) Set(value string) error {
	if *f == nil {
		*f = map[string]string{}
	}
	left, right, ok := strings.Cut(value, "=")
	if !ok {
		return errors.New("workbook-path must be Workbook.xlsx=/absolute/or/relative/path")
	}
	workbook := strings.TrimSpace(left)
	path := strings.TrimSpace(right)
	if workbook == "" || path == "" {
		return errors.New("workbook-path workbook and path are required")
	}
	(*f)[workbook] = path
	return nil
}

type workbookMappingFile struct {
	SourceRef     string         `json:"source_ref"`
	MappedColumns []mappedColumn `json:"mapped_columns"`
}

type mappedColumn struct {
	SourceSystem   string `json:"source_system"`
	Workbook       string `json:"workbook"`
	Sheet          string `json:"sheet"`
	SourceRole     string `json:"source_role"`
	ColumnName     string `json:"column_name"`
	CanonicalField string `json:"canonical_field"`
	ValueType      string `json:"value_type"`
	Notes          string `json:"notes,omitempty"`
}

type requiredSheet struct {
	SourceSystem string
	Workbook     string
	Sheet        string
	Headers      []string
}

type workbookSourceManifest struct {
	SourceRef string
	Sheets    []requiredSheet
}

type workbookSourceScanResult struct {
	Workbooks       int
	Sheets          int
	RequiredHeaders int
	Missing         []string
}

type readinessWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type scannedWorkbook struct {
	Sheets map[string]scannedSheet
}

type scannedSheet struct {
	HeaderSet map[string]bool
}

type workbookSheet struct {
	Name string
	RID  string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	reader, closeFn, err := inputReader(cfg.Manifest)
	if err != nil {
		return err
	}
	defer closeFn()

	mapping, err := parseWorkbookMappingFile(reader)
	if err != nil {
		return err
	}
	manifest := sourceManifestFromMapping(mapping)
	if cfg.SourceRef == "" {
		cfg.SourceRef = manifest.SourceRef
	}
	if cfg.SourceRef == "" {
		return errors.New("source-ref is required in flags or manifest")
	}
	result := checkWorkbookSources(manifest, cfg.SourceRoot, cfg.WorkbookPaths, cfg.MaxHeaderRows)
	status, blocker := sourceScanStatus(cfg.SourceRef, result)
	if cfg.DryRun {
		fmt.Fprintf(stdout, "counts workbook source scan dry-run status=%s source_ref=%s workbooks=%d sheets=%d required_headers=%d missing=%d\n",
			status, cfg.SourceRef, result.Workbooks, result.Sheets, result.RequiredHeaders, len(result.Missing))
		if status == "blocked" {
			return errors.New(blocker)
		}
		return nil
	}
	if cfg.TenantID == "" {
		return errors.New("tenant-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := upsertCSG10SourceScanReadiness(ctx, pool, cfg.TenantID, cfg.SourceRef, status, blocker); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "counts workbook source scan status=%s source_ref=%s workbooks=%d sheets=%d required_headers=%d missing=%d\n",
		status, cfg.SourceRef, result.Workbooks, result.Sheets, result.RequiredHeaders, len(result.Missing))
	if status == "blocked" {
		return errors.New(blocker)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	cfg := config{WorkbookPaths: map[string]string{}}
	fs := flag.NewFlagSet("counts-workbook-source-scan", flag.ContinueOnError)
	fs.StringVar(&cfg.Manifest, "manifest", getenv("GOATOS_COUNTS_WORKBOOK_SOURCE_SCAN_MANIFEST"), "sanitized workbook mapping JSON file")
	fs.StringVar(&cfg.SourceRoot, "source-root", getenv("GOATOS_COUNTS_WORKBOOK_SOURCE_ROOT"), "local root to search for source XLSX workbooks")
	fs.Var(&cfg.WorkbookPaths, "workbook-path", "override workbook path as Workbook.xlsx=/path; repeatable")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.SourceRef, "source-ref", getenv("GOATOS_COUNTS_WORKBOOK_SOURCE_SCAN_SOURCE_REF"), "source finding or reviewed file reference")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_COUNTS_WORKBOOK_SOURCE_SCAN_DRY_RUN"), "validate workbook structure without DB writes")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_WORKBOOK_SOURCE_SCAN_TIMEOUT", defaultTimeout), "worker timeout")
	fs.IntVar(&cfg.MaxHeaderRows, "max-header-rows", intEnv("GOATOS_COUNTS_WORKBOOK_SOURCE_SCAN_MAX_HEADER_ROWS", defaultMaxHeaderRows), "number of leading rows to inspect for headers")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.Manifest = strings.TrimSpace(cfg.Manifest)
	cfg.SourceRoot = strings.TrimSpace(cfg.SourceRoot)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.SourceRef = strings.TrimSpace(cfg.SourceRef)
	if cfg.Manifest == "" {
		return config{}, errors.New("manifest is required")
	}
	if cfg.SourceRoot == "" && len(cfg.WorkbookPaths) == 0 {
		return config{}, errors.New("source-root or workbook-path is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if cfg.MaxHeaderRows <= 0 {
		return config{}, errors.New("max-header-rows must be positive")
	}
	return cfg, nil
}

func parseWorkbookMappingFile(r io.Reader) (workbookMappingFile, error) {
	var file workbookMappingFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return workbookMappingFile{}, err
	}
	file.SourceRef = strings.TrimSpace(file.SourceRef)
	if len(file.MappedColumns) == 0 {
		return workbookMappingFile{}, errors.New("mapped_columns must not be empty")
	}
	for i := range file.MappedColumns {
		column := &file.MappedColumns[i]
		column.SourceSystem = strings.TrimSpace(column.SourceSystem)
		column.Workbook = strings.TrimSpace(column.Workbook)
		column.Sheet = strings.TrimSpace(column.Sheet)
		column.SourceRole = strings.TrimSpace(column.SourceRole)
		column.ColumnName = strings.TrimSpace(column.ColumnName)
		column.CanonicalField = strings.TrimSpace(column.CanonicalField)
		column.ValueType = strings.TrimSpace(column.ValueType)
		column.Notes = strings.TrimSpace(column.Notes)
		if column.SourceSystem == "" || column.Workbook == "" || column.Sheet == "" || column.ColumnName == "" {
			return workbookMappingFile{}, fmt.Errorf("mapped_columns[%d] source_system, workbook, sheet, and column_name are required", i)
		}
	}
	return file, nil
}

func sourceManifestFromMapping(mapping workbookMappingFile) workbookSourceManifest {
	headersBySheet := map[string]map[string]bool{}
	sheetByKey := map[string]requiredSheet{}
	for _, column := range mapping.MappedColumns {
		key := workbookSheetKey(column.SourceSystem, column.Workbook, column.Sheet)
		if headersBySheet[key] == nil {
			headersBySheet[key] = map[string]bool{}
			sheetByKey[key] = requiredSheet{
				SourceSystem: column.SourceSystem,
				Workbook:     column.Workbook,
				Sheet:        column.Sheet,
			}
		}
		headersBySheet[key][column.ColumnName] = true
	}
	keys := make([]string, 0, len(sheetByKey))
	for key := range sheetByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	manifest := workbookSourceManifest{SourceRef: mapping.SourceRef}
	for _, key := range keys {
		sheet := sheetByKey[key]
		for header := range headersBySheet[key] {
			sheet.Headers = append(sheet.Headers, header)
		}
		sort.Strings(sheet.Headers)
		manifest.Sheets = append(manifest.Sheets, sheet)
	}
	return manifest
}

func checkWorkbookSources(manifest workbookSourceManifest, sourceRoot string, overrides map[string]string, maxHeaderRows int) workbookSourceScanResult {
	workbookSheets := map[string][]requiredSheet{}
	for _, sheet := range manifest.Sheets {
		workbookSheets[sheet.Workbook] = append(workbookSheets[sheet.Workbook], sheet)
	}
	workbooks := make([]string, 0, len(workbookSheets))
	for workbook := range workbookSheets {
		workbooks = append(workbooks, workbook)
	}
	sort.Strings(workbooks)

	result := workbookSourceScanResult{Workbooks: len(workbooks), Sheets: len(manifest.Sheets)}
	for _, sheet := range manifest.Sheets {
		result.RequiredHeaders += len(sheet.Headers)
	}
	for _, workbook := range workbooks {
		path, err := resolveWorkbookPath(sourceRoot, overrides, workbook)
		if err != nil {
			result.Missing = append(result.Missing, "workbook/"+workbook+": "+err.Error())
			continue
		}
		scanned, err := scanWorkbook(path, maxHeaderRows)
		if err != nil {
			result.Missing = append(result.Missing, "workbook/"+workbook+": "+err.Error())
			continue
		}
		for _, required := range workbookSheets[workbook] {
			sheet, ok := scanned.Sheets[required.Sheet]
			if !ok {
				result.Missing = append(result.Missing, workbook+"/"+required.Sheet+": missing sheet")
				continue
			}
			for _, header := range required.Headers {
				if !sheet.HeaderSet[normalizeHeader(header)] {
					result.Missing = append(result.Missing, workbook+"/"+required.Sheet+": missing header "+header)
				}
			}
		}
	}
	sort.Strings(result.Missing)
	return result
}

func sourceScanStatus(sourceRef string, result workbookSourceScanResult) (string, string) {
	if len(result.Missing) > 0 {
		return "blocked", fmt.Sprintf("Counts workbook source scan failed for %s: missing=%s",
			sourceRef, strings.Join(result.Missing, ","))
	}
	return "pending", fmt.Sprintf(
		"Counts workbook source scan found %d required headers across %d sheets/%d workbooks from %s; owner-approved mapping review, full row parity, observability breadth, and seeded local E2E remain before CSG10 can turn ready.",
		result.RequiredHeaders, result.Sheets, result.Workbooks, sourceRef)
}

func upsertCSG10SourceScanReadiness(ctx context.Context, db readinessWriter, tenantID, sourceRef, status, blocker string) error {
	_, err := db.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-workbook-source-scan;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
  now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`,
		tenantID, status, "counts-workbook-source-scan:"+sourceRef+":"+time.Now().UTC().Format(time.RFC3339), blocker)
	if err != nil {
		return fmt.Errorf("counts: upsert CSG10 workbook source scan readiness: %w", err)
	}
	return nil
}

func scanWorkbook(path string, maxHeaderRows int) (scannedWorkbook, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return scannedWorkbook{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	workbookXML, err := readZipFile(files, "xl/workbook.xml")
	if err != nil {
		return scannedWorkbook{}, err
	}
	sheets, err := parseWorkbookSheets(workbookXML)
	if err != nil {
		return scannedWorkbook{}, err
	}
	relsXML, err := readZipFile(files, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return scannedWorkbook{}, err
	}
	rels, err := parseWorkbookRels(relsXML)
	if err != nil {
		return scannedWorkbook{}, err
	}
	sharedStrings, err := readSharedStrings(files)
	if err != nil {
		return scannedWorkbook{}, err
	}

	scanned := scannedWorkbook{Sheets: map[string]scannedSheet{}}
	for _, sheet := range sheets {
		target := rels[sheet.RID]
		if target == "" {
			return scannedWorkbook{}, fmt.Errorf("sheet %q missing workbook relationship", sheet.Name)
		}
		sheetPath := normalizeWorkbookTarget(target)
		data, err := readZipFile(files, sheetPath)
		if err != nil {
			return scannedWorkbook{}, fmt.Errorf("sheet %q: %w", sheet.Name, err)
		}
		rows, err := parseSheetRows(data, sharedStrings, maxHeaderRows)
		if err != nil {
			return scannedWorkbook{}, fmt.Errorf("sheet %q: %w", sheet.Name, err)
		}
		scanned.Sheets[sheet.Name] = scannedSheet{HeaderSet: buildHeaderSet(rows)}
	}
	return scanned, nil
}

func parseWorkbookSheets(data []byte) ([]workbookSheet, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var sheets []workbookSheet
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		var sheet workbookSheet
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				sheet.Name = strings.TrimSpace(attr.Value)
			case "id":
				sheet.RID = strings.TrimSpace(attr.Value)
			}
		}
		if sheet.Name != "" && sheet.RID != "" {
			sheets = append(sheets, sheet)
		}
	}
	if len(sheets) == 0 {
		return nil, errors.New("workbook has no sheets")
	}
	return sheets, nil
}

func parseWorkbookRels(data []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	rels := map[string]string{}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "Id":
				id = strings.TrimSpace(attr.Value)
			case "Target":
				target = strings.TrimSpace(attr.Value)
			}
		}
		if id != "" && target != "" {
			rels[id] = target
		}
	}
	return rels, nil
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	data, err := readZipFile(files, "xl/sharedStrings.xml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stringsOut []string
	var current strings.Builder
	inSI := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch node := tok.(type) {
		case xml.StartElement:
			if node.Name.Local == "si" {
				current.Reset()
				inSI = true
				continue
			}
			if inSI && node.Name.Local == "t" {
				var text string
				if err := dec.DecodeElement(&text, &node); err != nil {
					return nil, err
				}
				current.WriteString(text)
			}
		case xml.EndElement:
			if node.Name.Local == "si" && inSI {
				stringsOut = append(stringsOut, current.String())
				current.Reset()
				inSI = false
			}
		}
	}
	return stringsOut, nil
}

type worksheetRow struct {
	Cells []worksheetCell `xml:"c"`
}

type worksheetCell struct {
	Ref          string       `xml:"r,attr"`
	Type         string       `xml:"t,attr"`
	Value        string       `xml:"v"`
	InlineString inlineString `xml:"is"`
}

type inlineString struct {
	Text []string `xml:"t"`
}

func parseSheetRows(data []byte, sharedStrings []string, maxRows int) ([][]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	rows := make([][]string, 0, maxRows)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		var row worksheetRow
		if err := dec.DecodeElement(&row, &start); err != nil {
			return nil, err
		}
		values := materializeRow(row, sharedStrings)
		if rowHasText(values) {
			rows = append(rows, values)
			if len(rows) >= maxRows {
				break
			}
		}
	}
	return rows, nil
}

func materializeRow(row worksheetRow, sharedStrings []string) []string {
	values := []string{}
	for _, cell := range row.Cells {
		value := cellText(cell, sharedStrings)
		index := columnIndex(cell.Ref)
		if index < 0 {
			values = append(values, value)
			continue
		}
		for len(values) <= index {
			values = append(values, "")
		}
		values[index] = value
	}
	return values
}

func cellText(cell worksheetCell, sharedStrings []string) string {
	switch strings.TrimSpace(cell.Type) {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || idx < 0 || idx >= len(sharedStrings) {
			return ""
		}
		return strings.TrimSpace(sharedStrings[idx])
	case "inlineStr":
		return strings.TrimSpace(strings.Join(cell.InlineString.Text, ""))
	default:
		return strings.TrimSpace(cell.Value)
	}
}

func buildHeaderSet(rows [][]string) map[string]bool {
	headers := map[string]bool{}
	for _, row := range rows {
		addHeaderRow(headers, row)
	}
	for i := 0; i+1 < len(rows); i++ {
		addHeaderRow(headers, combinedHeaderRow(rows[i], rows[i+1]))
	}
	return headers
}

func addHeaderRow(headers map[string]bool, row []string) {
	for _, value := range row {
		normalized := normalizeHeader(value)
		if normalized != "" {
			headers[normalized] = true
		}
	}
}

func combinedHeaderRow(top, bottom []string) []string {
	width := len(top)
	if len(bottom) > width {
		width = len(bottom)
	}
	combined := make([]string, width)
	for i := 0; i < width; i++ {
		left := cellAt(top, i)
		right := cellAt(bottom, i)
		switch {
		case left == "":
			combined[i] = right
		case right == "":
			combined[i] = left
		case strings.EqualFold(left, right):
			combined[i] = left
		default:
			combined[i] = left + " " + right
		}
	}
	return combined
}

func cellAt(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func normalizeHeader(value string) string {
	value = strings.TrimSpace(value)
	var withoutParens strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			withoutParens.WriteRune(r)
		}
	}
	var out strings.Builder
	for _, r := range strings.ToLower(withoutParens.String()) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func rowHasText(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func columnIndex(ref string) int {
	if ref == "" {
		return -1
	}
	letters := strings.Builder{}
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			letters.WriteRune(unicode.ToUpper(r))
			continue
		}
		break
	}
	if letters.Len() == 0 {
		return -1
	}
	index := 0
	for _, r := range letters.String() {
		index = index*26 + int(r-'A'+1)
	}
	return index - 1
}

func resolveWorkbookPath(sourceRoot string, overrides map[string]string, workbook string) (string, error) {
	if override := strings.TrimSpace(overrides[workbook]); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", err
		}
		return override, nil
	}
	if sourceRoot == "" {
		return "", errors.New("no source-root or workbook-path override")
	}
	direct := filepath.Join(sourceRoot, workbook)
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}
	var matches []string
	err := filepath.WalkDir(sourceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".next" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == workbook {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", os.ErrNotExist
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple matches under source-root; use -workbook-path for %s", workbook)
	}
}

func readZipFile(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", os.ErrNotExist, name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func normalizeWorkbookTarget(target string) string {
	target = strings.TrimPrefix(target, "/")
	target = filepath.ToSlash(target)
	if strings.HasPrefix(target, "xl/") {
		return target
	}
	return "xl/" + target
}

func workbookSheetKey(sourceSystem, workbook, sheet string) string {
	return strings.Join([]string{sourceSystem, workbook, sheet}, "\x00")
}

func inputReader(path string) (io.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func boolEnv(name string) bool {
	raw := strings.ToLower(getenv(name))
	return raw == "1" || raw == "true" || raw == "yes"
}

func intEnv(name string, fallback int) int {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
