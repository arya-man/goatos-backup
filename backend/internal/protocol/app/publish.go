package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// ErrNotPublishable is returned when a protocol version fails the source-backed approval gate.
// The wrapped message carries the specific reason for the UI/API.
var ErrNotPublishable = errors.New("protocol: version not publishable")

// ErrInvalidRuleDSL is returned when authored rule_dsl is not valid JSON or carries keys outside the
// versioned protocol-rule DSL schema.
var ErrInvalidRuleDSL = errors.New("protocol: invalid rule_dsl")

// ErrUnsupportedRepeatPolicy is returned when direct protocol authoring attempts to store a repeat
// policy that the generator does not execute yet.
var ErrUnsupportedRepeatPolicy = errors.New("protocol: unsupported repeat policy")

// publishableSources are the real source systems whose approved values may be published. Anything
// else (manual_admin, extracted, empty) stays draft / not source-backed.
var publishableSources = map[string]bool{"vaccinations_db": true, "phc": true, "vet": true}
var executableTriggerTypes = map[string]bool{
	"birth_age":                 true,
	"post_arrival":              true,
	"calendar":                  true,
	"manual_campaign":           true,
	"after_previous_completion": true,
}

type sourceMeta struct {
	SourceSystem string `json:"source_system"`
	SourceRef    string `json:"source_ref"`
	ReviewStatus string `json:"review_status"`
	ApprovedBy   string `json:"approved_by"`
	ApprovedAt   string `json:"approved_at"`
}

type ruleDSLEnvelope struct {
	Category         string          `json:"category"`
	Eligibility      json.RawMessage `json:"eligibility"`
	MissedDosePolicy string          `json:"missed_dose_policy"`
	Source           sourceMeta      `json:"source"`
	Schedule         []scheduleRow   `json:"schedule"`
}

type scheduleRow struct {
	DoseCode    string          `json:"dose_code"`
	Sequence    int32           `json:"sequence"`
	TriggerType string          `json:"trigger_type"`
	OffsetDays  int32           `json:"offset_days"`
	DueWindow   int32           `json:"due_window_days"`
	SOPVersion  string          `json:"sop_version"`
	SOPLabel    string          `json:"sop_label"`
	MinGapDays  int32           `json:"min_gap_days"`
	Repeat      string          `json:"repeat"`
	RepeatUntil string          `json:"repeat_until_after_age"`
	CatchUp     string          `json:"catch_up"`
	ProofPolicy json.RawMessage `json:"proof_policy"`
}

var (
	ruleDSLTopLevelKeys = map[string]bool{
		"category":           true,
		"scope":              true,
		"eligibility":        true,
		"missed_dose_policy": true,
		"stock_policy":       true,
		"schedule":           true,
		"escalation":         true,
		"source":             true,
		"ration":             true,
		"session_timing":     true,
		"inventory_policy":   true,
	}
	ruleDSLScopeKeys = map[string]bool{
		"type": true,
		"id":   true,
	}
	ruleDSLEligibilityKeys = map[string]bool{
		"animal_stage":                true,
		"animal_stage_source":         true,
		"stage":                       true,
		"sex":                         true,
		"breed":                       true,
		"breed_class":                 true,
		"lifecycle":                   true,
		"health":                      true,
		"reproductive":                true,
		"exclude_reproductive_states": true,
		"defer_states":                true,
		"min_age_days":                true,
		"max_age_days":                true,
		"age_band":                    true,
	}
	ruleDSLSourceKeys = map[string]bool{
		"source_system": true,
		"source_ref":    true,
		"imported_at":   true,
		"reviewed_by":   true,
		"review_status": true,
		"approved_by":   true,
		"approved_at":   true,
	}
	ruleDSLScheduleKeys = map[string]bool{
		"dose_code":              true,
		"sequence":               true,
		"trigger_type":           true,
		"offset_days":            true,
		"due_window_days":        true,
		"min_gap_days":           true,
		"repeat":                 true,
		"repeat_until_after_age": true,
		"catch_up":               true,
		"sop_version":            true,
		"sop_label":              true,
		"proof_policy":           true,
	}
	ruleDSLStockPolicyKeys = map[string]bool{
		"vaccine_lot_requirement": true,
		"item_id":                 true,
		"vaccine_item_id":         true,
		"pick":                    true,
		"reject_expired_lot":      true,
		"cold_chain_required":     true,
	}
	ruleDSLRationKeys = map[string]bool{
		"feed_item": true,
		"quantity":  true,
		"unit":      true,
	}
	ruleDSLSessionTimingKeys = map[string]bool{
		"session_order":          true,
		"session_time":           true,
		"packing_proof_policy":   true,
		"execution_proof_policy": true,
	}
	ruleDSLInventoryPolicyKeys = map[string]bool{
		"mode":    true,
		"reserve": true,
		"consume": true,
		"release": true,
	}
)

// ValidateRuleDSL enforces the committed protocol-rule DSL schema shape. It rejects unknown keys
// at every stable authored layer so typo'd immutable config cannot be silently stored.
func ValidateRuleDSL(ruleDSL []byte) error {
	if len(ruleDSL) == 0 {
		return nil
	}
	root, err := decodeRuleDSLObject(ruleDSL, "rule_dsl", ruleDSLTopLevelKeys)
	if err != nil {
		return err
	}
	if raw, ok := root["scope"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.scope", ruleDSLScopeKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["eligibility"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.eligibility", ruleDSLEligibilityKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["source"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.source", ruleDSLSourceKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["stock_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.stock_policy", ruleDSLStockPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["ration"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.ration", ruleDSLRationKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["inventory_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.inventory_policy", ruleDSLInventoryPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["schedule"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := validateRuleDSLArray(raw, "rule_dsl.schedule", ruleDSLScheduleKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["session_timing"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := validateRuleDSLArray(raw, "rule_dsl.session_timing", ruleDSLSessionTimingKeys); err != nil {
			return err
		}
	}
	return nil
}

func decodeRuleDSLObject(raw []byte, path string, allowed map[string]bool) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: %s must be a JSON object: %v", ErrInvalidRuleDSL, path, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: %s must be a JSON object", ErrInvalidRuleDSL, path)
	}
	for key := range obj {
		if !allowed[key] {
			return nil, fmt.Errorf("%w: unknown key %s.%s", ErrInvalidRuleDSL, path, key)
		}
	}
	return obj, nil
}

func validateRuleDSLArray(raw []byte, path string, allowed map[string]bool) error {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return fmt.Errorf("%w: %s must be an array: %v", ErrInvalidRuleDSL, path, err)
	}
	for idx, row := range rows {
		if _, err := decodeRuleDSLObject(row, fmt.Sprintf("%s[%d]", path, idx), allowed); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePublishable enforces the source-backed approval gate on a version's rule_dsl: the nested
// source object must be a real source (vaccinations_db/phc/vet), carry a source_ref, be
// review_status='approved', and name approved_by + approved_at. Pure logic — no DB. Mirrors the
// config-mock gate; this is the backend source of truth. Never publishes manual/extracted/unsourced
// values.
func ValidatePublishable(ruleDSL []byte) error {
	var env ruleDSLEnvelope
	if len(ruleDSL) > 0 {
		if err := json.Unmarshal(ruleDSL, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	s := env.Source
	switch {
	case !publishableSources[strings.TrimSpace(s.SourceSystem)]:
		return fmt.Errorf("%w: source_system must be vaccinations_db/phc/vet (got %q)", ErrNotPublishable, s.SourceSystem)
	case strings.TrimSpace(s.SourceRef) == "":
		return fmt.Errorf("%w: source_ref required", ErrNotPublishable)
	case strings.TrimSpace(s.ReviewStatus) != "approved":
		return fmt.Errorf("%w: review_status must be approved (got %q)", ErrNotPublishable, s.ReviewStatus)
	case strings.TrimSpace(s.ApprovedBy) == "":
		return fmt.Errorf("%w: approved_by required", ErrNotPublishable)
	case strings.TrimSpace(s.ApprovedAt) == "":
		return fmt.Errorf("%w: approved_at required", ErrNotPublishable)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(s.ApprovedAt)); err != nil {
		return fmt.Errorf("%w: approved_at must be RFC3339 (got %q)", ErrNotPublishable, s.ApprovedAt)
	}
	return nil
}

func ValidateExecutionContract(v domain.Version) error {
	if strings.TrimSpace(v.SopVersionID) == "" {
		return fmt.Errorf("%w: missing sop_version_id", ErrNotPublishable)
	}
	if !versionProofHasContent(v.ProofPolicy) {
		return fmt.Errorf("%w: missing proof_policy", ErrNotPublishable)
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	for idx, row := range env.Schedule {
		triggerType := strings.TrimSpace(row.TriggerType)
		if triggerType == "" {
			return fmt.Errorf("%w: schedule[%d] missing trigger_type", ErrNotPublishable, idx)
		}
		if !executableTriggerTypes[triggerType] {
			return fmt.Errorf("%w: schedule[%d] unsupported trigger_type %q", ErrNotPublishable, idx, triggerType)
		}
		if _, err := normalizeRepeatPolicy(row.Repeat, row.MinGapDays); err != nil {
			return fmt.Errorf("%w: schedule[%d] %w", ErrNotPublishable, idx, err)
		}
		if strings.TrimSpace(row.SOPVersion) == "" && strings.TrimSpace(v.SopVersionID) == "" {
			return fmt.Errorf("%w: schedule[%d] missing sop_version", ErrNotPublishable, idx)
		}
		// A row that supplies its own proof_policy must carry real row-level content. A row that
		// omits proof_policy inherits the version-level proof_policy already validated above. The
		// row's own blank/metadata proof does NOT silently fall back to the version proof.
		if len(row.ProofPolicy) > 0 && !rowProofHasContent(row.ProofPolicy) {
			return fmt.Errorf("%w: schedule[%d] missing proof_policy", ErrNotPublishable, idx)
		}
	}
	return nil
}

func normalizeRepeatPolicy(value string, minGapDays int32) (string, error) {
	repeat := strings.TrimSpace(value)
	if repeat == "" {
		return "none", nil
	}
	switch repeat {
	case "none":
		return repeat, nil
	case "yearly", "every_n_days":
		return "", fmt.Errorf("%w: forward recurrence %q is not materialized yet", ErrUnsupportedRepeatPolicy, repeat)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedRepeatPolicy, repeat)
	}
}

// recognizedProofKeys are the object keys under which a proof_policy may carry its proof-token
// array. A token is real only when it is a non-blank string inside one of these arrays.
var recognizedProofKeys = map[string]bool{"required_proofs": true, "types": true, "required": true}

// versionProofHasContent validates a VERSION-level proof_policy (protocol_versions.proof_policy).
// It must be a JSON OBJECT carrying at least one non-blank proof token under a recognized array key
// (required_proofs/types/required). It deliberately rejects everything that is not an object-with-
// real-tokens:
//   - {} , {"required_proofs":[]}            — object but zero tokens
//   - ["video"]                              — array shape is not valid at the version level
//   - {"required_proofs":[""]} / ["  "]      — blank tokens are not requirements
//   - {"subject_scope":"batch"}              — metadata, no recognized proof array
//   - {"required":true}                      — recognized key but scalar, not an array of tokens
func versionProofHasContent(raw []byte) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return false
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for key, val := range obj {
		if recognizedProofKeys[key] && arrayHasNonBlankString(val) {
			return true
		}
	}
	return false
}

// rowProofHasContent validates a ROW-level schedule[].proof_policy. A row may be either a bare array
// of non-blank proof tokens (["video"]) or an object carrying a recognized proof-token array (the
// same object shape as the version level). It rejects blank arrays ([""]), metadata-only objects,
// and scalar values (a bare string/bool/number is never a row proof).
func rowProofHasContent(raw []byte) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch t := v.(type) {
	case []any:
		return arrayHasNonBlankString(t)
	case map[string]any:
		for key, val := range t {
			if recognizedProofKeys[key] && arrayHasNonBlankString(val) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// arrayHasNonBlankString reports whether v is a JSON array holding at least one non-blank string.
func arrayHasNonBlankString(v any) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// PublishVersion publishes a draft version only after the source-backed gate passes. The DB also
// enforces the published-window EXCLUDE non-overlap; capability (CEO/COO protocol.publish.*) is
// enforced at the API/RBAC boundary (later slice).
func (s *Service) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error {
	v, err := s.repo.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if v.Status != "draft" && v.Status != "published" {
		return fmt.Errorf("%w: status=%q", ports.ErrVersionNotDraft, v.Status)
	}
	if err := ValidateRuleDSL(v.RuleDsl); err != nil {
		return err
	}
	if v.Status == "draft" {
		if err := ValidatePublishable(v.RuleDsl); err != nil {
			return err
		}
		if err := ValidateExecutionContract(v); err != nil {
			return err
		}
		if err := s.ensureExecutableRuleRows(ctx, tenantID, v, publishedBy); err != nil {
			return err
		}
	}
	if err := s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy, idempotencyKey...); err != nil {
		return err
	}
	return nil
}

func (s *Service) ensureExecutableRuleRows(ctx context.Context, tenantID string, v domain.Version, createdBy *string) error {
	rules, err := s.repo.ListRules(ctx, tenantID, v.ProtocolVersionID)
	if err != nil {
		return err
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	existing := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if doseCode := strings.TrimSpace(rule.DoseCode); doseCode != "" {
			existing[doseCode] = true
		}
	}
	created := 0
	for idx, row := range env.Schedule {
		in, err := scheduleRowRule(tenantID, v, env, row, idx, createdBy)
		if err != nil {
			return err
		}
		if existing[in.DoseCode] {
			continue
		}
		if _, err := s.repo.CreateRule(ctx, in); err != nil {
			return err
		}
		existing[in.DoseCode] = true
		created++
	}
	if len(rules)+created == 0 {
		return fmt.Errorf("%w: publish requires at least one executable protocol_rules row", ErrNotPublishable)
	}
	return nil
}

func scheduleRowRule(tenantID string, v domain.Version, env ruleDSLEnvelope, row scheduleRow, idx int, createdBy *string) (domain.NewRule, error) {
	doseCode := strings.TrimSpace(row.DoseCode)
	if doseCode == "" {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] missing dose_code", ErrNotPublishable, idx)
	}
	triggerType := strings.TrimSpace(row.TriggerType)
	if triggerType == "" {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] missing trigger_type", ErrNotPublishable, idx)
	}
	repeat, err := normalizeRepeatPolicy(row.Repeat, row.MinGapDays)
	if err != nil {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] %w", ErrNotPublishable, idx, err)
	}
	catchUp := strings.TrimSpace(row.CatchUp)
	if catchUp == "" {
		catchUp = strings.TrimSpace(env.MissedDosePolicy)
	}
	if catchUp == "" {
		catchUp = "immediate"
	}
	sequence := row.Sequence
	if sequence <= 0 {
		sequence = int32(idx + 1)
	}
	proof := row.ProofPolicy
	if len(proof) == 0 {
		proof = v.ProofPolicy
	}
	var sopVersion *string
	if rowSOP := strings.TrimSpace(row.SOPVersion); rowSOP != "" {
		sopVersion = &rowSOP
	}
	return domain.NewRule{
		TenantID:            tenantID,
		ProtocolVersionID:   v.ProtocolVersionID,
		DoseCode:            doseCode,
		Sequence:            sequence,
		TriggerType:         triggerType,
		OffsetDays:          row.OffsetDays,
		DueWindowDays:       row.DueWindow,
		MinGapDays:          row.MinGapDays,
		Repeat:              repeat,
		RepeatUntilAfterAge: strings.TrimSpace(row.RepeatUntil),
		CatchUp:             catchUp,
		EligibilityJSON:     ruleEligibilityJSON(env.Eligibility),
		SopVersionID:        sopVersion,
		ProofPolicy:         proof,
		SortOrder:           int32(idx + 1),
		CreatedBy:           createdBy,
		IdempotencyKey:      "protocol-schedule-rule:" + v.ProtocolVersionID + ":" + doseCode,
	}, nil
}

func ruleEligibilityJSON(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte(`{}`)
	}
	return []byte(trimmed)
}
