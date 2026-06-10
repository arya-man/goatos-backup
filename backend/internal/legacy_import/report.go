package legacy_import

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AnomalyReportInputRow struct {
	RowNumber         int
	SourceSystem      string
	SourceDataset     string
	SourceRowKey      string
	ProcessingState   string
	ErrorReason       *string
	NormalizedPayload json.RawMessage
}

type AnomalyReportOptions struct {
	ImportRunID      string
	SourceType       string
	SourceLabel      string
	SheetName        string
	IncludeSensitive bool
}

type AnomalyReportEntry struct {
	RowNumber       int
	ProcessingState string
	ReasonCode      string
	ReasonOrigin    string
	SourceSystem    string
	SourceDataset   string
	SourceRowKeyRef string
	RFID            string
	OldTag          string
}

type AnomalyReport struct {
	Details []AnomalyReportEntry
	Summary map[string]int
}

func BuildAnomalyReport(rows []AnomalyReportInputRow, opts AnomalyReportOptions) AnomalyReport {
	report := AnomalyReport{Summary: map[string]int{}}
	for _, row := range rows {
		payload := normalizedPayloadFields(row.NormalizedPayload)
		rfid := maskIdentifier(payload.rfid, opts.IncludeSensitive)
		oldTag := maskIdentifier(payload.oldTag, opts.IncludeSensitive)
		sourceRef := sourceRowKeyRef(row.SourceRowKey, opts.IncludeSensitive)
		reasons := rowAnomalyReasons(row.ErrorReason, payload.processingReasons)
		reasonCodes := make([]string, 0, len(reasons))
		for reason := range reasons {
			reasonCodes = append(reasonCodes, reason)
		}
		sort.Strings(reasonCodes)
		for _, reason := range reasonCodes {
			report.Details = append(report.Details, AnomalyReportEntry{
				RowNumber:       row.RowNumber,
				ProcessingState: row.ProcessingState,
				ReasonCode:      reason,
				ReasonOrigin:    reasons[reason],
				SourceSystem:    row.SourceSystem,
				SourceDataset:   row.SourceDataset,
				SourceRowKeyRef: sourceRef,
				RFID:            rfid,
				OldTag:          oldTag,
			})
			report.Summary[reason]++
		}
	}
	sort.SliceStable(report.Details, func(i, j int) bool {
		if report.Details[i].ReasonCode == report.Details[j].ReasonCode {
			return report.Details[i].RowNumber < report.Details[j].RowNumber
		}
		return report.Details[i].ReasonCode < report.Details[j].ReasonCode
	})
	return report
}

func rowAnomalyReasons(errorReason *string, processingReasons []string) map[string]string {
	out := map[string]string{}
	for _, reason := range splitReasonCodes(errorReason) {
		addAnomalyReason(out, reason, "error_reason")
	}
	for _, reason := range processingReasons {
		addAnomalyReason(out, reason, "processing_reasons")
	}
	return out
}

func addAnomalyReason(reasons map[string]string, reason, origin string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	existing := reasons[reason]
	switch existing {
	case "":
		reasons[reason] = origin
	case origin:
		return
	default:
		reasons[reason] = existing + "+" + origin
	}
}

func WriteAnomalyReportCSV(outputDir string, opts AnomalyReportOptions, report AnomalyReport) (detailsPath string, summaryPath string, err error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Join(".codex-goatos-render", "import-reports")
	}
	if strings.TrimSpace(opts.ImportRunID) == "" {
		return "", "", fmt.Errorf("import_run_id is required for anomaly report")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", "", err
	}
	base := safeFilename(opts.ImportRunID)
	detailsPath = filepath.Join(outputDir, "anomalies-"+base+".csv")
	summaryPath = filepath.Join(outputDir, "anomaly-summary-"+base+".csv")
	if err := writeAnomalyDetails(detailsPath, opts, report.Details); err != nil {
		return "", "", err
	}
	if err := writeAnomalySummary(summaryPath, opts, report.Summary); err != nil {
		return "", "", err
	}
	return detailsPath, summaryPath, nil
}

func writeAnomalyDetails(path string, opts AnomalyReportOptions, details []AnomalyReportEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	header := []string{"source_type", "source_label", "sheet", "import_run_id", "row_number", "processing_state", "reason_code", "reason_origin", "source_system", "source_dataset", "source_row_key_ref", "rfid_masked", "old_tag_masked"}
	if opts.IncludeSensitive {
		header[len(header)-2] = "rfid"
		header[len(header)-1] = "old_tag"
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, detail := range details {
		if err := writer.Write([]string{
			opts.SourceType,
			opts.SourceLabel,
			opts.SheetName,
			opts.ImportRunID,
			fmt.Sprint(detail.RowNumber),
			detail.ProcessingState,
			detail.ReasonCode,
			detail.ReasonOrigin,
			detail.SourceSystem,
			detail.SourceDataset,
			detail.SourceRowKeyRef,
			detail.RFID,
			detail.OldTag,
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeAnomalySummary(path string, opts AnomalyReportOptions, summary map[string]int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"source_type", "source_label", "sheet", "import_run_id", "reason_code", "count"}); err != nil {
		return err
	}
	reasons := make([]string, 0, len(summary))
	for reason := range summary {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		if err := writer.Write([]string{opts.SourceType, opts.SourceLabel, opts.SheetName, opts.ImportRunID, reason, fmt.Sprint(summary[reason])}); err != nil {
			return err
		}
	}
	return writer.Error()
}

type normalizedReportFields struct {
	rfid              string
	oldTag            string
	processingReasons []string
}

func normalizedPayloadFields(data []byte) normalizedReportFields {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return normalizedReportFields{}
	}
	fields := normalizedReportFields{
		rfid: stringField(payload, "rfid"),
		oldTag: firstNonEmpty(
			stringField(payload, "normalized_old_tag"),
			stringField(payload, "old_tag_id"),
		),
	}
	for _, item := range anySlice(payload["processing_reasons"]) {
		if reason, ok := item.(string); ok && strings.TrimSpace(reason) != "" {
			fields.processingReasons = append(fields.processingReasons, strings.TrimSpace(reason))
		}
	}
	return fields
}

func splitReasonCodes(reason *string) []string {
	if reason == nil || strings.TrimSpace(*reason) == "" {
		return nil
	}
	parts := strings.Split(*reason, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func maskIdentifier(value string, includeSensitive bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if includeSensitive {
		return value
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "import-run"
	}
	return b.String()
}

func sourceRowKeyRef(value string, includeSensitive bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if includeSensitive {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
