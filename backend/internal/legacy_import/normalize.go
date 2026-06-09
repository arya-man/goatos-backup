package legacy_import

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type normalizedDraft struct {
	row               WorkbookRow
	rawPayload        map[string]string
	normalizedPayload map[string]any
	sourceRowKey      string
	versionHash       string
	processingState   string
	reasons           []string
	errorReason       *string
	rfid              string
	oldTag            string
	parkCode          string
	oldTagScopeKey    string
}

func NormalizeWorkbookRows(policy Policy, tenantID string, rows []WorkbookRow) ([]StagedRow, error) {
	rfidCounts := map[string]int{}
	oldTagScopeCounts := map[string]int{}
	drafts := make([]normalizedDraft, 0, len(rows))

	for _, row := range rows {
		draft, err := normalizeRow(policy, row)
		if err != nil {
			return nil, err
		}
		if draft.rfid != "" && draft.errorReason == nil {
			rfidCounts[draft.rfid]++
		}
		if draft.oldTag != "" && draft.parkCode != "" {
			oldTagScopeCounts[draft.oldTag+"|"+draft.parkCode]++
		}
		drafts = append(drafts, draft)
	}

	staged := make([]StagedRow, 0, len(drafts))
	for _, draft := range drafts {
		if draft.rfid != "" && rfidCounts[draft.rfid] > 1 {
			draft.setError("duplicate_rfid_in_workbook")
		}
		if draft.oldTag != "" && draft.parkCode != "" && oldTagScopeCounts[draft.oldTag+"|"+draft.parkCode] > 1 && draft.errorReason == nil {
			draft.addReview("duplicate_old_tag_same_scope")
		}
		if draft.errorReason == nil && len(draft.reasons) > 0 {
			draft.processingState = StateNeedsReview
		}
		draft.normalizedPayload["processing_reasons"] = append([]string(nil), draft.reasons...)
		if draft.errorReason != nil {
			draft.normalizedPayload["error_reason"] = *draft.errorReason
		}
		rawPayload, err := json.Marshal(draft.rawPayload)
		if err != nil {
			return nil, err
		}
		normalizedPayload, err := json.Marshal(draft.normalizedPayload)
		if err != nil {
			return nil, err
		}
		sourceRecordID := draft.sourceRowKey
		staged = append(staged, StagedRow{
			TenantID:             tenantID,
			RowNumber:            draft.row.RowNumber,
			SourceSystem:         policy.SourceSystem,
			SourceDataset:        policy.SourceDataset,
			SourceRecordID:       &sourceRecordID,
			SourceRowKey:         draft.sourceRowKey,
			SourceKeyRecipeVer:   policy.SourceKeyRecipeVersion,
			SourceRowVersionHash: draft.versionHash,
			HashRecipeVersion:    policy.HashRecipeVersion,
			RawPayload:           rawPayload,
			NormalizedPayload:    normalizedPayload,
			ProcessingState:      draft.processingState,
			ErrorReason:          draft.errorReason,
		})
	}
	return staged, nil
}

func MarkChangedRowsNeedsReview(rows []StagedRow, changed map[string]bool) []StagedRow {
	for i := range rows {
		if !changed[rows[i].SourceRowKey] || rows[i].ProcessingState == StateError {
			continue
		}
		rows[i].ProcessingState = StateNeedsReview
		reason := appendReason(rows[i].ErrorReason, "source_row_changed")
		rows[i].ErrorReason = &reason
		var payload map[string]any
		if err := json.Unmarshal(rows[i].NormalizedPayload, &payload); err == nil {
			reasons, _ := payload["processing_reasons"].([]any)
			reasons = append(reasons, "source_row_changed")
			payload["processing_reasons"] = reasons
			if updated, err := json.Marshal(payload); err == nil {
				rows[i].NormalizedPayload = updated
			}
		}
	}
	return rows
}

func normalizeRow(policy Policy, row WorkbookRow) (normalizedDraft, error) {
	raw := orderedRawPayload(row.Raw)
	rfid, malformedRFID := normalizeRFID(row.Raw["RFID"])
	oldTag := normalizeIdentifierText(row.Raw["Old ID"])
	parkCode := normalizeParkCode(row.Raw["Old ID Suffix"])
	gender := normalizeIdentifierText(row.Raw["Gender"])
	sex, sexOK := sexFromGender(gender)
	tag := strings.TrimSpace(row.Raw["Tag"])
	growthCohort := growthCohortFromTag(tag)

	payload := map[string]any{
		"source_system":        policy.SourceSystem,
		"source_dataset":       policy.SourceDataset,
		"normalizer_version":   policy.NormalizerVersion,
		"farm":                 normalizeIdentifierText(row.Raw["Farm"]),
		"origin_farm":          "",
		"old_tag_id":           oldTag,
		"normalized_old_tag":   oldTag,
		"old_id_suffix":        strings.TrimSpace(row.Raw["Old ID Suffix"]),
		"normalized_park_code": parkCode,
		"rfid":                 rfid,
		"age":                  normalizeIdentifierText(row.Raw["Age"]),
		"gender":               gender,
		"breed":                strings.TrimSpace(row.Raw["Breed"]),
		"tag":                  tag,
		"shed":                 strings.TrimSpace(row.Raw["Shed"]),
		"shed_tag":             tag,
		"partition":            strings.TrimSpace(row.Raw["Partition"]),
		"growth_cohort_tag":    growthCohort,
	}
	if sex != "" && sexOK {
		payload["sex"] = sex
	} else {
		payload["sex"] = nil
	}

	identifierCandidates := make([]map[string]any, 0, 2)
	if rfid != "" && !malformedRFID {
		identifierCandidates = append(identifierCandidates, map[string]any{
			"identifier_type":  "rfid",
			"normalized_value": rfid,
			"scope_key":        "global",
		})
	}
	if oldTag != "" && parkCode != "" {
		identifierCandidates = append(identifierCandidates, map[string]any{
			"identifier_type":  "old_tag",
			"normalized_value": oldTag,
			"scope_key":        "park:" + parkCode,
		})
	}
	payload["identifier_candidates"] = identifierCandidates

	draft := normalizedDraft{
		row:               row,
		rawPayload:        raw,
		normalizedPayload: payload,
		processingState:   StatePending,
		rfid:              rfid,
		oldTag:            oldTag,
		parkCode:          parkCode,
	}
	if oldTag != "" && parkCode != "" {
		draft.oldTagScopeKey = "park:" + parkCode
	}
	if malformedRFID {
		draft.setError("malformed_rfid")
	}
	if rfid == "" {
		draft.addReview("missing_rfid")
	}
	if oldTag != "" && parkCode == "" {
		draft.addReview("blank_old_tag_suffix")
	}
	if oldTag == "" && rfid == "" {
		draft.addReview("tagless_identity_evidence")
	}
	if gender == "" {
		draft.addReview("blank_gender")
	} else if !sexOK {
		draft.addReview("unknown_gender")
	}

	sourceKey, err := BuildSourceRowKey(policy, payload)
	if err != nil {
		return normalizedDraft{}, err
	}
	versionHash, err := BuildSourceRowVersionHash(policy, payload)
	if err != nil {
		return normalizedDraft{}, err
	}
	draft.sourceRowKey = sourceKey
	draft.versionHash = versionHash
	return draft, nil
}

func BuildSourceRowKey(policy Policy, normalized map[string]any) (string, error) {
	fields := policy.SourceKeyRecipe.Fields
	if len(fields) == 0 {
		return "", fmt.Errorf("source key recipe has no fields")
	}
	forbidden := map[string]bool{}
	for _, field := range policy.SourceKeyRecipe.ForbiddenFields {
		forbidden[field] = true
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if forbidden[field] {
			return "", fmt.Errorf("source key recipe uses forbidden field %q", field)
		}
		parts = append(parts, field+"="+escapeKeyPart(valueForField(policy, normalized, field)))
	}
	return strings.Join(parts, "|"), nil
}

func BuildSourceRowVersionHash(policy Policy, normalized map[string]any) (string, error) {
	fields := policy.HashRecipe.IncludeFields
	if len(fields) == 0 {
		return "", fmt.Errorf("hash recipe has no include fields")
	}
	excluded := map[string]bool{}
	for _, field := range policy.HashRecipe.ExcludeFields {
		excluded[field] = true
	}
	type pair struct {
		Field string `json:"field"`
		Value string `json:"value"`
	}
	pairs := make([]pair, 0, len(fields))
	for _, field := range fields {
		if excluded[field] {
			continue
		}
		pairs = append(pairs, pair{Field: field, Value: valueForField(policy, normalized, field)})
	}
	serialized, err := json.Marshal(pairs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(policy.HashRecipeVersion + "\n" + string(serialized)))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func valueForField(policy Policy, normalized map[string]any, field string) string {
	switch field {
	case "source_system":
		return policy.SourceSystem
	case "source_dataset":
		return policy.SourceDataset
	case "normalized_old_tag":
		return stringValue(normalized["normalized_old_tag"])
	case "normalized_park_code":
		return stringValue(normalized["normalized_park_code"])
	case "rfid":
		return stringValue(normalized["rfid"])
	case "old_tag_id":
		return stringValue(normalized["old_tag_id"])
	case "farm", "origin_farm", "breed", "gender", "shed", "shed_tag", "age":
		return stringValue(normalized[field])
	default:
		return stringValue(normalized[field])
	}
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func (d *normalizedDraft) setError(reason string) {
	d.processingState = StateError
	d.errorReason = &reason
}

func (d *normalizedDraft) addReview(reason string) {
	if d.processingState == StateError {
		return
	}
	d.reasons = append(d.reasons, reason)
}

func appendReason(existing *string, reason string) string {
	if existing == nil || *existing == "" {
		return reason
	}
	return *existing + ";" + reason
}

func orderedRawPayload(raw map[string]string) map[string]string {
	out := make(map[string]string, len(requiredWorkbookColumns))
	for _, key := range requiredWorkbookColumns {
		out[key] = raw[key]
	}
	return out
}

func normalizeRFID(raw string) (string, bool) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return "", false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return value, true
		}
	}
	return value, false
}

func normalizeIdentifierText(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizeParkCode(raw string) string {
	value := normalizeIdentifierText(raw)
	switch value {
	case "CJB":
		return "CBE"
	case "BLR":
		return "CPT"
	default:
		return value
	}
}

func sexFromGender(gender string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(gender)) {
	case "MALE", "M":
		return "male", true
	case "FEMALE", "F":
		return "female", true
	case "":
		return "", false
	default:
		return "", false
	}
}

func growthCohortFromTag(tag string) *string {
	normalized := strings.ToUpper(strings.TrimSpace(tag))
	switch normalized {
	case "F2", "F2-MALE", "F2-FEMALE", "FATTENING":
		value := "F2"
		return &value
	default:
		return nil
	}
}

func escapeKeyPart(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `|`, `\|`, `=`, `\=`)
	return replacer.Replace(value)
}

func StateCounts(rows []StagedRow) map[string]int {
	counts := map[string]int{
		StatePending:     0,
		StateNeedsReview: 0,
		StateError:       0,
	}
	for _, row := range rows {
		counts[row.ProcessingState]++
	}
	return counts
}

func ErrorCount(rows []StagedRow) int {
	count := 0
	for _, row := range rows {
		if row.ProcessingState == StateError {
			count++
		}
	}
	return count
}

func StableStateSummary(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
