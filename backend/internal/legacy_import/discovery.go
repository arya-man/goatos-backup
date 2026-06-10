package legacy_import

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	SourceTypeLocalXLSX   = "local_xlsx"
	SourceTypeGoogleSheet = "google_sheet"
)

var shape1WorkbookColumns = []string{
	"Farm",
	"Origin Farm",
	"Old Tag ID",
	"RFID",
	"Breed",
	"Gender",
	"Shed",
	"Shed Tag",
	"Age",
}

func DiscoverXLSXSource(data []byte, sheetName string) (SourceDiscoveryResult, error) {
	if len(data) == 0 {
		return SourceDiscoveryResult{}, errors.New("workbook is empty")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return SourceDiscoveryResult{}, errors.New("input workbook is not a valid xlsx file")
	}
	sharedStrings, err := readSharedStrings(reader)
	if err != nil {
		return SourceDiscoveryResult{}, err
	}
	sheets, err := workbookSheets(reader)
	if err != nil {
		return SourceDiscoveryResult{}, err
	}
	sheetPath, err := worksheetPath(reader, sheetName)
	if err != nil {
		return SourceDiscoveryResult{}, err
	}
	targetSheet := strings.TrimSpace(sheetName)
	if targetSheet == "" && len(sheets) > 0 {
		targetSheet = sheets[0].Name
	}
	sheetBytes, err := readZipFile(reader, sheetPath)
	if err != nil {
		return SourceDiscoveryResult{}, fmt.Errorf("read worksheet %q: %w", displaySheetName(sheetName), err)
	}
	headers, err := worksheetHeaders(sheetBytes, sharedStrings)
	if err != nil {
		return SourceDiscoveryResult{}, err
	}
	result := SourceDiscoveryResult{
		SourceType:             SourceTypeLocalXLSX,
		TargetSheet:            targetSheet,
		SheetNames:             sheetNames(sheets),
		Headers:                headers,
		GoogleSheetExportBuilt: false,
	}
	classifyHeaders(&result)
	return result, nil
}

func GoogleSheetDiscoverySkipped() SourceDiscoveryResult {
	return SourceDiscoveryResult{
		SourceType:             SourceTypeGoogleSheet,
		Classification:         "google_sheet_export_skipped",
		Importable:             false,
		GoogleSheetExportBuilt: false,
		RecommendedNextStep:    "Export the source sheet to a local XLSX file and run rfid-import --source-type=local_xlsx --input <file> --sheet Combined.",
	}
}

func classifyHeaders(result *SourceDiscoveryResult) {
	normalized := normalizedHeaderSet(result.Headers)
	missingShape2 := missingHeaders(normalized, requiredWorkbookColumns)
	switch {
	case len(missingShape2) == 0:
		result.Classification = SourceClassificationImportableShape2
		result.Importable = true
		result.RecommendedNextStep = "Run rfid-import with --sheet " + result.TargetSheet + " to stage Shape 2 RFID source rows."
	case len(missingHeaders(normalized, shape1WorkbookColumns)) == 0:
		result.Classification = SourceClassificationRecognizedShape1NeedsMap
		result.Importable = false
		result.MissingRequiredHeaders = missingShape2
		result.RecommendedNextStep = "Shape 1 is recognized but not importable until the Phase 1 mapping extension is written."
	default:
		result.Classification = SourceClassificationOperationalOrUnknown
		result.Importable = false
		result.MissingRequiredHeaders = missingShape2
		result.RecommendedNextStep = "Use the Shape 2 RFID source export with headers: " + strings.Join(requiredWorkbookColumns, ", ") + "."
	}
}

func normalizedHeaderSet(headers []string) map[string]bool {
	set := make(map[string]bool, len(headers))
	for _, header := range headers {
		set[strings.ToLower(strings.TrimSpace(header))] = true
	}
	return set
}

func missingHeaders(headerSet map[string]bool, required []string) []string {
	var missing []string
	for _, header := range required {
		if !headerSet[strings.ToLower(header)] {
			missing = append(missing, header)
		}
	}
	sort.Strings(missing)
	return missing
}

func headersEqualIgnoringOrder(headers []string, expected []string) bool {
	if len(headers) != len(expected) {
		return false
	}
	set := normalizedHeaderSet(headers)
	for _, header := range expected {
		if !set[strings.ToLower(header)] {
			return false
		}
	}
	for header := range set {
		if !slices.ContainsFunc(expected, func(expectedHeader string) bool {
			return strings.EqualFold(header, expectedHeader)
		}) {
			return false
		}
	}
	return true
}
