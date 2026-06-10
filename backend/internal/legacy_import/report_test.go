package legacy_import

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
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
	details, summary, groups, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
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
	details, _, _, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
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

	_, _, groups, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
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
	}
	report := BuildAnomalyReport(rows, AnomalyReportOptions{})
	assertContextGroup(t, report.Groups, "blank_old_tag_suffix", "CBE", "S1", "P1", 1)
	assertGroup(t, report.Groups, "source_gender_label", "blank_gender", "", 1)
	foundMaskedScope := false
	for _, group := range report.Groups {
		if group.GroupType != "masked_old_tag_scope" {
			continue
		}
		foundMaskedScope = true
		if group.Scope != "park:CBE" || !strings.HasPrefix(group.OldTagRef, "*") || strings.Contains(group.OldTagRef, "OLDSECRET202") {
			t.Fatalf("bad duplicate old-tag group: %#v", group)
		}
	}
	if !foundMaskedScope {
		t.Fatalf("masked duplicate old-tag group not found: %#v", report.Groups)
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
