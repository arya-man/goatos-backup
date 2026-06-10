package legacy_import

import (
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
	details, summary, err := WriteAnomalyReportCSV(t.TempDir(), AnomalyReportOptions{
		ImportRunID: "11111111-1111-4111-8111-111111111111",
		SourceType:  SourceTypeLocalXLSX,
		SourceLabel: "Synthetic",
		SheetName:   "Combined",
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{details, summary} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), longRFID) {
			t.Fatalf("%s leaked raw RFID:\n%s", path, string(data))
		}
	}
}
