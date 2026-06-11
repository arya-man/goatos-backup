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
	RawPayload        json.RawMessage
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
	OldTagRef       string
	Farm            string
	Shed            string
	Partition       string
	SourceStatus    string
	SourceBreed     string
	SourceGender    string
}

type AnomalyReport struct {
	Details []AnomalyReportEntry
	Summary map[string]int
	Groups  []AnomalyReportGroup
}

type AnomalyReportGroup struct {
	GroupType  string
	ReasonCode string
	Count      int
	LabelField string
	LabelValue string
	Farm       string
	Shed       string
	Partition  string
	OldTagRef  string
	Scope      string
	ReviewNote string
}

func BuildAnomalyReport(rows []AnomalyReportInputRow, opts AnomalyReportOptions) AnomalyReport {
	report := AnomalyReport{Summary: map[string]int{}}
	for _, row := range rows {
		payload := normalizedPayloadFields(row.NormalizedPayload)
		raw := safeRawPayloadFields(row.RawPayload, payload)
		rfid := maskIdentifier(payload.rfid, opts.IncludeSensitive)
		oldTag := maskIdentifier(payload.oldTag, opts.IncludeSensitive)
		oldTagRef := scopedIdentifierRef(payload.oldTag, payload.oldTagScope)
		sourceRef := sourceRowKeyRef(row.SourceRowKey)
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
				OldTagRef:       oldTagRef,
				Farm:            raw.farm,
				Shed:            raw.shed,
				Partition:       raw.partition,
				SourceStatus:    raw.tag,
				SourceBreed:     raw.breed,
				SourceGender:    raw.gender,
			})
			report.Summary[reason]++
			addAnomalyGroup(&report, reason, raw, payload)
		}
	}
	sort.SliceStable(report.Details, func(i, j int) bool {
		if report.Details[i].ReasonCode == report.Details[j].ReasonCode {
			return report.Details[i].RowNumber < report.Details[j].RowNumber
		}
		return report.Details[i].ReasonCode < report.Details[j].ReasonCode
	})
	sort.SliceStable(report.Groups, func(i, j int) bool {
		left := report.Groups[i]
		right := report.Groups[j]
		if left.ReasonCode != right.ReasonCode {
			return left.ReasonCode < right.ReasonCode
		}
		if left.GroupType != right.GroupType {
			return left.GroupType < right.GroupType
		}
		if left.LabelValue != right.LabelValue {
			return left.LabelValue < right.LabelValue
		}
		if left.Farm != right.Farm {
			return left.Farm < right.Farm
		}
		if left.Shed != right.Shed {
			return left.Shed < right.Shed
		}
		if left.Partition != right.Partition {
			return left.Partition < right.Partition
		}
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		return left.OldTagRef < right.OldTagRef
	})
	return report
}

func addAnomalyGroup(report *AnomalyReport, reason string, raw safeReportFields, normalized normalizedReportFields) {
	switch reason {
	case "unknown_status_mapping":
		report.addGroup(AnomalyReportGroup{
			GroupType:  "source_status_label",
			ReasonCode: reason,
			LabelField: "Tag",
			LabelValue: raw.tag,
		})
	case "species_or_breed_requires_review":
		report.addGroup(AnomalyReportGroup{
			GroupType:  "source_breed_label",
			ReasonCode: reason,
			LabelField: "Breed",
			LabelValue: raw.breed,
			ReviewNote: "Decide whether each label is a goat breed alias or an exclusion; do not auto-alias from this report alone.",
		})
	case "blank_old_tag_suffix":
		report.addGroup(AnomalyReportGroup{
			GroupType:  "safe_context",
			ReasonCode: reason,
			Farm:       raw.farm,
			Shed:       raw.shed,
			Partition:  raw.partition,
			ReviewNote: "RFID-only creation for blank old-tag suffix rows is a future policy decision, not part of this report.",
		})
	case "blank_gender", "unknown_gender":
		report.addGroup(AnomalyReportGroup{
			GroupType:  "source_gender_label",
			ReasonCode: reason,
			LabelField: "Gender",
			LabelValue: raw.gender,
		})
	case "duplicate_old_tag_same_scope":
		report.addGroup(AnomalyReportGroup{
			GroupType:  "masked_old_tag_scope",
			ReasonCode: reason,
			OldTagRef:  scopedIdentifierRef(normalized.oldTag, normalized.oldTagScope),
			Scope:      normalized.oldTagScope,
		})
	}
}

func (r *AnomalyReport) addGroup(group AnomalyReportGroup) {
	group.LabelValue = strings.TrimSpace(group.LabelValue)
	group.Farm = strings.TrimSpace(group.Farm)
	group.Shed = strings.TrimSpace(group.Shed)
	group.Partition = strings.TrimSpace(group.Partition)
	group.OldTagRef = strings.TrimSpace(group.OldTagRef)
	group.Scope = strings.TrimSpace(group.Scope)
	for i := range r.Groups {
		existing := &r.Groups[i]
		if existing.GroupType == group.GroupType &&
			existing.ReasonCode == group.ReasonCode &&
			existing.LabelField == group.LabelField &&
			existing.LabelValue == group.LabelValue &&
			existing.Farm == group.Farm &&
			existing.Shed == group.Shed &&
			existing.Partition == group.Partition &&
			existing.OldTagRef == group.OldTagRef &&
			existing.Scope == group.Scope {
			existing.Count++
			return
		}
	}
	group.Count = 1
	r.Groups = append(r.Groups, group)
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

func WriteAnomalyReportCSV(outputDir string, opts AnomalyReportOptions, report AnomalyReport) (detailsPath string, summaryPath string, groupsPath string, reviewerDir string, err error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Join(".codex-goatos-render", "import-reports")
	}
	if strings.TrimSpace(opts.ImportRunID) == "" {
		return "", "", "", "", fmt.Errorf("import_run_id is required for anomaly report")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", "", "", "", err
	}
	base := safeFilename(opts.ImportRunID)
	detailsPath = filepath.Join(outputDir, "anomalies-"+base+".csv")
	summaryPath = filepath.Join(outputDir, "anomaly-summary-"+base+".csv")
	groupsPath = filepath.Join(outputDir, "anomaly-groups-"+base+".csv")
	reviewerDir = filepath.Join(outputDir, "reviewer-"+base)
	if err := writeAnomalyDetails(detailsPath, opts, report.Details); err != nil {
		return "", "", "", "", err
	}
	if err := writeAnomalySummary(summaryPath, opts, report.Summary); err != nil {
		return "", "", "", "", err
	}
	if err := writeAnomalyGroups(groupsPath, opts, report.Groups); err != nil {
		return "", "", "", "", err
	}
	if err := writeReviewerCSVPack(reviewerDir, opts, report); err != nil {
		return "", "", "", "", err
	}
	return detailsPath, summaryPath, groupsPath, reviewerDir, nil
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
		if err := writeSafeCSVRow(writer, []string{
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
		if err := writeSafeCSVRow(writer, []string{opts.SourceType, opts.SourceLabel, opts.SheetName, opts.ImportRunID, reason, fmt.Sprint(summary[reason])}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeAnomalyGroups(path string, opts AnomalyReportOptions, groups []AnomalyReportGroup) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{
		"source_type",
		"source_label",
		"sheet",
		"import_run_id",
		"group_type",
		"reason_code",
		"count",
		"label_field",
		"label_value",
		"farm",
		"shed",
		"partition",
		"old_tag_ref",
		"scope",
		"review_note",
	}); err != nil {
		return err
	}
	for _, group := range groups {
		if err := writeSafeCSVRow(writer, []string{
			opts.SourceType,
			opts.SourceLabel,
			opts.SheetName,
			opts.ImportRunID,
			group.GroupType,
			group.ReasonCode,
			fmt.Sprint(group.Count),
			group.LabelField,
			group.LabelValue,
			group.Farm,
			group.Shed,
			group.Partition,
			group.OldTagRef,
			group.Scope,
			group.ReviewNote,
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeReviewerCSVPack(outputDir string, opts AnomalyReportOptions, report AnomalyReport) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	if err := writeReviewSummary(filepath.Join(outputDir, "review-summary.csv"), opts, report.Summary); err != nil {
		return err
	}
	focused := []struct {
		filename string
		filter   func(AnomalyReportEntry) bool
	}{
		{
			filename: "needs-review-rows.csv",
			filter: func(AnomalyReportEntry) bool {
				return true
			},
		},
		{
			filename: "non-goat-exclusion-candidates.csv",
			filter: func(entry AnomalyReportEntry) bool {
				return isNonGoatExclusionCandidate(entry)
			},
		},
		{
			filename: "blank-old-tag-suffix.csv",
			filter: func(entry AnomalyReportEntry) bool {
				return entry.ReasonCode == "blank_old_tag_suffix"
			},
		},
		{
			filename: "blank-gender.csv",
			filter: func(entry AnomalyReportEntry) bool {
				return entry.ReasonCode == "blank_gender" || entry.ReasonCode == "unknown_gender"
			},
		},
		{
			filename: "duplicate-old-tag-same-scope.csv",
			filter: func(entry AnomalyReportEntry) bool {
				return entry.ReasonCode == "duplicate_old_tag_same_scope"
			},
		},
	}
	for _, item := range focused {
		if err := writeFocusedReviewRows(filepath.Join(outputDir, item.filename), opts, report.Details, item.filter); err != nil {
			return err
		}
	}
	classificationRows := filterAnomalyEntries(report.Details, func(entry AnomalyReportEntry) bool {
		return entry.ReasonCode == "species_or_breed_requires_review" && !isConfirmedNonGoatBreedLabel(entry.SourceBreed)
	})
	if len(classificationRows) > 0 {
		breedDir := filepath.Join(outputDir, "breed")
		if err := os.MkdirAll(breedDir, 0755); err != nil {
			return err
		}
		if err := writeFocusedReviewRows(filepath.Join(breedDir, "species-needs-classification.csv"), opts, classificationRows, func(AnomalyReportEntry) bool {
			return true
		}); err != nil {
			return err
		}
	}
	return writeReviewerInstructions(filepath.Join(outputDir, "README.txt"), opts)
}

func writeReviewSummary(path string, opts AnomalyReportOptions, summary map[string]int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"source_type", "source_label", "sheet", "import_run_id", "reason_code", "count", "suggested_action", "focused_file"}); err != nil {
		return err
	}
	reasons := make([]string, 0, len(summary))
	for reason := range summary {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		if err := writeSafeCSVRow(writer, []string{
			opts.SourceType,
			opts.SourceLabel,
			opts.SheetName,
			opts.ImportRunID,
			reason,
			fmt.Sprint(summary[reason]),
			suggestedAction(reason),
			focusedFilename(reason),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeFocusedReviewRows(path string, opts AnomalyReportOptions, details []AnomalyReportEntry, filter func(AnomalyReportEntry) bool) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	identifierHeader := "rfid_masked"
	oldTagHeader := "old_tag_ref"
	if opts.IncludeSensitive {
		identifierHeader = "rfid"
		oldTagHeader = "old_tag"
	}
	if err := writer.Write([]string{
		"source_type",
		"source_label",
		"sheet",
		"import_run_id",
		"row_number",
		"processing_state",
		"reason_code",
		"reason_origin",
		"farm",
		"shed",
		"partition",
		"source_status",
		"source_breed",
		"source_gender",
		"source_row_key_ref",
		identifierHeader,
		oldTagHeader,
		"suggested_action",
		"reviewer_action",
		"reviewer_notes",
	}); err != nil {
		return err
	}
	for _, detail := range details {
		if filter != nil && !filter(detail) {
			continue
		}
		oldTag := detail.OldTagRef
		if opts.IncludeSensitive {
			oldTag = detail.OldTag
		}
		if err := writeSafeCSVRow(writer, []string{
			opts.SourceType,
			opts.SourceLabel,
			opts.SheetName,
			opts.ImportRunID,
			fmt.Sprint(detail.RowNumber),
			detail.ProcessingState,
			detail.ReasonCode,
			detail.ReasonOrigin,
			detail.Farm,
			detail.Shed,
			detail.Partition,
			detail.SourceStatus,
			detail.SourceBreed,
			detail.SourceGender,
			detail.SourceRowKeyRef,
			detail.RFID,
			oldTag,
			suggestedAction(detail.ReasonCode),
			"",
			"",
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeReviewerInstructions(path string, opts AnomalyReportOptions) error {
	lines := []string{
		"RFID import reviewer report",
		"",
		"This folder is generated after rfid-apply, so reason codes reflect post-apply review outcomes.",
		"Existing anomaly detail, reason summary, and grouped summary CSVs are still written for audit and reconciliation.",
		"",
		"Reviewer CSVs are export-only. Goat OS does not ingest reviewer_action or reviewer_notes yet.",
		"Apply corrections in the source workbook, or in a future approved correction overlay, then rerun the normal Shape-2 import/apply flow.",
		"",
		"Default CSVs mask RFID and hash old-tag/source-row references. Internal cleanup usually needs --include-sensitive.",
		"Never commit generated reports or private source data.",
		"",
		"non-goat-exclusion-candidates.csv contains confirmed non-goat labels such as Anantapur Sheep; confirm exclusion and do not create goats.",
		"blank-old-tag-suffix.csv is for source correction or a future RFID-only creation policy decision.",
		"blank-gender.csv is for source correction or an explicit reviewed sex policy.",
		"duplicate-old-tag-same-scope.csv is for source correction, conflict review, or merge review.",
	}
	if opts.IncludeSensitive {
		lines = append(lines, "", "Sensitive mode was used for this report; keep these files local-only.")
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func filterAnomalyEntries(details []AnomalyReportEntry, filter func(AnomalyReportEntry) bool) []AnomalyReportEntry {
	out := []AnomalyReportEntry{}
	for _, detail := range details {
		if filter(detail) {
			out = append(out, detail)
		}
	}
	return out
}

func isNonGoatExclusionCandidate(entry AnomalyReportEntry) bool {
	return entry.ReasonCode == "species_or_breed_requires_review" && isConfirmedNonGoatBreedLabel(entry.SourceBreed)
}

func isConfirmedNonGoatBreedLabel(label string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(label), " ")) {
	case "anantapur sheep":
		return true
	default:
		return false
	}
}

func focusedFilename(reason string) string {
	switch reason {
	case "species_or_breed_requires_review":
		return "non-goat-exclusion-candidates.csv or breed/species-needs-classification.csv"
	case "blank_old_tag_suffix":
		return "blank-old-tag-suffix.csv"
	case "blank_gender", "unknown_gender":
		return "blank-gender.csv"
	case "duplicate_old_tag_same_scope":
		return "duplicate-old-tag-same-scope.csv"
	default:
		return "needs-review-rows.csv"
	}
}

func suggestedAction(reason string) string {
	switch reason {
	case "species_or_breed_requires_review":
		return "If source_breed is Anantapur Sheep, confirm non-goat exclusion and do not create goat; otherwise classify goat breed/species before import."
	case "blank_old_tag_suffix":
		return "Choose future RFID-only creation policy or correct source old-tag suffix before import."
	case "blank_gender":
		return "Correct source gender or apply an explicit reviewed sex policy before import."
	case "unknown_gender":
		return "Correct source gender label or approve a reviewed sex mapping before import."
	case "duplicate_old_tag_same_scope":
		return "Correct source old tag or complete conflict/merge review before import."
	default:
		return "Review source data, correct upstream, and rerun import/apply."
	}
}

func writeSafeCSVRow(writer *csv.Writer, fields []string) error {
	safe := make([]string, len(fields))
	for i, field := range fields {
		safe[i] = safeCSVCell(field)
	}
	return writer.Write(safe)
}

type normalizedReportFields struct {
	rfid              string
	oldTag            string
	oldTagScope       string
	tag               string
	breed             string
	gender            string
	farm              string
	shed              string
	partition         string
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
		oldTagScope: oldTagScope(payload),
		tag:         stringField(payload, "tag"),
		breed:       stringField(payload, "breed"),
		gender:      stringField(payload, "gender"),
		farm:        stringField(payload, "farm"),
		shed:        stringField(payload, "shed"),
		partition:   stringField(payload, "partition"),
	}
	for _, item := range anySlice(payload["processing_reasons"]) {
		if reason, ok := item.(string); ok && strings.TrimSpace(reason) != "" {
			fields.processingReasons = append(fields.processingReasons, strings.TrimSpace(reason))
		}
	}
	return fields
}

type safeReportFields struct {
	tag       string
	breed     string
	gender    string
	farm      string
	shed      string
	partition string
}

func safeRawPayloadFields(rawData []byte, normalized normalizedReportFields) safeReportFields {
	fields := safeReportFields{}
	var raw map[string]any
	_ = json.Unmarshal(rawData, &raw)
	fields.tag = safeLabel(firstNonEmpty(anyString(raw["Tag"]), normalized.tag))
	fields.breed = safeLabel(firstNonEmpty(anyString(raw["Breed"]), normalized.breed))
	fields.gender = safeLabel(firstNonEmpty(anyString(raw["Gender"]), normalized.gender))
	fields.farm = safeLabel(firstNonEmpty(anyString(raw["Farm"]), normalized.farm))
	fields.shed = safeLabel(firstNonEmpty(anyString(raw["Shed"]), normalized.shed))
	fields.partition = safeLabel(firstNonEmpty(anyString(raw["Partition"]), normalized.partition))
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

func sourceRowKeyRef(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func scopedIdentifierRef(value, scope string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value + "\x00" + strings.TrimSpace(scope)))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func safeCSVCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func anyString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func safeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20:
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > 120 {
		return out[:120]
	}
	return out
}

func oldTagScope(payload map[string]any) string {
	if parkCode := stringField(payload, "normalized_park_code"); parkCode != "" {
		return "park:" + parkCode
	}
	for _, item := range anySlice(payload["identifier_candidates"]) {
		candidate, ok := item.(map[string]any)
		if !ok || stringField(candidate, "identifier_type") != "old_tag" {
			continue
		}
		if scope := stringField(candidate, "scope_key"); scope != "" {
			return scope
		}
	}
	return ""
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
