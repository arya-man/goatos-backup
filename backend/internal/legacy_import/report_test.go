package legacy_import

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAnomalyReportMasksIdentifiersAndGroupsActualReasons(t *testing.T) {
	errorReason := "malformed_rfid;duplicate_rfid_in_workbook"
	payload, err := json.Marshal(map[string]any{
		"rfid":               longRFID,
		"normalized_old_tag": "OLD-SECRET-1234",
		"processing_reasons": []string{"blank_gender", "missing_rfid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := BuildAnomalyReport([]AnomalyReportInputRow{{
		RowNumber:         12,
		SourceSystem:      "legacy_rfid_db",
		SourceDataset:     "rfid_db_first_import",
		SourceRowKey:      "source_system=legacy_rfid_db|source_dataset=rfid_db_first_import|rfid=masked",
		ProcessingState:   StateNeedsReview,
		ErrorReason:       &errorReason,
		NormalizedPayload: payload,
	}}, AnomalyReportOptions{})

	for _, reason := range []string{"malformed_rfid", "duplicate_rfid_in_workbook", "blank_gender", "missing_rfid"} {
		if report.Summary[reason] != 1 {
			t.Fatalf("summary[%s]=%d, want 1", reason, report.Summary[reason])
		}
	}
	for _, detail := range report.Details {
		if strings.Contains(detail.RFID, longRFID) || strings.Contains(detail.OldTag, "OLD-SECRET-1234") {
			t.Fatalf("detail leaked raw identifiers: %#v", detail)
		}
		if !strings.HasPrefix(detail.RFID, "*") || !strings.HasSuffix(detail.RFID, "0123") {
			t.Fatalf("rfid mask=%q", detail.RFID)
		}
	}
}

func TestWriteAnomalyReportCSVDoesNotWriteRawIdentifiersByDefault(t *testing.T) {
	errorReason := "duplicate_rfid_in_workbook"
	payload, err := json.Marshal(map[string]any{
		"rfid":               longRFID,
		"normalized_old_tag": "1900",
		"processing_reasons": []string{"blank_gender"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := BuildAnomalyReport([]AnomalyReportInputRow{{
		RowNumber:         2,
		ProcessingState:   StateError,
		ErrorReason:       &errorReason,
		NormalizedPayload: payload,
	}}, AnomalyReportOptions{})
	details, summary, groups, _, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-111111111111",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{details, summary, groups} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), longRFID) {
			t.Fatalf("%s leaked raw RFID:\n%s", path, string(data))
		}
		if strings.Contains(string(data), "1900") {
			t.Fatalf("%s leaked raw old tag:\n%s", path, string(data))
		}
	}
}

func TestAnomalyReportDedupesSameReasonPerRowAcrossOrigins(t *testing.T) {
	errorReason := "source_row_changed"
	payload, err := json.Marshal(map[string]any{
		"rfid":               longRFID,
		"normalized_old_tag": "1900",
		"processing_reasons": []string{"source_row_changed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := BuildAnomalyReport([]AnomalyReportInputRow{{
		RowNumber:         8,
		ProcessingState:   StateNeedsReview,
		ErrorReason:       &errorReason,
		NormalizedPayload: payload,
	}}, AnomalyReportOptions{})

	if got := report.Summary["source_row_changed"]; got != 1 {
		t.Fatalf("source_row_changed count=%d, want 1", got)
	}
	if len(report.Details) != 1 {
		t.Fatalf("details=%#v, want one deduped row", report.Details)
	}
	if report.Details[0].ReasonOrigin != "error_reason+processing_reasons" {
		t.Fatalf("origin=%q, want both origins preserved", report.Details[0].ReasonOrigin)
	}
}

func TestAnomalyReportKeepsSourceRowKeyHashedWithIncludeSensitive(t *testing.T) {
	errorReason := "duplicate_rfid_in_workbook"
	rawOldTag := "OLDSECRET999"
	payload, err := json.Marshal(map[string]any{
		"rfid":               longRFID,
		"normalized_old_tag": rawOldTag,
	})
	if err != nil {
		t.Fatal(err)
	}
	report := BuildAnomalyReport([]AnomalyReportInputRow{{
		RowNumber:         3,
		SourceRowKey:      "source_system=legacy_rfid_db|rfid=" + longRFID + "|old_tag=" + rawOldTag,
		ProcessingState:   StateError,
		ErrorReason:       &errorReason,
		NormalizedPayload: payload,
	}}, AnomalyReportOptions{IncludeSensitive: true})
	if len(report.Details) != 1 {
		t.Fatalf("details=%#v, want one row", report.Details)
	}
	if got := report.Details[0].SourceRowKeyRef; !strings.HasPrefix(got, "sha256:") || strings.Contains(got, longRFID) || strings.Contains(got, rawOldTag) {
		t.Fatalf("source_row_key_ref leaked identifier evidence: %q", got)
	}
	details, _, _, _, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
		ImportRunID:      "11111111-1111-4111-8111-333333333333",
		SourceType:       SourceTypeLocalXLSX,
		SourceLabel:      "Synthetic",
		SheetName:        "Combined",
		IncludeSensitive: true,
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(details)
	if err != nil {
		t.Fatal(err)
	}
	rows := readCSVRows(t, data)
	if len(rows) != 2 {
		t.Fatalf("detail csv rows=%d, want header+row", len(rows))
	}
	sourceKeyRefColumn := 10
	got := rows[1][sourceKeyRefColumn]
	if !strings.HasPrefix(got, "sha256:") || strings.Contains(got, longRFID) || strings.Contains(got, rawOldTag) {
		t.Fatalf("source_row_key_ref csv leaked identifier evidence: %q", got)
	}
}

func TestAnomalyReportGroupsPostApplyReasonsAndSafeLabels(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 2, "unknown_status_mapping", map[string]string{
			"Tag":       "Mystery Status",
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S1",
			"Partition": "P1",
			"RFID":      longRFID,
			"Old ID":    "OLDSECRET100",
		}),
		anomalyReportRow(t, 3, "unknown_status_mapping", map[string]string{
			"Tag":       "Mystery Status",
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S2",
			"Partition": "P1",
			"RFID":      "9900000000000000001234567890999",
			"Old ID":    "OLDSECRET101",
		}),
		anomalyReportRow(t, 4, "species_or_breed_requires_review", map[string]string{
			"Tag":       "",
			"Breed":     "Synthetic Mystery Breed",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S2",
			"Partition": "P2",
			"RFID":      "9900000000000000001234567890888",
			"Old ID":    "OLDSECRET102",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	if report.Summary["unknown_status_mapping"] != 2 {
		t.Fatalf("unknown_status_mapping summary=%d, want 2", report.Summary["unknown_status_mapping"])
	}
	if report.Summary["species_or_breed_requires_review"] != 1 {
		t.Fatalf("species_or_breed_requires_review summary=%d, want 1", report.Summary["species_or_breed_requires_review"])
	}
	assertGroup(t, report.Groups, "source_status_label", "unknown_status_mapping", "Mystery Status", 2)
	assertGroup(t, report.Groups, "source_breed_label", "species_or_breed_requires_review", "Synthetic Mystery Breed", 1)

	_, _, groups, _, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-222222222222",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(groups)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{longRFID, "9900000000000000001234567890999", "OLDSECRET100", "OLDSECRET101", "OLDSECRET102"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("grouped report leaked forbidden value %q:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, "Mystery Status") || !strings.Contains(text, "Synthetic Mystery Breed") {
		t.Fatalf("grouped report missing safe labels:\n%s", text)
	}
}

func TestAnomalyReportGroupsContextGenderAndMaskedOldTagScope(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 5, "blank_old_tag_suffix", map[string]string{
			"Tag":       "",
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S1",
			"Partition": "P1",
			"RFID":      longRFID,
			"Old ID":    "OLDSECRET200",
		}),
		anomalyReportRow(t, 6, "blank_gender", map[string]string{
			"Tag":           "",
			"Breed":         "Boer",
			"Gender":        "",
			"Farm":          "CBE",
			"Shed":          "S1",
			"Partition":     "P1",
			"RFID":          "9900000000000000001234567890777",
			"Old ID":        "OLDSECRET201",
			"Old ID Suffix": "CBE",
		}),
		anomalyReportRow(t, 7, "duplicate_old_tag_same_scope", map[string]string{
			"Tag":           "",
			"Breed":         "Boer",
			"Gender":        "Female",
			"Farm":          "CBE",
			"Shed":          "S1",
			"Partition":     "P1",
			"RFID":          "9900000000000000001234567890666",
			"Old ID":        "OLDSECRET202",
			"Old ID Suffix": "CBE",
		}),
		anomalyReportRow(t, 8, "duplicate_old_tag_same_scope", map[string]string{
			"Tag":           "",
			"Breed":         "Boer",
			"Gender":        "Female",
			"Farm":          "CBE",
			"Shed":          "S1",
			"Partition":     "P1",
			"RFID":          "9900000000000000001234567890555",
			"Old ID":        "1901",
			"Old ID Suffix": "CBE",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	assertContextGroup(t, report.Groups, "blank_old_tag_suffix", "CBE", "S1", "P1", 1)
	assertGroup(t, report.Groups, "source_gender_label", "blank_gender", "", 1)
	refs := map[string]bool{}
	for _, group := range report.Groups {
		if group.GroupType != "masked_old_tag_scope" {
			continue
		}
		if group.Scope != "park:CBE" || !strings.HasPrefix(group.OldTagRef, "sha256:") || strings.Contains(group.OldTagRef, "OLDSECRET202") || strings.Contains(group.OldTagRef, "1901") || group.OldTagRef == "****" {
			t.Fatalf("bad duplicate old-tag group: %#v", group)
		}
		refs[group.OldTagRef] = true
	}
	if len(refs) != 2 {
		t.Fatalf("duplicate old-tag refs=%#v, want two stable non-reversible refs", refs)
	}
}

func TestAnomalyReportGroupsEscapeSpreadsheetFormulaCells(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 9, "unknown_status_mapping", map[string]string{
			"Tag":       "=1+1",
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S1",
			"Partition": "P1",
			"RFID":      longRFID,
			"Old ID":    "OLDSECRET300",
		}),
		anomalyReportRow(t, 10, "species_or_breed_requires_review", map[string]string{
			"Tag":       "",
			"Breed":     "+SUM(A1:A2)",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S1",
			"Partition": "P1",
			"RFID":      "9900000000000000001234567890444",
			"Old ID":    "OLDSECRET301",
		}),
		anomalyReportRow(t, 11, "blank_old_tag_suffix", map[string]string{
			"Tag":       "",
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "-Farm",
			"Shed":      "@Shed",
			"Partition": "=Partition",
			"RFID":      "9900000000000000001234567890333",
			"Old ID":    "OLDSECRET302",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	_, _, groups, _, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-444444444444",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(groups)
	if err != nil {
		t.Fatal(err)
	}
	records := readCSVRows(t, data)
	if len(records) < 4 {
		t.Fatalf("group csv records=%d, want header plus grouped rows", len(records))
	}
	assertCSVCellPresent(t, records, "'=1+1")
	assertCSVCellPresent(t, records, "'+SUM(A1:A2)")
	assertCSVCellPresent(t, records, "'-Farm")
	assertCSVCellPresent(t, records, "'@Shed")
	assertCSVCellPresent(t, records, "'=Partition")
	for _, dangerous := range []string{"=1+1", "+SUM(A1:A2)", "-Farm", "@Shed", "=Partition"} {
		assertCSVCellAbsent(t, records, dangerous)
	}
}

func TestWriteAnomalyReportCSVWritesReviewerFocusedFiles(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 12, "species_or_breed_requires_review", map[string]string{
			"Breed":     "Anantapur Sheep",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S1",
			"Partition": "P1",
			"RFID":      longRFID,
			"Old ID":    "OLDSECRET400",
		}),
		anomalyReportRow(t, 13, "blank_old_tag_suffix", map[string]string{
			"Breed":     "Boer",
			"Gender":    "Female",
			"Farm":      "CBE",
			"Shed":      "S2",
			"Partition": "P2",
			"RFID":      "9900000000000000001234567890222",
			"Old ID":    "OLDSECRET401",
		}),
		anomalyReportRow(t, 14, "blank_gender", map[string]string{
			"Breed":     "Boer",
			"Gender":    "",
			"Farm":      "CBE",
			"Shed":      "S3",
			"Partition": "P3",
			"RFID":      "9900000000000000001234567890111",
			"Old ID":    "OLDSECRET402",
		}),
		anomalyReportRow(t, 15, "duplicate_old_tag_same_scope", map[string]string{
			"Breed":         "Boer",
			"Gender":        "Female",
			"Farm":          "CBE",
			"Shed":          "S4",
			"Partition":     "P4",
			"RFID":          "9900000000000000001234567890000",
			"Old ID":        "OLDSECRET403",
			"Old ID Suffix": "CBE",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	dir := t.TempDir()
	_, _, _, reviewerDir, err := WriteAnomalyReportCSV(dir, AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-555555555555",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{
		"review-summary.csv",
		"needs-review-rows.csv",
		"non-goat-exclusion-candidates.csv",
		"blank-old-tag-suffix.csv",
		"blank-gender.csv",
		"duplicate-old-tag-same-scope.csv",
		"README.txt",
	} {
		if _, err := os.Stat(filepath.Join(reviewerDir, filename)); err != nil {
			t.Fatalf("%s not generated: %v", filename, err)
		}
	}
	if _, err := os.Stat(filepath.Join(reviewerDir, "breed", "species-needs-classification.csv")); !os.IsNotExist(err) {
		t.Fatalf("breed/species-needs-classification.csv generated for all confirmed non-goat rows: %v", err)
	}
	nonGoatRows := readCSVFile(t, filepath.Join(reviewerDir, "non-goat-exclusion-candidates.csv"))
	assertCSVCellPresent(t, nonGoatRows, "Anantapur Sheep")
	assertCSVCellPresent(t, nonGoatRows, "If source_breed is Anantapur Sheep, confirm non-goat exclusion and do not create goat; otherwise classify goat breed/species before import.")

	needsRows := readCSVFile(t, filepath.Join(reviewerDir, "needs-review-rows.csv"))
	if got := len(needsRows) - 1; got != len(report.Details) {
		t.Fatalf("needs-review-rows count=%d, want details=%d", got, len(report.Details))
	}
	summaryRows := readCSVFile(t, filepath.Join(reviewerDir, "review-summary.csv"))
	if got := sumCSVCounts(t, summaryRows, "count"); got != len(report.Details) {
		t.Fatalf("review-summary total=%d, want details=%d", got, len(report.Details))
	}
}

func TestReviewerCSVCreatesBreedClassificationOnlyForUnknownBreed(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 16, "species_or_breed_requires_review", map[string]string{
			"Breed":  "Synthetic Mystery Breed",
			"Gender": "Female",
			"Farm":   "CBE",
			"RFID":   longRFID,
			"Old ID": "OLDSECRET500",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	dir := t.TempDir()
	_, _, _, reviewerDir, err := WriteAnomalyReportCSV(dir, AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-666666666666",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	classificationRows := readCSVFile(t, filepath.Join(reviewerDir, "breed", "species-needs-classification.csv"))
	assertCSVCellPresent(t, classificationRows, "Synthetic Mystery Breed")
	nonGoatRows := readCSVFile(t, filepath.Join(reviewerDir, "non-goat-exclusion-candidates.csv"))
	if got := len(nonGoatRows); got != 1 {
		t.Fatalf("non-goat rows=%d, want header only for unknown breed", got)
	}
}

func TestReviewerCSVRewritesRunDirectoryWithoutStaleOptionalFiles(t *testing.T) {
	importRunID := "11111111-1111-4111-8111-aaaaaaaaaaaa"
	dir := t.TempDir()

	unknownBreedReport := BuildAnomalyReport([]AnomalyReportInputRow{
		anomalyReportRow(t, 20, "species_or_breed_requires_review", map[string]string{
			"Breed":  "Synthetic Mystery Breed",
			"Gender": "Female",
			"Farm":   "CBE",
			"RFID":   longRFID,
			"Old ID": "OLDSECRET800",
		}),
	}, AnomalyReportOptions{})
	_, _, _, reviewerDir, err := WriteAnomalyReportCSV(dir, AnomalyReportOptions{
		ImportRunID: importRunID,
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, unknownBreedReport)
	if err != nil {
		t.Fatal(err)
	}
	classificationPath := filepath.Join(reviewerDir, "breed", "species-needs-classification.csv")
	if _, err := os.Stat(classificationPath); err != nil {
		t.Fatalf("expected initial breed classification file: %v", err)
	}

	confirmedNonGoatReport := BuildAnomalyReport([]AnomalyReportInputRow{
		anomalyReportRow(t, 21, "species_or_breed_requires_review", map[string]string{
			"Breed":  "Anantapur Sheep",
			"Gender": "Female",
			"Farm":   "CBE",
			"RFID":   "9900000000000000001234567890888",
			"Old ID": "OLDSECRET801",
		}),
	}, AnomalyReportOptions{})
	_, _, _, reviewerDir, err = WriteAnomalyReportCSV(dir, AnomalyReportOptions{
		ImportRunID: importRunID,
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, confirmedNonGoatReport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(reviewerDir, "breed", "species-needs-classification.csv")); !os.IsNotExist(err) {
		t.Fatalf("stale breed classification file remained after same-run rewrite: %v", err)
	}
	nonGoatRows := readCSVFile(t, filepath.Join(reviewerDir, "non-goat-exclusion-candidates.csv"))
	assertCSVCellPresent(t, nonGoatRows, "Anantapur Sheep")
}

func TestReviewerCSVDefaultMasksIdentifiersAndSensitiveModeIncludesThem(t *testing.T) {
	rawOldTag := "OLDSECRET600"
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 17, "blank_gender", map[string]string{
			"Breed":  "Boer",
			"Gender": "",
			"Farm":   "CBE",
			"RFID":   longRFID,
			"Old ID": rawOldTag,
		}),
	}
	defaultReport := BuildAnomalyReport(rows, AnomalyReportOptions{})
	defaultDir := t.TempDir()
	_, _, _, defaultReviewerDir, err := WriteAnomalyReportCSV(defaultDir, AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-777777777777",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, defaultReport)
	if err != nil {
		t.Fatal(err)
	}
	defaultData, err := os.ReadFile(filepath.Join(defaultReviewerDir, "needs-review-rows.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defaultText := string(defaultData)
	if strings.Contains(defaultText, longRFID) || strings.Contains(defaultText, rawOldTag) {
		t.Fatalf("default reviewer CSV leaked identifiers:\n%s", defaultText)
	}
	if !strings.Contains(defaultText, "rfid_masked") || !strings.Contains(defaultText, "old_tag_ref") {
		t.Fatalf("default reviewer CSV missing masked/hash headers:\n%s", defaultText)
	}

	sensitiveReport := BuildAnomalyReport(rows, AnomalyReportOptions{IncludeSensitive: true})
	sensitiveDir := t.TempDir()
	_, _, _, sensitiveReviewerDir, err := WriteAnomalyReportCSV(sensitiveDir, AnomalyReportOptions{
		ImportRunID:      "11111111-1111-4111-8111-888888888888",
		SourceType:       SourceTypeLocalXLSX,
		SourceLabel:      "Synthetic",
		SheetName:        "Combined",
		IncludeSensitive: true,
	}, sensitiveReport)
	if err != nil {
		t.Fatal(err)
	}
	sensitiveData, err := os.ReadFile(filepath.Join(sensitiveReviewerDir, "needs-review-rows.csv"))
	if err != nil {
		t.Fatal(err)
	}
	sensitiveText := string(sensitiveData)
	if !strings.Contains(sensitiveText, longRFID) || !strings.Contains(sensitiveText, rawOldTag) {
		t.Fatalf("sensitive reviewer CSV did not include identifiers:\n%s", sensitiveText)
	}
	if !strings.Contains(sensitiveText, "rfid") || !strings.Contains(sensitiveText, "old_tag") {
		t.Fatalf("sensitive reviewer CSV missing raw identifier headers:\n%s", sensitiveText)
	}
}

func TestReviewerCSVEscapesFormulaCells(t *testing.T) {
	rows := []AnomalyReportInputRow{
		anomalyReportRow(t, 18, "blank_old_tag_suffix", map[string]string{
			"Tag":       "=tag",
			"Breed":     "+breed",
			"Gender":    "@gender",
			"Farm":      "-farm",
			"Shed":      "=shed",
			"Partition": "+partition",
			"RFID":      longRFID,
			"Old ID":    "OLDSECRET700",
		}),
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	dir := t.TempDir()
	_, _, _, reviewerDir, err := WriteAnomalyReportCSV(dir, AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-999999999999",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	records := readCSVFile(t, filepath.Join(reviewerDir, "blank-old-tag-suffix.csv"))
	for _, want := range []string{"'=tag", "'+breed", "'@gender", "'-farm", "'=shed", "'+partition"} {
		assertCSVCellPresent(t, records, want)
	}
	for _, dangerous := range []string{"=tag", "+breed", "@gender", "-farm", "=shed", "+partition"} {
		assertCSVCellAbsent(t, records, dangerous)
	}
}

func TestSafeCSVCellEscapesFormulaPrefixesAfterWhitespace(t *testing.T) {
	cases := map[string]string{
		"\t=1+1":      "'\t=1+1",
		"\r+SUM(A:A)": "'\r+SUM(A:A)",
		"\n-42":       "'\n-42",
		" @cmd":       "' @cmd",
		" old tag ":   " old tag ",
	}
	for input, want := range cases {
		if got := safeCSVCell(input); got != want {
			t.Fatalf("safeCSVCell(%q)=%q, want %q", input, got, want)
		}
	}
}

func anomalyReportRow(t *testing.T, rowNumber int, reason string, raw map[string]string) AnomalyReportInputRow {
	t.Helper()
	rawPayload, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	normalized := map[string]any{
		"rfid":                 raw["RFID"],
		"normalized_old_tag":   raw["Old ID"],
		"normalized_park_code": raw["Old ID Suffix"],
		"tag":                  raw["Tag"],
		"breed":                raw["Breed"],
		"gender":               raw["Gender"],
		"farm":                 raw["Farm"],
		"shed":                 raw["Shed"],
		"partition":            raw["Partition"],
		"processing_reasons":   []string{reason},
	}
	normalizedPayload, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return AnomalyReportInputRow{
		RowNumber:         rowNumber,
		ProcessingState:   StateNeedsReview,
		ErrorReason:       &reason,
		RawPayload:        rawPayload,
		NormalizedPayload: normalizedPayload,
	}
}

func assertGroup(t *testing.T, groups []AnomalyReportGroup, groupType, reason, label string, wantCount int) {
	t.Helper()
	for _, group := range groups {
		if group.GroupType == groupType && group.ReasonCode == reason && group.LabelValue == label {
			if group.Count != wantCount {
				t.Fatalf("group %#v count=%d, want %d", group, group.Count, wantCount)
			}
			return
		}
	}
	t.Fatalf("group %s/%s/%q not found in %#v", groupType, reason, label, groups)
}

func assertContextGroup(t *testing.T, groups []AnomalyReportGroup, reason, farm, shed, partition string, wantCount int) {
	t.Helper()
	for _, group := range groups {
		if group.GroupType == "safe_context" && group.ReasonCode == reason && group.Farm == farm && group.Shed == shed && group.Partition == partition {
			if group.Count != wantCount {
				t.Fatalf("context group %#v count=%d, want %d", group, group.Count, wantCount)
			}
			return
		}
	}
	t.Fatalf("context group %s/%s/%s/%s not found in %#v", reason, farm, shed, partition, groups)
}

func readCSVRows(t *testing.T, data []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func readCSVFile(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return readCSVRows(t, data)
}

func sumCSVCounts(t *testing.T, records [][]string, columnName string) int {
	t.Helper()
	if len(records) == 0 {
		t.Fatal("missing CSV header")
	}
	column := -1
	for i, header := range records[0] {
		if header == columnName {
			column = i
			break
		}
	}
	if column < 0 {
		t.Fatalf("column %q not found in header %#v", columnName, records[0])
	}
	total := 0
	for _, record := range records[1:] {
		if column >= len(record) {
			t.Fatalf("record too short for count column: %#v", record)
		}
		count, err := strconv.Atoi(record[column])
		if err != nil {
			t.Fatalf("parse count %q: %v", record[column], err)
		}
		total += count
	}
	return total
}

func assertCSVCellPresent(t *testing.T, records [][]string, want string) {
	t.Helper()
	for _, record := range records {
		for _, cell := range record {
			if cell == want {
				return
			}
		}
	}
	t.Fatalf("CSV cell %q not found in %#v", want, records)
}

func assertCSVCellAbsent(t *testing.T, records [][]string, forbidden string) {
	t.Helper()
	for _, record := range records {
		for _, cell := range record {
			if cell == forbidden {
				t.Fatalf("CSV cell %q was not escaped in %#v", forbidden, records)
			}
		}
	}
}
